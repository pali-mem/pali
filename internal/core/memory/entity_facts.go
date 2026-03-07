package memory

import (
	"context"
	"regexp"
	"slices"
	"strings"

	"github.com/pali-mem/pali/internal/domain"
)

var (
	factLeadingDatePattern  = regexp.MustCompile(`(?i)^on\s+[^,]{3,80},\s*`)
	factEntityPrefixPattern = regexp.MustCompile(`^([A-Z][A-Za-z0-9'\-]*(?:\s+[A-Z][A-Za-z0-9'\-]*){0,2})\b`)

	factActivityValuePattern = regexp.MustCompile(`(?i)\b(?:enjoys?|likes?|loves?|practices?|plays?|does|doing|did|interested in|hobbies?\s+include|chooses?|prefer(?:s)?)\s+([^.,;]+)`)
	factEventValuePattern    = regexp.MustCompile(`(?i)\b(?:attended|participated in|joined|went to|visited)\s+([^.,;]+)`)
	factBookValuePattern     = regexp.MustCompile(`(?i)\b(?:read(?:ing)?|reads?|book(?:s)?)\s+([^.,;]+)`)
	factPlaceValuePattern    = regexp.MustCompile(`(?i)\b(?:lives? in|moved to|went to|visited)\s+([^.,;]+)`)
	factPlanValuePattern     = regexp.MustCompile(`(?i)\b(?:plans? to|planning to|going to|will)\s+([^.,;]+)`)
	factLeadingVerbPattern   = regexp.MustCompile(`(?i)^(?:is|was|attended|participated in|joined|went to|visited|likes?|loves?|enjoys?|practices?|plays?|does|chooses?|prefers?)\s+`)
	factValueStopPattern     = regexp.MustCompile(`(?i)\b(?:because|since|while|although|but|and\s+(?:it|that|this)\b)\b`)
)

func inferEntityRelationValue(content string, kind domain.MemoryKind) (string, string, string) {
	content = strings.Join(strings.Fields(strings.TrimSpace(content)), " ")
	if content == "" {
		return "", "", ""
	}
	entity := inferEntityFromFact(content)
	relation := inferRelationFromFact(content, kind)
	if entity == "" || relation == "" {
		return "", "", ""
	}
	value := inferValueFromFact(content, relation, entity)
	if value == "" {
		return "", "", ""
	}
	return entity, relation, value
}

func inferEntityFromFact(content string) string {
	candidate := strings.TrimSpace(content)
	candidate = factLeadingDatePattern.ReplaceAllString(candidate, "")
	m := factEntityPrefixPattern.FindStringSubmatch(candidate)
	if len(m) < 2 {
		return ""
	}
	return strings.Join(strings.Fields(strings.TrimSpace(m[1])), " ")
}

func inferRelationFromFact(content string, kind domain.MemoryKind) string {
	l := strings.ToLower(content)
	switch {
	case kind == domain.MemoryKindEvent:
		return "event"
	case strings.Contains(l, "attended"), strings.Contains(l, "participated"), strings.Contains(l, "joined"), strings.Contains(l, "event"):
		return "event"
	case strings.Contains(l, "book"), strings.Contains(l, "read"):
		return "book"
	case strings.Contains(l, "place"), strings.Contains(l, "lives in"), strings.Contains(l, "moved to"), strings.Contains(l, "visited"):
		return "place"
	case strings.Contains(l, "plan to"), strings.Contains(l, "planning to"), strings.Contains(l, "going to"), strings.Contains(l, " will "):
		return "plan"
	case identityValuePattern.MatchString(l):
		return "identity"
	case strings.Contains(l, "works as"), strings.Contains(l, "job"), roleValuePattern.MatchString(l):
		return "role"
	case strings.Contains(l, "activit"), strings.Contains(l, "hobb"), strings.Contains(l, "interest"):
		return "activity"
	case strings.Contains(l, "enjoy"), strings.Contains(l, "like "), strings.Contains(l, "likes "), strings.Contains(l, "love "), strings.Contains(l, "practice"), strings.Contains(l, "play "), strings.Contains(l, "choose "), strings.Contains(l, "chooses "), strings.Contains(l, "prefer "):
		return "activity"
	default:
		return ""
	}
}

func inferValueFromFact(content, relation, entity string) string {
	selector := factActivityValuePattern
	switch relation {
	case "event":
		selector = factEventValuePattern
	case "book":
		selector = factBookValuePattern
	case "place":
		selector = factPlaceValuePattern
	case "plan":
		selector = factPlanValuePattern
	}

	if m := selector.FindStringSubmatch(content); len(m) >= 2 {
		return cleanupEntityFactValue(m[1])
	}

	value := strings.TrimSpace(content)
	if entity != "" {
		prefix := strings.ToLower(entity) + " "
		if strings.HasPrefix(strings.ToLower(value), prefix) {
			value = strings.TrimSpace(value[len(entity):])
		}
	}
	value = factLeadingVerbPattern.ReplaceAllString(value, "")
	return cleanupEntityFactValue(value)
}

func cleanupEntityFactValue(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" {
		return ""
	}
	if idx := factValueStopPattern.FindStringIndex(value); idx != nil {
		value = strings.TrimSpace(value[:idx[0]])
	}
	value = strings.Trim(value, " \t\r\n.,;:!?\"'")
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "to ") && len(value) > 3 {
		value = strings.TrimSpace(value[3:])
		lower = strings.ToLower(value)
	}
	if strings.HasPrefix(lower, "the ") && len(value) > 4 {
		value = strings.TrimSpace(value[4:])
	}
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func parsedFactHasEntityTriple(fact ParsedFact) bool {
	return strings.TrimSpace(fact.Entity) != "" &&
		strings.TrimSpace(fact.Relation) != "" &&
		strings.TrimSpace(fact.Value) != ""
}

func normalizeEntityFactEntity(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func normalizeEntityFactRelation(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func normalizeEntityFactValue(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func buildEntityFactRecord(memory domain.Memory, fact ParsedFact) (domain.EntityFact, bool) {
	if strings.TrimSpace(memory.ID) == "" {
		return domain.EntityFact{}, false
	}
	entity := normalizeEntityFactEntity(fact.Entity)
	relation := normalizeEntityFactRelation(fact.Relation)
	value := normalizeEntityFactValue(fact.Value)
	if entity == "" || relation == "" || value == "" {
		return domain.EntityFact{}, false
	}
	return domain.EntityFact{
		TenantID: memory.TenantID,
		Entity:   entity,
		Relation: relation,
		Value:    value,
		MemoryID: memory.ID,
	}, true
}

func normalizedRelationTupleKey(fact ParsedFact) (string, bool) {
	entity := normalizeEntityFactEntity(fact.Entity)
	relation := normalizeEntityFactRelation(fact.Relation)
	value := normalizeEntityFactValue(fact.Value)
	if entity == "" || relation == "" || value == "" {
		return "", false
	}
	return entity + "|" + relation + "|" + value, true
}

func (s *Service) findMemoryByRelationTuple(
	ctx context.Context,
	tenantID string,
	fact ParsedFact,
) (*domain.Memory, error) {
	if s == nil || s.entityRepo == nil {
		return nil, nil
	}
	entity := normalizeEntityFactEntity(fact.Entity)
	relation := normalizeEntityFactRelation(fact.Relation)
	value := normalizeEntityFactValue(fact.Value)
	if entity == "" || relation == "" || value == "" {
		return nil, nil
	}

	records, err := s.entityRepo.ListByEntityRelation(ctx, tenantID, entity, relation, 16)
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(records))
	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		if normalizeEntityFactValue(record.Value) != value {
			continue
		}
		memoryID := strings.TrimSpace(record.MemoryID)
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
		return nil, nil
	}

	memories, err := s.repo.GetByIDs(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	if len(memories) == 0 {
		return nil, nil
	}
	slices.SortFunc(memories, func(a, b domain.Memory) int {
		switch {
		case a.UpdatedAt.After(b.UpdatedAt):
			return -1
		case a.UpdatedAt.Before(b.UpdatedAt):
			return 1
		default:
			return 0
		}
	})
	memory := memories[0]
	return &memory, nil
}

func (s *Service) storeEntityFacts(ctx context.Context, facts []domain.EntityFact) error {
	if s == nil || s.entityRepo == nil || len(facts) == 0 {
		return nil
	}
	unique := make([]domain.EntityFact, 0, len(facts))
	seen := make(map[string]struct{}, len(facts))
	for _, fact := range facts {
		key := fact.TenantID + "|" + fact.Entity + "|" + fact.Relation + "|" + fact.Value + "|" + fact.MemoryID
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, fact)
	}
	if len(unique) == 0 {
		return nil
	}
	if batchRepo, ok := s.entityRepo.(domain.EntityFactBatchRepository); ok && batchRepo != nil {
		_, err := batchRepo.StoreBatch(ctx, unique)
		return err
	}
	for _, fact := range unique {
		if _, err := s.entityRepo.Store(ctx, fact); err != nil {
			return err
		}
	}
	return nil
}
