package serial

import "math"

// normalizeScalar rounds the physical slider to two decimal places.
func normalizeScalar(v float32) float32 {
	return float32(math.Round(float64(v)*100) / 100)
}

func significantlyDifferent(oldValue, newValue int, level string) bool {
	threshold := 10
	switch level {
	case "high":
		threshold = 20
	case "low":
		threshold = 5
	case "none":
		threshold = 1
	}
	if (newValue < 10 || newValue > 1013) && threshold > 5 {
		threshold = 5
	}
	difference := oldValue - newValue
	if difference < 0 {
		difference = -difference
	}
	return difference >= threshold
}
