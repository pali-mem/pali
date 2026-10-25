package memory

import (
	"context"
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
// stored text. Importance is not rescored on content changes. Derived
// observations and entity facts created from the old content are not rewritten.
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
	}
	return &updated, nil
}
