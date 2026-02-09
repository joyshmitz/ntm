// Package process provides PID-based process liveness checks.
package process

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// IsAlive checks whether a process with the given PID is still running.
// It uses /proc on Linux for an efficient, non-racy check and falls back
// to kill(pid, 0) on other platforms.
func IsAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	// Fast path: check /proc/<pid>/status exists (Linux).
	if _, err := os.Stat(fmt.Sprintf("/proc/%d/status", pid)); err == nil {
		return true
	}

	// Fallback: signal 0 check (works on all POSIX systems).
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

// HasChildAlive returns true if the given shell PID has at least one
// living child process. This is useful for detecting whether an agent
// launched inside a tmux shell pane is still running.
func HasChildAlive(shellPID int) bool {
	if shellPID <= 0 {
		return false
	}

	childPID := GetChildPID(shellPID)
	if childPID <= 0 {
		return false
	}
	return IsAlive(childPID)
}

// GetChildPID returns the first child PID of the given parent, or 0 if
// no child is found. It reads /proc on Linux and falls back to pgrep.
func GetChildPID(parentPID int) int {
	if parentPID <= 0 {
		return 0
	}

	// Try /proc first (Linux).
	taskPath := fmt.Sprintf("/proc/%d/task/%d/children", parentPID, parentPID)
	data, err := os.ReadFile(taskPath)
	if err == nil {
		parts := strings.Fields(string(data))
		if len(parts) > 0 {
			pid, err := strconv.Atoi(parts[0])
			if err == nil && pid > 0 {
				return pid
			}
		}
	}

	return 0
}
