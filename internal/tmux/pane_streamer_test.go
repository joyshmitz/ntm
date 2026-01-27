package tmux

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"
)

func TestPaneStreamerConfig_Defaults(t *testing.T) {
	cfg := DefaultPaneStreamerConfig()

	if cfg.FIFODir != "/tmp/ntm_pane_streams" {
		t.Errorf("expected FIFODir /tmp/ntm_pane_streams, got %s", cfg.FIFODir)
	}
	if cfg.MaxLinesPerEvent != 100 {
		t.Errorf("expected MaxLinesPerEvent 100, got %d", cfg.MaxLinesPerEvent)
	}
	if cfg.FlushInterval != 50*time.Millisecond {
		t.Errorf("expected FlushInterval 50ms, got %v", cfg.FlushInterval)
	}
	if cfg.FallbackPollInterval != 500*time.Millisecond {
		t.Errorf("expected FallbackPollInterval 500ms, got %v", cfg.FallbackPollInterval)
	}
	if cfg.FallbackPollLines != LinesHealthCheck {
		t.Errorf("expected FallbackPollLines %d, got %d", LinesHealthCheck, cfg.FallbackPollLines)
	}
}

func TestStreamEvent_Fields(t *testing.T) {
	event := StreamEvent{
		Target:    "mysession:0",
		Lines:     []string{"line1", "line2"},
		Seq:       42,
		Timestamp: time.Now(),
		IsFull:    true,
	}

	if event.Target != "mysession:0" {
		t.Errorf("expected target mysession:0, got %s", event.Target)
	}
	if len(event.Lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(event.Lines))
	}
	if event.Seq != 42 {
		t.Errorf("expected seq 42, got %d", event.Seq)
	}
	if !event.IsFull {
		t.Error("expected IsFull=true")
	}
}

func TestSimpleHash(t *testing.T) {
	// Short strings return as-is
	short := "hello"
	if hash := simpleHash(short); hash != short {
		t.Errorf("expected short hash to equal string, got %s", hash)
	}

	// Long strings get hashed
	long := "this is a very long string that exceeds 64 characters and should be hashed differently"
	hash := simpleHash(long)
	if hash == long {
		t.Error("expected long string to be hashed")
	}

	// Different strings should produce different hashes
	long2 := "this is a very long string that exceeds 64 characters but ends with something else"
	hash2 := simpleHash(long2)
	if hash == hash2 {
		t.Error("expected different strings to produce different hashes")
	}
}

func TestCreateFIFO(t *testing.T) {
	dir := t.TempDir()
	fifoPath := dir + "/test.fifo"

	if err := createFIFO(fifoPath); err != nil {
		t.Fatalf("createFIFO failed: %v", err)
	}

	info, err := os.Stat(fifoPath)
	if err != nil {
		t.Fatalf("stat fifo: %v", err)
	}

	// Check it's a named pipe
	if info.Mode()&os.ModeNamedPipe == 0 {
		t.Errorf("expected named pipe, got mode %v", info.Mode())
	}
}

func TestStreamManager_Lifecycle(t *testing.T) {
	var mu sync.Mutex
	var events []StreamEvent

	callback := func(event StreamEvent) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	}

	cfg := DefaultPaneStreamerConfig()
	cfg.FIFODir = t.TempDir()
	cfg.FallbackPollInterval = 100 * time.Millisecond

	sm := NewStreamManager(DefaultClient, callback, cfg)

	// Check empty stats
	stats := sm.Stats()
	if stats["active_streams"].(int) != 0 {
		t.Errorf("expected 0 active streams, got %v", stats["active_streams"])
	}

	// ListActive should be empty
	active := sm.ListActive()
	if len(active) != 0 {
		t.Errorf("expected empty active list, got %v", active)
	}

	// Stop all should be safe when empty
	sm.StopAll()
}

func TestPaneStreamer_DoubleStart(t *testing.T) {
	callback := func(event StreamEvent) {}
	cfg := DefaultPaneStreamerConfig()
	cfg.FIFODir = t.TempDir()

	ps := NewPaneStreamer(DefaultClient, "nonexistent:0", callback, cfg)

	// First start will likely fail (no tmux session) but sets running=true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start and immediately stop
	_ = ps.Start(ctx)
	defer ps.Stop()

	// Second start should fail with already running
	err := ps.Start(ctx)
	if err == nil {
		t.Error("expected error on double start")
	}
}

func TestStreamManager_StartStop(t *testing.T) {
	callback := func(event StreamEvent) {}
	cfg := DefaultPaneStreamerConfig()
	cfg.FIFODir = t.TempDir()

	sm := NewStreamManager(DefaultClient, callback, cfg)
	defer sm.StopAll()

	// Start streaming (will fail because no tmux session, but tests the flow)
	_ = sm.StartStream("fake:0")

	// Should be tracked
	active := sm.ListActive()
	if len(active) != 1 {
		t.Errorf("expected 1 active stream, got %d", len(active))
	}

	// Idempotent - starting again should not error
	_ = sm.StartStream("fake:0")
	active = sm.ListActive()
	if len(active) != 1 {
		t.Errorf("expected still 1 active stream, got %d", len(active))
	}

	// Stop specific stream
	sm.StopStream("fake:0")
	active = sm.ListActive()
	if len(active) != 0 {
		t.Errorf("expected 0 active streams after stop, got %d", len(active))
	}

	// Stop again should be safe
	sm.StopStream("fake:0")
}

func TestPaneStreamer_UsingFallback(t *testing.T) {
	callback := func(event StreamEvent) {}
	cfg := DefaultPaneStreamerConfig()
	cfg.FIFODir = t.TempDir()

	ps := NewPaneStreamer(DefaultClient, "nonexistent:0", callback, cfg)

	// Before start, fallback should be false
	if ps.UsingFallback() {
		t.Error("expected fallback=false before start")
	}

	// After starting with a nonexistent pane, should switch to fallback
	ctx, cancel := context.WithCancel(context.Background())
	_ = ps.Start(ctx)

	// Give it a moment to switch to fallback
	time.Sleep(100 * time.Millisecond)

	// Now it should be using fallback
	if !ps.UsingFallback() {
		t.Error("expected fallback=true after pipe-pane fails")
	}

	cancel()
	ps.Stop()
}

func TestPaneStreamer_Target(t *testing.T) {
	callback := func(event StreamEvent) {}
	cfg := DefaultPaneStreamerConfig()

	ps := NewPaneStreamer(DefaultClient, "mysession:5", callback, cfg)

	if target := ps.Target(); target != "mysession:5" {
		t.Errorf("expected target mysession:5, got %s", target)
	}
}

func TestStreamManager_Stats(t *testing.T) {
	callback := func(event StreamEvent) {}
	cfg := DefaultPaneStreamerConfig()
	cfg.FIFODir = t.TempDir()

	sm := NewStreamManager(DefaultClient, callback, cfg)
	defer sm.StopAll()

	stats := sm.Stats()

	// Check expected keys
	if _, ok := stats["active_streams"]; !ok {
		t.Error("expected active_streams in stats")
	}
	if _, ok := stats["pipe_pane_count"]; !ok {
		t.Error("expected pipe_pane_count in stats")
	}
	if _, ok := stats["fallback_count"]; !ok {
		t.Error("expected fallback_count in stats")
	}
	if _, ok := stats["fifo_dir"]; !ok {
		t.Error("expected fifo_dir in stats")
	}
	if _, ok := stats["flush_interval_ms"]; !ok {
		t.Error("expected flush_interval_ms in stats")
	}

	if stats["fifo_dir"] != cfg.FIFODir {
		t.Errorf("expected fifo_dir %s, got %v", cfg.FIFODir, stats["fifo_dir"])
	}
}
