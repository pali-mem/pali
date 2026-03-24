package memory

import (
	"context"
	"slices"
	"strings"

	"github.com/pali-mem/pali/internal/domain"
)

func (s *Service) searchByEntityFacts(
	ctx context.Context,
	tenantID, query string,
	topK int,
	opts SearchOptions,
	tierFilter map[domain.MemoryTier]struct{},
	kindFilter map[domain.MemoryKind]struct{},
	plan queryPlan,
) ([]domain.Memory, bool, error) {
	entity := plan.primaryEntity()
	relation := plan.primaryRelation()
	if plan.Intent != "aggregation_lookup" || entity == "" || relation == "" {
		s.logDebugf("[pali-search] aggregation_detected=false query=%q", sanitizeLogSnippet(query, 120))
		return nil, false, nil
	}
	s.logDebugf("[pali-search] aggregation_detected=true entity=%q relation=%q", entity, relation)
	relations := expandEntityFactRelations([]string{relation})
	if len(relations) == 0 {
		relations = []string{relation}
	}

	lookupLimit := topK * 6
	if lookupLimit < 20 {
		lookupLimit = 20
	}
	facts := make([]domain.EntityFact, 0, lookupLimit*len(relations))
	for _, relation := range relations {
		relationFacts, err := s.entityRepo.ListByEntityRelation(ctx, tenantID, entity, relation, lookupLimit)
		if err != nil {
			return nil, false, err
		}
		if s.multiHop.GraphSingletonInvalidation {
			relationFacts = filterActiveEntityFacts(relationFacts)
		}
		facts = append(facts, relationFacts...)
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
		filtered = append(filtered, memory)
	}
	if len(filtered) == 0 {
		return nil, false, nil
	}

	signalByID := make(map[string]candidateSignal, len(filtered))
	for _, memory := range filtered {
		signalByID[memory.ID] = candidateSignal{
			LexicalScore: lexicalContentScore(query, memory.Content),
			RRFScore:     1,
		}
	}
	profile := classifyQuery(query)
	scored := rankMemories(filtered, signalByID, maxInt(topK, len(filtered)), query, profile, plan, s.ranking, s.retrieval)
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
	if shouldApplyPairwiseRerank(profile, s.multiHop) {
		scored = s.rerankScoredCandidates(scored, query, topK)
	}
	ranked := make([]domain.Memory, 0, len(scored))
	for _, item := range scored {
		if item.Score < opts.MinScore {
			continue
		}
		ranked = append(ranked, item.Memory)
	}
	if len(ranked) == 0 {
		return nil, false, nil
	}
	if len(kindFilter) == 0 {
		promoted := make([]domain.Memory, 0, len(ranked))
		raw := make([]domain.Memory, 0, len(ranked))
		for _, memory := range ranked {
			if memory.Kind == domain.MemoryKindRawTurn {
				raw = append(raw, memory)
				continue
			}
			promoted = append(promoted, memory)
		}
		ranked = append(promoted, raw...)
	}

	if len(kindFilter) == 0 {
		expanded, err := s.expandGroundedContextMemories(ctx, tenantID, ranked, topK)
		if err != nil {
			return nil, false, err
		}
		ranked = expanded
	}
	if len(ranked) > topK {
		ranked = ranked[:topK]
	}
	if !opts.DisableTouch {
		touchIDs := make([]string, 0, len(ranked))
		for _, memory := range ranked {
			touchIDs = append(touchIDs, memory.ID)
		}
		if len(touchIDs) > 0 {
			_ = s.repo.Touch(ctx, tenantID, touchIDs)
		}
	}
	return ranked, true, nil
}

func (s *Service) collectLexicalCandidates(
	ctx context.Context,
	tenantID string,
	searchQueries []string,
	topK int,
	opts SearchOptions,
	tierFilter map[domain.MemoryTier]struct{},
	kindFilter map[domain.MemoryKind]struct{},
) ([]lexicalCandidate, error) {
	candidates := make([]lexicalCandidate, 0, len(searchQueries)*topK)
	filteredRepo, hasFilteredRepo := s.repo.(domain.MemoryFilteredSearchRepository)
	for _, searchQuery := range searchQueries {
		var (
			memories []domain.Memory
			err      error
		)
		if hasFilteredRepo {
			memories, err = filteredRepo.SearchWithFilters(ctx, tenantID, searchQuery, topK, domain.MemorySearchFilters{
				Tiers: opts.Tiers,
				Kinds: opts.Kinds,
			})
		} else {
			memories, err = s.repo.Search(ctx, tenantID, searchQuery, topK)
		}
		if err != nil {
			return nil, err
		}
		for idx, memory := range memories {
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
			// Repository lexical results are already BM25-ranked (SQLite FTS5),
			// so use rank-derived score as the primary lexical confidence signal.
			bm25RankScore := rankToNormalized(idx+1, topK)
			bowScore := lexicalContentScore(searchQuery, memory.Content)
			candidates = append(candidates, lexicalCandidate{
				Memory: memory,
				Score:  clamp01((0.8 * bm25RankScore) + (0.2 * bowScore)),
			})
		}
	}
	return candidates, nil
}

func (s *Service) filterDenseCandidatesByMetadata(
	ctx context.Context,
	tenantID string,
	candidates []domain.VectorstoreCandidate,
	tierFilter map[domain.MemoryTier]struct{},
	kindFilter map[domain.MemoryKind]struct{},
) ([]domain.VectorstoreCandidate, error) {
	if len(candidates) == 0 {
		return []domain.VectorstoreCandidate{}, nil
	}
	ids := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		id := strings.TrimSpace(candidate.MemoryID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return []domain.VectorstoreCandidate{}, nil
	}
	memories, err := s.repo.GetByIDs(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]struct{}, len(memories))
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
		allowed[memory.ID] = struct{}{}
	}
	filtered := make([]domain.VectorstoreCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if _, ok := allowed[candidate.MemoryID]; !ok {
			continue
		}
		filtered = append(filtered, candidate)
	}
	return filtered, nil
}

func (s *Service) collectEntityHintCandidates(
	ctx context.Context,
	tenantID, query, entity string,
	topK int,
	tierFilter map[domain.MemoryTier]struct{},
	kindFilter map[domain.MemoryKind]struct{},
) ([]lexicalCandidate, error) {
	entity = normalizeEntityFactEntity(entity)
	if entity == "" || s.entityRepo == nil {
		return []lexicalCandidate{}, nil
	}
	relations := []string{"activity", "preference", "event", "plan", "goal", "identity", "role", "relationship", "relationship status", "belief", "value", "trait", "place", "book"}
	lookupLimit := topK / 10
	if lookupLimit < 8 {
		lookupLimit = 8
	}
	if lookupLimit > 24 {
		lookupLimit = 24
	}
	ids := make([]string, 0, lookupLimit*len(relations))
	seen := make(map[string]struct{}, lookupLimit*len(relations))
	for _, relation := range relations {
		facts, err := s.entityRepo.ListByEntityRelation(ctx, tenantID, entity, relation, lookupLimit)
		if err != nil {
			return nil, err
		}
		if s.multiHop.GraphSingletonInvalidation {
			facts = filterActiveEntityFacts(facts)
		}
		for _, fact := range facts {
			memoryID := strings.TrimSpace(fact.MemoryID)
			if memoryID == "" {
				continue
			}
			if _, ok := seen[memoryID]; ok {
				continue
			}
			seen[memoryID] = struct{}{}
			ids = append(ids, memoryID)
		}
	}
	if len(ids) == 0 {
		return []lexicalCandidate{}, nil
	}
	memories, err := s.repo.GetByIDs(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	candidates := make([]lexicalCandidate, 0, len(memories))
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
		score := lexicalContentScore(query, memory.Content)
		if score < 0.12 {
			continue
		}
		candidates = append(candidates, lexicalCandidate{
			Memory: memory,
			Score:  score,
		})
	}
	slices.SortFunc(candidates, func(a, b lexicalCandidate) int {
		switch {
		case a.Score > b.Score:
			return -1
		case a.Score < b.Score:
			return 1
		default:
			return strings.Compare(a.Memory.ID, b.Memory.ID)
		}
	})
	maxHintCandidates := topK / 2
	if maxHintCandidates < 8 {
		maxHintCandidates = 8
	}
	if len(candidates) > maxHintCandidates {
		candidates = candidates[:maxHintCandidates]
	}
	return candidates, nil
}
