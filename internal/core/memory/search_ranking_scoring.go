package memory

import (
	"strings"
	"time"

	"github.com/pali-mem/pali/internal/core/scoring"
	"github.com/pali-mem/pali/internal/domain"
)

func (s *Service) rerankScoredCandidates(items []scoredMemory, query string, topK int) []scoredMemory {
	opts := s.rerank
	if !opts.Enabled || len(items) < 2 {
		return items
	}

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
	return append(sortScoredByScore(head), tail...)
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
		idf[token] = mathLog1pSafe((n-df+0.5)/(df+0.5)) + 1
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

type retrievalSignalWeightSet struct {
	DenseScore   float64
	DenseRank    float64
	LexicalScore float64
	LexicalRank  float64
	RRF          float64
	Entity       float64
	Route        float64
}

func retrievalSignalWeights(query string, profile queryProfile, signal candidateSignal) retrievalSignalWeightSet {
	weights := retrievalSignalWeightSet{
		DenseScore:   0.34,
		DenseRank:    0.08,
		LexicalScore: 0.12,
		LexicalRank:  0.20,
		RRF:          0.10,
		Entity:       0.10,
		Route:        0.06,
	}
	_ = query
	_ = profile
	if signal.DenseScore == 0 && signal.DenseRank == 0 {
		weights.DenseScore = 0
		weights.DenseRank = 0
	}
	return retrievalSignalWeightSet{
		DenseScore:   max(0.0, weights.DenseScore),
		DenseRank:    max(0.0, weights.DenseRank),
		LexicalScore: max(0.0, weights.LexicalScore),
		LexicalRank:  max(0.0, weights.LexicalRank),
		RRF:          max(0.0, weights.RRF),
		Entity:       max(0.0, weights.Entity),
		Route:        max(0.0, weights.Route),
	}
}

func rankMemories(
	memories []domain.Memory,
	signalByID map[string]candidateSignal,
	candidateLimit int,
	query string,
	profile queryProfile,
	plan queryPlan,
	ranking RankingOptions,
	behavior RetrievalBehaviorOptions,
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
		routeFit := normalizedRouteBoost(routeBoost(m, profile, plan, behavior))
		entitySlotHit := entityRelationSignal(plan, m.Content)
		signalWeights := retrievalSignalWeights(query, profile, signal)
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
			[]float64{
				signalWeights.DenseScore,
				signalWeights.LexicalScore,
				signalWeights.DenseRank,
				signalWeights.LexicalRank,
				signalWeights.RRF,
				signalWeights.Entity,
				signalWeights.Route,
			},
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
			route := normalizedRouteBoost(routeBoost(m, profile, plan, behavior))
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
				total = applySingleHopPrecisionBoost(total, overlap, dependency, m, hasNonRawKinds, plan, behavior)
			}
			if !profile.Temporal && !profile.MultiHop {
				total = applyLowEvidencePenalty(total, rawRelevance[i], overlap, signalByID[m.ID].RRFScore)
			}
			if profile.Temporal || profile.Person || profile.MultiHop {
				if total == 0 {
					total = 1
				}
				total = clamp01(total * routeBoost(m, profile, plan, behavior))
			}
		}
		scored = append(scored, scoredMemory{Memory: m, Score: total})
	}
	return scored
}

// applyLowEvidencePenalty demotes items whose retrieval signal is weak on
// ALL axes. The rrfScore guard prevents demoting items that BM25 + vector
// retrieval both ranked highly - those are paraphrase/synonym matches where
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

func applySingleHopPrecisionBoost(total, overlap, dependency float64, memory domain.Memory, hasNonRawKinds bool, plan queryPlan, behavior RetrievalBehaviorOptions) float64 {
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
		if strings.HasPrefix(plan.AnswerType, "open_domain_") {
			kindMultiplier *= 1.10
		}
	case domain.MemoryKindRawTurn:
		if hasNonRawKinds {
			kindMultiplier *= 0.86
			if plan.AnswerType == "single_fact_boolean" || plan.AnswerType == "single_fact_quote" {
				kindMultiplier *= 1.18
			}
			if conversationalNoisePattern.MatchString(strings.ToLower(memory.Content)) {
				kindMultiplier *= 0.80
			}
		}
	}
	if behavior.AnswerTypeRoutingEnabled {
		switch plan.AnswerType {
		case "single_fact_quote":
			if memory.AnswerMetadata.AnswerKind == "quote" || quoteQueryPattern.MatchString(strings.ToLower(memory.Content)) {
				kindMultiplier *= 1.14
			}
		case "single_fact_boolean":
			if memory.AnswerMetadata.AnswerKind == "boolean" {
				kindMultiplier *= 1.12
			}
		case "single_fact_location_or_person":
			if memory.AnswerMetadata.AnswerKind == "entity" {
				kindMultiplier *= 1.10
			}
		case "open_domain_binary", "open_domain_choice", "open_domain_label":
			if behavior.ProfileSupportLinksEnabled && len(memory.AnswerMetadata.SupportLines) > 0 {
				kindMultiplier *= 1.08
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

func (s *Service) applyEarlyRankRerank(
	scored []scoredMemory,
	query string,
	plan queryPlan,
	topK int,
	profile queryProfile,
) []scoredMemory {
	if len(scored) <= 1 {
		return scored
	}
	window := s.earlyRankRerankWindow(len(scored), topK, profile, plan)
	if window <= 1 {
		return scored
	}
	queryTokens := normalizedRankingTokens(query)
	head := append([]scoredMemory{}, scored[:window]...)
	for i := range head {
		head[i].Score = clamp01((0.72 * head[i].Score) + (0.28 * earlyRankSignal(query, queryTokens, plan, head[i].Memory, s.retrieval)))
	}
	return append(sortScoredByScore(head), scored[window:]...)
}

func (s *Service) earlyRankRerankWindow(scoredLen, topK int, profile queryProfile, plan queryPlan) int {
	if scoredLen <= 1 {
		return scoredLen
	}
	tuning := normalizeRetrievalSearchTuningOptions(s.retrieval.SearchTuning)
	window := tuning.EarlyRerankBaseWindow
	if topK > 0 && topK*4 > window {
		window = topK * 4
	}
	if profile.Temporal || profile.MultiHop {
		window += topK
	}
	if plan.Confidence < tuning.AdaptiveQueryPlanConfidenceThreshold {
		window += max(8, topK*2)
	}
	if window > tuning.EarlyRerankMaxWindow {
		window = tuning.EarlyRerankMaxWindow
	}
	if window > scoredLen {
		window = scoredLen
	}
	if window < 2 {
		return 2
	}
	return window
}

func earlyRankSignal(query string, queryTokens map[string]struct{}, plan queryPlan, memory domain.Memory, behavior RetrievalBehaviorOptions) float64 {
	docTokens := normalizedRankingTokens(memory.Content)
	score := queryOverlapScore(queryTokens, docTokens)
	lowered := strings.ToLower(memory.Content)
	if plan.primaryEntity() != "" && strings.Contains(lowered, strings.ToLower(plan.primaryEntity())) {
		score += 0.10
	}
	if len(plan.TimeConstraints) > 0 {
		for _, hint := range plan.TimeConstraints {
			if hint != "" && strings.Contains(lowered, strings.ToLower(hint)) {
				score += 0.10
				break
			}
		}
	}
	switch plan.AnswerType {
	case "single_fact_boolean":
		if memory.AnswerMetadata.AnswerKind == "boolean" {
			score += 0.18
		}
		if memory.Kind == domain.MemoryKindRawTurn || memory.Kind == domain.MemoryKindEvent {
			score += 0.10
		}
	case "single_fact_quote":
		if memory.AnswerMetadata.AnswerKind == "quote" || quoteQueryPattern.MatchString(lowered) {
			score += 0.22
		}
		if memory.Kind == domain.MemoryKindSummary {
			score -= 0.08
		}
	case "single_fact_list":
		if memory.Kind == domain.MemoryKindSummary || memory.Kind == domain.MemoryKindObservation || memory.Kind == domain.MemoryKindEvent {
			score += 0.12
		}
	case "single_fact_location_or_person":
		if memory.AnswerMetadata.AnswerKind == "entity" {
			score += 0.15
		}
		if memory.Kind == domain.MemoryKindSummary {
			score -= 0.06
		}
	case "temporal_absolute", "temporal_relative", "temporal_duration":
		if memory.Kind == domain.MemoryKindEvent || memory.Kind == domain.MemoryKindRawTurn {
			score += 0.12
		}
		if strings.TrimSpace(memory.AnswerMetadata.TemporalAnchor) != "" ||
			strings.TrimSpace(memory.AnswerMetadata.RelativeTimePhrase) != "" ||
			strings.TrimSpace(memory.AnswerMetadata.ResolvedTimeStart) != "" {
			score += 0.18
		}
		if timeTagPattern.MatchString(lowered) {
			score += 0.08
		}
	case "open_domain_binary", "open_domain_choice", "open_domain_label":
		if memory.Kind == domain.MemoryKindSummary {
			score += 0.18
		}
		if behavior.ProfileSupportLinksEnabled && (len(memory.AnswerMetadata.SupportLines) > 0 || len(memory.AnswerMetadata.SupportMemoryIDs) > 0) {
			score += 0.15
		}
	}
	if behavior.ProfileSupportLinksEnabled && len(memory.AnswerMetadata.SupportLines) > 0 {
		score += 0.03
	}
	return clamp01(score)
}
