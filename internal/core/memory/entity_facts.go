package memory

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/pali-mem/pali/internal/domain"
)

var (
	factLeadingDatePattern  = regexp.MustCompile(`(?i)^on\s+[^,]{3,80},\s*`)
	factEntityPrefixPattern = regexp.MustCompile(`^([A-Z][A-Za-z0-9'\-]*(?:\s+[A-Z][A-Za-z0-9'\-]*){0,2})\b`)
	factFirstPersonPattern  = regexp.MustCompile(`(?i)^(?:i|i'm|i am|i've|i have|i'd|i will|i'll|my|mine)\b`)

	factActivityValuePattern = regexp.MustCompile(`(?i)\b(?:enjoys?|likes?|loves?|practices?|plays?|uses?|using|does|doing|did|interested in|hobbies?\s+include|chooses?|prefer(?:s)?)\s+([^.,;]+)`)
	factEventValuePattern    = regexp.MustCompile(`(?i)\b(?:attended|participated in|joined|went to|visited)\s+([^.,;]+)`)
	factBookValuePattern     = regexp.MustCompile(`(?i)\b(?:read(?:ing)?|reads?|book(?:s)?)\s+([^.,;]+)`)
	factPlaceValuePattern    = regexp.MustCompile(`(?i)\b(?:lives? in|moved to|went to|visited)\s+([^.,;]+)`)
	factPlanValuePattern     = regexp.MustCompile(`(?i)\b(?:plans? to|planning to|going to|will)\s+([^.,;]+)`)
	factRelationshipPattern  = regexp.MustCompile(`(?i)\b(?:husband|wife|partner|boyfriend|girlfriend|spouse|friend|friends|mentor|mentors|parent|mother|father|son|daughter|child|children|kids?)\b`)
	factStatusPattern        = regexp.MustCompile(`(?i)\b(?:single|married|divorced|engaged|dating|widowed|separated|in a relationship)\b`)
	factPreferencePattern    = regexp.MustCompile(`(?i)\b(?:favorite|prefer(?:s)?|likes?|loves?|enjoys?)\b`)
	factBeliefPattern        = regexp.MustCompile(`(?i)\b(?:believes?|believed|thinks?|thought|values?|valued|cares? about|stands? for)\b`)
	factTraitPattern         = regexp.MustCompile(`(?i)\b(?:personality|trait|traits|tendency|tendencies|character|characteristic|known for)\b`)
	factLeadingVerbPattern   = regexp.MustCompile(`(?i)^(?:is|was|attended|participated in|joined|went to|visited|likes?|loves?|enjoys?|practices?|plays?|uses?|using|does|chooses?|prefers?)\s+`)
	factValueStopPattern     = regexp.MustCompile(`(?i)\b(?:because|since|while|although|but|and\s+(?:it|that|this)\b)\b`)

	entityFactCanonicalRelations = map[string]struct{}{
		"activity":            {},
		"attribute":           {},
		"belief":              {},
		"book":                {},
		"event":               {},
		"goal":                {},
		"identity":            {},
		"place":               {},
		"plan":                {},
		"preference":          {},
		"relationship":        {},
		"relationship status": {},
		"role":                {},
		"trait":               {},
		"value":               {},
	}
)

var entityFactRelationFamilies = map[string][]string{
	"activity":            {"activity", "preference"},
	"attribute":           {"attribute", "trait", "value", "belief"},
	"belief":              {"belief", "value", "trait", "attribute"},
	"book":                {"book"},
	"event":               {"event"},
	"goal":                {"goal", "plan"},
	"identity":            {"identity", "role", "trait", "attribute"},
	"place":               {"place"},
	"plan":                {"plan", "goal"},
	"preference":          {"preference", "activity"},
	"relationship":        {"relationship", "relationship status"},
	"relationship status": {"relationship status", "relationship"},
	"role":                {"role", "identity"},
	"trait":               {"trait", "attribute", "belief", "value"},
	"value":               {"value", "belief", "trait", "attribute"},
}

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
	if factFirstPersonPattern.MatchString(candidate) {
		return "user"
	}
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
	case factStatusPattern.MatchString(l):
		return "relationship status"
	case factRelationshipPattern.MatchString(l):
		return "relationship"
	case strings.Contains(l, "plan to"), strings.Contains(l, "planning to"), strings.Contains(l, "going to"), strings.Contains(l, " will "):
		return "plan"
	case strings.Contains(l, "goal"), strings.Contains(l, "dream"), strings.Contains(l, "aim"), strings.Contains(l, "aspire"):
		return "goal"
	case factPreferencePattern.MatchString(l):
		return "preference"
	case factBeliefPattern.MatchString(l):
		return "belief"
	case factTraitPattern.MatchString(l):
		return "trait"
	case identityValuePattern.MatchString(l):
		return "identity"
	case strings.Contains(l, "works as"), strings.Contains(l, "job"), roleValuePattern.MatchString(l):
		return "role"
	case strings.Contains(l, "activit"), strings.Contains(l, "hobb"), strings.Contains(l, "interest"):
		return "activity"
	case strings.Contains(l, "enjoy"), strings.Contains(l, "like "), strings.Contains(l, "likes "), strings.Contains(l, "love "), strings.Contains(l, "practice"), strings.Contains(l, "play "), strings.Contains(l, "choose "), strings.Contains(l, "chooses "), strings.Contains(l, "prefer "), strings.Contains(l, "use "), strings.Contains(l, "uses "), strings.Contains(l, "using "):
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
	relation := strings.TrimSpace(strings.ToLower(value))
	relation = strings.ReplaceAll(relation, "_", " ")
	relation = strings.ReplaceAll(relation, "-", " ")
	relation = strings.Join(strings.Fields(relation), " ")
	if relation == "" {
		return ""
	}
	if _, ok := entityFactCanonicalRelations[relation]; ok {
		return relation
	}
	switch {
	case relationHasAny(relation,
		"event", "attend", "attendance", "joined", "join", "participat", "met ",
		"encounter", "happen", "occur", "celebrat", "conference", "meetup",
		"event date", "event type", "time", "created", "received"):
		return "event"
	case relationHasAny(relation, "book", "read", "novel", "author", "story"):
		return "book"
	case relationHasAny(relation,
		"place", "location", "city", "country", "moved", "move", "lives",
		"live", "reside", "visited", "visit", "travel"):
		return "place"
	case relationHasAny(relation,
		"relationship status", "marital", "single", "married", "dating",
		"divorc", "engaged", "widowed", "separated"):
		return "relationship status"
	case relationHasAny(relation,
		"relationship", "family", "friend", "partner", "spouse", "wife",
		"husband", "boyfriend", "girlfriend", "mentor", "child", "parent"):
		return "relationship"
	case relationHasAny(relation, "role", "job", "profession", "career", "occupation", "works as"):
		return "role"
	case relationHasAny(relation,
		"plan", "goal", "intention", "intent", "purpose", "aim", "hopes",
		"hope", "will ", "going to"):
		return "plan"
	case relationHasAny(relation, "goal", "dream", "aspiration", "milestone"):
		return "goal"
	case relationHasAny(relation,
		"preference", "favorite", "likes", "like ", "prefers", "prefer",
		"loves", "love", "enjoys", "enjoy"):
		return "preference"
	case relationHasAny(relation,
		"activity", "hobby", "interest", "prefer", "preference", "desire",
		"passion", "motivation", "coping", "practice", "play", "enjoy",
		"like ", "likes", "love", "action", "appreciat"):
		return "activity"
	case relationHasAny(relation, "belief", "believes", "thinks", "opinion", "stance", "worldview"):
		return "belief"
	case relationHasAny(relation, "value", "values", "principle", "priority", "ethic"):
		return "value"
	case relationHasAny(relation, "trait", "personality", "character", "tendency", "temperament"):
		return "trait"
	case relationHasAny(relation,
		"identity", "name", "belief", "opinion", "value", "emotion", "feeling",
		"feels", "state", "characteristic", "quality", "attribute", "description",
		"assessment", "evaluation", "sentiment", "perception", "support",
		"appearance", "gender", "orientation"):
		return "identity"
	default:
		return "attribute"
	}
}

func normalizeEntityFactValue(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func relationHasAny(relation string, needles ...string) bool {
	if relation == "" {
		return false
	}
	for _, needle := range needles {
		needle = strings.TrimSpace(strings.ToLower(needle))
		if needle == "" {
			continue
		}
		if strings.Contains(relation, needle) {
			return true
		}
	}
	return false
}

func expandEntityFactRelations(relations []string) []string {
	out := make([]string, 0, len(relations)*2)
	seen := make(map[string]struct{}, len(relations)*2)
	add := func(relation string) {
		relation = normalizeEntityFactRelation(relation)
		if relation == "" {
			return
		}
		if _, ok := seen[relation]; ok {
			return
		}
		seen[relation] = struct{}{}
		out = append(out, relation)
	}
	for _, relation := range relations {
		normalized := normalizeEntityFactRelation(relation)
		if normalized == "" {
			continue
		}
		add(normalized)
		if family, ok := entityFactRelationFamilies[normalized]; ok {
			for _, member := range family {
				add(member)
			}
		}
	}
	return out
}

func normalizeEntityFactValueForRelation(relation, value, content string) string {
	relation = normalizeEntityFactRelation(relation)
	value = normalizeEntityFactValue(value)
	content = normalizeFactContent(content)
	if value == "" {
		value = inferSpecificValueCandidate(content)
	}
	switch relation {
	case "relationship status":
		lowered := strings.ToLower(strings.TrimSpace(value + " " + content))
		switch {
		case strings.Contains(lowered, "single"):
			return "single"
		case strings.Contains(lowered, "married"):
			return "married"
		case strings.Contains(lowered, "divorc"):
			return "divorced"
		case strings.Contains(lowered, "engaged"):
			return "engaged"
		case strings.Contains(lowered, "widowed"):
			return "widowed"
		case strings.Contains(lowered, "separated"):
			return "separated"
		case strings.Contains(lowered, "dating"), strings.Contains(lowered, "in a relationship"):
			return "dating"
		}
	case "identity":
		if match := identityValuePattern.FindString(strings.ToLower(value + " " + content)); match != "" {
			return strings.TrimSpace(match)
		}
	case "role":
		if match := roleValuePattern.FindString(strings.ToLower(value + " " + content)); match != "" {
			return strings.TrimSpace(match)
		}
	}
	return value
}

func buildEntityFactRecord(memory domain.Memory, fact ParsedFact) (domain.EntityFact, bool) {
	if strings.TrimSpace(memory.ID) == "" {
		return domain.EntityFact{}, false
	}
	entity := normalizeEntityFactEntity(fact.Entity)
	relationRaw := normalizeEntityFactRawRelation(fact.Relation)
	relation := normalizeEntityFactRelation(fact.Relation)
	value := normalizeEntityFactValueForRelation(relation, fact.Value, fact.Content)
	if entity == "" || relation == "" || value == "" {
		return domain.EntityFact{}, false
	}
	if relationRaw == "" {
		relationRaw = relation
	}
	return domain.EntityFact{
		TenantID:    memory.TenantID,
		Entity:      entity,
		Relation:    relation,
		RelationRaw: relationRaw,
		Value:       value,
		MemoryID:    memory.ID,
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

func normalizeEntityFactRawRelation(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
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
	if s.multiHop.GraphSingletonInvalidation {
		records = filterActiveEntityFacts(records)
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
		stored, err := batchRepo.StoreBatch(ctx, unique)
		if err != nil {
			return err
		}
		return s.invalidateSupersededEntityFacts(ctx, stored)
	}
	for _, fact := range unique {
		stored, err := s.entityRepo.Store(ctx, fact)
		if err != nil {
			return err
		}
		if err := s.invalidateSupersededEntityFacts(ctx, []domain.EntityFact{stored}); err != nil {
			return err
		}
	}
	return nil
}

func filterActiveEntityFacts(facts []domain.EntityFact) []domain.EntityFact {
	if len(facts) == 0 {
		return []domain.EntityFact{}
	}
	out := make([]domain.EntityFact, 0, len(facts))
	for _, fact := range facts {
		if strings.TrimSpace(fact.InvalidatedByFactID) != "" {
			continue
		}
		if fact.ValidTo != nil && !fact.ValidTo.IsZero() {
			continue
		}
		out = append(out, fact)
	}
	return out
}

func entityFactUsesSingletonInvalidation(relation string) bool {
	switch normalizeEntityFactRelation(relation) {
	case "identity", "role", "relationship status", "place":
		return true
	default:
		return false
	}
}

func (s *Service) invalidateSupersededEntityFacts(ctx context.Context, facts []domain.EntityFact) error {
	if s == nil || !s.multiHop.GraphSingletonInvalidation || len(facts) == 0 {
		return nil
	}
	invalidator, ok := s.entityRepo.(domain.EntityFactInvalidationRepository)
	if !ok || invalidator == nil {
		return nil
	}
	for _, fact := range facts {
		if !entityFactUsesSingletonInvalidation(fact.Relation) {
			continue
		}
		validTo := fact.ValidFrom
		if validTo.IsZero() {
			validTo = fact.ObservedAt
		}
		if validTo.IsZero() {
			validTo = fact.CreatedAt
		}
		if validTo.IsZero() {
			validTo = time.Now().UTC()
		}
		if err := invalidator.InvalidateEntityRelation(
			ctx,
			fact.TenantID,
			fact.Entity,
			fact.Relation,
			fact.Value,
			fact.ID,
			validTo,
		); err != nil {
			return err
		}
	}
	return nil
}
