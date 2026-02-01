package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAMAdapter_SetServerURL_TrimsTrailingSlash(t *testing.T) {
	t.Parallel()

	a := NewAMAdapter()
	a.SetServerURL("http://example.test/")
	if a.ServerURL() != "http://example.test" {
		t.Fatalf("ServerURL() = %q, want %q", a.ServerURL(), "http://example.test")
	}
}

func TestAMAdapter_Capabilities_ServerAvailable(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health/liveness" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(ts.Close)

	a := NewAMAdapter()
	a.SetServerURL(ts.URL + "/")

	caps, err := a.Capabilities(context.Background())
	if err != nil {
		t.Fatalf("Capabilities() error: %v", err)
	}
	if !containsCapability(caps, CapMacros) {
		t.Fatalf("expected %q in capabilities", CapMacros)
	}
	if !containsCapability(caps, Capability("server_available")) {
		t.Fatalf("expected %q in capabilities", "server_available")
	}

	out, err := a.HealthCheck(context.Background())
	if err != nil {
		t.Fatalf("HealthCheck() error: %v", err)
	}
	if string(out) != `{"ok":true}` {
		t.Fatalf("HealthCheck() = %q, want %q", string(out), `{"ok":true}`)
	}
}

func TestAMAdapter_Capabilities_ServerUnavailableOnNonOK(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health/liveness" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(ts.Close)

	a := NewAMAdapter()
	a.SetServerURL(ts.URL)

	caps, err := a.Capabilities(context.Background())
	if err != nil {
		t.Fatalf("Capabilities() error: %v", err)
	}
	if containsCapability(caps, Capability("server_available")) {
		t.Fatalf("did not expect %q in capabilities", "server_available")
	}
}

func containsCapability(caps []Capability, want Capability) bool {
	for _, cap := range caps {
		if cap == want {
			return true
		}
	}
	return false
}
