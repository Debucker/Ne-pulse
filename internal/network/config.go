// Package network is Stage 3 of the NE-PULSE pipeline: it groups device
// picks into statistically independent spatial units, associates unit votes
// that are consistent with seismic propagation from a common point source,
// locates that source by grid search, and decides — with an explicit
// significance test against each unit's measured background trigger rate —
// whether the evidence supports a candidate, corroborated or confirmed
// event.
//
// The engine is synchronous and clock-free: every method takes the current
// time, so the server and the simulator run identical logic.
package network

// Level is an event's evidence level. Levels only ever increase while an
// event is active.
type Level string

const (
	LevelCandidate    Level = "candidate"    // >= 2 consistent units, weak significance
	LevelCorroborated Level = "corroborated" // multiple independent units, strong significance
	LevelConfirmed    Level = "confirmed"    // meets every configured confirmation criterion
)

// Criteria are the thresholds for one evidence level.
type Criteria struct {
	MinUnits       int     // inlier independent units
	MaxLog10PAdj   float64 // log10 of the look-elsewhere-adjusted p-value must be <= this
	MinFootprint   float64 // fraction of sensitive footprint units that triggered
	MinSources     int     // distinct network sources among contributing devices
	MinApertureKM  float64 // max distance between inlier units
	MinEstablished int     // contributing devices that are established (see Config)
	// MinEstablishedFrac: fraction of contributing devices that must be
	// established. Fabricated evidence has to come from devices that have
	// already streamed real data for hours.
	MinEstablishedFrac float64
	MaxRMSSec          float64 // travel-time residual RMS
	// MinConsistency: inliers / (inliers + unexplained votes near the
	// event). A real quake explains nearly every nearby trigger; a sweeping
	// front (sonic boom, blast, traffic) that a line of phones happens to
	// mimic as a source-below leaves most of its triggers unexplained.
	MinConsistency float64
	// HoldSec: the criteria must hold continuously for this long before the
	// level is granted. Long enough for any sub-seismic front (<= 0.35 km/s:
	// sound, traffic) to cross several unit cells and reveal itself.
	HoldSec float64
	// MinSweepVelKMS: if the triggers around an event are well fitted
	// (R^2 >= SweepR2) by a plane wave slower than this, they are a
	// sweeping front (sound, traffic), not seismic waves (>= ~2.5 km/s
	// apparent velocity even with mixed P/S picks). 0 disables.
	MinSweepVelKMS float64
	// MaxContradiction: largest allowed fraction contradictions /
	// (contradictions + inliers), where a contradiction is a silent,
	// listening unit whose predicted shaking (from the triggered units'
	// peaks, decayed as 1/R^2, steeper than real attenuation) exceeds
	// ContradictionFactor x its detection threshold. Physically: strong
	// shaking cannot stop at the edge of a few units. 0 disables.
	MaxContradiction float64
}

// Config holds every tunable of the network stage. docs/MODEL.md documents
// each default and marks which are provisional pending empirical data.
type Config struct {
	UnitKM          float64 // independence-unit cell size
	UnitVoteSec     float64 // picks of one unit within this gap belong to one vote
	UnitMinFraction float64 // fraction of a unit's eligible stations that must pick for a vote

	PriorVotes       float64 // Gamma prior shape alpha0 for unit background rate
	PriorExposureSec float64 // Gamma prior rate beta0 (s); prior mean = alpha0/beta0

	StationTimeoutSec    float64 // no status for this long => not eligible
	AssocWindowSec       float64 // how long votes stay associable
	EstablishedAgeSec    float64 // enrolled at least this long ago...
	EstablishedStreamSec float64 // ...and streamed at least this much data

	DepthKM             float64
	VpKMS, VsKMS        float64
	SigmaModel          float64 // travel-time model + location-quantisation error, s
	ResidualK           float64 // inlier if |residual| <= K * sigma
	GridNodes           int     // nodes per side of each grid-search level
	MinSearchKM         float64 // minimum half-width of the search region
	MaxLinkKM           float64 // votes further apart never belong to one event
	SweepR2             float64 // plane-wave fit quality above which a slow sweep is rejected
	ContradictionFactor float64 // predicted peak / detection threshold above which silence contradicts
	SensRatioMax        float64 // footprint excludes units this much less sensitive than the triggered ones

	EventQuietSec  float64 // close an event after this long without new supporting votes
	EventMinAgeSec float64 // ...and at least this long after origin
	MergeKM        float64
	MergeSec       float64

	Candidate, Corroborated, Confirmed Criteria
}

// DefaultConfig returns the reference configuration.
func DefaultConfig() Config {
	return Config{
		UnitKM:          1,
		UnitVoteSec:     2,
		UnitMinFraction: 0.34,

		PriorVotes:       1,
		PriorExposureSec: 1800, // prior mean 2 votes/hour: deliberately pessimistic for new units

		StationTimeoutSec:    20,
		AssocWindowSec:       120,
		EstablishedAgeSec:    12 * 3600,
		EstablishedStreamSec: 4 * 3600,

		DepthKM:             8,
		VpKMS:               5.80,
		VsKMS:               3.36,
		SigmaModel:          0.5,
		ResidualK:           3,
		GridNodes:           25,
		MinSearchKM:         30,
		MaxLinkKM:           200,
		SweepR2:             0.7,
		ContradictionFactor: 5,
		SensRatioMax:        2,

		EventQuietSec:  60,
		EventMinAgeSec: 90,
		MergeKM:        40,
		MergeSec:       20,

		Candidate:    Criteria{MinUnits: 2, MaxLog10PAdj: -2},
		Corroborated: Criteria{MinUnits: 4, MaxLog10PAdj: -4, MinFootprint: 0.25, MinSources: 3, MinApertureKM: 2, MaxRMSSec: 1.5, MinConsistency: 0.6, HoldSec: 8, MinSweepVelKMS: 1.0, MaxContradiction: 0.4},
		Confirmed:    Criteria{MinUnits: 6, MaxLog10PAdj: -6, MinFootprint: 0.4, MinSources: 4, MinApertureKM: 4, MinEstablished: 4, MinEstablishedFrac: 0.5, MaxRMSSec: 1.2, MinConsistency: 0.75, HoldSec: 15, MinSweepVelKMS: 1.0, MaxContradiction: 0.35},
	}
}
