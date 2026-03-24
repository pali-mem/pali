package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/pali-mem/pali/internal/domain"
)

func (s *Service) shouldApplyImplicitCanonicalKinds(opts SearchOptions) bool {
	if s == nil || !s.preferCanonicalEntityKinds || s.entityRepo == nil {
		return false
	}
	return len(opts.Kinds) == 0
}

func (s *Service) embedSearchQueries(ctx context.Context, queries []string) ([][]float32, error) {
	if len(queries) == 0 {
		return [][]float32{}, nil
	}
	out := make([][]float32, len(queries))
	pendingQueries := make([]string, 0, len(queries))
	pendingIdx := make([]int, 0, len(queries))
	for i, query := range queries {
		if embedding, ok := s.getCachedQueryEmbedding(query); ok {
			out[i] = embedding
			continue
		}
		pendingQueries = append(pendingQueries, query)
		pendingIdx = append(pendingIdx, i)
	}
	if len(pendingQueries) == 0 {
		return out, nil
	}
	embeddings, err := s.embedContents(ctx, pendingQueries)
	if err != nil {
		return nil, err
	}
	if len(embeddings) != len(pendingQueries) {
		return nil, fmt.Errorf("search query embedding count mismatch: got %d for %d queries", len(embeddings), len(pendingQueries))
	}
	for i := range embeddings {
		out[pendingIdx[i]] = cloneEmbedding(embeddings[i])
		s.setCachedQueryEmbedding(pendingQueries[i], embeddings[i])
	}
	return out, nil
}

func buildTierFilter(tiers []domain.MemoryTier) (map[domain.MemoryTier]struct{}, error) {
	if len(tiers) == 0 {
		return nil, nil
	}
	allowed := map[domain.MemoryTier]struct{}{
		domain.MemoryTierWorking:  {},
		domain.MemoryTierEpisodic: {},
		domain.MemoryTierSemantic: {},
	}
	out := make(map[domain.MemoryTier]struct{}, len(tiers))
	for _, tier := range tiers {
		if _, ok := allowed[tier]; !ok {
			return nil, domain.ErrInvalidInput
		}
		out[tier] = struct{}{}
	}
	return out, nil
}

func buildKindFilter(kinds []domain.MemoryKind) (map[domain.MemoryKind]struct{}, error) {
	if len(kinds) == 0 {
		return nil, nil
	}
	allowed := map[domain.MemoryKind]struct{}{
		domain.MemoryKindRawTurn:     {},
		domain.MemoryKindObservation: {},
		domain.MemoryKindSummary:     {},
		domain.MemoryKindEvent:       {},
	}
	out := make(map[domain.MemoryKind]struct{}, len(kinds))
	for _, kind := range kinds {
		if _, ok := allowed[kind]; !ok {
			return nil, domain.ErrInvalidInput
		}
		out[kind] = struct{}{}
	}
	return out, nil
}

func (s *Service) buildLLMMultiHopQueries(ctx context.Context, query string) ([]string, error) {
	if !s.multiHop.LLMDecompositionEnabled || s.queryDecomposer == nil {
		return []string{}, nil
	}
	maxQueries := s.multiHop.MaxDecompositionQueries
	if maxQueries <= 0 {
		maxQueries = defaultMultiHopOptions().MaxDecompositionQueries
	}
	queries, err := s.queryDecomposer.Decompose(ctx, query, maxQueries)
	if err != nil {
		return nil, err
	}
	base := condenseSearchQuery(query)
	out := make([]string, 0, len(queries))
	for _, candidate := range queries {
		candidate = condenseSearchQuery(candidate)
		if candidate == "" {
			continue
		}
		if strings.EqualFold(candidate, base) {
			continue
		}
		out = append(out, candidate)
	}
	return out, nil
}
