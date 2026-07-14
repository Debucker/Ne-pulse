// Package hub implements a generic websocket fan-out broadcaster: any
// number of connected peers (browser dashboard clients, or a load-client
// process listening for control commands) receive every message pushed
// through Broadcast. It is used twice in ne-pulse — once for the telemetry/
// rupture-alert stream (internal/dashboard) and once for chaos-simulation
// control commands (internal/control) — so the connection bookkeeping only
// has to be written once.
package hub

import (
	"context"
	"log"
	"net/http"
	"sync/atomic"

	"github.com/gorilla/websocket"
)

// client wraps one connected peer with its own outbound buffer, so one
// slow reader can never stall delivery to every other subscriber.
type client struct {
	conn *websocket.Conn
	send chan []byte
}

// Hub is a single-owning-goroutine broadcaster: all client bookkeeping
// (the registered-peer set) is exclusively touched by the goroutine
// running Run, matching the "no mutex, one owner" pattern used throughout
// ne-pulse (WorkerPool, SpatialRadar). Every other goroutine only ever talks to
// it through channels.
type Hub struct {
	ctx context.Context

	upgrader websocket.Upgrader

	register   chan *client
	unregister chan *client
	broadcast  chan []byte

	// retained holds the last message passed to SetRetained (a []byte, or
	// nil if never set). Only Run's goroutine ever reads it, and only to
	// replay it to a client that's just registering — see the register
	// case below — so there's no ordering race with a concurrent
	// Broadcast: a freshly-registered client always sees the retained
	// snapshot before any subsequent live broadcast can reach it.
	retained atomic.Value

	clientCount atomic.Int64
	dropped     atomic.Int64
}

// New builds a Hub bound to ctx: Run exits when ctx is canceled, and every
// blocking handoff between ServeWS's goroutines and Run also selects on
// ctx.Done() so nothing can leak a goroutine blocked forever on a channel
// send after Run has already stopped receiving.
//
// checkOrigin decides whether to accept a websocket handshake based on its
// Origin header — pass nil to accept every origin (fine for a purely
// internal/loopback hub), or a strict allowlist function for anything a
// browser on the public internet connects to. See cmd/server/main.go for
// the production allowlist this is wired up with.
func New(ctx context.Context, checkOrigin func(r *http.Request) bool) *Hub {
	if checkOrigin == nil {
		checkOrigin = func(r *http.Request) bool { return true }
	}
	return &Hub{
		ctx: ctx,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin:     checkOrigin,
		},
		register:   make(chan *client),
		unregister: make(chan *client),
		broadcast:  make(chan []byte, 256),
	}
}

// Run owns the registered-client set for the Hub's lifetime. Call it once,
// typically as its own goroutine.
func (h *Hub) Run() {
	clients := make(map[*client]struct{})
	for {
		select {
		case <-h.ctx.Done():
			for c := range clients {
				close(c.send)
			}
			return
		case c := <-h.register:
			clients[c] = struct{}{}
			h.clientCount.Store(int64(len(clients)))
			if v := h.retained.Load(); v != nil {
				if msg, ok := v.([]byte); ok {
					select {
					case c.send <- msg:
					default:
					}
				}
			}
		case c := <-h.unregister:
			if _, ok := clients[c]; ok {
				delete(clients, c)
				close(c.send)
				h.clientCount.Store(int64(len(clients)))
			}
		case msg := <-h.broadcast:
			for c := range clients {
				select {
				case c.send <- msg:
				default:
					// A stuck/slow client is dropped rather than allowed
					// to stall delivery to everyone else.
					delete(clients, c)
					close(c.send)
					h.dropped.Add(1)
				}
			}
			h.clientCount.Store(int64(len(clients)))
		}
	}
}

// Broadcast enqueues a message for delivery to every currently registered
// client. Never blocks: callers (e.g. a periodic dashboard-snapshot
// ticker) must never stall behind slow network I/O, so a full internal
// buffer just drops the message and counts it.
func (h *Hub) Broadcast(message []byte) bool {
	select {
	case h.broadcast <- message:
		return true
	default:
		h.dropped.Add(1)
		return false
	}
}

// SetRetained stores message as the current "last known state," replayed to
// every newly-registered client the instant it connects (see the register
// case in Run) — this is what lets a browser tab opened well after the
// last broadcast still see current state immediately, rather than waiting
// for the next periodic tick or seeing nothing at all if whatever was
// generating traffic has already stopped. It does not itself push message
// to already-connected clients; call Broadcast for that. message must be
// non-nil (atomic.Value panics if a nil interface is ever stored after a
// concrete value already was).
func (h *Hub) SetRetained(message []byte) {
	h.retained.Store(message)
}

func (h *Hub) ClientCount() int64 { return h.clientCount.Load() }
func (h *Hub) Dropped() int64     { return h.dropped.Load() }

// ServeWS upgrades an incoming HTTP request to a websocket connection and
// registers it with the hub. Connections are outbound-only from the hub's
// side — the read loop's only job is to detect disconnects (and, for
// control clients, notice a closed connection) so the peer is unregistered
// promptly.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("hub: websocket upgrade failed: %v", err)
		return
	}
	c := &client{conn: conn, send: make(chan []byte, 32)}

	select {
	case h.register <- c:
	case <-h.ctx.Done():
		conn.Close()
		return
	}

	go c.writePump()
	go c.readPump(h)
}

func (c *client) writePump() {
	defer c.conn.Close()
	for msg := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}
	}
	_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
}

func (c *client) readPump(h *Hub) {
	defer func() {
		select {
		case h.unregister <- c:
		case <-h.ctx.Done():
		}
	}()
	c.conn.SetReadLimit(4096)
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}
