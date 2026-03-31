//go:build !windows

package testutil

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func withGlobalTmuxTestLock(fn func()) {
	lockPath := filepath.Join(os.TempDir(), "ntm_tmux_tests.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		fn()
		return
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		fn()
		return
	}
	defer func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}()

	fn()
}

func tryWithGlobalTmuxTestLock(fn func()) bool {
	lockPath := filepath.Join(os.TempDir(), "ntm_tmux_tests.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		fn()
		return true
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return false
		}
		fn()
		return true
	}
	defer func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}()

	fn()
	return true
}
