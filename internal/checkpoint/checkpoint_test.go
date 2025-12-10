package checkpoint

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGenerateID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantLen  int
		contains string
	}{
		{
			name:    "simple name",
			input:   "backup",
			wantLen: 22, // YYYYMMDD-HHMMSS-backup
		},
		{
			name:    "empty name",
			input:   "",
			wantLen: 15, // YYYYMMDD-HHMMSS
		},
		{
			name:     "name with spaces",
			input:    "my backup",
			contains: "my_backup",
		},
		{
			name:     "name with slashes",
			input:    "test/backup",
			contains: "test-backup",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := GenerateID(tt.input)

			if tt.wantLen > 0 && len(id) != tt.wantLen {
				t.Errorf("GenerateID(%q) length = %d, want %d", tt.input, len(id), tt.wantLen)
			}

			if tt.contains != "" && !containsSubstring(id, tt.contains) {
				t.Errorf("GenerateID(%q) = %q, want to contain %q", tt.input, id, tt.contains)
			}
		})
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "simple"},
		{"with spaces", "with_spaces"},
		{"with/slash", "with-slash"},
		{"with\\backslash", "with-backslash"},
		{"with:colon", "with-colon"},
		{"a*b?c<d>e|f", "a-b-c-d-e-f"},
		{"  trimmed  ", "trimmed"},
		{"verylongnamethatexceedsfiftycharacterssothatshouldbetruncated", "verylongnamethatexceedsfiftycharacterssothatshou"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeName(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestStorage_SaveAndLoad(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "ntm-checkpoint-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	storage := NewStorageWithDir(tmpDir)

	// Create a test checkpoint
	cp := &Checkpoint{
		ID:          "20251210-120000-test",
		Name:        "test",
		Description: "Test checkpoint",
		SessionName: "myproject",
		WorkingDir:  "/tmp/myproject",
		CreatedAt:   time.Now(),
		Session: SessionState{
			Panes: []PaneState{
				{
					Index:     0,
					ID:        "%0",
					Title:     "myproject__cc_1",
					AgentType: "cc",
					Width:     80,
					Height:    24,
				},
			},
			ActivePaneIndex: 0,
		},
		Git: GitState{
			Branch:  "main",
			Commit:  "abc123",
			IsDirty: false,
		},
		PaneCount: 1,
	}

	// Save checkpoint
	if err := storage.Save(cp); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	// Verify directory was created
	checkpointDir := storage.CheckpointDir(cp.SessionName, cp.ID)
	if _, err := os.Stat(checkpointDir); os.IsNotExist(err) {
		t.Errorf("Checkpoint directory was not created: %s", checkpointDir)
	}

	// Load checkpoint
	loaded, err := storage.Load(cp.SessionName, cp.ID)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Verify fields
	if loaded.ID != cp.ID {
		t.Errorf("ID = %q, want %q", loaded.ID, cp.ID)
	}
	if loaded.Name != cp.Name {
		t.Errorf("Name = %q, want %q", loaded.Name, cp.Name)
	}
	if loaded.SessionName != cp.SessionName {
		t.Errorf("SessionName = %q, want %q", loaded.SessionName, cp.SessionName)
	}
	if loaded.Git.Branch != cp.Git.Branch {
		t.Errorf("Git.Branch = %q, want %q", loaded.Git.Branch, cp.Git.Branch)
	}
	if len(loaded.Session.Panes) != 1 {
		t.Errorf("len(Session.Panes) = %d, want 1", len(loaded.Session.Panes))
	}
}

func TestStorage_List(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ntm-checkpoint-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	storage := NewStorageWithDir(tmpDir)
	sessionName := "testproject"

	// Create multiple checkpoints
	times := []time.Time{
		time.Date(2025, 12, 10, 10, 0, 0, 0, time.UTC),
		time.Date(2025, 12, 10, 11, 0, 0, 0, time.UTC),
		time.Date(2025, 12, 10, 12, 0, 0, 0, time.UTC),
	}

	for i, cpTime := range times {
		cp := &Checkpoint{
			ID:          GenerateID("backup" + string(rune('A'+i))),
			Name:        "backup" + string(rune('A'+i)),
			SessionName: sessionName,
			CreatedAt:   cpTime,
			Session:     SessionState{Panes: []PaneState{}},
		}
		if err := storage.Save(cp); err != nil {
			t.Fatalf("Save() failed: %v", err)
		}
	}

	// List checkpoints
	list, err := storage.List(sessionName)
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}

	if len(list) != 3 {
		t.Errorf("len(list) = %d, want 3", len(list))
	}

	// Verify sorted by newest first
	for i := 1; i < len(list); i++ {
		if list[i].CreatedAt.After(list[i-1].CreatedAt) {
			t.Errorf("List not sorted by newest first: %v after %v", list[i].CreatedAt, list[i-1].CreatedAt)
		}
	}
}

func TestStorage_SaveScrollback(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ntm-checkpoint-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	storage := NewStorageWithDir(tmpDir)
	sessionName := "myproject"
	checkpointID := "20251210-120000-test"

	// Create checkpoint directory first
	cp := &Checkpoint{
		ID:          checkpointID,
		SessionName: sessionName,
		CreatedAt:   time.Now(),
		Session:     SessionState{Panes: []PaneState{}},
	}
	if err := storage.Save(cp); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	// Save scrollback
	content := "Line 1\nLine 2\nLine 3\n"
	relativePath, err := storage.SaveScrollback(sessionName, checkpointID, 0, content)
	if err != nil {
		t.Fatalf("SaveScrollback() failed: %v", err)
	}

	if relativePath != "panes/pane_0.txt" {
		t.Errorf("relativePath = %q, want %q", relativePath, "panes/pane_0.txt")
	}

	// Load scrollback
	loaded, err := storage.LoadScrollback(sessionName, checkpointID, 0)
	if err != nil {
		t.Fatalf("LoadScrollback() failed: %v", err)
	}

	if loaded != content {
		t.Errorf("loaded content = %q, want %q", loaded, content)
	}
}

func TestStorage_GetLatest(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ntm-checkpoint-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	storage := NewStorageWithDir(tmpDir)
	sessionName := "testproject"

	// No checkpoints yet
	_, err = storage.GetLatest(sessionName)
	if err == nil {
		t.Error("GetLatest() should fail with no checkpoints")
	}

	// Create checkpoints
	cp1 := &Checkpoint{
		ID:          "20251210-100000-first",
		Name:        "first",
		SessionName: sessionName,
		CreatedAt:   time.Date(2025, 12, 10, 10, 0, 0, 0, time.UTC),
		Session:     SessionState{Panes: []PaneState{}},
	}
	cp2 := &Checkpoint{
		ID:          "20251210-120000-second",
		Name:        "second",
		SessionName: sessionName,
		CreatedAt:   time.Date(2025, 12, 10, 12, 0, 0, 0, time.UTC),
		Session:     SessionState{Panes: []PaneState{}},
	}

	storage.Save(cp1)
	storage.Save(cp2)

	latest, err := storage.GetLatest(sessionName)
	if err != nil {
		t.Fatalf("GetLatest() failed: %v", err)
	}

	if latest.Name != "second" {
		t.Errorf("GetLatest().Name = %q, want %q", latest.Name, "second")
	}
}

func TestCheckpoint_Age(t *testing.T) {
	cp := &Checkpoint{
		CreatedAt: time.Now().Add(-1 * time.Hour),
	}

	age := cp.Age()
	if age < 59*time.Minute || age > 61*time.Minute {
		t.Errorf("Age() = %v, want ~1 hour", age)
	}
}

func TestCheckpoint_HasGitPatch(t *testing.T) {
	cp := &Checkpoint{}
	if cp.HasGitPatch() {
		t.Error("HasGitPatch() should be false with no patch file")
	}

	cp.Git.PatchFile = "git.patch"
	if !cp.HasGitPatch() {
		t.Error("HasGitPatch() should be true with patch file")
	}
}

func TestParseGitStatus(t *testing.T) {
	tests := []struct {
		name      string
		status    string
		staged    int
		unstaged  int
		untracked int
	}{
		{
			name:      "clean",
			status:    "",
			staged:    0,
			unstaged:  0,
			untracked: 0,
		},
		{
			name:      "staged file",
			status:    "M  file.go",
			staged:    1,
			unstaged:  0,
			untracked: 0,
		},
		{
			name:      "unstaged file",
			status:    " M file.go",
			staged:    0,
			unstaged:  1,
			untracked: 0,
		},
		{
			name:      "untracked file",
			status:    "?? newfile.go",
			staged:    0,
			unstaged:  0,
			untracked: 1,
		},
		{
			name:      "mixed status",
			status:    "M  staged.go\n M unstaged.go\n?? untracked.go\nMM both.go",
			staged:    2, // M staged.go and MM both.go
			unstaged:  2, // M unstaged.go and MM both.go
			untracked: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			staged, unstaged, untracked := parseGitStatus(tt.status)
			if staged != tt.staged {
				t.Errorf("staged = %d, want %d", staged, tt.staged)
			}
			if unstaged != tt.unstaged {
				t.Errorf("unstaged = %d, want %d", unstaged, tt.unstaged)
			}
			if untracked != tt.untracked {
				t.Errorf("untracked = %d, want %d", untracked, tt.untracked)
			}
		})
	}
}

func TestCountLines(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"one", 1},
		{"one\ntwo", 2},
		{"one\ntwo\nthree", 3},
		{"one\ntwo\nthree\n", 4},
	}

	for _, tt := range tests {
		got := countLines(tt.input)
		if got != tt.want {
			t.Errorf("countLines(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestMatchWildcard(t *testing.T) {
	tests := []struct {
		s       string
		pattern string
		want    bool
	}{
		{"backup", "backup", true},
		{"backup", "BACKUP", true},
		{"backup", "back*", true},
		{"backup", "*up", true},
		{"backup", "b*p", true},
		{"backup", "*", true},
		{"backup", "nope", false},
		{"backup", "b*x", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			got := matchWildcard(tt.s, tt.pattern)
			if got != tt.want {
				t.Errorf("matchWildcard(%q, %q) = %v, want %v", tt.s, tt.pattern, got, tt.want)
			}
		})
	}
}

// Helper function
func containsSubstring(s, substr string) bool {
	return filepath.Base(s) == substr || len(s) >= len(substr) && s[len(s)-len(substr):] == substr
}
