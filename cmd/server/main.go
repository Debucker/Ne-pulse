// Command server runs the ne-pulse gRPC telemetry ingestion daemon, alongside
// an HTTP sidecar that serves the live dashboard websocket feed, the
// chaos-simulation admin API, the hardware/browser ingress gateway, and
// (optionally) emergency public notification egress.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"ne-pulse/internal/control"
	"ne-pulse/internal/dashboard"
	"ne-pulse/internal/detector"
	"ne-pulse/internal/hub"
	"ne-pulse/internal/ingest"
	"ne-pulse/internal/ingress"
	"ne-pulse/internal/notify"
	"ne-pulse/internal/ratelimit"
	"ne-pulse/internal/solver"
	"ne-pulse/internal/storage"
	nepulsepb "ne-pulse/proto"

	"google.golang.org/grpc"
)

func main() {
	addr := flag.String("addr", ":50051", "gRPC listen address")
	httpAddr := flag.String("http-addr", ":8080", "HTTP listen address (dashboard websocket + admin API + ingress gateway)")
	workerCount := flag.Int("workers", 8, "background consumer worker goroutines")
	queueDepth := flag.Int("queue", 8192, "worker pool backlog channel capacity")

	simMode := flag.String("sim-mode", "redis", `storage backend: "redis" (default — attempts RedisTimeSeries, auto-falls back to in-memory if the target rejects TS.ADD) or "memory" (skip Redis entirely)`)
	redisAddr := flag.String("redis-addr", "localhost:6379", "RedisTimeSeries host:port (ignored when -sim-mode=memory)")
	redisPassword := flag.String("redis-password", "", "Redis AUTH password (blank if none)")
	redisDB := flag.Int("redis-db", 0, "Redis logical DB index")
	redisPoolSize := flag.Int("redis-pool-size", 64, "Redis client connection pool size")
	redisBatchSize := flag.Int("redis-batch-size", storage.DefaultBatchSize, "entries buffered per worker before a pipelined TS.ADD flush")
	redisFlushInterval := flag.Duration("redis-flush-interval", storage.DefaultFlushInterval, "max time a partial batch waits before flushing")

	h3Resolution := flag.Int("radar-h3-resolution", 8, "H3 resolution for the tectonic-rupture radar's spatial cells")
	shockThreshold := flag.Float64("radar-shock-threshold", detector.DefaultConfig().ShockThreshold, "minimum acceleration vector norm (g) counted as a high-acceleration incident")
	radarThreshold := flag.Int("radar-threshold", 50, "unique devices required in one cell to confirm a rupture")
	coincidenceWindow := flag.Duration("radar-coincidence-window", detector.DefaultConfig().CoincidenceWindow, "trailing time window unique devices must fall within to confirm a rupture")
	bucketCapacity := flag.Int("radar-bucket-capacity", detector.DefaultConfig().BucketCapacity, "recent readings retained per cell; must comfortably exceed one coincidence-window's worth of traffic at your busiest expected cell, or high insertion volume can evict still-in-window readings by count before they age out by time")
	radarCooldown := flag.Duration("radar-cooldown", detector.DefaultConfig().CooldownDuration, "latch duration after a confirmed rupture during which further triggers (same cell re-firing, or a neighboring cell crossing threshold from the same event) are discarded")

	dashboardFlushInterval := flag.Duration("dashboard-flush-interval", dashboard.DefaultFlushInterval, "how often aggregated cell-density snapshots are broadcast to dashboard clients")

	notifyWebhookURL := flag.String("notify-webhook-url", "", "external gateway URL (Telegram Bot relay, Twilio webhook, etc.) to POST emergency alerts to; empty disables notification egress entirely")
	notifySafetyMargin := flag.Float64("notify-safety-margin", solver.DefaultSafetyMarginSeconds, "seconds of remaining warning time below which a city triggers an emergency public notification")

	corsAllowedOrigins := flag.String("cors-allowed-origins", defaultAllowedOrigins,
		"comma-separated list of origins allowed to reach the HTTP API and websocket endpoints (CORS + WebSocket handshake Origin check)")
	rateLimitPerSecond := flag.Float64("rate-limit-per-second", 5, "max HTTP requests/second allowed per client IP, across the dashboard/control/ingress HTTP API")
	rateLimitBurst := flag.Int("rate-limit-burst", 5, "short burst allowance per client IP on top of the steady-state rate")
	apiTokens := flag.String("api-tokens", "", "comma-separated list of valid X-API-Token values for third-party hardware ingress (POST /api/ingress/hardware); empty leaves that endpoint open to any caller (the default, matching prior behavior) until real tokens are provisioned")
	flag.Parse()

	originAllowed := newOriginChecker(*corsAllowedOrigins)
	limiter := ratelimit.New(*rateLimitPerSecond, *rateLimitBurst)
	hardwareAuth := ingress.NewTokenAuthenticator(*apiTokens)
	if !hardwareAuth.Configured() {
		log.Println("warning: -api-tokens is empty — POST /api/ingress/hardware is open to any caller with no authentication")
	}

	var store *storage.Store
	if *simMode == "memory" {
		store = storage.NewMemoryOnlyStore()
		log.Println("storage: sim-mode=memory — running without Redis; time-series data is in-memory only")
	} else {
		redisCfg := storage.DefaultConfig()
		redisCfg.Addr = *redisAddr
		redisCfg.Password = *redisPassword
		redisCfg.DB = *redisDB
		redisCfg.PoolSize = *redisPoolSize

		store = storage.NewStore(redisCfg)
		// A dead Redis at startup should not prevent the ingestion server
		// from accepting traffic — frames simply accumulate failed flushes
		// (visible via store.FailedTotal()) until Redis becomes reachable.
		// A *reachable* Redis that rejects TS.ADD (no RedisTimeSeries
		// module) is a separate case handled automatically inside
		// Store.flushBatch: it falls back to the in-memory collector the
		// first time that's discovered, logging exactly once.
		pingCtx, pingCancel := context.WithTimeout(context.Background(), 3*time.Second)
		if err := store.Ping(pingCtx); err != nil {
			log.Printf("warning: could not reach Redis at %s: %v (server will still start; flushes will fail until Redis is reachable)", *redisAddr, err)
		} else {
			log.Printf("connected to Redis at %s", *redisAddr)
		}
		pingCancel()
	}

	radarCfg := detector.DefaultConfig()
	radarCfg.ShockThreshold = *shockThreshold
	radarCfg.TriggerDensity = *radarThreshold
	radarCfg.CoincidenceWindow = *coincidenceWindow
	radarCfg.BucketCapacity = *bucketCapacity
	radarCfg.CooldownDuration = *radarCooldown
	radar := detector.NewSpatialRadar(radarCfg)
	cellIndexer := detector.NewDefaultIndexer(*h3Resolution)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Two independent broadcast hubs sharing the same primitive
	// (internal/hub): telemetryHub pushes aggregated cell-density snapshots
	// and rupture alerts to the Next.js dashboard; controlHub relays
	// simulate-rupture commands to connected loadclient chaos-engine
	// processes. Both are bound to the same lifecycle ctx as everything
	// else.
	telemetryHub := hub.New(ctx, originAllowed)
	controlHub := hub.New(ctx, originAllowed)
	go telemetryHub.Run()
	go controlHub.Run()

	aggregator := dashboard.NewAggregator(telemetryHub, cellIndexer, *dashboardFlushInterval)
	go aggregator.Run(ctx)

	// notifyHook is nil (disabled) unless an external gateway URL was
	// configured — RunWithHooks treats a nil hook exactly like RunWithSink.
	var notifyHook solver.NotificationHook
	if *notifyWebhookURL != "" {
		dispatcher := notify.NewDispatcher(*notifyWebhookURL)
		notifyHook = dispatcher.Hook()
		log.Printf("notify: emergency notification egress armed — webhook=%s safety-margin=%.1fs", *notifyWebhookURL, *notifySafetyMargin)
	}

	// The radar runs off the same lifecycle context as the worker pool: it
	// is the dedicated background evaluator goroutine that owns all
	// per-cell coincidence state, running alongside (not blocking) the
	// Redis time-series pipeline below. RunWithHooks prints the terminal
	// alert block, fans the same payload out to dashboard clients, and
	// arms per-city emergency-notification timers — all on its own
	// goroutine, so neither a slow terminal write, a slow websocket
	// client, nor a slow external gateway can ever back up rupture
	// detection itself.
	radar.Run(ctx)
	go solver.RunWithHooks(ctx, radar.Events(), aggregator.BroadcastRupture, notifyHook, *notifySafetyMargin)

	pool := ingest.NewWorkerPool(
		*queueDepth,
		*workerCount,
		*redisFlushInterval,
		ingest.NewMultiConsumerFactory(
			storage.NewRedisConsumerFactory(store, *redisBatchSize),
			detector.NewRadarConsumerFactory(radar, cellIndexer),
			dashboard.NewDashboardConsumerFactory(aggregator, cellIndexer),
		),
	)
	pool.Start(ctx)

	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", *addr, err)
	}

	grpcServer := grpc.NewServer()
	nepulsepb.RegisterTelemetryIngestServer(grpcServer, ingest.NewServer(pool))

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/telemetry", telemetryHub.ServeWS)
	mux.HandleFunc("/ws/control", controlHub.ServeWS)
	mux.HandleFunc("/api/simulate-rupture", control.SimulateRuptureHandler(controlHub, radar))
	mux.HandleFunc("/api/ingress/hardware", hardwareAuth.Middleware(ingress.NewHardwareHandler(pool)))
	mux.HandleFunc("/api/v1/docs", ingress.NewDocsHandler())
	mux.HandleFunc("/api/v1/alert", alertHandler(controlHub))
	mux.HandleFunc("/api/health", healthHandler(pool, store, radar, telemetryHub, controlHub))
	httpServer := &http.Server{Addr: *httpAddr, Handler: withCORS(originAllowed, limiter.Middleware(mux))}

	go limiter.Run(ctx)
	go reportThroughput(pool, store, radar, aggregator, telemetryHub, controlHub)

	go func() {
		log.Printf("ne-pulse telemetry ingestion server listening on %s (workers=%d, queue=%d, redis-batch=%d, redis-flush=%s, radar-threshold=%d, radar-cooldown=%s)",
			*addr, *workerCount, *queueDepth, *redisBatchSize, *redisFlushInterval, *radarThreshold, *radarCooldown)
		if err := grpcServer.Serve(listener); err != nil {
			log.Fatalf("grpc server exited with error: %v", err)
		}
	}()

	go func() {
		log.Printf("ne-pulse dashboard/control/ingress HTTP server listening on %s (ws:/ws/telemetry, ws:/ws/control, POST:/api/simulate-rupture, POST:/api/ingress/hardware, GET:/api/v1/docs, rate-limit=%.0f req/s burst=%d per IP, hardware-ingress-auth=%v)", *httpAddr, *rateLimitPerSecond, *rateLimitBurst, hardwareAuth.Configured())
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server exited with error: %v", err)
		}
	}()

	waitForShutdownSignal()

	log.Println("shutdown signal received: draining in-flight streams...")
	// GracefulStop blocks until every in-flight StreamTelemetry handler has
	// returned, which guarantees no goroutine can call pool.Submit after
	// this point — only then is it safe to close the pool's channel.
	grpcServer.GracefulStop()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("warning: HTTP server did not shut down cleanly: %v", err)
	}
	shutdownCancel()

	log.Println("in-flight streams drained: stopping worker pool...")
	cancel() // also stops the radar's evaluator goroutine, both hubs, and the dashboard aggregator
	pool.Stop()

	if err := store.Close(); err != nil {
		log.Printf("warning: error closing Redis connection pool: %v", err)
	}

	log.Printf("shutdown complete: accepted=%d dropped=%d ts-flushed=%d ts-failed=%d ruptures=%d suppressed=%d dashboard-snapshots=%d",
		pool.Accepted(), pool.Dropped(), store.FlushedTotal(), store.FailedTotal(), radar.RuptureCount(), radar.SuppressedRuptures(), aggregator.SnapshotsSent())
}

func waitForShutdownSignal() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh
}

// defaultAllowedOrigins covers the production domain (both apex and www)
// plus local dev servers, so a fresh checkout works out of the box without
// needing -cors-allowed-origins set; override it for staging/other
// environments rather than editing this list.
//
// Ports 3001-3005 are included alongside 3000: `next dev`/`next start`
// silently falls back to the next free port whenever 3000 is already taken
// (extremely common with multiple dev servers running), and a frontend
// served from that fallback port would otherwise fail the websocket's
// Origin check with no visible error beyond a generic connection failure —
// looking exactly like a broken trigger button or a dashboard stuck on
// "Disconnected" rather than the port mismatch it actually is.
const defaultAllowedOrigins = "https://ne-pulse.com,https://www.ne-pulse.com," +
	"http://localhost:3000,http://127.0.0.1:3000," +
	"http://localhost:3001,http://127.0.0.1:3001," +
	"http://localhost:3002,http://127.0.0.1:3002," +
	"http://localhost:3003,http://127.0.0.1:3003," +
	"http://localhost:3004,http://127.0.0.1:3004," +
	"http://localhost:3005,http://127.0.0.1:3005"

// newOriginChecker parses a comma-separated origin list into a matcher
// shared by both the HTTP CORS middleware and every websocket hub's
// handshake check. A request with no Origin header at all — which is what
// non-browser Go clients (the loadclient's gRPC/websocket connections) send
// by default — is always allowed: Origin enforcement is fundamentally a
// browser cooperation mechanism (a browser refuses to let page JS forge a
// different Origin), so it has nothing meaningful to enforce against a
// client that was never a browser in the first place. Only requests that
// *do* carry an Origin header (i.e. real browsers) are checked against the
// allowlist.
func newOriginChecker(commaSeparated string) func(r *http.Request) bool {
	allowed := make(map[string]struct{})
	for _, o := range strings.Split(commaSeparated, ",") {
		o = strings.TrimSpace(o)
		if o != "" {
			allowed[o] = struct{}{}
		}
	}
	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		_, ok := allowed[origin]
		return ok
	}
}

// withCORS reflects back the request's Origin header only when it's in the
// allowlist (the standard non-wildcard CORS pattern — a single response can
// only ever declare one allowed origin, so it must be computed per
// request), and always sets Vary: Origin so a caching proxy/CDN in front of
// this server can never serve one origin's CORS-approved response to a
// different, disallowed origin.
func withCORS(originAllowed func(r *http.Request) bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Vary", "Origin")
		if origin := r.Header.Get("Origin"); origin != "" && originAllowed(r) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		// X-API-Token is here alongside Content-Type because it's a
		// non-simple header: a browser sending it cross-origin (e.g. the
		// Lite dashboard's crowdsourced telemetry, Vercel frontend -> Render
		// backend) preflights first, and silently drops the real request if
		// the preflight response doesn't list every header the actual
		// request will send -- Content-Type alone was already enough to
		// force a preflight (application/json isn't CORS-safelisted), so
		// this costs nothing extra for existing callers.
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Token")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// alertPayload is broadcast to every connected /ws/control client (the same
// hub the "Trigger Rupture" demo already uses) on POST /api/v1/alert.
// Distinct Type from control.RuptureCommand's "simulate-rupture" so a
// client can tell the two apart if it ever needs to.
type alertPayload struct {
	Type      string    `json:"type"`
	Lat       float64   `json:"lat"`
	Lng       float64   `json:"lng"`
	Magnitude float64   `json:"magnitude"`
	Timestamp time.Time `json:"timestamp"`
}

type alertRequest struct {
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	Magnitude float64 `json:"magnitude"`
}

// alertHandler returns an http.HandlerFunc for POST /api/v1/alert: an
// external caller (a future push-notification relay, a manual test,
// another sensor gateway) supplies a lat/lng/magnitude, which is broadcast
// as-is to every connected control-hub client — the same websocket
// (/ws/control) control.SimulateRuptureHandler already publishes to, so any
// existing listener needs no changes to also receive these.
//
// Same validation and unauthenticated exposure as
// control.SimulateRuptureHandler — this route has no auth of its own
// (matching the rest of the demo API), so anyone who can reach it can
// broadcast an alert. Fine for internal testing; lock it down before using
// it for anything a real user would see.
func alertHandler(controlHub *hub.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()

		var req alertRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.Lat < -90 || req.Lat > 90 || req.Lng < -180 || req.Lng > 180 {
			http.Error(w, "lat/lng out of range", http.StatusBadRequest)
			return
		}
		if req.Magnitude < 0 || req.Magnitude > 10 {
			http.Error(w, "magnitude out of range", http.StatusBadRequest)
			return
		}

		payload := alertPayload{Type: "alert", Lat: req.Lat, Lng: req.Lng, Magnitude: req.Magnitude, Timestamp: time.Now()}
		body, err := json.Marshal(payload)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		delivered := controlHub.Broadcast(body)
		log.Printf("alert: v1 alert broadcast lat=%.4f lng=%.4f mag=%.1f delivered=%v control-clients=%d",
			req.Lat, req.Lng, req.Magnitude, delivered, controlHub.ClientCount())

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(payload)
	}
}

func healthHandler(pool *ingest.WorkerPool, store *storage.Store, radar *detector.SpatialRadar, telemetryHub, controlHub *hub.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":             "ok",
			"accepted":           pool.Accepted(),
			"dropped":            pool.Dropped(),
			"storageMode":        store.Mode(),
			"tsFlushed":          store.FlushedTotal(),
			"tsFailed":           store.FailedTotal(),
			"ruptures":           radar.RuptureCount(),
			"suppressedRuptures": radar.SuppressedRuptures(),
			"radarDropped":       radar.DroppedReadings(),
			"dashboardClients":   telemetryHub.ClientCount(),
			"controlClients":     controlHub.ClientCount(),
		})
	}
}

func reportThroughput(pool *ingest.WorkerPool, store *storage.Store, radar *detector.SpatialRadar, aggregator *dashboard.Aggregator, telemetryHub, controlHub *hub.Hub) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	var lastFlushed int64
	for range ticker.C {
		current := store.FlushedTotal()
		rate := current - lastFlushed
		lastFlushed = current
		log.Printf(
			"throughput: %d ts-flushes/2s (~%d/s) [%s] accepted=%d dropped=%d queueDepth=%d ts-failed=%d ruptures=%d suppressed=%d radar-dropped=%d dashboard-snapshots=%d dashboard-clients=%d control-clients=%d",
			rate, rate/2, store.Mode(), pool.Accepted(), pool.Dropped(), pool.QueueDepth(), store.FailedTotal(),
			radar.RuptureCount(), radar.SuppressedRuptures(), radar.DroppedReadings(),
			aggregator.SnapshotsSent(), telemetryHub.ClientCount(), controlHub.ClientCount(),
		)
	}
}
