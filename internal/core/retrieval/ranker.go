// Package retrieval contains ranking helpers and retrieval abstractions.
package retrieval

import "sort"

// RankedMemory pairs a memory ID with a score.
type RankedMemory struct {
	MemoryID string
	Score    float64
}

// RankByScore sorts memories from highest score to lowest score.
func RankByScore(items []RankedMemory) []RankedMemory {
	sort.Slice(items, func(i, j int) bool {
		return items[i].Score > items[j].Score
	})
	return items
}
