package retrieval

import "sort"

type RankedMemory struct {
	MemoryID string
	Score    float64
}

func RankByScore(items []RankedMemory) []RankedMemory {
	sort.Slice(items, func(i, j int) bool {
		return items[i].Score > items[j].Score
	})
	return items
}
