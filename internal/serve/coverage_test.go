package serve

import (
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/scanner"
)

// =============================================================================
// ScannerStore: GetScans - reverse chronological ordering & pagination
// =============================================================================

func TestScannerStore_GetScans_ReverseOrder(t *testing.T) {
	store := NewScannerStore()

	for i := 0; i < 5; i++ {
		store.AddScan(&ScanRecord{
			ID:        generateScanID(),
			State:     ScanStateCompleted,
			StartedAt: time.Now(),
		})
	}

	scans := store.GetScans(10, 0)
	if len(scans) != 5 {
		t.Fatalf("expected 5 scans, got %d", len(scans))
	}
}

func TestScannerStore_GetScans_Pagination(t *testing.T) {
	store := NewScannerStore()

	for i := 0; i < 10; i++ {
		store.AddScan(&ScanRecord{
			ID:        generateScanID(),
			State:     ScanStateCompleted,
			StartedAt: time.Now(),
		})
	}

	// Limit to 3 results
	scans := store.GetScans(3, 0)
	if len(scans) != 3 {
		t.Errorf("expected 3 scans, got %d", len(scans))
	}

	// Offset beyond available
	scans = store.GetScans(10, 100)
	if scans != nil {
		t.Errorf("expected nil for offset beyond range, got %d items", len(scans))
	}

	// Offset + limit = partial result
	scans = store.GetScans(5, 7)
	if len(scans) != 3 {
		t.Errorf("expected 3 scans (offset 7 from 10), got %d", len(scans))
	}
}

// =============================================================================
// ScannerStore: GetRunningScan
// =============================================================================

func TestScannerStore_GetRunningScan(t *testing.T) {
	store := NewScannerStore()

	// No running scan
	if scan := store.GetRunningScan(); scan != nil {
		t.Error("expected nil when no running scan")
	}

	// Add a completed scan
	store.AddScan(&ScanRecord{ID: "scan-1", State: ScanStateCompleted})

	// Still no running scan
	if scan := store.GetRunningScan(); scan != nil {
		t.Error("expected nil when only completed scans exist")
	}

	// Add a running scan
	store.AddScan(&ScanRecord{ID: "scan-2", State: ScanStateRunning})

	scan := store.GetRunningScan()
	if scan == nil {
		t.Fatal("expected running scan")
	}
	if scan.ID != "scan-2" {
		t.Errorf("expected scan-2, got %s", scan.ID)
	}
}

// =============================================================================
// ScannerStore: AddScan / AddFinding / GetFinding / UpdateFinding
// =============================================================================

func TestScannerStore_FindingCRUD(t *testing.T) {
	store := NewScannerStore()

	finding := &FindingRecord{
		ID:     "finding-1",
		ScanID: "scan-1",
		Finding: scanner.Finding{
			Severity: scanner.SeverityCritical,
		},
		CreatedAt: time.Now(),
	}

	store.AddFinding(finding)

	// Get existing finding
	got, ok := store.GetFinding("finding-1")
	if !ok {
		t.Fatal("expected to find finding-1")
	}
	if got.ScanID != "scan-1" {
		t.Errorf("ScanID = %s, want scan-1", got.ScanID)
	}

	// Get non-existent finding
	_, ok = store.GetFinding("nonexistent")
	if ok {
		t.Error("expected not found for nonexistent finding")
	}

	// Update finding
	store.UpdateFinding(finding.ID, func(fr *FindingRecord) {
		fr.Dismissed = true
	})

	got, _ = store.GetFinding("finding-1")
	if !got.Dismissed {
		t.Error("expected finding to be dismissed after update")
	}
}

// =============================================================================
// ScannerStore: GetFindings - filtering, sorting, pagination
// =============================================================================

func TestScannerStore_GetFindings_Filtering(t *testing.T) {
	store := NewScannerStore()

	now := time.Now()
	store.AddFinding(&FindingRecord{
		ID: "f1", ScanID: "scan-1",
		Finding:   scanner.Finding{Severity: scanner.SeverityCritical},
		CreatedAt: now.Add(-3 * time.Second),
	})
	store.AddFinding(&FindingRecord{
		ID: "f2", ScanID: "scan-1",
		Finding:   scanner.Finding{Severity: scanner.SeverityInfo},
		Dismissed: true,
		CreatedAt: now.Add(-2 * time.Second),
	})
	store.AddFinding(&FindingRecord{
		ID: "f3", ScanID: "scan-2",
		Finding:   scanner.Finding{Severity: scanner.SeverityCritical},
		CreatedAt: now.Add(-1 * time.Second),
	})

	// Filter by scan ID
	findings := store.GetFindings("scan-1", true, "", 10, 0)
	if len(findings) != 2 {
		t.Errorf("expected 2 findings for scan-1, got %d", len(findings))
	}

	// Exclude dismissed
	findings = store.GetFindings("scan-1", false, "", 10, 0)
	if len(findings) != 1 {
		t.Errorf("expected 1 non-dismissed finding, got %d", len(findings))
	}

	// Filter by severity
	findings = store.GetFindings("", true, string(scanner.SeverityCritical), 10, 0)
	if len(findings) != 2 {
		t.Errorf("expected 2 critical severity findings, got %d", len(findings))
	}

	// Pagination: offset beyond range
	findings = store.GetFindings("", true, "", 10, 100)
	if findings != nil {
		t.Errorf("expected nil for offset beyond range, got %d", len(findings))
	}

	// Pagination: limit 1
	findings = store.GetFindings("", true, "", 1, 0)
	if len(findings) != 1 {
		t.Errorf("expected 1 finding with limit 1, got %d", len(findings))
	}
}

// =============================================================================
// JobStore: List
// =============================================================================

func TestJobStore_List(t *testing.T) {
	store := NewJobStore()

	// Empty list
	jobs := store.List()
	if len(jobs) != 0 {
		t.Errorf("expected empty list, got %d", len(jobs))
	}

	// Add some jobs
	store.Create("scan")
	store.Create("export")
	store.Create("import")

	jobs = store.List()
	if len(jobs) != 3 {
		t.Errorf("expected 3 jobs, got %d", len(jobs))
	}
}

// =============================================================================
// WSClient: canSubscribe
// =============================================================================

func TestWSClient_CanSubscribe(t *testing.T) {

	client := &WSClient{
		id:     "auth-client",
		topics: make(map[string]struct{}),
		send:   make(chan []byte, 16),
	}

	if !client.canSubscribe("sessions:test") {
		t.Error("expected canSubscribe to return true")
	}
	if !client.canSubscribe("anything") {
		t.Error("expected canSubscribe to return true for any topic")
	}
}
