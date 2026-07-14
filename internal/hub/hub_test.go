package hub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func newTestHub(t *testing.T) (*Hub, *httptest.Server, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h := New(ctx, nil) // nil checkOrigin = allow every origin, appropriate for an in-process test
	go h.Run()

	server := httptest.NewServer(http.HandlerFunc(h.ServeWS))
	t.Cleanup(server.Close)
	return h, server, cancel
}

func dial(t *testing.T, server *httptest.Server) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("failed to dial test websocket server: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// TestHub_BroadcastsToRegisteredClient proves a message pushed via
// Broadcast is actually delivered end-to-end over a real websocket
// connection (in-process, via httptest — no external services needed).
func TestHub_BroadcastsToRegisteredClient(t *testing.T) {
	h, server, cancel := newTestHub(t)
	defer cancel()

	conn := dial(t, server)

	// Give the hub's Run goroutine a moment to process the registration
	// before broadcasting, so the message isn't sent before the client is
	// in the registered set.
	deadline := time.Now().Add(500 * time.Millisecond)
	for h.ClientCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if h.ClientCount() != 1 {
		t.Fatalf("ClientCount() = %d, want 1 before broadcasting", h.ClientCount())
	}

	if !h.Broadcast([]byte(`{"hello":"world"}`)) {
		t.Fatal("Broadcast reported failure (buffer full?) on a fresh hub")
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("client failed to read broadcast message: %v", err)
	}
	if string(msg) != `{"hello":"world"}` {
		t.Errorf("received %q, want %q", msg, `{"hello":"world"}`)
	}
}

// TestHub_UnregistersOnClientDisconnect proves ClientCount drops back to 0
// once a client closes its connection.
func TestHub_UnregistersOnClientDisconnect(t *testing.T) {
	h, server, cancel := newTestHub(t)
	defer cancel()

	conn := dial(t, server)

	deadline := time.Now().Add(500 * time.Millisecond)
	for h.ClientCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if h.ClientCount() != 1 {
		t.Fatalf("ClientCount() = %d, want 1 after connect", h.ClientCount())
	}

	conn.Close()

	deadline = time.Now().Add(500 * time.Millisecond)
	for h.ClientCount() != 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if h.ClientCount() != 0 {
		t.Errorf("ClientCount() = %d, want 0 after disconnect", h.ClientCount())
	}
}

// TestHub_NewClientImmediatelyReceivesRetainedMessage proves a client that
// connects well after SetRetained was last called — with no fresh
// Broadcast in between — still receives that retained message right away,
// instead of seeing nothing until (or unless) another broadcast happens.
// This is what fixes an empty dashboard map for a browser tab opened after
// whatever was generating traffic has already stopped.
func TestHub_NewClientImmediatelyReceivesRetainedMessage(t *testing.T) {
	h, server, cancel := newTestHub(t)
	defer cancel()

	h.SetRetained([]byte(`{"type":"telemetry","cells":[]}`))
	time.Sleep(20 * time.Millisecond) // no client connected yet; retained must not require one

	conn := dial(t, server)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("new client never received the retained message: %v", err)
	}
	if string(msg) != `{"type":"telemetry","cells":[]}` {
		t.Errorf("received %q, want the retained message", msg)
	}
}

// TestHub_RetainedMessageUpdatesToLatestValue proves SetRetained replaces
// (not accumulates) the retained value — a newly-connecting client only
// ever sees the most recent state, not a backlog of every past one.
func TestHub_RetainedMessageUpdatesToLatestValue(t *testing.T) {
	h, server, cancel := newTestHub(t)
	defer cancel()

	h.SetRetained([]byte("first"))
	h.SetRetained([]byte("second"))

	conn := dial(t, server)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("new client never received a retained message: %v", err)
	}
	if string(msg) != "second" {
		t.Errorf("received %q, want the most recent retained value %q", msg, "second")
	}
}

// TestHub_NoRetainedMessageMeansNewClientGetsNothingUntilBroadcast proves a
// hub that never had SetRetained called behaves exactly as before: a new
// client gets nothing until an actual Broadcast happens.
func TestHub_NoRetainedMessageMeansNewClientGetsNothingUntilBroadcast(t *testing.T) {
	_, server, cancel := newTestHub(t)
	defer cancel()

	conn := dial(t, server)
	conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	_, _, err := conn.ReadMessage()
	if err == nil {
		t.Fatal("expected no message before any Broadcast/SetRetained, got one")
	}
}

// TestHub_BroadcastReachesMultipleClients proves fan-out actually fans
// out — every registered client gets its own copy of the message.
func TestHub_BroadcastReachesMultipleClients(t *testing.T) {
	h, server, cancel := newTestHub(t)
	defer cancel()

	const n = 5
	conns := make([]*websocket.Conn, n)
	for i := range conns {
		conns[i] = dial(t, server)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for h.ClientCount() != n && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if h.ClientCount() != n {
		t.Fatalf("ClientCount() = %d, want %d", h.ClientCount(), n)
	}

	h.Broadcast([]byte("ping"))

	for _, conn := range conns {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("a client failed to receive the broadcast: %v", err)
		}
		if string(msg) != "ping" {
			t.Errorf("received %q, want %q", msg, "ping")
		}
	}
}
