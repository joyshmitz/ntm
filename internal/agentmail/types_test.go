package agentmail

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// =============================================================================
// FlexTime.UnmarshalJSON tests
// =============================================================================

func TestFlexTimeUnmarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
		wantUTC bool // expect result in UTC
	}{
		{"RFC3339", `"2026-01-15T10:30:00Z"`, false, true},
		{"RFC3339 with offset", `"2026-01-15T10:30:00+05:00"`, false, false},
		{"RFC3339Nano", `"2026-01-15T10:30:00.123456789Z"`, false, true},
		{"bare ISO8601", `"2026-01-15T10:30:00"`, false, true},
		{"bare with millis", `"2026-01-15T10:30:00.123"`, false, true},
		{"bare with micros", `"2026-01-15T10:30:00.123456"`, false, true},
		{"bare with nanos", `"2026-01-15T10:30:00.123456789"`, false, true},
		{"empty string", `""`, false, true},
		{"invalid format", `"not-a-date"`, true, false},
		{"invalid JSON", `42`, true, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var ft FlexTime
			err := json.Unmarshal([]byte(tc.input), &ft)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error for %s", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.input == `""` {
				if !ft.Time.IsZero() {
					t.Error("empty string should produce zero time")
				}
				return
			}
			if ft.Time.IsZero() {
				t.Error("parsed time should not be zero")
			}
		})
	}
}

func TestFlexTimeMarshalJSON(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	ft := FlexTime{Time: ts}
	data, err := json.Marshal(ft)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	var s string
	json.Unmarshal(data, &s)
	if s == "" {
		t.Error("marshaled time should not be empty")
	}
}

func TestFlexTimeRoundTrip(t *testing.T) {
	t.Parallel()

	original := FlexTime{Time: time.Date(2026, 2, 4, 12, 0, 0, 123456789, time.UTC)}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var parsed FlexTime
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if !original.Time.Equal(parsed.Time) {
		t.Errorf("round-trip mismatch: %v != %v", original.Time, parsed.Time)
	}
}

// =============================================================================
// JSONRPCError tests
// =============================================================================

func TestJSONRPCError_Error(t *testing.T) {
	t.Parallel()

	t.Run("without data", func(t *testing.T) {
		t.Parallel()
		e := &JSONRPCError{Code: -32600, Message: "invalid request"}
		got := e.Error()
		if got != "JSON-RPC error -32600: invalid request" {
			t.Errorf("Error() = %q", got)
		}
	})

	t.Run("with data", func(t *testing.T) {
		t.Parallel()
		e := &JSONRPCError{Code: -32602, Message: "invalid params", Data: "missing field"}
		got := e.Error()
		if got != "JSON-RPC error -32602: invalid params (data: missing field)" {
			t.Errorf("Error() = %q", got)
		}
	})
}

// =============================================================================
// APIError tests
// =============================================================================

func TestAPIError_Error(t *testing.T) {
	t.Parallel()

	t.Run("with status code", func(t *testing.T) {
		t.Parallel()
		e := &APIError{Operation: "send_message", StatusCode: 500, Err: errors.New("internal error")}
		got := e.Error()
		want := "agentmail: send_message failed (HTTP 500): internal error"
		if got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("without status code", func(t *testing.T) {
		t.Parallel()
		e := &APIError{Operation: "fetch_inbox", Err: errors.New("connection refused")}
		got := e.Error()
		want := "agentmail: fetch_inbox failed: connection refused"
		if got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("unwrap", func(t *testing.T) {
		t.Parallel()
		inner := errors.New("root cause")
		e := &APIError{Operation: "test", Err: inner}
		if e.Unwrap() != inner {
			t.Error("Unwrap() should return inner error")
		}
	})
}

func TestNewAPIError(t *testing.T) {
	t.Parallel()

	inner := errors.New("cause")
	e := NewAPIError("register_agent", 400, inner)
	if e.Operation != "register_agent" {
		t.Errorf("Operation = %q, want %q", e.Operation, "register_agent")
	}
	if e.StatusCode != 400 {
		t.Errorf("StatusCode = %d, want 400", e.StatusCode)
	}
	if e.Err != inner {
		t.Error("Err should be the provided error")
	}
}

// =============================================================================
// mapJSONRPCError tests
// =============================================================================

func TestMapJSONRPCError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     *JSONRPCError
		wantNil   bool
		checkWith error // use errors.Is
	}{
		{"nil input", nil, true, nil},
		{"agent not registered", &JSONRPCError{Code: -32000, Message: "Agent not registered in project"}, false, ErrAgentNotRegistered},
		{"message not found", &JSONRPCError{Code: -32000, Message: "Message not found"}, false, ErrMessageNotFound},
		{"reservation conflict", &JSONRPCError{Code: -32000, Message: "Reservation conflict detected"}, false, ErrReservationConflict},
		{"invalid request code", &JSONRPCError{Code: -32600, Message: "bad request"}, false, ErrInvalidRequest},
		{"method not found code", &JSONRPCError{Code: -32601, Message: "not found"}, false, ErrInvalidRequest},
		{"invalid params code", &JSONRPCError{Code: -32602, Message: "params"}, false, ErrInvalidRequest},
		{"unknown code", &JSONRPCError{Code: -32099, Message: "custom error"}, false, nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := mapJSONRPCError(tc.input)
			if tc.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil error")
			}
			if tc.checkWith != nil && !errors.Is(got, tc.checkWith) {
				t.Errorf("errors.Is(%v, %v) = false", got, tc.checkWith)
			}
		})
	}
}

// =============================================================================
// Error sentinel helpers
// =============================================================================

func TestErrorSentinelHelpers(t *testing.T) {
	t.Parallel()

	t.Run("IsServerUnavailable", func(t *testing.T) {
		t.Parallel()
		if !IsServerUnavailable(ErrServerUnavailable) {
			t.Error("should detect ErrServerUnavailable")
		}
		if IsServerUnavailable(errors.New("other")) {
			t.Error("should not match other errors")
		}
	})

	t.Run("IsUnauthorized", func(t *testing.T) {
		t.Parallel()
		if !IsUnauthorized(ErrUnauthorized) {
			t.Error("should detect ErrUnauthorized")
		}
	})

	t.Run("IsNotFound", func(t *testing.T) {
		t.Parallel()
		if !IsNotFound(ErrNotFound) {
			t.Error("should detect ErrNotFound")
		}
	})

	t.Run("IsTimeout", func(t *testing.T) {
		t.Parallel()
		if !IsTimeout(ErrTimeout) {
			t.Error("should detect ErrTimeout")
		}
	})

	t.Run("IsReservationConflict", func(t *testing.T) {
		t.Parallel()
		if !IsReservationConflict(ErrReservationConflict) {
			t.Error("should detect ErrReservationConflict")
		}
	})

	t.Run("wrapped error", func(t *testing.T) {
		t.Parallel()
		wrapped := &APIError{Operation: "test", Err: ErrNotFound}
		if !IsNotFound(wrapped) {
			t.Error("should detect wrapped ErrNotFound via errors.Is")
		}
	})
}
