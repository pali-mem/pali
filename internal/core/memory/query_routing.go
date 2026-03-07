package memory

import (
	"regexp"
	"strings"

	"github.com/vein05/pali/internal/domain"
)

var (
	temporalPattern   = regexp.MustCompile(`\b(when|what time|date|day|month|year|before|after|first|last|earlier|later|yesterday|today|tomorrow)\b`)
	personPattern     = regexp.MustCompile(`\b(who|name|which person|whose)\b`)
	multiHopPattern   = regexp.MustCompile(`\b(before|after|first|last|both|either|then|and|while|followed|prior|previously)\b`)
	entityNamePattern = regexp.MustCompile(`\b[A-Z][a-z]+(?:\s+[A-Z][a-z]+)?\b`)
	timeTagPattern    = regexp.MustCompile(`(?i)\[time:[^\]]+\]|\b\d{4}\b|\b(?:jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\b`)

	aggregationIntentPattern           = regexp.MustCompile(`\b(what all|list|activities?|events?|things?|places?|books?|hobbies?|interests?|participated|attended|done)\b`)
	aggregationEntityDoesDoPattern     = regexp.MustCompile(`(?i)\b(?:does|did)\s+([a-z][a-z0-9'\-]*(?:\s+[a-z][a-z0-9'\-]*){0,2})\s+do\b`)
	aggregationEntityAfterVerbPattern  = regexp.MustCompile(`(?i)\b(?:does|did|do|for|of)\s+([a-z][a-z0-9'\-]*(?:\s+[a-z][a-z0-9'\-]*){0,2})\b`)
	aggregationEntityBeforeVerbPattern = regexp.MustCompile(`(?i)\b([a-z][a-z0-9'\-]*(?:\s+[a-z][a-z0-9'\-]*){0,2})\s+(?:attended|participated|did|does)\b`)
	aggregationEntityPossessivePattern = regexp.MustCompile(`(?i)\b([a-z][a-z0-9'\-]*(?:\s+[a-z][a-z0-9'\-]*){0,2})'s\s+(?:activities?|events?|hobbies?|interests?|places?|books?)\b`)
)

var aggregationEntityStopwords = map[string]struct{}{
	"a": {}, "an": {}, "the": {}, "all": {}, "any": {}, "what": {}, "which": {}, "list": {}, "show": {}, "does": {}, "did": {}, "do": {},
	"activity": {}, "activities": {}, "event": {}, "events": {}, "thing": {}, "things": {}, "place": {}, "places": {}, "book": {}, "books": {},
	"hobby": {}, "hobbies": {}, "interest": {}, "interests": {},
}

var entityHintStopwords = map[string]struct{}{
	"what": {}, "when": {}, "where": {}, "who": {}, "why": {}, "how": {}, "which": {}, "whose": {},
	"did": {}, "does": {}, "do": {}, "is": {}, "are": {}, "was": {}, "were": {}, "the": {}, "a": {}, "an": {},
}

type aggregationQuery struct {
	Entity   string
	Relation string
}

type queryPlan struct {
	Intent           string
	Confidence       float64
	Entities         []string
	Relations        []string
	TimeConstraints  []string
	RequiredEvidence string
	Temporal         bool
	Person           bool
	MultiHop         bool
	FallbackPath     []string
}

type queryProfile struct {
	Temporal bool
	Person   bool
	MultiHop bool
}

func classifyQuery(query string) queryProfile {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return queryProfile{}
	}
	return queryProfile{
		Temporal: temporalPattern.MatchString(q),
		Person:   personPattern.MatchString(q),
		MultiHop: isLikelyMultiHopQuery(q),
	}
}

func buildQueryPlan(query string, profile queryProfile) queryPlan {
	plan := queryPlan{
		Intent:           "hybrid_vector_fallback",
		Confidence:       0.55,
		Temporal:         profile.Temporal,
		Person:           profile.Person,
		MultiHop:         profile.MultiHop,
		TimeConstraints:  extractTimeHints(query),
		RequiredEvidence: "any_relevant_memory",
	}

	if route, ok := classifyAggregationQuery(query); ok {
		plan.Intent = "aggregation_lookup"
		plan.Confidence = 0.88
		plan.Entities = []string{route.Entity}
		plan.Relations = []string{route.Relation}
		plan.RequiredEvidence = "set_of_entity_relation_facts"
		plan.FallbackPath = []string{"direct_fact_lookup", "hybrid_vector_fallback"}
		return plan
	}

	if profile.MultiHop {
		plan.Intent = "graph_entity_expansion"
		plan.Confidence = 0.74
		plan.RequiredEvidence = "multi_hop_supporting_facts"
		plan.FallbackPath = []string{"direct_fact_lookup", "hybrid_vector_fallback"}
		return plan
	}

	if profile.Temporal {
		plan.Intent = "temporal_lookup"
		plan.Confidence = 0.72
		plan.RequiredEvidence = "time_anchored_fact"
		plan.FallbackPath = []string{"direct_fact_lookup", "hybrid_vector_fallback"}
		return plan
	}

	if profile.Person {
		plan.Intent = "direct_fact_lookup"
		plan.Confidence = 0.68
		plan.RequiredEvidence = "entity_attribute_fact"
		if entity, ok := classifyEntityHintQuery(query, profile); ok {
			plan.Entities = []string{normalizeEntityFactEntity(entity)}
		}
		plan.FallbackPath = []string{"hybrid_vector_fallback"}
		return plan
	}

	if entity, ok := classifyEntityHintQuery(query, profile); ok {
		plan.Entities = []string{normalizeEntityFactEntity(entity)}
	}
	plan.FallbackPath = []string{"direct_fact_lookup"}
	return plan
}

func (p queryPlan) primaryEntity() string {
	for _, entity := range p.Entities {
		entity = strings.TrimSpace(entity)
		if entity != "" {
			return entity
		}
	}
	return ""
}

func (p queryPlan) primaryRelation() string {
	for _, relation := range p.Relations {
		relation = strings.TrimSpace(relation)
		if relation != "" {
			return relation
		}
	}
	return ""
}

func isLikelyMultiHopQuery(q string) bool {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" || !multiHopPattern.MatchString(q) {
		return false
	}
	if strings.Contains(q, "both") || strings.Contains(q, "either") {
		return true
	}

	parts := searchSplitPattern.Split(q, -1)
	informativeParts := 0
	for _, part := range parts {
		if len(strings.Fields(condenseSearchQuery(part))) >= 2 {
			informativeParts++
		}
	}
	return informativeParts >= 2
}

func classifyEntityHintQuery(query string, profile queryProfile) (string, bool) {
	query = strings.TrimSpace(query)
	if query == "" || profile.Temporal || profile.MultiHop {
		return "", false
	}
	if _, ok := classifyAggregationQuery(query); ok {
		return "", false
	}
	matches := entityNamePattern.FindAllString(query, -1)
	for _, match := range matches {
		candidate := strings.ToLower(strings.TrimSpace(match))
		if candidate == "" {
			continue
		}
		if _, blocked := entityHintStopwords[candidate]; blocked {
			continue
		}
		return match, true
	}
	return "", false
}

func classifyAggregationQuery(query string) (aggregationQuery, bool) {
	q := strings.TrimSpace(query)
	if q == "" {
		return aggregationQuery{}, false
	}
	lowered := strings.ToLower(q)
	if !aggregationIntentPattern.MatchString(lowered) {
		return aggregationQuery{}, false
	}

	entity := extractAggregationEntity(q)
	relation := inferAggregationRelation(lowered)
	if entity == "" || relation == "" {
		return aggregationQuery{}, false
	}
	return aggregationQuery{
		Entity:   normalizeEntityFactEntity(entity),
		Relation: normalizeEntityFactRelation(relation),
	}, true
}

func inferAggregationRelation(loweredQuery string) string {
	switch {
	case strings.Contains(loweredQuery, "event"), strings.Contains(loweredQuery, "attended"), strings.Contains(loweredQuery, "participated"), strings.Contains(loweredQuery, "joined"):
		return "event"
	case strings.Contains(loweredQuery, "book"), strings.Contains(loweredQuery, "read"):
		return "book"
	case strings.Contains(loweredQuery, "place"), strings.Contains(loweredQuery, "visited"), strings.Contains(loweredQuery, "went"):
		return "place"
	case strings.Contains(loweredQuery, "activit"), strings.Contains(loweredQuery, "hobb"), strings.Contains(loweredQuery, "interest"), strings.Contains(loweredQuery, "thing"), strings.Contains(loweredQuery, "done"):
		return "activity"
	default:
		return ""
	}
}

func extractAggregationEntity(query string) string {
	for _, pattern := range []*regexp.Regexp{
		aggregationEntityDoesDoPattern,
		aggregationEntityPossessivePattern,
		aggregationEntityBeforeVerbPattern,
		aggregationEntityAfterVerbPattern,
	} {
		m := pattern.FindStringSubmatch(query)
		if len(m) < 2 {
			continue
		}
		entity := normalizeAggregationEntityCandidate(m[1])
		if isLikelyEntity(entity) {
			return entity
		}
	}
	return ""
}

func normalizeAggregationEntityCandidate(raw string) string {
	tokens := strings.Fields(strings.ToLower(strings.TrimSpace(raw)))
	for len(tokens) > 0 {
		if _, blocked := aggregationEntityStopwords[tokens[0]]; blocked {
			tokens = tokens[1:]
			continue
		}
		break
	}
	for len(tokens) > 0 {
		last := tokens[len(tokens)-1]
		if last == "do" || last == "does" || last == "did" {
			tokens = tokens[:len(tokens)-1]
			continue
		}
		break
	}
	return strings.Join(tokens, " ")
}

func isLikelyEntity(entity string) bool {
	entity = strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(entity)), " "))
	if entity == "" {
		return false
	}
	tokens := strings.Fields(entity)
	for _, token := range tokens {
		if _, blocked := aggregationEntityStopwords[token]; blocked {
			return false
		}
	}
	return true
}

func extractTimeHints(query string) []string {
	if strings.TrimSpace(query) == "" {
		return []string{}
	}
	matches := timeTagPattern.FindAllString(strings.ToLower(query), -1)
	if len(matches) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		hint := strings.TrimSpace(match)
		if hint == "" {
			continue
		}
		if _, ok := seen[hint]; ok {
			continue
		}
		seen[hint] = struct{}{}
		out = append(out, hint)
	}
	return out
}

func routeBoost(m domain.Memory, profile queryProfile) float64 {
	boost := 1.0

	if profile.Temporal {
		switch m.Kind {
		case domain.MemoryKindEvent:
			boost *= 1.25
		case domain.MemoryKindObservation:
			boost *= 1.15
		case domain.MemoryKindSummary:
			boost *= 1.05
		case domain.MemoryKindRawTurn:
			boost *= 0.95
		}
		if timeTagPattern.MatchString(strings.ToLower(m.Content)) {
			boost *= 1.05
		}
	}

	if profile.Person {
		switch m.Kind {
		case domain.MemoryKindObservation:
			boost *= 1.10
		case domain.MemoryKindSummary:
			boost *= 1.05
		}
		if strings.Contains(m.Content, ":") {
			boost *= 1.03
		}
	}

	if profile.MultiHop {
		switch m.Kind {
		case domain.MemoryKindSummary, domain.MemoryKindEvent:
			boost *= 1.08
		}
		if strings.Contains(strings.ToLower(m.Content), "[dialog:") {
			boost *= 1.02
		}
	}

	if boost < 0.8 {
		return 0.8
	}
	if boost > 1.35 {
		return 1.35
	}
	return boost
}
