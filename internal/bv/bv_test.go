package bv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// getProjectRoot finds the project root by looking for .beads directory
func getProjectRoot() string {
	dir, _ := os.Getwd()
	for dir != "/" {
		if _, err := os.Stat(filepath.Join(dir, ".beads")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	return ""
}

func TestIsInstalled(t *testing.T) {
	// This test verifies the function works - actual result depends on environment
	result := IsInstalled()
	t.Logf("bv installed: %v", result)
}

func TestDriftStatusString(t *testing.T) {
	tests := []struct {
		status DriftStatus
		want   string
	}{
		{DriftOK, "OK"},
		{DriftCritical, "critical"},
		{DriftWarning, "warning"},
		{DriftNoBaseline, "no baseline"},
		{DriftStatus(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.status.String()
			if got != tt.want {
				t.Errorf("DriftStatus(%d).String() = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestCheckDrift(t *testing.T) {
	if !IsInstalled() {
		t.Skip("bv not installed")
	}

	root := getProjectRoot()
	if root == "" {
		t.Skip("Project root not found (no .beads)")
	}

	result := CheckDrift(root)

	// Handle case where flag is not supported by installed version
	if strings.Contains(result.Message, "flag provided but not defined") {
		t.Skipf("bv does not support -check-drift: %s", result.Message)
	}

	t.Logf("Drift status: %s, message: %s", result.Status, result.Message)

	// Status should be one of the defined values
	switch result.Status {
	case DriftOK, DriftCritical, DriftWarning, DriftNoBaseline:
		// Valid status
	default:
		t.Errorf("Unexpected drift status: %d", result.Status)
	}
}

func TestGetInsights(t *testing.T) {
	if !IsInstalled() {
		t.Skip("bv not installed")
	}

	root := getProjectRoot()
	if root == "" {
		t.Skip("Project root not found")
	}

	insights, err := GetInsights(root)
	if err != nil {
		t.Fatalf("GetInsights() error: %v", err)
	}

	t.Logf("Got %d bottlenecks", len(insights.Bottlenecks))

	// Verify structure
	for _, b := range insights.Bottlenecks {
		if b.ID == "" {
			t.Error("Bottleneck has empty ID")
		}
	}
}

func TestGetPriority(t *testing.T) {
	if !IsInstalled() {
		t.Skip("bv not installed")
	}

	root := getProjectRoot()
	if root == "" {
		t.Skip("Project root not found")
	}

	priority, err := GetPriority(root)
	if err != nil {
		t.Fatalf("GetPriority() error: %v", err)
	}

	t.Logf("Got %d recommendations", len(priority.Recommendations))

	// Verify structure
	for _, r := range priority.Recommendations {
		if r.IssueID == "" {
			t.Error("Recommendation has empty IssueID")
		}
		if r.Confidence < 0 || r.Confidence > 1 {
			t.Errorf("Invalid confidence: %f", r.Confidence)
		}
	}
}

func TestGetPlan(t *testing.T) {
	if !IsInstalled() {
		t.Skip("bv not installed")
	}

	root := getProjectRoot()
	if root == "" {
		t.Skip("Project root not found")
	}

	plan, err := GetPlan(root)
	if err != nil {
		t.Fatalf("GetPlan() error: %v", err)
	}

	t.Logf("Got %d tracks", len(plan.Plan.Tracks))

	// Verify structure
	for _, track := range plan.Plan.Tracks {
		if track.TrackID == "" {
			t.Error("Track has empty TrackID")
		}
		if len(track.Items) == 0 {
			t.Errorf("Track %s has no items", track.TrackID)
		}
	}
}

func TestGetRecipes(t *testing.T) {
	if !IsInstalled() {
		t.Skip("bv not installed")
	}

	root := getProjectRoot()
	if root == "" {
		t.Skip("Project root not found")
	}

	recipes, err := GetRecipes(root)
	if err != nil {
		t.Fatalf("GetRecipes() error: %v", err)
	}

	t.Logf("Got %d recipes", len(recipes.Recipes))

	// Should have at least the builtin recipes
	if len(recipes.Recipes) == 0 {
		t.Error("Expected at least one recipe")
	}

	// Verify structure
	for _, r := range recipes.Recipes {
		if r.Name == "" {
			t.Error("Recipe has empty name")
		}
		if r.Source == "" {
			t.Error("Recipe has empty source")
		}
	}
}

func TestGetTopBottlenecks(t *testing.T) {
	if !IsInstalled() {
		t.Skip("bv not installed")
	}

	root := getProjectRoot()
	if root == "" {
		t.Skip("Project root not found")
	}

	bottlenecks, err := GetTopBottlenecks(root, 3)
	if err != nil {
		t.Fatalf("GetTopBottlenecks() error: %v", err)
	}

	if len(bottlenecks) > 3 {
		t.Errorf("Expected at most 3 bottlenecks, got %d", len(bottlenecks))
	}

	t.Logf("Top bottlenecks: %v", bottlenecks)
}

func TestGetNextActions(t *testing.T) {
	if !IsInstalled() {
		t.Skip("bv not installed")
	}

	root := getProjectRoot()
	if root == "" {
		t.Skip("Project root not found")
	}

	actions, err := GetNextActions(root, 5)
	if err != nil {
		t.Fatalf("GetNextActions() error: %v", err)
	}

	if len(actions) > 5 {
		t.Errorf("Expected at most 5 actions, got %d", len(actions))
	}

	t.Logf("Next actions: %d items", len(actions))
}

func TestGetParallelTracks(t *testing.T) {
	if !IsInstalled() {
		t.Skip("bv not installed")
	}

	root := getProjectRoot()
	if root == "" {
		t.Skip("Project root not found")
	}

	tracks, err := GetParallelTracks(root)
	if err != nil {
		t.Fatalf("GetParallelTracks() error: %v", err)
	}

	t.Logf("Parallel tracks: %d", len(tracks))
}

func TestIsBottleneck(t *testing.T) {
	if !IsInstalled() {
		t.Skip("bv not installed")
	}

	root := getProjectRoot()
	if root == "" {
		t.Skip("Project root not found")
	}

	// Test with a likely non-existent ID
	isBottle, score, err := IsBottleneck(root, "nonexistent-issue-xyz")
	if err != nil {
		t.Fatalf("IsBottleneck() error: %v", err)
	}

	if isBottle {
		t.Error("Expected nonexistent issue to not be a bottleneck")
	}
	if score != 0 {
		t.Errorf("Expected score 0 for non-bottleneck, got %f", score)
	}
}

func TestGetHealthSummary(t *testing.T) {
	if !IsInstalled() {
		t.Skip("bv not installed")
	}

	root := getProjectRoot()
	if root == "" {
		t.Skip("Project root not found")
	}

	summary, err := GetHealthSummary(root)
	if err != nil {
		t.Fatalf("GetHealthSummary() error: %v", err)
	}

	t.Logf("Health: drift=%s, bottlenecks=%d, top=%s",
		summary.DriftStatus, summary.BottleneckCount, summary.TopBottleneck)
}

func TestNotInstalled(t *testing.T) {
	// Test error behavior when bv is not in PATH
	// We can't easily test this without modifying PATH, so just verify the error exists
	if ErrNotInstalled == nil {
		t.Error("ErrNotInstalled should not be nil")
	}
	if ErrNoBaseline == nil {
		t.Error("ErrNoBaseline should not be nil")
	}
}

func TestIsKeystone(t *testing.T) {
	if !IsInstalled() {
		t.Skip("bv not installed")
	}

	root := getProjectRoot()
	if root == "" {
		t.Skip("Project root not found")
	}

	// Test with a likely non-existent ID
	isKey, score, err := IsKeystone(root, "nonexistent-issue-xyz")
	if err != nil {
		t.Fatalf("IsKeystone() error: %v", err)
	}

	if isKey {
		t.Error("Expected nonexistent issue to not be a keystone")
	}
	if score != 0 {
		t.Errorf("Expected score 0 for non-keystone, got %f", score)
	}
}

func TestIsHub(t *testing.T) {
	if !IsInstalled() {
		t.Skip("bv not installed")
	}

	root := getProjectRoot()
	if root == "" {
		t.Skip("Project root not found")
	}

	// Test with a likely non-existent ID
	isHub, score, err := IsHub(root, "nonexistent-issue-xyz")
	if err != nil {
		t.Fatalf("IsHub() error: %v", err)
	}

	if isHub {
		t.Error("Expected nonexistent issue to not be a hub")
	}
	if score != 0 {
		t.Errorf("Expected score 0 for non-hub, got %f", score)
	}
}

func TestIsAuthority(t *testing.T) {
	if !IsInstalled() {
		t.Skip("bv not installed")
	}

	root := getProjectRoot()
	if root == "" {
		t.Skip("Project root not found")
	}

	// Test with a likely non-existent ID
	isAuth, score, err := IsAuthority(root, "nonexistent-issue-xyz")
	if err != nil {
		t.Fatalf("IsAuthority() error: %v", err)
	}

	if isAuth {
		t.Error("Expected nonexistent issue to not be an authority")
	}
	if score != 0 {
		t.Errorf("Expected score 0 for non-authority, got %f", score)
	}
}

func TestGetGraphPosition(t *testing.T) {
	if !IsInstalled() {
		t.Skip("bv not installed")
	}

	root := getProjectRoot()
	if root == "" {
		t.Skip("Project root not found")
	}

	// Test with a known issue ID (one that exists in the project)
	// First, get a bottleneck to use as a test case
	bottlenecks, err := GetTopBottlenecks(root, 1)
	if err != nil {
		t.Skipf("Could not get bottlenecks: %v", err)
	}

	if len(bottlenecks) == 0 {
		t.Skip("No bottlenecks found to test with")
	}

	testID := bottlenecks[0].ID
	pos, err := GetGraphPosition(root, testID)
	if err != nil {
		t.Fatalf("GetGraphPosition() error: %v", err)
	}

	if pos.IssueID != testID {
		t.Errorf("IssueID = %s, want %s", pos.IssueID, testID)
	}

	// Should be a bottleneck since we got it from bottleneck list
	if !pos.IsBottleneck {
		t.Errorf("Expected %s to be a bottleneck", testID)
	}

	if pos.Summary == "" {
		t.Error("Expected non-empty summary")
	}

	t.Logf("Graph position for %s: %+v", testID, pos)
}

func TestGetGraphPositionNonExistent(t *testing.T) {
	if !IsInstalled() {
		t.Skip("bv not installed")
	}

	root := getProjectRoot()
	if root == "" {
		t.Skip("Project root not found")
	}

	pos, err := GetGraphPosition(root, "nonexistent-issue-xyz")
	if err != nil {
		t.Fatalf("GetGraphPosition() error: %v", err)
	}

	if pos.IsBottleneck || pos.IsKeystone || pos.IsHub || pos.IsAuthority {
		t.Error("Expected nonexistent issue to have no graph roles")
	}

	if pos.Summary != "regular node" {
		t.Errorf("Summary = %q, want 'regular node'", pos.Summary)
	}
}

func TestGetGraphPositionsBatch(t *testing.T) {
	if !IsInstalled() {
		t.Skip("bv not installed")
	}

	root := getProjectRoot()
	if root == "" {
		t.Skip("Project root not found")
	}

	// Get some real IDs to test with
	bottlenecks, err := GetTopBottlenecks(root, 2)
	if err != nil {
		t.Skipf("Could not get bottlenecks: %v", err)
	}

	var ids []string
	for _, b := range bottlenecks {
		ids = append(ids, b.ID)
	}
	// Add a fake ID too
	ids = append(ids, "fake-id-xyz")

	positions, err := GetGraphPositionsBatch(root, ids)
	if err != nil {
		t.Fatalf("GetGraphPositionsBatch() error: %v", err)
	}

	if len(positions) != len(ids) {
		t.Errorf("Expected %d positions, got %d", len(ids), len(positions))
	}

	// Verify bottlenecks are marked as such
	for _, b := range bottlenecks {
		pos, ok := positions[b.ID]
		if !ok {
			t.Errorf("Missing position for %s", b.ID)
			continue
		}
		if !pos.IsBottleneck {
			t.Errorf("Expected %s to be marked as bottleneck", b.ID)
		}
	}

	// Verify fake ID is not a bottleneck
	fakePos := positions["fake-id-xyz"]
	if fakePos.IsBottleneck {
		t.Error("Fake ID should not be a bottleneck")
	}
}

func TestGeneratePositionSummary(t *testing.T) {
	tests := []struct {
		name     string
		pos      *GraphPosition
		contains []string
	}{
		{
			name:     "regular node",
			pos:      &GraphPosition{},
			contains: []string{"regular node"},
		},
		{
			name:     "bottleneck only",
			pos:      &GraphPosition{IsBottleneck: true},
			contains: []string{"bottleneck"},
		},
		{
			name:     "keystone only",
			pos:      &GraphPosition{IsKeystone: true},
			contains: []string{"keystone"},
		},
		{
			name:     "hub only",
			pos:      &GraphPosition{IsHub: true},
			contains: []string{"hub"},
		},
		{
			name:     "authority only",
			pos:      &GraphPosition{IsAuthority: true},
			contains: []string{"authority"},
		},
		{
			name:     "multiple roles",
			pos:      &GraphPosition{IsBottleneck: true, IsKeystone: true},
			contains: []string{"bottleneck", "keystone"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := generatePositionSummary(tt.pos)
			for _, want := range tt.contains {
				if !containsSubstring(summary, want) {
					t.Errorf("Summary %q should contain %q", summary, want)
				}
			}
		})
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
