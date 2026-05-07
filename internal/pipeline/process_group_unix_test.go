//go:build linux || darwin

package pipeline

import (
	"context"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestExecuteCommand_CancelKillsProcessGroupChild(t *testing.T) {
	e := newCommandTestExecutor(t)
	pidFile := e.config.ProjectDir + "/child.pid"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	step := &Step{
		ID:      "process-group-cancel",
		Command: "sleep 30 & echo $! > " + strconv.Quote(pidFile) + "; wait",
		Timeout: Duration{Duration: 10 * time.Second},
	}

	resultCh := make(chan StepResult, 1)
	go func() {
		resultCh <- e.executeCommand(ctx, step, &Workflow{Name: "test"})
	}()

	childPID := waitForPIDFile(t, pidFile)
	if !processExists(childPID) {
		t.Fatalf("child process %d was not running before cancellation", childPID)
	}

	cancel()

	select {
	case result := <-resultCh:
		if result.Status != StatusCancelled {
			t.Fatalf("Status = %s, want cancelled; error=%+v", result.Status, result.Error)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("executeCommand did not return within cancellation grace period")
	}

	waitUntil(t, 2*time.Second, func() bool {
		return !processExists(childPID)
	}, "child process still exists after process-group cancellation")
}

func waitForPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if parseErr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for child PID file %s", path)
	return 0
}

func waitUntil(t *testing.T, timeout time.Duration, ok func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(message)
}

func processExists(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errorsIsPermission(err)
}

func errorsIsPermission(err error) bool {
	return err == syscall.EPERM
}
