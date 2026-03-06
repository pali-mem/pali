package memory

import (
	"context"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/vein05/pali/internal/core/scoring"
	"github.com/vein05/pali/internal/domain"
)

const reciprocalRankFusionK = 60

var rankingTokenPattern = regexp.MustCompile(`[a-zA-Z0-9_]+`)

type SearchOptions struct {
	MinScore     float64
	Tiers        []domain.MemoryTier
	Kinds        []domain.MemoryKind
	DisableTouch bool
}

func (s *Service) Search(ctx context.Context, tenantID, query string, topK int) ([]domain.Memory, error) {
	return s.SearchWithFilters(ctx, tenantID, query, topK, SearchOptions{})
}

func (s *Service) SearchWithFilters(ctx context.Context, tenantID, query string, topK int, opts SearchOptions) ([]domain.Memory, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(query) == "" {
		return nil, domain.ErrInvalidInput
	}
	if opts.MinScore < 0 || opts.MinScore > 1 {
		return nil, domain.ErrInvalidInput
	}
	tierFilter, err := buildTierFilter(opts.Tiers)
	if err != nil {
		return nil, err
	}
	kindFilter, err := buildKindFilter(opts.Kinds)
	if err != nil {
		return nil, err
	}
	if err := s.ensureTenantExists(ctx, tenantID); err != nil {
		return nil, err
	}
	if topK <= 0 {
		topK = 10
	}
	candidateTopK := candidateWindow(topK)

	if s.structured.QueryRoutingEnabled && s.entityRepo != nil {
		aggregated, handled, err := s.searchByEntityFacts(ctx, tenantID, query, topK, opts, tierFilter, kindFilter)
		if err != nil {
			return nil, err
		}
		if handled {
			return aggregated, nil
		}
	}

	lexicalCandidates, err := s.repo.Search(ctx, tenantID, query, candidateTopK)
	if err != nil {
		return nil, err
	}

	var denseCandidates []domain.VectorstoreCandidate
	if s.vector != nil && s.embedder != nil {
		queryEmbedding, err := s.embedder.Embed(ctx, query)
		if err != nil {
			return nil, err
		}
		denseCandidates, err = s.vector.Search(ctx, tenantID, queryEmbedding, candidateTopK)
		if err != nil {
			return nil, err
		}
	}

	if len(lexicalCandidates) == 0 && len(denseCandidates) == 0 {
		return []domain.Memory{}, nil
	}

	ids, similarityByID := fuseCandidatesByRRF(denseCandidates, lexicalCandidates, candidateTopK)
	if len(ids) == 0 {
		return []domain.Memory{}, nil
	}

	memories, err := s.repo.GetByIDs(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	if len(memories) == 0 {
		return []domain.Memory{}, nil
	}

	profile := queryProfile{}
	if s.structured.QueryRoutingEnabled {
		profile = classifyQuery(query)
	}
	scored := rankMemories(memories, similarityByID, query, profile, s.ranking)
	slices.SortFunc(scored, func(a, b scoredMemory) int {
		switch {
		case a.Score > b.Score:
			return -1
		case a.Score < b.Score:
			return 1
		default:
			return 0
		}
	})

	filtered := make([]scoredMemory, 0, len(scored))
	for _, item := range scored {
		if len(kindFilter) > 0 {
			if _, ok := kindFilter[item.Memory.Kind]; !ok {
				continue
			}
		}
		if len(tierFilter) > 0 {
			if _, ok := tierFilter[item.Memory.Tier]; !ok {
				continue
			}
		}
		if item.Score < opts.MinScore {
			continue
		}
		filtered = append(filtered, item)
	}

	if len(filtered) > topK {
		filtered = filtered[:topK]
	}

	out := make([]domain.Memory, 0, len(filtered))
	orderedIDs := make([]string, 0, len(filtered))
	for _, item := range filtered {
		out = append(out, item.Memory)
		orderedIDs = append(orderedIDs, item.Memory.ID)
	}
	if !opts.DisableTouch && len(orderedIDs) > 0 {
		_ = s.repo.Touch(ctx, tenantID, orderedIDs)
	}

	return out, nil
}

func (s *Service) searchByEntityFacts(
	ctx context.Context,
	tenantID, query string,
	topK int,
	opts SearchOptions,
	tierFilter map[domain.MemoryTier]struct{},
	kindFilter map[domain.MemoryKind]struct{},
) ([]domain.Memory, bool, error) {
	route, ok := classifyAggregationQuery(query)
	if !ok {
		return nil, false, nil
	}

	lookupLimit := topK * 6
	if lookupLimit < 20 {
		lookupLimit = 20
	}
	facts, err := s.entityRepo.ListByEntityRelation(ctx, tenantID, route.Entity, route.Relation, lookupLimit)
	if err != nil {
		return nil, false, err
	}
	if len(facts) == 0 {
		return nil, false, nil
	}

	ids := make([]string, 0, len(facts))
	seen := make(map[string]struct{}, len(facts))
	for _, fact := range facts {
		id := strings.TrimSpace(fact.MemoryID)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, false, nil
	}

	memories, err := s.repo.GetByIDs(ctx, tenantID, ids)
	if err != nil {
		return nil, false, err
	}
	if len(memories) == 0 {
		return nil, false, nil
	}

	filtered := make([]domain.Memory, 0, len(memories))
	for _, memory := range memories {
		if len(kindFilter) > 0 {
			if _, ok := kindFilter[memory.Kind]; !ok {
				continue
			}
		}
		if len(tierFilter) > 0 {
			if _, ok := tierFilter[memory.Tier]; !ok {
				continue
			}
		}
		if opts.MinScore > 1 {
			continue
		}
		filtered = append(filtered, memory)
	}
	if len(filtered) == 0 {
		return nil, false, nil
	}

	if len(filtered) > topK {
		filtered = filtered[:topK]
	}
	if !opts.DisableTouch {
		touchIDs := make([]string, 0, len(filtered))
		for _, memory := range filtered {
			touchIDs = append(touchIDs, memory.ID)
		}
		if len(touchIDs) > 0 {
			_ = s.repo.Touch(ctx, tenantID, touchIDs)
		}
	}
	return filtered, true, nil
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

type scoredMemory struct {
	Memory domain.Memory
	Score  float64
}

func rankMemories(
	memories []domain.Memory,
	similarityByID map[string]float64,
	query string,
	profile queryProfile,
	ranking RankingOptions,
) []scoredMemory {
	now := time.Now().UTC()
	recencyRaw := make([]float64, len(memories))
	relevanceRaw := make([]float64, len(memories))
	importanceRaw := make([]float64, len(memories))

	minRec, maxRec := 1.0, 0.0
	minRel, maxRel := 1.0, 0.0
	minImp, maxImp := 1.0, 0.0

	for i, m := range memories {
		lastAccess := m.LastAccessedAt
		if lastAccess.IsZero() {
			lastAccess = m.UpdatedAt
		}
		if lastAccess.IsZero() {
			lastAccess = m.CreatedAt
		}
		hours := now.Sub(lastAccess).Hours()
		if hours < 0 {
			hours = 0
		}

		rec := scoring.Recency(0.995, hours)
		rel := scoring.Relevance(similarityByID[m.ID])
		imp := m.Importance
		if imp < 0 {
			imp = 0
		}
		if imp > 1 {
			imp = 1
		}

		recencyRaw[i] = rec
		relevanceRaw[i] = rel
		importanceRaw[i] = imp

		if rec < minRec {
			minRec = rec
		}
		if rec > maxRec {
			maxRec = rec
		}
		if rel < minRel {
			minRel = rel
		}
		if rel > maxRel {
			maxRel = rel
		}
		if imp < minImp {
			minImp = imp
		}
		if imp > maxImp {
			maxImp = imp
		}
	}

	scored := make([]scoredMemory, 0, len(memories))
	queryTokens := normalizedRankingTokens(query)
	for i, m := range memories {
		rec := scoring.MinMax(recencyRaw[i], minRec, maxRec)
		rel := scoring.MinMax(relevanceRaw[i], minRel, maxRel)
		imp := scoring.MinMax(importanceRaw[i], minImp, maxImp)

		total := 0.0
		switch ranking.Algorithm {
		case "match":
			route := normalizedRouteBoost(routeBoost(m, profile))
			overlap := queryOverlapScore(queryTokens, normalizedRankingTokens(m.Content))
			total = weightedAverage(
				[]float64{rec, rel, imp, overlap, route},
				[]float64{
					ranking.Match.Recency,
					ranking.Match.Relevance,
					ranking.Match.Importance,
					ranking.Match.QueryOverlap,
					ranking.Match.Routing,
				},
			)
		default:
			total = weightedAverage(
				[]float64{rec, rel, imp},
				[]float64{
					ranking.WAL.Recency,
					ranking.WAL.Relevance,
					ranking.WAL.Importance,
				},
			)
			if profile.Temporal || profile.Person || profile.MultiHop {
				if total == 0 {
					total = 1
				}
				total = clamp01(total * routeBoost(m, profile))
			}
		}
		scored = append(scored, scoredMemory{Memory: m, Score: total})
	}
	return scored
}

func weightedAverage(values []float64, weights []float64) float64 {
	if len(values) == 0 || len(values) != len(weights) {
		return 0
	}
	totalWeight := 0.0
	total := 0.0
	for i := range values {
		w := weights[i]
		if w <= 0 {
			continue
		}
		totalWeight += w
		total += w * clamp01(values[i])
	}
	if totalWeight == 0 {
		return 0
	}
	return clamp01(total / totalWeight)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func normalizedRouteBoost(v float64) float64 {
	const (
		minBoost = 0.8
		maxBoost = 1.35
	)
	return clamp01((v - minBoost) / (maxBoost - minBoost))
}

func normalizedRankingTokens(text string) map[string]struct{} {
	tokens := rankingTokenPattern.FindAllString(strings.ToLower(strings.TrimSpace(text)), -1)
	out := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		if len(token) < 2 {
			continue
		}
		out[token] = struct{}{}
	}
	return out
}

func queryOverlapScore(queryTokens, docTokens map[string]struct{}) float64 {
	if len(queryTokens) == 0 || len(docTokens) == 0 {
		return 0
	}
	matches := 0
	for token := range queryTokens {
		if _, ok := docTokens[token]; ok {
			matches++
		}
	}
	return float64(matches) / float64(len(queryTokens))
}

func candidateWindow(topK int) int {
	n := topK * 5
	if n < 50 {
		n = 50
	}
	if n > 200 {
		n = 200
	}
	return n
}

func fuseCandidatesByRRF(
	dense []domain.VectorstoreCandidate,
	lexical []domain.Memory,
	limit int,
) ([]string, map[string]float64) {
	if limit <= 0 {
		limit = 10
	}

	rrfScore := make(map[string]float64, len(dense)+len(lexical))
	similarityByID := make(map[string]float64, len(dense)+len(lexical))

	for idx, candidate := range dense {
		if strings.TrimSpace(candidate.MemoryID) == "" {
			continue
		}
		rank := idx + 1
		rrfScore[candidate.MemoryID] += 1.0 / float64(reciprocalRankFusionK+rank)
		if candidate.Similarity > similarityByID[candidate.MemoryID] {
			similarityByID[candidate.MemoryID] = candidate.Similarity
		}
	}

	for idx, candidate := range lexical {
		if strings.TrimSpace(candidate.ID) == "" {
			continue
		}
		rank := idx + 1
		lexicalSignal := 1.0 / float64(reciprocalRankFusionK+rank)
		rrfScore[candidate.ID] += lexicalSignal
		if lexicalSignal > similarityByID[candidate.ID] {
			similarityByID[candidate.ID] = lexicalSignal
		}
	}

	ids := make([]string, 0, len(rrfScore))
	for id := range rrfScore {
		ids = append(ids, id)
	}

	sort.Slice(ids, func(i, j int) bool {
		a := ids[i]
		b := ids[j]
		if rrfScore[a] == rrfScore[b] {
			return a < b
		}
		return rrfScore[a] > rrfScore[b]
	})

	if len(ids) > limit {
		ids = ids[:limit]
	}

	filteredSimilarity := make(map[string]float64, len(ids))
	for _, id := range ids {
		filteredSimilarity[id] = similarityByID[id]
	}

	return ids, filteredSimilarity
}
