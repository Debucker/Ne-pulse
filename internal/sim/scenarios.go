package sim

import (
	"fmt"
	"math"
	"math/rand"

	"ne-pulse/internal/geo"
	"ne-pulse/internal/network"
)

// T0 is the default scenario start (arbitrary fixed epoch for determinism).
const T0 = 1_800_000_000.0

// Home is the default study area (Tashkent).
var Home = [2]float64{41.30, 69.25}

// RandomPhone draws a device from a mix resembling real browser clients:
// 60 Hz (iOS Safari) and 50/100/200 Hz Android rates, 100-400 ug/sqrt(Hz)
// noise, and a fraction behind browsers that round output to 0.1 m/s^2.
func RandomPhone(rng *rand.Rand, coarseFrac float64) DeviceModel {
	rates := []float64{60, 60, 60, 50, 100, 200}
	dm := DeviceModel{
		RateHz:       rates[rng.Intn(len(rates))],
		JitterSec:    0.001 + rng.Float64()*0.004,
		NoiseDensity: (100 + rng.Float64()*300) * 1e-6 * G,
		RangeG:       []float64{2, 4, 8, 16}[rng.Intn(4)],
		ClockErrSec:  rng.NormFloat64() * 0.05,
		Orientation:  Euler(rng.Float64()*360, rng.NormFloat64()*2, rng.NormFloat64()*2),
		Bias:         Vec{rng.NormFloat64() * 0.05, rng.NormFloat64() * 0.05, rng.NormFloat64() * 0.05},
	}
	if rng.Float64() < coarseFrac {
		dm.Resolution = 0.1
	}
	return dm
}

// City places n devices around (lat, lng): a few Gaussian neighbourhoods of
// the given spread (km), like the clustering of real participants.
func City(rng *rand.Rand, prefix string, lat, lng float64, n int, spreadKM float64, coarseFrac float64) []*Device {
	type hood struct{ x, y, s float64 }
	var hoods []hood
	for i := 0; i < 6; i++ {
		hoods = append(hoods, hood{rng.NormFloat64() * spreadKM * 0.6, rng.NormFloat64() * spreadKM * 0.6, spreadKM * (0.2 + 0.3*rng.Float64())})
	}
	var out []*Device
	for i := 0; i < n; i++ {
		h := hoods[rng.Intn(len(hoods))]
		la, lo := geo.FromLocalXY(lat, lng, h.x+rng.NormFloat64()*h.s, h.y+rng.NormFloat64()*h.s)
		out = append(out, &Device{ID: fmt.Sprintf("%s%04d", prefix, i), Lat: la, Lng: lo, Model: RandomPhone(rng, coarseFrac), AgeSec: 3 * 86400})
	}
	return out
}

// Nuisances returns a random schedule of everyday disturbances at the given
// rate per hour: knocks and door slams, typing on the phone, vibration
// alerts, passing traffic, the phone being picked up and put back.
func Nuisances(rng *rand.Rand, t0, t1, perHour float64) []Source {
	var out []Source
	t := t0 + 45 // after warm-up, so nuisances are tested against eligible stations
	for {
		t += rng.ExpFloat64() * 3600 / perHour
		if t > t1 {
			return out
		}
		switch rng.Intn(6) {
		case 0: // knock / door slam / something dropped nearby
			out = append(out, Impulse{T: t, A: 0.3 + rng.Float64()*2, F: 6 + rng.Float64()*14, Tau: 0.02 + rng.Float64()*0.08, Dir: Vec{rng.Float64(), rng.Float64(), 1}})
		case 1: // typing on the phone lying on a table
			for k := 0; k < 8+rng.Intn(20); k++ {
				out = append(out, Impulse{T: t + float64(k)*0.25 + rng.Float64()*0.1, A: 0.1 + rng.Float64()*0.3, F: 15, Tau: 0.015, Dir: Vec{0, 0, 1}})
			}
		case 2: // vibration alert (incoming message/call)
			out = append(out, Vibration{T: t, On: 0.3 + rng.Float64()*0.4, Period: 1 + rng.Float64(), F: 150 + rng.Float64()*100, A: 0.5 + rng.Float64()*3, Count: 1 + rng.Intn(5)})
		case 3: // heavy vehicle / machinery nearby
			out = append(out, Emergent(rng, t, 1+rng.Float64()*4, 2+rng.Float64()*6, 0.05+rng.Float64()*0.3, 3, 12))
		case 4: // picked up and put down again
			out = append(out, Pickup{T: t, Dur: 0.6 + rng.Float64(), TiltDeg: 20 + rng.Float64()*60, HandAccel: 1 + rng.Float64()*2})
		case 5: // footsteps / someone sitting down on the bed
			out = append(out, Emergent(rng, t, 0.2, 0.8+rng.Float64()*2, 0.05+rng.Float64()*0.4, 1.5, 8))
		}
	}
}

func addNuisances(rng *rand.Rand, ds []*Device, t0, t1, perHour float64) {
	for _, d := range ds {
		d.Sources = append(d.Sources, Nuisances(rng, t0, t1, perHour)...)
	}
}

// Suite returns the standard scenario suite (the fourteen situations of the
// project brief plus adversarial ones).
func Suite() []Scenario {
	var out []Scenario
	cityQuake := func(seed int64, n int, pga float64) (Scenario, *rand.Rand) {
		rng := rand.New(rand.NewSource(seed))
		ds := City(rng, "c", Home[0], Home[1], n, 8, 0.25)
		addNuisances(rng, ds, T0, T0+300, 2)
		qlat, qlng := geo.FromLocalXY(Home[0], Home[1], 4, -3)
		q := DefaultQuake(qlat, qlng, T0+150, pga)
		return Scenario{Seed: seed, Start: T0, Duration: 300, Devices: ds, Quakes: []Quake{q},
			Expect: Expectation{MinDetectedLevel: network.LevelConfirmed, DetectAll: true}}, rng
	}

	// 1. One noisy phone.
	{
		rng := rand.New(rand.NewSource(1))
		ds := City(rng, "n", Home[0], Home[1], 1, 1, 0)
		addNuisances(rng, ds, T0, T0+1800, 40)
		out = append(out, Scenario{Name: "01-one-noisy-phone", Description: "One phone, 40 everyday disturbances per hour for 30 min.",
			Seed: 1, Start: T0, Duration: 1800, Devices: ds})
	}
	// 2. Ten unrelated phones.
	{
		rng := rand.New(rand.NewSource(2))
		ds := City(rng, "u", Home[0], Home[1], 10, 8, 0.25)
		addNuisances(rng, ds, T0, T0+1800, 12)
		out = append(out, Scenario{Name: "02-ten-unrelated-phones", Description: "10 phones, independent disturbances (12/h each), 30 min.",
			Seed: 2, Start: T0, Duration: 1800, Devices: ds})
	}
	// 3. A hundred unrelated phones.
	{
		rng := rand.New(rand.NewSource(3))
		ds := City(rng, "u", Home[0], Home[1], 100, 8, 0.25)
		addNuisances(rng, ds, T0, T0+1800, 8)
		out = append(out, Scenario{Name: "03-hundred-unrelated-phones", Description: "100 phones, independent disturbances (8/h each), 30 min.",
			Seed: 3, Start: T0, Duration: 1800, Devices: ds})
	}
	// 4. A genuine earthquake.
	{
		sc, _ := cityQuake(4, 300, 2)
		sc.Name, sc.Description = "04-genuine-quake", "300 phones in a city, moderate quake (2 m/s² peak S at 10 km) inside the network."
		out = append(out, sc)
	}
	// 5. Weak earthquake.
	{
		sc, _ := cityQuake(5, 300, 0.06)
		sc.Name, sc.Description = "05-weak-quake", "Same network, weak quake (0.06 m/s² ≈ 0.6 %g at 10 km, MMI ~III): below most phones' threshold."
		sc.Expect = Expectation{}
		out = append(out, sc)
	}
	// 6. Strong earthquake.
	{
		sc, _ := cityQuake(6, 300, 8)
		sc.Name, sc.Description = "06-strong-quake", "Same network, strong quake (8 m/s² at 10 km): clipping, phones sliding."
		out = append(out, sc)
	}
	// 7. One false-positive cluster (one building).
	{
		rng := rand.New(rand.NewSource(7))
		ds := City(rng, "c", Home[0], Home[1], 200, 8, 0.25)
		addNuisances(rng, ds, T0, T0+600, 2)
		blat, blng := geo.FromLocalXY(Home[0], Home[1], 1, 1)
		for i := 0; i < 40; i++ {
			d := &Device{ID: fmt.Sprintf("b%03d", i), Lat: blat + rng.NormFloat64()*0.0005, Lng: blng + rng.NormFloat64()*0.0005, Model: RandomPhone(rng, 0.25), AgeSec: 3 * 86400}
			for k := 0; k < 3; k++ { // furniture moved three times
				d.Sources = append(d.Sources, Emergent(rand.New(rand.NewSource(int64(700+k))), T0+200+float64(k)*90+rng.Float64()*0.2, 0.3, 1.5, 0.4, 2, 10))
			}
			ds = append(ds, d)
		}
		out = append(out, Scenario{Name: "07-one-building-cluster", Description: "40 phones in one building register furniture moving 3 times, 200 phones elsewhere.",
			Seed: 7, Start: T0, Duration: 600, Devices: ds})
	}
	// 8. Several geographically separated clusters.
	{
		rng := rand.New(rand.NewSource(8))
		var ds []*Device
		for c, off := range [][2]float64{{0, 0}, {45, 10}, {-20, 50}} {
			la, lo := geo.FromLocalXY(Home[0], Home[1], off[0], off[1])
			town := City(rng, fmt.Sprintf("t%d", c), la, lo, 60, 4, 0.25)
			for _, d := range town[:20] { // a building-scale event in each town, simultaneously
				d.Lat, d.Lng = la+rng.NormFloat64()*0.0004, lo+rng.NormFloat64()*0.0004
				d.Sources = append(d.Sources, Emergent(rand.New(rand.NewSource(int64(800+c))), T0+240+rng.Float64()*0.3, 0.3, 2, 0.5, 2, 10))
			}
			ds = append(ds, town...)
		}
		out = append(out, Scenario{Name: "08-separated-clusters", Description: "Three towns 45-55 km apart; a building-scale disturbance hits 20 phones in each at the same moment.",
			Seed: 8, Start: T0, Duration: 480, Devices: ds})
	}
	// 9. Missing telemetry.
	{
		sc, rng := cityQuake(9, 300, 2)
		sc.Name, sc.Description = "09-missing-telemetry", "Genuine quake; 30% of phones drop out from 5 s before to 20 s after the origin."
		for _, d := range sc.Devices {
			if rng.Float64() < 0.3 {
				d.Offline = [][2]float64{{sc.Quakes[0].OriginT - 5, sc.Quakes[0].OriginT + 20}}
			}
		}
		sc.Expect.MinDetectedLevel = network.LevelCorroborated
		out = append(out, sc)
	}
	// 10. Delayed telemetry.
	{
		sc, rng := cityQuake(10, 300, 2)
		sc.Name, sc.Description = "10-delayed-telemetry", "Genuine quake; half the phones deliver everything 20-40 s late."
		for _, d := range sc.Devices {
			if rng.Float64() < 0.5 {
				d.DelaySec = 20 + rng.Float64()*20
			}
		}
		out = append(out, sc)
	}
	// 11. Duplicate telemetry.
	{
		sc, _ := cityQuake(11, 300, 2)
		sc.Name, sc.Description = "11-duplicate-telemetry", "Genuine quake; every report is delivered twice."
		for _, d := range sc.Devices {
			d.Duplicate = true
		}
		out = append(out, sc)
	}
	// 12. Incorrect timestamps.
	{
		sc, rng := cityQuake(12, 300, 2)
		sc.Name, sc.Description = "12-incorrect-timestamps", "Genuine quake; 25% of phones have clocks off by 2-8 s despite synchronisation."
		for _, d := range sc.Devices {
			if rng.Float64() < 0.25 {
				d.Model.ClockErrSec = (2 + rng.Float64()*6) * float64(1-2*rng.Intn(2))
			}
		}
		out = append(out, sc)
	}
	// 13. Offline devices.
	{
		sc, rng := cityQuake(13, 300, 2)
		sc.Name, sc.Description = "13-offline-devices", "Genuine quake; half the phones went offline a minute before it."
		for _, d := range sc.Devices {
			if rng.Float64() < 0.5 {
				d.Offline = [][2]float64{{sc.Quakes[0].OriginT - 60, sc.Start + sc.Duration}}
			}
		}
		out = append(out, sc)
	}
	// 14. Sudden network spike.
	{
		sc, rng := cityQuake(14, 150, 2)
		sc.Name, sc.Description = "14-network-spike", "150 established phones; 1000 new phones join within 10 s, 15 s before a genuine quake."
		spike := City(rng, "s", Home[0], Home[1], 1000, 8, 0.25)
		for _, d := range spike {
			d.JoinT = sc.Quakes[0].OriginT - 15 - rng.Float64()*10
			d.AgeSec = 0
		}
		sc.Devices = append(sc.Devices, spike...)
		out = append(out, sc)
	}
	// X1. Sonic boom / explosion sweeping the city at the speed of sound.
	{
		rng := rand.New(rand.NewSource(21))
		ds := City(rng, "c", Home[0], Home[1], 300, 8, 0.25)
		slat, slng := geo.FromLocalXY(Home[0], Home[1], -25, 0)
		for i, d := range ds {
			dist := geo.HaversineKM(slat, slng, d.Lat, d.Lng)
			d.Sources = append(d.Sources, Emergent(rand.New(rand.NewSource(int64(2100+i))), T0+150+dist/0.343, 0.05, 0.6, 0.3+rng.Float64()*0.3, 4, 12))
		}
		out = append(out, Scenario{Name: "X1-sonic-boom", Description: "An acoustic front (0.343 km/s) crosses a 300-phone city and shakes every building.",
			Seed: 21, Start: T0, Duration: 400, Devices: ds})
	}
	// X2. Heavy traffic crossing the city.
	{
		rng := rand.New(rand.NewSource(22))
		ds := City(rng, "c", Home[0], Home[1], 300, 8, 0.25)
		for i, d := range ds {
			x, y := geo.LocalXY(Home[0], Home[1], d.Lat, d.Lng)
			if math.Abs(y) < 0.4 { // within 400 m of the road
				d.Sources = append(d.Sources, Emergent(rand.New(rand.NewSource(int64(2200+i))), T0+120+(x+15)/0.015, 2, 4, 0.3, 3, 12))
			}
		}
		out = append(out, Scenario{Name: "X2-traffic-convoy", Description: "A heavy convoy drives through the city at 54 km/h shaking phones near its route.",
			Seed: 22, Start: T0, Duration: 2400, Devices: ds})
	}
	// X3. Sybil attack from one network.
	{
		rng := rand.New(rand.NewSource(23))
		ds := City(rng, "c", Home[0], Home[1], 200, 8, 0.25)
		flat, flng := geo.FromLocalXY(Home[0], Home[1], 0, 25)
		fake := DefaultQuake(flat, flng, T0+150, 3)
		for i := 0; i < 60; i++ {
			la, lo := geo.FromLocalXY(flat, flng, rng.NormFloat64()*6, rng.NormFloat64()*6)
			d := &Device{ID: fmt.Sprintf("sy%03d", i), Lat: la, Lng: lo, Model: TypicalPhone(rng.Float64() * 360), Source: "attacker-network", AgeSec: 3600}
			// fabricated but physically consistent waveforms, fed to a real processor
			d.Sources = append(d.Sources, QuakeAt(rand.New(rand.NewSource(int64(2300+i))), fake, la, lo))
			ds = append(ds, d)
		}
		out = append(out, Scenario{Name: "X3-sybil-one-network", Description: "60 fake phones on one network stream physically consistent fabricated quake waveforms.",
			Seed: 23, Start: T0, Duration: 400, Devices: ds})
	}
	// X4. Sybil attack from many networks, fresh devices.
	{
		rng := rand.New(rand.NewSource(24))
		ds := City(rng, "c", Home[0], Home[1], 200, 8, 0.25)
		flat, flng := geo.FromLocalXY(Home[0], Home[1], 0, 25)
		fake := DefaultQuake(flat, flng, T0+150, 3)
		for i := 0; i < 60; i++ {
			la, lo := geo.FromLocalXY(flat, flng, rng.NormFloat64()*6, rng.NormFloat64()*6)
			d := &Device{ID: fmt.Sprintf("sy%03d", i), Lat: la, Lng: lo, Model: TypicalPhone(rng.Float64() * 360), Source: fmt.Sprintf("vpn-%d", i%10), AgeSec: 3600}
			d.Sources = append(d.Sources, QuakeAt(rand.New(rand.NewSource(int64(2400+i))), fake, la, lo))
			ds = append(ds, d)
		}
		out = append(out, Scenario{Name: "X4-sybil-ten-networks", Description: "Same fabricated quake from 60 fake phones spread over 10 networks (residual-risk case).",
			Seed: 24, Start: T0, Duration: 400, Devices: ds, Expect: Expectation{MaxFalseCorroborated: 1}})
	}
	// X5. A crowd walking with phones in hand.
	{
		rng := rand.New(rand.NewSource(25))
		ds := City(rng, "w", Home[0], Home[1], 400, 3, 0.25)
		for i, d := range ds {
			if i%2 == 0 {
				d.Sources = append(d.Sources, Walking{T0: T0, T1: T0 + 900, StepHz: 1.6 + rng.Float64()*0.6, Impact: 2 + rng.Float64()*2})
			} else {
				d.Sources = append(d.Sources, &Handheld{T0: T0, T1: T0 + 900, Seed: int64(i)})
			}
		}
		out = append(out, Scenario{Name: "X5-crowd-walking", Description: "400 people walking or holding their phones (a Telegram crowd opening the link on the street).",
			Seed: 25, Start: T0, Duration: 900, Devices: ds})
	}
	// X6. One person shaking their phone, another sliding theirs on a table.
	{
		rng := rand.New(rand.NewSource(26))
		ds := City(rng, "c", Home[0], Home[1], 200, 8, 0.25)
		addNuisances(rng, ds, T0, T0+600, 2)
		ds[0].Sources = append(ds[0].Sources, Shake{T: T0 + 200, Dur: 8, Hz: 4, A: 12, WristDeg: 8})
		ds[1].Sources = append(ds[1].Sources, Shake{T: T0 + 320, Dur: 6, Hz: 3, A: 6, WristDeg: 2})
		ds[2].Sources = append(ds[2].Sources, Slide{T: T0 + 260, Dur: 10, Hz: 1.5, A: 3})
		out = append(out, Scenario{Name: "X6-shaking-and-sliding", Description: "Individual phones deliberately shaken in the hand or slid back and forth on a table.",
			Seed: 26, Start: T0, Duration: 600, Devices: ds})
	}
	// X7. A coordinated prank: established participants across the city all
	// shake their phones on a countdown, within about a second of each
	// other, while everyone else's phone stays quiet.
	for _, pr := range []struct {
		name  string
		frac  float64
		sync  float64
		seed  int64
	}{{"X7-prank-countdown-20pct", 0.2, 1.0, 27}, {"X8-prank-countdown-40pct", 0.4, 0.5, 28}} {
		rng := rand.New(rand.NewSource(pr.seed))
		ds := City(rng, "c", Home[0], Home[1], 300, 8, 0.25)
		addNuisances(rng, ds, T0, T0+400, 2)
		for _, d := range ds {
			if rng.Float64() < pr.frac {
				d.Sources = append(d.Sources, Shake{T: T0 + 200 + rng.NormFloat64()*pr.sync, Dur: 5 + rng.Float64()*5, Hz: 2.5 + rng.Float64()*3,
					A: 4 + rng.Float64()*10, WristDeg: rng.Float64() * 6})
			}
		}
		out = append(out, Scenario{Name: pr.name, Description: fmt.Sprintf("%.0f%% of 300 established participants shake their phones on a countdown (±%.1f s); the rest stay quiet.", pr.frac*100, pr.sync),
			Seed: pr.seed, Start: T0, Duration: 400, Devices: ds})
	}
	// X6. One person shaking their phone, another sliding theirs on a table.
	{
		rng := rand.New(rand.NewSource(26))
		ds := City(rng, "c", Home[0], Home[1], 200, 8, 0.25)
		addNuisances(rng, ds, T0, T0+600, 2)
		ds[0].Sources = append(ds[0].Sources, Shake{T: T0 + 200, Dur: 8, Hz: 4, A: 12, WristDeg: 8})
		ds[1].Sources = append(ds[1].Sources, Shake{T: T0 + 320, Dur: 6, Hz: 3, A: 6, WristDeg: 2})
		ds[2].Sources = append(ds[2].Sources, Slide{T: T0 + 260, Dur: 10, Hz: 1.5, A: 3})
		out = append(out, Scenario{Name: "X6-shaking-and-sliding", Description: "Individual phones deliberately shaken in the hand or slid back and forth on a table.",
			Seed: 26, Start: T0, Duration: 600, Devices: ds})
	}
	// X7. A coordinated prank: established participants across the city all
	// shake their phones on a countdown, within about a second of each
	// other, while everyone else's phone stays quiet.
	for _, pr := range []struct {
		name  string
		frac  float64
		sync  float64
		seed  int64
	}{{"X7-prank-countdown-20pct", 0.2, 1.0, 27}, {"X8-prank-countdown-40pct", 0.4, 0.5, 28}} {
		rng := rand.New(rand.NewSource(pr.seed))
		ds := City(rng, "c", Home[0], Home[1], 300, 8, 0.25)
		addNuisances(rng, ds, T0, T0+400, 2)
		for _, d := range ds {
			if rng.Float64() < pr.frac {
				d.Sources = append(d.Sources, Shake{T: T0 + 200 + rng.NormFloat64()*pr.sync, Dur: 5 + rng.Float64()*5, Hz: 2.5 + rng.Float64()*3,
					A: 4 + rng.Float64()*10, WristDeg: rng.Float64() * 6})
			}
		}
		out = append(out, Scenario{Name: pr.name, Description: fmt.Sprintf("%.0f%% of 300 established participants shake their phones on a countdown (±%.1f s); the rest stay quiet.", pr.frac*100, pr.sync),
			Seed: pr.seed, Start: T0, Duration: 400, Devices: ds})
	}
	return out
}
