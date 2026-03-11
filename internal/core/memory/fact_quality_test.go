package memory

import (
	"testing"

	"github.com/pali-mem/pali/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestPassesCanonicalFactAdmission_RejectsGenericOffer(t *testing.T) {
	fact := ParsedFact{
		Content:  "Would you like me to help?",
		Entity:   "Alice",
		Relation: "activity",
		Value:    "help",
		Kind:     domain.MemoryKindObservation,
	}

	require.False(t, passesCanonicalFactAdmission("Would you like me to help?", fact))
}

func TestPassesCanonicalFactAdmission_RejectsBareEmotion(t *testing.T) {
	fact := ParsedFact{
		Content:  "Alice is sad",
		Entity:   "Alice",
		Relation: "activity",
		Value:    "sad",
		Kind:     domain.MemoryKindObservation,
	}

	require.False(t, passesCanonicalFactAdmission("Alice is sad", fact))
}

func TestPassesCanonicalFactAdmission_RejectsSpeechOrReaction(t *testing.T) {
	fact := ParsedFact{
		Content:  "Alice looks great today",
		Entity:   "Alice",
		Relation: "activity",
		Value:    "looks great",
		Kind:     domain.MemoryKindObservation,
	}

	require.False(t, passesCanonicalFactAdmission("Alice looks great today", fact))
}

func TestPassesCanonicalFactAdmission_RejectsTemporalEventWithoutAnchor(t *testing.T) {
	fact := ParsedFact{
		Content:  "Alice will visit Paris tomorrow",
		Entity:   "Alice",
		Relation: "activity",
		Value:    "visit paris",
		Kind:     domain.MemoryKindEvent,
	}

	require.False(t, passesCanonicalFactAdmission("Alice will visit Paris tomorrow", fact))
}

func TestPassesCanonicalFactAdmission_AcceptsHighSignalFact(t *testing.T) {
	fact := ParsedFact{
		Content:  "Alice is a vegetarian and avoids dairy",
		Entity:   "Alice",
		Relation: "activity",
		Value:    "vegetarian and avoids dairy",
		Kind:     domain.MemoryKindObservation,
	}

	require.True(t, passesCanonicalFactAdmission("Alice is a vegetarian and avoids dairy", fact))
}

func TestIsBareEmotionFact_PersistsForEmotionFragments(t *testing.T) {
	require.True(t, isBareEmotionFact("Alice is sad"))
	require.False(t, isBareEmotionFact("Alice avoided the problem this morning."))
}

func TestIsSpeechOrReactionFact(t *testing.T) {
	require.True(t, isSpeechOrReactionFact("Alice looks great today!"))
	require.False(t, isSpeechOrReactionFact("Alice is finishing the report today."))
}

func TestBuildFactQuestionView_DeduplicatesAndBuildsEntityFromContent(t *testing.T) {
	fact := ParsedFact{
		Content:  "Alice likes hiking",
		Entity:   "Alice",
		Relation: "activity",
		Value:    "hiking",
		Kind:     domain.MemoryKindObservation,
	}

	view := buildFactQuestionView(fact)

	require.NotContains(t, view, "what does Alice do")
	require.NotContains(t, view, "what activities does Alice do")
	require.GreaterOrEqual(t, len(stringLines(view)), 2)
	require.Greater(t, len(view), 0)
	require.Contains(t, view, "what does Alice enjoy hiking")
	require.Contains(t, view, "Alice hiking")
}

func TestBuildFactQuestionView_UsesKnownRelationTemplates(t *testing.T) {
	fact := ParsedFact{
		Content:  "Alice likes coffee",
		Entity:   "Alice",
		Relation: "activity",
		Value:    "coffee",
	}

	view := buildFactQuestionView(fact)
	require.NotContains(t, view, "what does Alice do")
	require.NotContains(t, view, "what activities does Alice do")
	require.Contains(t, view, "what does Alice enjoy coffee")
	require.Contains(t, view, "Alice coffee")
}

func stringLines(value string) []string {
	if value == "" {
		return nil
	}
	out := make([]string, 0)
	start := 0
	for i := 0; i <= len(value); i++ {
		if i == len(value) || value[i] == '\n' {
			if segment := value[start:i]; len(segment) > 0 {
				out = append(out, segment)
			}
			start = i + 1
		}
	}
	return out
}
