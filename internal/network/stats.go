package network

import "math"

// PoissonBinomialTail returns P(K >= k) where K is the number of successes
// among independent Bernoulli trials with probabilities p. Computed exactly
// with the O(n*k) dynamic programme over P(K = j), truncated at k (mass at
// or above k is accumulated into the last bin), so it is stable for tiny
// probabilities.
func PoissonBinomialTail(p []float64, k int) float64 {
	if k <= 0 {
		return 1
	}
	if k > len(p) {
		return 0
	}
	// dist[j] = P(K = j) for j < k; dist[k] = P(K >= k).
	dist := make([]float64, k+1)
	dist[0] = 1
	for _, pi := range p {
		if pi < 0 {
			pi = 0
		} else if pi > 1 {
			pi = 1
		}
		q := 1 - pi
		dist[k] = dist[k] + dist[k-1]*pi
		for j := k - 1; j >= 1; j-- {
			dist[j] = dist[j]*q + dist[j-1]*pi
		}
		dist[0] *= q
	}
	return dist[k]
}

// GammaPoissonRate is the posterior-mean event rate (per second) for a
// Poisson process with a Gamma(alpha0, beta0) prior after observing n events
// in exposure seconds: (alpha0 + n) / (beta0 + exposure).
func GammaPoissonRate(alpha0, beta0, n, exposure float64) float64 {
	return (alpha0 + n) / (beta0 + exposure)
}

// ChanceProbability is the probability that a Poisson process of rate
// lambda (1/s) produces at least one event inside a window of w seconds.
func ChanceProbability(lambda, w float64) float64 {
	return -math.Expm1(-lambda * w)
}

// MMIFromPGA converts peak ground acceleration (m/s^2) to Modified Mercalli
// Intensity with the Worden et al. (2012, BSSA 102(1)) GMICE:
//
//	MMI = 1.78 + 1.55 log10(PGA)   for log10(PGA) <= 1.57
//	MMI = -1.60 + 3.70 log10(PGA)  otherwise
//
// with PGA in cm/s^2. Phones are not calibrated strong-motion instruments
// (coupling to furniture, floor level, sensor gain), so NE-PULSE labels the
// result "MMI-equivalent (uncalibrated)".
func MMIFromPGA(pgaMS2 float64) float64 {
	if pgaMS2 <= 0 {
		return 1
	}
	l := math.Log10(pgaMS2 * 100)
	var m float64
	if l <= 1.57 {
		m = 1.78 + 1.55*l
	} else {
		m = -1.60 + 3.70*l
	}
	return math.Max(1, math.Min(10, m))
}
