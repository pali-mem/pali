package scoring

import "math"

func Recency(decayFactor, hoursSinceAccess float64) float64 {
	if decayFactor <= 0 || decayFactor >= 1 {
		decayFactor = 0.98
	}
	if hoursSinceAccess < 0 {
		hoursSinceAccess = 0
	}
	return math.Pow(decayFactor, hoursSinceAccess)
}
