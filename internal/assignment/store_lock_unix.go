//go:build unix

package assignment

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func acquireAssignmentFileLock(ctx context.Context, lockPath string) (func(), error) {
	localUnlock, err := lockAssignmentPathLocally(ctx, lockPath)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(lockPath), 0700); err != nil {
		localUnlock()
		return nil, err
	}

	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		localUnlock()
		return nil, err
	}
	for {
		err = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = lockFile.Close()
			localUnlock()
			return nil, err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			_ = lockFile.Close()
			localUnlock()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}

	return func() {
		_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		_ = lockFile.Close()
		localUnlock()
	}, nil
}

func syncAssignmentDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
