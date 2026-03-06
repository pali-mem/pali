package memory

import (
	"regexp"
	"strings"
)

var (
	searchSplitPattern    = regexp.MustCompile(`(?i)\b(?:before|after|then|and)\b`)
	searchTokenStripper   = regexp.MustCompile(`[^a-zA-Z0-9'\- ]+`)
	searchStopwordPattern = map[string]struct{}{
		"a": {}, "an": {}, "the": {}, "what": {}, "when": {}, "where": {}, "who": {}, "why": {}, "how": {}, "which": {}, "whose": {},
		"did": {}, "does": {}, "do": {}, "is": {}, "are": {}, "was": {}, "were": {}, "to": {}, "of": {}, "in": {}, "on": {}, "at": {},
		"for": {}, "with": {}, "about": {}, "tell": {}, "me": {}, "wasn't": {}, "weren't": {}, "before": {}, "after": {}, "then": {}, "and": {},
	}
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
