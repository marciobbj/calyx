package crypto

import (
	"math/rand"
	"time"
)

// AddGaussianNoise applies additive Gaussian noise to a slice of float64 data.
// This is used as a differential privacy mechanism to prevent embedding inversion.
func AddGaussianNoise(data []float64, stdDev float64) []float64 {
	if stdDev <= 0.0 {
		return data
	}

	// Create a local rand source to prevent global seed lock issues
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	perturbed := make([]float64, len(data))
	for i, val := range data {
		noise := r.NormFloat64() * stdDev
		perturbed[i] = val + noise
	}
	return perturbed
}
