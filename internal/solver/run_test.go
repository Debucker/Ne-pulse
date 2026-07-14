package solver

import (
	"context"
	"sync"
	"testing"
	"time"

	"ne-pulse/internal/detector"
)

func TestRunWithSink_InvokesSinkWithEvaluatedPayload(t *testing.T) {
	events := make(chan detector.RuptureEvent, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	received := make(chan WarningBroadcastPayload, 1)
	go RunWithSink(ctx, events, func(p WarningBroadcastPayload) { received <- p })

	events <- detector.RuptureEvent{
		CellID:        7,
		DeviceCount:   10,
		EpicenterLat:  41.30,
		EpicenterLng:  69.25,
		WindowEnd:     time.Now(),
		TriggerDevice: "device-x",
	}

	select {
	case payload := <-received:
		if payload.CellID != 7 || payload.DeviceCount != 10 {
			t.Errorf("payload = %+v, want CellID=7 DeviceCount=10", payload)
		}
		if len(payload.Targets) != len(Cities()) {
			t.Errorf("got %d targets, want %d", len(payload.Targets), len(Cities()))
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("sink was never invoked")
	}
}

func TestRunWithSink_NilSinkIsSafe(t *testing.T) {
	events := make(chan detector.RuptureEvent, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		RunWithSink(ctx, events, nil)
		close(done)
	}()

	events <- detector.RuptureEvent{CellID: 1, EpicenterLat: 41.30, EpicenterLng: 69.25, WindowEnd: time.Now()}
	close(events)

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("RunWithSink(nil sink) did not exit after the channel closed")
	}
}

func TestScheduleNotifications_FiresImmediatelyForTargetsAtOrBelowMargin(t *testing.T) {
	payload := WarningBroadcastPayload{
		Targets: []TargetWarning{
			{City: City{Name: "Tashkent"}, TWarningSeconds: -1, BlindZone: true},
			{City: City{Name: "Samarkand"}, TWarningSeconds: 5},
		},
	}

	var mu sync.Mutex
	var fired []string
	notify := func(_ WarningBroadcastPayload, target TargetWarning) {
		mu.Lock()
		defer mu.Unlock()
		fired = append(fired, target.City.Name)
	}

	scheduleNotifications(payload, 10.0, notify) // margin=10s; both targets are already <= it

	mu.Lock()
	defer mu.Unlock()
	if len(fired) != 2 {
		t.Fatalf("fired = %v, want both cities to fire immediately", fired)
	}
}

func TestScheduleNotifications_SchedulesDeferredFireForTargetsAboveMargin(t *testing.T) {
	payload := WarningBroadcastPayload{
		Targets: []TargetWarning{
			{City: City{Name: "Bukhara"}, TWarningSeconds: 10.05}, // margin=10.0 -> fires in ~50ms
		},
	}

	fired := make(chan string, 1)
	notify := func(_ WarningBroadcastPayload, target TargetWarning) {
		fired <- target.City.Name
	}

	start := time.Now()
	scheduleNotifications(payload, 10.0, notify)

	select {
	case name := <-fired:
		if name != "Bukhara" {
			t.Errorf("fired = %q, want Bukhara", name)
		}
		if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
			t.Errorf("fired after only %s, want a deferred (not immediate) dispatch", elapsed)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("deferred notification never fired")
	}
}

func TestScheduleNotifications_NilHookIsSafe(t *testing.T) {
	payload := WarningBroadcastPayload{Targets: []TargetWarning{{City: City{Name: "Tashkent"}, TWarningSeconds: 0}}}
	scheduleNotifications(payload, 10.0, nil) // must not panic
}

func TestRunWithHooks_InvokesNotifyForEveryTargetOnConfirmedRupture(t *testing.T) {
	events := make(chan detector.RuptureEvent, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	notified := make(chan string, len(Cities()))
	go RunWithHooks(ctx, events, nil, func(_ WarningBroadcastPayload, target TargetWarning) {
		notified <- target.City.Name
	}, 100000.0) // enormous margin so every city fires immediately, deterministically

	events <- detector.RuptureEvent{CellID: 1, EpicenterLat: 41.30, EpicenterLng: 69.25, WindowEnd: time.Now()}

	deadline := time.After(500 * time.Millisecond)
	count := 0
	for count < len(Cities()) {
		select {
		case <-notified:
			count++
		case <-deadline:
			t.Fatalf("only received %d/%d notifications", count, len(Cities()))
		}
	}
}
