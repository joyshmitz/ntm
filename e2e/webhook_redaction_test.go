//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM CLI commands.
// [E2E-WEBHOOK] Redaction coverage for webhook payloads.
package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWebhookE2E_Redaction(t *testing.T) {
	CommonE2EPrerequisites(t)
	SkipIfNoAgents(t)

	agentType := GetAvailableAgent()
	if agentType == "" {
		t.Skip("no agent CLI available")
	}

	suite := NewWebhookTestSuite(t, "webhook_redaction")
	defer suite.Cleanup()

	secret := "ghp_" + strings.Repeat("a", 30)
	secretBase, err := os.MkdirTemp("", "ntm-webhook-"+secret+"-")
	if err != nil {
		t.Fatalf("[E2E-WEBHOOK] Failed to create secret base dir: %v", err)
	}
	suite.cleanup = append(suite.cleanup, func() {
		_ = os.RemoveAll(secretBase)
	})
	suite.baseDir = secretBase
	suite.projectDir = filepath.Join(secretBase, suite.session)
	if err := os.MkdirAll(suite.projectDir, 0755); err != nil {
		t.Fatalf("[E2E-WEBHOOK] Failed to create project dir: %v", err)
	}

	recv := make(chan webhookCapture, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)
		var payload WebhookPayload
		_ = json.Unmarshal(body, &payload)
		recv <- webhookCapture{payload: payload, raw: body}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	suite.WriteConfig("\nwebhooks:\n  - name: redaction\n    url: " + srv.URL + "\n    events: [\"session.created\"]\n    formatter: json\n")

	if err := suite.spawn(agentType); err != nil {
		t.Fatalf("[E2E-WEBHOOK] spawn failed: %v", err)
	}
	defer suite.kill()

	capture := waitForWebhook(t, recv, 30*time.Second)
	suite.logger.LogJSON("[E2E-WEBHOOK] payload", capture.payload)
	suite.logger.Log("[E2E-WEBHOOK] raw body: %s", string(capture.raw))

	raw := string(capture.raw)
	if strings.Contains(raw, secret) {
		t.Fatalf("[E2E-WEBHOOK] raw payload leaked secret: %s", secret)
	}
	if !strings.Contains(raw, "[REDACTED:GITHUB_TOKEN:") {
		t.Fatalf("[E2E-WEBHOOK] expected redaction placeholder in payload")
	}

	if projectDir := capture.payload.Details["project_dir"]; projectDir != "" {
		if strings.Contains(projectDir, secret) {
			t.Fatalf("[E2E-WEBHOOK] project_dir leaked secret: %s", projectDir)
		}
		if !strings.Contains(projectDir, "[REDACTED:GITHUB_TOKEN:") {
			t.Fatalf("[E2E-WEBHOOK] project_dir missing redaction placeholder")
		}
	}
}
