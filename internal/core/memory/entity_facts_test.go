package memory

import (
	"testing"

	"github.com/pali-mem/pali/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestInferEntityRelationValue_ObservationActivity(t *testing.T) {
	entity, relation, value := inferEntityRelationValue("Alice likes coffee", domain.MemoryKindObservation)

	require.Equal(t, "Alice", entity)
	require.Equal(t, "activity", relation)
	require.Equal(t, "coffee", value)
}

func TestInferEntityRelationValue_EventKindForcesEventRelation(t *testing.T) {
	entity, relation, value := inferEntityRelationValue(
		"On 22 March 2025, Bob attended the open house.",
		domain.MemoryKindEvent,
	)

	require.Equal(t, "Bob", entity)
	require.Equal(t, "event", relation)
	require.Equal(t, "open house", value)
}

func TestInferEntityRelationValue_RemovesLeadingDatePrefix(t *testing.T) {
	entity, relation, value := inferEntityRelationValue(
		"On Friday, Carol moved to Austin.",
		domain.MemoryKindObservation,
	)

	require.Equal(t, "Carol", entity)
	require.Equal(t, "place", relation)
	require.Equal(t, "Austin", value)
}

func TestInferEntityRelationValue_FirstPersonEntityFallback(t *testing.T) {
	entity, relation, value := inferEntityRelationValue("i like coffee", domain.MemoryKindObservation)

	require.Equal(t, "user", entity)
	require.Equal(t, "activity", relation)
	require.Equal(t, "coffee", value)
}

func TestInferEntityFromFact_TrimsAndNormalizes(t *testing.T) {
	entity := inferEntityFromFact("  Alice   Johnson enjoys hiking")
	require.Equal(t, "Alice Johnson", entity)
}

func TestInferEntityFromFact_FirstPersonFallsBackToUser(t *testing.T) {
	require.Equal(t, "user", inferEntityFromFact("I use TypeScript at work"))
	require.Equal(t, "user", inferEntityFromFact("On 8 May 2023, I fixed the auth bug"))
}

func TestInferRelationFromFact_DetectsRoleAndPlace(t *testing.T) {
	require.Equal(t, "role", inferRelationFromFact("Alice works as a designer in Austin", domain.MemoryKindObservation))
	require.Equal(t, "place", inferRelationFromFact("Alice moved to Austin this summer", domain.MemoryKindObservation))
}

func TestInferValueFromFact_NormalizesPrefixes(t *testing.T) {
	value := inferValueFromFact("Alice is a vegetarian", "activity", "Alice")
	require.Equal(t, "a vegetarian", value)
}

func TestInferEntityRelationValue_FirstPersonTechPreference(t *testing.T) {
	entity, relation, value := inferEntityRelationValue("I use TypeScript for backend services", domain.MemoryKindObservation)
	require.Equal(t, "user", entity)
	require.Equal(t, "activity", relation)
	require.Equal(t, "TypeScript for backend services", value)
}

func TestNormalizedRelationTupleAndParsedFactPredicate(t *testing.T) {
	fact := ParsedFact{
		Entity:   " Alice  Johnson ",
		Relation: " likes ",
		Value:    "  Coffee ",
	}

	record, ok := buildEntityFactRecord(domain.Memory{
		ID:       "mem_1",
		TenantID: "tenant_1",
	}, fact)
	require.True(t, ok)
	require.Equal(t, "alice johnson", record.Entity)
	require.Equal(t, "activity", record.Relation)
	require.Equal(t, "likes", record.RelationRaw)
	require.Equal(t, "Coffee", record.Value)
	require.True(t, parsedFactHasEntityTriple(fact))
}

func TestNormalizeEntityFactRelationCanonicalizesLongTailLabels(t *testing.T) {
	cases := map[string]string{
		"participated_in":  "event",
		"event date":       "event",
		"career path":      "role",
		"coping mechanism": "activity",
		"preference":       "activity",
		"goal":             "plan",
		"belief":           "identity",
		"conveys_message":  "identity",
		"book":             "book",
		"location":         "place",
	}
	for in, want := range cases {
		require.Equal(t, want, normalizeEntityFactRelation(in), in)
	}
}
