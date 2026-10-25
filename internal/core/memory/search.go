package memory

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"time"

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

// SearchOptions configures memory retrieval and filtering.
type SearchOptions struct {
	MinScore      float64
	Tiers         []domain.MemoryTier
	Kinds         []domain.MemoryKind
	RetrievalKind SearchRetrievalKind
	DisableTouch  bool
	Debug         bool
}

// SearchRetrievalKind selects the retrieval path to use.
type SearchRetrievalKind string

const (
	// SearchRetrievalKindAuto lets the service choose the retrieval path.
	SearchRetrievalKindAuto SearchRetrievalKind = "auto"
	// SearchRetrievalKindVector forces vector retrieval.
	SearchRetrievalKindVector SearchRetrievalKind = "vector"
	// SearchRetrievalKindEntity forces entity-fact retrieval.
	SearchRetrievalKindEntity SearchRetrievalKind = "entity"
)

func normalizeSearchRetrievalKind(kind SearchRetrievalKind) (SearchRetrievalKind, bool) {
	switch strings.ToLower(strings.TrimSpace(string(kind))) {
	case "", string(SearchRetrievalKindAuto):
		return SearchRetrievalKindAuto, true
	case string(SearchRetrievalKindVector):
		return SearchRetrievalKindVector, true
	case string(SearchRetrievalKindEntity):
		return SearchRetrievalKindEntity, true
	default:
		return SearchRetrievalKindAuto, false
	}
}

// SearchDebugInfo captures search planning and ranking debug output.
type SearchDebugInfo struct {
	Plan    SearchPlanDebug      `json:"plan"`
	Ranking []SearchRankingDebug `json:"ranking,omitempty"`
}

// SearchPlanDebug captures the planner decision used for a search.
type SearchPlanDebug struct {
	Intent           string   `json:"intent"`
	Confidence       float64  `json:"confidence"`
	AnswerType       string   `json:"answer_type,omitempty"`
	Entities         []string `json:"entities,omitempty"`
	Relations        []string `json:"relations,omitempty"`
	TimeConstraints  []string `json:"time_constraints,omitempty"`
	RequiredEvidence string   `json:"required_evidence,omitempty"`
	FallbackPath     []string `json:"fallback_path,omitempty"`
}

// SearchRankingDebug captures per-result ranking diagnostics.
type SearchRankingDebug struct {
	Rank         int     `json:"rank"`
	MemoryID     string  `json:"memory_id"`
	Kind         string  `json:"kind"`
	Tier         string  `json:"tier"`
	LexicalScore float64 `json:"lexical_score"`
	QueryOverlap float64 `json:"query_overlap"`
	RouteFit     float64 `json:"route_fit"`
}

// compareLexicalCandidates orders lexical candidates by score descending.
// BM25 ties (e.g. identical content) keep the repository's newest-first
// order (UpdatedAt descending) instead of falling to random IDs; ID is the
// final tie-break.
func compareLexicalCandidates(a, b lexicalCandidate) int {
	switch {
	case a.Score > b.Score:
		return -1
	case a.Score < b.Score:
		return 1
	default:
		if c := b.Memory.UpdatedAt.Compare(a.Memory.UpdatedAt); c != 0 {
			return c
		}
		return strings.Compare(a.Memory.ID, b.Memory.ID)
	}
}

// Search runs a query with default filters and returns matching memories.
func (s *Service) Search(ctx context.Context, tenantID, query string, topK int) ([]domain.Memory, error) {
	return s.SearchWithFilters(ctx, tenantID, query, topK, SearchOptions{})
}

// SearchWithFiltersDebug runs search and returns matching memories plus debug data.
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
			AnswerType:       plan.AnswerType,
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
			RouteFit:     clamp01(normalizedRouteBoost(routeBoost(memory, profile, plan, s.retrieval))),
		})
	}

	return items, debug, nil
}

// SearchWithFilters runs a filtered memory search.
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
	retrievalKind, ok := normalizeSearchRetrievalKind(opts.RetrievalKind)
	if !ok {
		return nil, domain.ErrInvalidInput
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
	req := s.planSearch(tenantID, query, topK, opts, tierFilter, kindFilter, retrievalKind)
	var timings searchStageTimings

	if s.entityRepo != nil && retrievalKind != SearchRetrievalKindVector {
		aggregated, handled, err := s.searchByEntityFacts(ctx, tenantID, query, topK, opts, tierFilter, kindFilter, req.plan)
		if err != nil {
			return nil, err
		}
		if handled {
			return aggregated, nil
		}
	}

	lexicalCandidates, plannedQueries, err := s.collectLexicalSearchCandidates(ctx, req, &timings)
	if err != nil {
		return nil, err
	}
	denseCandidates, err := s.collectDenseSearchCandidates(ctx, req, plannedQueries, &timings)
	if err != nil {
		return nil, err
	}
	if len(lexicalCandidates) == 0 && len(denseCandidates) == 0 {
		return []domain.Memory{}, nil
	}

	ids, signalByID := fuseSearchCandidates(req, lexicalCandidates, denseCandidates, &timings)
	if len(ids) == 0 {
		return []domain.Memory{}, nil
	}

	scored, loaded, err := s.rankSearchCandidates(ctx, req, ids, signalByID)
	if err != nil {
		return nil, err
	}
	if !loaded {
		return []domain.Memory{}, nil
	}

	out, err := s.postFilterSearchResults(ctx, req, scored)
	if err != nil {
		return nil, err
	}
	s.finishSearch(ctx, req, out, timings)
	return out, nil
}

// searchRequest is the validated input and query plan shared by the
// SearchWithFilters stages.
type searchRequest struct {
	tenantID      string
	query         string
	topK          int
	opts          SearchOptions
	tierFilter    map[domain.MemoryTier]struct{}
	kindFilter    map[domain.MemoryKind]struct{}
	retrievalKind SearchRetrievalKind
	profile       queryProfile
	plan          queryPlan
	searchTuning  RetrievalSearchTuningOptions
	candidateTopK int
	searchQueries []string
}

// searchStageTimings records per-stage latency for the search log line.
type searchStageTimings struct {
	embed  time.Duration
	bm25   time.Duration
	vector time.Duration
	fuse   time.Duration
}

// planSearch classifies the query and derives the candidate window and the
// initial lexical query variants.
func (s *Service) planSearch(
	tenantID, query string,
	topK int,
	opts SearchOptions,
	tierFilter map[domain.MemoryTier]struct{},
	kindFilter map[domain.MemoryKind]struct{},
	retrievalKind SearchRetrievalKind,
) searchRequest {
	profile := classifyQuery(query)
	plan := buildQueryPlan(query, profile)
	searchTuning := normalizeRetrievalSearchTuningOptions(s.retrieval.SearchTuning)
	return searchRequest{
		tenantID:      tenantID,
		query:         query,
		topK:          topK,
		opts:          opts,
		tierFilter:    tierFilter,
		kindFilter:    kindFilter,
		retrievalKind: retrievalKind,
		profile:       profile,
		plan:          plan,
		searchTuning:  searchTuning,
		candidateTopK: candidateWindow(topK, profile, plan, len(opts.Kinds) > 0 || len(opts.Tiers) > 0, searchTuning),
		searchQueries: buildSearchQueries(query, profile),
	}
}

// collectLexicalSearchCandidates gathers entity-hint, BM25 (initial, adaptive,
// multi-hop) and graph-bridge candidates. It also returns every query
// variant that was run, for dense retrieval.
func (s *Service) collectLexicalSearchCandidates(
	ctx context.Context,
	req searchRequest,
	timings *searchStageTimings,
) ([]lexicalCandidate, []string, error) {
	tenantID, query := req.tenantID, req.query
	profile, plan := req.profile, req.plan
	candidateTopK := req.candidateTopK
	opts, tierFilter, kindFilter := req.opts, req.tierFilter, req.kindFilter
	searchQueries := req.searchQueries

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
				return nil, nil, err
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
		return nil, nil, err
	}
	lexicalCandidates = append(lexicalCandidates, initialLexical...)
	adaptiveQueries := buildAdaptiveSearchQueries(query, profile, plan, initialLexical, req.searchTuning)
	if len(adaptiveQueries) > 0 {
		plannedQueries = appendUniqueSearchQueries(plannedQueries, adaptiveQueries)
		adaptiveLexical, err := s.collectLexicalCandidates(
			ctx,
			tenantID,
			adaptiveQueries,
			candidateTopK,
			opts,
			tierFilter,
			kindFilter,
		)
		if err != nil {
			return nil, nil, err
		}
		lexicalCandidates = append(lexicalCandidates, adaptiveLexical...)
		s.logDebugf(
			"[pali-search] adaptive_expansion=true queries=%d best_lexical=%.3f confidence=%.3f",
			len(adaptiveQueries),
			bestLexicalCandidateScore(initialLexical),
			clamp01(plan.Confidence),
		)
	}
	enableGraphEntityBridge := s.entityRepo != nil && s.multiHop.EntityFactBridgeEnabled && (profile.MultiHop || req.retrievalKind == SearchRetrievalKindEntity)
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
				return nil, nil, err
			}
			lexicalCandidates = append(lexicalCandidates, decomposedLexical...)
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
					return nil, nil, err
				}
				lexicalCandidates = append(lexicalCandidates, iterativeLexical...)
			}
		}
	}
	if enableGraphEntityBridge {
		bridgeCandidates, err := s.collectGraphEntityBridgeCandidates(
			ctx,
			tenantID,
			query,
			plan,
			lexicalCandidates,
			candidateTopK,
			tierFilter,
			kindFilter,
			req.retrievalKind == SearchRetrievalKindEntity,
		)
		if err != nil {
			return nil, nil, err
		}
		lexicalCandidates = append(lexicalCandidates, bridgeCandidates...)
	}
	timings.bm25 = time.Since(bm25Start)
	return lexicalCandidates, plannedQueries, nil
}

// collectDenseSearchCandidates embeds every planned query and gathers vector
// candidates, applying kind/tier filters when set.
func (s *Service) collectDenseSearchCandidates(
	ctx context.Context,
	req searchRequest,
	plannedQueries []string,
	timings *searchStageTimings,
) ([]domain.VectorstoreCandidate, error) {
	var denseCandidates []domain.VectorstoreCandidate
	if req.retrievalKind == SearchRetrievalKindEntity || s.vector == nil || s.embedder == nil {
		return denseCandidates, nil
	}
	tierFilter, kindFilter := req.tierFilter, req.kindFilter
	embedStart := time.Now()
	embeddings, err := s.embedSearchQueries(ctx, plannedQueries)
	if err != nil {
		return nil, err
	}
	timings.embed = time.Since(embedStart)
	vectorStart := time.Now()
	vectorCandidateTopK := req.candidateTopK
	if len(kindFilter) > 0 || len(tierFilter) > 0 {
		vectorCandidateTopK = req.candidateTopK * 3
		if vectorCandidateTopK > 600 {
			vectorCandidateTopK = 600
		}
	}
	for _, queryEmbedding := range embeddings {
		candidates, err := s.vector.Search(ctx, req.tenantID, queryEmbedding, vectorCandidateTopK)
		if err != nil {
			return nil, err
		}
		if len(kindFilter) > 0 || len(tierFilter) > 0 {
			candidates, err = s.filterDenseCandidatesByMetadata(ctx, req.tenantID, candidates, tierFilter, kindFilter)
			if err != nil {
				return nil, err
			}
		}
		denseCandidates = append(denseCandidates, candidates...)
	}
	timings.vector = time.Since(vectorStart)
	return denseCandidates, nil
}

// fuseSearchCandidates orders both candidate lists and fuses them with
// reciprocal rank fusion into at most candidateTopK memory IDs.
func fuseSearchCandidates(
	req searchRequest,
	lexicalCandidates []lexicalCandidate,
	denseCandidates []domain.VectorstoreCandidate,
	timings *searchStageTimings,
) ([]string, map[string]candidateSignal) {
	slices.SortFunc(lexicalCandidates, compareLexicalCandidates)
	slices.SortFunc(denseCandidates, func(a, b domain.VectorstoreCandidate) int {
		return compareScoreDescThenID(a.Similarity, b.Similarity, a.MemoryID, b.MemoryID)
	})

	fuseStart := time.Now()
	ids, signalByID := fuseCandidatesByRRF(denseCandidates, lexicalCandidates, req.candidateTopK)
	timings.fuse = time.Since(fuseStart)
	return ids, signalByID
}

// rankSearchCandidates loads the fused memories, scores them, and applies the
// pairwise and early-rank rerank stages. loaded is false when none of the
// fused IDs could be loaded.
func (s *Service) rankSearchCandidates(
	ctx context.Context,
	req searchRequest,
	ids []string,
	signalByID map[string]candidateSignal,
) ([]scoredMemory, bool, error) {
	memories, err := s.repo.GetByIDs(ctx, req.tenantID, ids)
	if err != nil {
		return nil, false, err
	}
	if len(memories) == 0 {
		return nil, false, nil
	}

	scored := sortScoredByScore(rankMemories(memories, signalByID, req.candidateTopK, req.query, req.profile, req.plan, s.ranking, s.retrieval))
	if shouldApplyPairwiseRerank(req.profile, s.multiHop) {
		scored = s.rerankScoredCandidates(scored, req.query, req.topK)
	}
	if s.retrieval.EarlyRankRerankEnabled && !req.profile.MultiHop {
		scored = s.applyEarlyRankRerank(scored, req.query, req.plan, req.topK, req.profile)
	}
	return scored, true, nil
}

// postFilterSearchResults applies kind/tier/min-score filters, canonical-unit
// ordering and grounded-context expansion, then truncates to topK.
func (s *Service) postFilterSearchResults(
	ctx context.Context,
	req searchRequest,
	scored []scoredMemory,
) ([]domain.Memory, error) {
	filtered := make([]scoredMemory, 0, len(scored))
	for _, item := range scored {
		if !passesFilters(item.Memory, req.kindFilter, req.tierFilter) {
			continue
		}
		if item.Score < req.opts.MinScore {
			continue
		}
		filtered = append(filtered, item)
	}
	if len(req.kindFilter) == 0 {
		filtered = preferCanonicalUnits(filtered)
	}

	out := make([]domain.Memory, 0, len(filtered))
	for _, item := range filtered {
		out = append(out, item.Memory)
	}
	if len(req.kindFilter) == 0 && shouldExpandGroundedContext(req.profile) {
		var err error
		out, err = s.expandGroundedContextMemories(ctx, req.tenantID, out, req.topK)
		if err != nil {
			return nil, err
		}
	}
	if len(out) > req.topK {
		out = out[:req.topK]
	}
	return out, nil
}

// finishSearch records recalls for the returned memories and logs timings.
func (s *Service) finishSearch(ctx context.Context, req searchRequest, out []domain.Memory, timings searchStageTimings) {
	tenantID := req.tenantID
	orderedIDs := make([]string, 0, len(out))
	for _, memory := range out {
		orderedIDs = append(orderedIDs, memory.ID)
	}
	if !req.opts.DisableTouch && len(orderedIDs) > 0 {
		_ = s.repo.Touch(ctx, tenantID, orderedIDs)
	}
	s.logInfof(
		"[pali-search] tenant=%s retrieval_kind=%s embed_ms=%d bm25_ms=%d vector_ms=%d fuse_ms=%d queried_k=%d returned=%d",
		tenantID,
		req.retrievalKind,
		timings.embed.Milliseconds(),
		timings.bm25.Milliseconds(),
		timings.vector.Milliseconds(),
		timings.fuse.Milliseconds(),
		req.candidateTopK,
		len(out),
	)
	if memoryCount, err := s.tenantRepo.MemoryCount(ctx, tenantID); err == nil {
		s.logInfof("[pali-search] tenant=%s memories=%d queried_k=%d", tenantID, memoryCount, req.candidateTopK)
	} else {
		s.logDebugf("[pali-search] tenant=%s memory_count_error=%v", tenantID, err)
	}
}
