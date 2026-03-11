package memory

import (
	"regexp"
	"strings"

	"github.com/pali-mem/pali/internal/domain"
)

var (
	temporalPattern         = regexp.MustCompile(`\b(when|what time|date|day|month|year|before|after|first|last|earlier|later|yesterday|today|tomorrow)\b`)
	personPattern           = regexp.MustCompile(`\b(who|name|which person|whose)\b`)
	multiHopPattern         = regexp.MustCompile(`\b(before|after|first|last|both|either|then|and|while|followed|prior|previously)\b`)
	entityNamePattern       = regexp.MustCompile(`\b[A-Z][a-z]+(?:\s+[A-Z][a-z]+)?\b`)
	timeTagPattern          = regexp.MustCompile(`(?i)\[time:[^\]]+\]|\b\d{4}\b|\b(?:jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\b`)
	booleanQueryPattern     = regexp.MustCompile(`^(?:is|are|was|were|do|does|did|can|could|would|should|has|have|had|will)\b`)
	quoteQueryPattern       = regexp.MustCompile(`\b(?:quote|quoted|say|said|poster|posters|sign|signs|slogan|written|wrote|exact words?)\b`)
	listQueryPattern        = regexp.MustCompile(`\b(?:what all|list|which activities|which events|which places|which books|which hobbies|which interests)\b`)
	locationQueryPattern    = regexp.MustCompile(`\b(?:where|what country|what city|what town|what state|which person|who|whose)\b`)
	durationQueryPattern    = regexp.MustCompile(`\b(?:how long|duration|for how many|for how long)\b`)
	relativeTemporalPattern = regexp.MustCompile(`\b(?:before|after|earlier|later|last|next|yesterday|today|tomorrow|ago|week before|month before|year before)\b`)
	openDomainBinaryPattern = regexp.MustCompile(`\b(?:would|could|likely|probably|might|consider|interested|leaning|support|value|belief)\b`)
	openDomainLabelPattern  = regexp.MustCompile(`\b(?:political|leaning|religious|religion|faith|spiritual|financial status|class|personality|trait|traits)\b`)
	openDomainChoicePattern = regexp.MustCompile(`\b(?:or|rather than|instead of|between)\b`)

	aggregationIntentPattern           = regexp.MustCompile(`(?i)\b(what all|list|activities?|events?|places?|books?|hobbies?|interests?)\b`)
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
	AnswerType       string
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
	answerType := classifyAnswerType(query, profile)
	plan := queryPlan{
		Intent:           "hybrid_vector_fallback",
		Confidence:       0.55,
		AnswerType:       answerType,
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
		plan.Entities = extractMultiHopRouteEntities(query)
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

	if strings.HasPrefix(answerType, "open_domain_") {
		plan.Intent = "profile_summary_lookup"
		plan.Confidence = 0.66
		plan.RequiredEvidence = "profile_summary_or_supported_facts"
		if entity, ok := classifyEntityHintQuery(query, profile); ok {
			plan.Entities = []string{normalizeEntityFactEntity(entity)}
		}
		plan.FallbackPath = []string{"direct_fact_lookup", "hybrid_vector_fallback"}
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

func extractMultiHopRouteEntities(query string) []string {
	matches := entityNamePattern.FindAllString(query, -1)
	if len(matches) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		entity := normalizeEntityFactEntity(match)
		if entity == "" {
			continue
		}
		if _, ok := seen[entity]; ok {
			continue
		}
		seen[entity] = struct{}{}
		out = append(out, entity)
	}
	return out
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
	if !isExplicitAggregationSetQuery(lowered) {
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

func isExplicitAggregationSetQuery(loweredQuery string) bool {
	if strings.TrimSpace(loweredQuery) == "" {
		return false
	}
	// Keep graph-route short-circuit for explicit "list/set" requests only.
	// This avoids hijacking factual/temporal queries that should stay on hybrid retrieval.
	if strings.Contains(loweredQuery, "what all") || strings.Contains(loweredQuery, "list") {
		return true
	}
	if strings.Contains(loweredQuery, "all activities") ||
		strings.Contains(loweredQuery, "all events") ||
		strings.Contains(loweredQuery, "all places") ||
		strings.Contains(loweredQuery, "all books") ||
		strings.Contains(loweredQuery, "all hobbies") ||
		strings.Contains(loweredQuery, "all interests") {
		return true
	}
	if strings.Contains(loweredQuery, "activities does") ||
		strings.Contains(loweredQuery, "events does") ||
		strings.Contains(loweredQuery, "places does") ||
		strings.Contains(loweredQuery, "books does") ||
		strings.Contains(loweredQuery, "hobbies does") ||
		strings.Contains(loweredQuery, "interests does") {
		return true
	}
	return false
}

func inferAggregationRelation(loweredQuery string) string {
	switch {
	case strings.Contains(loweredQuery, "event"), strings.Contains(loweredQuery, "attended"), strings.Contains(loweredQuery, "participated"), strings.Contains(loweredQuery, "joined"):
		return "event"
	case strings.Contains(loweredQuery, "book"), strings.Contains(loweredQuery, "read"):
		return "book"
	case strings.Contains(loweredQuery, "place"), strings.Contains(loweredQuery, "visited"), strings.Contains(loweredQuery, "went"):
		return "place"
	case strings.Contains(loweredQuery, "favorite"), strings.Contains(loweredQuery, "prefer"), strings.Contains(loweredQuery, "likes"), strings.Contains(loweredQuery, "enjoys"):
		return "preference"
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

func classifyAnswerType(query string, profile queryProfile) string {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return "single_fact"
	}
	if profile.Temporal {
		switch {
		case durationQueryPattern.MatchString(q):
			return "temporal_duration"
		case relativeTemporalPattern.MatchString(q):
			return "temporal_relative"
		default:
			return "temporal_absolute"
		}
	}
	if openDomainLabelPattern.MatchString(q) {
		return "open_domain_label"
	}
	if openDomainChoicePattern.MatchString(q) && openDomainBinaryPattern.MatchString(q) {
		return "open_domain_choice"
	}
	if booleanQueryPattern.MatchString(q) && openDomainBinaryPattern.MatchString(q) {
		return "open_domain_binary"
	}
	if quoteQueryPattern.MatchString(q) {
		return "single_fact_quote"
	}
	if listQueryPattern.MatchString(q) {
		return "single_fact_list"
	}
	if booleanQueryPattern.MatchString(q) {
		return "single_fact_boolean"
	}
	if locationQueryPattern.MatchString(q) {
		return "single_fact_location_or_person"
	}
	if openDomainBinaryPattern.MatchString(q) {
		return "open_domain_binary"
	}
	return "single_fact"
}

func routeBoost(m domain.Memory, profile queryProfile, plan queryPlan, behavior RetrievalBehaviorOptions) float64 {
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

	if behavior.AnswerTypeRoutingEnabled {
		switch plan.AnswerType {
		case "single_fact_boolean", "single_fact_quote":
			switch m.Kind {
			case domain.MemoryKindRawTurn:
				boost *= 1.15
			case domain.MemoryKindEvent:
				boost *= 1.12
			case domain.MemoryKindSummary:
				boost *= 0.93
			}
		case "single_fact_list":
			switch m.Kind {
			case domain.MemoryKindSummary:
				boost *= 1.14
			case domain.MemoryKindObservation:
				boost *= 1.10
			case domain.MemoryKindEvent:
				boost *= 1.06
			case domain.MemoryKindRawTurn:
				boost *= 0.92
			}
		case "single_fact_location_or_person":
			switch m.Kind {
			case domain.MemoryKindObservation:
				boost *= 1.14
			case domain.MemoryKindEvent:
				boost *= 1.09
			case domain.MemoryKindSummary:
				boost *= 0.95
			case domain.MemoryKindRawTurn:
				boost *= 0.94
			}
		case "temporal_absolute", "temporal_relative", "temporal_duration":
			switch m.Kind {
			case domain.MemoryKindEvent:
				boost *= 1.15
			case domain.MemoryKindRawTurn:
				boost *= 1.08
			case domain.MemoryKindSummary:
				boost *= 0.94
			}
			if strings.TrimSpace(m.AnswerMetadata.TemporalAnchor) != "" ||
				strings.TrimSpace(m.AnswerMetadata.RelativeTimePhrase) != "" ||
				strings.TrimSpace(m.AnswerMetadata.ResolvedTimeStart) != "" {
				boost *= 1.08
			}
		case "open_domain_binary", "open_domain_choice", "open_domain_label":
			switch m.Kind {
			case domain.MemoryKindSummary:
				boost *= 1.18
			case domain.MemoryKindObservation:
				boost *= 1.06
			case domain.MemoryKindRawTurn:
				boost *= 0.94
			}
			if behavior.ProfileSupportLinksEnabled && len(m.AnswerMetadata.SupportLines) > 0 {
				boost *= 1.05
			}
		}
	}

	switch plan.AnswerType {
	case "single_fact_quote":
		if strings.TrimSpace(m.AnswerMetadata.AnswerKind) == "quote" {
			boost *= 1.12
		}
	case "single_fact_boolean":
		if strings.TrimSpace(m.AnswerMetadata.AnswerKind) == "boolean" {
			boost *= 1.10
		}
	case "single_fact_location_or_person":
		if strings.TrimSpace(m.AnswerMetadata.AnswerKind) == "entity" {
			boost *= 1.08
		}
	case "open_domain_binary", "open_domain_choice", "open_domain_label":
		if len(m.AnswerMetadata.SupportLines) > 0 || len(m.AnswerMetadata.SupportMemoryIDs) > 0 {
			boost *= 1.04
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
