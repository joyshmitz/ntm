package testutil

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"
	"testing"
)

// TmuxTestThrottle limits concurrent tmux session spawning in tests.
// This prevents fork bombs when running tests with high parallelism.
//
// The default limit is 8 concurrent tmux-spawning tests, which is safe
// even on systems with lower process limits. Override with NTM_TEST_PARALLEL.
var TmuxTestThrottle = newThrottle(getTmuxTestLimit())

func getTmuxTestLimit() int {
	if env := os.Getenv("NTM_TEST_PARALLEL"); env != "" {
		if n, err := strconv.Atoi(env); err == nil && n > 0 {
			return n
		}
	}
	// Default to 8, or GOMAXPROCS/8 if that's larger, capped at 16
	limit := runtime.GOMAXPROCS(0) / 8
	if limit < 8 {
		limit = 8
	}
	if limit > 16 {
		limit = 16
	}
	return limit
}

// throttle is a counting semaphore for limiting concurrent operations.
type throttle struct {
	sem chan struct{}
	mu  sync.Mutex
}

func newThrottle(limit int) *throttle {
	return &throttle{
		sem: make(chan struct{}, limit),
	}
}

// Acquire acquires a slot from the throttle, blocking if necessary.
// Returns a release function that must be called when done.
func (th *throttle) Acquire() func() {
	th.sem <- struct{}{}
	return func() {
		<-th.sem
	}
}

// AcquireForTest acquires a slot and registers cleanup to release it.
// This is the recommended way to use the throttle in tests.
func (th *throttle) AcquireForTest(t *testing.T) {
	t.Helper()
	th.sem <- struct{}{}
	t.Cleanup(func() {
		<-th.sem
	})
}

// RequireTmuxThrottled combines RequireTmux with throttle acquisition.
// Use this at the start of any test that spawns tmux sessions.
//
// Example:
//
//	func TestSpawnSession(t *testing.T) {
//	    testutil.RequireTmuxThrottled(t)
//	    // ... test code that spawns tmux sessions
//	}
func RequireTmuxThrottled(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping real tmux session integration in short mode")
	}
	RequireTmux(t)
	// Cross-process lock to prevent tmux overload when `go test ./...` runs
	// multiple packages in parallel.
	acquireGlobalTmuxTestLock(t)
	TmuxTestThrottle.AcquireForTest(t)
}

// AcquireGlobalTmuxTestLockForTest serializes tmux access across package test
// processes without skipping short-mode coverage or requiring a live session.
func AcquireGlobalTmuxTestLockForTest(t *testing.T) {
	t.Helper()
	acquireGlobalTmuxTestLock(t)
}

// IsolateTmuxTestProcess gives a package test binary its own tmux server.
// Call it from TestMain before m.Run so tests and child processes cannot
// discover or mutate the invoking user's tmux server.
func IsolateTmuxTestProcess() error {
	dir, err := os.MkdirTemp("", "ntm-tmux-test-*")
	if err != nil {
		return fmt.Errorf("create tmux test directory: %w", err)
	}

	settings := []struct {
		key   string
		value string
	}{
		{key: "TMUX", value: ""},
		{key: "TMUX_PANE", value: ""},
		{key: "TMUX_TMPDIR", value: dir},
	}
	for _, setting := range settings {
		if err := os.Setenv(setting.key, setting.value); err != nil {
			return fmt.Errorf("set %s for tmux test isolation: %w", setting.key, err)
		}
	}
	return nil
}

// IntegrationTestPrecheckThrottled runs integration prechecks with throttling.
// Use this instead of IntegrationTestPrecheck for tests that spawn tmux.
func IntegrationTestPrecheckThrottled(t *testing.T) {
	t.Helper()
	RequireIntegration(t)
	RequireTmuxThrottled(t)
	RequireNTMBinary(t)
}

// E2ETestPrecheckThrottled runs E2E prechecks with throttling.
// Use this instead of E2ETestPrecheck for tests that spawn tmux.
func E2ETestPrecheckThrottled(t *testing.T) {
	t.Helper()
	RequireE2E(t)
	RequireTmuxThrottled(t)
	RequireNTMBinary(t)
}
