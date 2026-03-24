package memory

import (
	"context"
	"slices"
	"strings"

	"github.com/pali-mem/pali/internal/domain"
)

func (s *Service) collectGraphEntityBridgeCandidates(
	ctx context.Context,
	tenantID, query string,
	plan queryPlan,
	lexical []lexicalCandidate,
	topK int,
	tierFilter map[domain.MemoryTier]struct{},
	kindFilter map[domain.MemoryKind]struct{},
	force bool,
) ([]lexicalCandidate, error) {
	if s.entityRepo == nil || (!force && plan.Intent != "graph_entity_expansion") {
		return []lexicalCandidate{}, nil
	}
	entities := graphEntityBridgeSeeds(query, plan, lexical, s.multiHop.GraphSeedLimit)
	if len(entities) == 0 {
		return []lexicalCandidate{}, nil
	}
	relations := expandEntityFactRelations(graphRelationHints(query, plan))
	lookupLimit := topK / 10
	if lookupLimit < 8 {
		lookupLimit = 8
	}
	if lookupLimit > 24 {
		lookupLimit = 24
	}
	ids := make([]string, 0, len(entities)*len(relations)*lookupLimit)
	seen := make(map[string]struct{}, len(entities)*len(relations)*lookupLimit)
	pathScoreByID := make(map[string]float64, lookupLimit)
	if s.multiHop.GraphPathEnabled {
		if pathRepo, ok := s.entityRepo.(domain.EntityFactPathRepository); ok && pathRepo != nil {
			pathLimit := s.multiHop.GraphPathLimit
			if pathLimit <= 0 {
				pathLimit = defaultMultiHopOptions().GraphPathLimit
			}
			pathCandidates, err := pathRepo.ListByEntityPaths(ctx, tenantID, domain.EntityFactPathQuery{
				SeedEntities:     entities,
				RelationHints:    relations,
				MaxHops:          s.multiHop.GraphMaxHops,
				Limit:            pathLimit,
				TemporalValidity: s.multiHop.GraphTemporalValidity,
			})
			if err != nil {
				return nil, err
			}
			for _, candidate := range pathCandidates {
				memoryID := strings.TrimSpace(candidate.MemoryID)
				if memoryID == "" {
					continue
				}
				score := clamp01(candidate.TraversalScore)
				if score < s.multiHop.GraphMinScore {
					continue
				}
				if existing, ok := pathScoreByID[memoryID]; !ok || score > existing {
					pathScoreByID[memoryID] = score
				}
				if _, ok := seen[memoryID]; ok {
					continue
				}
				seen[memoryID] = struct{}{}
				ids = append(ids, memoryID)
			}
		}
	}
	for _, entity := range entities {
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
	}
	if graphRepo, ok := s.entityRepo.(domain.EntityFactGraphRepository); ok && graphRepo != nil {
		neighborLimit := lookupLimit * len(relations) * 2
		if neighborLimit < 32 {
			neighborLimit = 32
		}
		if neighborLimit > 256 {
			neighborLimit = 256
		}
		neighborFacts, err := graphRepo.ListByEntityNeighborhood(ctx, tenantID, entities, neighborLimit)
		if err != nil {
			return nil, err
		}
		for _, fact := range neighborFacts {
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
		score := graphBridgeCandidateScore(query, memory.Content, entities, pathScoreByID[memory.ID], s.multiHop.GraphWeight)
		threshold := 0.16
		if pathScoreByID[memory.ID] > 0 {
			threshold = s.multiHop.GraphMinScore
		}
		if score < threshold {
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
	maxBridgeCandidates := topK
	if maxBridgeCandidates < 12 {
		maxBridgeCandidates = 12
	}
	if len(candidates) > maxBridgeCandidates {
		candidates = candidates[:maxBridgeCandidates]
	}
	return candidates, nil
}

func graphEntityBridgeSeeds(query string, plan queryPlan, lexical []lexicalCandidate, seedLimit int) []string {
	seeds := make([]string, 0, 6)
	seen := make(map[string]struct{}, 6)
	add := func(entity string) {
		entity = normalizeEntityFactEntity(entity)
		if entity == "" {
			return
		}
		if _, ok := seen[entity]; ok {
			return
		}
		seen[entity] = struct{}{}
		seeds = append(seeds, entity)
	}

	for _, entity := range plan.Entities {
		add(entity)
	}
	for _, match := range entityNamePattern.FindAllString(query, -1) {
		add(match)
	}

	ordered := append([]lexicalCandidate{}, lexical...)
	slices.SortFunc(ordered, func(a, b lexicalCandidate) int {
		switch {
		case a.Score > b.Score:
			return -1
		case a.Score < b.Score:
			return 1
		default:
			return strings.Compare(a.Memory.ID, b.Memory.ID)
		}
	})
	maxSeedScan := 8
	if len(ordered) < maxSeedScan {
		maxSeedScan = len(ordered)
	}
	for i := 0; i < maxSeedScan; i++ {
		add(inferEntityFromFact(ordered[i].Memory.Content))
	}
	if seedLimit <= 0 {
		seedLimit = 4
	}
	if len(seeds) > seedLimit {
		return seeds[:seedLimit]
	}
	return seeds
}

func graphRelationHints(query string, plan queryPlan) []string {
	hints := make([]string, 0, 8)
	seen := make(map[string]struct{}, 8)
	add := func(value string) {
		value = normalizeEntityFactRelation(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		hints = append(hints, value)
	}

	for _, relation := range plan.Relations {
		add(relation)
	}
	lowered := strings.ToLower(strings.TrimSpace(query))
	relationHints := []struct {
		relation string
		terms    []string
	}{
		{relation: "activity", terms: []string{"activity", "activities", "hobby", "hobbies", "interest", "interests", "participat", "partake"}},
		{relation: "preference", terms: []string{"favorite", "favorites", "prefer", "prefers", "likes", "enjoy", "love", "loves"}},
		{relation: "event", terms: []string{"event", "events", "attend", "attended", "join", "joined", "meet", "met", "happen", "happened"}},
		{relation: "plan", terms: []string{"plan", "planned", "planning", "goal", "goals"}},
		{relation: "goal", terms: []string{"dream", "dreams", "aspire", "aspiring", "aim", "aims"}},
		{relation: "identity", terms: []string{"name", "identity", "called"}},
		{relation: "role", terms: []string{"role", "job", "work", "works", "position"}},
		{relation: "relationship", terms: []string{"relationship", "friend", "friends", "family", "parent", "child", "children", "kids", "mentor", "partner"}},
		{relation: "relationship status", terms: []string{"status", "single", "married", "divorced", "engaged", "dating"}},
		{relation: "belief", terms: []string{"believe", "belief", "beliefs", "think", "thinks", "opinion", "stance"}},
		{relation: "value", terms: []string{"value", "values", "principle", "principles", "care about"}},
		{relation: "trait", terms: []string{"trait", "traits", "personality", "character", "tendency", "tendencies"}},
		{relation: "place", terms: []string{"place", "places", "live", "lives", "move", "moved", "travel", "trip", "location", "where"}},
		{relation: "book", terms: []string{"book", "books", "read", "reading", "novel"}},
	}
	for _, candidate := range relationHints {
		for _, term := range candidate.terms {
			if strings.Contains(lowered, term) {
				add(candidate.relation)
				break
			}
		}
	}
	if len(hints) == 0 {
		return []string{"activity", "preference", "event", "plan", "goal", "identity", "role", "relationship", "relationship status", "place", "book"}
	}
	return hints
}

func graphBridgeCandidateScore(query, content string, entities []string, pathScore, graphWeight float64) float64 {
	lexicalScore := lexicalContentScore(query, content)
	entityHit := 0.0
	lowered := strings.ToLower(content)
	for _, entity := range entities {
		if strings.Contains(lowered, entity) {
			entityHit = 1
			break
		}
	}
	baseScore := clamp01((0.72 * lexicalScore) + (0.28 * entityHit))
	pathScore = clamp01(pathScore)
	graphWeight = clamp01(graphWeight)
	if pathScore == 0 || graphWeight == 0 {
		return baseScore
	}
	return clamp01(((1 - graphWeight) * baseScore) + (graphWeight * pathScore))
}

func lexicalContentScore(query, content string) float64 {
	queryTokens := normalizedRankingTokens(query)
	contentTokens := normalizedRankingTokens(content)
	if len(queryTokens) == 0 || len(contentTokens) == 0 {
		return 0
	}
	matches := 0
	for token := range queryTokens {
		if _, ok := contentTokens[token]; ok {
			matches++
		}
	}
	if matches == 0 {
		return 0
	}
	recall := float64(matches) / float64(len(queryTokens))
	union := len(queryTokens) + len(contentTokens) - matches
	jaccard := 0.0
	if union > 0 {
		jaccard = float64(matches) / float64(union)
	}
	return clamp01((0.7 * recall) + (0.3 * jaccard))
}
