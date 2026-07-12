// Package robot provides machine-readable output for AI agents.
// tools.go provides the --robot-tools command for tool inventory and health.
package robot

import (
	"context"
	"sort"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/tools"
)

// ToolsOutput represents the output for --robot-tools
type ToolsOutput struct {
	RobotResponse
	Tools        []ToolInfoOutput    `json:"tools"`
	HealthReport *tools.HealthReport `json:"health_report"`
}

// ToolInfoOutput represents a single tool's info in robot output
type ToolInfoOutput struct {
	Name         string            `json:"name"`
	Installed    bool              `json:"installed"`
	Version      string            `json:"version,omitempty"`
	Path         string            `json:"path,omitempty"`
	Capabilities []string          `json:"capabilities"`
	Health       *ToolHealthOutput `json:"health"`
	Required     bool              `json:"required,omitempty"`
}

// ToolHealthOutput represents tool health in robot output
type ToolHealthOutput struct {
	Healthy     bool   `json:"healthy"`
	Message     string `json:"message,omitempty"`
	Error       string `json:"error,omitempty"`
	LatencyMs   int64  `json:"latency_ms,omitempty"`
	LastChecked string `json:"last_checked"`
}

// RequiredTools lists tools that are required for NTM operation
var RequiredTools = map[tools.ToolName]bool{
	tools.ToolBV: true, // bv is required for triage
}

// GetTools collects tool inventory and health.
// This function returns the data struct directly, enabling CLI/REST parity.
func GetTools(ctx context.Context) (*ToolsOutput, error) {
	// Get all tool info from registry
	allInfo := tools.GetAllInfo(ctx)

	// Convert to output format
	toolOutputs := make([]ToolInfoOutput, 0, len(allInfo))
	for _, info := range allInfo {
		if info == nil {
			continue
		}

		// Convert capabilities to strings
		caps := make([]string, len(info.Capabilities))
		for i, c := range info.Capabilities {
			caps[i] = string(c)
		}

		// Convert health
		healthOutput := &ToolHealthOutput{
			Healthy:     info.Health.Healthy,
			Message:     info.Health.Message,
			Error:       info.Health.Error,
			LatencyMs:   info.Health.Latency.Milliseconds(),
			LastChecked: FormatTimestamp(info.Health.LastChecked),
		}

		toolOutput := ToolInfoOutput{
			Name:         string(info.Name),
			Installed:    info.Installed,
			Version:      info.Version.String(),
			Path:         info.Path,
			Capabilities: caps,
			Health:       healthOutput,
			Required:     RequiredTools[info.Name],
		}

		toolOutputs = append(toolOutputs, toolOutput)
	}

	// Sort by name for stable output
	sort.Slice(toolOutputs, func(i, j int) bool {
		return toolOutputs[i].Name < toolOutputs[j].Name
	})

	// Get health report summary
	healthReport := tools.GetHealthReport(ctx)

	return &ToolsOutput{
		RobotResponse: NewRobotResponse(true),
		Tools:         toolOutputs,
		HealthReport:  healthReport,
	}, nil
}

// PrintTools outputs tool inventory and health as JSON.
// This is a thin wrapper around GetTools() for CLI output.
func PrintTools() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, err := GetTools(ctx)
	if err != nil {
		return err
	}
	return encodeTerminalRobotOutput(output, output.RobotResponse, "robot tools failed")
}

// GetToolsSummary returns a lightweight tools summary for inclusion in snapshots
func GetToolsSummary(ctx context.Context) []ToolInfoOutput {
	allInfo := tools.GetAllInfo(ctx)

	toolOutputs := make([]ToolInfoOutput, 0, len(allInfo))
	for _, info := range allInfo {
		if info == nil {
			continue
		}

		// Convert capabilities to strings
		caps := make([]string, len(info.Capabilities))
		for i, c := range info.Capabilities {
			caps[i] = string(c)
		}

		// Convert health
		healthOutput := &ToolHealthOutput{
			Healthy:     info.Health.Healthy,
			Message:     info.Health.Message,
			Error:       info.Health.Error,
			LatencyMs:   info.Health.Latency.Milliseconds(),
			LastChecked: FormatTimestamp(info.Health.LastChecked),
		}

		toolOutput := ToolInfoOutput{
			Name:         string(info.Name),
			Installed:    info.Installed,
			Version:      info.Version.String(),
			Path:         info.Path,
			Capabilities: caps,
			Health:       healthOutput,
			Required:     RequiredTools[info.Name],
		}

		toolOutputs = append(toolOutputs, toolOutput)
	}

	// Sort by name for stable output
	sort.Slice(toolOutputs, func(i, j int) bool {
		return toolOutputs[i].Name < toolOutputs[j].Name
	})

	return toolOutputs
}
