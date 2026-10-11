// Package alert turns event-level transitions into outward notifications
// under an explicit policy. NE-PULSE is a research network, not an official
// warning authority: every message says so, nothing is sent below the
// configured level, and each (event, level, channel) is sent at most once.
package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"ne-pulse/internal/network"
)

// Channel delivers one message.
type Channel interface {
	Name() string
	MinLevel() network.Level
	Send(ctx context.Context, m Message) error
}

// Message is the channel-independent alert content.
type Message struct {
	EventID   string        `json:"eventId"`
	Level     network.Level `json:"level"`
	Title     string        `json:"title"`
	Body      string        `json:"body"`
	URL       string        `json:"url"`
	Lat       float64       `json:"lat"`
	Lng       float64       `json:"lng"`
	OriginT   float64       `json:"originTime"`
	Units     int           `json:"independentUnits"`
	Stations  int           `json:"stations"`
	Log10PAdj float64       `json:"log10PAdj"`
	MaxMMI    float64       `json:"maxMmiUncalibrated"`
	Place     string        `json:"place"`
}

// Rank orders levels.
func Rank(l network.Level) int {
	switch l {
	case network.LevelCandidate:
		return 1
	case network.LevelCorroborated:
		return 2
	case network.LevelConfirmed:
		return 3
	}
	return 0
}

// ParseLevel parses a configured level name.
func ParseLevel(s string) (network.Level, error) {
	switch network.Level(s) {
	case network.LevelCandidate, network.LevelCorroborated, network.LevelConfirmed:
		return network.Level(s), nil
	}
	return "", fmt.Errorf("unknown level %q (candidate|corroborated|confirmed)", s)
}

// Compose builds the message for an event at its current level.
func Compose(ev network.Event, baseURL, place string) Message {
	var title string
	switch ev.Level {
	case network.LevelCandidate:
		title = "NE-PULSE: possible seismic signal (unverified)"
	case network.LevelCorroborated:
		title = "NE-PULSE: correlated motion across independent sensors"
	case network.LevelConfirmed:
		title = "NE-PULSE: network-confirmed seismic event"
	}
	t := time.Unix(int64(ev.OriginT), 0).UTC().Format("2006-01-02 15:04:05 UTC")
	loc := fmt.Sprintf("%.2f°, %.2f° (±%.0f km)", ev.Lat, ev.Lng, math.Max(1, ev.LocUncKM))
	if place != "" {
		loc = place + " — " + loc
	}
	body := fmt.Sprintf("%s\nNear %s\n%d independent sensor areas, %d phones. Strongest phone-measured shaking ≈ MMI %.0f (uncalibrated).\n"+
		"This is an experimental research network observation, not an official earthquake warning. Follow your national seismic and emergency services.",
		t, loc, ev.Metrics.InlierUnits, ev.Metrics.TriggeredStations, math.Round(ev.Metrics.MaxMMI))
	u := ""
	if baseURL != "" {
		u = strings.TrimRight(baseURL, "/") + "/event?id=" + url.QueryEscape(ev.ID)
	}
	return Message{EventID: ev.ID, Level: ev.Level, Title: title, Body: body, URL: u, Lat: ev.Lat, Lng: ev.Lng, OriginT: ev.OriginT,
		Units: ev.Metrics.InlierUnits, Stations: ev.Metrics.TriggeredStations, Log10PAdj: ev.Metrics.Log10PAdj, MaxMMI: ev.Metrics.MaxMMI, Place: place}
}

// Claimer provides exactly-once bookkeeping (the store implements it).
type Claimer interface {
	ClaimAlert(ctx context.Context, eventID, level, channel string, now float64) (bool, error)
	FinishAlert(eventID, level, channel string, ok bool, detail string)
}

// Dispatcher fans alerts out to channels.
type Dispatcher struct {
	channels  []Channel
	claimer   Claimer
	baseURL   string
	maxPerHr  int
	mu        sync.Mutex
	sentTimes []time.Time
	Place     func(lat, lng float64) string
	OnResult  func(channel string, ok bool)
}

// NewDispatcher builds a dispatcher. maxPerHour caps outward messages per
// channel set as a last-resort brake against a malfunction or attack.
func NewDispatcher(claimer Claimer, baseURL string, maxPerHour int, channels ...Channel) *Dispatcher {
	return &Dispatcher{channels: channels, claimer: claimer, baseURL: baseURL, maxPerHr: maxPerHour}
}

// Channels lists configured channel names.
func (d *Dispatcher) Channels() []string {
	var out []string
	for _, c := range d.channels {
		out = append(out, fmt.Sprintf("%s(>=%s)", c.Name(), c.MinLevel()))
	}
	return out
}

func (d *Dispatcher) budget(now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	kept := d.sentTimes[:0]
	for _, t := range d.sentTimes {
		if now.Sub(t) < time.Hour {
			kept = append(kept, t)
		}
	}
	d.sentTimes = kept
	if d.maxPerHr > 0 && len(d.sentTimes) >= d.maxPerHr {
		return false
	}
	d.sentTimes = append(d.sentTimes, now)
	return true
}

// Handle is called for every event update; it sends each qualifying channel
// its message once per level reached. Delivery is asynchronous.
func (d *Dispatcher) Handle(ev network.Event) {
	if ev.Level == "" {
		return
	}
	for _, c := range d.channels {
		if Rank(ev.Level) < Rank(c.MinLevel()) {
			continue
		}
		c := c
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			now := time.Now()
			ok, err := d.claimer.ClaimAlert(ctx, ev.ID, string(ev.Level), c.Name(), float64(now.Unix()))
			if err != nil || !ok {
				return
			}
			if !d.budget(now) {
				d.claimer.FinishAlert(ev.ID, string(ev.Level), c.Name(), false, "suppressed: hourly budget exhausted")
				log.Printf("alert: hourly budget exhausted, suppressed %s %s via %s", ev.ID, ev.Level, c.Name())
				return
			}
			place := ""
			if d.Place != nil {
				place = d.Place(ev.Lat, ev.Lng)
			}
			err = c.Send(ctx, Compose(ev, d.baseURL, place))
			detail := "sent"
			if err != nil {
				detail = err.Error()
				log.Printf("alert: %s via %s failed: %v", ev.ID, c.Name(), err)
			}
			d.claimer.FinishAlert(ev.ID, string(ev.Level), c.Name(), err == nil, detail)
			if d.OnResult != nil {
				d.OnResult(c.Name(), err == nil)
			}
		}()
	}
}

// Telegram posts to a chat or channel via the Bot API.
type Telegram struct {
	Token  string
	ChatID string
	Min    network.Level
	API    string // override for tests; default https://api.telegram.org
	Client *http.Client
}

func (t *Telegram) Name() string            { return "telegram" }
func (t *Telegram) MinLevel() network.Level { return t.Min }

func (t *Telegram) Send(ctx context.Context, m Message) error {
	api := t.API
	if api == "" {
		api = "https://api.telegram.org"
	}
	text := "*" + escapeMD(m.Title) + "*\n" + escapeMD(m.Body)
	if m.URL != "" {
		text += "\n" + escapeMD(m.URL)
	}
	body, _ := json.Marshal(map[string]any{"chat_id": t.ChatID, "text": text, "parse_mode": "MarkdownV2", "disable_web_page_preview": true})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, api+"/bot"+t.Token+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c := t.Client
	if c == nil {
		c = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("telegram: HTTP %d", resp.StatusCode)
	}
	return nil
}

func escapeMD(s string) string {
	r := strings.NewReplacer("_", `\_`, "*", `\*`, "[", `\[`, "]", `\]`, "(", `\(`, ")", `\)`, "~", `\~`, "`", "\\`",
		">", `\>`, "#", `\#`, "+", `\+`, "-", `\-`, "=", `\=`, "|", `\|`, "{", `\{`, "}", `\}`, ".", `\.`, "!", `\!`)
	return r.Replace(s)
}

// Webhook POSTs the JSON message to a URL (for SMS gateways, Slack relays…).
type Webhook struct {
	URL    string
	Min    network.Level
	Client *http.Client
}

func (w *Webhook) Name() string            { return "webhook" }
func (w *Webhook) MinLevel() network.Level { return w.Min }

func (w *Webhook) Send(ctx context.Context, m Message) error {
	body, _ := json.Marshal(m)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c := w.Client
	if c == nil {
		c = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook: HTTP %d", resp.StatusCode)
	}
	return nil
}
