package pipeline

// MailSendStep describes a first-class MCP Agent Mail send operation.
type MailSendStep struct {
	ProjectKey  string       `yaml:"project_key,omitempty" toml:"project_key,omitempty" json:"project_key,omitempty"`
	AgentName   string       `yaml:"agent_name,omitempty" toml:"agent_name,omitempty" json:"agent_name,omitempty"`
	To          StringOrList `yaml:"to,omitempty" toml:"to,omitempty" json:"to,omitempty"`
	Subject     string       `yaml:"subject,omitempty" toml:"subject,omitempty" json:"subject,omitempty"`
	Body        string       `yaml:"body,omitempty" toml:"body,omitempty" json:"body,omitempty"`
	ThreadID    string       `yaml:"thread_id,omitempty" toml:"thread_id,omitempty" json:"thread_id,omitempty"`
	AckRequired bool         `yaml:"ack_required,omitempty" toml:"ack_required,omitempty" json:"ack_required,omitempty"`
}

// FileReservationPathsStep describes an MCP Agent Mail file reservation
// acquisition operation.
type FileReservationPathsStep struct {
	ProjectKey string       `yaml:"project_key,omitempty" toml:"project_key,omitempty" json:"project_key,omitempty"`
	AgentName  string       `yaml:"agent_name,omitempty" toml:"agent_name,omitempty" json:"agent_name,omitempty"`
	Paths      StringOrList `yaml:"paths,omitempty" toml:"paths,omitempty" json:"paths,omitempty"`
	TTLSeconds int          `yaml:"ttl_seconds,omitempty" toml:"ttl_seconds,omitempty" json:"ttl_seconds,omitempty"`
	Exclusive  bool         `yaml:"exclusive,omitempty" toml:"exclusive,omitempty" json:"exclusive,omitempty"`
	Reason     string       `yaml:"reason,omitempty" toml:"reason,omitempty" json:"reason,omitempty"`
}

// MailInboxCheckStep describes a first-class MCP Agent Mail inbox polling
// operation.
type MailInboxCheckStep struct {
	ProjectKey    string `yaml:"project_key,omitempty" toml:"project_key,omitempty" json:"project_key,omitempty"`
	AgentName     string `yaml:"agent_name,omitempty" toml:"agent_name,omitempty" json:"agent_name,omitempty"`
	UntilAckCount int    `yaml:"until_ack_count,omitempty" toml:"until_ack_count,omitempty" json:"until_ack_count,omitempty"`
}

// FileReservationReleaseStep describes an MCP Agent Mail file reservation
// release operation.
type FileReservationReleaseStep struct {
	ProjectKey string       `yaml:"project_key,omitempty" toml:"project_key,omitempty" json:"project_key,omitempty"`
	AgentName  string       `yaml:"agent_name,omitempty" toml:"agent_name,omitempty" json:"agent_name,omitempty"`
	Paths      StringOrList `yaml:"paths,omitempty" toml:"paths,omitempty" json:"paths,omitempty"`
}

func (s *Step) mailStepKindNames() []string {
	if s == nil {
		return nil
	}

	var names []string
	if s.MailSend != nil {
		names = append(names, "mail_send")
	}
	if s.FileReservationPaths != nil {
		names = append(names, "file_reservation_paths")
	}
	if s.MailInboxCheck != nil {
		names = append(names, "mail_inbox_check")
	}
	if s.FileReservationRelease != nil {
		names = append(names, "file_reservation_release")
	}
	return names
}
