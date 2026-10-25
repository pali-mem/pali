package memory

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Facts with byte-identical normalized content are duplicates regardless of
// how the inferred entity/relation/value triple differs.
func TestDedupePreparedFactsCollapsesSameContentDifferentTriple(t *testing.T) {
	facts := []ParsedFact{
		{Content: "Melanie is so excited, my sister just got married in Napa.", Entity: "Melanie", Relation: "relationship status", Value: "said at 8 May, 2023: x"},
		{Content: "Melanie is so excited, my sister just got married in Napa", Entity: "Melanie", Relation: "relationship status", Value: "(8 May, 2023): x"},
	}
	out := dedupePreparedFacts(facts)
	require.Len(t, out, 1)
}

func TestDedupePreparedFactsKeepsDistinctContent(t *testing.T) {
	facts := []ParsedFact{
		{Content: "Melanie moved to Boston"},
		{Content: "Melanie works as a nurse"},
	}
	out := dedupePreparedFacts(facts)
	require.Len(t, out, 2)
}
