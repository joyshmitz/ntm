package bv

import (
	"reflect"
	"testing"
)

func TestParseBeadStatusOutput_Object(t *testing.T) {
	t.Parallel()

	status, err := parseBeadStatusOutput(`{"id":"bd-123","status":"in_progress"}`)
	if err != nil {
		t.Fatalf("parseBeadStatusOutput returned error: %v", err)
	}
	if status != "in_progress" {
		t.Fatalf("status = %q, want %q", status, "in_progress")
	}
}

func TestParseBeadStatusOutput_Array(t *testing.T) {
	t.Parallel()

	status, err := parseBeadStatusOutput(`[{"id":"bd-123","status":"closed"}]`)
	if err != nil {
		t.Fatalf("parseBeadStatusOutput returned error: %v", err)
	}
	if status != "closed" {
		t.Fatalf("status = %q, want %q", status, "closed")
	}
}

func TestParseBeadStatusOutput_MissingStatus(t *testing.T) {
	t.Parallel()

	_, err := parseBeadStatusOutput(`{"id":"bd-123"}`)
	if err == nil {
		t.Fatal("expected error for missing status")
	}
}

func TestParseBeadStatusOutput_InvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := parseBeadStatusOutput(`{`)
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestParseBeadClaimOutput(t *testing.T) {
	t.Parallel()

	claim, err := parseBeadClaimOutput(`[{
		"id":"ntm-123",
		"title":"Atomic assignment",
		"status":"in_progress",
		"priority":1,
		"updated_at":"2026-07-11T12:00:00Z"
	}]`)
	if err != nil {
		t.Fatalf("parseBeadClaimOutput: %v", err)
	}
	if claim.ID != "ntm-123" || claim.Status != "in_progress" {
		t.Fatalf("claim=%+v", claim)
	}
	if claim.Title != "Atomic assignment" {
		t.Fatalf("claim title=%q", claim.Title)
	}
}

func TestParseBeadClaimOutputRejectsNonClaimedState(t *testing.T) {
	t.Parallel()

	if _, err := parseBeadClaimOutput(`[{"id":"ntm-123","status":"open"}]`); err == nil {
		t.Fatal("expected non-in_progress claim output to fail")
	}
	if _, err := parseBeadClaimOutput(`[]`); err == nil {
		t.Fatal("expected empty claim output to fail")
	}
}

func TestBeadClaimArgsUseAtomicPrimitive(t *testing.T) {
	t.Parallel()

	want := []string{"update", "ntm-123", "--claim", "--actor", "BlueLake/ntm-key", "--json"}
	if got := beadClaimArgs("ntm-123", "BlueLake/ntm-key"); !reflect.DeepEqual(got, want) {
		t.Fatalf("beadClaimArgs=%q, want %q", got, want)
	}
}
