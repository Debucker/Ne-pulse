package tgbot

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ne-pulse/internal/alert"
	"ne-pulse/internal/geo"
	"ne-pulse/internal/network"
	"ne-pulse/internal/store"
)

type fakeTG struct {
	mu      sync.Mutex
	updates []map[string]any
	sent    []map[string]any
	blocked map[int64]bool
}

func (f *fakeTG) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			ups := f.updates
			f.updates = nil
			b, _ := json.Marshal(ups)
			fmt.Fprintf(w, `{"ok":true,"result":%s}`, b)
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			id := int64(p["chat_id"].(float64))
			if f.blocked[id] {
				fmt.Fprint(w, `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`)
				return
			}
			f.sent = append(f.sent, p)
			fmt.Fprint(w, `{"ok":true,"result":{}}`)
		default:
			t.Errorf("unexpected call %s", r.URL.Path)
		}
	})
}

func msg(id int64, chat int64, lang, textIn string, loc *[2]float64) map[string]any {
	m := map[string]any{"chat": map[string]any{"id": chat, "type": "private"}, "from": map[string]any{"language_code": lang}, "text": textIn}
	if loc != nil {
		m["location"] = map[string]any{"latitude": loc[0], "longitude": loc[1]}
	}
	return map[string]any{"update_id": id, "message": m}
}

func newBot(t *testing.T, api string) (*Bot, *store.Store) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	b := New("TOKEN", st, network.LevelConfirmed, "https://ne-pulse.example", network.DefaultConfig())
	b.API = api
	b.PerSec = 1000
	return b, st
}

func TestSubscribeAndAlertOrdering(t *testing.T) {
	f := &fakeTG{blocked: map[int64]bool{}}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	b, st := newBot(t, srv.URL)
	ctx := context.Background()

	tashkent := [2]float64{41.31, 69.28}
	fergana := [2]float64{40.39, 71.78}   // ~230 km from Tashkent
	samarkand := [2]float64{39.65, 66.96} // ~270 km from Tashkent
	namangan := [2]float64{41.00, 71.67}
	c := b.Area.CellOf(tashkent[0], tashkent[1])
	f.updates = []map[string]any{
		msg(1, 101, "ru", fmt.Sprintf("/start a%dx%d", c.Row, c.Col), nil), // deep link from the site
		msg(2, 102, "en", "", &fergana),                                    // shared location
		msg(3, 103, "en", "", &samarkand),
		msg(4, 104, "en", "hello", nil), // not subscribed: gets the welcome
		msg(5, 105, "en", "", &namangan),
	}
	rctx, cancel := context.WithCancel(ctx)
	go func() {
		for i := 0; i < 50; i++ {
			subs, _ := st.Subscribers(ctx)
			if len(subs) == 4 {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		cancel()
	}()
	b.Run(rctx)
	subs, _ := st.Subscribers(ctx)
	if len(subs) != 4 {
		t.Fatalf("subscribers = %d, want 4", len(subs))
	}
	for _, s := range subs {
		lat, lng := b.Area.Center(geo.Cell{Row: s.Row, Col: s.Col})
		// Stored area is the 10 km square, never the shared point.
		if s.ChatID == 102 && (lat == fergana[0] || lng == fergana[1]) {
			t.Fatalf("exact location stored")
		}
		if s.ChatID == 101 && s.Lang != "ru" {
			t.Fatalf("language not detected: %q", s.Lang)
		}
	}

	// A quake in the Fergana Valley 20 s ago, delivered now.
	now := time.Unix(1_800_000_000, 0)
	b.Now = func() time.Time { return now }
	f.mu.Lock()
	f.sent = nil
	f.blocked[105] = true // the Namangan user blocked the bot; Samarkand is out of range
	f.mu.Unlock()
	m := alert.Message{EventID: "NP-TEST-1", Level: network.LevelConfirmed, Lat: 40.60, Lng: 71.20, OriginT: float64(now.Unix()) - 20}
	if err := b.Send(ctx, m); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	sent := f.sent
	f.mu.Unlock()
	if len(sent) != 2 {
		t.Fatalf("sent %d, want 2 (one blocked)", len(sent))
	}
	// Tashkent (~180 km) still has warning time; the Fergana subscriber (~50 km)
	// does not. Positive warning goes first.
	first := sent[0]["text"].(string)
	second := sent[1]["text"].(string)
	if int64(sent[0]["chat_id"].(float64)) != 101 || !strings.Contains(first, "примерно через") {
		t.Fatalf("first message should be the Tashkent warning, got %v: %s", sent[0]["chat_id"], first)
	}
	if !strings.Contains(second, "already reached") {
		t.Fatalf("Fergana message should say shaking already arrived: %s", second)
	}
	// Lead time matches sqrt(d^2+h^2)/Vs - elapsed.
	lat, lng := b.Area.Center(c)
	d := geo.HaversineKM(lat, lng, m.Lat, m.Lng)
	want := math.Floor(math.Hypot(d, 8)/3.36 - 20)
	if !strings.Contains(first, fmt.Sprintf("через %.0f с", want)) {
		t.Fatalf("lead time: want %.0f s in %q", want, first)
	}
	if subs, _ := st.Subscribers(ctx); len(subs) != 3 {
		t.Fatalf("blocked subscriber not removed")
	}
	for _, s := range sent {
		if !strings.Contains(s["text"].(string), "not an official") && !strings.Contains(s["text"].(string), "не официальное") {
			t.Fatalf("missing disclaimer")
		}
	}
}

func TestOutOfRangeAndStop(t *testing.T) {
	f := &fakeTG{blocked: map[int64]bool{}}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	b, st := newBot(t, srv.URL)
	ctx := context.Background()
	c := b.Area.CellOf(41.31, 69.28)
	_ = st.UpsertSubscriber(ctx, store.Subscriber{ChatID: 1, Row: c.Row, Col: c.Col, Lang: "en"})
	// 600 km away: outside the 250 km radius, nobody is messaged.
	if err := b.Send(ctx, alert.Message{EventID: "X", Lat: 37.0, Lng: 72.5, OriginT: float64(time.Now().Unix())}); err != nil {
		t.Fatal(err)
	}
	if len(f.sent) != 0 {
		t.Fatalf("out-of-range subscriber messaged")
	}
	if err := b.handle(ctx, update{UpdateID: 1, Message: &struct {
		Chat struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"chat"`
		From *struct {
			LanguageCode string `json:"language_code"`
		} `json:"from"`
		Text     string `json:"text"`
		Location *struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		} `json:"location"`
	}{Chat: struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	}{ID: 1, Type: "private"}, Text: "/stop"}}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.GetSubscriber(ctx, 1); ok {
		t.Fatal("/stop did not delete")
	}
}

func TestParseAreaRejectsGarbage(t *testing.T) {
	b := New("x", nil, network.LevelConfirmed, "", network.DefaultConfig())
	for _, s := range []string{"", "a", "a1", "ax1", "a-5x3", "a99999999x1", "b10x10", "a10x99999999"} {
		if _, ok := b.parseArea(s); ok {
			t.Errorf("accepted %q", s)
		}
	}
	c := b.Area.CellOf(41.3, 69.2)
	if got, ok := b.parseArea(fmt.Sprintf("a%dx%d", c.Row, c.Col)); !ok || got != c {
		t.Errorf("valid area rejected")
	}
}

type fakeDevices struct {
	codes      map[string]string
	conn       bool
	lastSeen   float64
	gone       bool
	nightHours float64
}

func (f *fakeDevices) ClaimTelegramLink(code string) (string, float64, float64, bool) {
	dev, ok := f.codes[code]
	delete(f.codes, code)
	return dev, 41.31, 69.28, ok
}

func (f *fakeDevices) DeviceSeen(string) (bool, float64, bool) { return f.conn, f.lastSeen, !f.gone }

func (f *fakeDevices) NightSummary(dev, night string) (float64, string, bool, float64) {
	if f.nightHours == 0 {
		return 0, "", false, 4
	}
	return f.nightHours, "ABCD-EFGH", f.nightHours >= 4, 4
}

func TestPhoneLinkAndStoppedListeningReminder(t *testing.T) {
	f := &fakeTG{blocked: map[int64]bool{}}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	b, st := newBot(t, srv.URL)
	ctx := context.Background()
	// 12:00 Tashkent time.
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, b.Loc)
	b.Now = func() time.Time { return now }
	dev := &fakeDevices{codes: map[string]string{"abc123": "dev-1"}, lastSeen: float64(now.Unix()) - 45*60}
	b.Devices = dev
	b.StoreNudge = st.SetSubscriberNudged

	// /start d<code> links the chat to the phone and subscribes its area.
	if err := b.handle(ctx, upd(t, msg(1, 77, "en", "/start dabc123", nil))); err != nil {
		t.Fatal(err)
	}
	sub, ok, _ := st.GetSubscriber(ctx, 77)
	if !ok || sub.DeviceID != "dev-1" {
		t.Fatalf("not linked: %+v %v", sub, ok)
	}
	// The same code again is refused.
	if err := b.handle(ctx, upd(t, msg(2, 78, "en", "/start dabc123", nil))); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.GetSubscriber(ctx, 78); ok {
		t.Fatal("used code linked a second chat")
	}
	// Re-subscribing by location keeps the linked phone.
	if err := b.handle(ctx, upd(t, msg(3, 77, "en", "", &[2]float64{41.3, 69.2}))); err != nil {
		t.Fatal(err)
	}
	if sub, _, _ := st.GetSubscriber(ctx, 77); sub.DeviceID != "dev-1" {
		t.Fatal("location update dropped the linked phone")
	}

	sentBefore := len(f.sent)
	b.remindOnce(ctx) // phone silent 45 min at noon -> one reminder
	if len(f.sent) != sentBefore+1 || !strings.Contains(f.sent[len(f.sent)-1]["text"].(string), "stopped listening</b> about 45 min") {
		t.Fatalf("no reminder: %v", f.sent[sentBefore:])
	}
	b.remindOnce(ctx) // already reminded for this gap
	now = now.Add(7 * time.Hour)
	b.remindOnce(ctx) // 19:00, same gap: still no repeat
	if len(f.sent) != sentBefore+1 {
		t.Fatal("reminder repeated for the same gap")
	}
	// A new gap at night: nothing until morning.
	dev.lastSeen = float64(now.Add(3*time.Hour).Unix()) - 30*60
	now = now.Add(3 * time.Hour) // 22:00
	b.remindOnce(ctx)
	if len(f.sent) != sentBefore+1 {
		t.Fatal("reminder at night")
	}
	// Deleted phone: no reminders.
	dev.gone = true
	now = time.Date(2026, 10, 3, 9, 0, 0, 0, b.Loc)
	dev.lastSeen = float64(now.Unix()) - 30*60
	b.remindOnce(ctx)
	if len(f.sent) != sentBefore+1 {
		t.Fatal("reminder for a deleted phone")
	}
}

func upd(t *testing.T, m map[string]any) update {
	b, _ := json.Marshal(m)
	var u update
	if err := json.Unmarshal(b, &u); err != nil {
		t.Fatal(err)
	}
	return u
}

func TestMorningSummary(t *testing.T) {
	f := &fakeTG{blocked: map[int64]bool{}}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	b, st := newBot(t, srv.URL)
	ctx := context.Background()
	now := time.Date(2026, 10, 12, 9, 0, 0, 0, b.Loc)
	b.Now = func() time.Time { return now }
	dev := &fakeDevices{codes: map[string]string{"x1": "dev-1"}, conn: true, nightHours: 6.3}
	b.Devices = dev
	b.StoreNudge = st.SetSubscriberNudged
	b.StoreMorning = st.SetSubscriberMorning
	if err := b.handle(ctx, upd(t, msg(1, 5, "ru", "/start dx1", nil))); err != nil {
		t.Fatal(err)
	}
	n := len(f.sent)
	b.morningOnce(ctx)
	b.morningOnce(ctx) // once per night
	if len(f.sent) != n+1 {
		t.Fatalf("want one morning message, got %d", len(f.sent)-n)
	}
	got := f.sent[len(f.sent)-1]["text"].(string)
	if !strings.Contains(got, "11.10") || !strings.Contains(got, "6.3") || !strings.Contains(got, "/contributors") {
		t.Fatalf("morning text: %s", got)
	}
	// not in the evening
	now = time.Date(2026, 10, 12, 20, 0, 0, 0, b.Loc)
	dev.nightHours = 2
	b.morningOnce(ctx)
	if len(f.sent) != n+1 {
		t.Fatal("morning message in the evening")
	}
}
