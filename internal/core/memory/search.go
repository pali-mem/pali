package memory

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/pali-mem/pali/internal/core/scoring"
	"github.com/pali-mem/pali/internal/domain"
)

const reciprocalRankFusionK = 60

var rankingTokenPattern = regexp.MustCompile(`[a-zA-Z0-9_]+`)
var conversationalNoisePattern = regexp.MustCompile(`(?i)\b(?:said that|totally agree|absolutely|thanks|thank you|yeah|yep|wow|great|sounds good)\b`)
var factoidQueryPattern = regexp.MustCompile(`(?i)^\s*(?:what|who|which|whose|where|when)\b`)
var rankingStopwordPattern = map[string]struct{}{
	"a": {}, "an": {}, "the": {}, "what": {}, "when": {}, "where": {}, "who": {}, "why": {}, "how": {}, "which": {}, "whose": {},
	"did": {}, "does": {}, "do": {}, "is": {}, "are": {}, "was": {}, "were": {}, "to": {}, "of": {}, "in": {}, "on": {}, "at": {},
	"for": {}, "with": {}, "about": {}, "tell": {}, "me": {}, "it": {}, "this": {}, "that": {}, "and": {}, "or": {}, "if": {},
	"be": {}, "been": {}, "being": {}, "as": {}, "from": {}, "by": {},
}

type SearchOptions struct {
	MinScore     float64
	Tiers        []domain.MemoryTier
	Kinds        []domain.MemoryKind
	DisableTouch bool
	Debug        bool
}

type SearchDebugInfo struct {
	Plan    SearchPlanDebug      `json:"plan"`
	Ranking []SearchRankingDebug `json:"ranking,omitempty"`
}

type SearchPlanDebug struct {
	Intent           string   `json:"intent"`
	Confidence       float64  `json:"confidence"`
	Entities         []string `json:"entities,omitempty"`
	Relations        []string `json:"relations,omitempty"`
	TimeConstraints  []string `json:"time_constraints,omitempty"`
	RequiredEvidence string   `json:"required_evidence,omitempty"`
	FallbackPath     []string `json:"fallback_path,omitempty"`
}

type SearchRankingDebug struct {
	Rank         int     `json:"rank"`
	MemoryID     string  `json:"memory_id"`
	Kind         string  `json:"kind"`
	Tier         string  `json:"tier"`
	LexicalScore float64 `json:"lexical_score"`
	QueryOverlap float64 `json:"query_overlap"`
	RouteFit     float64 `json:"route_fit"`
}

func (s *Service) Search(ctx context.Context, tenantID, query string, topK int) ([]domain.Memory, error) {
	return s.SearchWithFilters(ctx, tenantID, query, topK, SearchOptions{})
}

func (s *Service) SearchWithFiltersDebug(
	ctx context.Context,
	tenantID, query string,
	topK int,
	opts SearchOptions,
) ([]domain.Memory, *SearchDebugInfo, error) {
	items, err := s.SearchWithFilters(ctx, tenantID, query, topK, opts)
	if err != nil {
		return nil, nil, err
	}
	profile := classifyQuery(query)
	plan := buildQueryPlan(query, profile)
	debug := &SearchDebugInfo{
		Plan: SearchPlanDebug{
			Intent:           plan.Intent,
			Confidence:       clamp01(plan.Confidence),
			Entities:         append([]string{}, plan.Entities...),
			Relations:        append([]string{}, plan.Relations...),
			TimeConstraints:  append([]string{}, plan.TimeConstraints...),
			RequiredEvidence: plan.RequiredEvidence,
			FallbackPath:     append([]string{}, plan.FallbackPath...),
		},
	}

	queryTokens := normalizedRankingTokens(query)
	for i, memory := range items {
		debug.Ranking = append(debug.Ranking, SearchRankingDebug{
			Rank:         i + 1,
			MemoryID:     memory.ID,
			Kind:         string(memory.Kind),
			Tier:         string(memory.Tier),
			LexicalScore: clamp01(lexicalContentScore(query, memory.Content)),
			QueryOverlap: clamp01(queryOverlapScore(queryTokens, normalizedRankingTokens(memory.Content))),
			RouteFit:     clamp01(normalizedRouteBoost(routeBoost(memory, profile))),
		})
	}

	return items, debug, nil
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
	if s.shouldApplyImplicitCanonicalKinds(opts) {
		canonicalOpts := opts
		canonicalOpts.Kinds = []domain.MemoryKind{
			domain.MemoryKindObservation,
			domain.MemoryKindEvent,
			domain.MemoryKindSummary,
		}
		canonicalResults, err := s.SearchWithFilters(ctx, tenantID, query, topK, canonicalOpts)
		if err != nil {
			return nil, err
		}
		if len(canonicalResults) > 0 {
			return canonicalResults, nil
		}
	}
	if topK <= 0 {
		topK = 10
	}
	profile := classifyQuery(query)
	plan := buildQueryPlan(query, profile)
	candidateTopK := candidateWindow(topK, profile, len(opts.Kinds) > 0 || len(opts.Tiers) > 0)
	searchQueries := buildSearchQueries(query, profile)
	var (
		embedDur  time.Duration
		bm25Dur   time.Duration
		vectorDur time.Duration
		fuseDur   time.Duration
	)

	if s.entityRepo != nil {
		aggregated, handled, err := s.searchByEntityFacts(ctx, tenantID, query, topK, opts, tierFilter, kindFilter, plan)
		if err != nil {
			return nil, err
		}
		if handled {
			return aggregated, nil
		}
	}

	lexicalCandidates := make([]lexicalCandidate, 0, len(searchQueries)*candidateTopK)
	plannedQueries := append([]string{}, searchQueries...)
	if s.entityRepo != nil {
		if hintedEntity, ok := classifyEntityHintQuery(query, profile); ok {
			hintCandidates, err := s.collectEntityHintCandidates(
				ctx,
				tenantID,
				query,
				hintedEntity,
				candidateTopK,
				tierFilter,
				kindFilter,
			)
			if err != nil {
				return nil, err
			}
			lexicalCandidates = append(lexicalCandidates, hintCandidates...)
		}
	}
	bm25Start := time.Now()
	initialLexical, err := s.collectLexicalCandidates(
		ctx,
		tenantID,
		searchQueries,
		candidateTopK,
		opts,
		tierFilter,
		kindFilter,
	)
	if err != nil {
		return nil, err
	}
	lexicalCandidates = append(lexicalCandidates, initialLexical...)
	if profile.MultiHop {
		decomposedQueries, err := s.buildLLMMultiHopQueries(ctx, query)
		if err != nil {
			s.logDebugf("[pali-search] multihop_llm_decomposition_error=%v", err)
		}
		if len(decomposedQueries) > 0 {
			plannedQueries = appendUniqueSearchQueries(plannedQueries, decomposedQueries)
			decomposedLexical, err := s.collectLexicalCandidates(
				ctx,
				tenantID,
				decomposedQueries,
				candidateTopK,
				opts,
				tierFilter,
				kindFilter,
			)
			if err != nil {
				return nil, err
			}
			lexicalCandidates = append(lexicalCandidates, decomposedLexical...)
		}
		if s.entityRepo != nil && s.multiHop.EntityFactBridgeEnabled {
			bridgeCandidates, err := s.collectGraphEntityBridgeCandidates(
				ctx,
				tenantID,
				query,
				plan,
				lexicalCandidates,
				candidateTopK,
				tierFilter,
				kindFilter,
			)
			if err != nil {
				return nil, err
			}
			lexicalCandidates = append(lexicalCandidates, bridgeCandidates...)
		}
		if s.multiHop.TokenExpansionFallback {
			iterativeQueries := buildIterativeMultiHopQueries(query, lexicalCandidates, 2)
			if len(iterativeQueries) > 0 {
				plannedQueries = appendUniqueSearchQueries(plannedQueries, iterativeQueries)
				iterativeLexical, err := s.collectLexicalCandidates(
					ctx,
					tenantID,
					iterativeQueries,
					candidateTopK,
					opts,
					tierFilter,
					kindFilter,
				)
				if err != nil {
					return nil, err
				}
				lexicalCandidates = append(lexicalCandidates, iterativeLexical...)
			}
		}
	}
	bm25Dur = time.Since(bm25Start)

	var denseCandidates []domain.VectorstoreCandidate
	if s.vector != nil && s.embedder != nil {
		embedStart := time.Now()
		embeddings, err := s.embedSearchQueries(ctx, plannedQueries)
		if err != nil {
			return nil, err
		}
		embedDur = time.Since(embedStart)
		vectorStart := time.Now()
		vectorCandidateTopK := candidateTopK
		if len(kindFilter) > 0 || len(tierFilter) > 0 {
			vectorCandidateTopK = candidateTopK * 3
			if vectorCandidateTopK > 600 {
				vectorCandidateTopK = 600
			}
		}
		for _, queryEmbedding := range embeddings {
			candidates, err := s.vector.Search(ctx, tenantID, queryEmbedding, vectorCandidateTopK)
			if err != nil {
				return nil, err
			}
			if len(kindFilter) > 0 || len(tierFilter) > 0 {
				candidates, err = s.filterDenseCandidatesByMetadata(ctx, tenantID, candidates, tierFilter, kindFilter)
				if err != nil {
					return nil, err
				}
			}
			denseCandidates = append(denseCandidates, candidates...)
		}
		vectorDur = time.Since(vectorStart)
	}

	if len(lexicalCandidates) == 0 && len(denseCandidates) == 0 {
		return []domain.Memory{}, nil
	}
	slices.SortFunc(lexicalCandidates, func(a, b lexicalCandidate) int {
		switch {
		case a.Score > b.Score:
			return -1
		case a.Score < b.Score:
			return 1
		default:
			return strings.Compare(a.Memory.ID, b.Memory.ID)
		}
	})
	slices.SortFunc(denseCandidates, func(a, b domain.VectorstoreCandidate) int {
		switch {
		case a.Similarity > b.Similarity:
			return -1
		case a.Similarity < b.Similarity:
			return 1
		default:
			return strings.Compare(a.MemoryID, b.MemoryID)
		}
	})

	fuseStart := time.Now()
	ids, signalByID := fuseCandidatesByRRF(denseCandidates, lexicalCandidates, candidateTopK)
	fuseDur = time.Since(fuseStart)
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

	rankingProfile := profile
	scored := rankMemories(memories, signalByID, candidateTopK, query, rankingProfile, plan, s.ranking)
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
	if len(kindFilter) == 0 {
		filtered = preferCanonicalUnits(filtered)
	}

	out := make([]domain.Memory, 0, len(filtered))
	for _, item := range filtered {
		out = append(out, item.Memory)
	}
	if len(kindFilter) == 0 && shouldExpandGroundedContext(profile) {
		out, err = s.expandGroundedContextMemories(ctx, tenantID, out, topK)
		if err != nil {
			return nil, err
		}
	}
	if len(out) > topK {
		out = out[:topK]
	}
	orderedIDs := make([]string, 0, len(out))
	for _, memory := range out {
		orderedIDs = append(orderedIDs, memory.ID)
	}
	if !opts.DisableTouch && len(orderedIDs) > 0 {
		_ = s.repo.Touch(ctx, tenantID, orderedIDs)
	}
	s.logInfof(
		"[pali-search] tenant=%s embed_ms=%d bm25_ms=%d vector_ms=%d fuse_ms=%d queried_k=%d returned=%d",
		tenantID,
		embedDur.Milliseconds(),
		bm25Dur.Milliseconds(),
		vectorDur.Milliseconds(),
		fuseDur.Milliseconds(),
		candidateTopK,
		len(out),
	)
	if memoryCount, err := s.tenantRepo.MemoryCount(ctx, tenantID); err == nil {
		s.logInfof("[pali-search] tenant=%s memories=%d queried_k=%d", tenantID, memoryCount, candidateTopK)
	} else {
		s.logDebugf("[pali-search] tenant=%s memory_count_error=%v", tenantID, err)
	}

	return out, nil
}

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

	lookupLimit := topK * 6
	if lookupLimit < 20 {
		lookupLimit = 20
	}
	facts, err := s.entityRepo.ListByEntityRelation(ctx, tenantID, entity, relation, lookupLimit)
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
	scored := rankMemories(filtered, signalByID, maxInt(topK, len(filtered)), query, profile, plan, s.ranking)
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

type candidateSignal struct {
	DenseScore   float64
	DenseRank    int
	LexicalScore float64
	LexicalRank  int
	RRFScore     float64
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
	relations := []string{"activity", "event", "plan", "identity", "role", "place", "book"}
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

func (s *Service) collectGraphEntityBridgeCandidates(
	ctx context.Context,
	tenantID, query string,
	plan queryPlan,
	lexical []lexicalCandidate,
	topK int,
	tierFilter map[domain.MemoryTier]struct{},
	kindFilter map[domain.MemoryKind]struct{},
) ([]lexicalCandidate, error) {
	if s.entityRepo == nil || plan.Intent != "graph_entity_expansion" {
		return []lexicalCandidate{}, nil
	}
	entities := graphEntityBridgeSeeds(query, plan, lexical)
	if len(entities) == 0 {
		return []lexicalCandidate{}, nil
	}
	relations := []string{"activity", "event", "plan", "identity", "role", "place", "book"}
	lookupLimit := topK / 10
	if lookupLimit < 8 {
		lookupLimit = 8
	}
	if lookupLimit > 24 {
		lookupLimit = 24
	}
	ids := make([]string, 0, len(entities)*len(relations)*lookupLimit)
	seen := make(map[string]struct{}, len(entities)*len(relations)*lookupLimit)
	for _, entity := range entities {
		for _, relation := range relations {
			facts, err := s.entityRepo.ListByEntityRelation(ctx, tenantID, entity, relation, lookupLimit)
			if err != nil {
				return nil, err
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
		lexicalScore := lexicalContentScore(query, memory.Content)
		entityHit := 0.0
		lowered := strings.ToLower(memory.Content)
		for _, entity := range entities {
			if strings.Contains(lowered, entity) {
				entityHit = 1
				break
			}
		}
		score := clamp01((0.72 * lexicalScore) + (0.28 * entityHit))
		if score < 0.16 {
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

func graphEntityBridgeSeeds(query string, plan queryPlan, lexical []lexicalCandidate) []string {
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
	if len(seeds) > 4 {
		return seeds[:4]
	}
	return seeds
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

func (s *Service) rerankScoredCandidates(items []scoredMemory, query string, topK int) []scoredMemory {
	if len(items) < 2 {
		return items
	}
	opts := s.rerank

	window := opts.Window
	if window <= 0 {
		window = defaultRerankOptions().Window
	}
	if topK > window {
		window = topK
	}
	if window > len(items) {
		window = len(items)
	}
	blend := opts.Blend
	if blend < 0 {
		blend = 0
	}
	if blend > 1 {
		blend = 1
	}
	if blend == 0 {
		blend = defaultRerankOptions().Blend
	}

	head := append([]scoredMemory{}, items[:window]...)
	tail := append([]scoredMemory{}, items[window:]...)
	idfByToken := buildLocalIDFMap(query, head)
	for i := range head {
		pairwise := pairwiseRerankScore(query, head[i].Memory, idfByToken)
		head[i].Score = clamp01(((1 - blend) * head[i].Score) + (blend * pairwise))
	}
	slices.SortFunc(head, func(a, b scoredMemory) int {
		switch {
		case a.Score > b.Score:
			return -1
		case a.Score < b.Score:
			return 1
		default:
			return strings.Compare(a.Memory.ID, b.Memory.ID)
		}
	})
	return append(head, tail...)
}

func pairwiseRerankScore(query string, memory domain.Memory, idfByToken map[string]float64) float64 {
	content := strings.TrimSpace(memory.Content)
	queryView := strings.TrimSpace(memory.QueryViewText)
	doc := content
	if queryView != "" {
		if doc != "" {
			doc += "\n"
		}
		doc += queryView
	}
	lexical := lexicalContentScore(query, doc)
	phrase := orderedBigramCoverage(query, doc)
	proximity := queryTokenProximityScore(query, doc)
	queryViewMatch := lexicalContentScore(query, queryView)
	idfCoverage := idfCoverageScore(query, doc, idfByToken)
	idfQueryView := idfCoverageScore(query, queryView, idfByToken)
	return weightedAverage(
		[]float64{lexical, phrase, proximity, queryViewMatch, idfCoverage, idfQueryView},
		[]float64{0.28, 0.16, 0.15, 0.11, 0.20, 0.10},
	)
}

func buildLocalIDFMap(query string, candidates []scoredMemory) map[string]float64 {
	queryTokens := normalizedRankingTokenList(query)
	if len(queryTokens) == 0 || len(candidates) == 0 {
		return map[string]float64{}
	}
	seenQuery := make(map[string]struct{}, len(queryTokens))
	uniqueQuery := make([]string, 0, len(queryTokens))
	for _, token := range queryTokens {
		if _, ok := seenQuery[token]; ok {
			continue
		}
		seenQuery[token] = struct{}{}
		uniqueQuery = append(uniqueQuery, token)
	}
	docFreq := make(map[string]int, len(uniqueQuery))
	for _, item := range candidates {
		docTokens := normalizedRankingTokens(item.Memory.Content + "\n" + item.Memory.QueryViewText)
		for _, token := range uniqueQuery {
			if _, ok := docTokens[token]; ok {
				docFreq[token]++
			}
		}
	}
	n := float64(len(candidates))
	idf := make(map[string]float64, len(uniqueQuery))
	for _, token := range uniqueQuery {
		df := float64(docFreq[token])
		// BM25-style local IDF with +1 smoothing to keep values positive.
		idf[token] = math.Log(1+((n-df+0.5)/(df+0.5))) + 1
	}
	return idf
}

func idfCoverageScore(query, content string, idfByToken map[string]float64) float64 {
	queryTokens := normalizedRankingTokens(query)
	docTokens := normalizedRankingTokens(content)
	if len(queryTokens) == 0 || len(docTokens) == 0 {
		return 0
	}
	total := 0.0
	matched := 0.0
	for token := range queryTokens {
		w := idfByToken[token]
		if w <= 0 {
			w = 1
		}
		total += w
		if _, ok := docTokens[token]; ok {
			matched += w
		}
	}
	if total == 0 {
		return 0
	}
	return clamp01(matched / total)
}

func queryTokenProximityScore(query, content string) float64 {
	queryTokens := normalizedRankingTokenList(query)
	contentTokens := normalizedRankingTokenList(content)
	if len(queryTokens) == 0 || len(contentTokens) == 0 {
		return 0
	}
	seen := make(map[string]struct{}, len(queryTokens))
	orderedUnique := make([]string, 0, len(queryTokens))
	for _, token := range queryTokens {
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		orderedUnique = append(orderedUnique, token)
	}
	if len(orderedUnique) == 0 {
		return 0
	}

	prev := -1
	matched := 0
	totalGap := 0
	for _, token := range orderedUnique {
		pos := findTokenPositionAfter(contentTokens, token, prev+1)
		if pos < 0 {
			continue
		}
		if prev >= 0 && pos > prev {
			totalGap += pos - prev - 1
		}
		prev = pos
		matched++
	}
	if matched == 0 {
		return 0
	}
	coverage := float64(matched) / float64(len(orderedUnique))
	if matched == 1 {
		return clamp01(coverage * 0.5)
	}
	avgGap := float64(totalGap) / float64(matched-1)
	compactness := 1.0 / (1.0 + avgGap)
	return clamp01((0.65 * coverage) + (0.35 * compactness))
}

func findTokenPositionAfter(tokens []string, target string, start int) int {
	if target == "" || start >= len(tokens) {
		return -1
	}
	if start < 0 {
		start = 0
	}
	for i := start; i < len(tokens); i++ {
		if tokens[i] == target {
			return i
		}
	}
	return -1
}

func appendUniqueSearchQueries(base, extra []string) []string {
	if len(extra) == 0 {
		return base
	}
	seen := make(map[string]struct{}, len(base)+len(extra))
	for _, query := range base {
		seen[strings.ToLower(strings.TrimSpace(query))] = struct{}{}
	}
	out := append([]string{}, base...)
	for _, query := range extra {
		key := strings.ToLower(strings.TrimSpace(query))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, query)
	}
	return out
}

type lexicalCandidate struct {
	Memory domain.Memory
	Score  float64
}

func rankMemories(
	memories []domain.Memory,
	signalByID map[string]candidateSignal,
	candidateLimit int,
	query string,
	profile queryProfile,
	plan queryPlan,
	ranking RankingOptions,
) []scoredMemory {
	now := time.Now().UTC()
	hasNonRawKinds := false
	for _, memory := range memories {
		if memory.Kind != domain.MemoryKindRawTurn {
			hasNonRawKinds = true
			break
		}
	}
	recencyRaw := make([]float64, len(memories))
	relevanceRaw := make([]float64, len(memories))
	rawRelevance := make([]float64, len(memories))
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
		signal := signalByID[m.ID]
		denseRankNorm := rankToNormalized(signal.DenseRank, candidateLimit)
		lexicalRankNorm := rankToNormalized(signal.LexicalRank, candidateLimit)
		routeFit := normalizedRouteBoost(routeBoost(m, profile))
		entitySlotHit := entityRelationSignal(plan, m.Content)
		denseScoreW, denseRankW := 0.34, 0.08
		if signal.DenseScore == 0 && signal.DenseRank == 0 {
			denseScoreW, denseRankW = 0, 0
		}
		rawRel := weightedAverage(
			[]float64{
				signal.DenseScore,
				signal.LexicalScore,
				denseRankNorm,
				lexicalRankNorm,
				signal.RRFScore,
				entitySlotHit,
				routeFit,
			},
			[]float64{denseScoreW, 0.12, denseRankW, 0.20, 0.10, 0.10, 0.06},
		)
		rel := scoring.Relevance(rawRel)
		imp := m.Importance
		if imp < 0 {
			imp = 0
		}
		if imp > 1 {
			imp = 1
		}

		recencyRaw[i] = rec
		relevanceRaw[i] = rel
		rawRelevance[i] = rawRel
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
		docTokens := normalizedRankingTokens(m.Content)
		overlap := queryOverlapScore(queryTokens, docTokens)
		dependency := orderedBigramCoverage(query, m.Content)

		rec := scoring.MinMax(recencyRaw[i], minRec, maxRec)
		rel := scoring.MinMax(relevanceRaw[i], minRel, maxRel)
		imp := scoring.MinMax(importanceRaw[i], minImp, maxImp)

		total := 0.0
		switch ranking.Algorithm {
		case "match":
			route := normalizedRouteBoost(routeBoost(m, profile))
			if dependency > 0 {
				overlap = clamp01((0.8 * overlap) + (0.2 * dependency))
			}
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
			recencyW := ranking.WAL.Recency
			relevanceW := ranking.WAL.Relevance
			importanceW := ranking.WAL.Importance
			// For factoid-style non-temporal questions, prioritize retrieval
			// relevance over freshness/importance to avoid recency noise.
			if shouldUseRelevanceFirst(query, profile) {
				recencyW *= 0.20
				importanceW *= 0.25
				relevanceW *= 1.75
			}
			total = weightedAverage(
				[]float64{rec, rel, imp},
				[]float64{
					recencyW,
					relevanceW,
					importanceW,
				},
			)
			if !profile.Temporal && !profile.MultiHop {
				total = applySingleHopPrecisionBoost(total, overlap, dependency, m, hasNonRawKinds)
			}
			if !profile.Temporal && !profile.MultiHop {
				total = applyLowEvidencePenalty(total, rawRelevance[i], overlap, signalByID[m.ID].RRFScore)
			}
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

// applyLowEvidencePenalty demotes items whose retrieval signal is weak on
// ALL axes. The rrfScore guard prevents demoting items that BM25 + vector
// retrieval both ranked highly — those are paraphrase/synonym matches where
// surface overlap is low but the underlying retrieval signal is strong.
func applyLowEvidencePenalty(total, rawRelevance, overlap, rrfScore float64) float64 {
	// High fused retrieval confidence means at least one route is strongly
	// supportive. Do not penalize such items for low token overlap.
	if rrfScore >= 0.75 {
		return clamp01(total)
	}
	// Penalize only when BOTH overlap and fused retrieval confidence are weak.
	if rawRelevance < 0.30 && overlap < 0.15 && rrfScore < 0.65 {
		return clamp01(total * 0.25)
	}
	if rawRelevance < 0.55 && overlap < 0.20 && rrfScore < 0.55 {
		return clamp01(total * 0.45)
	}
	return clamp01(total)
}

func entityRelationSignal(plan queryPlan, content string) float64 {
	entity := strings.ToLower(strings.TrimSpace(plan.primaryEntity()))
	if entity == "" {
		return 0
	}
	lowered := strings.ToLower(content)
	hasEntity := strings.Contains(lowered, entity)
	if !hasEntity {
		return 0
	}
	relation := strings.TrimSpace(plan.primaryRelation())
	if relation == "" {
		return 0.65
	}
	for _, token := range relationHintTokens(relation) {
		if strings.Contains(lowered, token) {
			return 1
		}
	}
	return 0.65
}

func relationHintTokens(relation string) []string {
	switch strings.ToLower(strings.TrimSpace(relation)) {
	case "activity":
		return []string{"activity", "activities", "hobby", "interest", "enjoys", "likes", "does"}
	case "event":
		return []string{"event", "attended", "joined", "participated", "met", "happened"}
	case "place":
		return []string{"place", "visited", "went", "travel", "trip", "location"}
	case "book":
		return []string{"book", "read", "reading", "novel"}
	case "role":
		return []string{"role", "job", "works", "position"}
	case "identity":
		return []string{"name", "identity", "is", "called"}
	default:
		if relation == "" {
			return []string{}
		}
		return []string{strings.ToLower(relation)}
	}
}

func applySingleHopPrecisionBoost(total, overlap, dependency float64, memory domain.Memory, hasNonRawKinds bool) float64 {
	adjusted := clamp01((0.76 * clamp01(total)) + (0.14 * clamp01(overlap)) + (0.10 * clamp01(dependency)))
	kindMultiplier := 1.0
	if isCanonicalFactMemory(memory) {
		kindMultiplier *= 1.12
	}
	if dependency >= 0.50 {
		kindMultiplier *= 1.04
	}
	switch memory.Kind {
	case domain.MemoryKindObservation:
		kindMultiplier *= 1.08
	case domain.MemoryKindEvent:
		kindMultiplier *= 1.06
	case domain.MemoryKindSummary:
		kindMultiplier *= 0.96
	case domain.MemoryKindRawTurn:
		if hasNonRawKinds {
			kindMultiplier *= 0.86
			if conversationalNoisePattern.MatchString(strings.ToLower(memory.Content)) {
				kindMultiplier *= 0.80
			}
		}
	}
	return clamp01(adjusted * kindMultiplier)
}

func shouldUseRelevanceFirst(query string, profile queryProfile) bool {
	if profile.MultiHop || profile.Temporal {
		return false
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return false
	}
	return profile.Person || factoidQueryPattern.MatchString(q)
}

func shouldExpandGroundedContext(profile queryProfile) bool {
	return profile.Temporal || profile.MultiHop
}

func shouldApplyPairwiseRerank(profile queryProfile, multiHop MultiHopOptions) bool {
	if profile.Temporal {
		return false
	}
	if profile.MultiHop {
		return multiHop.EnablePairwiseRerank
	}
	return true
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
	tokens := normalizedRankingTokenList(text)
	out := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		out[token] = struct{}{}
	}
	return out
}

func normalizedRankingTokenList(text string) []string {
	rawTokens := rankingTokenPattern.FindAllString(strings.ToLower(strings.TrimSpace(text)), -1)
	if len(rawTokens) == 0 {
		return []string{}
	}
	filtered := make([]string, 0, len(rawTokens))
	for _, token := range rawTokens {
		if len(token) < 2 {
			continue
		}
		if _, stop := rankingStopwordPattern[token]; stop {
			continue
		}
		filtered = append(filtered, token)
	}
	if len(filtered) > 0 {
		return filtered
	}
	// Fallback for short queries that collapse to only stopwords.
	out := make([]string, 0, len(rawTokens))
	for _, token := range rawTokens {
		if len(token) < 2 {
			continue
		}
		out = append(out, token)
	}
	return out
}

func orderedBigramCoverage(query, content string) float64 {
	queryTokens := normalizedRankingTokenList(query)
	if len(queryTokens) < 2 {
		return 0
	}
	contentTokens := normalizedRankingTokenList(content)
	if len(contentTokens) < 2 {
		return 0
	}
	docBigrams := make(map[string]struct{}, len(contentTokens)-1)
	for i := 0; i < len(contentTokens)-1; i++ {
		docBigrams[contentTokens[i]+" "+contentTokens[i+1]] = struct{}{}
	}
	matches := 0
	total := len(queryTokens) - 1
	for i := 0; i < total; i++ {
		if _, ok := docBigrams[queryTokens[i]+" "+queryTokens[i+1]]; ok {
			matches++
		}
	}
	if total <= 0 {
		return 0
	}
	return clamp01(float64(matches) / float64(total))
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

func candidateWindow(topK int, profile queryProfile, hasFilters bool) int {
	n := topK * 5
	if n < 50 {
		n = 50
	}
	if profile.MultiHop {
		n += 80
	} else if profile.Temporal {
		n += 40
	}
	if hasFilters {
		n += 30
	}
	maxWindow := 200
	if profile.MultiHop || hasFilters {
		maxWindow = 320
	}
	if n > maxWindow {
		n = maxWindow
	}
	return n
}

func preferCanonicalUnits(items []scoredMemory) []scoredMemory {
	if len(items) == 0 {
		return items
	}
	promoted := make([]scoredMemory, 0, len(items))
	raw := make([]scoredMemory, 0, len(items))
	for _, item := range items {
		if item.Memory.Kind == domain.MemoryKindRawTurn {
			raw = append(raw, item)
			continue
		}
		promoted = append(promoted, item)
	}
	if len(promoted) == 0 || len(raw) == 0 {
		return items
	}
	return append(promoted, raw...)
}

func isCanonicalFactMemory(memory domain.Memory) bool {
	if strings.TrimSpace(memory.CanonicalKey) == "" {
		return false
	}
	if memory.SourceFactIndex < 0 {
		return false
	}
	return memory.Kind == domain.MemoryKindObservation || memory.Kind == domain.MemoryKindEvent
}

func (s *Service) expandGroundedContextMemories(
	ctx context.Context,
	tenantID string,
	memories []domain.Memory,
	topK int,
) ([]domain.Memory, error) {
	repo, ok := s.repo.(domain.MemorySourceTurnRepository)
	if !ok || repo == nil || len(memories) == 0 {
		return memories, nil
	}

	out := make([]domain.Memory, 0, len(memories)+min(len(memories), 4))
	seen := make(map[string]struct{}, len(memories)+4)
	appendMemory := func(memory domain.Memory) {
		if strings.TrimSpace(memory.ID) == "" {
			return
		}
		if _, ok := seen[memory.ID]; ok {
			return
		}
		seen[memory.ID] = struct{}{}
		out = append(out, memory)
	}

	for _, memory := range memories {
		appendMemory(memory)
		if topK > 0 && len(out) >= topK {
			continue
		}
		if memory.Kind == domain.MemoryKindRawTurn || strings.TrimSpace(memory.SourceTurnHash) == "" {
			continue
		}
		siblings, err := repo.ListBySourceTurnHash(ctx, tenantID, memory.SourceTurnHash, 4)
		if err != nil {
			return nil, err
		}
		var rawTurn *domain.Memory
		var supportingFact *domain.Memory
		for i := range siblings {
			sibling := siblings[i]
			if sibling.ID == memory.ID {
				continue
			}
			switch sibling.Kind {
			case domain.MemoryKindRawTurn:
				if rawTurn == nil {
					candidate := sibling
					rawTurn = &candidate
				}
			case domain.MemoryKindEvent, domain.MemoryKindObservation:
				if supportingFact == nil {
					candidate := sibling
					supportingFact = &candidate
				}
			}
		}
		if rawTurn != nil {
			appendMemory(*rawTurn)
		}
		if supportingFact != nil {
			appendMemory(*supportingFact)
		}
	}
	return out, nil
}

func fuseCandidatesByRRF(
	dense []domain.VectorstoreCandidate,
	lexical []lexicalCandidate,
	limit int,
) ([]string, map[string]candidateSignal) {
	if limit <= 0 {
		limit = 10
	}

	rrfScore := make(map[string]float64, len(dense)+len(lexical))
	signalByID := make(map[string]candidateSignal, len(dense)+len(lexical))
	type rankScore struct {
		rank  int
		score float64
	}
	denseBest := make(map[string]rankScore, len(dense))
	for idx, candidate := range dense {
		id := strings.TrimSpace(candidate.MemoryID)
		if id == "" {
			continue
		}
		rank := idx + 1
		current, ok := denseBest[id]
		if !ok || rank < current.rank {
			denseBest[id] = rankScore{rank: rank, score: candidate.Similarity}
			continue
		}
		if candidate.Similarity > current.score {
			current.score = candidate.Similarity
			denseBest[id] = current
		}
	}
	for id, item := range denseBest {
		rrfScore[id] += 1.0 / float64(reciprocalRankFusionK+item.rank)
		signalByID[id] = candidateSignal{
			DenseScore: clamp01(item.score),
			DenseRank:  item.rank,
		}
	}

	lexicalBest := make(map[string]rankScore, len(lexical))
	for idx, candidate := range lexical {
		id := strings.TrimSpace(candidate.Memory.ID)
		if id == "" {
			continue
		}
		rank := idx + 1
		score := clamp01(candidate.Score)
		current, ok := lexicalBest[id]
		if !ok || rank < current.rank {
			lexicalBest[id] = rankScore{rank: rank, score: score}
			continue
		}
		if score > current.score {
			current.score = score
			lexicalBest[id] = current
		}
	}
	for id, item := range lexicalBest {
		rrfScore[id] += 1.0 / float64(reciprocalRankFusionK+item.rank)
		signal := signalByID[id]
		signal.LexicalScore = clamp01(item.score)
		signal.LexicalRank = item.rank
		signalByID[id] = signal
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

	filteredSignals := make(map[string]candidateSignal, len(ids))
	for _, id := range ids {
		signal := signalByID[id]
		signal.RRFScore = clamp01(rrfToNormalized(rrfScore[id]))
		filteredSignals[id] = signal
	}

	return ids, filteredSignals
}

func rankToNormalized(rank int, limit int) float64 {
	if rank <= 0 {
		return 0
	}
	if limit <= 1 {
		return 1
	}
	r := 1.0 - (float64(rank-1) / float64(limit-1))
	return clamp01(r)
}

func rrfToNormalized(rrf float64) float64 {
	if rrf <= 0 {
		return 0
	}
	// Map raw RRF to 0..1 without saturating top-ranked items to the same value.
	// Baseline is the single-route top-1 raw RRF.
	base := 1.0 / float64(reciprocalRankFusionK+1)
	return clamp01(rrf / (rrf + base))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
