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
	facts, err := s.listEntityFactsByRelations(ctx, tenantID, []string{entity}, relations, lookupLimit)
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
		if !passesFilters(memory, kindFilter, tierFilter) {
			continue
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
	scored := rankMemories(filtered, signalByID, max(topK, len(filtered)), query, profile, plan, s.ranking, s.retrieval)
	// Unlike sortScoredByScore this has no ID tie-break; adding one would
	// reorder tied aggregation results.
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
	filters := domain.MemorySearchFilters{Tiers: opts.Tiers, Kinds: opts.Kinds}
	scoredRepo, hasScoredRepo := s.repo.(domain.MemoryScoredSearchRepository)
	filteredRepo, hasFilteredRepo := s.repo.(domain.MemoryFilteredSearchRepository)
	for _, searchQuery := range searchQueries {
		var (
			hits []domain.LexicalHit
			err  error
		)
		switch {
		case hasScoredRepo:
			hits, err = scoredRepo.SearchScored(ctx, tenantID, searchQuery, topK, filters)
		case hasFilteredRepo:
			var memories []domain.Memory
			memories, err = filteredRepo.SearchWithFilters(ctx, tenantID, searchQuery, topK, filters)
			hits = lexicalHitsWithoutScores(memories)
		default:
			var memories []domain.Memory
			memories, err = s.repo.Search(ctx, tenantID, searchQuery, topK)
			hits = lexicalHitsWithoutScores(memories)
		}
		if err != nil {
			return nil, err
		}
		for idx, hit := range hits {
			memory := hit.Memory
			if !passesFilters(memory, kindFilter, tierFilter) {
				continue
			}
			// Prefer the backend's real BM25 strength (normalized so the best hit
			// for this query is 1.0). Fall back to rank position only when the
			// backend cannot expose a score; rank position alone gave every
			// query variant's top hit a near-1.0 score regardless of match quality.
			lexicalStrength := hit.Relevance
			if lexicalStrength <= 0 {
				lexicalStrength = rankToNormalized(idx+1, topK)
			}
			bowScore := lexicalContentScore(searchQuery, memory.Content)
			candidates = append(candidates, lexicalCandidate{
				Memory: memory,
				Score:  clamp01((0.8 * lexicalStrength) + (0.2 * bowScore)),
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
		if !passesFilters(memory, kindFilter, tierFilter) {
			continue
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
	facts, err := s.listEntityFactsByRelations(ctx, tenantID, []string{entity}, relations, entityLookupLimit(topK))
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(facts))
	seen := make(map[string]struct{}, len(facts))
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
	if len(ids) == 0 {
		return []lexicalCandidate{}, nil
	}
	memories, err := s.repo.GetByIDs(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	candidates := make([]lexicalCandidate, 0, len(memories))
	for _, memory := range memories {
		if !passesFilters(memory, kindFilter, tierFilter) {
			continue
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
	sortLexicalByScore(candidates)
	maxHintCandidates := topK / 2
	if maxHintCandidates < 8 {
		maxHintCandidates = 8
	}
	if len(candidates) > maxHintCandidates {
		candidates = candidates[:maxHintCandidates]
	}
	return candidates, nil
}

// listEntityFactsByRelations returns the facts for every entity (outer) and
// relation (inner) pair in that order, using the repository's batch lookup
// when available and one ListByEntityRelation call per pair otherwise. With
// GraphSingletonInvalidation, invalidated facts are dropped.
func (s *Service) listEntityFactsByRelations(
	ctx context.Context,
	tenantID string,
	entities, relations []string,
	limit int,
) ([]domain.EntityFact, error) {
	if len(entities) == 0 || len(relations) == 0 {
		return []domain.EntityFact{}, nil
	}
	var facts []domain.EntityFact
	if batchRepo, ok := s.entityRepo.(domain.EntityFactBatchLookupRepository); ok && batchRepo != nil {
		batch, err := batchRepo.ListByEntityRelations(ctx, tenantID, entities, relations, limit)
		if err != nil {
			return nil, err
		}
		facts = batch
	} else {
		facts = make([]domain.EntityFact, 0, len(entities)*len(relations))
		for _, entity := range entities {
			for _, relation := range relations {
				pairFacts, err := s.entityRepo.ListByEntityRelation(ctx, tenantID, entity, relation, limit)
				if err != nil {
					return nil, err
				}
				facts = append(facts, pairFacts...)
			}
		}
	}
	if s.multiHop.GraphSingletonInvalidation {
		facts = filterActiveEntityFacts(facts)
	}
	return facts, nil
}

func lexicalHitsWithoutScores(memories []domain.Memory) []domain.LexicalHit {
	hits := make([]domain.LexicalHit, 0, len(memories))
	for _, m := range memories {
		hits = append(hits, domain.LexicalHit{Memory: m})
	}
	return hits
}
