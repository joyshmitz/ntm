package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseWebhookConfig_Basic(t *testing.T) {
	t.Setenv("NTM_WEBHOOK_SECRET", "secret-value")

	content := `
scanner:
  defaults:
    timeout: 30s
webhooks:
  - name: slack-notifications
    url: https://hooks.slack.com/services/xxx
    events:
      - agent.completed
      - agent.error
    formatter: slack
    filter:
      session: myproject*
      agent_type: [claude, codex]
      severity: [warning, error]
    retry:
      max_attempts: 5
      backoff: exponential
    timeout: 30s
    secret: ${NTM_WEBHOOK_SECRET}
`

	cfgs, err := ParseWebhookConfig([]byte(content))
	if err != nil {
		t.Fatalf("ParseWebhookConfig failed: %v", err)
	}
	if len(cfgs) != 1 {
		t.Fatalf("expected 1 webhook, got %d", len(cfgs))
	}

	cfg := cfgs[0]
	if cfg.Name != "slack-notifications" {
		t.Fatalf("unexpected name: %q", cfg.Name)
	}
	if cfg.Formatter != "slack" {
		t.Fatalf("unexpected formatter: %q", cfg.Formatter)
	}
	if cfg.Secret != "secret-value" {
		t.Fatalf("expected env-substituted secret, got %q", cfg.Secret)
	}
	if cfg.Filter.Session != "myproject*" {
		t.Fatalf("unexpected filter.session: %q", cfg.Filter.Session)
	}
	if len(cfg.Filter.AgentType) != 2 {
		t.Fatalf("unexpected filter.agent_type: %#v", cfg.Filter.AgentType)
	}
	if cfg.Timeout != "30s" {
		t.Fatalf("unexpected timeout: %q", cfg.Timeout)
	}
}

func TestParseWebhookConfig_MissingEnvVar(t *testing.T) {
	content := `
webhooks:
  - name: missing-secret
    url: https://example.com/hook
    secret: ${NTM_MISSING_SECRET}
`
	_, err := ParseWebhookConfig([]byte(content))
	if err == nil {
		t.Fatal("expected error for missing env var, got nil")
	}
	if !strings.Contains(err.Error(), "missing environment variables") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseWebhookConfig_UnknownWebhookField(t *testing.T) {
	content := `
webhooks:
  - name: bad
    url: https://example.com/hook
    unknown_field: true
`
	_, err := ParseWebhookConfig([]byte(content))
	if err == nil {
		t.Fatal("expected error for unknown field, got nil")
	}
}

func TestParseWebhookConfig_InvalidURL(t *testing.T) {
	content := `
webhooks:
  - name: bad-url
    url: ftp://example.com/hook
`
	_, err := ParseWebhookConfig([]byte(content))
	if err == nil {
		t.Fatal("expected error for invalid url, got nil")
	}
	if !strings.Contains(err.Error(), "invalid url scheme") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseWebhookConfig_InvalidFormatter(t *testing.T) {
	content := `
webhooks:
  - name: bad-formatter
    url: https://example.com/hook
    formatter: not-a-real-formatter
`
	_, err := ParseWebhookConfig([]byte(content))
	if err == nil {
		t.Fatal("expected error for invalid formatter, got nil")
	}
	if !strings.Contains(err.Error(), "unknown formatter") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseWebhookConfig_InvalidEvent(t *testing.T) {
	content := `
webhooks:
  - name: bad-event
    url: https://example.com/hook
    events: ["not.real"]
`
	_, err := ParseWebhookConfig([]byte(content))
	if err == nil {
		t.Fatal("expected error for invalid event, got nil")
	}
	if !strings.Contains(err.Error(), "unknown event") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWatchProjectWebhooks(t *testing.T) {
	tmpDir := t.TempDir()

	path := filepath.Join(tmpDir, ".ntm.yaml")
	if err := os.WriteFile(path, []byte("webhooks: []\n"), 0644); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	updates := make(chan []WebhookConfig, 10)
	closeFn, err := WatchProjectWebhooks(tmpDir, func(cfgs []WebhookConfig) {
		updates <- cfgs
	})
	if err != nil {
		t.Fatalf("WatchProjectWebhooks failed: %v", err)
	}
	t.Cleanup(closeFn)

	// Drain initial.
	select {
	case <-updates:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for initial webhook config")
	}

	if err := os.WriteFile(path, []byte(`
webhooks:
  - name: one
    url: https://example.com/hook
`), 0644); err != nil {
		t.Fatalf("write updated config: %v", err)
	}

	select {
	case cfgs := <-updates:
		if len(cfgs) != 1 {
			t.Fatalf("expected 1 webhook after reload, got %d", len(cfgs))
		}
		if cfgs[0].Name != "one" {
			t.Fatalf("unexpected webhook name after reload: %q", cfgs[0].Name)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for webhook config reload")
	}
}
