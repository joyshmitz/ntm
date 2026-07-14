package bv

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunBdContextCancelsWhileQueuedForWorkspaceGate(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	normalized, err := normalizeTriageDir(dir)
	if err != nil {
		t.Fatalf("normalize triage dir: %v", err)
	}
	gate := workspaceBDMutex(normalized)
	gate.Lock()
	defer gate.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = RunBdContext(ctx, dir, "ready", "--json")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("RunBdContext error=%v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("workspace gate cancellation took %s", elapsed)
	}
}

func TestGetBeadsSummaryContextCancelsWhileStatsWaitsForWorkspaceGate(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{".git", ".beads"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
	}
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "br"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write fake br: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	normalized, err := normalizeTriageDir(dir)
	if err != nil {
		t.Fatalf("normalize triage dir: %v", err)
	}
	gate := workspaceBDMutex(normalized)
	gate.Lock()
	defer gate.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	result, err := GetBeadsSummaryContext(ctx, dir, 10)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("GetBeadsSummaryContext result=%+v error=%v, want deadline exceeded", result, err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("Beads summary cancellation took %s", elapsed)
	}
}

func TestRunBdContextCancelsDuringTransientRetryBackoff(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	binDir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "attempts")
	script := "#!/bin/sh\nprintf 'x\\n' >> " + marker + "\nprintf 'database is locked\\n' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "br"), []byte(script), 0o700); err != nil {
		t.Fatalf("write fake br: %v", err)
	}
	t.Setenv("PATH", binDir)

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := RunBdContext(ctx, dir, "ready", "--json")
		result <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		data, _ := os.ReadFile(marker)
		if strings.Count(string(data), "x") >= 3 {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("fake br did not reach third transient attempt")
		}
		time.Sleep(5 * time.Millisecond)
	}
	started := time.Now()
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("RunBdContext error=%v, want context canceled", err)
		}
		if elapsed := time.Since(started); elapsed > 120*time.Millisecond {
			t.Fatalf("transient retry cancellation took %s", elapsed)
		}
	case <-time.After(time.Second):
		t.Fatal("RunBdContext ignored cancellation during retry backoff")
	}
}

func TestGetRecentlyCompletedListContext(t *testing.T) {
	t.Run("returns_limited_completed_rows", func(t *testing.T) {
		dir := t.TempDir()
		for _, name := range []string{".git", ".beads"} {
			if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", name, err)
			}
		}
		binDir := t.TempDir()
		script := "#!/bin/sh\nprintf '[{\"id\":\"ntm-done-1\",\"title\":\"first\"},{\"id\":\"ntm-done-2\",\"title\":\"second\"}]\\n'\n"
		if err := os.WriteFile(filepath.Join(binDir, "br"), []byte(script), 0o700); err != nil {
			t.Fatalf("write fake br: %v", err)
		}
		t.Setenv("PATH", binDir)

		items, err := GetRecentlyCompletedListContext(context.Background(), dir, 1)
		if err != nil {
			t.Fatalf("GetRecentlyCompletedListContext error: %v", err)
		}
		if len(items) != 1 || items[0].ID != "ntm-done-1" || items[0].Title != "first" {
			t.Fatalf("completed items=%+v, want first row only", items)
		}
	})

	t.Run("cancels_blocking_br", func(t *testing.T) {
		dir := t.TempDir()
		for _, name := range []string{".git", ".beads"} {
			if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", name, err)
			}
		}
		binDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(binDir, "br"), []byte("#!/bin/sh\nexec /bin/sleep 30\n"), 0o700); err != nil {
			t.Fatalf("write blocking fake br: %v", err)
		}
		t.Setenv("PATH", binDir)

		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
		defer cancel()
		started := time.Now()
		items, err := GetRecentlyCompletedListContext(ctx, dir, 10)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("items=%+v error=%v, want deadline exceeded", items, err)
		}
		if elapsed := time.Since(started); elapsed > time.Second {
			t.Fatalf("blocking completed-list read took %s after cancellation", elapsed)
		}
	})
}

func TestUnmarshalBdList(t *testing.T) {
	t.Parallel()

	type bead struct {
		ID string `json:"id"`
	}

	tests := []struct {
		name    string
		input   string
		wantIDs []string
	}{
		{
			name:    "raw_array",
			input:   `[{"id":"bd-1"},{"id":"bd-2"}]`,
			wantIDs: []string{"bd-1", "bd-2"},
		},
		{
			name:    "issues_envelope",
			input:   `{"issues":[{"id":"bd-3"},{"id":"bd-4"}]}`,
			wantIDs: []string{"bd-3", "bd-4"},
		},
		{
			name:    "single_object",
			input:   `{"id":"bd-5"}`,
			wantIDs: []string{"bd-5"},
		},
		{
			name:    "empty_null",
			input:   `null`,
			wantIDs: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := UnmarshalBdList[bead](tt.input)
			if err != nil {
				t.Fatalf("UnmarshalBdList() error = %v", err)
			}

			if len(got) != len(tt.wantIDs) {
				t.Fatalf("len(got) = %d, want %d", len(got), len(tt.wantIDs))
			}
			for i, wantID := range tt.wantIDs {
				if got[i].ID != wantID {
					t.Fatalf("got[%d].ID = %q, want %q", i, got[i].ID, wantID)
				}
			}
		})
	}
}

func TestUnmarshalBdList_RawMessagesFromEnvelope(t *testing.T) {
	t.Parallel()

	raw, err := UnmarshalBdList[json.RawMessage](`{"issues":[{"id":"bd-7"}]}`)
	if err != nil {
		t.Fatalf("UnmarshalBdList() error = %v", err)
	}
	if len(raw) != 1 {
		t.Fatalf("len(raw) = %d, want 1", len(raw))
	}

	var decoded map[string]string
	if err := json.Unmarshal(raw[0], &decoded); err != nil {
		t.Fatalf("json.Unmarshal(raw[0]) error = %v", err)
	}
	if decoded["id"] != "bd-7" {
		t.Fatalf("decoded id = %q, want %q", decoded["id"], "bd-7")
	}
}

func TestCheckDrift_EarlyValidation(t *testing.T) {
	t.Parallel()

	t.Run("missing_project_dir", func(t *testing.T) {
		t.Parallel()

		res := CheckDrift("/path/that/does/not/exist")
		if res.Status != DriftNoBaseline {
			t.Fatalf("Status = %v, want %v", res.Status, DriftNoBaseline)
		}
		if IsInstalled() {
			if !strings.Contains(res.Message, "project directory does not exist") {
				t.Fatalf("Message = %q, want contains %q", res.Message, "project directory does not exist")
			}
		} else {
			if !strings.Contains(res.Message, "bv not installed") {
				t.Fatalf("Message = %q, want contains %q", res.Message, "bv not installed")
			}
		}
	})

	t.Run("missing_beads_dir", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		res := CheckDrift(dir)
		if res.Status != DriftNoBaseline {
			t.Fatalf("Status = %v, want %v", res.Status, DriftNoBaseline)
		}
		if IsInstalled() {
			if !strings.Contains(res.Message, "no .beads directory") {
				t.Fatalf("Message = %q, want contains %q", res.Message, "no .beads directory")
			}
		} else {
			if !strings.Contains(res.Message, "bv not installed") {
				t.Fatalf("Message = %q, want contains %q", res.Message, "bv not installed")
			}
		}
	})
}

func TestGetBeadsSummary_EarlyValidation(t *testing.T) {
	t.Run("missing_project_dir", func(t *testing.T) {
		res := GetBeadsSummary("/path/that/does/not/exist", 3)
		if res.Available {
			t.Fatalf("Available = true, want false")
		}
		if !strings.Contains(res.Reason, "project directory does not exist") {
			t.Fatalf("Reason = %q, want contains %q", res.Reason, "project directory does not exist")
		}
	})

	t.Run("missing_beads_dir", func(t *testing.T) {
		dir := t.TempDir()
		res := GetBeadsSummary(dir, 3)
		if res.Available {
			t.Fatalf("Available = true, want false")
		}
		if !strings.Contains(res.Reason, "no .beads/ directory") {
			t.Fatalf("Reason = %q, want contains %q", res.Reason, "no .beads/ directory")
		}
	})

	t.Run("missing_br_binary_with_beads_dir", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, ".beads"), 0755); err != nil {
			t.Fatalf("mkdir .beads: %v", err)
		}
		t.Setenv("PATH", "")

		res := GetBeadsSummary(dir, 3)
		if res.Available {
			t.Fatalf("Available = true, want false")
		}
		if res.Reason != "br not installed" {
			t.Fatalf("Reason = %q, want %q", res.Reason, "br not installed")
		}
	})
}

func TestGetHealthSummary_NonFatalBottlenecksError(t *testing.T) {
	t.Parallel()

	summary, err := GetHealthSummary("/path/that/does/not/exist")
	if err != nil {
		t.Fatalf("GetHealthSummary err = %v, want nil", err)
	}
	if summary == nil {
		t.Fatalf("GetHealthSummary summary = nil")
	}
	if summary.BottleneckCount != 0 {
		t.Fatalf("BottleneckCount = %d, want 0", summary.BottleneckCount)
	}
	if summary.DriftStatus != DriftNoBaseline {
		t.Fatalf("DriftStatus = %v, want %v", summary.DriftStatus, DriftNoBaseline)
	}
}

func TestGetDependencyContext_HandlesToolErrors(t *testing.T) {
	t.Parallel()

	ctx, err := GetDependencyContext("/path/that/does/not/exist", 3)
	if err != nil {
		t.Fatalf("GetDependencyContext err = %v, want nil", err)
	}
	if ctx == nil {
		t.Fatalf("GetDependencyContext ctx = nil")
	}
	if ctx.BlockedCount != 0 || ctx.ReadyCount != 0 {
		t.Fatalf("BlockedCount/ReadyCount = %d/%d, want 0/0", ctx.BlockedCount, ctx.ReadyCount)
	}
	if len(ctx.InProgressTasks) != 0 {
		t.Fatalf("len(InProgressTasks) = %d, want 0", len(ctx.InProgressTasks))
	}
	if len(ctx.TopBlockers) != 0 {
		t.Fatalf("len(TopBlockers) = %d, want 0", len(ctx.TopBlockers))
	}
}
