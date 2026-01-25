package serve

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/events"
	"github.com/Dicklesworthstone/ntm/internal/state"
)

func setupTestServer(t *testing.T) (*Server, *state.Store) {
	t.Helper()

	// Create temp database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := state.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open store: %v", err)
	}

	if err := store.Migrate(); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	t.Cleanup(func() {
		store.Close()
		os.Remove(dbPath)
	})

	eventBus := events.NewEventBus(100)

	srv := New(Config{
		Port:       0, // Will use default
		EventBus:   eventBus,
		StateStore: store,
	})

	return srv, store
}

func TestNew(t *testing.T) {
	srv := New(Config{})
	if srv == nil {
		t.Fatal("New returned nil")
	}
	if srv.Port() != 7337 {
		t.Errorf("Default port = %d, want 7337", srv.Port())
	}
}

func TestNewWithCustomPort(t *testing.T) {
	srv := New(Config{Port: 8080})
	if srv.Port() != 8080 {
		t.Errorf("Port = %d, want 8080", srv.Port())
	}
}

func TestHealthEndpoint(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	srv.handleHealth(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp["success"] != true {
		t.Error("Expected success=true")
	}
	if resp["status"] != "healthy" {
		t.Error("Expected status=healthy")
	}
}

func TestHealthEndpointMethodNotAllowed(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	rec := httptest.NewRecorder()

	srv.handleHealth(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestSessionsEndpoint(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rec := httptest.NewRecorder()

	srv.handleSessions(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp["success"] != true {
		t.Error("Expected success=true")
	}
	if _, ok := resp["sessions"]; !ok {
		t.Error("Expected sessions field")
	}
}

func TestSessionsEndpointNoStore(t *testing.T) {
	srv := New(Config{})

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rec := httptest.NewRecorder()

	srv.handleSessions(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestSessionEndpointNotFound(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/nonexistent", nil)
	rec := httptest.NewRecorder()

	srv.handleSession(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestSessionEndpointMissingID(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/", nil)
	rec := httptest.NewRecorder()

	srv.handleSession(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestRobotStatusEndpoint(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/robot/status", nil)
	rec := httptest.NewRecorder()

	srv.handleRobotStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp["success"] != true {
		t.Error("Expected success=true")
	}
}

func TestRobotHealthEndpoint(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/robot/health", nil)
	rec := httptest.NewRecorder()

	srv.handleRobotHealth(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestSessionEventsEndpoint(t *testing.T) {
	srv, _ := setupTestServer(t)

	// Publish an event
	srv.eventBus.Publish(events.BaseEvent{
		Type:      "test_event",
		Timestamp: time.Now().UTC(),
		Session:   "test-session",
	})

	// Give event time to be recorded
	time.Sleep(10 * time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/test-session/events", nil)
	rec := httptest.NewRecorder()

	srv.handleSessionEvents(rec, req, "test-session")

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp["success"] != true {
		t.Error("Expected success=true")
	}
}

func TestSSEClientManagement(t *testing.T) {
	srv, _ := setupTestServer(t)

	ch := make(chan events.BusEvent, 10)
	srv.addSSEClient(ch)

	srv.sseClientsMu.RLock()
	clientCount := len(srv.sseClients)
	srv.sseClientsMu.RUnlock()

	if clientCount != 1 {
		t.Errorf("Client count = %d, want 1", clientCount)
	}

	srv.removeSSEClient(ch)

	srv.sseClientsMu.RLock()
	clientCount = len(srv.sseClients)
	srv.sseClientsMu.RUnlock()

	if clientCount != 0 {
		t.Errorf("Client count = %d, want 0 after removal", clientCount)
	}
}

func TestBroadcastEvent(t *testing.T) {
	srv, _ := setupTestServer(t)

	ch := make(chan events.BusEvent, 10)
	srv.addSSEClient(ch)
	defer srv.removeSSEClient(ch)

	testEvent := events.BaseEvent{
		Type:      "broadcast_test",
		Timestamp: time.Now().UTC(),
		Session:   "test",
	}

	srv.broadcastEvent(testEvent)

	select {
	case e := <-ch:
		if e.EventType() != "broadcast_test" {
			t.Errorf("Event type = %s, want broadcast_test", e.EventType())
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for broadcast")
	}
}

func TestEventStreamSSE(t *testing.T) {
	srv, _ := setupTestServer(t)

	// Create a request with a cancelable context
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	// Start the handler in a goroutine
	done := make(chan struct{})
	go func() {
		srv.handleEventStream(rec, req)
		close(done)
	}()

	// Give time for connection setup
	time.Sleep(50 * time.Millisecond)

	// Cancel to end the request
	cancel()

	// Wait for handler to complete
	select {
	case <-done:
		// OK
	case <-time.After(1 * time.Second):
		t.Error("Handler did not complete after context cancel")
	}

	// Check headers
	contentType := rec.Header().Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("Content-Type = %s, want text/event-stream", contentType)
	}

	// Check for connected event
	body, _ := io.ReadAll(rec.Body)
	if len(body) == 0 {
		t.Error("Expected some output from SSE stream")
	}
}

func TestCORSMiddleware(t *testing.T) {
	srv := New(Config{})
	handler := srv.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Test preflight OPTIONS request
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Error("Expected CORS allowlist header")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("OPTIONS Status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestCORSMiddlewareRejectsOrigin(t *testing.T) {
	srv := New(Config{})
	handler := srv.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "http://evil.example.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestLoggingMiddleware(t *testing.T) {
	handler := loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAuthMiddlewareAPIKey(t *testing.T) {
	srv := New(Config{
		Auth: AuthConfig{
			Mode:   AuthModeAPIKey,
			APIKey: "secret",
		},
	})
	handler := srv.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("missing api key status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("valid api key status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAuthMiddlewareOIDC(t *testing.T) {
	issuer := "https://issuer.example.com"
	audience := "ntm"
	key := mustGenerateKey(t)
	jwksURL := startJWKS(t, key, "kid1")
	token := signJWT(t, key, "kid1", issuer, audience, time.Now().Add(1*time.Hour))

	srv := New(Config{
		Auth: AuthConfig{
			Mode: AuthModeOIDC,
			OIDC: OIDCConfig{
				Issuer:   issuer,
				Audience: audience,
				JWKSURL:  jwksURL,
			},
		},
	})
	handler := srv.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("missing oidc token status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("valid oidc token status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAuthMiddlewareMTLS(t *testing.T) {
	srv := New(Config{
		Auth: AuthConfig{
			Mode: AuthModeMTLS,
		},
	})
	handler := srv.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("missing mtls cert status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{}}}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("valid mtls cert status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestWebSocketRejectedWithoutToken(t *testing.T) {
	srv := New(Config{
		Auth: AuthConfig{
			Mode:   AuthModeAPIKey,
			APIKey: "secret",
		},
	})
	handler := srv.authMiddleware(http.HandlerFunc(srv.handleWS))

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("ws unauth status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func mustGenerateKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

func startJWKS(t *testing.T, key *rsa.PrivateKey, kid string) string {
	t.Helper()
	n := base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes())
	payload := map[string]interface{}{
		"keys": []map[string]string{
			{
				"kty": "RSA",
				"kid": kid,
				"n":   n,
				"e":   e,
			},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func signJWT(t *testing.T, key *rsa.PrivateKey, kid, issuer, audience string, exp time.Time) string {
	t.Helper()
	header := map[string]string{
		"alg": "RS256",
		"kid": kid,
		"typ": "JWT",
	}
	claims := map[string]interface{}{
		"iss": issuer,
		"aud": audience,
		"exp": exp.Unix(),
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	headerEnc := base64.RawURLEncoding.EncodeToString(headerJSON)
	claimsEnc := base64.RawURLEncoding.EncodeToString(claimsJSON)
	signingInput := headerEnc + "." + claimsEnc
	hash := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	sigEnc := base64.RawURLEncoding.EncodeToString(sig)
	return signingInput + "." + sigEnc
}

// =============================================================================
// API v1 Tests
// =============================================================================

func TestHealthV1Endpoint(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp["success"] != true {
		t.Error("Expected success=true")
	}
	if resp["status"] != "healthy" {
		t.Error("Expected status=healthy")
	}
	if _, ok := resp["timestamp"]; !ok {
		t.Error("Expected timestamp field")
	}
}

func TestVersionV1Endpoint(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp["success"] != true {
		t.Error("Expected success=true")
	}
	if resp["api_version"] != "v1" {
		t.Error("Expected api_version=v1")
	}
}

func TestCapabilitiesV1Endpoint(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/capabilities", nil)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp["success"] != true {
		t.Error("Expected success=true")
	}
	if _, ok := resp["auth_modes"]; !ok {
		t.Error("Expected auth_modes field")
	}
	if _, ok := resp["features"]; !ok {
		t.Error("Expected features field")
	}
}

func TestSessionsV1Endpoint(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp["success"] != true {
		t.Error("Expected success=true")
	}
	// Verify sessions is an array (never null)
	sessions, ok := resp["sessions"].([]interface{})
	if !ok {
		t.Error("Expected sessions to be an array")
	}
	if sessions == nil {
		t.Error("Expected sessions to be non-nil array")
	}
}

// =============================================================================
// Jobs API Tests
// =============================================================================

func TestListJobsEndpoint(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/", nil)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp["success"] != true {
		t.Error("Expected success=true")
	}
	// Verify jobs is an array (never null)
	jobs, ok := resp["jobs"].([]interface{})
	if !ok {
		t.Error("Expected jobs to be an array")
	}
	if jobs == nil {
		t.Error("Expected jobs to be non-nil array")
	}
}

func TestCreateJobEndpoint(t *testing.T) {
	srv, _ := setupTestServer(t)

	body := `{"type": "spawn", "session": "test-session"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusAccepted)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp["success"] != true {
		t.Error("Expected success=true")
	}

	job, ok := resp["job"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected job object in response")
	}

	if job["type"] != "spawn" {
		t.Errorf("Job type = %v, want spawn", job["type"])
	}
	// Status can be "pending" or "running" due to the goroutine race
	status, _ := job["status"].(string)
	if status != "pending" && status != "running" {
		t.Errorf("Job status = %v, want pending or running", job["status"])
	}
}

func TestCreateJobInvalidType(t *testing.T) {
	srv, _ := setupTestServer(t)

	body := `{"type": "invalid"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGetJobEndpoint(t *testing.T) {
	srv, _ := setupTestServer(t)

	// Create a job first
	job := srv.jobStore.Create("spawn")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+job.ID, nil)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp["success"] != true {
		t.Error("Expected success=true")
	}
}

func TestGetJobNotFound(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/nonexistent", nil)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestCancelJobEndpoint(t *testing.T) {
	srv, _ := setupTestServer(t)

	// Create a job first
	job := srv.jobStore.Create("spawn")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/jobs/"+job.ID, nil)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Verify job is cancelled
	updatedJob := srv.jobStore.Get(job.ID)
	if updatedJob.Status != JobStatusCancelled {
		t.Errorf("Job status = %v, want cancelled", updatedJob.Status)
	}
}

// =============================================================================
// Idempotency Tests
// =============================================================================

func TestIdempotencyStore(t *testing.T) {
	store := NewIdempotencyStore(time.Hour)

	// Test set and get
	store.Set("key1", []byte(`{"test": true}`), 200)

	resp, status, ok := store.Get("key1")
	if !ok {
		t.Fatal("Expected to find key1")
	}
	if status != 200 {
		t.Errorf("Status = %d, want 200", status)
	}
	if string(resp) != `{"test": true}` {
		t.Errorf("Response = %s, want {\"test\": true}", string(resp))
	}

	// Test non-existent key
	_, _, ok = store.Get("nonexistent")
	if ok {
		t.Error("Expected not to find nonexistent key")
	}
}

func TestIdempotencyMiddleware(t *testing.T) {
	srv, _ := setupTestServer(t)

	// First request
	body := `{"type": "spawn"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "test-key-123")
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Errorf("First request status = %d, want %d", rec.Code, http.StatusAccepted)
	}

	// Capture the job ID from first response
	var firstResp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&firstResp); err != nil {
		t.Fatalf("Failed to decode first response: %v", err)
	}
	firstJob := firstResp["job"].(map[string]interface{})
	firstJobID := firstJob["id"].(string)

	// Second request with same idempotency key
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/", strings.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Idempotency-Key", "test-key-123")
	rec2 := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec2, req2)

	// Should get same response (replay)
	if rec2.Header().Get("X-Idempotent-Replay") != "true" {
		t.Error("Expected X-Idempotent-Replay header")
	}

	var secondResp map[string]interface{}
	if err := json.NewDecoder(rec2.Body).Decode(&secondResp); err != nil {
		t.Fatalf("Failed to decode second response: %v", err)
	}
	secondJob := secondResp["job"].(map[string]interface{})
	secondJobID := secondJob["id"].(string)

	// Should return same job ID
	if firstJobID != secondJobID {
		t.Errorf("Idempotent replay returned different job ID: %s vs %s", firstJobID, secondJobID)
	}
}

// =============================================================================
// Panic Recovery Tests
// =============================================================================

func TestRecovererMiddleware(t *testing.T) {
	srv := New(Config{})

	// Create a handler that panics
	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	// Wrap with recoverer
	handler := srv.recovererMiddleware(panicHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	// Should not panic
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp["success"] != false {
		t.Error("Expected success=false")
	}
	if resp["error_code"] != "INTERNAL_ERROR" {
		t.Errorf("Error code = %v, want INTERNAL_ERROR", resp["error_code"])
	}
}
