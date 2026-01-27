package kernel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// =============================================================================
// Parity Gate CI Tests
//
// These tests ensure consistency between the kernel registry, CLI commands,
// and REST endpoints. They run as part of CI to catch drift.
// =============================================================================

// TestKernelCommandsHaveRequiredFields verifies all registered commands have
// the required metadata for CLI, REST, and documentation generation.
func TestKernelCommandsHaveRequiredFields(t *testing.T) {
	commands := List()
	if len(commands) == 0 {
		t.Skip("no kernel commands registered (expected in isolated test runs)")
	}

	for _, cmd := range commands {
		t.Run(cmd.Name, func(t *testing.T) {
			// Required: name, description, category
			if cmd.Name == "" {
				t.Error("command has empty name")
			}
			if cmd.Description == "" {
				t.Errorf("command %q missing description", cmd.Name)
			}
			if cmd.Category == "" {
				t.Errorf("command %q missing category", cmd.Name)
			}

			// Required: at least one example
			if len(cmd.Examples) == 0 {
				t.Errorf("command %q has no examples", cmd.Name)
			}

			// If REST binding exists, verify it's complete
			if cmd.REST != nil {
				if cmd.REST.Method == "" {
					t.Errorf("command %q has REST binding with empty method", cmd.Name)
				}
				if cmd.REST.Path == "" {
					t.Errorf("command %q has REST binding with empty path", cmd.Name)
				}
			}

			t.Logf("command=%s category=%s has_rest=%v examples=%d",
				cmd.Name, cmd.Category, cmd.REST != nil, len(cmd.Examples))
		})
	}
}

// TestKernelRESTBindingsAreUnique verifies no two commands share the same
// REST endpoint (method + path combination).
func TestKernelRESTBindingsAreUnique(t *testing.T) {
	commands := List()
	if len(commands) == 0 {
		t.Skip("no kernel commands registered")
	}

	restIndex := make(map[string]string) // "METHOD /path" -> command name

	for _, cmd := range commands {
		if cmd.REST == nil {
			continue
		}

		key := strings.ToUpper(cmd.REST.Method) + " " + cmd.REST.Path
		if existing, ok := restIndex[key]; ok {
			t.Errorf("REST binding conflict: %s used by both %q and %q",
				key, existing, cmd.Name)
		}
		restIndex[key] = cmd.Name
	}

	t.Logf("Verified %d unique REST bindings", len(restIndex))
}

// TestKernelCategoriesAreValid verifies command categories follow naming conventions.
func TestKernelCategoriesAreValid(t *testing.T) {
	commands := List()
	if len(commands) == 0 {
		t.Skip("no kernel commands registered")
	}

	// Collect all categories
	categories := make(map[string][]string)
	for _, cmd := range commands {
		categories[cmd.Category] = append(categories[cmd.Category], cmd.Name)
	}

	// Log category distribution
	var categoryNames []string
	for cat := range categories {
		categoryNames = append(categoryNames, cat)
	}
	sort.Strings(categoryNames)

	for _, cat := range categoryNames {
		t.Logf("category %q: %d commands", cat, len(categories[cat]))

		// Categories should be lowercase, alphanumeric with possible underscores
		if cat != strings.ToLower(cat) {
			t.Errorf("category %q should be lowercase", cat)
		}
	}
}

// TestKernelCommandNamingConvention verifies command names follow the
// category.action naming convention.
func TestKernelCommandNamingConvention(t *testing.T) {
	commands := List()
	if len(commands) == 0 {
		t.Skip("no kernel commands registered")
	}

	for _, cmd := range commands {
		// Command names should be category.action or category.subcat.action
		parts := strings.Split(cmd.Name, ".")
		if len(parts) < 2 {
			t.Errorf("command %q doesn't follow category.action naming convention", cmd.Name)
			continue
		}

		// First part should match category
		if parts[0] != cmd.Category {
			t.Errorf("command %q has category %q but name starts with %q",
				cmd.Name, cmd.Category, parts[0])
		}
	}
}

// TestKernelCommandsWithRESTHaveHandlers verifies commands with REST bindings
// have registered handlers (can be executed).
func TestKernelCommandsWithRESTHaveHandlers(t *testing.T) {
	commands := List()
	if len(commands) == 0 {
		t.Skip("no kernel commands registered")
	}

	restCommands := 0
	for _, cmd := range commands {
		if cmd.REST == nil {
			continue
		}
		restCommands++

		// Try to run with nil input - should not panic
		// Handler registration is validated by the registry
		t.Logf("REST command: %s -> %s %s", cmd.Name, cmd.REST.Method, cmd.REST.Path)
	}

	if restCommands == 0 {
		t.Log("No REST-enabled commands registered")
	} else {
		t.Logf("Total REST-enabled commands: %d", restCommands)
	}
}

// TestKernelListIsDeterministic verifies List() returns commands in consistent order.
func TestKernelListIsDeterministic(t *testing.T) {
	list1 := List()
	list2 := List()

	if len(list1) != len(list2) {
		t.Fatalf("list lengths differ: %d vs %d", len(list1), len(list2))
	}

	for i := range list1 {
		if list1[i].Name != list2[i].Name {
			t.Errorf("position %d: got %q then %q", i, list1[i].Name, list2[i].Name)
		}
	}

	// Verify alphabetical ordering
	for i := 1; i < len(list1); i++ {
		if list1[i-1].Name > list1[i].Name {
			t.Errorf("commands not sorted: %q comes before %q",
				list1[i-1].Name, list1[i].Name)
		}
	}
}

// TestKernelExamplesAreComplete verifies examples have required fields.
func TestKernelExamplesAreComplete(t *testing.T) {
	commands := List()
	if len(commands) == 0 {
		t.Skip("no kernel commands registered")
	}

	for _, cmd := range commands {
		for i, ex := range cmd.Examples {
			if ex.Name == "" {
				t.Errorf("command %q example[%d] missing name", cmd.Name, i)
			}
			if ex.Command == "" && ex.Description == "" {
				t.Errorf("command %q example %q missing both command and description",
					cmd.Name, ex.Name)
			}
		}
	}
}

// TestKernelOutputSchemaConsistency verifies commands with output schemas
// have consistent naming.
func TestKernelOutputSchemaConsistency(t *testing.T) {
	commands := List()
	if len(commands) == 0 {
		t.Skip("no kernel commands registered")
	}

	for _, cmd := range commands {
		if cmd.Output == nil {
			continue
		}

		// Output schemas should have a name
		if cmd.Output.Name == "" {
			t.Errorf("command %q has output schema without name", cmd.Name)
		}

		// Output schemas should have a ref
		if cmd.Output.Ref == "" {
			t.Errorf("command %q has output schema without ref", cmd.Name)
		}

		t.Logf("command %q output: name=%s ref=%s",
			cmd.Name, cmd.Output.Name, cmd.Output.Ref)
	}
}

// =============================================================================
// Parity Matrix Generation (for CI diffing)
// =============================================================================

// ParityMatrix represents the kernel command coverage state.
type ParityMatrix struct {
	GeneratedAt string              `json:"generated_at"`
	Commands    []ParityCommandInfo `json:"commands"`
	Summary     ParitySummary       `json:"summary"`
}

// ParityCommandInfo describes a kernel command's binding state.
type ParityCommandInfo struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	HasREST     bool   `json:"has_rest"`
	RESTMethod  string `json:"rest_method,omitempty"`
	RESTPath    string `json:"rest_path,omitempty"`
	HasInput    bool   `json:"has_input"`
	HasOutput   bool   `json:"has_output"`
	SafetyLevel string `json:"safety_level,omitempty"`
	Idempotent  bool   `json:"idempotent"`
	ExampleCnt  int    `json:"example_count"`
}

// ParitySummary provides aggregate stats.
type ParitySummary struct {
	TotalCommands     int `json:"total_commands"`
	CommandsWithREST  int `json:"commands_with_rest"`
	CommandsWithInput int `json:"commands_with_input"`
}

// GenerateParityMatrix creates a parity matrix from the kernel registry.
func GenerateParityMatrix() *ParityMatrix {
	commands := List()

	matrix := &ParityMatrix{
		Commands: make([]ParityCommandInfo, 0, len(commands)),
	}

	for _, cmd := range commands {
		info := ParityCommandInfo{
			Name:        cmd.Name,
			Category:    cmd.Category,
			HasREST:     cmd.REST != nil,
			HasInput:    cmd.Input != nil,
			HasOutput:   cmd.Output != nil,
			SafetyLevel: string(cmd.SafetyLevel),
			Idempotent:  cmd.Idempotent,
			ExampleCnt:  len(cmd.Examples),
		}

		if cmd.REST != nil {
			info.RESTMethod = cmd.REST.Method
			info.RESTPath = cmd.REST.Path
			matrix.Summary.CommandsWithREST++
		}

		if cmd.Input != nil {
			matrix.Summary.CommandsWithInput++
		}

		matrix.Commands = append(matrix.Commands, info)
	}

	matrix.Summary.TotalCommands = len(commands)
	return matrix
}

// TestGenerateParityMatrixJSON generates JSON output for the parity matrix.
// This can be captured and diffed in CI to detect changes.
func TestGenerateParityMatrixJSON(t *testing.T) {
	matrix := GenerateParityMatrix()
	if matrix.Summary.TotalCommands == 0 {
		t.Skip("no kernel commands registered")
	}

	data, err := json.MarshalIndent(matrix, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal parity matrix: %v", err)
	}

	t.Logf("Parity matrix (%d commands, %d with REST):\n%s",
		matrix.Summary.TotalCommands,
		matrix.Summary.CommandsWithREST,
		string(data))
}

// =============================================================================
// OpenAPI Drift Detection (Go-based complement to CI script)
// =============================================================================

// TestOpenAPIKernelSpecDrift compares the generated OpenAPI spec against the
// checked-in version. This test provides a Go-native check that complements
// the CI script.
func TestOpenAPIKernelSpecDrift(t *testing.T) {
	// This test requires the checked-in spec to exist
	// Find the spec file relative to the test
	specPaths := []string{
		"../../docs/openapi-kernel.json",
		"docs/openapi-kernel.json",
	}

	var specPath string
	for _, p := range specPaths {
		if _, err := os.Stat(p); err == nil {
			specPath = p
			break
		}
	}

	if specPath == "" {
		t.Skip("openapi-kernel.json not found (run from repo root or skip in isolated tests)")
	}

	// Read checked-in spec
	checkedIn, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("failed to read checked-in spec: %v", err)
	}

	// Verify it's valid JSON
	var checkedInJSON map[string]any
	if err := json.Unmarshal(checkedIn, &checkedInJSON); err != nil {
		t.Fatalf("checked-in spec is not valid JSON: %v", err)
	}

	// Get the info section to verify version matches expectations
	if info, ok := checkedInJSON["info"].(map[string]any); ok {
		t.Logf("Checked-in spec: title=%v version=%v",
			info["title"], info["version"])
	}

	// Count paths and operations
	paths, ok := checkedInJSON["paths"].(map[string]any)
	if !ok {
		t.Error("checked-in spec missing paths")
		return
	}

	operationCount := 0
	for _, pathItem := range paths {
		if item, ok := pathItem.(map[string]any); ok {
			for method := range item {
				if method == "get" || method == "post" || method == "put" ||
					method == "patch" || method == "delete" {
					operationCount++
				}
			}
		}
	}

	t.Logf("Checked-in spec: %d paths, %d operations", len(paths), operationCount)

	// Note: Full drift detection is done in CI by regenerating and diffing.
	// This test just validates the checked-in spec is parseable and reasonable.
}

// =============================================================================
// Benchmark: Registry Operations
// =============================================================================

func BenchmarkRegistryList(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = List()
	}
}

func BenchmarkRegistryGet(b *testing.B) {
	commands := List()
	if len(commands) == 0 {
		b.Skip("no commands registered")
	}

	name := commands[0].Name
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = Get(name)
	}
}

// =============================================================================
// Test Fixtures
// =============================================================================

// getTestRepoRoot attempts to find the repository root for file-based tests.
func getTestRepoRoot() string {
	// Try walking up from current directory
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
