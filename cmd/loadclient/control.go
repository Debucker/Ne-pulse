package main

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/grpc"

	"ne-pulse/internal/control"
)

// reactorSwarm tracks the currently-active rupture-reactor batch's cancel
// function so that a new simulate-rupture trigger tears down the previous
// batch's ~80 device goroutines before starting its own, instead of piling
// up an ever-growing set of concurrent reactor swarms every time the
// dashboard's trigger button is pressed more than once within
// reactorLifetime of the last press.
type reactorSwarm struct {
	mu     sync.Mutex
	cancel context.CancelFunc
}

// start begins a new reactorLifetime-bounded child of parent, canceling
// whichever batch this swarm previously started (a no-op if it already
// finished on its own). Exactly one reactor batch is ever alive at a time.
func (s *reactorSwarm) start(parent context.Context) context.Context {
	ctx, cancel := context.WithTimeout(parent, reactorLifetime)

	s.mu.Lock()
	prevCancel := s.cancel
	s.cancel = cancel
	s.mu.Unlock()

	if prevCancel != nil {
		prevCancel()
	}
	return ctx
}

// listenForControlCommands maintains a connection to the server's control
// websocket for the lifetime of ctx, redialing with a short backoff if the
// connection drops or was never available (e.g. the server started after
// the load client did). Every simulate-rupture command received replaces
// the shared wave state, which every device goroutine reads lock-free via
// activeWave.Load(), and also spawns a cluster of reactor devices right on
// the epicenter (see spawnRuptureReactors) so the detector's coincidence
// threshold is actually reachable.
func listenForControlCommands(ctx context.Context, controlURL string, activeWave *atomic.Pointer[waveState], conns []*grpc.ClientConn, stats *chaosStats) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		conn, _, err := websocket.DefaultDialer.Dial(controlURL, nil)
		if err != nil {
			log.Printf("loadclient: control channel dial to %s failed, retrying in 2s: %v", controlURL, err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
			continue
		}

		log.Printf("loadclient: connected to control channel at %s", controlURL)
		readControlLoop(ctx, conn, activeWave, conns, stats)
		conn.Close()

		select {
		case <-ctx.Done():
			return
		default:
			log.Printf("loadclient: control channel disconnected, reconnecting in 2s...")
			time.Sleep(2 * time.Second)
		}
	}
}

func readControlLoop(ctx context.Context, conn *websocket.Conn, activeWave *atomic.Pointer[waveState], conns []*grpc.ClientConn, stats *chaosStats) {
	var swarm reactorSwarm
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var cmd control.RuptureCommand
			if err := json.Unmarshal(msg, &cmd); err != nil || cmd.Type != "simulate-rupture" {
				continue
			}
			log.Printf("loadclient: RUPTURE COMMAND RECEIVED — epicenter=(%.4f, %.4f); wavefront propagation starting now",
				cmd.EpicenterLat, cmd.EpicenterLng)
			activeWave.Store(newWaveFromEpicenter(cmd.EpicenterLat, cmd.EpicenterLng))
			reactorCtx := swarm.start(ctx)
			go spawnRuptureReactors(reactorCtx, conns, cmd.EpicenterLat, cmd.EpicenterLng, activeWave, stats)
		}
	}()

	select {
	case <-ctx.Done():
		conn.Close()
		<-done
	case <-done:
	}
}
