package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/vein05/pali/internal/domain"
)

// parserBatchRepoDedupeMinTenantRows gates parser dedupe FTS probes.
// For small tenant corpora, pending-batch dedupe is usually enough and avoids
// O(facts) repo.Search calls behind SQLite's single-writer/read connection cap.
const parserBatchRepoDedupeMinTenantRows int64 = 1000

type parserBatchItem struct {
	item     preparedStoreInput
	facts    []ParsedFact
	parseErr error
}

type parserPendingWrite struct {
	memory     domain.Memory
	embedding  []float32
	dropped    bool
	replacedBy int
}

type parserBatchResultRef struct {
	pendingIdx  int
	existing    domain.Memory
	hasExisting bool
}

type parserPendingEntityFact struct {
	pendingIdx  int
	existing    domain.Memory
	hasExisting bool
	fact        ParsedFact
}

func (s *Service) storeBatchWithParser(ctx context.Context, items []preparedStoreInput) ([]domain.Memory, error) {
	if len(items) == 0 {
		return []domain.Memory{}, nil
	}
	if len(items) == 1 {
		return s.storeBatchWithParserSequential(ctx, items)
	}

	batchEmbedder, ok := s.embedder.(domain.BatchEmbedder)
	if !ok || batchEmbedder == nil {
		return s.storeBatchWithParserSequential(ctx, items)
	}

	repoDedupeEnabledByTenant, err := s.parserBatchRepoDedupeFlags(ctx, items)
	if err != nil {
		return nil, err
	}
	repoNoHit := make(map[string]struct{}, len(items))

	parsed := make([]parserBatchItem, 0, len(items))
	uniqueTexts := make([]string, 0, len(items)*(s.parser.MaxFacts+1))
	seenTexts := make(map[string]struct{}, len(uniqueTexts))
	pushText := func(text string) {
		if text == "" {
			return
		}
		if _, ok := seenTexts[text]; ok {
			return
		}
		seenTexts[text] = struct{}{}
		uniqueTexts = append(uniqueTexts, text)
	}

	for _, item := range items {
		facts, parseErr := s.parseFactsWithFallback(ctx, item.input.Content)
		if parseErr == nil {
			// Apply same date-injection and content preparation as the non-batch path.
			// prepareParsedFactsForStore normalizes content, prepends "On <date>, " when
			// the source turn carries a time anchor, filters low-quality facts, and
			// infers entity/relation/value triples. Skipping this was causing facts to
			// be embedded and stored without temporal anchors even when M1 was "on".
			facts = prepareParsedFactsForStore(item.input.Content, facts)
		}
		parsed = append(parsed, parserBatchItem{
			item:     item,
			facts:    facts,
			parseErr: parseErr,
		})
		pushText(item.input.Content)
		if parseErr != nil {
			continue
		}
		for _, fact := range facts {
			// fact.Content is already normalized by prepareParsedFactsForStore above.
			pushText(fact.Content)
		}
	}

	embeddings, err := batchEmbedder.BatchEmbed(ctx, uniqueTexts)
	if err != nil {
		return s.storeBatchWithParserSequential(ctx, items)
	}
	if len(embeddings) != len(uniqueTexts) {
		return s.storeBatchWithParserSequential(ctx, items)
	}

	embeddingByContent := make(map[string][]float32, len(uniqueTexts))
	for i := range uniqueTexts {
		if len(embeddings[i]) == 0 {
			return s.storeBatchWithParserSequential(ctx, items)
		}
		embeddingByContent[uniqueTexts[i]] = append([]float32{}, embeddings[i]...)
	}

	pending := make([]parserPendingWrite, 0, len(uniqueTexts))
	pendingEntityFacts := make([]parserPendingEntityFact, 0, len(uniqueTexts))
	resultRefs := make([]parserBatchResultRef, len(items))
	for i := range resultRefs {
		resultRefs[i].pendingIdx = -1
	}

	stageMemory := func(memory domain.Memory) (int, error) {
		embedding, ok := embeddingByContent[memory.Content]
		if !ok || len(embedding) == 0 {
			return -1, fmt.Errorf("missing precomputed embedding for parser batch content")
		}
		pending = append(pending, parserPendingWrite{
			memory:     memory,
			embedding:  append([]float32{}, embedding...),
			replacedBy: -1,
		})
		return len(pending) - 1, nil
	}

	stageRawMemory := func(item preparedStoreInput) (int, error) {
		return stageMemory(domain.Memory{
			TenantID:  item.input.TenantID,
			Content:   item.input.Content,
			Tier:      item.resolvedTier,
			Kind:      item.resolvedKind,
			Tags:      item.input.Tags,
			Source:    item.input.Source,
			CreatedBy: item.input.CreatedBy,
		})
	}

	for i := range parsed {
		current := parsed[i]
		if current.parseErr != nil {
			rawIdx, err := stageRawMemory(current.item)
			if err != nil {
				return nil, err
			}
			resultRefs[i].pendingIdx = rawIdx
			continue
		}

		if s.parser.StoreRawTurn {
			rawIdx, err := stageRawMemory(current.item)
			if err != nil {
				return nil, err
			}
			resultRefs[i].pendingIdx = rawIdx
		}

		firstFound := false
		firstPendingIdx := -1
		var firstExisting domain.Memory
		allowRepoDedupe := repoDedupeEnabledByTenant[current.item.input.TenantID]
		for _, fact := range current.facts {
			memory, pendingIdx, err := s.applyParsedFactWithPending(
				ctx,
				current.item.input.TenantID,
				current.item.input.Tags,
				current.item.input.Source,
				fact,
				embeddingByContent,
				&pending,
				allowRepoDedupe,
				repoNoHit,
			)
			if err != nil {
				return nil, err
			}
			if memory == nil || firstFound {
				if memory != nil && parsedFactHasEntityTriple(fact) {
					ref := parserPendingEntityFact{
						pendingIdx: pendingIdx,
						fact:       fact,
					}
					if pendingIdx < 0 {
						ref.existing = *memory
						ref.hasExisting = true
					}
					pendingEntityFacts = append(pendingEntityFacts, ref)
				}
				continue
			}
			firstFound = true
			if pendingIdx >= 0 {
				firstPendingIdx = pendingIdx
			} else {
				firstExisting = *memory
			}
			if parsedFactHasEntityTriple(fact) {
				ref := parserPendingEntityFact{
					pendingIdx: pendingIdx,
					fact:       fact,
				}
				if pendingIdx < 0 {
					ref.existing = *memory
					ref.hasExisting = true
				}
				pendingEntityFacts = append(pendingEntityFacts, ref)
			}
		}

		if s.parser.StoreRawTurn {
			continue
		}
		if firstFound {
			if firstPendingIdx >= 0 {
				resultRefs[i].pendingIdx = firstPendingIdx
			} else {
				resultRefs[i].existing = firstExisting
				resultRefs[i].hasExisting = true
			}
			continue
		}

		rawIdx, err := stageRawMemory(current.item)
		if err != nil {
			return nil, err
		}
		resultRefs[i].pendingIdx = rawIdx
	}

	pendingToStored := make(map[int]domain.Memory, len(pending))
	memoriesToStore := make([]domain.Memory, 0, len(pending))
	embeddingsToStore := make([][]float32, 0, len(pending))
	pendingToStoreIdx := make(map[int]int, len(pending))
	for i := range pending {
		if pending[i].dropped {
			continue
		}
		importance, err := s.scorer.Score(ctx, pending[i].memory.Content)
		if err != nil {
			return nil, err
		}
		pending[i].memory.Importance = importance
		pendingToStoreIdx[i] = len(memoriesToStore)
		memoriesToStore = append(memoriesToStore, pending[i].memory)
		embeddingsToStore = append(embeddingsToStore, pending[i].embedding)
	}

	if len(memoriesToStore) > 0 {
		stored, err := s.storeInRepo(ctx, memoriesToStore)
		if err != nil {
			return nil, err
		}
		if len(stored) != len(embeddingsToStore) {
			return nil, fmt.Errorf("parser batch store result mismatch: stored=%d embeddings=%d", len(stored), len(embeddingsToStore))
		}
		if err := s.upsertStoredEmbeddings(ctx, stored, embeddingsToStore); err != nil {
			return nil, err
		}
		for pendingIdx, storedIdx := range pendingToStoreIdx {
			pendingToStored[pendingIdx] = stored[storedIdx]
		}
	}

	entityFacts := make([]domain.EntityFact, 0, len(pendingEntityFacts))
	for _, pendingFact := range pendingEntityFacts {
		var memory domain.Memory
		hasMemory := false
		if pendingFact.pendingIdx >= 0 {
			resolved := resolveParserPendingIndex(pending, pendingFact.pendingIdx)
			if resolved >= 0 {
				if stored, ok := pendingToStored[resolved]; ok {
					memory = stored
					hasMemory = true
				}
			}
		} else if pendingFact.hasExisting {
			memory = pendingFact.existing
			hasMemory = true
		}
		if !hasMemory {
			continue
		}
		if entityFact, ok := buildEntityFactRecord(memory, pendingFact.fact); ok {
			entityFacts = append(entityFacts, entityFact)
		}
	}
	if err := s.storeEntityFacts(ctx, entityFacts); err != nil {
		return nil, err
	}

	out := make([]domain.Memory, len(items))
	for i := range resultRefs {
		if resultRefs[i].pendingIdx >= 0 {
			resolved := resolveParserPendingIndex(pending, resultRefs[i].pendingIdx)
			if resolved >= 0 {
				if memory, ok := pendingToStored[resolved]; ok {
					out[i] = memory
					continue
				}
			}
		}
		if resultRefs[i].hasExisting {
			out[i] = resultRefs[i].existing
			continue
		}
		return nil, fmt.Errorf("parser batch result resolution failed at index %d", i)
	}

	return out, nil
}

func (s *Service) storeBatchWithParserSequential(ctx context.Context, items []preparedStoreInput) ([]domain.Memory, error) {
	out := make([]domain.Memory, 0, len(items))
	for _, item := range items {
		stored, err := s.storeWithParser(ctx, item.input, item.resolvedTier, item.resolvedKind)
		if err != nil {
			return nil, err
		}
		out = append(out, stored)
	}
	return out, nil
}

func (s *Service) parserBatchRepoDedupeFlags(
	ctx context.Context,
	items []preparedStoreInput,
) (map[string]bool, error) {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		tenantID := item.input.TenantID
		if _, ok := out[tenantID]; ok {
			continue
		}
		count, err := s.tenantRepo.MemoryCount(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		out[tenantID] = count >= parserBatchRepoDedupeMinTenantRows
	}
	return out, nil
}

func (s *Service) findSimilarMemoryWithPending(
	ctx context.Context,
	tenantID, content string,
	kind domain.MemoryKind,
	pending []parserPendingWrite,
	allowRepoSearch bool,
	repoNoHit map[string]struct{},
) (*domain.Memory, float64, int, error) {
	var pendingMatch *domain.Memory
	pendingScore := 0.0
	pendingIdx := -1

	for i := len(pending) - 1; i >= 0; i-- {
		if pending[i].dropped {
			continue
		}
		if pending[i].memory.TenantID != tenantID || pending[i].memory.Kind != kind {
			continue
		}
		score := lexicalSimilarity(content, pending[i].memory.Content)
		if score <= pendingScore {
			continue
		}
		pendingScore = score
		pendingIdx = i
		candidate := pending[i].memory
		pendingMatch = &candidate
	}

	// Pending hit already exceeds dedupe threshold; no need for extra FTS probe.
	if pendingMatch != nil && pendingScore >= s.parser.DedupeThreshold {
		return pendingMatch, pendingScore, pendingIdx, nil
	}

	if !allowRepoSearch {
		if pendingMatch != nil {
			return pendingMatch, pendingScore, pendingIdx, nil
		}
		return nil, 0, -1, nil
	}

	repoKey := parserBatchRepoNoHitKey(tenantID, content, kind)
	if _, skip := repoNoHit[repoKey]; skip {
		if pendingMatch != nil {
			return pendingMatch, pendingScore, pendingIdx, nil
		}
		return nil, 0, -1, nil
	}

	repoMatch, repoScore, err := s.findSimilarMemory(ctx, tenantID, content, kind)
	if err != nil {
		return nil, 0, -1, err
	}
	if repoMatch == nil {
		repoNoHit[repoKey] = struct{}{}
	}
	if repoScore >= pendingScore {
		return repoMatch, repoScore, -1, nil
	}
	return pendingMatch, pendingScore, pendingIdx, nil
}

func (s *Service) applyParsedFactWithPending(
	ctx context.Context,
	tenantID string,
	baseTags []string,
	baseSource string,
	fact ParsedFact,
	embeddingByContent map[string][]float32,
	pending *[]parserPendingWrite,
	allowRepoSearch bool,
	repoNoHit map[string]struct{},
) (*domain.Memory, int, error) {
	content := normalizeFactContent(fact.Content)
	if !shouldStoreParsedFactContent(content) {
		return nil, -1, nil
	}
	kind := resolveKind(fact.Kind)
	if kind != domain.MemoryKindEvent && kind != domain.MemoryKindObservation {
		kind = domain.MemoryKindObservation
	}

	existing, similarity, pendingIdx, err := s.findSimilarMemoryWithPending(
		ctx,
		tenantID,
		content,
		kind,
		*pending,
		allowRepoSearch,
		repoNoHit,
	)
	if err != nil {
		return nil, -1, err
	}

	replacedPendingIdx := -1
	if existing != nil {
		if similarity >= s.parser.UpdateThreshold && shouldReplaceMemory(*existing, content) {
			if pendingIdx >= 0 {
				(*pending)[pendingIdx].dropped = true
				replacedPendingIdx = pendingIdx
			} else {
				if err := s.deleteForReplacement(ctx, tenantID, existing.ID); err != nil {
					return nil, -1, err
				}
			}
		} else if similarity >= s.parser.DedupeThreshold {
			return existing, pendingIdx, nil
		}
	}

	embedding, ok := embeddingByContent[content]
	if !ok || len(embedding) == 0 {
		return nil, -1, fmt.Errorf("missing precomputed embedding for parsed fact content")
	}

	*pending = append(*pending, parserPendingWrite{
		memory: domain.Memory{
			TenantID:  tenantID,
			Content:   content,
			Tier:      domain.MemoryTierSemantic,
			Kind:      kind,
			Tags:      mergeTags(baseTags, fact.Tags...),
			Source:    appendDerivedSource(baseSource, "parser"),
			CreatedBy: domain.MemoryCreatedBySystem,
		},
		embedding:  append([]float32{}, embedding...),
		replacedBy: -1,
	})
	newPendingIdx := len(*pending) - 1
	if replacedPendingIdx >= 0 {
		(*pending)[replacedPendingIdx].replacedBy = newPendingIdx
	}
	staged := (*pending)[newPendingIdx].memory
	return &staged, newPendingIdx, nil
}

func resolveParserPendingIndex(pending []parserPendingWrite, idx int) int {
	for idx >= 0 && idx < len(pending) {
		if !pending[idx].dropped {
			return idx
		}
		if pending[idx].replacedBy < 0 {
			return -1
		}
		idx = pending[idx].replacedBy
	}
	return -1
}

func parserBatchRepoNoHitKey(tenantID, content string, kind domain.MemoryKind) string {
	return strings.TrimSpace(tenantID) + "|" + string(kind) + "|" + content
}
