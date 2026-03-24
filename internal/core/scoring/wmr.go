package scoring

// Weights controls the relative contribution of each score component.
type Weights struct {
	Recency    float64
	Relevance  float64
	Importance float64
}

// DefaultWeights returns equal weights for recency, relevance, and importance.
func DefaultWeights() Weights {
	return Weights{Recency: 1, Relevance: 1, Importance: 1}
}

// Score computes the weighted sum of the supplied components.
func Score(w Weights, recency, relevance, importance float64) float64 {
	return (w.Recency * recency) + (w.Relevance * relevance) + (w.Importance * importance)
}
