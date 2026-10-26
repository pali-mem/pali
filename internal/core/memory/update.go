package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/pali-mem/pali/internal/domain"
)

// UpdateInput is a partial update; nil fields are left unchanged.
type UpdateInput struct {
	Content    *string
	Tags       *[]string
	Tier       *domain.MemoryTier
	Importance *float64
}

func (in UpdateInput) empty() bool {
	return in.Content == nil && in.Tags == nil && in.Tier == nil && in.Importance == nil
}

// Update applies a partial update to a memory. When content is supplied the
// lexical index is rewritten and the memory is re-embedded and upserted into
// the vector store synchronously, so every backend stays consistent with the
// stored text. Importance is not rescored on content changes. A content change
// drops the memory's own entity facts; for a raw turn it also re-runs parser
// (or structured dual-write) extraction on the new text and deletes the
// derived memories of the old text that the new text no longer produces.
func (s *Service) Update(ctx context.Context, tenantID, memoryID string, in UpdateInput) (*domain.Memory, error) {
	tenantID = strings.TrimSpace(tenantID)
	memoryID = strings.TrimSpace(memoryID)
	if tenantID == "" || memoryID == "" || in.empty() {
		return nil, domain.ErrInvalidInput
	}
	if in.Content != nil && strings.TrimSpace(*in.Content) == "" {
		return nil, domain.ErrInvalidInput
	}
	if in.Importance != nil && (*in.Importance < 0 || *in.Importance > 1) {
		return nil, domain.ErrInvalidInput
	}
	if in.Tier != nil {
		switch *in.Tier {
		case domain.MemoryTierAuto, domain.MemoryTierWorking, domain.MemoryTierEpisodic, domain.MemoryTierSemantic:
		default:
			return nil, domain.ErrInvalidInput
		}
	}
	if s.embedder == nil || s.vector == nil {
		return nil, fmt.Errorf("memory service dependencies are not initialized")
	}
	repo, ok := s.repo.(domain.MemoryUpdateRepository)
	if !ok || repo == nil {
		return nil, fmt.Errorf("memory repository does not support updates")
	}

	existing, err := s.Get(ctx, tenantID, memoryID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, domain.ErrNotFound
	}

	patch := domain.MemoryPatch{Tags: in.Tags, Importance: in.Importance}
	content := existing.Content
	if in.Content != nil {
		content = *in.Content
		patch.Content = in.Content
		// Refresh the implicit raw-turn identity so dedupe keys follow the new text.
		if existing.Kind == domain.MemoryKindRawTurn &&
			existing.CanonicalKey == buildRawTurnIdentity(existing.Content).CanonicalKey {
			identity := buildRawTurnIdentity(content)
			patch.CanonicalKey = &identity.CanonicalKey
			patch.SourceTurnHash = &identity.SourceTurnHash
		}
	}
	if in.Tier != nil {
		tier := *in.Tier
		if tier == domain.MemoryTierAuto {
			tags := existing.Tags
			if in.Tags != nil {
				tags = *in.Tags
			}
			tier = resolveTier(StoreInput{
				Content:   content,
				Tier:      domain.MemoryTierAuto,
				Tags:      tags,
				CreatedBy: existing.CreatedBy,
			})
		}
		patch.Tier = &tier
	}

	// Embed before writing so an embedder failure leaves the memory untouched.
	var embedding []float32
	if in.Content != nil {
		next := *existing
		next.Content = content
		next.QueryViewText = "" // cleared by the repository on content change
		embedding, err = s.embedder.Embed(ctx, embeddingLookupTextForMemory(next))
		if err != nil {
			return nil, err
		}
		if len(embedding) == 0 {
			return nil, fmt.Errorf("embedding vector is empty")
		}
	}

	updated, err := repo.Update(ctx, tenantID, memoryID, patch)
	if err != nil {
		return nil, err
	}
	if in.Content != nil {
		ids := []string{memoryID}
		s.markIndexState(ctx, tenantID, ids, domain.MemoryIndexOperationUpsert, domain.MemoryIndexStatePending, nil)
		if err := s.vector.Upsert(ctx, tenantID, memoryID, embedding); err != nil {
			s.markIndexState(ctx, tenantID, ids, domain.MemoryIndexOperationUpsert, domain.MemoryIndexStateFailed, err)
			return nil, fmt.Errorf("memory updated but vector reindex failed: %w", err)
		}
		s.markIndexState(ctx, tenantID, ids, domain.MemoryIndexOperationUpsert, domain.MemoryIndexStateIndexed, nil)
		if err := s.deleteEntityFactsForMemories(ctx, tenantID, ids); err != nil {
			return nil, fmt.Errorf("memory updated but entity fact cleanup failed: %w", err)
		}
		if updated.Kind == domain.MemoryKindRawTurn {
			if err := s.rederiveFromEditedTurn(ctx, updated, existing.Content); err != nil {
				return nil, fmt.Errorf("memory updated but re-deriving facts failed: %w", err)
			}
		}
	}
	return &updated, nil
}

// rederiveSourceTurnLimit bounds the derived memories looked up for one turn.
const rederiveSourceTurnLimit = 256

// rederiveFromEditedTurn derives facts from an edited raw turn the way Store
// does, then deletes the memories derived from its previous content that the
// new content did not reuse. Derived memories are tied to a turn by the hash of
// its normalized content; when another raw turn still has the previous content
// they are shared with it and kept.
func (s *Service) rederiveFromEditedTurn(ctx context.Context, raw domain.Memory, previousContent string) error {
	previousHash := buildSourceTurnHash(previousContent)
	if previousHash == buildSourceTurnHash(raw.Content) {
		return nil
	}

	var stale []string
	if lister, ok := s.repo.(domain.MemorySourceTurnRepository); ok && lister != nil {
		previous, err := lister.ListBySourceTurnHash(ctx, raw.TenantID, previousHash, rederiveSourceTurnLimit)
		if err != nil {
			return err
		}
		for _, m := range previous {
			if m.ID == raw.ID {
				continue
			}
			if m.Kind == domain.MemoryKindRawTurn {
				stale = nil
				break
			}
			if m.CreatedBy == domain.MemoryCreatedBySystem && m.SourceFactIndex >= 0 {
				stale = append(stale, m.ID)
			}
		}
	}

	derived, err := s.deriveFromRawTurn(ctx, raw)
	if err != nil {
		return err
	}
	kept := make(map[string]struct{}, len(derived))
	for _, id := range derived {
		kept[id] = struct{}{}
	}
	newHash := buildSourceTurnHash(raw.Content)
	repo, _ := s.repo.(domain.MemoryUpdateRepository)
	for _, id := range stale {
		if _, ok := kept[id]; ok {
			// Reused by relation-tuple dedupe: move it to the new turn so the
			// next edit of this turn finds it.
			if repo != nil {
				if _, err := repo.Update(ctx, raw.TenantID, id, domain.MemoryPatch{SourceTurnHash: &newHash}); err != nil && !errors.Is(err, domain.ErrNotFound) {
					return err
				}
			}
			continue
		}
		if err := s.Delete(ctx, raw.TenantID, id); err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
	}
	return nil
}

// deriveFromRawTurn writes the parser (or structured dual-write) memories and
// entity facts for an already-stored raw turn and returns their IDs. It mirrors
// storeWithParser without storing the raw turn again.
func (s *Service) deriveFromRawTurn(ctx context.Context, raw domain.Memory) ([]string, error) {
	if !s.parser.Enabled || s.infoParser == nil {
		return s.writeCanonicalStructuredDerived(ctx, raw)
	}
	parsed, err := s.parseFactsWithFallback(ctx, raw.Content, 1)
	if err != nil {
		// Store keeps the raw turn without derived facts on parser errors.
		s.logInfof("[pali-update] memory=%s parser_error=%v", raw.ID, err)
		return nil, nil
	}
	embeddingByContent, err := s.precomputeParserEmbeddings(ctx, raw.Content, parsed.Facts)
	if err != nil {
		embeddingByContent = nil
	}
	ids := make([]string, 0, len(parsed.Facts))
	for factIdx, fact := range parsed.Facts {
		m, err := s.applyParsedFact(
			ctx,
			raw.TenantID,
			raw.Content,
			raw.Tags,
			raw.Source,
			fact,
			factIdx,
			parsed.Extractor,
			parsed.ExtractorVersion,
			embeddingByContent,
		)
		if err != nil {
			return nil, err
		}
		if m == nil {
			continue
		}
		ids = append(ids, m.ID)
		if entityFact, ok := buildEntityFactRecord(*m, fact); ok {
			if err := s.storeEntityFacts(ctx, []domain.EntityFact{entityFact}); err != nil {
				return nil, err
			}
		}
	}
	return ids, nil
}
