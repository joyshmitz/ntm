//go:build windows

package cli

import "os"

// termProcess terminates the process `pid`.
//
// Windows has no SIGTERM delivery for an arbitrary unrelated process, so this
// falls back to a hard termination via the process handle (TerminateProcess).
// Orphan reaping is fundamentally a Unix/tmux concern in ntm; on Windows this
// keeps the build compiling and still cleans up a leaked child, just without the
// graceful-then-forceful escalation. Errors are ignored (the process may already
// have exited), matching the Unix behavior.
func termProcess(pid int) {
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Kill()
	}
}

// killProcess forcefully terminates the process `pid`.
func killProcess(pid int) {
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Kill()
	}
}
