package robot

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGetDocs_Index(t *testing.T) {
	// Test getting topic index (empty topic)
	output, err := GetDocs("")
	if err != nil {
		t.Fatalf("GetDocs failed: %v", err)
	}

	if !output.Success {
		t.Errorf("expected Success=true, got false")
	}

	if output.Version == "" {
		t.Errorf("expected non-empty version")
	}

	if output.SchemaVersion != CurrentSchemaVersion {
		t.Errorf("expected schema version %s, got %s", CurrentSchemaVersion, output.SchemaVersion)
	}

	if len(output.Topics) == 0 {
		t.Errorf("expected topics list, got empty")
	}

	// Verify all expected topics are present
	expectedTopics := map[string]bool{
		"quickstart": false,
		"commands":   false,
		"examples":   false,
		"exit-codes": false,
	}

	for _, topic := range output.Topics {
		if _, exists := expectedTopics[topic.Name]; exists {
			expectedTopics[topic.Name] = true
		}
	}

	for name, found := range expectedTopics {
		if !found {
			t.Errorf("expected topic %q not found", name)
		}
	}
}

func TestGetDocs_Quickstart(t *testing.T) {
	output, err := GetDocs("quickstart")
	if err != nil {
		t.Fatalf("GetDocs(quickstart) failed: %v", err)
	}

	if !output.Success {
		t.Errorf("expected Success=true, got false")
	}

	if output.Topic != "quickstart" {
		t.Errorf("expected topic 'quickstart', got %q", output.Topic)
	}

	if output.Content == nil {
		t.Fatal("expected content, got nil")
	}

	if output.Content.Title == "" {
		t.Errorf("expected non-empty title")
	}

	if len(output.Content.Sections) == 0 {
		t.Errorf("expected sections, got empty")
	}

	if len(output.Content.Examples) == 0 {
		t.Errorf("expected examples, got empty")
	}
}

func TestGetDocs_Commands(t *testing.T) {
	output, err := GetDocs("commands")
	if err != nil {
		t.Fatalf("GetDocs(commands) failed: %v", err)
	}

	if !output.Success {
		t.Errorf("expected Success=true, got false")
	}

	if output.Content == nil {
		t.Fatal("expected content, got nil")
	}

	if len(output.Content.Sections) == 0 {
		t.Errorf("expected sections for commands topic")
	}
}

func TestGetDocs_Examples(t *testing.T) {
	output, err := GetDocs("examples")
	if err != nil {
		t.Fatalf("GetDocs(examples) failed: %v", err)
	}

	if !output.Success {
		t.Errorf("expected Success=true, got false")
	}

	if output.Content == nil {
		t.Fatal("expected content, got nil")
	}

	if len(output.Content.Examples) == 0 {
		t.Errorf("expected examples, got empty")
	}

	// Verify example structure
	for _, ex := range output.Content.Examples {
		if ex.Name == "" {
			t.Errorf("expected example name, got empty")
		}
		if ex.Command == "" {
			t.Errorf("expected example command, got empty")
		}
		if ex.Description == "" {
			t.Errorf("expected example description, got empty")
		}
	}

	for _, ex := range output.Content.Examples {
		if strings.Contains(ex.Command, "--ack-timeout") || strings.Contains(ex.Command, "--ack-poll") || strings.Contains(ex.Command, "--wait-timeout") || strings.Contains(ex.Command, "--wait-poll") || strings.Contains(ex.Command, "--spawn-timeout") || strings.Contains(ex.Command, "--ready-timeout") {
			t.Fatalf("examples topic still uses deprecated shared modifiers: %q", ex.Command)
		}
	}

	foundCanonicalTrack := false
	for _, ex := range output.Content.Examples {
		if ex.Name == "send_and_track" && strings.Contains(ex.Command, "--timeout=60s") {
			foundCanonicalTrack = true
			break
		}
	}
	if !foundCanonicalTrack {
		t.Fatal("examples topic missing canonical send_and_track timeout example")
	}

	foundCanonicalWait := false
	for _, ex := range output.Content.Examples {
		if ex.Name == "wait_for_attention" && strings.Contains(ex.Command, "--attention-cursor=42") && strings.Contains(ex.Command, "--timeout=2m") {
			foundCanonicalWait = true
			break
		}
	}
	if !foundCanonicalWait {
		t.Fatal("examples topic missing canonical wait_for_attention cursor/timeout example")
	}

	foundRestartWithBead := false
	foundSmartRestartHardKill := false
	foundActivityFiltered := false
	foundSupportBundleRedacted := false
	for _, ex := range output.Content.Examples {
		switch ex.Name {
		case "restart_with_bead":
			foundRestartWithBead = strings.Contains(ex.Command, "--robot-restart-pane=proj") && strings.Contains(ex.Command, "--restart-bead=bd-abc12")
		case "smart_restart_hard_kill":
			foundSmartRestartHardKill = strings.Contains(ex.Command, "--robot-smart-restart=proj") && strings.Contains(ex.Command, "--hard-kill")
		case "activity_filtered":
			foundActivityFiltered = strings.Contains(ex.Command, "--robot-activity=proj") && strings.Contains(ex.Command, "--panes=0.1,%7")
		case "support_bundle_redacted":
			foundSupportBundleRedacted = strings.Contains(ex.Command, "--robot-support-bundle=proj") && strings.Contains(ex.Command, "--bundle-since=1h") && strings.Contains(ex.Command, "--bundle-redact=redact")
		}
	}
	if !foundRestartWithBead {
		t.Fatal("examples topic missing restart_with_bead example")
	}
	if !foundSmartRestartHardKill {
		t.Fatal("examples topic missing smart_restart_hard_kill example")
	}
	if !foundActivityFiltered {
		t.Fatal("examples topic missing activity_filtered example")
	}
	if !foundSupportBundleRedacted {
		t.Fatal("examples topic missing support_bundle_redacted example")
	}
}

func TestGetDocs_QuickstartMentionsJSONDocs(t *testing.T) {
	output, err := GetDocs("quickstart")
	if err != nil {
		t.Fatalf("GetDocs returned error: %v", err)
	}
	if output.Content == nil {
		t.Fatal("expected content, got nil")
	}
	if len(output.Content.Sections) == 0 {
		t.Fatal("quickstart topic should include sections")
	}

	var discovery string
	for _, section := range output.Content.Sections {
		if section.Heading == "Discovery" {
			discovery = section.Body
			break
		}
	}
	if discovery == "" {
		t.Fatal("quickstart topic missing Discovery section")
	}
	if !strings.Contains(discovery, "topic-scoped JSON documentation") {
		t.Fatalf("Discovery section should describe --robot-docs as JSON docs, got %q", discovery)
	}
	if strings.Contains(discovery, "human-readable documentation") {
		t.Fatalf("Discovery section should not describe --robot-docs as human-readable docs, got %q", discovery)
	}
	for _, canonical := range []string{"--capability-compact", "--capability-command=NAME", "--capability-category=NAME", "--robot-schema=TYPE"} {
		if !strings.Contains(discovery, canonical) {
			t.Errorf("Discovery section missing canonical discovery option %q: %q", canonical, discovery)
		}
	}
}

func TestGetDocs_ExamplesUseCanonicalPaneAndCapabilityDiscovery(t *testing.T) {
	output, err := GetDocs("examples")
	if err != nil {
		t.Fatalf("GetDocs(examples): %v", err)
	}
	if output.Content == nil {
		t.Fatal("examples content is nil")
	}

	var paneExample, discoveryExample *DocsExample
	for i := range output.Content.Examples {
		example := &output.Content.Examples[i]
		switch example.Name {
		case "send_to_panes":
			paneExample = example
		case "discover_one_command":
			discoveryExample = example
		}
	}
	if paneExample == nil || !strings.Contains(paneExample.Command, "--panes=0.1,%7") || !strings.Contains(paneExample.Notes, "N, W.P, or %N") {
		t.Fatalf("canonical pane example = %+v", paneExample)
	}
	if discoveryExample == nil || !strings.Contains(discoveryExample.Command, "--capability-command=send") ||
		!strings.Contains(discoveryExample.Command, "--capability-compact") {
		t.Fatalf("one-command capability example = %+v", discoveryExample)
	}
}

func TestGetDocs_ExitCodes(t *testing.T) {
	output, err := GetDocs("exit-codes")
	if err != nil {
		t.Fatalf("GetDocs(exit-codes) failed: %v", err)
	}

	if !output.Success {
		t.Errorf("expected Success=true, got false")
	}

	if output.Content == nil {
		t.Fatal("expected content, got nil")
	}

	if len(output.Content.ExitCodes) != 3 {
		t.Fatalf("exit code count = %d, want exactly 3", len(output.Content.ExitCodes))
	}

	want := []struct {
		code int
		name string
	}{
		{code: 0, name: "SUCCESS"},
		{code: 1, name: "ERROR"},
		{code: 2, name: "UNAVAILABLE"},
	}
	for i, expected := range want {
		got := output.Content.ExitCodes[i]
		if got.Code != expected.code || got.Name != expected.name {
			t.Errorf("exit_codes[%d] = (%d, %q), want (%d, %q)", i, got.Code, got.Name, expected.code, expected.name)
		}
	}
}

func TestGetDocs_InvalidTopic(t *testing.T) {
	output, err := GetDocs("invalid-topic")
	if err != nil {
		t.Fatalf("GetDocs(invalid-topic) should not return error, got: %v", err)
	}

	if output.Success {
		t.Errorf("expected Success=false for invalid topic")
	}

	if output.ErrorCode != ErrCodeInvalidFlag {
		t.Errorf("expected error code %s, got %s", ErrCodeInvalidFlag, output.ErrorCode)
	}

	if output.Content != nil {
		t.Errorf("expected nil content for invalid topic")
	}
}

func TestDocsOutputJSON(t *testing.T) {
	output, err := GetDocs("quickstart")
	if err != nil {
		t.Fatalf("GetDocs failed: %v", err)
	}

	// Verify JSON serialization roundtrip
	data, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("failed to marshal output: %v", err)
	}

	var decoded DocsOutput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal output: %v", err)
	}

	if decoded.Topic != output.Topic {
		t.Errorf("topic mismatch: got %q, want %q", decoded.Topic, output.Topic)
	}

	if decoded.SchemaVersion != output.SchemaVersion {
		t.Errorf("schema_version mismatch: got %q, want %q", decoded.SchemaVersion, output.SchemaVersion)
	}

	if decoded.Content == nil {
		t.Fatal("decoded content is nil")
	}

	if decoded.Content.Title != output.Content.Title {
		t.Errorf("content.title mismatch: got %q, want %q", decoded.Content.Title, output.Content.Title)
	}
}

func TestDocsExitCodeRecoverability(t *testing.T) {
	output, err := GetDocs("exit-codes")
	if err != nil {
		t.Fatalf("GetDocs(exit-codes) failed: %v", err)
	}

	for _, ec := range output.Content.ExitCodes {
		if !ec.Recoverable {
			t.Errorf("exit code %d (%s) should direct consumers to inspect the structured error and retry or adapt", ec.Code, ec.Name)
		}
	}
}
