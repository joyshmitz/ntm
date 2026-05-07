package pipeline

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestMailSteps_ParseYAMLAndValidate(t *testing.T) {
	content := `
schema_version: "2.0"
name: mail-steps
steps:
  - id: notify
    mail_send:
      project_key: /data/projects/ntm
      agent_name: TealCrane
      to: [SageFern, OrangeFalcon]
      subject: "[bd-b5l8d] status"
      body: "Done"
      thread_id: bd-b5l8d
      ack_required: true
  - id: reserve
    file_reservation_paths:
      project_key: /data/projects/ntm
      agent_name: TealCrane
      paths: [internal/pipeline/schema.go, internal/pipeline/mail_steps.go]
      ttl_seconds: 3600
      exclusive: true
      reason: bd-b5l8d
  - id: inbox
    mail_inbox_check:
      project_key: /data/projects/ntm
      agent_name: TealCrane
      until_ack_count: 2
  - id: release
    file_reservation_release:
      project_key: /data/projects/ntm
      agent_name: TealCrane
      paths: internal/pipeline/schema.go
`

	workflow, err := ParseString(content, "yaml")
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}
	if result := Validate(workflow); !result.Valid {
		t.Fatalf("Validate() failed: %+v", result.Errors)
	}

	send := workflow.Steps[0].MailSend
	if send == nil {
		t.Fatal("MailSend = nil")
	}
	if !reflect.DeepEqual(send.To, StringOrList{"SageFern", "OrangeFalcon"}) {
		t.Fatalf("MailSend.To = %#v, want two recipients", send.To)
	}
	if !send.AckRequired || send.ThreadID != "bd-b5l8d" {
		t.Fatalf("MailSend metadata = %#v", send)
	}

	reserve := workflow.Steps[1].FileReservationPaths
	if reserve == nil {
		t.Fatal("FileReservationPaths = nil")
	}
	if reserve.TTLSeconds != 3600 || !reserve.Exclusive || reserve.Reason != "bd-b5l8d" {
		t.Fatalf("FileReservationPaths = %#v", reserve)
	}

	inbox := workflow.Steps[2].MailInboxCheck
	if inbox == nil || inbox.UntilAckCount != 2 {
		t.Fatalf("MailInboxCheck = %#v, want until_ack_count=2", inbox)
	}

	release := workflow.Steps[3].FileReservationRelease
	if release == nil {
		t.Fatal("FileReservationRelease = nil")
	}
	if !reflect.DeepEqual(release.Paths, StringOrList{"internal/pipeline/schema.go"}) {
		t.Fatalf("FileReservationRelease.Paths = %#v, want scalar path as one-item list", release.Paths)
	}
}

func TestMailSteps_ParseTOMLKnownFields(t *testing.T) {
	content := `
schema_version = "2.0"
name = "mail-steps-toml"

[[steps]]
id = "notify"

[steps.mail_send]
project_key = "/data/projects/ntm"
agent_name = "TealCrane"
to = ["SageFern"]
subject = "[bd-b5l8d] status"
body = "Done"
thread_id = "bd-b5l8d"
ack_required = true
`

	workflow, err := ParseString(content, "toml")
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}
	if result := Validate(workflow); !result.Valid {
		t.Fatalf("Validate() failed: %+v", result.Errors)
	}
	if got := workflow.Steps[0].MailSend.To; !reflect.DeepEqual(got, StringOrList{"SageFern"}) {
		t.Fatalf("MailSend.To = %#v, want SageFern", got)
	}
}

func TestMailSteps_JSONRoundTrip(t *testing.T) {
	step := Step{
		ID: "reserve",
		FileReservationPaths: &FileReservationPathsStep{
			ProjectKey: "/data/projects/ntm",
			AgentName:  "TealCrane",
			Paths:      StringOrList{"internal/pipeline/schema.go"},
			TTLSeconds: 3600,
			Exclusive:  true,
			Reason:     "bd-b5l8d",
		},
	}

	data, err := json.Marshal(step)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var got Step
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v\nJSON:\n%s", err, data)
	}
	if !reflect.DeepEqual(got, step) {
		t.Fatalf("JSON round trip mismatch\nwant: %#v\n got: %#v\nJSON:\n%s", step, got, data)
	}
}

func TestMailSteps_ValidationConflicts(t *testing.T) {
	tests := []struct {
		name    string
		step    Step
		wantErr string
	}{
		{
			name: "mail send with command",
			step: Step{
				ID:       "bad",
				Command:  "echo should-not-run",
				MailSend: validMailSendStep(),
			},
			wantErr: "cannot combine Agent Mail step kind",
		},
		{
			name: "two mail kinds",
			step: Step{
				ID:             "bad",
				MailSend:       validMailSendStep(),
				MailInboxCheck: &MailInboxCheckStep{ProjectKey: "/data/projects/ntm", AgentName: "TealCrane"},
			},
			wantErr: "can only use one Agent Mail step kind",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Validate(&Workflow{
				SchemaVersion: SchemaVersion,
				Name:          "mail-step-conflict",
				Steps:         []Step{tt.step},
			})
			if result.Valid {
				t.Fatal("Validate() succeeded, want conflict")
			}
			for _, err := range result.Errors {
				if strings.Contains(err.Message, tt.wantErr) {
					return
				}
			}
			t.Fatalf("Validate() errors = %+v, want message containing %q", result.Errors, tt.wantErr)
		})
	}
}

func validMailSendStep() *MailSendStep {
	return &MailSendStep{
		ProjectKey: "/data/projects/ntm",
		AgentName:  "TealCrane",
		To:         StringOrList{"SageFern"},
		Subject:    "[bd-b5l8d] status",
		Body:       "Done",
		ThreadID:   "bd-b5l8d",
	}
}
