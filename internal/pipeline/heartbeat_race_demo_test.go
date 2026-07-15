package pipeline

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type heartbeatSignalHandler struct {
	slog.Handler
	once sync.Once
	seen chan struct{}
}

func (h *heartbeatSignalHandler) Handle(ctx context.Context, record slog.Record) error {
	if record.Message == EventCommandHeartbeat {
		capturedOutput := false
		record.Attrs(func(attr slog.Attr) bool {
			if attr.Key == "bytes_captured" && attr.Value.Kind() == slog.KindInt64 && attr.Value.Int64() > 0 {
				capturedOutput = true
			}
			return true
		})
		if capturedOutput {
			h.once.Do(func() { close(h.seen) })
		}
	}
	return h.Handler.Handle(ctx, record)
}

// TestExecuteCommand_HeartbeatRaceFreeOnNoisyStdout is the regression test
// for bd-1vhq5: previously the heartbeat goroutine read stdoutBuf.Len()
// while exec.Cmd's writer goroutine wrote to the same cappedWriter,
// triggering a data race on bytes.Buffer. cappedWriter now serialises
// access via its internal mutex; this test would fail under -race before
// that fix.
func TestExecuteCommand_HeartbeatRaceFreeOnNoisyStdout(t *testing.T) {
	prev := commandHeartbeatInterval
	commandHeartbeatInterval = 5 * time.Millisecond
	t.Cleanup(func() { commandHeartbeatInterval = prev })

	heartbeatSeen := make(chan struct{})
	handler := &heartbeatSignalHandler{
		Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}),
		seen:    heartbeatSeen,
	}
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(prevLogger) })

	e := newCommandTestExecutor(t)
	step := &Step{
		ID:      "noisy",
		Command: "i=0; while :; do printf 'line-%s\\n' \"$i\"; i=$((i + 1)); done",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resultDone := make(chan StepResult, 1)
	go func() {
		resultDone <- e.executeCommand(ctx, step, &Workflow{Name: "test"})
	}()

	waitForResult := func() StepResult {
		t.Helper()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case result := <-resultDone:
			return result
		case <-timer.C:
			t.Fatal("executeCommand did not return within 5s of cancellation")
			return StepResult{}
		}
	}

	heartbeatTimer := time.NewTimer(5 * time.Second)
	defer heartbeatTimer.Stop()
	select {
	case <-heartbeatSeen:
		cancel()
	case <-heartbeatTimer.C:
		cancel()
		result := waitForResult()
		t.Fatalf("no heartbeat observed with captured stdout; result = %+v", result)
	}

	result := waitForResult()
	if result.Status != StatusCancelled {
		t.Fatalf("Status = %q, want %q after heartbeat-triggered cancellation; error = %+v", result.Status, StatusCancelled, result.Error)
	}
}
