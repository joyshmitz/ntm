package robot

import "testing"

func TestCompileBeadMentionPattern(t *testing.T) {

	if _, err := compileBeadMentionPattern(""); err == nil {
		t.Fatal("expected error for empty bead ID")
	}

	re, err := compileBeadMentionPattern("bd-123")
	if err != nil {
		t.Fatalf("compileBeadMentionPattern returned error: %v", err)
	}
	if !re.MatchString("Working on BD-123 now") {
		t.Fatal("expected case-insensitive match")
	}
	if re.MatchString("Working on bd-1234 now") {
		t.Fatal("unexpected partial match")
	}
}

func TestFindBeadMentionMatches(t *testing.T) {

	re, err := compileBeadMentionPattern("bd-77")
	if err != nil {
		t.Fatalf("compileBeadMentionPattern returned error: %v", err)
	}

	lines := []string{
		"",
		"starting bd-77",
		"noise",
		"done with BD-77",
		"bd-770 should not match",
	}

	matches := findBeadMentionMatches(lines, re)
	if len(matches) != 2 {
		t.Fatalf("matches len = %d, want 2", len(matches))
	}
	if matches[0].LineNum != 2 {
		t.Fatalf("first line number = %d, want 2", matches[0].LineNum)
	}
	if matches[1].LineNum != 4 {
		t.Fatalf("second line number = %d, want 4", matches[1].LineNum)
	}
}

func TestGetWatchBeadValidation(t *testing.T) {

	out, err := GetWatchBead(WatchBeadOptions{
		Session: "any",
		BeadID:  "",
	})
	if err != nil {
		t.Fatalf("GetWatchBead returned error: %v", err)
	}
	if out.Success {
		t.Fatalf("expected success=false for missing bead")
	}
	if out.ErrorCode != ErrCodeInvalidFlag {
		t.Fatalf("error_code = %q, want %q", out.ErrorCode, ErrCodeInvalidFlag)
	}
}
