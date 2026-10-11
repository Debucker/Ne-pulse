// Package server wires the NE-PULSE pipeline into a network service:
//
//	device WebSocket -> session (auth, validation, dedup, rate limits)
//	  -> station shard (one station.Processor per device, single owner)
//	  -> network engine goroutine (association, significance, lifecycle)
//	  -> store / alerts / snippet capture / public aggregation
//
// Every piece of mutable pipeline state has exactly one owning goroutine;
// goroutines communicate over bounded channels, and every drop is counted.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"ne-pulse/internal/alert"
	"ne-pulse/internal/catalog"
	"ne-pulse/internal/geo"
	"ne-pulse/internal/metrics"
	"ne-pulse/internal/network"
	"ne-pulse/internal/ratelimit"
	"ne-pulse/internal/record"
	"ne-pulse/internal/security"
	"ne-pulse/internal/station"
	"ne-pulse/internal/store"
)

// CatalogConfig controls external-catalog validation.
type CatalogConfig struct {
	Enabled bool
	Region  *catalog.Region // nil: derive from network coverage (+ margin)
	Every   time.Duration
	MinMag  float64
	Client  *catalog.Client
}

// Config is the full server configuration.
type Config struct {
	Station station.Config
	Network network.Config

	Shards       int
	LocGridKM    float64 // device location quantisation (on the phone)
	PublicGridKM float64 // public map aggregation
	KAnon        int     // public cells with fewer sensors are hidden
	BatchMS      int

	AllowedOrigins []string
	ClientIP       security.ClientIP
	Secret         string

	// TelegramBot is the alert bot's username (without @), published so the
	// site can offer a one-tap subscription link; AlertRadiusKM is its range.
	TelegramBot   string
	AlertRadiusKM float64

	// Research recording: devices with a registered class code (cohort)
	// have their raw samples written under RecordDir, kept RecordDays days.
	RecordDir  string
	RecordDays int
	Cohorts    []string // codes registered at startup
	// RecordMaxGB stops recording (cleanly, counted) once RecordDir holds
	// this many gigabytes; 0 = no limit.
	RecordMaxGB float64
	// NightMinHours: listening hours in one night that earn a participation code.
	NightMinHours float64

	PoWDifficulty      int
	EnrollPerIPPerHour float64
	HTTPRatePerSec     float64
	MaxConnsPerIP      int
	MaxPublicConns     int
	MaxPublicPerIP     int
	MaxSamplesPerSec   float64

	SnippetPreSec  float64
	SnippetPostSec float64
	Retention      store.Retention

	PublicBaseURL string
	AdminToken    string
	Catalog       CatalogConfig
	EvidenceLog   string
	ConfigHash    string

	// TickInterval is the real-time period of the engine loop (1 s in
	// production). Now overrides the clock; with a sped-up clock and a
	// shorter tick the whole pipeline can be tested faster than real time.
	TickInterval time.Duration
	Now          func() float64
}

// DefaultConfig returns production defaults.
func DefaultConfig() Config {
	return Config{
		Station:       station.DefaultConfig(),
		Network:       network.DefaultConfig(),
		Shards:        8,
		LocGridKM:     1,
		PublicGridKM:  10,
		KAnon:         3,
		BatchMS:       1000,
		PoWDifficulty: 18,
		// Per-IP limits are generous on purpose: mobile carriers put many
		// users behind one address (carrier-grade NAT), and a group joining
		// from one Telegram post arrives at once. Proof of work, not the
		// IP, is what makes mass enrollment expensive.
		EnrollPerIPPerHour: 300,
		HTTPRatePerSec:     25,
		MaxConnsPerIP:      1000,
		MaxPublicConns:     5000,
		MaxPublicPerIP:     300,
		MaxSamplesPerSec:   250,
		SnippetPreSec:      10,
		SnippetPostSec:     45,
		Retention:          store.Retention{Picks: 180 * 86400, Snippets: 365 * 86400, Stats: 365 * 86400, IdleDevices: 180 * 86400},
		Catalog:            CatalogConfig{Every: 5 * time.Minute, MinMag: 2.5},
	}
}

// Server is the running service.
type Server struct {
	cfg   Config
	st    *store.Store
	keys  *security.Keys
	pow   *security.PoW
	src   *security.SourceHasher
	reg   *metrics.Registry
	m     *serverMetrics
	disp  *alert.Dispatcher
	grid  geo.Grid
	pgrid geo.Grid

	shards []*shard
	engIn  chan engMsg

	upgrader  websocket.Upgrader
	linksOnce sync.Once
	tl        *telegramLinks
	sessMu    sync.Mutex
	sessions  map[string]*session
	ipConns   map[string]int

	pubMu      sync.Mutex
	pubClients map[*pubClient]struct{}
	pubIP      map[string]int

	snapshot  atomic.Pointer[[]byte]
	coverage  atomic.Pointer[catalog.Region]
	adminView atomic.Pointer[adminSnapshot]

	recent   *recentEvents
	pickHist *pickHistory

	httpLimiter   *ratelimit.Limiter
	enrollLimiter *ratelimit.Limiter
	evlog         *evidenceLog
	rec           *record.Recorder

	startedAt time.Time
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	origins   map[string]bool
}

// New builds a server around an open store.
func New(cfg Config, st *store.Store, channels ...alert.Channel) (*Server, error) {
	keys, stable := security.NewKeys(cfg.Secret)
	if !stable {
		log.Println("warning: no -secret configured; enrollment challenges and source hashes will not survive a restart")
	}
	reg := metrics.NewRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		cfg: cfg, st: st, keys: keys,
		pow:           security.NewPoW(keys, cfg.PoWDifficulty, 5*time.Minute),
		src:           security.NewSourceHasher(keys),
		reg:           reg,
		m:             newServerMetrics(reg),
		grid:          geo.Grid{CellKM: cfg.LocGridKM},
		pgrid:         geo.Grid{CellKM: cfg.PublicGridKM},
		engIn:         make(chan engMsg, 65536),
		sessions:      map[string]*session{},
		ipConns:       map[string]int{},
		pubClients:    map[*pubClient]struct{}{},
		pubIP:         map[string]int{},
		httpLimiter:   ratelimit.New(cfg.HTTPRatePerSec, int(cfg.HTTPRatePerSec*3)),
		enrollLimiter: ratelimit.New(cfg.EnrollPerIPPerHour/3600, int(cfg.EnrollPerIPPerHour/4)+1),
		startedAt:     time.Now(),
		ctx:           ctx,
		cancel:        cancel,
		origins:       map[string]bool{},
		recent:        &recentEvents{evs: map[string]PublicEvent{}},
		pickHist:      &pickHistory{},
	}
	for _, o := range cfg.AllowedOrigins {
		s.origins[strings.TrimSpace(o)] = true
	}
	// Compression is off: gorilla's read limit counts compressed bytes, so a
	// small deflated frame could inflate to tens of MB (decompression bomb).
	s.upgrader = websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096, CheckOrigin: s.originOK}
	s.disp = alert.NewDispatcher(st, cfg.PublicBaseURL, 12, channels...)
	s.disp.OnResult = func(ch string, ok bool) {
		if ok {
			s.m.alertsSent.Inc()
		} else {
			s.m.alertsFailed.Inc()
		}
	}
	if cfg.RecordDir != "" {
		rec, err := record.New(cfg.RecordDir, keys.Derive("record-pseudonym"))
		if err != nil {
			return nil, err
		}
		s.rec = rec
		if cfg.RecordMaxGB > 0 {
			rec.LimitBytes(int64(cfg.RecordMaxGB*(1<<30)), s.ctx)
		}
		if cfg.RecordDays > 0 {
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				t := time.NewTicker(6 * time.Hour)
				defer t.Stop()
				for {
					rec.Prune(cfg.RecordDays, time.Now())
					select {
					case <-t.C:
					case <-s.ctx.Done():
						return
					}
				}
			}()
		}
	}
	for _, c := range cfg.Cohorts {
		code, ok := store.NormCohort(c)
		if !ok || code == "" {
			return nil, fmt.Errorf("invalid class code %q (2-24 characters: A-Z, 0-9, dash)", c)
		}
		if err := st.AddCohort(context.Background(), code, "", float64(time.Now().Unix())); err != nil {
			return nil, err
		}
	}
	if cfg.EvidenceLog != "" {
		el, err := openEvidenceLog(cfg.EvidenceLog)
		if err != nil {
			return nil, err
		}
		s.evlog = el
	}
	n := cfg.Shards
	if n < 1 {
		n = 1
	}
	for i := 0; i < n; i++ {
		s.shards = append(s.shards, newShard(s, i))
	}
	empty := []byte(`{"type":"network","stats":{},"cells":[],"trace":[],"events":[]}`)
	s.snapshot.Store(&empty)
	return s, nil
}

// Metrics exposes the registry (tests, monitoring).
func (s *Server) Metrics() *metrics.Registry { return s.reg }

// Start launches every background goroutine.
func (s *Server) Start() error {
	eng := network.NewEngine(s.cfg.Network)
	if units, err := s.st.LoadUnits(s.ctx); err == nil {
		states := make([]network.UnitState, len(units))
		for i, u := range units {
			states[i] = network.UnitState{Key: u.Key, Votes: u.Votes, Exposure: u.Exposure}
		}
		eng.ImportUnitStates(states)
		log.Printf("restored background history for %d units", len(units))
	} else {
		return err
	}
	for _, sh := range s.shards {
		s.wg.Add(1)
		go sh.run(s.ctx, &s.wg)
	}
	s.wg.Add(1)
	go s.engineLoop(eng)
	go s.httpLimiter.Run(s.ctx)
	go s.enrollLimiter.Run(s.ctx)
	go s.maintenanceLoop()
	if s.cfg.Catalog.Enabled {
		go s.catalogLoop()
	}
	return nil
}

// Shutdown stops background work and closes client connections.
func (s *Server) Shutdown() {
	s.sessMu.Lock()
	for _, se := range s.sessions {
		se.close("server shutting down")
	}
	s.sessMu.Unlock()
	s.cancel()
	s.wg.Wait()
	if s.evlog != nil {
		s.evlog.Close()
	}
	if s.rec != nil {
		s.rec.Close()
	}
}

func (s *Server) now() float64 {
	if s.cfg.Now != nil {
		return s.cfg.Now()
	}
	return float64(time.Now().UnixNano()) / 1e9
}

func (s *Server) shardFor(dev string) *shard {
	h := fnv.New32a()
	h.Write([]byte(dev))
	return s.shards[int(h.Sum32())%len(s.shards)]
}

func (s *Server) originOK(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true // native clients, load generator: no browser to enforce CORS against
	}
	return s.origins[o]
}

// cellCenter returns the centre of a device location cell.
func (s *Server) cellCenter(l geo.Cell) (float64, float64) { return s.grid.Center(l) }

// ---- engine goroutine --------------------------------------------------------

type engKind int

const (
	engStatus engKind = iota
	engPick
	engEnd
	engRemove
)

type engMsg struct {
	kind   engKind
	dev    string
	info   network.StationInfo
	status station.Status
	pick   station.Pick
	end    station.PickEnd
	at     float64
}

func (s *Server) sendEng(m engMsg, mustDeliver bool) {
	if mustDeliver {
		select {
		case s.engIn <- m:
		case <-time.After(250 * time.Millisecond):
			s.m.engineDropped.Inc()
		}
		return
	}
	select {
	case s.engIn <- m:
	default:
		s.m.engineDropped.Inc()
	}
}

type snippetJob struct {
	due   float64
	event network.Event
}

func (s *Server) engineLoop(eng *network.Engine) {
	defer s.wg.Done()
	every := s.cfg.TickInterval
	if every <= 0 {
		every = time.Second
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	var jobs []snippetJob
	captured := map[string]bool{}
	lastPublic, lastMinute, lastUnits := 0.0, 0.0, s.now()
	trace := newTraceRing(300)
	var lastSamples int64
	for {
		select {
		case <-s.ctx.Done():
			units := eng.ExportUnitStates()
			rows := make([]store.UnitRow, len(units))
			for i, u := range units {
				rows[i] = store.UnitRow{Key: u.Key, Votes: u.Votes, Exposure: u.Exposure}
			}
			s.st.SaveUnits(rows, s.now())
			return
		case m := <-s.engIn:
			switch m.kind {
			case engStatus:
				eng.UpdateStation(m.info, m.status, m.at)
			case engPick:
				eng.AddPick(m.dev, m.pick, m.at)
			case engEnd:
				eng.EndPick(m.dev, m.end, m.at)
			case engRemove:
				eng.RemoveStation(m.dev)
			}
			s.m.engineQueue.Set(float64(len(s.engIn)))
		case <-tick.C:
			now := s.now()
			t0 := time.Now()
			ups := eng.Tick(now)
			s.m.tickSeconds.Observe(time.Since(t0).Seconds())
			for _, u := range ups {
				s.onEventUpdate(u, now)
				if rank := alert.Rank(u.Event.Level); rank >= 1 && !captured[u.Event.ID] {
					jobs = append(jobs, snippetJob{due: u.Event.OriginT + s.cfg.SnippetPostSec, event: u.Event})
					captured[u.Event.ID] = true
				} else if rank >= 1 {
					for i := range jobs {
						if jobs[i].event.ID == u.Event.ID {
							jobs[i].event = u.Event
						}
					}
				}
			}
			kept := jobs[:0]
			for _, j := range jobs {
				if now >= j.due {
					s.captureSnippets(j.event)
					continue
				}
				kept = append(kept, j)
			}
			jobs = kept
			if len(captured) > 10000 {
				captured = map[string]bool{}
			}
			if now-lastPublic >= 2 {
				samples := s.m.samples.Value()
				rate := float64(samples-lastSamples) / (now - lastPublic)
				if lastPublic == 0 {
					rate = 0
				}
				lastSamples = samples
				lastPublic = now
				s.publish(eng, now, rate, trace)
			}
			if now-lastMinute >= 60 {
				lastMinute = now
				s.recordMinute(eng, now)
			}
			if now-lastUnits >= 300 {
				lastUnits = now
				units := eng.ExportUnitStates()
				rows := make([]store.UnitRow, len(units))
				for i, u := range units {
					rows[i] = store.UnitRow{Key: u.Key, Votes: u.Votes, Exposure: u.Exposure}
				}
				s.st.SaveUnits(rows, now)
				pruneStations(eng, now, 24*3600)
			}
		}
	}
}

func pruneStations(eng *network.Engine, now, idle float64) {
	for _, st := range eng.Stations(now) {
		if now-st.LastSeen > idle {
			eng.RemoveStation(st.DeviceID)
		}
	}
}

func (s *Server) onEventUpdate(u network.Update, now float64) {
	ev := u.Event
	doc, _ := json.Marshal(ev)
	s.st.UpsertEvent(ev.ID, string(ev.Level), ev.Active, ev.Outcome, ev.OriginT, ev.Lat, ev.Lng, ev.CreatedAt, now, doc)
	switch u.Kind {
	case "created":
		s.m.eventsCreated.Inc()
	case "level":
		switch ev.Level {
		case network.LevelCorroborated:
			s.m.eventsCorroborated.Inc()
		case network.LevelConfirmed:
			s.m.eventsConfirmed.Inc()
		}
	case "closed":
		if ev.Outcome == "dismissed" {
			s.m.eventsDismissed.Inc()
		}
	}
	if u.Kind == "created" || u.Kind == "level" {
		for _, d := range ev.Decisions {
			if d.T == now || u.Kind == "created" {
				log.Printf("event %s: %s -> %s (units=%d log10p_adj=%.1f footprint=%.2f consistency=%.2f)", ev.ID, d.From, d.To,
					ev.Metrics.InlierUnits, ev.Metrics.Log10PAdj, ev.Metrics.FootprintFraction, ev.Metrics.Consistency)
			}
		}
	}
	s.rememberEvent(ev)
	s.disp.Handle(ev)
	if s.evlog != nil {
		s.evlog.write("event", map[string]any{"kind": u.Kind, "event": ev})
	}
	s.broadcastPublic(publicEventMessage(ev, s.pgrid))
}

func (s *Server) captureSnippets(ev network.Event) {
	from := ev.OriginT - s.cfg.SnippetPreSec
	to := ev.OriginT + s.cfg.SnippetPostSec
	for _, u := range ev.Units {
		for _, dev := range deviceList(ev, u.Unit) {
			s.shardFor(dev).submit(shardMsg{kind: shardSnippet, dev: dev, snip: &snipReq{eventID: ev.ID, from: from, to: to, unit: u.Unit}}, false)
		}
	}
}

func deviceList(ev network.Event, unit string) []string {
	var out []string
	for _, u := range ev.Units {
		if u.Unit == unit {
			out = append(out, network.UnitDevices(u)...)
		}
	}
	return out
}

func (s *Server) maintenanceLoop() {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
			ctx, cancel := context.WithTimeout(s.ctx, 5*time.Minute)
			if err := s.st.Prune(ctx, s.cfg.Retention, s.now()); err != nil {
				log.Printf("retention: %v", err)
			}
			cancel()
		}
	}
}
