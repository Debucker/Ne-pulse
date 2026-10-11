// Command nepulse-server runs the NE-PULSE sensing network backend.
//
// Every flag can also be set with an environment variable NEPULSE_<FLAG>
// (upper case, dashes as underscores), e.g. NEPULSE_ADMIN_TOKEN. Secrets
// should always come from the environment, never the command line.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"ne-pulse/internal/alert"
	"ne-pulse/internal/catalog"
	"ne-pulse/internal/security"
	"ne-pulse/internal/server"
	"ne-pulse/internal/store"
	"ne-pulse/internal/tgbot"
)

func main() {
	cfg := server.DefaultConfig()
	fs := flag.NewFlagSet("nepulse-server", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "HTTP listen address")
	dbPath := fs.String("db", "nepulse.db", "SQLite database path")
	origins := fs.String("allowed-origins", "https://ne-pulse.com,https://www.ne-pulse.com,http://localhost:3000,http://127.0.0.1:3000", "comma-separated browser origins allowed to call the API")
	ipHeader := fs.String("client-ip-header", "", `trusted proxy header carrying the client IP ("CF-Connecting-IP", "X-Real-IP", "X-Forwarded-For"); empty = TCP peer only`)
	ipHops := fs.Int("client-ip-hops", 1, "for X-Forwarded-For: number of trusted proxies appending to it")
	secret := fs.String("secret", "", "server secret (>= 32 random bytes) for challenges and source hashes; set via NEPULSE_SECRET")
	adminToken := fs.String("admin-token", "", "bearer token for /api/admin and /metrics; empty disables them; set via NEPULSE_ADMIN_TOKEN")
	publicURL := fs.String("public-url", "https://ne-pulse.com", "public site URL used in alert links")
	evidence := fs.String("evidence-log", "", "append pipeline evidence (statuses, picks, events) as JSONL to this file")

	fs.IntVar(&cfg.Shards, "shards", cfg.Shards, "station processing shards (goroutines)")
	fs.IntVar(&cfg.PoWDifficulty, "pow-difficulty", cfg.PoWDifficulty, "enrollment proof-of-work difficulty (leading zero bits)")
	fs.Float64Var(&cfg.EnrollPerIPPerHour, "enroll-per-ip-hour", cfg.EnrollPerIPPerHour, "new devices per client IP per hour")
	fs.IntVar(&cfg.MaxConnsPerIP, "max-conns-per-ip", cfg.MaxConnsPerIP, "concurrent device connections per client IP")
	fs.Float64Var(&cfg.HTTPRatePerSec, "http-rate-per-ip", cfg.HTTPRatePerSec, "HTTP requests per second per client IP (mobile carriers put many phones behind one IP)")
	fs.IntVar(&cfg.KAnon, "k-anon", cfg.KAnon, "hide public map cells with fewer sensors than this")
	fs.Float64Var(&cfg.PublicGridKM, "public-grid-km", cfg.PublicGridKM, "public map aggregation cell size")
	fs.Float64Var(&cfg.Network.EstablishedAgeSec, "established-age", cfg.Network.EstablishedAgeSec, "device age (s) before it counts as established for confirmation")
	fs.Float64Var(&cfg.Network.EstablishedStreamSec, "established-stream", cfg.Network.EstablishedStreamSec, "seconds of streamed data before a device counts as established")

	tgToken := fs.String("telegram-token", "", "Telegram bot token for alerts; set via NEPULSE_TELEGRAM_TOKEN")
	tgChat := fs.String("telegram-chat", "", "Telegram chat/channel id, e.g. @ne_pulse_alerts")
	tgLevel := fs.String("telegram-level", "confirmed", "minimum level posted to Telegram")
	recordDir := fs.String("record-dir", "", "write continuous raw recordings of devices with a class code here (research); empty disables")
	recordDays := fs.Int("record-days", 365, "delete recordings older than this many days (0 keeps them forever)")
	recordMaxGB := fs.Float64("record-max-gb", 0, "stop recording when the recording folder reaches this size (GB); 0 = no limit")
	nightMinHours := fs.Float64("night-min-hours", 4, "listening hours in one night that earn a participation code")
	cohorts := fs.String("cohorts", "", "comma-separated class codes to register at startup, e.g. TASH-9B,TASH-10A")
	botToken := fs.String("bot-token", "", "Telegram bot token for personal area alerts (subscribe via /start); set via NEPULSE_BOT_TOKEN")
	botTokenFile := fs.String("bot-token-file", "", "file containing the Telegram bot token (alternative to -bot-token)")
	botLevel := fs.String("bot-level", "confirmed", "minimum level that triggers personal alerts")
	botRadius := fs.Float64("bot-radius", 250, "alert subscribers within this distance (km) of the epicentre")
	hookURL := fs.String("webhook-url", "", "generic alert webhook URL")
	hookLevel := fs.String("webhook-level", "corroborated", "minimum level sent to the webhook")

	catOn := fs.Bool("catalog", true, "fetch USGS/EMSC catalogs for validation")
	catRegion := fs.String("catalog-region", "", "minLat,maxLat,minLng,maxLng (default: network coverage + 3 degrees)")
	catMinMag := fs.Float64("catalog-min-mag", cfg.Catalog.MinMag, "minimum catalog magnitude")

	applyEnv(fs)
	fs.Parse(os.Args[1:])

	cfg.AllowedOrigins = strings.Split(*origins, ",")
	cfg.ClientIP = security.ClientIP{Header: *ipHeader, Hops: *ipHops}
	cfg.Secret = *secret
	cfg.AdminToken = *adminToken
	cfg.PublicBaseURL = *publicURL
	cfg.EvidenceLog = *evidence
	cfg.Catalog.Enabled = *catOn
	cfg.Catalog.MinMag = *catMinMag
	if *catRegion != "" {
		var r catalog.Region
		if _, err := fmt.Sscanf(*catRegion, "%f,%f,%f,%f", &r.MinLat, &r.MaxLat, &r.MinLng, &r.MaxLng); err != nil {
			log.Fatalf("bad -catalog-region: %v", err)
		}
		cfg.Catalog.Region = &r
	}
	cfg.RecordDir = *recordDir
	cfg.RecordDays = *recordDays
	cfg.RecordMaxGB = *recordMaxGB
	cfg.NightMinHours = *nightMinHours
	for _, c := range strings.Split(*cohorts, ",") {
		if c = strings.TrimSpace(c); c != "" {
			cfg.Cohorts = append(cfg.Cohorts, c)
		}
	}
	cfg.ConfigHash = configHash(cfg)

	var channels []alert.Channel
	if *tgToken != "" && *tgChat != "" {
		lvl, err := alert.ParseLevel(*tgLevel)
		if err != nil {
			log.Fatal(err)
		}
		channels = append(channels, &alert.Telegram{Token: *tgToken, ChatID: *tgChat, Min: lvl})
	}
	if *hookURL != "" {
		lvl, err := alert.ParseLevel(*hookLevel)
		if err != nil {
			log.Fatal(err)
		}
		channels = append(channels, &alert.Webhook{URL: *hookURL, Min: lvl})
	}

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	if *botToken == "" && *botTokenFile != "" {
		b, err := os.ReadFile(*botTokenFile)
		if err != nil {
			log.Fatalf("bot token file: %v", err)
		}
		*botToken = strings.TrimSpace(string(b))
	}
	var bot *tgbot.Bot
	if *botToken != "" {
		lvl, err := alert.ParseLevel(*botLevel)
		if err != nil {
			log.Fatal(err)
		}
		bot = tgbot.New(*botToken, st, lvl, *publicURL, cfg.Network)
		bot.RadiusKM = *botRadius
		uctx, ucancel := context.WithTimeout(context.Background(), 15*time.Second)
		name, err := bot.Username(uctx)
		ucancel()
		if err != nil {
			log.Fatalf("telegram bot: %v (check -bot-token)", err)
		}
		cfg.TelegramBot = name
		cfg.AlertRadiusKM = *botRadius
		channels = append(channels, bot)
		log.Printf("telegram bot @%s: personal alerts at >= %s within %.0f km", name, lvl, *botRadius)
	}
	srv, err := server.New(cfg, st, channels...)
	if err != nil {
		log.Fatal(err)
	}
	if bot != nil {
		// Phones can link a chat from the website; the bot reminds people
		// whose linked phone stopped listening.
		bot.Devices = srv
		bot.StoreNudge = st.SetSubscriberNudged
		bot.StoreMorning = st.SetSubscriberMorning
	}
	if err := srv.Start(); err != nil {
		log.Fatal(err)
	}
	if cfg.AdminToken == "" {
		log.Println("admin API disabled (no admin token)")
	}
	log.Printf("NE-PULSE server %s on %s (db=%s, shards=%d, config=%s, alert channels=%d)", cfg.ConfigHash, *addr, *dbPath, cfg.Shards, cfg.ConfigHash, len(channels))

	hs := &http.Server{Addr: *addr, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second, MaxHeaderBytes: 16 << 10}
	go func() {
		if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()
	botCtx, botStop := context.WithCancel(context.Background())
	defer botStop()
	if bot != nil {
		go bot.Run(botCtx)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	log.Println("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hs.Shutdown(ctx)
	srv.Shutdown()
	st.Close()
}

// applyEnv sets flags from NEPULSE_* environment variables before parsing,
// so explicit command-line flags still win.
func applyEnv(fs *flag.FlagSet) {
	fs.VisitAll(func(f *flag.Flag) {
		env := "NEPULSE_" + strings.ToUpper(strings.ReplaceAll(f.Name, "-", "_"))
		if v, ok := os.LookupEnv(env); ok {
			if err := f.Value.Set(v); err != nil {
				log.Fatalf("%s: %v", env, err)
			}
		}
	})
}

// configHash identifies the detection configuration; it is stored with every
// statistic and shown publicly so results can be tied to exact parameters.
func configHash(c server.Config) string {
	b, _ := json.Marshal(struct {
		S, N     any
		Loc, Pub float64
		K        int
	}{c.Station, c.Network, c.LocGridKM, c.PublicGridKM, c.KAnon})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:6])
}
