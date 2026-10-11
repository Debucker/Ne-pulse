package network

import (
	"math"
	"slices"

	"ne-pulse/internal/geo"
)

// Obs is one unit vote as seen by the locator: position on a local tangent
// plane (km), arrival time (s) and timing tolerance tau (s).
type Obs struct {
	X, Y float64
	T    float64
	Tau  float64
}

// Fit is the result of locating a set of observations.
type Fit struct {
	X, Y      float64 // km on the local plane
	OriginT   float64
	Inlier    []bool
	Phase     []byte    // 'P' or 'S' for inliers
	Residual  []float64 // observed - predicted, s (inliers)
	Hypo      []float64 // hypocentral distance to each obs, km
	NInliers  int
	RMS       float64
	LocUncKM  float64
	Nodes     int     // hypotheses evaluated
	HalfWidth float64 // search half-width, km
}

type interval struct {
	pos   float64
	start bool
	obs   int
}

// evalNode finds the origin time that makes the largest number of
// observations consistent (|residual| <= tau) with a source at (x, y, depth),
// allowing each observation to be either a P or an S arrival, and scores the
// node with an Epanechnikov kernel, score = sum over inliers of
// 1 - (r/tau)^2: a robust M-estimator that ignores outliers but, unlike a
// plain inlier count, prefers hypotheses that fit their inliers tightly.
func evalNode(cfg *Config, obs []Obs, x, y float64, iv []interval, cover []int) (n int, t0, rms, score float64) {
	iv = iv[:0]
	h2 := cfg.DepthKM * cfg.DepthKM
	for i, o := range obs {
		r := math.Sqrt((x-o.X)*(x-o.X) + (y-o.Y)*(y-o.Y) + h2)
		for _, v := range [2]float64{cfg.VpKMS, cfg.VsKMS} {
			c := o.T - r/v
			iv = append(iv, interval{c - o.Tau, true, i}, interval{c + o.Tau, false, i})
		}
	}
	slices.SortFunc(iv, func(a, b interval) int {
		switch {
		case a.pos < b.pos:
			return -1
		case a.pos > b.pos:
			return 1
		case a.start && !b.start:
			return -1
		case !a.start && b.start:
			return 1
		}
		return 0
	})
	for i := range cover {
		cover[i] = 0
	}
	distinct, best := 0, 0
	bestAt := 0.0
	for j, e := range iv {
		if e.start {
			if cover[e.obs] == 0 {
				distinct++
			}
			cover[e.obs]++
			if distinct > best {
				best = distinct
				// centre of the max-cover region: up to the next event
				end := e.pos
				if j+1 < len(iv) {
					end = iv[j+1].pos
				}
				bestAt = (e.pos + end) / 2
			}
		} else {
			cover[e.obs]--
			if cover[e.obs] == 0 {
				distinct--
			}
		}
	}
	t0 = bestAt
	// Refine t0 by weighted least squares over inliers, then re-count.
	for iter := 0; iter < 2; iter++ {
		var sw, swt float64
		n = 0
		var ss float64
		for _, o := range obs {
			r := math.Sqrt((x-o.X)*(x-o.X) + (y-o.Y)*(y-o.Y) + h2)
			rp := o.T - (t0 + r/cfg.VpKMS)
			rs := o.T - (t0 + r/cfg.VsKMS)
			res, v := rp, cfg.VpKMS
			if math.Abs(rs) < math.Abs(rp) {
				res, v = rs, cfg.VsKMS
			}
			if math.Abs(res) <= o.Tau {
				w := 1 / (o.Tau * o.Tau)
				sw += w
				swt += w * (o.T - r/v)
				ss += res * res
				n++
			}
		}
		if n == 0 {
			return 0, t0, 0, 0
		}
		rms = math.Sqrt(ss / float64(n))
		if iter == 0 {
			t0 = swt / sw
		}
	}
	for _, o := range obs {
		r := math.Sqrt((x-o.X)*(x-o.X) + (y-o.Y)*(y-o.Y) + h2)
		res := math.Min(math.Abs(o.T-(t0+r/cfg.VpKMS)), math.Abs(o.T-(t0+r/cfg.VsKMS)))
		if res <= o.Tau {
			score += 1 - (res/o.Tau)*(res/o.Tau)
		}
	}
	return n, t0, rms, score
}

// Locate grid-searches a point source (fixed depth) that explains the
// largest number of observations, coarse-to-fine, and estimates location
// uncertainty from the spread of near-equivalent grid nodes.
func Locate(cfg *Config, obs []Obs) Fit {
	var cx, cy float64
	for _, o := range obs {
		cx += o.X
		cy += o.Y
	}
	cx /= float64(len(obs))
	cy /= float64(len(obs))
	half := cfg.MinSearchKM
	for _, o := range obs {
		if d := math.Hypot(o.X-cx, o.Y-cy) + 20; d > half {
			half = d
		}
	}
	n := cfg.GridNodes
	iv := make([]interval, 0, 4*len(obs))
	cover := make([]int, len(obs))

	type node struct {
		x, y, t0, rms, score float64
		n                    int
	}
	var nodes []node
	best := node{n: -1, score: -1}
	search := func(ox, oy, hw float64) float64 {
		step := 2 * hw / float64(n-1)
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				x := ox - hw + float64(i)*step
				y := oy - hw + float64(j)*step
				k, t0, rms, sc := evalNode(cfg, obs, x, y, iv, cover)
				nd := node{x, y, t0, rms, sc, k}
				nodes = append(nodes, nd)
				if sc > best.score {
					best = nd
				}
			}
		}
		return step
	}
	step1 := search(cx, cy, half)
	step2 := search(best.x, best.y, step1)

	// Uncertainty: every evaluated node whose score is within one
	// perfectly-fitting observation (or 5 %) of the best is an admissible
	// epicentre.
	unc := step2 / 2
	for _, nd := range nodes {
		if nd.score >= best.score-math.Max(1, 0.05*best.score) {
			if d := math.Hypot(nd.x-best.x, nd.y-best.y); d > unc {
				unc = d
			}
		}
	}

	f := Fit{X: best.x, Y: best.y, OriginT: best.t0, RMS: best.rms, LocUncKM: unc, Nodes: len(nodes), HalfWidth: half}
	classify(cfg, obs, &f)
	return f
}

// classify fills per-observation inlier/phase/residual/distance for the
// hypothesis already in f.
func classify(cfg *Config, obs []Obs, f *Fit) {
	f.Inlier, f.Phase = make([]bool, len(obs)), make([]byte, len(obs))
	f.Residual, f.Hypo = make([]float64, len(obs)), make([]float64, len(obs))
	f.NInliers = 0
	h2 := cfg.DepthKM * cfg.DepthKM
	for i, o := range obs {
		r := math.Sqrt((f.X-o.X)*(f.X-o.X) + (f.Y-o.Y)*(f.Y-o.Y) + h2)
		f.Hypo[i] = r
		rp := o.T - (f.OriginT + r/cfg.VpKMS)
		rs := o.T - (f.OriginT + r/cfg.VsKMS)
		res, ph := rp, byte('P')
		if math.Abs(rs) < math.Abs(rp) {
			res, ph = rs, 'S'
		}
		if math.Abs(res) <= o.Tau {
			f.Inlier[i] = true
			f.Phase[i] = ph
			f.Residual[i] = res
			f.NInliers++
		}
	}
}

// MaxLocateObs bounds the grid search's cost for very large events.
const MaxLocateObs = 250

// LocateLarge locates from at most MaxLocateObs of the earliest arrivals
// (nearest the source, so they constrain it best), then re-estimates the
// origin time and classifies every observation against that epicentre.
// Identical to Locate for smaller sets.
func LocateLarge(cfg *Config, obs []Obs) Fit {
	if len(obs) <= MaxLocateObs {
		return Locate(cfg, obs)
	}
	idx := make([]int, len(obs))
	for i := range idx {
		idx[i] = i
	}
	slices.SortStableFunc(idx, func(a, b int) int {
		switch {
		case obs[a].T < obs[b].T:
			return -1
		case obs[a].T > obs[b].T:
			return 1
		}
		return 0
	})
	sub := make([]Obs, MaxLocateObs)
	for i := range sub {
		sub[i] = obs[idx[i]]
	}
	f := Locate(cfg, sub)
	iv := make([]interval, 0, 4*len(obs))
	cover := make([]int, len(obs))
	_, t0, rms, _ := evalNode(cfg, obs, f.X, f.Y, iv, cover)
	f.OriginT, f.RMS = t0, rms
	classify(cfg, obs, &f)
	return f
}

// project converts lat/lng observations to the plane around their centroid.
func project(lats, lngs []float64) (lat0, lng0 float64, xs, ys []float64) {
	for i := range lats {
		lat0 += lats[i]
		lng0 += lngs[i]
	}
	lat0 /= float64(len(lats))
	lng0 /= float64(len(lngs))
	xs = make([]float64, len(lats))
	ys = make([]float64, len(lats))
	for i := range lats {
		xs[i], ys[i] = geo.LocalXY(lat0, lng0, lats[i], lngs[i])
	}
	return
}
