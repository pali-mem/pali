package memory

import (
	"testing"

	"github.com/pali-mem/pali/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestInferValueFromFact_DoesNotReturnSentenceFragment(t *testing.T) {
	content := "Melanie at 8 May, 2023: I'm so excited, my sister just got married in Napa"
	require.Equal(t, "", inferValueFromFact(content, "event", "Melanie"))
}

func TestInferEntityRelationValue_KeepsEntityAndRelationWhenValueEmpty(t *testing.T) {
	entity, relation, value := inferEntityRelationValue("Alice works as a nurse", domain.MemoryKindObservation)
	require.Equal(t, "Alice", entity)
	require.NotEmpty(t, relation)
	require.Equal(t, "", value)
}

func TestBuildFactQuestionView_EmptyValueHasNoTemplateJunk(t *testing.T) {
	for _, relation := range []string{"activity", "event", "plan", "place", "preference"} {
		view := buildFactQuestionView(ParsedFact{
			Content:  "Melanie at 8 May, 2023: I'm so excited, my sister just got married in Napa",
			Entity:   "Melanie",
			Relation: relation,
		})
		require.NotContains(t, view, "8 May", relation)
		require.NotContains(t, view, "Napa", relation)
		require.NotContains(t, view, "excited", relation)
	}

	require.Equal(t, "", buildFactQuestionView(ParsedFact{Content: "Melanie attended something", Entity: "Melanie", Relation: "event"}))
}

func TestBuildFactQuestionView_UsesValueWhenPresent(t *testing.T) {
	view := buildFactQuestionView(ParsedFact{Content: "Melanie attended the Bali Arts Festival", Entity: "Melanie", Relation: "event", Value: "the Bali Arts Festival"})
	require.Contains(t, view, "when did Melanie the Bali Arts Festival")
}
