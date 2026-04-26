// Package process provides PID-based process liveness checks.
package process

import (
	"fmt"
	"os"
	"os/exec"
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

	// Fast path: inspect /proc state directly on Linux. Zombie and dead
	// processes still have a /proc entry until reaped, but they are not alive.
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid)); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if !strings.HasPrefix(line, "State:") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				state := fields[1]
				return state != "Z" && state != "X" && state != "x"
			}
			break
		}
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

	// Fallback to pgrep (works on macOS and Linux without /proc/.../children)
	cmd := exec.Command("pgrep", "-P", strconv.Itoa(parentPID))
	out, err := cmd.Output()
	if err == nil {
		parts := strings.Fields(string(out))
		if len(parts) > 0 {
			pid, err := strconv.Atoi(parts[0])
			if err == nil && pid > 0 {
				return pid
			}
		}
	}

	return 0
}

// IsChildAlive is an alias for HasChildAlive for backward compatibility.
var IsChildAlive = HasChildAlive

// GetCmdline returns the full argv of the process with the given PID,
// or an empty slice if it can't be read. On Linux this uses
// `/proc/<pid>/cmdline`; on other systems it falls back to `ps -o
// command=`.
//
// This is useful for distinguishing wrapper processes (`bun`, `node`,
// `npx`, `python`) from the agent binary they launched: the wrapper
// shows up as the process's basename, but the agent it's running is
// usually visible in argv (e.g. `bun /home/.../codex ...`).
func GetCmdline(pid int) []string {
	if pid <= 0 {
		return nil
	}
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid)); err == nil {
		// /proc/.../cmdline is NUL-separated, with a trailing NUL.
		raw := strings.TrimRight(string(data), "\x00")
		if raw == "" {
			return nil
		}
		return strings.Split(raw, "\x00")
	}
	// Fallback for non-Linux. `ps -o command=` returns a single
	// space-separated string with no easy way to recover argv0
	// boundaries inside arguments, but it's enough for substring
	// matching against agent binary names.
	cmd := exec.Command("ps", "-o", "command=", "-p", strconv.Itoa(pid))
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return nil
	}
	return strings.Fields(line)
}

// GetChildPIDs returns up to `limit` direct children of `parentPID`.
// Linux fast path reads `/proc/<pid>/task/<pid>/children`; falls back
// to `pgrep -P`. Returns nil on failure.
func GetChildPIDs(parentPID int, limit int) []int {
	if parentPID <= 0 {
		return nil
	}
	if limit <= 0 {
		limit = 8
	}

	collect := func(parts []string) []int {
		var out []int
		for _, p := range parts {
			pid, err := strconv.Atoi(p)
			if err != nil || pid <= 0 {
				continue
			}
			out = append(out, pid)
			if len(out) >= limit {
				return out
			}
		}
		return out
	}

	taskPath := fmt.Sprintf("/proc/%d/task/%d/children", parentPID, parentPID)
	if data, err := os.ReadFile(taskPath); err == nil {
		return collect(strings.Fields(string(data)))
	}
	cmd := exec.Command("pgrep", "-P", strconv.Itoa(parentPID))
	if out, err := cmd.Output(); err == nil {
		return collect(strings.Fields(string(out)))
	}
	return nil
}

// processStateNames maps single-character /proc state codes to human names.
var processStateNames = map[string]string{
	"R": "running",
	"S": "sleeping",
	"D": "disk sleep",
	"Z": "zombie",
	"T": "stopped",
	"t": "tracing stop",
	"X": "dead",
	"x": "dead",
	"K": "wakekill",
	"W": "waking",
	"P": "parked",
	"I": "idle",
}

// GetProcessState reads the process state.
// It tries /proc/<pid>/status on Linux and falls back to ps on macOS/Linux.
// Returns the single-character state code (R, S, D, Z, T, etc.),
// a human-readable name, and any error.
func GetProcessState(pid int) (string, string, error) {
	if pid <= 0 {
		return "", "", fmt.Errorf("invalid pid: %d", pid)
	}

	// Try /proc first (Linux)
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid)); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "State:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					state := fields[1]
					name := processStateNames[state]
					if name == "" {
						name = "unknown"
					}
					return state, name, nil
				}
			}
		}
	}

	// Fallback to ps (macOS and Linux)
	// 'state' column provides the process state
	cmd := exec.Command("ps", "-o", "state=", "-p", strconv.Itoa(pid))
	out, err := cmd.Output()
	if err == nil {
		state := strings.TrimSpace(string(out))
		if state != "" {
			// ps state can have multiple chars (e.g., S+ on macOS), we take the first
			shortState := string(state[0])
			name := processStateNames[shortState]
			if name == "" {
				// macOS specific states
				switch shortState {
				case "I":
					name = "idle"
				case "U":
					name = "uninterruptible"
				default:
					name = "unknown"
				}
			}
			return shortState, name, nil
		}
	}

	return "", "", fmt.Errorf("failed to get process state for pid %d", pid)
}
