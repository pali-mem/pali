package scoring

type Weights struct {
	Recency    float64
	Relevance  float64
	Importance float64
}

func DefaultWeights() Weights {
	return Weights{Recency: 1, Relevance: 1, Importance: 1}
}

func Score(w Weights, recency, relevance, importance float64) float64 {
	return (w.Recency * recency) + (w.Relevance * relevance) + (w.Importance * importance)
}
