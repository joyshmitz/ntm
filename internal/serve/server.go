// Package serve provides an HTTP server for NTM with REST API and event streaming.
package serve

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/websocket"

	"github.com/Dicklesworthstone/ntm/internal/agentmail"
	"github.com/Dicklesworthstone/ntm/internal/events"
	"github.com/Dicklesworthstone/ntm/internal/kernel"
	"github.com/Dicklesworthstone/ntm/internal/redaction"
	"github.com/Dicklesworthstone/ntm/internal/robot"
	"github.com/Dicklesworthstone/ntm/internal/state"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

// Server provides HTTP API and event streaming for NTM.
type Server struct {
	host          string
	port          int
	publicBaseURL string
	eventBus      *events.EventBus
	stateStore    *state.Store
	server        *http.Server
	auth          AuthConfig

	// SSE clients
	sseClients   map[chan events.BusEvent]struct{}
	sseClientsMu sync.RWMutex

	corsAllowedOrigins []string
	jwksCache          *jwksCache

	// Idempotency support
	idempotencyStore *IdempotencyStore

	// Job management
	jobStore *JobStore

	// Chi router for /api/v1
	router chi.Router

	// WebSocket hub for real-time subscriptions
	wsHub *WSHub

	// Pane output streaming
	streamManager *tmux.StreamManager

	// Agent Mail client (lazy-init)
	mailClient *agentmail.Client
	projectDir string
	mu         sync.Mutex

	// Redaction configuration for REST API
	redactionCfg *RedactionConfig
}

// AuthMode configures authentication for the server.
type AuthMode string

const (
	AuthModeLocal  AuthMode = "local"
	AuthModeAPIKey AuthMode = "api_key"
	AuthModeOIDC   AuthMode = "oidc"
	AuthModeMTLS   AuthMode = "mtls"
)

// AuthConfig holds server authentication configuration.
type AuthConfig struct {
	Mode   AuthMode
	APIKey string
	OIDC   OIDCConfig
	MTLS   MTLSConfig
}

// OIDCConfig configures OIDC/JWT verification for API access.
type OIDCConfig struct {
	Issuer   string
	Audience string
	JWKSURL  string
	CacheTTL time.Duration
}

// MTLSConfig configures mutual TLS for API access.
type MTLSConfig struct {
	CertFile     string
	KeyFile      string
	ClientCAFile string
}

// Config holds server configuration.
type Config struct {
	Host string
	Port int
	// PublicBaseURL advertises the externally reachable base URL for clients.
	// Optional: leave empty to derive from host/port in documentation or clients.
	PublicBaseURL string
	EventBus      *events.EventBus
	StateStore    *state.Store
	Auth          AuthConfig
	// AllowedOrigins controls CORS origin allowlist. Empty means default localhost only.
	AllowedOrigins []string
}

const (
	defaultPort         = 7337
	defaultJWKSCacheTTL = 10 * time.Minute
)

const requestIDHeader = "X-Request-Id"

type ctxKey string

const requestIDKey ctxKey = "request_id"

// Response envelope types matching robot mode output format.
// Arrays are always initialized to [] (never null).

// APIResponse is the base envelope for all API responses.
type APIResponse struct {
	Success   bool   `json:"success"`
	Timestamp string `json:"timestamp"`
	RequestID string `json:"request_id,omitempty"`
}

// APIError represents a structured error response.
type APIError struct {
	APIResponse
	Error     string                 `json:"error"`
	ErrorCode string                 `json:"error_code,omitempty"`
	Details   map[string]interface{} `json:"details,omitempty"`
	Hint      string                 `json:"hint,omitempty"`
}

// Common error codes (matching robot mode conventions).
const (
	ErrCodeBadRequest       = "BAD_REQUEST"
	ErrCodeUnauthorized     = "UNAUTHORIZED"
	ErrCodeForbidden        = "FORBIDDEN"
	ErrCodeNotFound         = "NOT_FOUND"
	ErrCodeMethodNotAllowed = "METHOD_NOT_ALLOWED"
	ErrCodeConflict         = "CONFLICT"
	ErrCodeInternalError    = "INTERNAL_ERROR"
	ErrCodeServiceUnavail   = "SERVICE_UNAVAILABLE"
	ErrCodeIdempotentReplay = "IDEMPOTENT_REPLAY"
	ErrCodeJobPending       = "JOB_PENDING"
)

// IdempotencyStore caches responses by idempotency key.
type IdempotencyStore struct {
	mu       sync.RWMutex
	entries  map[string]*idempotencyEntry
	ttl      time.Duration
	stop     chan struct{}
	stopOnce sync.Once
}

type idempotencyEntry struct {
	response   []byte
	statusCode int
	createdAt  time.Time
}

// NewIdempotencyStore creates an idempotency cache with the given TTL.
func NewIdempotencyStore(ttl time.Duration) *IdempotencyStore {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	store := &IdempotencyStore{
		entries: make(map[string]*idempotencyEntry),
		ttl:     ttl,
		stop:    make(chan struct{}),
	}
	// Start cleanup goroutine
	go store.cleanup()
	return store
}

// Stop terminates the cleanup goroutine. Call this when the store is no longer needed.
// Safe to call multiple times.
func (s *IdempotencyStore) Stop() {
	s.stopOnce.Do(func() {
		close(s.stop)
	})
}

func (s *IdempotencyStore) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.mu.Lock()
			now := time.Now()
			for key, entry := range s.entries {
				if now.Sub(entry.createdAt) > s.ttl {
					delete(s.entries, key)
				}
			}
			s.mu.Unlock()
		}
	}
}

// Get returns a cached response for the idempotency key.
func (s *IdempotencyStore) Get(key string) ([]byte, int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.entries[key]
	if !ok {
		return nil, 0, false
	}
	if time.Since(entry.createdAt) > s.ttl {
		return nil, 0, false
	}
	return entry.response, entry.statusCode, true
}

// Set stores a response for the idempotency key.
func (s *IdempotencyStore) Set(key string, response []byte, statusCode int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[key] = &idempotencyEntry{
		response:   response,
		statusCode: statusCode,
		createdAt:  time.Now(),
	}
}

// Job represents an asynchronous operation.
type Job struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Status    JobStatus              `json:"status"`
	Progress  float64                `json:"progress,omitempty"`
	Result    map[string]interface{} `json:"result,omitempty"`
	Error     string                 `json:"error,omitempty"`
	CreatedAt string                 `json:"created_at"`
	UpdatedAt string                 `json:"updated_at"`
}

// JobStatus represents the state of a job.
type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
	JobStatusCancelled JobStatus = "cancelled"
)

// JobStore manages asynchronous jobs.
type JobStore struct {
	mu   sync.RWMutex
	jobs map[string]*Job
}

// NewJobStore creates a new job store.
func NewJobStore() *JobStore {
	return &JobStore{
		jobs: make(map[string]*Job),
	}
}

// Create creates a new job.
func (s *JobStore) Create(jobType string) *Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := generateRequestID()
	now := time.Now().UTC().Format(time.RFC3339)
	job := &Job{
		ID:        id,
		Type:      jobType,
		Status:    JobStatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.jobs[id] = job
	return s.cloneJob(job)
}

// Get retrieves a job by ID.
func (s *JobStore) Get(id string) *Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if job, ok := s.jobs[id]; ok {
		return s.cloneJob(job)
	}
	return nil
}

// Update updates a job's status and progress.
func (s *JobStore) Update(id string, status JobStatus, progress float64, result map[string]interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return
	}
	job.Status = status
	job.Progress = progress
	job.Result = result
	job.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
}

// cloneJob creates a deep copy of a job
func (s *JobStore) cloneJob(job *Job) *Job {
	if job == nil {
		return nil
	}
	copy := *job
	if job.Result != nil {
		copy.Result = make(map[string]interface{})
		for k, v := range job.Result {
			copy.Result[k] = v // shallow copy of values is usually enough for these JSON results
		}
	}
	return &copy
}

// List returns all jobs.
func (s *JobStore) List() []*Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	jobs := make([]*Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, s.cloneJob(job))
	}
	return jobs
}

// =============================================================================
// Background Jobs API
// =============================================================================

// CreateJobRequest is the request body for job creation.
type CreateJobRequest struct {
	Type    string                 `json:"type"`
	Params  map[string]interface{} `json:"params,omitempty"`
	Session string                 `json:"session,omitempty"`
}

// handleCreateJob handles POST /api/v1/jobs.
func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	reqID := requestIDFromContext(r.Context())

	var req CreateJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid request body", nil, reqID)
		return
	}

	if req.Type == "" {
		writeErrorResponse(w, http.StatusBadRequest, ErrCodeBadRequest, "job type required", nil, reqID)
		return
	}

	// Validate job type
	validTypes := map[string]bool{
		"spawn":      true,
		"scan":       true,
		"checkpoint": true,
		"import":     true,
		"export":     true,
	}
	if !validTypes[req.Type] {
		writeErrorResponse(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid job type", map[string]interface{}{
			"valid_types": []string{"spawn", "scan", "checkpoint", "import", "export"},
		}, reqID)
		return
	}

	job := s.jobStore.Create(req.Type)

	// Start job execution in background
	go s.executeJob(job.ID, req)

	writeSuccessResponse(w, http.StatusAccepted, map[string]interface{}{
		"job": job,
	}, reqID)
}

// executeJob runs a job asynchronously.
func (s *Server) executeJob(jobID string, req CreateJobRequest) {
	defer func() {
		if r := recover(); r != nil {
			s.jobStore.Update(jobID, JobStatusFailed, 0, nil, fmt.Sprintf("panic: %v", r))
		}
	}()
	s.jobStore.Update(jobID, JobStatusRunning, 0, nil, "")

	// Simulate job execution - in production, this would dispatch to actual handlers
	time.Sleep(100 * time.Millisecond)

	// Mark as completed
	result := map[string]interface{}{
		"type":    req.Type,
		"params":  req.Params,
		"session": req.Session,
	}
	s.jobStore.Update(jobID, JobStatusCompleted, 100, result, "")
}

// handleGetJob handles GET /api/v1/jobs/{id}.
func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	reqID := requestIDFromContext(r.Context())
	jobID := chi.URLParam(r, "id")

	job := s.jobStore.Get(jobID)
	if job == nil {
		writeErrorResponse(w, http.StatusNotFound, ErrCodeNotFound, "job not found", nil, reqID)
		return
	}

	writeSuccessResponse(w, http.StatusOK, map[string]interface{}{
		"job": job,
	}, reqID)
}

// handleCancelJob handles DELETE /api/v1/jobs/{id}.
func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	reqID := requestIDFromContext(r.Context())
	jobID := chi.URLParam(r, "id")

	job := s.jobStore.Get(jobID)
	if job == nil {
		writeErrorResponse(w, http.StatusNotFound, ErrCodeNotFound, "job not found", nil, reqID)
		return
	}

	// Only allow cancelling pending or running jobs
	if job.Status != JobStatusPending && job.Status != JobStatusRunning {
		writeErrorResponse(w, http.StatusConflict, ErrCodeConflict, "job cannot be cancelled", map[string]interface{}{
			"status": job.Status,
		}, reqID)
		return
	}

	s.jobStore.Update(jobID, JobStatusCancelled, job.Progress, nil, "cancelled by user")

	writeSuccessResponse(w, http.StatusOK, map[string]interface{}{
		"job": s.jobStore.Get(jobID),
	}, reqID)
}

// Router returns the chi router for testing.
func (s *Server) Router() chi.Router {
	return s.router
}

// ============================================================================
// WebSocket Handler
// ============================================================================

// checkWSOrigin validates the Origin header for WebSocket connections.
// In local auth mode, it allows any origin. Otherwise, it validates against
// the configured allowed origins to prevent WebSocket CSRF attacks.
func (s *Server) checkWSOrigin(r *http.Request) bool {
	// In local mode, accept any origin for development convenience
	if s.auth.Mode == AuthModeLocal || s.auth.Mode == "" {
		return true
	}

	origin := r.Header.Get("Origin")
	if origin == "" {
		// No origin header - allow for non-browser clients
		return true
	}

	// Parse the origin URL to extract scheme and host
	originURL, err := url.Parse(origin)
	if err != nil {
		log.Printf("ws: invalid origin URL %q: %v", origin, err)
		return false
	}

	// Reject malformed origins (e.g., "//example.com" or "https://")
	if originURL.Scheme == "" || originURL.Host == "" {
		log.Printf("ws: malformed origin %q (missing scheme or host)", origin)
		return false
	}

	// Check against configured allowed origins using full URL comparison
	// (not prefix matching, which would allow https://evil.com to match https://e)
	for _, allowed := range s.corsAllowedOrigins {
		allowedURL, err := url.Parse(allowed)
		if err != nil {
			continue
		}
		// Skip malformed allowed origins
		if allowedURL.Scheme == "" || allowedURL.Host == "" {
			continue
		}
		// Compare scheme and host (host includes port if specified)
		if originURL.Scheme == allowedURL.Scheme && originURL.Host == allowedURL.Host {
			return true
		}
	}

	log.Printf("ws: rejected origin %q (allowed: %v)", origin, s.corsAllowedOrigins)
	return false
}

// handleWebSocket handles WebSocket connections at /api/v1/ws.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Validate origin to prevent WebSocket CSRF attacks
	// Note: CORS middleware does NOT apply to WebSocket upgrades
	if !s.checkWSOrigin(r) {
		reqID := requestIDFromContext(r.Context())
		writeErrorResponse(w, http.StatusForbidden, ErrCodeForbidden, "origin not allowed", nil, reqID)
		return
	}

	// Upgrade HTTP connection to WebSocket
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade failed: %v", err)
		return
	}

	// Generate client ID
	clientID := generateRequestID()

	// Create client
	client := &WSClient{
		id:         clientID,
		conn:       conn,
		hub:        s.wsHub,
		send:       make(chan []byte, 256),
		topics:     make(map[string]struct{}),
		authClaims: extractAuthClaims(r),
	}

	// Register client with hub
	s.wsHub.register <- client

	// Start read and write pumps
	go client.writePump()
	go client.readPump()
}

// extractAuthClaims extracts auth claims from the request context.
func extractAuthClaims(r *http.Request) map[string]interface{} {
	// If using OIDC, extract claims from verified token
	claims := make(map[string]interface{})
	if authCtx := r.Context().Value(authContextKey); authCtx != nil {
		if m, ok := authCtx.(map[string]interface{}); ok {
			claims = m
		}
	}
	return claims
}

// authContextKey is the context key for auth claims.
type ctxKeyAuth struct{}

var authContextKey = ctxKeyAuth{}

// readPump reads messages from the WebSocket connection.
func (c *WSClient) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.closeOnce.Do(func() {
			if err := c.conn.Close(); err != nil {
				log.Printf("ws close error id=%s: %v", c.id, err)
			}
		})
	}()

	c.conn.SetReadLimit(wsMaxMessageSize)
	if err := c.conn.SetReadDeadline(time.Now().Add(wsPongWait)); err != nil {
		log.Printf("ws set read deadline error id=%s: %v", c.id, err)
		return
	}
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("ws read error id=%s: %v", c.id, err)
			}
			break
		}

		c.handleMessage(message)
	}
}

// writePump writes messages to the WebSocket connection.
func (c *WSClient) writePump() {
	ticker := time.NewTicker(wsPingPeriod)
	defer func() {
		ticker.Stop()
		c.closeOnce.Do(func() {
			if err := c.conn.Close(); err != nil {
				log.Printf("ws close error id=%s: %v", c.id, err)
			}
		})
	}()

	for {
		select {
		case message, ok := <-c.send:
			if err := c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait)); err != nil {
				log.Printf("ws set write deadline error id=%s: %v", c.id, err)
				return
			}
			if !ok {
				// Hub closed the channel
				if err := c.conn.WriteMessage(websocket.CloseMessage, []byte{}); err != nil {
					log.Printf("ws close frame error id=%s: %v", c.id, err)
				}
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			if _, err := w.Write(message); err != nil {
				return
			}

			// Drain queued messages
			n := len(c.send)
			for i := 0; i < n; i++ {
				if _, err := w.Write([]byte{'\n'}); err != nil {
					return
				}
				if _, err := w.Write(<-c.send); err != nil {
					return
				}
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait)); err != nil {
				log.Printf("ws ping deadline error id=%s: %v", c.id, err)
				return
			}
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage processes an incoming WebSocket message.
func (c *WSClient) handleMessage(data []byte) {
	var msg WSMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		c.sendError("", "parse_error", "invalid JSON message")
		return
	}

	switch msg.Type {
	case WSMsgSubscribe:
		c.handleSubscribe(msg)
	case WSMsgUnsubscribe:
		c.handleUnsubscribe(msg)
	case WSMsgPing:
		c.sendPong(msg.RequestID)
	default:
		c.sendError(msg.RequestID, "unknown_type", fmt.Sprintf("unknown message type: %s", msg.Type))
	}
}

// handleSubscribe processes a subscribe request.
func (c *WSClient) handleSubscribe(msg WSMessage) {
	// Extract topics from data
	topicsRaw, ok := msg.Data["topics"]
	if !ok {
		c.sendError(msg.RequestID, "missing_topics", "subscribe requires topics array")
		return
	}

	topicsSlice, ok := topicsRaw.([]interface{})
	if !ok {
		c.sendError(msg.RequestID, "invalid_topics", "topics must be an array")
		return
	}

	topics := make([]string, 0, len(topicsSlice))
	for _, t := range topicsSlice {
		if str, ok := t.(string); ok {
			// Validate topic format
			if !isValidTopic(str) {
				c.sendError(msg.RequestID, "invalid_topic", fmt.Sprintf("invalid topic: %s", str))
				return
			}
			topics = append(topics, str)
		}
	}

	if len(topics) == 0 {
		c.sendError(msg.RequestID, "empty_topics", "at least one topic required")
		return
	}

	// Check RBAC for topics
	for _, topic := range topics {
		if !c.canSubscribe(topic) {
			c.sendError(msg.RequestID, "unauthorized", fmt.Sprintf("not authorized for topic: %s", topic))
			return
		}
	}

	c.Subscribe(topics)
	c.sendAck(msg.RequestID, map[string]interface{}{
		"subscribed": topics,
		"total":      len(c.Topics()),
	})
}

// handleUnsubscribe processes an unsubscribe request.
func (c *WSClient) handleUnsubscribe(msg WSMessage) {
	topicsRaw, ok := msg.Data["topics"]
	if !ok {
		c.sendError(msg.RequestID, "missing_topics", "unsubscribe requires topics array")
		return
	}

	topicsSlice, ok := topicsRaw.([]interface{})
	if !ok {
		c.sendError(msg.RequestID, "invalid_topics", "topics must be an array")
		return
	}

	topics := make([]string, 0, len(topicsSlice))
	for _, t := range topicsSlice {
		if str, ok := t.(string); ok {
			topics = append(topics, str)
		}
	}

	c.Unsubscribe(topics)
	c.sendAck(msg.RequestID, map[string]interface{}{
		"unsubscribed": topics,
		"total":        len(c.Topics()),
	})
}

// isValidTopic checks if a topic string is valid.
//
// Note: This is intentionally permissive for topic *values* and primarily
// validates known topic namespaces, not the full shape of each topic string.
func isValidTopic(topic string) bool {
	if topic == "" {
		return false
	}
	if topic == "*" || topic == "global" || topic == "global:*" || topic == "scanner" || topic == "memory" {
		return true
	}
	// sessions:* or sessions:{name}
	if strings.HasPrefix(topic, "sessions:") {
		return true
	}
	// panes:* or panes:{session}:{idx}
	if strings.HasPrefix(topic, "panes:") {
		return true
	}
	// agent:{type}
	if strings.HasPrefix(topic, "agent:") {
		return true
	}
	// tool systems
	if strings.HasPrefix(topic, "beads:") ||
		strings.HasPrefix(topic, "mail:") ||
		strings.HasPrefix(topic, "reservations:") ||
		strings.HasPrefix(topic, "pipelines:") ||
		strings.HasPrefix(topic, "approvals:") ||
		strings.HasPrefix(topic, "accounts:") ||
		strings.HasPrefix(topic, "attention:") {
		return true
	}
	// attention topic without prefix (for main feed)
	if topic == "attention" {
		return true
	}
	return false
}

// canSubscribe checks if the client is authorized to subscribe to a topic.
func (c *WSClient) canSubscribe(topic string) bool {
	// For now, allow all authenticated clients to subscribe to any topic.
	// Future: implement RBAC based on auth claims.
	// Example checks:
	// - Check if user has access to specific session
	// - Check if user has agent-type filter permissions
	return true
}

// sendError sends a WebSocket error frame.
func (c *WSClient) sendError(requestID, code, message string) {
	errMsg := WSError{
		Type:      WSMsgError,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		RequestID: requestID,
		Code:      code,
		Message:   message,
	}
	data, err := json.Marshal(errMsg)
	if err != nil {
		return
	}
	select {
	case c.send <- data:
	default:
		log.Printf("ws client buffer full, dropping error id=%s", c.id)
	}
}

// sendAck sends a WebSocket acknowledgment frame.
func (c *WSClient) sendAck(requestID string, data map[string]interface{}) {
	ack := WSMessage{
		Type:      WSMsgAck,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		RequestID: requestID,
		Data:      data,
	}
	msg, err := json.Marshal(ack)
	if err != nil {
		return
	}
	select {
	case c.send <- msg:
	default:
		log.Printf("ws client buffer full, dropping ack id=%s", c.id)
	}
}

// sendPong sends a WebSocket pong response.
func (c *WSClient) sendPong(requestID string) {
	pong := WSMessage{
		Type:      WSMsgPong,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		RequestID: requestID,
	}
	data, err := json.Marshal(pong)
	if err != nil {
		return
	}
	select {
	case c.send <- data:
	default:
		// Buffer full, skip
	}
}

// WSHub returns the WebSocket hub for testing.
func (s *Server) WSHub() *WSHub {
	return s.wsHub
}

// =============================================================================
// Attention Feed Handlers
// =============================================================================

type attentionStreamFeed interface {
	Stats() robot.JournalStats
	Replay(sinceCursor int64, limit int) ([]robot.AttentionEvent, int64, error)
	Subscribe(robot.AttentionHandler) func()
}

type preparedAttentionStream struct {
	stats          robot.JournalStats
	replayBoundary int64
	replayEvents   []robot.AttentionEvent
	eventCh        chan robot.AttentionEvent
	unsubscribe    func()
}

func prepareAttentionStream(feed attentionStreamFeed, sinceCursor int64, bufferSize int) (*preparedAttentionStream, error) {
	if feed == nil {
		return nil, errors.New("attention feed unavailable")
	}
	if bufferSize <= 0 {
		bufferSize = 100
	}

	eventCh := make(chan robot.AttentionEvent, bufferSize)
	unsubscribe := feed.Subscribe(func(event robot.AttentionEvent) {
		select {
		case eventCh <- event:
		default:
			// Buffer full, drop event.
		}
	})

	stats := feed.Stats()
	if expired, earliest := attentionCursorExpired(sinceCursor, stats); expired {
		unsubscribe()
		return nil, &robot.CursorExpiredError{
			RequestedCursor: sinceCursor,
			EarliestCursor:  earliest,
			RetentionPeriod: stats.RetentionPeriod,
		}
	}

	prepared := &preparedAttentionStream{
		stats:          stats,
		replayBoundary: stats.NewestCursor,
		replayEvents:   []robot.AttentionEvent{},
		eventCh:        eventCh,
		unsubscribe:    unsubscribe,
	}
	if sinceCursor < 0 {
		return prepared, nil
	}

	replayLimit := stats.Count
	if replayLimit <= 0 {
		replayLimit = 1
	}
	events, _, err := feed.Replay(sinceCursor, replayLimit)
	if err != nil {
		unsubscribe()
		return nil, err
	}

	prepared.replayEvents = filterAttentionReplayBoundary(events, prepared.replayBoundary)
	return prepared, nil
}

func filterAttentionReplayBoundary(events []robot.AttentionEvent, maxCursor int64) []robot.AttentionEvent {
	if len(events) == 0 || maxCursor <= 0 {
		return []robot.AttentionEvent{}
	}

	filtered := make([]robot.AttentionEvent, 0, len(events))
	for _, event := range events {
		if event.Cursor > maxCursor {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered
}

// handleAttentionStreamV1 handles SSE streaming at /api/v1/attention/stream.
// Query params:
//   - since_cursor: replay from this cursor (0 = start from beginning, -1 = from now)
//   - category: filter by event category (comma-separated)
//   - session: filter by session name
//   - actionability: filter by actionability level (comma-separated)
//   - heartbeat: heartbeat interval in seconds (default 30, 0 to disable)
func (s *Server) handleAttentionStreamV1(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	sinceCursor := int64(0)
	if sc := r.URL.Query().Get("since_cursor"); sc != "" {
		parsed, err := strconv.ParseInt(sc, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid since_cursor: "+err.Error())
			return
		}
		sinceCursor = parsed
	}

	categoryFilter := parseCSVParam(r.URL.Query().Get("category"))
	sessionFilter := r.URL.Query().Get("session")
	actionabilityFilter := parseCSVParam(r.URL.Query().Get("actionability"))

	heartbeatInterval := 30 * time.Second
	if hb := r.URL.Query().Get("heartbeat"); hb != "" {
		parsed, err := strconv.Atoi(hb)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid heartbeat: "+err.Error())
			return
		}
		heartbeatInterval = time.Duration(parsed) * time.Second
	}

	feed := robot.GetAttentionFeed()
	prepared, err := prepareAttentionStream(feed, sinceCursor, 100)
	if err != nil {
		var cursorErr *robot.CursorExpiredError
		if !errors.As(err, &cursorErr) {
			writeError(w, http.StatusInternalServerError, "attention stream setup failed: "+err.Error())
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, http.StatusInternalServerError, "streaming not supported")
			return
		}

		cursorExpiredEvent := map[string]interface{}{
			"error_code":    robot.ErrCodeCursorExpired,
			"message":       "cursor has expired, resync required",
			"oldest_cursor": cursorErr.EarliestCursor,
			"newest_cursor": feed.Stats().NewestCursor,
			"resync_hint":   "fetch --robot-snapshot then resume from newest_cursor",
		}
		data, _ := json.Marshal(cursorExpiredEvent)
		if _, err := fmt.Fprintf(w, "event: error\ndata: %s\n\n", data); err != nil {
			return
		}
		flusher.Flush()
		return
	}
	defer prepared.unsubscribe()

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Send connection event
	connEvent := map[string]interface{}{
		"status":        "connected",
		"time":          time.Now().UTC().Format(time.RFC3339),
		"since_cursor":  sinceCursor,
		"oldest_cursor": attentionReplayEarliestCursor(prepared.stats),
		"newest_cursor": prepared.stats.NewestCursor,
	}
	connData, _ := json.Marshal(connEvent)
	if _, err := fmt.Fprintf(w, "event: connected\ndata: %s\n\n", connData); err != nil {
		return
	}
	flusher.Flush()

	// Replay events from cursor if requested
	replayCursor := prepared.replayBoundary
	for _, event := range prepared.replayEvents {
		if !matchesAttentionFilters(event, categoryFilter, sessionFilter, actionabilityFilter) {
			continue
		}
		data, err := json.Marshal(event)
		if err != nil {
			continue
		}
		if _, err := fmt.Fprintf(w, "event: attention\ndata: %s\n\n", data); err != nil {
			return
		}
	}
	flusher.Flush()

	// Set up heartbeat ticker
	var heartbeatTicker *time.Ticker
	var heartbeatCh <-chan time.Time
	if heartbeatInterval > 0 {
		heartbeatTicker = time.NewTicker(heartbeatInterval)
		heartbeatCh = heartbeatTicker.C
		defer heartbeatTicker.Stop()
	}

	// Stream events
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-prepared.eventCh:
			if event.Cursor <= replayCursor {
				// Already sent during replay
				continue
			}
			if !matchesAttentionFilters(event, categoryFilter, sessionFilter, actionabilityFilter) {
				continue
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: attention\ndata: %s\n\n", data); err != nil {
				return
			}
			flusher.Flush()
			replayCursor = event.Cursor
		case <-heartbeatCh:
			currentStats := feed.Stats()
			heartbeat := map[string]interface{}{
				"type":          "heartbeat",
				"time":          time.Now().UTC().Format(time.RFC3339),
				"oldest_cursor": attentionReplayEarliestCursor(currentStats),
				"newest_cursor": currentStats.NewestCursor,
				"event_count":   currentStats.Count,
			}
			data, _ := json.Marshal(heartbeat)
			if _, err := fmt.Fprintf(w, "event: heartbeat\ndata: %s\n\n", data); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// handleAttentionEventsV1 handles HTTP replay at /api/v1/attention/events.
// Query params:
//   - since_cursor: replay from this cursor (required)
//   - category: filter by event category (comma-separated)
//   - session: filter by session name
//   - actionability: filter by actionability level (comma-separated)
//   - limit: max events to return (default 100)
func (s *Server) handleAttentionEventsV1(w http.ResponseWriter, r *http.Request) {
	sinceCursor := int64(0)
	if sc := r.URL.Query().Get("since_cursor"); sc != "" {
		parsed, err := strconv.ParseInt(sc, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid since_cursor: "+err.Error())
			return
		}
		sinceCursor = parsed
	}

	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		parsed, err := strconv.Atoi(l)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid limit: "+err.Error())
			return
		}
		if parsed > 0 && parsed < 10000 {
			limit = parsed
		}
	}

	categoryFilter := parseCSVParam(r.URL.Query().Get("category"))
	sessionFilter := r.URL.Query().Get("session")
	actionabilityFilter := parseCSVParam(r.URL.Query().Get("actionability"))

	feed := robot.GetAttentionFeed()
	stats := feed.Stats()
	earliestCursor := attentionReplayEarliestCursor(stats)

	// Check for cursor expiration using the same boundary as the underlying journal.
	if expired, earliest := attentionCursorExpired(sinceCursor, stats); expired {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error_code":    robot.ErrCodeCursorExpired,
			"message":       "cursor has expired, resync required",
			"oldest_cursor": earliest,
			"newest_cursor": stats.NewestCursor,
			"resync_hint":   "fetch --robot-snapshot then resume from newest_cursor",
		})
		return
	}

	events, newestCursor, err := feed.Replay(sinceCursor, limit*2) // Fetch extra to account for filtering
	if err != nil {
		writeError(w, http.StatusInternalServerError, "replay failed: "+err.Error())
		return
	}

	// Apply filters
	filtered := make([]robot.AttentionEvent, 0, len(events))
	for _, event := range events {
		if !matchesAttentionFilters(event, categoryFilter, sessionFilter, actionabilityFilter) {
			continue
		}
		filtered = append(filtered, event)
		if len(filtered) >= limit {
			break
		}
	}

	truncated := len(filtered) >= limit && len(events) > len(filtered)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"events":        filtered,
		"since_cursor":  sinceCursor,
		"newest_cursor": newestCursor,
		"oldest_cursor": earliestCursor,
		"event_count":   len(filtered),
		"truncated":     truncated,
	})
}

// handleAttentionDigestV1 handles digest at /api/v1/attention/digest.
// Query params:
//   - since_cursor: aggregate events from this cursor
//   - category: filter by event category (comma-separated)
//   - session: filter by session name
//   - action_required_limit: max action_required items (default 5)
//   - interesting_limit: max interesting items (default 10)
//   - background_limit: max background items (default 5)
//   - trace: include decision trace (default false)
func (s *Server) handleAttentionDigestV1(w http.ResponseWriter, r *http.Request) {
	sinceCursor := int64(0)
	if sc := r.URL.Query().Get("since_cursor"); sc != "" {
		parsed, err := strconv.ParseInt(sc, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid since_cursor: "+err.Error())
			return
		}
		sinceCursor = parsed
	}

	opts := robot.DefaultAttentionDigestOptions()

	if arl := r.URL.Query().Get("action_required_limit"); arl != "" {
		if parsed, err := strconv.Atoi(arl); err == nil && parsed >= 0 {
			opts.ActionRequiredLimit = parsed
		}
	}
	if il := r.URL.Query().Get("interesting_limit"); il != "" {
		if parsed, err := strconv.Atoi(il); err == nil && parsed >= 0 {
			opts.InterestingLimit = parsed
		}
	}
	if bl := r.URL.Query().Get("background_limit"); bl != "" {
		if parsed, err := strconv.Atoi(bl); err == nil && parsed >= 0 {
			opts.BackgroundLimit = parsed
		}
	}
	if trace := r.URL.Query().Get("trace"); trace == "true" || trace == "1" {
		opts.IncludeTrace = true
	}

	categoryFilter := parseCSVParam(r.URL.Query().Get("category"))
	if len(categoryFilter) > 0 {
		categories := make([]robot.EventCategory, 0, len(categoryFilter))
		for _, cat := range categoryFilter {
			categories = append(categories, robot.EventCategory(cat))
		}
		opts.Categories = categories
	}

	sessionFilter := r.URL.Query().Get("session")
	if sessionFilter != "" {
		opts.Session = sessionFilter
	}

	feed := robot.GetAttentionFeed()
	stats := feed.Stats()

	// Check for cursor expiration using the same boundary as the underlying journal.
	if expired, earliest := attentionCursorExpired(sinceCursor, stats); expired {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error_code":    robot.ErrCodeCursorExpired,
			"message":       "cursor has expired, resync required",
			"oldest_cursor": earliest,
			"newest_cursor": stats.NewestCursor,
			"resync_hint":   "fetch --robot-snapshot then resume from newest_cursor",
		})
		return
	}

	digest, err := feed.Digest(sinceCursor, opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "digest failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, digest)
}

func attentionReplayEarliestCursor(stats robot.JournalStats) int64 {
	if stats.Count == 0 {
		return stats.NewestCursor
	}
	return stats.OldestCursor
}

func attentionCursorExpired(sinceCursor int64, stats robot.JournalStats) (bool, int64) {
	earliest := attentionReplayEarliestCursor(stats)
	if sinceCursor <= 0 || stats.NewestCursor == 0 {
		return false, earliest
	}
	if stats.Count == 0 {
		return sinceCursor < stats.NewestCursor, earliest
	}
	return sinceCursor < earliest-1, earliest
}

// matchesAttentionFilters checks if an event matches the specified filters.
func matchesAttentionFilters(event robot.AttentionEvent, categoryFilter []string, sessionFilter string, actionabilityFilter []string) bool {
	// Category filter
	if len(categoryFilter) > 0 {
		matched := false
		for _, cat := range categoryFilter {
			if string(event.Category) == cat {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Session filter
	if sessionFilter != "" && event.Session != sessionFilter {
		return false
	}

	// Actionability filter
	if len(actionabilityFilter) > 0 {
		matched := false
		for _, act := range actionabilityFilter {
			if string(event.Actionability) == act {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	return true
}

// parseCSVParam parses a comma-separated query parameter into a slice.
func parseCSVParam(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// Stop cleans up resources used by the Server.
func (s *Server) Stop() {
	if s.idempotencyStore != nil {
		s.idempotencyStore.Stop()
	}
}
