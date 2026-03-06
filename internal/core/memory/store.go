package memory

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/vein05/pali/internal/domain"
)

const (
	// DualWriteDedupeThreshold is the semantic similarity cutoff used to suppress
	// near-duplicate legacy structured dual-write entries.
	DualWriteDedupeThreshold = 0.92

	dualWriteLexicalSafetyThreshold = 0.50
	dualWriteCandidateTopK          = 12
	minParsedFactContentLength      = 25
)

var dualWriteNegationTokens = map[string]struct{}{
	"not":   {},
	"no":    {},
	"never": {},
}

type StoreInput struct {
	TenantID  string
	Content   string
	Tier      domain.MemoryTier
	Kind      domain.MemoryKind
	Tags      []string
	Source    string
	CreatedBy domain.MemoryCreatedBy
}

func (s *Service) Store(ctx context.Context, in StoreInput) (domain.Memory, error) {
	stored, err := s.StoreBatch(ctx, []StoreInput{in})
	if err != nil {
		return domain.Memory{}, err
	}
	if len(stored) != 1 {
		return domain.Memory{}, fmt.Errorf("store batch returned %d records for single input", len(stored))
	}
	return stored[0], nil
}

type preparedStoreInput struct {
	idx          int
	input        StoreInput
	resolvedTier domain.MemoryTier
	resolvedKind domain.MemoryKind
}

func (s *Service) StoreBatch(ctx context.Context, inputs []StoreInput) ([]domain.Memory, error) {
	if len(inputs) == 0 {
		return []domain.Memory{}, nil
	}

	prepared, err := s.prepareStoreInputs(ctx, inputs)
	if err != nil {
		return nil, err
	}

	results := make([]domain.Memory, len(prepared))
	nonParser := make([]preparedStoreInput, 0, len(prepared))
	parserItems := make([]preparedStoreInput, 0, len(prepared))
	for _, item := range prepared {
		if s.parser.Enabled && item.resolvedKind == domain.MemoryKindRawTurn && s.infoParser != nil {
			parserItems = append(parserItems, item)
			continue
		}
		nonParser = append(nonParser, item)
	}

	if len(parserItems) > 0 {
		stored, err := s.storeBatchWithParser(ctx, parserItems)
		if err != nil {
			return nil, err
		}
		for i := range parserItems {
			results[parserItems[i].idx] = stored[i]
		}
	}

	if len(nonParser) > 0 {
		stored, err := s.storeBatchWithoutParser(ctx, nonParser)
		if err != nil {
			return nil, err
		}
		for i := range nonParser {
			results[nonParser[i].idx] = stored[i]
		}
	}

	return results, nil
}

func (s *Service) prepareStoreInputs(ctx context.Context, inputs []StoreInput) ([]preparedStoreInput, error) {
	if s.scorer == nil || s.embedder == nil || s.vector == nil {
		return nil, fmt.Errorf("memory service dependencies are not initialized")
	}

	prepared := make([]preparedStoreInput, 0, len(inputs))
	tenantChecked := make(map[string]struct{}, len(inputs))
	for i := range inputs {
		in := inputs[i]
		tenantID := strings.TrimSpace(in.TenantID)
		if tenantID == "" || strings.TrimSpace(in.Content) == "" {
			return nil, domain.ErrInvalidInput
		}
		if _, ok := tenantChecked[tenantID]; !ok {
			if err := s.ensureTenantExists(ctx, tenantID); err != nil {
				return nil, err
			}
			tenantChecked[tenantID] = struct{}{}
		}
		in.TenantID = tenantID
		prepared = append(prepared, preparedStoreInput{
			idx:          i,
			input:        in,
			resolvedTier: resolveTier(in),
			resolvedKind: resolveKind(in.Kind),
		})
	}
	return prepared, nil
}

func (s *Service) storeBatchWithoutParser(ctx context.Context, items []preparedStoreInput) ([]domain.Memory, error) {
	if len(items) == 0 {
		return []domain.Memory{}, nil
	}

	memories := make([]domain.Memory, 0, len(items))
	contents := make([]string, 0, len(items))
	for _, item := range items {
		importance, err := s.scorer.Score(ctx, item.input.Content)
		if err != nil {
			return nil, err
		}
		memories = append(memories, domain.Memory{
			TenantID:   item.input.TenantID,
			Content:    item.input.Content,
			Tier:       item.resolvedTier,
			Kind:       item.resolvedKind,
			Tags:       item.input.Tags,
			Source:     item.input.Source,
			CreatedBy:  item.input.CreatedBy,
			Importance: importance,
		})
		contents = append(contents, item.input.Content)
	}

	embeddings, err := s.embedContents(ctx, contents)
	if err != nil {
		return nil, err
	}
	stored, err := s.storeInRepo(ctx, memories)
	if err != nil {
		return nil, err
	}
	if len(stored) != len(embeddings) {
		return nil, fmt.Errorf("store result mismatch: stored=%d embeddings=%d", len(stored), len(embeddings))
	}
	if err := s.upsertStoredEmbeddings(ctx, stored, embeddings); err != nil {
		return nil, err
	}

	for _, memory := range stored {
		if err := s.writeLegacyStructuredDerived(ctx, memory); err != nil {
			return nil, err
		}
	}
	return stored, nil
}

func (s *Service) storeWithParser(
	ctx context.Context,
	in StoreInput,
	resolvedTier domain.MemoryTier,
	resolvedKind domain.MemoryKind,
) (domain.Memory, error) {
	facts, err := s.parseFactsWithFallback(ctx, in.Content)
	if err != nil {
		if s.parser.StoreRawTurn {
			return s.storeOne(ctx, domain.Memory{
				TenantID:  in.TenantID,
				Content:   in.Content,
				Tier:      resolvedTier,
				Kind:      resolvedKind,
				Tags:      in.Tags,
				Source:    in.Source,
				CreatedBy: in.CreatedBy,
			})
		}
		// Parser errors should not drop writes; fallback to raw turn store.
		return s.storeOne(ctx, domain.Memory{
			TenantID:  in.TenantID,
			Content:   in.Content,
			Tier:      resolvedTier,
			Kind:      resolvedKind,
			Tags:      in.Tags,
			Source:    in.Source,
			CreatedBy: in.CreatedBy,
		})
	}

	embeddingByContent, err := s.precomputeParserEmbeddings(ctx, in.Content, facts)
	if err != nil {
		// Batch precompute is an optimization; fallback preserves existing behavior.
		embeddingByContent = nil
	}

	var storedRaw domain.Memory
	if s.parser.StoreRawTurn {
		storedRaw, err = s.storeOneWithOptionalEmbedding(ctx, domain.Memory{
			TenantID:  in.TenantID,
			Content:   in.Content,
			Tier:      resolvedTier,
			Kind:      resolvedKind,
			Tags:      in.Tags,
			Source:    in.Source,
			CreatedBy: in.CreatedBy,
		}, embeddingByContent)
		if err != nil {
			return domain.Memory{}, err
		}
	}

	var firstParsed *domain.Memory
	for _, fact := range facts {
		m, err := s.applyParsedFact(ctx, in.TenantID, in.Tags, in.Source, fact, embeddingByContent)
		if err != nil {
			return domain.Memory{}, err
		}
		if m != nil {
			if entityFact, ok := buildEntityFactRecord(*m, fact); ok {
				if err := s.storeEntityFacts(ctx, []domain.EntityFact{entityFact}); err != nil {
					return domain.Memory{}, err
				}
			}
		}
		if firstParsed == nil && m != nil {
			firstParsed = m
		}
	}

	if s.parser.StoreRawTurn {
		return storedRaw, nil
	}
	if firstParsed != nil {
		return *firstParsed, nil
	}
	// Parser yielded no storable facts; preserve reliability by storing raw input.
	return s.storeOneWithOptionalEmbedding(ctx, domain.Memory{
		TenantID:  in.TenantID,
		Content:   in.Content,
		Tier:      resolvedTier,
		Kind:      resolvedKind,
		Tags:      in.Tags,
		Source:    in.Source,
		CreatedBy: in.CreatedBy,
	}, embeddingByContent)
}

func (s *Service) parseFactsWithFallback(ctx context.Context, content string) ([]ParsedFact, error) {
	primaryFacts, primaryErr := s.infoParser.Parse(ctx, content, s.parser.MaxFacts)
	if primaryErr == nil && len(primaryFacts) > 0 {
		prepared := prepareParsedFactsForStore(content, primaryFacts)
		if len(prepared) > 0 {
			return prepared, nil
		}
	}

	fallback := NewHeuristicInfoParser()
	fallbackFacts, fallbackErr := fallback.Parse(ctx, content, s.parser.MaxFacts)
	if fallbackErr == nil && len(fallbackFacts) > 0 {
		prepared := prepareParsedFactsForStore(content, fallbackFacts)
		if len(prepared) > 0 {
			return prepared, nil
		}
	}

	if primaryErr != nil {
		return nil, primaryErr
	}
	if fallbackErr != nil {
		return nil, fallbackErr
	}
	return []ParsedFact{}, nil
}

func (s *Service) precomputeParserEmbeddings(
	ctx context.Context,
	rawContent string,
	facts []ParsedFact,
) (map[string][]float32, error) {
	batchEmbedder, ok := s.embedder.(domain.BatchEmbedder)
	if !ok || batchEmbedder == nil {
		return nil, nil
	}

	texts := make([]string, 0, len(facts)+1)
	seen := make(map[string]struct{}, len(facts)+1)
	push := func(text string) {
		if text == "" {
			return
		}
		if _, ok := seen[text]; ok {
			return
		}
		seen[text] = struct{}{}
		texts = append(texts, text)
	}

	push(rawContent)
	for _, fact := range facts {
		push(normalizeFactContent(fact.Content))
	}
	if len(texts) == 0 {
		return nil, nil
	}

	embeddings, err := batchEmbedder.BatchEmbed(ctx, texts)
	if err != nil {
		return nil, err
	}
	if len(embeddings) != len(texts) {
		return nil, fmt.Errorf("batch embedding count mismatch: got %d embeddings for %d texts", len(embeddings), len(texts))
	}

	out := make(map[string][]float32, len(texts))
	for i := range texts {
		if len(embeddings[i]) == 0 {
			return nil, fmt.Errorf("batch embedding vector is empty at index %d", i)
		}
		out[texts[i]] = append([]float32{}, embeddings[i]...)
	}
	return out, nil
}

func normalizeFactContent(content string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(content)), " ")
}

func (s *Service) applyParsedFact(
	ctx context.Context,
	tenantID string,
	baseTags []string,
	baseSource string,
	fact ParsedFact,
	embeddingByContent map[string][]float32,
) (*domain.Memory, error) {
	content := normalizeFactContent(fact.Content)
	if !shouldStoreParsedFactContent(content) {
		return nil, nil
	}
	kind := resolveKind(fact.Kind)
	if kind != domain.MemoryKindEvent && kind != domain.MemoryKindObservation {
		kind = domain.MemoryKindObservation
	}

	existing, similarity, err := s.findSimilarMemory(ctx, tenantID, content, kind)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if similarity >= s.parser.UpdateThreshold && shouldReplaceMemory(*existing, content) {
			if err := s.deleteForReplacement(ctx, tenantID, existing.ID); err != nil {
				return nil, err
			}
		} else if similarity >= s.parser.DedupeThreshold {
			return existing, nil
		}
	}

	stored, err := s.storeOneWithOptionalEmbedding(ctx, domain.Memory{
		TenantID:  tenantID,
		Content:   content,
		Tier:      domain.MemoryTierSemantic,
		Kind:      kind,
		Tags:      mergeTags(baseTags, fact.Tags...),
		Source:    appendDerivedSource(baseSource, "parser"),
		CreatedBy: domain.MemoryCreatedBySystem,
	}, embeddingByContent)
	if err != nil {
		return nil, err
	}
	return &stored, nil
}

func (s *Service) findSimilarMemory(
	ctx context.Context,
	tenantID, content string,
	kind domain.MemoryKind,
) (*domain.Memory, float64, error) {
	candidates, err := s.repo.Search(ctx, tenantID, content, 8)
	if err != nil {
		return nil, 0, err
	}
	var best *domain.Memory
	bestScore := 0.0
	for i := range candidates {
		candidate := candidates[i]
		if candidate.Kind != kind {
			continue
		}
		score := lexicalSimilarity(content, candidate.Content)
		if score > bestScore {
			bestScore = score
			c := candidate
			best = &c
		}
	}
	return best, bestScore, nil
}

func lexicalSimilarity(a, b string) float64 {
	ta := normalizedRankingTokens(a)
	tb := normalizedRankingTokens(b)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	inter := 0
	for token := range ta {
		if _, ok := tb[token]; ok {
			inter++
		}
	}
	union := len(ta) + len(tb) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func shouldReplaceMemory(existing domain.Memory, nextContent string) bool {
	existingLen := len(strings.TrimSpace(existing.Content))
	nextLen := len(strings.TrimSpace(nextContent))
	if nextLen == 0 {
		return false
	}
	return nextLen >= existingLen+12
}

func (s *Service) deleteForReplacement(ctx context.Context, tenantID, memoryID string) error {
	if strings.TrimSpace(memoryID) == "" {
		return nil
	}
	err := s.repo.Delete(ctx, tenantID, memoryID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	if s.vector != nil {
		if err := s.vector.Delete(ctx, tenantID, memoryID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) writeLegacyStructuredDerived(ctx context.Context, stored domain.Memory) error {
	if !s.structured.Enabled || stored.Kind != domain.MemoryKindRawTurn {
		return nil
	}
	if s.structured.DualWriteObservations {
		derived, err := deriveObservations(stored.Content, s.structured.MaxObservations)
		if err != nil {
			return err
		}
		for _, obs := range derived {
			err := s.storeLegacyDerivedFact(
				ctx,
				stored.TenantID,
				obs,
				domain.MemoryKindObservation,
				mergeTags(stored.Tags, "observation", "derived"),
				appendDerivedSource(stored.Source, "observation"),
			)
			if err != nil {
				return err
			}
		}
	}
	if s.structured.DualWriteEvents {
		if eventText, ok := deriveEvent(stored.Content); ok {
			err := s.storeLegacyDerivedFact(
				ctx,
				stored.TenantID,
				eventText,
				domain.MemoryKindEvent,
				mergeTags(stored.Tags, "event", "derived"),
				appendDerivedSource(stored.Source, "event"),
			)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) storeLegacyDerivedFact(
	ctx context.Context,
	tenantID string,
	content string,
	kind domain.MemoryKind,
	tags []string,
	source string,
) error {
	content = strings.Join(strings.Fields(strings.TrimSpace(content)), " ")
	if content == "" {
		return nil
	}
	skip, err := s.shouldSkipLegacyDerived(ctx, tenantID, content, kind)
	if err != nil {
		return err
	}
	if skip {
		return nil
	}
	_, err = s.storeOne(ctx, domain.Memory{
		TenantID:  tenantID,
		Content:   content,
		Tier:      domain.MemoryTierSemantic,
		Kind:      kind,
		Tags:      tags,
		Source:    source,
		CreatedBy: domain.MemoryCreatedBySystem,
	})
	return err
}

func (s *Service) shouldSkipLegacyDerived(
	ctx context.Context,
	tenantID string,
	content string,
	kind domain.MemoryKind,
) (bool, error) {
	if s.embedder == nil || s.vector == nil {
		return false, nil
	}
	embedding, err := s.embedder.Embed(ctx, content)
	if err != nil {
		return false, err
	}
	candidates, err := s.vector.Search(ctx, tenantID, embedding, dualWriteCandidateTopK)
	if err != nil {
		return false, err
	}
	if len(candidates) == 0 {
		return false, nil
	}
	ids := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		memoryID := strings.TrimSpace(candidate.MemoryID)
		if memoryID == "" {
			continue
		}
		if _, ok := seen[memoryID]; ok {
			continue
		}
		seen[memoryID] = struct{}{}
		ids = append(ids, memoryID)
	}
	if len(ids) == 0 {
		return false, nil
	}
	memories, err := s.repo.GetByIDs(ctx, tenantID, ids)
	if err != nil {
		return false, err
	}
	memoryByID := make(map[string]domain.Memory, len(memories))
	for _, memory := range memories {
		memoryByID[memory.ID] = memory
	}
	for _, candidate := range candidates {
		if candidate.Similarity < DualWriteDedupeThreshold {
			continue
		}
		existing, ok := memoryByID[candidate.MemoryID]
		if !ok || existing.Kind != kind {
			continue
		}
		if hasNegationConflict(content, existing.Content) {
			continue
		}
		if lexicalSimilarity(content, existing.Content) < dualWriteLexicalSafetyThreshold {
			continue
		}
		return true, nil
	}
	return false, nil
}

func hasNegationConflict(a, b string) bool {
	ta := normalizedRankingTokens(a)
	tb := normalizedRankingTokens(b)
	if len(ta) == 0 || len(tb) == 0 {
		return false
	}
	aHasNegation := false
	bHasNegation := false
	sharedNonNegation := false
	for token := range ta {
		if _, neg := dualWriteNegationTokens[token]; neg {
			aHasNegation = true
			continue
		}
		if _, ok := tb[token]; ok {
			sharedNonNegation = true
		}
	}
	for token := range tb {
		if _, neg := dualWriteNegationTokens[token]; neg {
			bHasNegation = true
			break
		}
	}
	if aHasNegation == bHasNegation {
		return false
	}
	return sharedNonNegation
}

func (s *Service) storeOne(ctx context.Context, m domain.Memory) (domain.Memory, error) {
	embedding, err := s.embedder.Embed(ctx, m.Content)
	if err != nil {
		return domain.Memory{}, err
	}
	return s.storeOnePrecomputed(ctx, m, embedding)
}

func (s *Service) storeOneWithOptionalEmbedding(
	ctx context.Context,
	m domain.Memory,
	embeddingByContent map[string][]float32,
) (domain.Memory, error) {
	if len(embeddingByContent) > 0 {
		if embedding, ok := embeddingByContent[m.Content]; ok && len(embedding) > 0 {
			return s.storeOnePrecomputed(ctx, m, embedding)
		}
	}
	return s.storeOne(ctx, m)
}

func (s *Service) storeOnePrecomputed(ctx context.Context, m domain.Memory, embedding []float32) (domain.Memory, error) {
	if len(embedding) == 0 {
		return domain.Memory{}, fmt.Errorf("embedding must not be empty")
	}
	importance, err := s.scorer.Score(ctx, m.Content)
	if err != nil {
		return domain.Memory{}, err
	}
	m.Importance = importance

	storedBatch, err := s.storeInRepo(ctx, []domain.Memory{m})
	if err != nil {
		return domain.Memory{}, err
	}
	if len(storedBatch) != 1 {
		return domain.Memory{}, fmt.Errorf("store returned %d records for single memory", len(storedBatch))
	}
	stored := storedBatch[0]
	if err := s.vector.Upsert(ctx, stored.TenantID, stored.ID, embedding); err != nil {
		return domain.Memory{}, err
	}
	return stored, nil
}

func prepareParsedFactsForStore(sourceContent string, facts []ParsedFact) []ParsedFact {
	if len(facts) == 0 {
		return []ParsedFact{}
	}

	anchor, hasAnchor := sourceTimeAnchor(sourceContent)
	prepared := make([]ParsedFact, 0, len(facts))
	for _, fact := range facts {
		content := normalizeFactContent(canonicalizeTurnStyleFact(sourceContent, fact.Content))
		if !shouldStoreParsedFactContent(content) {
			continue
		}
		if hasAnchor && !timeTagPattern.MatchString(strings.ToLower(content)) {
			// Prepend as natural-language phrase so the date is both embeddable and
			// extractable by the FULL_DATE_RE pattern (e.g. "8 May 2023").
			content = "On " + anchor + ", " + content
		}
		fact.Content = content
		if !parsedFactHasEntityTriple(fact) {
			entity, relation, value := inferEntityRelationValue(content, fact.Kind)
			if strings.TrimSpace(fact.Entity) == "" {
				fact.Entity = entity
			}
			if strings.TrimSpace(fact.Relation) == "" {
				fact.Relation = relation
			}
			if strings.TrimSpace(fact.Value) == "" {
				fact.Value = value
			}
		}
		prepared = append(prepared, fact)
	}
	return prepared
}

func shouldStoreParsedFactContent(content string) bool {
	content = normalizeFactContent(content)
	if content == "" {
		return false
	}
	if len([]rune(content)) >= minParsedFactContentLength {
		return true
	}
	return isInformativeFact(content)
}

func (s *Service) storeInRepo(ctx context.Context, memories []domain.Memory) ([]domain.Memory, error) {
	if len(memories) == 0 {
		return []domain.Memory{}, nil
	}
	if batchRepo, ok := s.repo.(domain.MemoryBatchRepository); ok && batchRepo != nil {
		return batchRepo.StoreBatch(ctx, memories)
	}

	stored := make([]domain.Memory, 0, len(memories))
	for _, memory := range memories {
		m, err := s.repo.Store(ctx, memory)
		if err != nil {
			return nil, err
		}
		stored = append(stored, m)
	}
	return stored, nil
}

func (s *Service) embedContents(ctx context.Context, contents []string) ([][]float32, error) {
	if len(contents) == 0 {
		return [][]float32{}, nil
	}
	if batchEmbedder, ok := s.embedder.(domain.BatchEmbedder); ok && batchEmbedder != nil {
		embeddings, err := batchEmbedder.BatchEmbed(ctx, contents)
		if err == nil {
			if len(embeddings) != len(contents) {
				return nil, fmt.Errorf("batch embedding count mismatch: got %d for %d contents", len(embeddings), len(contents))
			}
			for i := range embeddings {
				if len(embeddings[i]) == 0 {
					return nil, fmt.Errorf("batch embedding vector is empty at index %d", i)
				}
			}
			return embeddings, nil
		}
	}

	embeddings := make([][]float32, 0, len(contents))
	for i := range contents {
		vec, err := s.embedder.Embed(ctx, contents[i])
		if err != nil {
			return nil, err
		}
		if len(vec) == 0 {
			return nil, fmt.Errorf("embedding vector is empty at index %d", i)
		}
		embeddings = append(embeddings, vec)
	}
	return embeddings, nil
}

func (s *Service) upsertStoredEmbeddings(ctx context.Context, stored []domain.Memory, embeddings [][]float32) error {
	if len(stored) != len(embeddings) {
		return fmt.Errorf("upsert embedding mismatch: stored=%d embeddings=%d", len(stored), len(embeddings))
	}
	if len(stored) == 0 {
		return nil
	}

	if batchVector, ok := s.vector.(domain.VectorBatchStore); ok && batchVector != nil {
		upserts := make([]domain.VectorUpsert, 0, len(stored))
		for i := range stored {
			upserts = append(upserts, domain.VectorUpsert{
				TenantID:  stored[i].TenantID,
				MemoryID:  stored[i].ID,
				Embedding: embeddings[i],
			})
		}
		return batchVector.UpsertBatch(ctx, upserts)
	}

	for i := range stored {
		if err := s.vector.Upsert(ctx, stored[i].TenantID, stored[i].ID, embeddings[i]); err != nil {
			return err
		}
	}
	return nil
}

func resolveKind(kind domain.MemoryKind) domain.MemoryKind {
	switch kind {
	case domain.MemoryKindObservation, domain.MemoryKindSummary, domain.MemoryKindEvent:
		return kind
	case "", domain.MemoryKindRawTurn:
		return domain.MemoryKindRawTurn
	default:
		return domain.MemoryKindRawTurn
	}
}

func appendDerivedSource(base, suffix string) string {
	base = strings.TrimSpace(base)
	suffix = strings.TrimSpace(suffix)
	if suffix == "" {
		return base
	}
	if base == "" {
		return suffix
	}
	return base + ":" + suffix
}

func mergeTags(base []string, extra ...string) []string {
	out := append([]string{}, base...)
	seen := make(map[string]struct{}, len(out))
	for _, tag := range out {
		tag = strings.TrimSpace(strings.ToLower(tag))
		if tag == "" {
			continue
		}
		seen[tag] = struct{}{}
	}
	for _, tag := range extra {
		tag = strings.TrimSpace(strings.ToLower(tag))
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	slices.Sort(out)
	return out
}
