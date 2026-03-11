package memory

import (
	"testing"

	"github.com/pali-mem/pali/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestBuildParsedFactIdentity_Deterministic(t *testing.T) {
	fact := ParsedFact{
		Content:  "Alice likes coffee",
		Entity:   "Alice",
		Relation: "activity",
		Value:    "coffee",
		Kind:     domain.MemoryKindObservation,
	}

	id1 := buildParsedFactIdentity("Alice likes coffee", 0, fact, "heuristic", "v1")
	id2 := buildParsedFactIdentity("Alice likes coffee", 0, fact, "heuristic", "v1")

	require.Equal(t, id1, id2)
}

func TestBuildParsedFactIdentity_NormalizesInputs(t *testing.T) {
	fact := ParsedFact{
		Content:  "Alice likes COFFEE",
		Entity:   " Alice ",
		Relation: " Activity ",
		Value:    "COFFEE",
		Kind:     domain.MemoryKindObservation,
	}

	id := buildParsedFactIdentity("  Alice   says   likes COFFEE ", 0, fact, "  LLM  ", "  v1 ")
	idUpper := buildParsedFactIdentity("Alice says likes COFFEE", 0, fact, "llm", "v1")

	require.Equal(t, id, idUpper)
}

func TestBuildRawTurnIdentity_ReusesTurnHash(t *testing.T) {
	turn1 := buildRawTurnIdentity("Alice   said  hello")
	turn2 := buildRawTurnIdentity("  Alice said hello ")

	require.Equal(t, turn1.CanonicalKey, turn2.CanonicalKey)
	require.Equal(t, "raw_turn", turn1.Extractor)
	require.Equal(t, "v1", turn1.ExtractorVersion)
	require.Equal(t, -1, turn1.SourceFactIndex)
}

func TestHashIdentityParts_StableForSameInputs(t *testing.T) {
	h1 := hashIdentityParts("turn", "Alice", "likes", "coffee")
	h2 := hashIdentityParts("turn", "Alice", "likes", "coffee")

	require.Equal(t, h1, h2)
	require.NotEmpty(t, h1)
}

func TestHashIdentityParts_DiffersWithInput(t *testing.T) {
	h1 := hashIdentityParts("turn", "Alice")
	h2 := hashIdentityParts("turn", "Alice", "coffee")

	require.NotEqual(t, h1, h2)
}
