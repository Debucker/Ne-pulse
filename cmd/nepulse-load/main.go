// Command nepulse-load stress-tests a running NE-PULSE server over the real
// device protocol: it enrolls N devices (solving proof-of-work), opens N
// WebSocket sessions and streams realistic 60 Hz accelerometer batches
// once a second, then reports throughput, acknowledgement latency and the
// server's own pipeline metrics.
//
// For a local test run the server with a low -pow-difficulty, a high
// -enroll-per-ip-hour and -client-ip-header X-Real-IP (so simulated devices
// appear to come from distinct networks).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ne-pulse/internal/client"
	"ne-pulse/internal/dsp"
	"ne-pulse/internal/geo"
	"ne-pulse/internal/protocol"
	"ne-pulse/internal/sim"
)

func main() {
	base := flag.String("url", "http://localhost:8080", "server base URL")
	n := flag.Int("devices", 1000, "simulated devices")
	dur := flag.Duration("duration", 2*time.Minute, "streaming duration")
	rate := flag.Float64("rate", 60, "samples per second per device")
	ramp := flag.Duration("ramp", 20*time.Second, "time over which devices connect")
	admin := flag.String("admin-token", "", "admin token to read server metrics")
	quakeAt := flag.Duration("quake-at", 0, "if set, inject a synthetic earthquake this long after start (local demos only)")
	pga := flag.Float64("pga", 2, "synthetic quake: peak S acceleration at 10 km (m/s^2)")
	spread := flag.Float64("spread", 8, "device spread around the centre (km)")
	quakeEvery := flag.Duration("quake-every", 0, "if set, inject a new synthetic earthquake at a random nearby epicentre this often (local demos only)")
	ipHdr = flag.String("ip-header", "X-Real-Ip", "header carrying the simulated client IP (must match the server's -client-ip-header)")
	flag.Parse()

	ctx := context.Background()
	ws := "ws" + strings.TrimPrefix(*base, "http") + "/ws/device"
	grid := geo.Grid{CellKM: 1}
	hc := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 256}}

	log.Printf("enrolling %d devices...", *n)
	creds := make([]client.Creds, *n)
	var wg sync.WaitGroup
	sem := make(chan struct{}, 64)
	var enrollErr atomic.Int64
	t0 := time.Now()
	for i := 0; i < *n; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			c, err := client.Enroll(ctx, hc, *base, "android", ipHeader(i))
			if err != nil {
				if enrollErr.Add(1) == 1 {
					log.Printf("first enrol error: %v", err)
				}
				return
			}
			creds[i] = c
		}(i)
	}
	wg.Wait()
	log.Printf("enrolled in %s (%d errors)", time.Since(t0).Round(time.Millisecond), enrollErr.Load())

	var sent, connected, failed, closed atomic.Int64
	stop := time.Now().Add(*ramp + *dur)
	// Synthetic quakes: the current list is swapped atomically; every device
	// rebuilds its ground-motion sources when the list version changes.
	var quakes atomic.Pointer[[]sim.Quake]
	empty := []sim.Quake{}
	quakes.Store(&empty)
	schedule := func(at time.Time, x, y float64) {
		qlat, qlng := geo.FromLocalXY(41.3, 69.25, x, y)
		q := sim.DefaultQuake(qlat, qlng, float64(at.UnixNano())/1e9, *pga)
		list := append([]sim.Quake{}, (*quakes.Load())...)
		list = append(list, q)
		if len(list) > 2 {
			list = list[len(list)-2:]
		}
		quakes.Store(&list)
		log.Printf("synthetic quake scheduled at %s (%.3f, %.3f), PGA_ref %.2f m/s^2", at.Format(time.TimeOnly), qlat, qlng, *pga)
	}
	if *quakeAt > 0 {
		schedule(time.Now().Add(*quakeAt), 3, -2)
	}
	if *quakeEvery > 0 {
		go func() {
			rng := rand.New(rand.NewSource(time.Now().UnixNano()))
			for range time.Tick(*quakeEvery) {
				schedule(time.Now().Add(20*time.Second), (rng.Float64()-0.5)*16, (rng.Float64()-0.5)*16)
			}
		}()
	}
	for i := 0; i < *n; i++ {
		if creds[i].DeviceID == "" {
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			time.Sleep(time.Duration(float64(*ramp) * float64(i) / float64(*n)))
			rng := rand.New(rand.NewSource(int64(i)))
			lat, lng := geo.FromLocalXY(41.3, 69.25, rng.NormFloat64()**spread, rng.NormFloat64()**spread)
			dm := sim.TypicalPhone(rng.Float64() * 360)
			dm.RateHz = *rate
			var srcs []sim.Source
			var seen *[]sim.Quake
			rebuild := func() {
				cur := quakes.Load()
				if cur == seen {
					return
				}
				seen = cur
				srcs = srcs[:0]
				for qi, q := range *cur {
					if src := sim.QuakeAt(rand.New(rand.NewSource(int64(i*131+qi)+int64(q.OriginT))), q, lat, lng); src != nil {
						srcs = append(srcs, src)
					}
				}
			}
			rebuild()
			c := grid.CellOf(lat, lng)
			conn, err := client.Dial(ctx, ws, creds[i], protocol.Location{GridKM: 1, Row: c.Row, Col: c.Col}, "android", ipHeader(i))
			if err != nil {
				failed.Add(1)
				return
			}
			connected.Add(1)
			defer conn.Close()
			conn.Sync()
			tick := time.NewTicker(time.Second)
			defer tick.Stop()
			last := float64(time.Now().UnixNano()) / 1e9
			for now := range tick.C {
				if now.After(stop) {
					return
				}
				select {
				case <-conn.Done():
					closed.Add(1)
					return
				default:
				}
				t := float64(now.UnixNano()) / 1e9
				rebuild()
				var batch []dsp.Sample3
				sim.Stream(rng, dm, srcs, last, t, func(s dsp.Sample3) { batch = append(batch, s) })
				last = t
				if err := conn.SendSamples(batch, 0, 20); err != nil {
					return
				}
				sent.Add(1)
			}
		}(i)
	}

	report := time.NewTicker(10 * time.Second)
	defer report.Stop()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	for {
		select {
		case <-report.C:
			log.Printf("connected=%d failed=%d closed=%d batches sent=%d (%.0f/s)", connected.Load(), failed.Load(), closed.Load(), sent.Load(),
				float64(sent.Load())/time.Since(t0).Seconds())
			if *admin != "" {
				printMetrics(hc, *base, *admin)
			}
		case <-done:
			log.Printf("finished: connected=%d failed=%d batches=%d", connected.Load(), failed.Load(), sent.Load())
			if *admin != "" {
				printMetrics(hc, *base, *admin)
			}
			return
		}
	}
}

var ipHdr *string

func ipHeader(i int) http.Header {
	// one /24 per simulated device, like phones on separate home networks
	return http.Header{*ipHdr: {fmt.Sprintf("10.%d.%d.7", (i>>8)&255, i&255)}}
}

func printMetrics(hc *http.Client, base, token string) {
	req, _ := http.NewRequest("GET", base+"/api/admin/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := hc.Do(req)
	if err != nil {
		log.Printf("metrics: %v", err)
		return
	}
	defer resp.Body.Close()
	var m map[string]float64
	json.NewDecoder(resp.Body).Decode(&m)
	log.Printf("server: connected=%.0f eligible=%.0f samples=%.0f batches=%.0f dropped=%.0f engineDropped=%.0f lag p50=%.2fs p95=%.2fs tick p95=%.1fms",
		m["nepulse_devices_connected"], m["nepulse_devices_eligible"], m["nepulse_samples_total"], m["nepulse_batches_total"],
		m["nepulse_batches_dropped_total"], m["nepulse_engine_dropped_total"], m["nepulse_ingest_lag_seconds_p50"],
		m["nepulse_ingest_lag_seconds_p95"], 1000*m["nepulse_engine_tick_seconds_p95"])
}
