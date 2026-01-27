//go:build unix

package tmux

import (
	"os"
	"syscall"
)

// createFIFO creates a named pipe (FIFO) at the given path.
func createFIFO(path string) error {
	// Remove any existing file
	_ = os.Remove(path)
	return syscall.Mkfifo(path, 0600)
}
