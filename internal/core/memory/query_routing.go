package memory

import (
	"regexp"
	"strings"

	"github.com/vein05/pali/internal/domain"
)

var (
	temporalPattern = regexp.MustCompile(`\b(when|what time|date|day|month|year|before|after|first|last|earlier|later|yesterday|today|tomorrow)\b`)
	personPattern   = regexp.MustCompile(`\b(who|name|which person|whose)\b`)
	multiHopPattern = regexp.MustCompile(`\b(before|after|first|last|both|either|then|and)\b`)
	timeTagPattern  = regexp.MustCompile(`(?i)\[time:[^\]]+\]|\b\d{4}\b|\b(?:jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\b`)

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

type aggregationQuery struct {
	Entity   string
	Relation string
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
		MultiHop: multiHopPattern.MatchString(q),
	}
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
