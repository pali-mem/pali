package scoring

func Relevance(similarity float64) float64 {
	if similarity < 0 {
		return 0
	}
	if similarity > 1 {
		return 1
	}
	return similarity
}
