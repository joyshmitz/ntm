package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// BeadQueryStep describes a first-class br query operation. It replaces
// shell-piped br|jq command steps when a pipeline needs structured bead data.
type BeadQueryStep struct {
	Label    StringOrList `yaml:"label,omitempty" toml:"label,omitempty" json:"label,omitempty"`
	Status   string       `yaml:"status,omitempty" toml:"status,omitempty" json:"status,omitempty"`
	State    string       `yaml:"state,omitempty" toml:"state,omitempty" json:"state,omitempty"`
	Type     string       `yaml:"type,omitempty" toml:"type,omitempty" json:"type,omitempty"`
	Priority string       `yaml:"priority,omitempty" toml:"priority,omitempty" json:"priority,omitempty"`
	Assignee string       `yaml:"assignee,omitempty" toml:"assignee,omitempty" json:"assignee,omitempty"`
	Filter   string       `yaml:"filter,omitempty" toml:"filter,omitempty" json:"filter,omitempty"`
}

// BeadRecord is the typed subset of `br list --json` issue fields that
// pipeline authors commonly consume downstream.
type BeadRecord struct {
	ID          string   `json:"id"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	Status      string   `json:"status,omitempty"`
	State       string   `json:"state,omitempty"`
	Priority    int      `json:"priority,omitempty"`
	IssueType   string   `json:"issue_type,omitempty"`
	Assignee    string   `json:"assignee,omitempty"`
	CreatedAt   string   `json:"created_at,omitempty"`
	UpdatedAt   string   `json:"updated_at,omitempty"`
	ClosedAt    string   `json:"closed_at,omitempty"`
	SourceRepo  string   `json:"source_repo,omitempty"`
}

func (e *Executor) executeBeadQuery(ctx context.Context, step *Step, workflow *Workflow) StepResult {
	result := StepResult{
		StepID:    step.ID,
		Status:    StatusRunning,
		StartedAt: time.Now(),
		AgentType: "bead_query",
	}

	args := step.BeadQuery.brListArgs()
	if e.config.DryRun {
		result.Status = StatusCompleted
		result.Output = dryRunOutput(step, "Would run br "+strings.Join(args, " "))
		result.FinishedAt = time.Now()
		return result
	}

	slog.Info("bead_query step starting",
		"run_id", e.state.RunID,
		"workflow", workflow.Name,
		"step_id", step.ID,
		"agent_type", "bead_query",
	)

	resolver := IterationSourceResolver{
		ProjectDir: e.config.ProjectDir,
		RunBr:      e.config.BeadQueryRunBr,
	}
	out, err := resolver.runBr(ctx, args)
	if err != nil {
		result.Status = StatusFailed
		result.Error = stepRuntimeError(step, "bead_query", "bead_query",
			fmt.Sprintf("br list failed: %v", err),
			"verify br is installed and project_dir points at a Beads repo",
			err.Error())
		result.FinishedAt = time.Now()
		return result
	}

	records, err := parseBeadQueryRecords(out)
	if err != nil {
		result.Status = StatusFailed
		result.Error = stepRuntimeError(step, "bead_query", "bead_query",
			fmt.Sprintf("failed to parse br output: %v", err),
			"br list --json should emit an object with an issues array",
			err.Error())
		result.FinishedAt = time.Now()
		return result
	}
	records, err = filterBeadRecords(records, step.BeadQuery.Filter)
	if err != nil {
		result.Status = StatusFailed
		result.Error = stepRuntimeError(step, "bead_query", "bead_query",
			fmt.Sprintf("filter failed: %v", err),
			"use simple field==value or field!=value clauses joined by && or ||",
			err.Error())
		result.FinishedAt = time.Now()
		return result
	}

	data, err := json.Marshal(records)
	if err != nil {
		result.Status = StatusFailed
		result.Error = stepRuntimeError(step, "bead_query", "bead_query",
			fmt.Sprintf("failed to encode bead records: %v", err),
			"check for unsupported values in br output",
			err.Error())
		result.FinishedAt = time.Now()
		return result
	}

	result.Output = string(data)
	result.ParsedData = records
	result.Status = StatusCompleted
	result.FinishedAt = time.Now()

	slog.Info("bead_query step completed",
		"run_id", e.state.RunID,
		"workflow", workflow.Name,
		"step_id", step.ID,
		"agent_type", "bead_query",
		"records", len(records),
	)
	return result
}

func (q *BeadQueryStep) brListArgs() []string {
	args := []string{"list", "--json"}
	if q == nil {
		return args
	}
	for _, label := range q.Label {
		label = strings.TrimSpace(label)
		if label != "" {
			args = append(args, "--label", label)
		}
	}
	if q.Status != "" {
		args = append(args, "--status", q.Status)
	} else if q.State != "" {
		args = append(args, "--status", q.State)
	}
	if q.Type != "" {
		args = append(args, "--type", q.Type)
	}
	if q.Priority != "" {
		args = append(args, "--priority", q.Priority)
	}
	if q.Assignee != "" {
		args = append(args, "--assignee", q.Assignee)
	}
	return args
}

func parseBeadQueryRecords(out []byte) ([]BeadRecord, error) {
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return []BeadRecord{}, nil
	}
	var doc struct {
		Issues []BeadRecord `json:"issues"`
	}
	if err := json.Unmarshal([]byte(trimmed), &doc); err != nil {
		return nil, err
	}
	if doc.Issues == nil {
		return []BeadRecord{}, nil
	}
	return doc.Issues, nil
}

func filterBeadRecords(records []BeadRecord, filter string) ([]BeadRecord, error) {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return records, nil
	}
	out := make([]BeadRecord, 0, len(records))
	for _, record := range records {
		ok, err := matchBeadFilter(record, filter)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, record)
		}
	}
	return out, nil
}

func matchBeadFilter(record BeadRecord, expr string) (bool, error) {
	orParts := strings.Split(expr, "||")
	for _, orPart := range orParts {
		andParts := strings.Split(orPart, "&&")
		all := true
		for _, rawClause := range andParts {
			ok, err := matchBeadFilterClause(record, rawClause)
			if err != nil {
				return false, err
			}
			if !ok {
				all = false
				break
			}
		}
		if all {
			return true, nil
		}
	}
	return false, nil
}

func matchBeadFilterClause(record BeadRecord, rawClause string) (bool, error) {
	clause := strings.TrimSpace(rawClause)
	if clause == "" {
		return false, fmt.Errorf("empty filter clause")
	}

	op := "=="
	parts := strings.SplitN(clause, "==", 2)
	if len(parts) != 2 {
		op = "!="
		parts = strings.SplitN(clause, "!=", 2)
	}
	if len(parts) != 2 {
		return false, fmt.Errorf("unsupported filter clause %q", clause)
	}

	field := strings.TrimSpace(parts[0])
	want := trimFilterValue(parts[1])
	got, ok := beadRecordField(record, field)
	if !ok {
		return false, fmt.Errorf("unknown bead field %q", field)
	}

	matches := got == want
	if field == "label" || field == "labels" {
		matches = false
		for _, label := range record.Labels {
			if label == want {
				matches = true
				break
			}
		}
	}
	if op == "!=" {
		return !matches, nil
	}
	return matches, nil
}

func trimFilterValue(raw string) string {
	return strings.Trim(strings.TrimSpace(raw), `"'`)
}

func beadRecordField(record BeadRecord, field string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "id":
		return record.ID, true
	case "title":
		return record.Title, true
	case "description":
		return record.Description, true
	case "status":
		return record.Status, true
	case "state":
		if record.State != "" {
			return record.State, true
		}
		return record.Status, true
	case "priority":
		return strconv.Itoa(record.Priority), true
	case "type", "issue_type":
		return record.IssueType, true
	case "assignee":
		return record.Assignee, true
	case "label", "labels":
		return strings.Join(record.Labels, ","), true
	default:
		return "", false
	}
}
