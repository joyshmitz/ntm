package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// BVAdapter provides integration with the beads_viewer (bv) tool
type BVAdapter struct {
	*BaseAdapter
}

// NewBVAdapter creates a new BV adapter
func NewBVAdapter() *BVAdapter {
	return &BVAdapter{
		BaseAdapter: NewBaseAdapter(ToolBV, "bv"),
	}
}

// Detect checks if bv is installed
func (a *BVAdapter) Detect() (string, bool) {
	path, err := exec.LookPath(a.BinaryName())
	if err != nil {
		return "", false
	}
	return path, true
}

// Version returns the installed bv version
func (a *BVAdapter) Version(ctx context.Context) (Version, error) {
	ctx, cancel := context.WithTimeout(ctx, a.Timeout())
	defer cancel()

	cmd := exec.CommandContext(ctx, a.BinaryName(), "--version")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return Version{}, fmt.Errorf("failed to get bv version: %w", err)
	}

	return parseVersion(stdout.String())
}

// Capabilities returns the list of bv capabilities
func (a *BVAdapter) Capabilities(ctx context.Context) ([]Capability, error) {
	caps := []Capability{CapRobotMode}

	// Check for specific robot mode commands
	version, err := a.Version(ctx)
	if err != nil {
		return caps, nil
	}

	// bv 0.30+ has robot-triage
	if version.AtLeast(Version{Major: 0, Minor: 30, Patch: 0}) {
		caps = append(caps, "robot_triage", "robot_plan", "robot_insights")
	}

	return caps, nil
}

// Health checks if bv is functioning correctly
func (a *BVAdapter) Health(ctx context.Context) (*HealthStatus, error) {
	start := time.Now()

	path, installed := a.Detect()
	if !installed {
		return &HealthStatus{
			Healthy:     false,
			Message:     "bv not installed",
			LastChecked: time.Now(),
		}, nil
	}

	// Try to get version as a health check
	_, err := a.Version(ctx)
	latency := time.Since(start)

	if err != nil {
		return &HealthStatus{
			Healthy:     false,
			Message:     fmt.Sprintf("bv at %s not responding", path),
			Error:       err.Error(),
			LastChecked: time.Now(),
			Latency:     latency,
		}, nil
	}

	return &HealthStatus{
		Healthy:     true,
		Message:     "bv is healthy",
		LastChecked: time.Now(),
		Latency:     latency,
	}, nil
}

// HasCapability checks if bv has a specific capability
func (a *BVAdapter) HasCapability(ctx context.Context, cap Capability) bool {
	caps, err := a.Capabilities(ctx)
	if err != nil {
		return false
	}
	for _, c := range caps {
		if c == cap {
			return true
		}
	}
	return false
}

// Info returns complete bv tool information
func (a *BVAdapter) Info(ctx context.Context) (*ToolInfo, error) {
	return a.BaseAdapter.Info(ctx, a)
}

// BV-specific methods

// GetTriage returns the robot-triage output
func (a *BVAdapter) GetTriage(ctx context.Context, dir string) (json.RawMessage, error) {
	return a.runRobotCommand(ctx, dir, "--robot-triage")
}

// GetPlan returns the robot-plan output
func (a *BVAdapter) GetPlan(ctx context.Context, dir string) (json.RawMessage, error) {
	return a.runRobotCommand(ctx, dir, "--robot-plan")
}

// GetInsights returns the robot-insights output
func (a *BVAdapter) GetInsights(ctx context.Context, dir string) (json.RawMessage, error) {
	return a.runRobotCommand(ctx, dir, "--robot-insights")
}

// GetNext returns the robot-next output (single top pick)
func (a *BVAdapter) GetNext(ctx context.Context, dir string) (json.RawMessage, error) {
	return a.runRobotCommand(ctx, dir, "--robot-next")
}

// runRobotCommand executes a bv robot command and returns raw JSON
func (a *BVAdapter) runRobotCommand(ctx context.Context, dir string, args ...string) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, a.Timeout())
	defer cancel()

	cmd := exec.CommandContext(ctx, a.BinaryName(), args...)
	if dir != "" {
		cmd.Dir = dir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, ErrTimeout
		}
		return nil, fmt.Errorf("bv %s failed: %w: %s", strings.Join(args, " "), err, stderr.String())
	}

	// Validate JSON
	output := stdout.Bytes()
	if !json.Valid(output) {
		return nil, fmt.Errorf("%w: invalid JSON from bv", ErrSchemaValidation)
	}

	return output, nil
}

// parseVersion extracts version from --version output
func parseVersion(output string) (Version, error) {
	// Match patterns like "bv 0.31.0" or "0.31.0"
	re := regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)
	matches := re.FindStringSubmatch(output)
	if len(matches) < 4 {
		return Version{Raw: strings.TrimSpace(output)}, nil
	}

	var major, minor, patch int
	fmt.Sscanf(matches[1], "%d", &major)
	fmt.Sscanf(matches[2], "%d", &minor)
	fmt.Sscanf(matches[3], "%d", &patch)

	return Version{
		Major: major,
		Minor: minor,
		Patch: patch,
		Raw:   strings.TrimSpace(output),
	}, nil
}
