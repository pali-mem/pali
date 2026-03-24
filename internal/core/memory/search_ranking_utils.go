package memory

import (
	"context"
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/pali-mem/pali/internal/domain"
)

func weightedAverage(values []float64, weights []float64) float64 {
	if len(values) == 0 || len(values) != len(weights) {
		return 0
	}
	totalWeight := 0.0
	total := 0.0
	for i := range values {
		w := weights[i]
		if w <= 0 {
			continue
		}
		totalWeight += w
		total += w * clamp01(values[i])
	}
	if totalWeight == 0 {
		return 0
	}
	return clamp01(total / totalWeight)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func normalizedRouteBoost(v float64) float64 {
	const (
		minBoost = 0.8
		maxBoost = 1.35
	)
	return clamp01((v - minBoost) / (maxBoost - minBoost))
}

func normalizedRankingTokens(text string) map[string]struct{} {
	tokens := normalizedRankingTokenList(text)
	out := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		out[token] = struct{}{}
	}
	return out
}

func normalizedRankingTokenList(text string) []string {
	rawTokens := rankingTokenPattern.FindAllString(strings.ToLower(strings.TrimSpace(text)), -1)
	if len(rawTokens) == 0 {
		return []string{}
	}
	filtered := make([]string, 0, len(rawTokens))
	for _, token := range rawTokens {
		if len(token) < 2 {
			continue
		}
		if _, stop := rankingStopwordPattern[token]; stop {
			continue
		}
		filtered = append(filtered, token)
	}
	if len(filtered) > 0 {
		return filtered
	}
	out := make([]string, 0, len(rawTokens))
	for _, token := range rawTokens {
		if len(token) < 2 {
			continue
		}
		out = append(out, token)
	}
	return out
}

func orderedBigramCoverage(query, content string) float64 {
	queryTokens := normalizedRankingTokenList(query)
	if len(queryTokens) < 2 {
		return 0
	}
	contentTokens := normalizedRankingTokenList(content)
	if len(contentTokens) < 2 {
		return 0
	}
	docBigrams := make(map[string]struct{}, len(contentTokens)-1)
	for i := 0; i < len(contentTokens)-1; i++ {
		docBigrams[contentTokens[i]+" "+contentTokens[i+1]] = struct{}{}
	}
	matches := 0
	total := len(queryTokens) - 1
	for i := 0; i < total; i++ {
		if _, ok := docBigrams[queryTokens[i]+" "+queryTokens[i+1]]; ok {
			matches++
		}
	}
	if total <= 0 {
		return 0
	}
	return clamp01(float64(matches) / float64(total))
}

func queryOverlapScore(queryTokens, docTokens map[string]struct{}) float64 {
	if len(queryTokens) == 0 || len(docTokens) == 0 {
		return 0
	}
	matches := 0
	for token := range queryTokens {
		if _, ok := docTokens[token]; ok {
			matches++
		}
	}
	return float64(matches) / float64(len(queryTokens))
}

func findTokenPositionAfter(tokens []string, target string, start int) int {
	if target == "" || start >= len(tokens) {
		return -1
	}
	if start < 0 {
		start = 0
	}
	for i := start; i < len(tokens); i++ {
		if tokens[i] == target {
			return i
		}
	}
	return -1
}

func candidateWindow(
	topK int,
	profile queryProfile,
	plan queryPlan,
	hasFilters bool,
	tuning RetrievalSearchTuningOptions,
) int {
	if topK <= 0 {
		topK = 10
	}
	n := topK * tuning.CandidateWindowMultiplier
	if n < tuning.CandidateWindowMin {
		n = tuning.CandidateWindowMin
	}
	if profile.MultiHop {
		n += tuning.CandidateWindowMultiHopBoost
	} else if profile.Temporal {
		n += tuning.CandidateWindowTemporalBoost
	}
	if hasFilters {
		n += tuning.CandidateWindowFilterBoost
	}
	if plan.Confidence < tuning.AdaptiveQueryPlanConfidenceThreshold {
		n += topK * 2
	}
	maxWindow := tuning.CandidateWindowMax
	if profile.MultiHop || hasFilters {
		maxWindow = max(maxWindow, 320)
	}
	if n > maxWindow {
		n = maxWindow
	}
	return n
}

func preferCanonicalUnits(items []scoredMemory) []scoredMemory {
	if len(items) == 0 {
		return items
	}
	promoted := make([]scoredMemory, 0, len(items))
	raw := make([]scoredMemory, 0, len(items))
	for _, item := range items {
		if item.Memory.Kind == domain.MemoryKindRawTurn {
			raw = append(raw, item)
			continue
		}
		promoted = append(promoted, item)
	}
	if len(promoted) == 0 || len(raw) == 0 {
		return items
	}
	return append(promoted, raw...)
}

func isCanonicalFactMemory(memory domain.Memory) bool {
	if strings.TrimSpace(memory.CanonicalKey) == "" {
		return false
	}
	if memory.SourceFactIndex < 0 {
		return false
	}
	return memory.Kind == domain.MemoryKindObservation || memory.Kind == domain.MemoryKindEvent
}

func (s *Service) expandGroundedContextMemories(
	ctx context.Context,
	tenantID string,
	memories []domain.Memory,
	topK int,
) ([]domain.Memory, error) {
	repo, ok := s.repo.(domain.MemorySourceTurnRepository)
	if !ok || repo == nil || len(memories) == 0 {
		return memories, nil
	}

	out := make([]domain.Memory, 0, len(memories)+min(len(memories), 4))
	seen := make(map[string]struct{}, len(memories)+4)
	appendMemory := func(memory domain.Memory) {
		if strings.TrimSpace(memory.ID) == "" {
			return
		}
		if _, ok := seen[memory.ID]; ok {
			return
		}
		seen[memory.ID] = struct{}{}
		out = append(out, memory)
	}

	for _, memory := range memories {
		appendMemory(memory)
		if topK > 0 && len(out) >= topK {
			continue
		}
		if memory.Kind == domain.MemoryKindRawTurn || strings.TrimSpace(memory.SourceTurnHash) == "" {
			continue
		}
		siblings, err := repo.ListBySourceTurnHash(ctx, tenantID, memory.SourceTurnHash, 4)
		if err != nil {
			return nil, err
		}
		var rawTurn *domain.Memory
		var supportingFact *domain.Memory
		for i := range siblings {
			sibling := siblings[i]
			if sibling.ID == memory.ID {
				continue
			}
			switch sibling.Kind {
			case domain.MemoryKindRawTurn:
				if rawTurn == nil {
					candidate := sibling
					rawTurn = &candidate
				}
			case domain.MemoryKindEvent, domain.MemoryKindObservation:
				if supportingFact == nil {
					candidate := sibling
					supportingFact = &candidate
				}
			}
		}
		if rawTurn != nil {
			appendMemory(*rawTurn)
		}
		if supportingFact != nil {
			appendMemory(*supportingFact)
		}
	}
	return out, nil
}

func fuseCandidatesByRRF(
	dense []domain.VectorstoreCandidate,
	lexical []lexicalCandidate,
	limit int,
) ([]string, map[string]candidateSignal) {
	if limit <= 0 {
		limit = 10
	}

	rrfScore := make(map[string]float64, len(dense)+len(lexical))
	signalByID := make(map[string]candidateSignal, len(dense)+len(lexical))
	type rankScore struct {
		rank  int
		score float64
	}
	denseBest := make(map[string]rankScore, len(dense))
	for idx, candidate := range dense {
		id := strings.TrimSpace(candidate.MemoryID)
		if id == "" {
			continue
		}
		rank := idx + 1
		current, ok := denseBest[id]
		if !ok || rank < current.rank {
			denseBest[id] = rankScore{rank: rank, score: candidate.Similarity}
			continue
		}
		if candidate.Similarity > current.score {
			current.score = candidate.Similarity
			denseBest[id] = current
		}
	}
	for id, item := range denseBest {
		rrfScore[id] += 1.0 / float64(reciprocalRankFusionK+item.rank)
		signalByID[id] = candidateSignal{
			DenseScore: clamp01(item.score),
			DenseRank:  item.rank,
		}
	}

	lexicalBest := make(map[string]rankScore, len(lexical))
	for idx, candidate := range lexical {
		id := strings.TrimSpace(candidate.Memory.ID)
		if id == "" {
			continue
		}
		rank := idx + 1
		score := clamp01(candidate.Score)
		current, ok := lexicalBest[id]
		if !ok || rank < current.rank {
			lexicalBest[id] = rankScore{rank: rank, score: score}
			continue
		}
		if score > current.score {
			current.score = score
			lexicalBest[id] = current
		}
	}
	for id, item := range lexicalBest {
		rrfScore[id] += 1.0 / float64(reciprocalRankFusionK+item.rank)
		signal := signalByID[id]
		signal.LexicalScore = clamp01(item.score)
		signal.LexicalRank = item.rank
		signalByID[id] = signal
	}

	ids := make([]string, 0, len(rrfScore))
	for id := range rrfScore {
		ids = append(ids, id)
	}

	sort.Slice(ids, func(i, j int) bool {
		a := ids[i]
		b := ids[j]
		if rrfScore[a] == rrfScore[b] {
			return a < b
		}
		return rrfScore[a] > rrfScore[b]
	})

	if len(ids) > limit {
		ids = ids[:limit]
	}

	filteredSignals := make(map[string]candidateSignal, len(ids))
	for _, id := range ids {
		signal := signalByID[id]
		signal.RRFScore = clamp01(rrfToNormalized(rrfScore[id]))
		filteredSignals[id] = signal
	}

	return ids, filteredSignals
}

func rankToNormalized(rank int, limit int) float64 {
	if rank <= 0 {
		return 0
	}
	if limit <= 1 {
		return 1
	}
	r := 1.0 - (float64(rank-1) / float64(limit-1))
	return clamp01(r)
}

func rrfToNormalized(rrf float64) float64 {
	if rrf <= 0 {
		return 0
	}
	base := 1.0 / float64(reciprocalRankFusionK+1)
	return clamp01(rrf / (rrf + base))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func sortScoredByScore(items []scoredMemory) []scoredMemory {
	slices.SortFunc(items, func(a, b scoredMemory) int {
		switch {
		case a.Score > b.Score:
			return -1
		case a.Score < b.Score:
			return 1
		default:
			return strings.Compare(a.Memory.ID, b.Memory.ID)
		}
	})
	return items
}

func mathLog1pSafe(v float64) float64 {
	return math.Log(1 + v)
}
