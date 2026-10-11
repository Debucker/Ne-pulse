package alert

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"ne-pulse/internal/network"
)

type memClaims struct {
	mu   sync.Mutex
	seen map[string]bool
	ok   map[string]bool
}

func (m *memClaims) ClaimAlert(_ context.Context, e, l, c string, _ float64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := e + l + c
	if m.seen[k] {
		return false, nil
	}
	m.seen[k] = true
	return true, nil
}
func (m *memClaims) FinishAlert(e, l, c string, ok bool, _ string) {
	m.mu.Lock()
	m.ok[e+l+c] = ok
	m.mu.Unlock()
}

func TestPolicyLevelsAndExactlyOnce(t *testing.T) {
	var mu sync.Mutex
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		json.Unmarshal(b, &m)
		mu.Lock()
		got = append(got, r.URL.Path+" "+m["text"].(string))
		mu.Unlock()
	}))
	defer srv.Close()
	claims := &memClaims{seen: map[string]bool{}, ok: map[string]bool{}}
	d := NewDispatcher(claims, "https://ne-pulse.com", 10, &Telegram{Token: "T", ChatID: "@c", Min: network.LevelConfirmed, API: srv.URL})
	ev := network.Event{ID: "NP-1", Level: network.LevelCandidate, OriginT: 1.8e9, Lat: 41.3, Lng: 69.2}
	d.Handle(ev) // below policy: nothing
	ev.Level = network.LevelCorroborated
	d.Handle(ev) // below policy: nothing
	ev.Level = network.LevelConfirmed
	d.Handle(ev)
	d.Handle(ev) // duplicate: nothing
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("sent %d messages: %v", len(got), got)
	}
	if !strings.Contains(got[0], "/botT/sendMessage") || !strings.Contains(got[0], "not an official earthquake warning") {
		t.Fatalf("message %q", got[0])
	}
}

func TestComposeNeverClaimsAuthority(t *testing.T) {
	for _, l := range []network.Level{network.LevelCandidate, network.LevelCorroborated, network.LevelConfirmed} {
		m := Compose(network.Event{ID: "x", Level: l}, "", "")
		low := strings.ToLower(m.Title + m.Body)
		if !strings.Contains(low, "not an official") {
			t.Fatalf("%s message lacks disclaimer", l)
		}
		for _, bad := range []string{"take cover", "evacuate", "earthquake warning:"} {
			if strings.Contains(low, bad) {
				t.Fatalf("%s message contains %q", l, bad)
			}
		}
	}
}
