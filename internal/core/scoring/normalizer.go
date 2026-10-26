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

// MinMaxWithFloor is MinMax with the range floored at minRange: when
// max-min is smaller than minRange, v maps to (v-min)/minRange, so a small
// spread uses a proportionally small part of [0,1] instead of all of it.
func MinMaxWithFloor(v, min, max, minRange float64) float64 {
	if max-min >= minRange {
		return MinMax(v, min, max)
	}
	return MinMax(v, min, min+minRange)
}
