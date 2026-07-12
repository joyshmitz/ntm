// Package robot provides machine-readable output for AI agents.
// giil_fetch.go implements the --robot-giil-fetch command.
package robot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Dicklesworthstone/ntm/internal/tools"
)

// GIILFetchOutput represents the output for --robot-giil-fetch.
type GIILFetchOutput struct {
	RobotResponse
	SourceURL string `json:"source_url"`
	Path      string `json:"path,omitempty"`
	Filename  string `json:"filename,omitempty"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	Platform  string `json:"platform,omitempty"`
}

// GetGIILFetch downloads an image via giil and returns structured output.
// This function returns the data struct directly, enabling CLI/REST parity.
func GetGIILFetch(url string) (*GIILFetchOutput, error) {
	trimmed := strings.TrimSpace(url)
	output := &GIILFetchOutput{
		RobotResponse: NewRobotResponse(true),
		SourceURL:     trimmed,
	}

	if trimmed == "" {
		output.RobotResponse = NewErrorResponse(
			fmt.Errorf("missing url"),
			ErrCodeInvalidFlag,
			"Provide a share URL with --robot-giil-fetch",
		)
		return output, nil
	}

	adapter := tools.NewGIILAdapter()
	if _, installed := adapter.Detect(); !installed {
		output.RobotResponse = NewErrorResponse(
			fmt.Errorf("giil not installed"),
			ErrCodeDependencyMissing,
			"Install giil: curl -fsSL https://raw.githubusercontent.com/Dicklesworthstone/giil/main/install.sh?v=3.0.0 | bash",
		)
		return output, nil
	}

	meta, err := adapter.Download(context.Background(), trimmed, "")
	if err != nil {
		code := ErrCodeInternalError
		hint := "Run giil manually to diagnose the failure"
		if errors.Is(err, tools.ErrTimeout) {
			code = ErrCodeTimeout
			hint = "GIIL timed out; try again or shorten the URL scope"
		}
		output.RobotResponse = NewErrorResponse(err, code, hint)
		return output, nil
	}

	path := firstNonEmpty(meta.OutputPath, meta.Path)
	if path == "" {
		output.RobotResponse = NewErrorResponse(
			fmt.Errorf("giil output missing path"),
			ErrCodeInternalError,
			"Run giil with --json to inspect output",
		)
		return output, nil
	}

	filename := meta.Filename
	if filename == "" {
		filename = filepath.Base(path)
	}

	size := meta.Size
	if size == 0 {
		if info, statErr := os.Stat(path); statErr == nil {
			size = info.Size()
		}
	}

	output.Path = path
	output.Filename = filename
	output.SizeBytes = size
	output.Width = meta.Width
	output.Height = meta.Height
	output.Platform = meta.Platform

	return output, nil
}

// PrintGIILFetch outputs the GIIL fetch response as JSON/TOON.
// This is a thin wrapper around GetGIILFetch() for CLI output.
func PrintGIILFetch(url string) error {
	output, err := GetGIILFetch(url)
	if err != nil {
		return err
	}
	return encodeTerminalRobotOutput(output, output.RobotResponse, "robot giil fetch failed")
}
