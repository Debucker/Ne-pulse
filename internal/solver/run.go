package solver

import (
	"context"
	"fmt"
	"time"

	"ne-pulse/internal/detector"
)

// NotificationHook is invoked the instant one target city's remaining
// warning window crosses below the configured safety margin — the last
// realistic moment a real-world broadcast (SMS, Telegram, sirens) could
// still reach people before the destructive S-wave arrives at that city.
type NotificationHook func(payload WarningBroadcastPayload, target TargetWarning)

// DefaultSafetyMarginSeconds is the default remaining-warning threshold
// below which a target city is considered close enough to impact to
// warrant an emergency public notification.
const DefaultSafetyMarginSeconds = 10.0

// Run drains confirmed ruptures off events, evaluates the early-warning
// countdown for every registered city, and prints a formatted alert block
// for each — until events is closed or ctx is canceled. Intended to be
// started as its own goroutine alongside the radar's evaluator goroutine
// (see cmd/server/main.go), so a slow terminal write never backs up
// rupture detection itself.
func Run(ctx context.Context, events <-chan detector.RuptureEvent) {
	RunWithSink(ctx, events, nil)
}

// RunWithSink behaves exactly like Run, but additionally invokes sink with
// every computed WarningBroadcastPayload — e.g. to fan a rupture out to the
// dashboard websocket hub alongside the terminal alert block. sink may be
// nil (equivalent to Run).
func RunWithSink(ctx context.Context, events <-chan detector.RuptureEvent, sink func(WarningBroadcastPayload)) {
	RunWithHooks(ctx, events, sink, nil, DefaultSafetyMarginSeconds)
}

// RunWithHooks behaves exactly like RunWithSink, but additionally arms one
// emergency-notification timer per target city so notify fires at the
// exact moment (not on some polling interval) that city's remaining
// warning time crosses safetyMarginSeconds — immediately, synchronously,
// for any city already at or below the margin when the rupture confirms.
// notify may be nil (equivalent to RunWithSink).
func RunWithHooks(ctx context.Context, events <-chan detector.RuptureEvent, sink func(WarningBroadcastPayload), notify NotificationHook, safetyMarginSeconds float64) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			payload := Evaluate(event)
			fmt.Print(FormatAlert(payload))
			if sink != nil {
				sink(payload)
			}
			scheduleNotifications(payload, safetyMarginSeconds, notify)
		}
	}
}

// scheduleNotifications arms one timer per target city whose warning window
// will cross the safety margin in the future, and fires immediately for any
// city already at or below it — so notify runs at the precise instant a
// city becomes critically close to impact, not whenever some poller next
// happens to check. Timers aren't explicitly torn down on ctx cancellation:
// each is self-contained and bounded by the process's own lifetime, so
// tracking one cleanup goroutine per scheduled timer would only accumulate
// unboundedly over a long-running server's lifetime for no real benefit —
// any timer still pending at shutdown is reclaimed when the process exits.
func scheduleNotifications(payload WarningBroadcastPayload, safetyMarginSeconds float64, notify NotificationHook) {
	if notify == nil {
		return
	}
	for _, target := range payload.Targets {
		target := target
		remaining := target.TWarningSeconds - safetyMarginSeconds
		if remaining <= 0 {
			notify(payload, target)
			continue
		}
		time.AfterFunc(time.Duration(remaining*float64(time.Second)), func() {
			notify(payload, target)
		})
	}
}
