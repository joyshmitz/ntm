package history

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestConcurrentAppend(t *testing.T) {
	// Set up a temporary directory for history
	tmpDir := t.TempDir()
	os.Setenv("XDG_DATA_HOME", tmpDir)
	defer os.Unsetenv("XDG_DATA_HOME")

	// Verify path logic respects env var
	expectedPath := filepath.Join(tmpDir, "ntm", historyFileName)
	if StoragePath() != expectedPath {
		t.Fatalf("StoragePath() = %q, want %q", StoragePath(), expectedPath)
	}

	const numGoroutines = 10
	const entriesPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < entriesPerGoroutine; j++ {
				entry := NewEntry(
					fmt.Sprintf("session-%d", id),
					[]string{"0"},
					fmt.Sprintf("test command %d-%d", id, j),
					SourceCLI,
				)
				if err := Append(entry); err != nil {
					t.Errorf("Append failed: %v", err)
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify count
	count, err := Count()
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}

	expectedCount := numGoroutines * entriesPerGoroutine
	if count != expectedCount {
		t.Errorf("Count() = %d, want %d", count, expectedCount)
	}

	// Verify content integrity
	entries, err := ReadAll()
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	if len(entries) != expectedCount {
		t.Errorf("ReadAll returned %d entries, want %d", len(entries), expectedCount)
	}
}
