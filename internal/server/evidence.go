package server

import (
	"bufio"
	"encoding/json"
	"os"
	"sync"
	"time"

	"ne-pulse/internal/network"
	"ne-pulse/internal/station"
)

// evidenceLog appends every pipeline decision input and output to a JSONL
// file: station state changes (with location and source), picks, pick ends
// and event updates. cmd/nepulse-replay re-runs the network stage from it.
type evidenceLog struct {
	mu    sync.Mutex
	f     *os.File
	w     *bufio.Writer
	state map[string]station.State
	last  map[string]float64
}

func openEvidenceLog(path string) (*evidenceLog, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	el := &evidenceLog{f: f, w: bufio.NewWriterSize(f, 1<<16), state: map[string]station.State{}, last: map[string]float64{}}
	go func() {
		for range time.Tick(2 * time.Second) {
			el.mu.Lock()
			if el.w == nil {
				el.mu.Unlock()
				return
			}
			el.w.Flush()
			el.mu.Unlock()
		}
	}()
	return el, nil
}

func (el *evidenceLog) write(kind string, v map[string]any) {
	v["k"] = kind
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	el.mu.Lock()
	defer el.mu.Unlock()
	if el.w == nil {
		return
	}
	el.w.Write(b)
	el.w.WriteByte('\n')
}

// writeStatus logs a station status on state change, and at most once a
// minute otherwise (enough to reconstruct eligibility and sensitivity).
func (el *evidenceLog) writeStatus(dev string, info network.StationInfo, st station.Status) {
	el.mu.Lock()
	prev, seen := el.state[dev]
	lt := el.last[dev]
	if seen && prev == st.State && st.T-lt < 60 {
		el.mu.Unlock()
		return
	}
	el.state[dev] = st.State
	el.last[dev] = st.T
	el.mu.Unlock()
	el.write("status", map[string]any{"dev": dev, "info": info, "status": st})
}

func (el *evidenceLog) Close() {
	el.mu.Lock()
	defer el.mu.Unlock()
	if el.w != nil {
		el.w.Flush()
		el.f.Close()
		el.w = nil
	}
}
