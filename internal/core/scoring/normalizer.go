package scoring

func MinMax(v, min, max float64) float64 {
	if max <= min {
		return 0
	}
	if v < min {
		return 0
	}
	if v > max {
		return 1
	}
	return (v - min) / (max - min)
}
