// Package station turns one device's raw, irregularly-timed accelerometer
// stream into (a) a continuously updated sensor-quality status and (b)
// discrete "picks": candidate onsets of ground motion, each with a feature
// vector and a transparent accept/reject classification.
//
// It is Stage 1 (device-level preprocessing) and Stage 2 (device-level event
// detection) of the NE-PULSE pipeline. The same code runs inside the server
// and inside the offline simulator; it has no goroutines, no wall clock and
// no randomness — all time comes from sample timestamps.
package station

// Config holds every tunable of the station processor. Defaults are
// documented in docs/MODEL.md with their rationale; values marked
// "provisional" there require empirical calibration on real phones.
type Config struct {
	// FsHz is the uniform internal sample rate every device is resampled to.
	FsHz float64
	// GapMaxSec: an input gap longer than this breaks the stream and resets
	// all filters.
	GapMaxSec float64
	// MinInputRateHz: devices delivering fewer samples per second cannot
	// support the analysis band and are never eligible.
	MinInputRateHz float64

	// Analysis band (Butterworth): high-pass order/corner, low-pass order/corner.
	HPOrder   int
	BandLowHz float64
	LPOrder   int
	BandHiHz  float64

	// GravityCutHz: corner of the 2nd-order low-pass used to estimate the
	// gravity vector (device orientation).
	GravityCutHz float64

	// STA/LTA trigger.
	StaSec, LtaSec float64
	TriggerRatio   float64
	DetriggerRatio float64
	MinTriggerAmp  float64 // absolute floor on trigger amplitude, m/s^2
	QuantFactor    float64 // trigger amplitude must also exceed QuantFactor x measured resolution
	// AssumedResolution is used until a nonzero step has been observed: a
	// perfectly still phone behind a coarsening browser outputs constant
	// values, so its resolution is unobservable and must be assumed coarse.
	AssumedResolution float64
	PickSigmaSec      float64 // 1-sigma onset uncertainty of the AIC pick
	EvalDelaySec      float64 // features are computed this long after the trigger
	MaxTriggerSec     float64 // a trigger is force-closed after this long
	AICPreSec         float64 // AIC search window before the trigger
	PreWindowSec      float64 // pre-event noise window
	EndSettleSec      float64 // after detrigger, wait this long before measuring final tilt

	// Stationarity: a device is "still" when its gravity direction has
	// varied by less than StillMaxTiltDeg over the last StillWindowSec, and
	// it becomes eligible after SettleSec of continuous stillness.
	StillWindowSec  float64
	StillMaxTiltDeg float64
	// StillMaxRMS: every 1-s band-passed RMS in the still window must be
	// below this (m/s^2). Catches oscillatory motion (walking, pocket sway)
	// whose mean orientation is constant and so invisible to the tilt test.
	StillMaxRMS float64
	SettleSec   float64
	// A phone that was already listening and is only picked up briefly (a
	// tap, a scroll, a glance at the screen) keeps its noise floor: if the
	// handling lasted at most ResumeMaxSec, it is eligible again after
	// ResumeSettleSec of stillness instead of SettleSec, provided it lies
	// within ResumeMaxTiltDeg of its earlier position.
	ResumeSettleSec  float64
	ResumeMaxSec     float64
	ResumeMaxTiltDeg float64
	NoiseWindowSec   float64

	// Rejection gates (see Classify).
	FreefallFrac        float64 // |a| below this fraction of g counts as free fall
	FreefallMinSec      float64
	MaxEventTiltDeg     float64
	ImpulsiveCrest      float64 // crest factor above this => impulsive
	ImpulsiveMaxD595    float64 // significant duration below this (s) => impulsive
	TonalMaxFlatness    float64 // min short-window spectral flatness below this => tonal (0 disables)
	UnsettledNoiseRatio float64
	ClipLevel           float64 // |component| at or above this, repeated, flags clipping (m/s^2)

	// Buffers / reporting.
	BufferSec         float64
	StatusIntervalSec float64
}

// DefaultConfig returns the reference configuration.
func DefaultConfig() Config {
	return Config{
		FsHz:           50,
		GapMaxSec:      0.5,
		MinInputRateHz: 30,

		HPOrder:   4,
		BandLowHz: 0.5,
		LPOrder:   2,
		BandHiHz:  12,

		GravityCutHz: 0.1,

		StaSec:            1,
		LtaSec:            30,
		TriggerRatio:      4,
		DetriggerRatio:    1.5,
		MinTriggerAmp:     0.02,
		QuantFactor:       2,
		AssumedResolution: 0.1,
		PickSigmaSec:      0.2,
		EvalDelaySec:      2.56,
		MaxTriggerSec:     60,
		AICPreSec:         3,
		PreWindowSec:      5,
		EndSettleSec:      4,

		StillWindowSec:   10,
		StillMaxTiltDeg:  1.0,
		StillMaxRMS:      0.15,
		SettleSec:        30,
		ResumeSettleSec:  10,
		ResumeMaxSec:     120,
		ResumeMaxTiltDeg: 3,
		NoiseWindowSec:   300,

		FreefallFrac:        0.3,
		FreefallMinSec:      0.06,
		MaxEventTiltDeg:     2.0,
		ImpulsiveCrest:      8,
		ImpulsiveMaxD595:    0.4,
		TonalMaxFlatness:    0, // disabled: no separation found, see docs/MODEL.md
		UnsettledNoiseRatio: 3,
		ClipLevel:           19.0,

		BufferSec:         60,
		StatusIntervalSec: 5,
	}
}
