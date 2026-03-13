package memory

import (
	"regexp"
	"slices"
	"strings"

	"github.com/pali-mem/pali/internal/domain"
)

var (
	searchSplitPattern    = regexp.MustCompile(`(?i)\b(?:before|after|then|and|while)\b`)
	searchTokenStripper   = regexp.MustCompile(`[^a-zA-Z0-9'\- ]+`)
	searchStopwordPattern = map[string]struct{}{
		"a": {}, "an": {}, "the": {}, "what": {}, "when": {}, "where": {}, "who": {}, "why": {}, "how": {}, "which": {}, "whose": {},
		"did": {}, "does": {}, "do": {}, "is": {}, "are": {}, "was": {}, "were": {}, "to": {}, "of": {}, "in": {}, "on": {}, "at": {},
		"for": {}, "with": {}, "about": {}, "tell": {}, "me": {}, "wasn't": {}, "weren't": {}, "before": {}, "after": {}, "then": {}, "and": {},
	}
)

const (
	multiHopExpansionMinQueryTokens        = 3
	multiHopExpansionSkipStrongFirstHop    = 0.90
	multiHopExpansionMinCandidateScore     = 0.18
	multiHopExpansionNovelTokenMinLength   = 3
	multiHopExpansionMinNovelTokenCount    = 2
	multiHopExpansionMaxNovelTokenPerQuery = 4
	prfExpansionMinCandidates              = 6
	prfExpansionMaxFeedbackDocs            = 8
	prfExpansionMinBestScore               = 0.20
	prfExpansionSkipStrongFirstPass        = 0.75
	prfExpansionMinDocFrequency            = 3
	prfExpansionMaxExtraTokens             = 2
)

func buildSearchQueries(query string, profile queryProfile) []string {
	queries := make([]string, 0, 6)
	seen := make(map[string]struct{}, 6)
	add := func(text string) {
		text = normalizeFactContent(text)
		if text == "" {
			return
		}
		key := strings.ToLower(text)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		queries = append(queries, text)
	}

	add(query)
	add(condenseSearchQuery(query))
	for _, rewrite := range buildIntentAwareRewrites(query) {
		add(rewrite)
		add(condenseSearchQuery(rewrite))
	}

	if route, ok := classifyAggregationQuery(query); ok {
		add(route.Entity + " " + route.Relation)
	}

	if profile.Temporal || profile.MultiHop {
		parts := searchSplitPattern.Split(query, -1)
		for _, part := range parts {
			add(condenseSearchQuery(part))
		}
	}

	return queries
}

func buildAdaptiveSearchQueries(
	query string,
	profile queryProfile,
	plan queryPlan,
	lexical []lexicalCandidate,
	tuning RetrievalSearchTuningOptions,
) []string {
	if !tuning.AdaptiveQueryExpansionEnabled || tuning.AdaptiveQueryMaxExtraQueries <= 0 {
		return []string{}
	}
	bestLexical := bestLexicalCandidateScore(lexical)
	if len(lexical) > 0 &&
		plan.Confidence >= tuning.AdaptiveQueryPlanConfidenceThreshold &&
		bestLexical >= tuning.AdaptiveQueryWeakLexicalThreshold {
		return []string{}
	}
	maxExtra := tuning.AdaptiveQueryMaxExtraQueries
	out := make([]string, 0, maxExtra)
	out = appendUniqueSearchQueries(out, buildPseudoRelevanceQueries(query, profile, lexical, maxExtra))
	remaining := maxExtra - len(out)
	if remaining > 0 {
		out = appendUniqueSearchQueries(out, buildLowConfidenceBackoffQueries(query, profile, remaining))
	}
	if len(out) > maxExtra {
		out = out[:maxExtra]
	}
	return out
}

func buildLowConfidenceBackoffQueries(query string, profile queryProfile, maxQueries int) []string {
	if maxQueries <= 0 {
		return []string{}
	}
	out := make([]string, 0, maxQueries)
	add := func(text string) {
		if len(out) >= maxQueries {
			return
		}
		text = condenseSearchQuery(text)
		if text == "" {
			return
		}
		out = appendUniqueSearchQueries(out, []string{text})
	}

	add(query)
	if entity, ok := classifyEntityHintQuery(query, profile); ok {
		add(entity)
	}
	if route, ok := classifyAggregationQuery(query); ok {
		add(route.Entity + " " + route.Relation)
	}
	if profile.Temporal {
		add(query + " date time")
	}
	if profile.MultiHop {
		for _, part := range searchSplitPattern.Split(query, -1) {
			add(part)
			if len(out) >= maxQueries {
				break
			}
		}
	}
	return out
}

func bestLexicalCandidateScore(candidates []lexicalCandidate) float64 {
	best := 0.0
	for _, candidate := range candidates {
		if candidate.Score > best {
			best = candidate.Score
		}
	}
	return best
}

func buildIntentAwareRewrites(query string) []string {
	lowered := strings.ToLower(strings.TrimSpace(query))
	if lowered == "" {
		return []string{}
	}
	out := make([]string, 0, 4)
	if strings.HasPrefix(lowered, "why ") || strings.Contains(lowered, " reason ") {
		out = append(out, query+" motivation because")
	}
	if strings.Contains(lowered, "symbolize") || strings.Contains(lowered, "symbolise") || strings.Contains(lowered, "meaning") {
		out = append(out, query+" reminder represents means")
	}
	if strings.Contains(lowered, "when ") || strings.Contains(lowered, " date ") || strings.Contains(lowered, "time ") {
		out = append(out, query+" date time year month")
	}
	if strings.Contains(lowered, "who ") || strings.Contains(lowered, "whose ") {
		out = append(out, query+" person identity name")
	}
	return out
}

func condenseSearchQuery(query string) string {
	query = searchTokenStripper.ReplaceAllString(strings.ToLower(strings.TrimSpace(query)), " ")
	if query == "" {
		return ""
	}
	tokens := strings.Fields(query)
	out := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if _, skip := searchStopwordPattern[token]; skip {
			continue
		}
		out = append(out, token)
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, " ")
}

func buildIterativeMultiHopQueries(query string, lexical []lexicalCandidate, maxQueries int) []string {
	if maxQueries <= 0 || len(lexical) == 0 {
		return []string{}
	}
	if !shouldExpandMultiHopQueries(query, lexical) {
		return []string{}
	}
	baseTokens := normalizedRankingTokens(query)
	// Highest-confidence lexical hits are used as first-hop evidence.
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
	bestScore := 0.0
	for _, candidate := range ordered {
		if candidate.Memory.Kind == "" || candidate.Memory.Kind == domain.MemoryKindRawTurn {
			continue
		}
		if candidate.Score > bestScore {
			bestScore = candidate.Score
		}
	}
	minCandidateScore := maxFloat64(multiHopExpansionMinCandidateScore, bestScore*0.80)

	queries := make([]string, 0, maxQueries)
	seen := make(map[string]struct{}, maxQueries)
	for _, candidate := range ordered {
		if len(queries) >= maxQueries {
			break
		}
		if candidate.Score < minCandidateScore {
			continue
		}
		if candidate.Memory.Kind == "" || candidate.Memory.Kind == domain.MemoryKindRawTurn {
			continue
		}
		extraTokens := extractMultiHopNovelTokens(baseTokens, candidate.Memory.Content)
		if len(extraTokens) < multiHopExpansionMinNovelTokenCount {
			continue
		}
		if len(extraTokens) > multiHopExpansionMaxNovelTokenPerQuery {
			extraTokens = extraTokens[:multiHopExpansionMaxNovelTokenPerQuery]
		}
		refined := condenseSearchQuery(query + " " + strings.Join(extraTokens, " "))
		if strings.TrimSpace(refined) == "" {
			continue
		}
		key := strings.ToLower(refined)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		queries = append(queries, refined)
	}
	return queries
}

func buildPseudoRelevanceQueries(query string, profile queryProfile, lexical []lexicalCandidate, maxQueries int) []string {
	if maxQueries <= 0 || len(lexical) < prfExpansionMinCandidates {
		return []string{}
	}
	if profile.MultiHop {
		return []string{}
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
	best := ordered[0].Score
	if best < prfExpansionMinBestScore || best >= prfExpansionSkipStrongFirstPass {
		return []string{}
	}
	baseTokens := normalizedRankingTokens(query)
	if len(baseTokens) == 0 {
		return []string{}
	}
	type tokenStats struct {
		token string
		tf    int
		df    int
		score float64
	}
	statsByToken := make(map[string]*tokenStats, 32)
	feedbackDocs := 0
	for _, candidate := range ordered {
		if feedbackDocs >= prfExpansionMaxFeedbackDocs {
			break
		}
		feedbackDocs++
		docTokens := normalizedRankingTokenList(candidate.Memory.Content + "\n" + candidate.Memory.QueryViewText)
		seenInDoc := make(map[string]struct{}, len(docTokens))
		for _, token := range docTokens {
			if len(token) < 3 {
				continue
			}
			if _, exists := baseTokens[token]; exists {
				continue
			}
			if _, stop := searchStopwordPattern[token]; stop {
				continue
			}
			if isAllDigits(token) {
				continue
			}
			entry, ok := statsByToken[token]
			if !ok {
				entry = &tokenStats{token: token}
				statsByToken[token] = entry
			}
			entry.tf++
			if _, seen := seenInDoc[token]; !seen {
				entry.df++
				seenInDoc[token] = struct{}{}
			}
		}
	}
	if len(statsByToken) == 0 {
		return []string{}
	}
	scored := make([]tokenStats, 0, len(statsByToken))
	for _, stat := range statsByToken {
		if stat.df < prfExpansionMinDocFrequency {
			continue
		}
		// Prefer tokens that recur across feedback docs (df) while still
		// using term frequency as a tiebreaker.
		stat.score = (0.75 * float64(stat.df)) + (0.25 * float64(stat.tf))
		scored = append(scored, *stat)
	}
	if len(scored) == 0 {
		return []string{}
	}
	slices.SortFunc(scored, func(a, b tokenStats) int {
		switch {
		case a.score > b.score:
			return -1
		case a.score < b.score:
			return 1
		default:
			return strings.Compare(a.token, b.token)
		}
	})
	extraTokens := make([]string, 0, prfExpansionMaxExtraTokens)
	for _, token := range scored {
		if len(extraTokens) >= prfExpansionMaxExtraTokens {
			break
		}
		extraTokens = append(extraTokens, token.token)
	}
	if len(extraTokens) == 0 {
		return []string{}
	}
	base := condenseSearchQuery(query)
	refined := condenseSearchQuery(query + " " + strings.Join(extraTokens, " "))
	if refined == "" || strings.EqualFold(refined, base) {
		return []string{}
	}
	return []string{refined}
}

func shouldExpandMultiHopQueries(query string, lexical []lexicalCandidate) bool {
	if len(normalizedRankingTokens(query)) < multiHopExpansionMinQueryTokens {
		return false
	}
	bestScore := 0.0
	for _, candidate := range lexical {
		if candidate.Memory.Kind == "" || candidate.Memory.Kind == domain.MemoryKindRawTurn {
			continue
		}
		if candidate.Score > bestScore {
			bestScore = candidate.Score
		}
	}
	// Strong first-hop lexical evidence is usually precise enough; skip expansion to avoid drift.
	return bestScore > 0 && bestScore < multiHopExpansionSkipStrongFirstHop
}

func extractMultiHopNovelTokens(baseTokens map[string]struct{}, content string) []string {
	if len(baseTokens) == 0 {
		return []string{}
	}
	novel := make([]string, 0, 8)
	for token := range normalizedRankingTokens(content) {
		if len(token) < multiHopExpansionNovelTokenMinLength {
			continue
		}
		if _, exists := baseTokens[token]; exists {
			continue
		}
		if _, stop := searchStopwordPattern[token]; stop {
			continue
		}
		if isAllDigits(token) {
			continue
		}
		novel = append(novel, token)
	}
	slices.Sort(novel)
	return novel
}

func isAllDigits(token string) bool {
	if token == "" {
		return false
	}
	for _, ch := range token {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func maxFloat64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
