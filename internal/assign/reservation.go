// Package assign provides bead-to-agent assignment functionality.
package assign

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/Dicklesworthstone/ntm/internal/agentmail"
)

// FileReservationManager handles file path reservations for bead assignments.
type FileReservationManager struct {
	client     *agentmail.Client
	projectKey string
	ttlSeconds int
}

// FileReservationResult contains the result of a file reservation attempt.
type FileReservationResult struct {
	BeadID         string                          `json:"bead_id"`
	AgentName      string                          `json:"agent_name"`
	RequestedPaths []string                        `json:"requested_paths"`
	GrantedPaths   []string                        `json:"granted_paths"`
	Conflicts      []agentmail.ReservationConflict `json:"conflicts,omitempty"`
	ReservationIDs []int                           `json:"reservation_ids"`
	Success        bool                            `json:"success"`
	Error          string                          `json:"error,omitempty"`
}

// NewFileReservationManager creates a new file reservation manager.
func NewFileReservationManager(client *agentmail.Client, projectKey string) *FileReservationManager {
	return &FileReservationManager{
		client:     client,
		projectKey: projectKey,
		ttlSeconds: 3600, // Default 1 hour
	}
}

// SetTTL sets the TTL for reservations in seconds.
func (m *FileReservationManager) SetTTL(seconds int) {
	if seconds > 0 {
		m.ttlSeconds = seconds
	}
}

// Pre-compiled regexes for file path extraction (avoid recompilation per call).
var (
	filePathRegex = regexp.MustCompile(`(?m)(?:^|\s|[(\["'])([a-zA-Z0-9_./-]+(?:\.[a-zA-Z0-9]+)+)(?:\:\d+(?::\d+)?)?(?:\s|[)\]"']|$)`)
	dotfileRegex  = regexp.MustCompile(`(?m)(?:^|\s|[(\["'])(\.[a-zA-Z][a-zA-Z0-9_]*(?:\.[a-zA-Z0-9]+)*)(?:\s|[)\]"']|$)`)
	dirPathRegex  = regexp.MustCompile(`(?m)(?:^|\s|[(\["'])([a-zA-Z0-9_-]+(?:/[a-zA-Z0-9_-]+)+)(?:\s|[)\]"']|$)`)
	globRegex     = regexp.MustCompile(`(?m)(?:^|\s|[(\["'])([a-zA-Z0-9_./*-]+\*[a-zA-Z0-9_./*-]*)(?:\s|[)\]"']|$)`)
	// isValidPath regexes - pre-compiled for performance
	versionLikeRegex = regexp.MustCompile(`^\d+\.\d+`)
	validExtRegex    = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]{0,9}$`)
)

// ExtractFilePaths extracts file paths from a bead title and description.
// Patterns detected:
// - Explicit paths: src/api/handler.go, lib/utils.ts
// - Glob patterns: internal/**/*.go, *.json
// - Package references: internal/cli, pkg/api
func ExtractFilePaths(title, description string) []string {
	combined := title + "\n" + description

	var paths []string
	seen := make(map[string]bool)

	// Extract file paths first
	for _, match := range filePathRegex.FindAllStringSubmatch(combined, -1) {
		if len(match) > 1 && isValidPath(match[1]) && !seen[match[1]] {
			paths = append(paths, match[1])
			seen[match[1]] = true
		}
	}

	// Extract dotfiles
	for _, match := range dotfileRegex.FindAllStringSubmatch(combined, -1) {
		if len(match) > 1 && !seen[match[1]] {
			paths = append(paths, match[1])
			seen[match[1]] = true
		}
	}

	// Extract directory paths
	for _, match := range dirPathRegex.FindAllStringSubmatch(combined, -1) {
		if len(match) > 1 {
			dir := match[1]
			// Trim trailing slash if present
			dir = strings.TrimSuffix(dir, "/")
			if isValidPath(dir) && !seen[dir] {
				glob := dir + "/**/*"
				if !seen[glob] {
					paths = append(paths, glob)
					seen[glob] = true
				}
				seen[dir] = true
			}
		}
	}

	// Extract glob patterns
	for _, match := range globRegex.FindAllStringSubmatch(combined, -1) {
		if len(match) > 1 && !seen[match[1]] {
			paths = append(paths, match[1])
			seen[match[1]] = true
		}
	}

	return paths
}

// isValidPath checks if a path looks valid (not a URL, version, etc.)
func isValidPath(path string) bool {
	// Exclude URLs and domain names
	if strings.Contains(path, "://") || strings.HasPrefix(path, "www.") {
		return false
	}

	// Exclude version-like strings (e.g., 1.2.3, v1.2.3)
	if versionLikeRegex.MatchString(path) || strings.HasPrefix(strings.ToLower(path), "v") && versionLikeRegex.MatchString(path[1:]) {
		return false
	}

	// Exclude common non-path patterns and domain-like patterns
	excludePatterns := []string{
		"e.g.", "i.e.", "etc.", "fig.", "ref.", "http", "https", ".com", ".net", ".org", ".io", ".gov", ".edu",
	}
	lowerPath := strings.ToLower(path)
	for _, pattern := range excludePatterns {
		if lowerPath == pattern || strings.HasSuffix(lowerPath, pattern) || strings.HasPrefix(lowerPath, pattern) && strings.Contains(pattern, "http") {
			return false
		}
	}

	// Exclude obvious non-paths (e.g. sentences ending in dot)
	if strings.HasSuffix(path, ".") || strings.HasPrefix(path, ".") && !strings.Contains(path, "/") && len(path) < 3 {
		// Matches "." or ".." but allows ".github"
		if path == "." || path == ".." {
			return false
		}
	}

	// Check for file extension (must have content before and after dot)
	if strings.Contains(path, ".") {
		parts := strings.Split(path, ".")
		if len(parts) >= 2 {
			// Last part should be a valid extension with at least one letter
			// This excludes things like "fig.1" while allowing "config.json"
			ext := parts[len(parts)-1]
			if validExtRegex.MatchString(ext) {
				// First part should have content or be a dotfile
				if len(parts[0]) > 0 || strings.Contains(path, "/") {
					// Additional check: exclude common top-level domains that look like extensions
					tlds := []string{"com", "net", "org", "io", "gov", "edu", "me", "ai", "app", "dev"}
					isTLD := false
					for _, tld := range tlds {
						if ext == tld && !strings.Contains(path, "/") {
							isTLD = true
							break
						}
					}
					if !isTLD {
						return true
					}
				}
			}
		}
	}

	// Paths with slashes are valid
	return strings.Contains(path, "/") && !strings.Contains(path, " ")
}

// ReserveForBead reserves file paths mentioned in a bead for an agent.
func (m *FileReservationManager) ReserveForBead(ctx context.Context, beadID, beadTitle, beadDescription, agentName string) (*FileReservationResult, error) {
	result := &FileReservationResult{
		BeadID:    beadID,
		AgentName: agentName,
		Success:   false,
	}

	// Extract file paths from bead
	paths := ExtractFilePaths(beadTitle, beadDescription)
	result.RequestedPaths = paths

	if len(paths) == 0 {
		result.Success = true
		return result, nil
	}

	if m.client == nil {
		result.Error = "agent mail client not configured"
		return result, nil
	}

	// Reserve paths via Agent Mail
	reservationResult, err := m.client.ReservePaths(ctx, agentmail.FileReservationOptions{
		ProjectKey: m.projectKey,
		AgentName:  agentName,
		Paths:      paths,
		TTLSeconds: m.ttlSeconds,
		Exclusive:  true,
		Reason:     fmt.Sprintf("bead assignment: %s", beadID),
	})

	if err != nil {
		// Check if it's a conflict error with partial results
		if reservationResult != nil {
			result.Conflicts = reservationResult.Conflicts
			for _, granted := range reservationResult.Granted {
				result.GrantedPaths = append(result.GrantedPaths, granted.PathPattern)
				result.ReservationIDs = append(result.ReservationIDs, granted.ID)
			}
			result.Error = fmt.Sprintf("conflicts detected: %v", err)
			return result, nil
		}
		result.Error = err.Error()
		return result, err
	}

	// Process successful reservations
	for _, granted := range reservationResult.Granted {
		result.GrantedPaths = append(result.GrantedPaths, granted.PathPattern)
		result.ReservationIDs = append(result.ReservationIDs, granted.ID)
	}
	result.Success = true

	return result, nil
}

// ReleaseForBead releases all reservations held by an agent for a bead.
func (m *FileReservationManager) ReleaseForBead(ctx context.Context, agentName string, reservationIDs []int) error {
	if m.client == nil || len(reservationIDs) == 0 {
		return nil
	}

	releaseResult, err := m.client.ReleaseReservations(ctx, m.projectKey, agentName, nil, reservationIDs)
	if err != nil {
		return err
	}
	if releaseResult == nil {
		return fmt.Errorf("release returned no result")
	}
	if releaseResult.Released < len(reservationIDs) {
		return fmt.Errorf("released %d of %d reservations", releaseResult.Released, len(reservationIDs))
	}
	return nil
}

// ReleaseByPaths releases reservations by path patterns.
func (m *FileReservationManager) ReleaseByPaths(ctx context.Context, agentName string, paths []string) error {
	if m.client == nil || len(paths) == 0 {
		return nil
	}

	releaseResult, err := m.client.ReleaseReservations(ctx, m.projectKey, agentName, paths, nil)
	if err != nil {
		return err
	}
	if releaseResult == nil {
		return fmt.Errorf("release returned no result")
	}
	if releaseResult.Released == 0 {
		return fmt.Errorf("released 0 reservations for %d path patterns", len(paths))
	}
	return nil
}

// RenewReservations extends the TTL for an agent's reservations.
func (m *FileReservationManager) RenewReservations(ctx context.Context, agentName string, extendSeconds int) error {
	if m.client == nil {
		return nil
	}

	renewResult, err := m.client.RenewReservations(ctx, agentmail.RenewReservationsOptions{
		ProjectKey:    m.projectKey,
		AgentName:     agentName,
		ExtendSeconds: extendSeconds,
	})
	if err != nil {
		return err
	}
	if renewResult == nil {
		return fmt.Errorf("renew returned no result")
	}
	if renewResult.Renewed == 0 {
		return fmt.Errorf("renewed 0 reservations")
	}
	return nil
}
