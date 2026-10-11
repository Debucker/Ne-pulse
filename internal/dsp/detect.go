package dsp

import (
	"math"
	"math/cmplx"
	"sort"
)

// StaLta is the classic recursive short-term-average / long-term-average
// energy detector (Withers et al., 1998; parameter guidance in Trnkoczy,
// 2012, NMSOP-2 IS 8.1). Input is signal energy x^2 (units (m/s^2)^2);
// the ratio is dimensionless.
//
//	STA_k = STA_{k-1} + (e_k - STA_{k-1}) / N_sta
//	LTA_k = LTA_{k-1} + (e_k - LTA_{k-1}) / N_lta
//
// While Frozen is set, LTA is not updated so a long event cannot raise its
// own reference level (standard practice to avoid self-detriggering).
type StaLta struct {
	NSta, NLta float64
	Sta, Lta   float64
	Frozen     bool
	n          int
}

// NewStaLta builds a detector for window lengths in seconds at rate fs.
func NewStaLta(staSec, ltaSec, fs float64) *StaLta {
	return &StaLta{NSta: staSec * fs, NLta: ltaSec * fs}
}

// Update ingests one energy sample and returns the current ratio (0 until
// the LTA holds at least one LTA window of data).
func (s *StaLta) Update(e float64) float64 {
	s.n++
	if s.n == 1 {
		s.Sta, s.Lta = e, e
	}
	s.Sta += (e - s.Sta) / s.NSta
	if !s.Frozen {
		s.Lta += (e - s.Lta) / s.NLta
	}
	if !s.Warm() || s.Lta <= 0 {
		return 0
	}
	return s.Sta / s.Lta
}

// Warm reports whether a full LTA window has been observed.
func (s *StaLta) Warm() bool { return float64(s.n) >= s.NLta }

// Reset clears all state.
func (s *StaLta) Reset() { s.Sta, s.Lta, s.n, s.Frozen = 0, 0, 0, false }

// Seed sets both averages to a measured background energy and marks the
// detector warm. Used when a device settles after being handled, so the
// LTA does not have to spend minutes forgetting the handling energy.
func (s *StaLta) Seed(e float64) {
	s.Sta, s.Lta, s.Frozen = e, e, false
	s.n = int(math.Ceil(s.NLta))
}

// AICPick returns the index that minimises Maeda's (1985) AIC function
// computed directly on the samples:
//
//	AIC(k) = k log(var(x[0:k])) + (N-k-1) log(var(x[k:N]))
//
// The minimum marks the most likely change point between two stationary
// segments (noise -> signal). Returns -1 if x is too short.
func AICPick(x []float64) int {
	n := len(x)
	if n < 8 {
		return -1
	}
	s1 := make([]float64, n+1)
	s2 := make([]float64, n+1)
	for i, v := range x {
		s1[i+1] = s1[i] + v
		s2[i+1] = s2[i] + v*v
	}
	variance := func(a, b int) float64 {
		m := float64(b - a)
		mean := (s1[b] - s1[a]) / m
		v := (s2[b]-s2[a])/m - mean*mean
		if v < 1e-20 {
			v = 1e-20
		}
		return v
	}
	best, bestK := math.Inf(1), -1
	for k := 3; k < n-3; k++ {
		a := float64(k)*math.Log(variance(0, k)) + float64(n-k-1)*math.Log(variance(k, n))
		if a < best {
			best, bestK = a, k
		}
	}
	return bestK
}

// FFT computes an in-place radix-2 decimation-in-time FFT. len(a) must be a
// power of two.
func FFT(a []complex128) {
	n := len(a)
	if n&(n-1) != 0 {
		panic("dsp: FFT length must be a power of two")
	}
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			a[i], a[j] = a[j], a[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		w := cmplx.Exp(complex(0, -2*math.Pi/float64(size)))
		for start := 0; start < n; start += size {
			wk := complex(1, 0)
			for k := 0; k < size/2; k++ {
				u := a[start+k]
				v := a[start+k+size/2] * wk
				a[start+k] = u + v
				a[start+k+size/2] = u - v
				wk *= w
			}
		}
	}
}

// Spectrum holds one-sided power spectral features of a real signal.
type Spectrum struct {
	DominantHz float64 // frequency of maximum power within the band
	CentroidHz float64 // power-weighted mean frequency within the band
	Flatness   float64 // geometric / arithmetic mean power within the band (0 = pure tone, 1 = white)
}

// SpectralFeatures Hann-windows x (zero-padded to the next power of two),
// computes its power spectrum and summarises it over [fLo, fHi] Hz.
func SpectralFeatures(x []float64, fs, fLo, fHi float64) Spectrum {
	n := 1
	for n < len(x) {
		n <<= 1
	}
	if n < 8 {
		return Spectrum{}
	}
	mean := Mean(x)
	a := make([]complex128, n)
	for i, v := range x {
		w := 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(len(x)-1))
		a[i] = complex((v-mean)*w, 0)
	}
	FFT(a)
	var sumP, sumFP, sumLog float64
	var cnt int
	best, bestF := -1.0, 0.0
	for k := 1; k <= n/2; k++ {
		f := float64(k) * fs / float64(n)
		if f < fLo || f > fHi {
			continue
		}
		p := real(a[k])*real(a[k]) + imag(a[k])*imag(a[k])
		sumP += p
		sumFP += f * p
		sumLog += math.Log(p + 1e-30)
		cnt++
		if p > best {
			best, bestF = p, f
		}
	}
	if cnt == 0 || sumP <= 0 {
		return Spectrum{}
	}
	return Spectrum{
		DominantHz: bestF,
		CentroidHz: sumFP / sumP,
		Flatness:   math.Exp(sumLog/float64(cnt)) / (sumP / float64(cnt)),
	}
}

// SignificantDuration returns the Trifunac-Brady (1975) duration D5-95 in
// seconds: the time between 5 % and 95 % of the cumulative energy of x.
func SignificantDuration(x []float64, fs float64) float64 {
	var total float64
	for _, v := range x {
		total += v * v
	}
	if total <= 0 {
		return 0
	}
	var cum float64
	i5, i95 := -1, -1
	for i, v := range x {
		cum += v * v
		if i5 < 0 && cum >= 0.05*total {
			i5 = i
		}
		if i95 < 0 && cum >= 0.95*total {
			i95 = i
			break
		}
	}
	return float64(i95-i5) / fs
}

// Mean of x (0 for empty).
func Mean(x []float64) float64 {
	if len(x) == 0 {
		return 0
	}
	var s float64
	for _, v := range x {
		s += v
	}
	return s / float64(len(x))
}

// RMS of x (0 for empty).
func RMS(x []float64) float64 {
	if len(x) == 0 {
		return 0
	}
	var s float64
	for _, v := range x {
		s += v * v
	}
	return math.Sqrt(s / float64(len(x)))
}

// Percentile returns the p-th percentile (0..100) using linear
// interpolation between order statistics. x is not modified.
func Percentile(x []float64, p float64) float64 {
	if len(x) == 0 {
		return math.NaN()
	}
	s := append([]float64(nil), x...)
	sort.Float64s(s)
	if len(s) == 1 {
		return s[0]
	}
	r := p / 100 * float64(len(s)-1)
	lo := int(math.Floor(r))
	hi := int(math.Ceil(r))
	return s[lo] + (r-float64(lo))*(s[hi]-s[lo])
}

// Median is Percentile(x, 50).
func Median(x []float64) float64 { return Percentile(x, 50) }
