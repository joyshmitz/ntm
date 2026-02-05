package events

import (
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// SubscribeAll (global wrapper) — 0% → 100%
// ---------------------------------------------------------------------------

func TestGlobal_SubscribeAll(t *testing.T) {
	// Not parallel: uses global DefaultBus.
	var received atomic.Int32

	unsub := SubscribeAll(func(e BusEvent) {
		received.Add(1)
	})
	defer unsub()

	event := BaseEvent{Type: "global_subscribe_all_test", Timestamp: time.Now()}
	PublishSync(event)

	if got := received.Load(); got < 1 {
		t.Errorf("SubscribeAll handler received %d events, want >=1", got)
	}
}

// ---------------------------------------------------------------------------
// Publish (global async wrapper) — 0% → 100%
// ---------------------------------------------------------------------------

func TestGlobal_Publish(t *testing.T) {
	// Not parallel: uses global DefaultBus.
	var received atomic.Int32

	unsub := Subscribe("global_publish_test", func(e BusEvent) {
		received.Add(1)
	})
	defer unsub()

	event := BaseEvent{Type: "global_publish_test", Timestamp: time.Now()}
	Publish(event)

	// Publish is async — give it a brief moment.
	time.Sleep(50 * time.Millisecond)

	if got := received.Load(); got != 1 {
		t.Errorf("Publish handler received %d events, want 1", got)
	}
}

// ---------------------------------------------------------------------------
// History (global wrapper) — 0% → 100%
// ---------------------------------------------------------------------------

func TestGlobal_History(t *testing.T) {
	// Not parallel: uses global DefaultBus.

	// Publish a known event synchronously so it's in history.
	event := BaseEvent{Type: "global_history_test", Timestamp: time.Now()}
	PublishSync(event)

	history := History(10)
	// History should contain at least the event we just published.
	found := false
	for _, h := range history {
		if h.EventType() == "global_history_test" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected global_history_test event in History, got %d events", len(history))
	}
}

// ---------------------------------------------------------------------------
// DefaultEmitter — 0% → 100%
// ---------------------------------------------------------------------------

func TestDefaultEmitter(t *testing.T) {
	// Not parallel: accesses global singleton.
	em := DefaultEmitter()
	if em == nil {
		t.Fatal("DefaultEmitter() returned nil")
	}

	// Should return the same instance on subsequent calls.
	em2 := DefaultEmitter()
	if em != em2 {
		t.Error("DefaultEmitter() should return the same instance")
	}
}
