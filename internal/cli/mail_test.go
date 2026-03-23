package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/agentmail"
	"github.com/Dicklesworthstone/ntm/internal/startup"
)

type mailStub struct {
	server            *httptest.Server
	inbox             []agentmail.InboxMessage
	listAgents        []agentmail.Agent
	fetchCalls        []fetchCall
	readIDs           []int
	ackIDs            []int
	readAgents        []string
	ackAgents         []string
	ensureCalled      int
	ensureProjectKeys []string
	overseerCalls     []overseerCall
	releaseCalls      []releaseCall
	renewCalls        []renewCall
	releaseResult     agentmail.ReleaseReservationsResult
	renewResult       agentmail.RenewReservationsResult
	failIDs           map[int]string // messageID -> error message
}

type fetchCall struct {
	Agent   string
	Limit   int
	Urgent  bool
	From    string
	Project string
}

type releaseCall struct {
	Agent   string
	Project string
	Paths   []string
	IDs     []int
}

type renewCall struct {
	Agent         string
	Project       string
	ExtendSeconds int
	Paths         []string
	IDs           []int
}

type overseerCall struct {
	Recipients []string `json:"recipients"`
	Subject    string   `json:"subject"`
	BodyMD     string   `json:"body_md"`
	ThreadID   string   `json:"thread_id,omitempty"`
}

func newMailStub(t *testing.T, inbox []agentmail.InboxMessage) *mailStub {
	t.Helper()
	stub := &mailStub{
		inbox:         inbox,
		listAgents:    []agentmail.Agent{{Name: "BlueLake"}},
		failIDs:       make(map[int]string),
		releaseResult: agentmail.ReleaseReservationsResult{Released: 1},
		renewResult:   agentmail.RenewReservationsResult{Renewed: 1},
	}

	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/health/liveness" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}

		if r.Method == http.MethodPost && r.URL.Path == "/mail/stub/overseer/send" {
			var call overseerCall
			if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			stub.overseerCalls = append(stub.overseerCalls, call)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success":    true,
				"message_id": 123,
				"recipients": call.Recipients,
				"sent_at":    "2026-02-01T00:00:00Z",
			})
			return
		}

		var rpc agentmail.JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&rpc); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		params, ok := rpc.Params.(map[string]interface{})
		if !ok {
			http.Error(w, "invalid params", http.StatusBadRequest)
			return
		}

		name, _ := params["name"].(string)
		args, _ := params["arguments"].(map[string]interface{})

		writeResponse := func(result interface{}) {
			resp := agentmail.JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      rpc.ID,
				Result:  mustMarshalRaw(t, result),
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(&resp); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		}

		writeError := func(w http.ResponseWriter, id interface{}, code int, msg string) {
			resp := agentmail.JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      id,
				Error: &agentmail.JSONRPCError{
					Code:    code,
					Message: msg,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(&resp)
		}

		switch name {
		case "health_check":
			writeResponse(map[string]interface{}{"status": "ok"})
		case "ensure_project":
			stub.ensureCalled++
			stub.ensureProjectKeys = append(stub.ensureProjectKeys, toString(args["human_key"]))
			project := map[string]interface{}{
				"id":        1,
				"slug":      "stub",
				"human_key": args["human_key"],
			}
			writeResponse(project)
		case "list_agents":
			writeResponse(stub.listAgents)
		case "fetch_inbox":
			call := fetchCall{
				Agent:   toString(args["agent_name"]),
				Project: toString(args["project_key"]),
				Urgent:  toBool(args["urgent_only"]),
				Limit:   toInt(args["limit"]),
			}
			stub.fetchCalls = append(stub.fetchCalls, call)
			messages := stub.inbox
			if call.Urgent {
				filtered := make([]agentmail.InboxMessage, 0, len(messages))
				for _, m := range messages {
					if m.Importance == "urgent" {
						filtered = append(filtered, m)
					}
				}
				messages = filtered
			}
			writeResponse(map[string]interface{}{"result": messages})
		case "mark_message_read":
			id := toInt(args["message_id"])
			stub.readIDs = append(stub.readIDs, id)
			stub.readAgents = append(stub.readAgents, toString(args["agent_name"]))
			if msg, ok := stub.failIDs[id]; ok {
				writeError(w, rpc.ID, -32000, msg)
				return
			}
			writeResponse(map[string]interface{}{})
		case "acknowledge_message":
			id := toInt(args["message_id"])
			stub.ackIDs = append(stub.ackIDs, id)
			stub.ackAgents = append(stub.ackAgents, toString(args["agent_name"]))
			if msg, ok := stub.failIDs[id]; ok {
				writeError(w, rpc.ID, -32000, msg)
				return
			}
			writeResponse(map[string]interface{}{})
		case "release_file_reservations":
			stub.releaseCalls = append(stub.releaseCalls, releaseCall{
				Agent:   toString(args["agent_name"]),
				Project: toString(args["project_key"]),
				Paths:   toStringSlice(args["paths"]),
				IDs:     toIntSlice(args["file_reservation_ids"]),
			})
			writeResponse(stub.releaseResult)
		case "renew_file_reservations":
			stub.renewCalls = append(stub.renewCalls, renewCall{
				Agent:         toString(args["agent_name"]),
				Project:       toString(args["project_key"]),
				ExtendSeconds: toInt(args["extend_seconds"]),
				Paths:         toStringSlice(args["paths"]),
				IDs:           toIntSlice(args["file_reservation_ids"]),
			})
			writeResponse(stub.renewResult)
		default:
			http.Error(w, "unknown tool "+name, http.StatusNotFound)
		}
	}))

	return stub
}

func (s *mailStub) Close() {
	if s.server != nil {
		s.server.Close()
	}
}

func mustMarshalRaw(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func toInt(v interface{}) int {
	switch val := v.(type) {
	case float64:
		return int(val)
	case int:
		return val
	default:
		return 0
	}
}

func toBool(v interface{}) bool {
	val, _ := v.(bool)
	return val
}

func toString(v interface{}) string {
	val, _ := v.(string)
	return val
}

func toStringSlice(v interface{}) []string {
	raw, ok := v.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		if str, ok := item.(string); ok {
			result = append(result, str)
		}
	}
	return result
}

func toIntSlice(v interface{}) []int {
	raw, ok := v.([]interface{})
	if !ok {
		return nil
	}
	result := make([]int, 0, len(raw))
	for _, item := range raw {
		result = append(result, toInt(item))
	}
	return result
}

func saveSessionAgentForTest(t *testing.T, session, projectKey, agentName string) {
	t.Helper()
	now := time.Now()
	info := &agentmail.SessionAgentInfo{
		AgentName:    agentName,
		ProjectKey:   projectKey,
		RegisteredAt: now,
		LastActiveAt: now,
	}
	if err := agentmail.SaveSessionAgent(session, projectKey, info); err != nil {
		t.Fatalf("save session agent: %v", err)
	}
}

func execCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()
	resetFlags()
	// Reset startup config cache so AGENT_MAIL_URL env var is picked up
	// when config is re-loaded during command execution.
	startup.ResetConfig()
	rootCmd.SetArgs(args)
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	err := rootCmd.Execute()
	return buf.String(), err
}

func TestMailMarkRequiresSelector(t *testing.T) {
	inbox := []agentmail.InboxMessage{}
	stub := newMailStub(t, inbox)
	defer stub.Close()

	t.Setenv("AGENT_MAIL_URL", stub.server.URL+"/")
	t.Setenv("AGENT_NAME", "EnvAgent")

	_, err := execCommand(t, "mail", "read", "mysession", "--agent", "EnvAgent")
	if err == nil {
		t.Fatalf("expected error when no ids/filters/all provided")
	}
}

func TestMailMarkRequiresAgentOrEnv(t *testing.T) {
	inbox := []agentmail.InboxMessage{}
	stub := newMailStub(t, inbox)
	defer stub.Close()

	t.Setenv("AGENT_MAIL_URL", stub.server.URL+"/")

	_, err := execCommand(t, "mail", "ack", "mysession", "5")
	if err == nil {
		t.Fatalf("expected error when agent is missing")
	}
}

func TestMailAckUsesEnvAgent(t *testing.T) {
	inbox := []agentmail.InboxMessage{}
	stub := newMailStub(t, inbox)
	defer stub.Close()

	t.Setenv("AGENT_MAIL_URL", stub.server.URL+"/")
	t.Setenv("AGENT_NAME", "EnvAgent")

	if _, err := execCommand(t, "mail", "ack", "mysession", "42", "--json"); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if len(stub.ackIDs) != 1 || stub.ackIDs[0] != 42 {
		t.Fatalf("expected ack of id 42, got %v", stub.ackIDs)
	}
	if len(stub.ackAgents) != 1 || stub.ackAgents[0] != "EnvAgent" {
		t.Fatalf("expected agent EnvAgent, got %v", stub.ackAgents)
	}
}

func TestMailMarkReportsErrorsInJSON(t *testing.T) {
	inbox := []agentmail.InboxMessage{}
	stub := newMailStub(t, inbox)
	stub.failIDs[99] = "already acknowledged"
	defer stub.Close()

	t.Setenv("AGENT_MAIL_URL", stub.server.URL+"/")
	t.Setenv("AGENT_NAME", "EnvAgent")

	out, err := execCommand(t, "mail", "ack", "mysession", "99", "--json")
	if err != nil {
		t.Fatalf("expected command to finish with JSON summary, got error: %v", err)
	}
	if !strings.Contains(out, `"errors": 1`) {
		t.Fatalf("expected JSON summary to report errors, got: %s", out)
	}
}

func TestMailSendOverseer_RedactModeScrubsBodyAndSubject(t *testing.T) {
	stub := newMailStub(t, nil)
	defer stub.Close()

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AGENT_MAIL_URL", stub.server.URL+"/")

	_, err := execCommand(t, "--redact=redact", "mail", "send", "mysession", "--to", "BlueLake", "prefix password=hunter2hunter2 suffix")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(stub.overseerCalls) != 1 {
		t.Fatalf("expected 1 overseer call, got %d", len(stub.overseerCalls))
	}
	call := stub.overseerCalls[0]
	if strings.Contains(call.BodyMD, "hunter2hunter2") {
		t.Fatalf("expected body to be redacted, got %q", call.BodyMD)
	}
	if !strings.Contains(call.BodyMD, "[REDACTED:PASSWORD:") {
		t.Fatalf("expected redaction placeholder in body, got %q", call.BodyMD)
	}
	if strings.Contains(call.Subject, "hunter2hunter2") {
		t.Fatalf("expected subject to be redacted, got %q", call.Subject)
	}
}

func TestMailSendOverseer_BlockModeRefusesBeforeSend(t *testing.T) {
	stub := newMailStub(t, nil)
	defer stub.Close()

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AGENT_MAIL_URL", stub.server.URL+"/")

	_, err := execCommand(t, "--redact=block", "mail", "send", "mysession", "--to", "BlueLake", "password=hunter2hunter2")
	if err == nil {
		t.Fatalf("expected error")
	}
	if len(stub.overseerCalls) != 0 {
		t.Fatalf("expected no overseer calls when blocked, got %d", len(stub.overseerCalls))
	}
}

func TestMailSendOverseer_WarnModeSendsUnmodifiedButWarns(t *testing.T) {
	stub := newMailStub(t, nil)
	defer stub.Close()

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AGENT_MAIL_URL", stub.server.URL+"/")

	out, err := execCommand(t, "mail", "send", "mysession", "--to", "BlueLake", "password=hunter2hunter2")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(stub.overseerCalls) != 1 {
		t.Fatalf("expected 1 overseer call, got %d", len(stub.overseerCalls))
	}
	call := stub.overseerCalls[0]
	if !strings.Contains(call.BodyMD, "hunter2hunter2") {
		t.Fatalf("expected body to be unmodified in warn mode, got %q", call.BodyMD)
	}
	if !strings.Contains(out, "Warning: detected") {
		t.Fatalf("expected warning output, got %q", out)
	}
}

func TestMailMarkJSONPartialSuccess(t *testing.T) {
	inbox := []agentmail.InboxMessage{}
	stub := newMailStub(t, inbox)
	stub.failIDs[7] = "already read"
	defer stub.Close()

	t.Setenv("AGENT_MAIL_URL", stub.server.URL+"/")
	t.Setenv("AGENT_NAME", "EnvAgent")

	out, err := execCommand(t, "mail", "read", "mysession", "7", "8", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dec := json.NewDecoder(strings.NewReader(out))
	var summary markSummary
	if err := dec.Decode(&summary); err != nil {
		t.Fatalf("decode summary: %v (out=%s)", err, out)
	}
	if summary.Processed != 1 || summary.Errors != 1 || summary.Skipped != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if len(summary.IDs) != 2 || summary.IDs[0] != 7 || summary.IDs[1] != 8 {
		t.Fatalf("unexpected ids: %+v", summary.IDs)
	}
}

func TestMailReadWithFilters(t *testing.T) {
	inbox := []agentmail.InboxMessage{
		{ID: 1, From: "BlueBear", Importance: "urgent", CreatedTS: agentmail.FlexTime{Time: time.Now()}},
		{ID: 2, From: "LilacDog", Importance: "urgent", CreatedTS: agentmail.FlexTime{Time: time.Now()}},
		{ID: 3, From: "BlueBear", Importance: "normal", CreatedTS: agentmail.FlexTime{Time: time.Now()}},
	}
	stub := newMailStub(t, inbox)
	defer stub.Close()

	t.Setenv("AGENT_MAIL_URL", stub.server.URL+"/")

	if _, err := execCommand(t, "mail", "read", "mysession", "--agent", "TestAgent", "--urgent", "--from", "BlueBear"); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if len(stub.readIDs) != 1 {
		t.Fatalf("expected 1 message marked, got %d", len(stub.readIDs))
	}
	if stub.readIDs[0] != 1 {
		t.Fatalf("unexpected ids: %v", stub.readIDs)
	}
	if len(stub.fetchCalls) != 1 || !stub.fetchCalls[0].Urgent {
		t.Fatalf("expected urgent fetch, got %+v", stub.fetchCalls)
	}
}

func TestRunUnlockErrorsOnZeroSpecificRelease(t *testing.T) {
	resetFlags()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	projectKey := GetProjectRoot()
	session := "unlock-zero"
	agentName := "BlueLake"
	saveSessionAgentForTest(t, session, projectKey, agentName)

	stub := newMailStub(t, nil)
	stub.releaseResult = agentmail.ReleaseReservationsResult{Released: 0}
	defer stub.Close()

	t.Setenv("AGENT_MAIL_URL", stub.server.URL+"/")

	err := runUnlock(session, []string{"internal/cli/*.go"}, false)
	if err == nil {
		t.Fatal("expected unlock to fail when no requested reservations were released")
	}
	if !strings.Contains(err.Error(), "released 0 reservations") {
		t.Fatalf("expected zero-release error, got %v", err)
	}
	if len(stub.releaseCalls) != 1 {
		t.Fatalf("expected one release call, got %d", len(stub.releaseCalls))
	}
	if got := stub.releaseCalls[0].Paths; len(got) != 1 || got[0] != "internal/cli/*.go" {
		t.Fatalf("expected release call for requested pattern, got %v", got)
	}
}

func TestRunRenewLocksUsesProjectRootFromSubdir(t *testing.T) {
	resetFlags()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	projectKey := GetProjectRoot()
	session := "renew-root"
	agentName := "GreenLake"
	saveSessionAgentForTest(t, session, projectKey, agentName)

	stub := newMailStub(t, nil)
	stub.renewResult = agentmail.RenewReservationsResult{Renewed: 2}
	defer stub.Close()

	t.Setenv("AGENT_MAIL_URL", stub.server.URL+"/")
	t.Chdir(filepath.Join(projectKey, "internal"))

	if err := runRenewLocks(session, 30); err != nil {
		t.Fatalf("runRenewLocks: %v", err)
	}
	if len(stub.renewCalls) != 1 {
		t.Fatalf("expected one renew call, got %d", len(stub.renewCalls))
	}
	if stub.renewCalls[0].Project != projectKey {
		t.Fatalf("expected renew project %q, got %q", projectKey, stub.renewCalls[0].Project)
	}
}

func TestRunRenewLocksErrorsOnZeroRenewed(t *testing.T) {
	resetFlags()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	projectKey := GetProjectRoot()
	session := "renew-zero"
	agentName := "RedStone"
	saveSessionAgentForTest(t, session, projectKey, agentName)

	stub := newMailStub(t, nil)
	stub.renewResult = agentmail.RenewReservationsResult{Renewed: 0}
	defer stub.Close()

	t.Setenv("AGENT_MAIL_URL", stub.server.URL+"/")

	err := runRenewLocks(session, 30)
	if err == nil {
		t.Fatal("expected renew to fail when no reservations were renewed")
	}
	if !strings.Contains(err.Error(), "no active reservations were renewed") {
		t.Fatalf("expected zero-renew error, got %v", err)
	}
}

func TestMailInboxUsesProjectRootFromSubdir(t *testing.T) {
	resetFlags()
	t.Setenv("AGENT_MAIL_URL", "")
	stub := newMailStub(t, []agentmail.InboxMessage{
		{ID: 7, Subject: "Inbox subject", From: "BlueLake", CreatedTS: agentmail.FlexTime{Time: time.Now()}},
	})
	defer stub.Close()

	projectKey := GetProjectRoot()
	t.Setenv("AGENT_MAIL_URL", stub.server.URL+"/")
	t.Chdir(filepath.Join(projectKey, "internal"))

	if _, err := execCommand(t, "mail", "inbox", "--json"); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(stub.fetchCalls) != 1 {
		t.Fatalf("expected one fetch call, got %d", len(stub.fetchCalls))
	}
	if stub.fetchCalls[0].Project != projectKey {
		t.Fatalf("expected inbox project %q, got %q", projectKey, stub.fetchCalls[0].Project)
	}
}

func TestMailReadUsesProjectRootFromSubdir(t *testing.T) {
	resetFlags()
	stub := newMailStub(t, []agentmail.InboxMessage{
		{ID: 9, Subject: "Read me", From: "BlueLake", CreatedTS: agentmail.FlexTime{Time: time.Now()}},
	})
	defer stub.Close()

	projectKey := GetProjectRoot()
	t.Setenv("AGENT_MAIL_URL", stub.server.URL+"/")
	t.Chdir(filepath.Join(projectKey, "internal"))

	if _, err := execCommand(t, "mail", "read", "mysession", "--agent", "BlueLake", "--all"); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(stub.fetchCalls) != 1 {
		t.Fatalf("expected one fetch call, got %d", len(stub.fetchCalls))
	}
	if stub.fetchCalls[0].Project != projectKey {
		t.Fatalf("expected read project %q, got %q", projectKey, stub.fetchCalls[0].Project)
	}
	if len(stub.ensureProjectKeys) != 1 || stub.ensureProjectKeys[0] != projectKey {
		t.Fatalf("expected ensure_project for %q, got %v", projectKey, stub.ensureProjectKeys)
	}
}
