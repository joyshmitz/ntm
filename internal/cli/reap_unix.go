//go:build !windows

package cli

import "syscall"

// termProcess sends SIGTERM (graceful termination) to the process `pid`.
//
// Errors are intentionally ignored: the process may exit between the caller's
// liveness check and this signal, which is exactly the outcome the orphan reaper
// wants. See reapOrphanProcesses in send.go.
func termProcess(pid int) {
	_ = syscall.Kill(pid, syscall.SIGTERM)
}

// killProcess sends SIGKILL (forceful termination) to the process `pid`.
func killProcess(pid int) {
	_ = syscall.Kill(pid, syscall.SIGKILL)
}
