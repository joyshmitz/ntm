package assignment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
)

type assignmentPathLock struct {
	token chan struct{}
	refs  int
}

var assignmentPathLocks = struct {
	sync.Mutex
	entries map[string]*assignmentPathLock
}{entries: make(map[string]*assignmentPathLock)}

func acquireStoreFileLock(storePath string) (func(), error) {
	return acquireAssignmentFileLock(context.Background(), storePath+".lock")
}

func acquireAtomicBeadOperationLock(ctx context.Context, storePath, beadID string) (func(), error) {
	return acquireAtomicOperationLock(ctx, storePath, "bead", beadID)
}

func acquireAtomicTargetOperationLock(ctx context.Context, storePath, target string) (func(), error) {
	return acquireAtomicOperationLock(ctx, storePath, "target", strings.TrimSpace(target))
}

func acquireAtomicOperationLock(ctx context.Context, storePath, namespace, identity string) (func(), error) {
	digest := sha256.Sum256([]byte(namespace + "\x00" + identity))
	lockPath := storePath + ".atomic-" + namespace + "-" + hex.EncodeToString(digest[:16]) + ".lock"
	return acquireAssignmentFileLock(ctx, lockPath)
}

func lockAssignmentPathLocally(ctx context.Context, path string) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	assignmentPathLocks.Lock()
	entry := assignmentPathLocks.entries[path]
	if entry == nil {
		entry = &assignmentPathLock{token: make(chan struct{}, 1)}
		entry.token <- struct{}{}
		assignmentPathLocks.entries[path] = entry
	}
	entry.refs++
	assignmentPathLocks.Unlock()

	select {
	case <-ctx.Done():
		assignmentPathLocks.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(assignmentPathLocks.entries, path)
		}
		assignmentPathLocks.Unlock()
		return nil, ctx.Err()
	case <-entry.token:
	}
	return func() {
		entry.token <- struct{}{}
		assignmentPathLocks.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(assignmentPathLocks.entries, path)
		}
		assignmentPathLocks.Unlock()
	}, nil
}
