// Package scoring contains helpers for normalizing retrieval scores.
package scoring

// MinMax normalizes v into the [0,1] range bounded by min and max.
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
