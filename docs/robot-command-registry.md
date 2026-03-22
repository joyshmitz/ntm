# Robot Command and Section Registry

> **Authoritative specification for the unified robot surface and section registry.**
> All robot surfaces, sections, capabilities, help, and schema artifacts derive from this registry.

**Status:** RATIFIED
**Bead:** bd-j9jo3.7.1
**Created:** 2026-03-22
**Part of:** bd-j9jo3 (robot-redesign epic)
**Depends on:**
- robot-surface-taxonomy.md (bd-j9jo3.1.1)
- projection-section-model.md (bd-j9jo3.1.2)
- robot-schema-versioning.md (bd-j9jo3.1.3)

---

## 1. Design Principles

### 1.1 Why a Registry?

Before the redesign, robot mode metadata lived in three places:
- `schema.go` — Maps command names to Go types for schema generation
- `capabilities.go` — Hand-maintained list of commands with parameters/examples
- CLI flags — Cobra flag definitions scattered across `robot.go`

This led to drift:
- New commands missing from capabilities
- Schema types not matching actual output
- Help text diverging from implementation
- Transport availability undocumented

**The registry becomes the single source of truth.**

### 1.2 Core Invariants

1. **Declared once, derived everywhere** — Capabilities, help, schema, docs derive from registry
2. **Lane-first organization** — Surfaces belong to lanes per taxonomy
3. **Section composition explicit** — Each surface declares which sections it returns
4. **Transport-aware** — CLI, REST, WebSocket availability per surface
5. **Machine and human readable** — Serves both `--robot-help` and programmatic discovery

---

## 2. Registry Structure

### 2.1 Surface Registration

```go
package robot

// SurfaceInfo defines a robot surface in the registry.
type SurfaceInfo struct {
    // Identity
    Name        string `json:"name"`         // e.g., "snapshot"
    SchemaID    string `json:"schema_id"`    // e.g., "ntm:robot:snapshot:v1"

    // Classification
    Lane        string `json:"lane"`         // bootstrap, summarize, replay, triage, inspect, act, wait
    Category    string `json:"category"`     // state, attention, control, spawn, beads, utility

    // Description
    Summary     string `json:"summary"`      // One-line purpose
    Description string `json:"description"`  // Full explanation

    // CLI
    PrimaryFlag string           `json:"primary_flag"` // e.g., "--robot-snapshot"
    AliasFlags  []string         `json:"alias_flags,omitempty"`
    Parameters  []ParameterInfo  `json:"parameters,omitempty"`

    // Output
    Sections    []string         `json:"sections"`     // Section names returned
    Envelope    bool             `json:"envelope"`     // Wraps in RobotResponse

    // Transport
    Transports  []TransportInfo  `json:"transports"`   // CLI, REST, WebSocket

    // Examples
    Examples    []ExampleInfo    `json:"examples,omitempty"`

    // Metadata
    SinceVersion string          `json:"since_version,omitempty"` // When added
    Deprecated   *DeprecationInfo `json:"deprecated,omitempty"`
}

// ParameterInfo describes a surface parameter.
type ParameterInfo struct {
    Name        string `json:"name"`
    Flag        string `json:"flag"`          // CLI flag
    Type        string `json:"type"`          // bool, string, int, duration
    Required    bool   `json:"required"`
    Default     string `json:"default,omitempty"`
    Description string `json:"description"`
}

// TransportInfo describes transport availability.
type TransportInfo struct {
    Type     string `json:"type"`      // cli, rest, websocket
    Endpoint string `json:"endpoint"`  // e.g., "/api/robot/snapshot"
    Method   string `json:"method,omitempty"` // GET, POST (REST only)
}

// ExampleInfo provides usage examples.
type ExampleInfo struct {
    Command     string `json:"command"`
    Description string `json:"description,omitempty"`
}

// DeprecationInfo marks deprecated surfaces.
type DeprecationInfo struct {
    Since       string `json:"since"`
    RemoveIn    string `json:"remove_in"`
    Alternative string `json:"alternative,omitempty"`
    Message     string `json:"message"`
}
```

### 2.2 Section Registration

```go
// SectionInfo defines a projection section in the registry.
type SectionInfo struct {
    // Identity
    Name     string `json:"name"`      // e.g., "sessions"
    SchemaID string `json:"schema_id"` // e.g., "ntm:section:sessions:v1"

    // Description
    Summary     string `json:"summary"`
    Description string `json:"description"`

    // Scope
    Scope string `json:"scope"` // global, session, pane

    // Used by
    UsedBy []string `json:"used_by"` // Surface names that include this section
}
```

### 2.3 Global Registry

```go
var Registry = struct {
    Surfaces map[string]SurfaceInfo
    Sections map[string]SectionInfo
    Lanes    []string
}{
    Surfaces: surfaces,
    Sections: sections,
    Lanes:    []string{"bootstrap", "summarize", "replay", "triage", "inspect", "act", "wait"},
}
```

---

## 3. Surface Definitions

### 3.1 Bootstrap Lane

```go
var surfaces = map[string]SurfaceInfo{
    "snapshot": {
        Name:        "snapshot",
        SchemaID:    "ntm:robot:snapshot:v1",
        Lane:        "bootstrap",
        Category:    "state",
        Summary:     "Complete system state for AI orchestration bootstrap",
        Description: "Returns everything an operator needs to reason about the system: sessions, agents, beads, alerts, mail, and an initial cursor for subsequent event polling.",
        PrimaryFlag: "--robot-snapshot",
        Parameters: []ParameterInfo{
            {Name: "since", Flag: "--since", Type: "string", Description: "RFC3339 timestamp for delta snapshot"},
            {Name: "bead-limit", Flag: "--bead-limit", Type: "int", Default: "5", Description: "Max beads per category"},
            {Name: "limit", Flag: "--robot-limit", Type: "int", Default: "0", Description: "Max sessions to return"},
            {Name: "offset", Flag: "--robot-offset", Type: "int", Default: "0", Description: "Pagination offset"},
        },
        Sections: []string{"summary", "sessions", "work", "coordination", "quota", "alerts", "incidents", "attention", "source_health", "next_actions"},
        Envelope:  true,
        Transports: []TransportInfo{
            {Type: "cli", Endpoint: "ntm --robot-snapshot"},
            {Type: "rest", Endpoint: "/api/robot/snapshot", Method: "GET"},
            {Type: "websocket", Endpoint: "/api/robot/stream?surface=snapshot"},
        },
        Examples: []ExampleInfo{
            {Command: "ntm --robot-snapshot", Description: "Full bootstrap snapshot"},
            {Command: "ntm --robot-snapshot --since=2026-03-22T10:00:00Z", Description: "Delta snapshot"},
        },
    },
}
```

### 3.2 Summarize Lane

```go
"status": {
    Name:        "status",
    SchemaID:    "ntm:robot:status:v1",
    Lane:        "summarize",
    Category:    "state",
    Summary:     "Cheap high-level view for frequent polling",
    Description: "Returns session list, agent counts, and basic health indicators. Does NOT return full state.",
    PrimaryFlag: "--robot-status",
    Parameters: []ParameterInfo{
        {Name: "limit", Flag: "--robot-limit", Type: "int", Default: "0", Description: "Max sessions to return"},
        {Name: "offset", Flag: "--robot-offset", Type: "int", Default: "0", Description: "Pagination offset"},
    },
    Sections: []string{"summary", "sessions"},
    Envelope:  true,
    Transports: []TransportInfo{
        {Type: "cli", Endpoint: "ntm --robot-status"},
        {Type: "rest", Endpoint: "/api/robot/status", Method: "GET"},
    },
    Examples: []ExampleInfo{
        {Command: "ntm --robot-status", Description: "Quick status check"},
    },
},
```

### 3.3 Replay Lane

```go
"events": {
    Name:        "events",
    SchemaID:    "ntm:robot:events:v1",
    Lane:        "replay",
    Category:    "attention",
    Summary:     "Raw event stream since cursor",
    Description: "Returns events in monotonic cursor order. Use for log replay and audit trails.",
    PrimaryFlag: "--robot-events",
    Parameters: []ParameterInfo{
        {Name: "since", Flag: "--since", Type: "int", Required: true, Description: "Cursor to resume from"},
        {Name: "limit", Flag: "--limit", Type: "int", Default: "100", Description: "Max events to return"},
        {Name: "category", Flag: "--category", Type: "string", Description: "Filter by category"},
        {Name: "severity", Flag: "--severity", Type: "string", Description: "Minimum severity"},
    },
    Sections: []string{"events"},
    Envelope:  true,
    Transports: []TransportInfo{
        {Type: "cli", Endpoint: "ntm --robot-events"},
        {Type: "rest", Endpoint: "/api/robot/events", Method: "GET"},
    },
},
```

### 3.4 Triage Lane

```go
"attention": {
    Name:        "attention",
    SchemaID:    "ntm:robot:attention:v1",
    Lane:        "triage",
    Category:    "attention",
    Summary:     "Blocking wait for attention-worthy events",
    Description: "The primary tending primitive. Blocks until events need attention, then returns prioritized items.",
    PrimaryFlag: "--robot-attention",
    Parameters: []ParameterInfo{
        {Name: "timeout", Flag: "--timeout", Type: "duration", Default: "30s", Description: "Max wait time"},
        {Name: "session", Flag: "--session", Type: "string", Description: "Filter by session"},
    },
    Sections: []string{"attention", "incidents", "next_actions"},
    Envelope:  true,
    Transports: []TransportInfo{
        {Type: "cli", Endpoint: "ntm --robot-attention"},
        {Type: "websocket", Endpoint: "/api/robot/stream?surface=attention"},
    },
},

"digest": {
    Name:        "digest",
    SchemaID:    "ntm:robot:digest:v1",
    Lane:        "triage",
    Category:    "attention",
    Summary:     "Non-blocking attention summary",
    Description: "Returns what-changed counts and top items without waiting. Token-efficient for tight loops.",
    PrimaryFlag: "--robot-digest",
    Parameters: []ParameterInfo{
        {Name: "session", Flag: "--session", Type: "string", Description: "Filter by session"},
        {Name: "limit", Flag: "--limit", Type: "int", Default: "10", Description: "Max items"},
    },
    Sections: []string{"attention", "next_actions"},
    Envelope:  true,
    Transports: []TransportInfo{
        {Type: "cli", Endpoint: "ntm --robot-digest"},
        {Type: "rest", Endpoint: "/api/robot/digest", Method: "GET"},
    },
},
```

### 3.5 Inspect Lane

```go
"tail": {
    Name:        "tail",
    SchemaID:    "ntm:robot:tail:v1",
    Lane:        "inspect",
    Category:    "state",
    Summary:     "Recent pane output capture",
    Description: "Captures recent output from session panes for checking agent progress or errors.",
    PrimaryFlag: "--robot-tail",
    Parameters: []ParameterInfo{
        {Name: "session", Flag: "--robot-tail", Type: "string", Required: true, Description: "Session name"},
        {Name: "lines", Flag: "--lines", Type: "int", Default: "20", Description: "Lines per pane"},
        {Name: "panes", Flag: "--panes", Type: "string", Description: "Comma-separated pane indices to filter"},
    },
    Sections: []string{"pane_output"},
    Envelope:  true,
    Transports: []TransportInfo{
        {Type: "cli", Endpoint: "ntm --robot-tail=SESSION"},
        {Type: "rest", Endpoint: "/api/robot/tail/:session", Method: "GET"},
    },
},

"inspect-pane": {
    Name:        "inspect-pane",
    SchemaID:    "ntm:robot:inspect:v1",
    Lane:        "inspect",
    Category:    "state",
    Summary:     "Detailed pane state inspection",
    Description: "Returns full pane details including agent state, context window, health, and recent activity.",
    PrimaryFlag: "--robot-inspect",
    Parameters: []ParameterInfo{
        {Name: "session", Flag: "--robot-inspect", Type: "string", Required: true, Description: "Session name"},
        {Name: "pane", Flag: "--pane", Type: "string", Description: "Specific pane to inspect"},
    },
    Sections: []string{"pane_detail", "agent_state", "context_window"},
    Envelope:  true,
    Transports: []TransportInfo{
        {Type: "cli", Endpoint: "ntm --robot-inspect=SESSION"},
        {Type: "rest", Endpoint: "/api/robot/inspect/:session", Method: "GET"},
    },
},
```

### 3.6 Act Lane

```go
"send": {
    Name:        "send",
    SchemaID:    "ntm:robot:send:v1",
    Lane:        "act",
    Category:    "control",
    Summary:     "Send prompts to agents",
    Description: "Sends text to agent panes via tmux send-keys. Supports targeting specific panes.",
    PrimaryFlag: "--robot-send",
    Parameters: []ParameterInfo{
        {Name: "session", Flag: "--robot-send", Type: "string", Required: true, Description: "Target session"},
        {Name: "pane", Flag: "--pane", Type: "string", Description: "Target pane"},
        {Name: "text", Flag: "--text", Type: "string", Required: true, Description: "Text to send"},
        {Name: "no-enter", Flag: "--no-enter", Type: "bool", Default: "false", Description: "Don't append Enter"},
    },
    Sections: []string{"send_result"},
    Envelope:  true,
    Transports: []TransportInfo{
        {Type: "cli", Endpoint: "ntm --robot-send=SESSION --text=..."},
        {Type: "rest", Endpoint: "/api/robot/send", Method: "POST"},
    },
},

"spawn": {
    Name:        "spawn",
    SchemaID:    "ntm:robot:spawn:v1",
    Lane:        "act",
    Category:    "spawn",
    Summary:     "Create sessions with agents",
    Description: "Creates new tmux sessions and spawns AI coding agents into them.",
    PrimaryFlag: "--robot-spawn",
    Parameters: []ParameterInfo{
        {Name: "session", Flag: "--robot-spawn", Type: "string", Required: true, Description: "Session name"},
        {Name: "preset", Flag: "--preset", Type: "string", Default: "default", Description: "Spawn preset"},
        {Name: "project", Flag: "--project", Type: "string", Description: "Project directory"},
    },
    Sections: []string{"spawn_result"},
    Envelope:  true,
    Transports: []TransportInfo{
        {Type: "cli", Endpoint: "ntm --robot-spawn=SESSION"},
        {Type: "rest", Endpoint: "/api/robot/spawn", Method: "POST"},
    },
},

"interrupt": {
    Name:        "interrupt",
    SchemaID:    "ntm:robot:interrupt:v1",
    Lane:        "act",
    Category:    "control",
    Summary:     "Send interrupt signals to agents",
    Description: "Sends Ctrl+C to agent panes, optionally followed by new task text.",
    PrimaryFlag: "--robot-interrupt",
    Parameters: []ParameterInfo{
        {Name: "session", Flag: "--robot-interrupt", Type: "string", Required: true, Description: "Target session"},
        {Name: "pane", Flag: "--pane", Type: "string", Description: "Target pane"},
        {Name: "task", Flag: "--task", Type: "string", Description: "New task to send after interrupt"},
    },
    Sections: []string{"interrupt_result"},
    Envelope:  true,
    Transports: []TransportInfo{
        {Type: "cli", Endpoint: "ntm --robot-interrupt=SESSION"},
        {Type: "rest", Endpoint: "/api/robot/interrupt", Method: "POST"},
    },
},
```

### 3.7 Wait Lane

```go
"wait": {
    Name:        "wait",
    SchemaID:    "ntm:robot:wait:v1",
    Lane:        "wait",
    Category:    "control",
    Summary:     "Block until condition is met",
    Description: "Waits for a named condition (idle, busy, output, error) before returning.",
    PrimaryFlag: "--robot-wait",
    Parameters: []ParameterInfo{
        {Name: "condition", Flag: "--robot-wait", Type: "string", Required: true, Description: "Condition name"},
        {Name: "session", Flag: "--session", Type: "string", Description: "Session scope"},
        {Name: "timeout", Flag: "--timeout", Type: "duration", Default: "5m", Description: "Max wait time"},
    },
    Sections: []string{"wait_result"},
    Envelope:  true,
    Transports: []TransportInfo{
        {Type: "cli", Endpoint: "ntm --robot-wait=CONDITION"},
        {Type: "websocket", Endpoint: "/api/robot/stream?surface=wait&condition=..."},
    },
},
```

---

## 4. Section Definitions

```go
var sections = map[string]SectionInfo{
    "summary": {
        Name:        "summary",
        SchemaID:    "ntm:section:summary:v1",
        Summary:     "Cheap glance metrics",
        Description: "Session/agent counts, health score, work state. Fits in ~50 tokens.",
        Scope:       "global",
        UsedBy:      []string{"snapshot", "status", "terse"},
    },
    "sessions": {
        Name:        "sessions",
        SchemaID:    "ntm:section:sessions:v1",
        Summary:     "Session and agent inventory",
        Description: "Per-session details including agents, panes, state, and health.",
        Scope:       "session",
        UsedBy:      []string{"snapshot", "status"},
    },
    "work": {
        Name:        "work",
        SchemaID:    "ntm:section:work:v1",
        Summary:     "Bead-based work queue state",
        Description: "Ready beads, in-progress work, blocked items. Derived from .beads/.",
        Scope:       "global",
        UsedBy:      []string{"snapshot"},
    },
    "coordination": {
        Name:        "coordination",
        SchemaID:    "ntm:section:coordination:v1",
        Summary:     "Multi-agent coordination state",
        Description: "File reservations, Agent Mail status, active conflicts.",
        Scope:       "global",
        UsedBy:      []string{"snapshot"},
    },
    "quota": {
        Name:        "quota",
        SchemaID:    "ntm:section:quota:v1",
        Summary:     "API rate limit state",
        Description: "Provider quota status, remaining capacity, reset timing.",
        Scope:       "global",
        UsedBy:      []string{"snapshot"},
    },
    "alerts": {
        Name:        "alerts",
        SchemaID:    "ntm:section:alerts:v1",
        Summary:     "Active alerts requiring attention",
        Description: "Transient alerts that have fired but not yet cleared or promoted.",
        Scope:       "global",
        UsedBy:      []string{"snapshot", "status"},
    },
    "incidents": {
        Name:        "incidents",
        SchemaID:    "ntm:section:incidents:v1",
        Summary:     "Durable incident patterns",
        Description: "Promoted alerts with lifecycle and history. See incident-taxonomy.md.",
        Scope:       "global",
        UsedBy:      []string{"snapshot", "attention"},
    },
    "attention": {
        Name:        "attention",
        SchemaID:    "ntm:section:attention:v1",
        Summary:     "Prioritized attention items",
        Description: "What needs operator attention, ranked by urgency and actionability.",
        Scope:       "global",
        UsedBy:      []string{"snapshot", "attention", "digest"},
    },
    "source_health": {
        Name:        "source_health",
        SchemaID:    "ntm:section:source_health:v1",
        Summary:     "Data source freshness and provenance",
        Description: "Per-source status, staleness, degraded features. See freshness-degraded-state-contract.md.",
        Scope:       "global",
        UsedBy:      []string{"snapshot"},
    },
    "next_actions": {
        Name:        "next_actions",
        SchemaID:    "ntm:section:next_actions:v1",
        Summary:     "Suggested operator actions",
        Description: "Machine-actionable commands for addressing attention items.",
        Scope:       "global",
        UsedBy:      []string{"snapshot", "attention", "digest"},
    },
    "events": {
        Name:        "events",
        SchemaID:    "ntm:section:events:v1",
        Summary:     "Attention event journal entries",
        Description: "Raw event entries with cursors for replay.",
        Scope:       "global",
        UsedBy:      []string{"events"},
    },
    "pane_output": {
        Name:        "pane_output",
        SchemaID:    "ntm:section:pane_output:v1",
        Summary:     "Captured pane terminal output",
        Description: "Recent lines from pane terminal buffers.",
        Scope:       "pane",
        UsedBy:      []string{"tail"},
    },
    "pane_detail": {
        Name:        "pane_detail",
        SchemaID:    "ntm:section:pane_detail:v1",
        Summary:     "Full pane inspection data",
        Description: "Agent state, context window, health, activity markers.",
        Scope:       "pane",
        UsedBy:      []string{"inspect-pane"},
    },
}
```

---

## 5. Registry API

### 5.1 Lookup Functions

```go
// GetSurface returns surface info by name.
func GetSurface(name string) (SurfaceInfo, bool) {
    info, ok := Registry.Surfaces[name]
    return info, ok
}

// GetSection returns section info by name.
func GetSection(name string) (SectionInfo, bool) {
    info, ok := Registry.Sections[name]
    return info, ok
}

// GetSurfacesByLane returns all surfaces in a lane.
func GetSurfacesByLane(lane string) []SurfaceInfo {
    var result []SurfaceInfo
    for _, s := range Registry.Surfaces {
        if s.Lane == lane {
            result = append(result, s)
        }
    }
    return result
}

// GetSurfacesByCategory returns all surfaces in a category.
func GetSurfacesByCategory(category string) []SurfaceInfo {
    var result []SurfaceInfo
    for _, s := range Registry.Surfaces {
        if s.Category == category {
            result = append(result, s)
        }
    }
    return result
}

// GetSurfacesForSection returns surfaces that include a section.
func GetSurfacesForSection(section string) []SurfaceInfo {
    var result []SurfaceInfo
    for _, s := range Registry.Surfaces {
        for _, sec := range s.Sections {
            if sec == section {
                result = append(result, s)
                break
            }
        }
    }
    return result
}
```

### 5.2 Validation

```go
// ValidateRegistry checks registry consistency.
func ValidateRegistry() error {
    // Check all section references exist
    for name, surface := range Registry.Surfaces {
        for _, section := range surface.Sections {
            if _, ok := Registry.Sections[section]; !ok {
                return fmt.Errorf("surface %q references unknown section %q", name, section)
            }
        }
    }

    // Check all UsedBy references exist
    for name, section := range Registry.Sections {
        for _, surface := range section.UsedBy {
            if _, ok := Registry.Surfaces[surface]; !ok {
                return fmt.Errorf("section %q references unknown surface %q", name, surface)
            }
        }
    }

    // Check lane validity
    validLanes := make(map[string]bool)
    for _, lane := range Registry.Lanes {
        validLanes[lane] = true
    }
    for name, surface := range Registry.Surfaces {
        if !validLanes[surface.Lane] {
            return fmt.Errorf("surface %q has invalid lane %q", name, surface.Lane)
        }
    }

    return nil
}
```

---

## 6. Derived Outputs

### 6.1 Capabilities Output

`--robot-capabilities` derives entirely from registry:

```go
func GetCapabilities() (*CapabilitiesOutput, error) {
    surfaces := make([]SurfaceCapability, 0, len(Registry.Surfaces))
    for _, s := range Registry.Surfaces {
        surfaces = append(surfaces, SurfaceCapability{
            Name:        s.Name,
            SchemaID:    s.SchemaID,
            Lane:        s.Lane,
            Description: s.Summary,
            Flags:       append([]string{s.PrimaryFlag}, s.AliasFlags...),
            Parameters:  s.Parameters,
            Returns:     s.Sections,
        })
    }

    sections := make([]SectionCapability, 0, len(Registry.Sections))
    for _, sec := range Registry.Sections {
        sections = append(sections, SectionCapability{
            Name:        sec.Name,
            SchemaID:    sec.SchemaID,
            Description: sec.Summary,
            UsedBy:      sec.UsedBy,
        })
    }

    return &CapabilitiesOutput{
        RobotResponse: NewRobotResponse(true),
        Surfaces:      surfaces,
        Sections:      sections,
        Lanes:         Registry.Lanes,
    }, nil
}
```

### 6.2 Help Output

`--robot-help` derives from registry:

```go
func GetHelp(topic string) string {
    if topic == "" {
        return formatOverviewHelp()
    }
    if surface, ok := GetSurface(topic); ok {
        return formatSurfaceHelp(surface)
    }
    if section, ok := GetSection(topic); ok {
        return formatSectionHelp(section)
    }
    return formatNotFoundHelp(topic)
}

func formatSurfaceHelp(s SurfaceInfo) string {
    var b strings.Builder
    fmt.Fprintf(&b, "## %s\n\n", s.Name)
    fmt.Fprintf(&b, "**Lane:** %s | **Category:** %s\n\n", s.Lane, s.Category)
    fmt.Fprintf(&b, "%s\n\n", s.Description)
    fmt.Fprintf(&b, "**Flag:** `%s`\n\n", s.PrimaryFlag)

    if len(s.Parameters) > 0 {
        fmt.Fprintf(&b, "### Parameters\n\n")
        for _, p := range s.Parameters {
            req := ""
            if p.Required {
                req = " (required)"
            }
            fmt.Fprintf(&b, "- `%s` (%s)%s: %s\n", p.Flag, p.Type, req, p.Description)
        }
        fmt.Fprintf(&b, "\n")
    }

    if len(s.Examples) > 0 {
        fmt.Fprintf(&b, "### Examples\n\n")
        for _, e := range s.Examples {
            fmt.Fprintf(&b, "```\n%s\n```\n", e.Command)
            if e.Description != "" {
                fmt.Fprintf(&b, "%s\n\n", e.Description)
            }
        }
    }

    return b.String()
}
```

### 6.3 Schema Output

`--robot-schema` uses registry for schema IDs:

```go
func GetSchemaID(surface string) string {
    if info, ok := GetSurface(surface); ok {
        return info.SchemaID
    }
    return ""
}

func GetSchemaInfo(schemaID string) (*SchemaInfo, bool) {
    for _, s := range Registry.Surfaces {
        if s.SchemaID == schemaID {
            return &SchemaInfo{
                ID:       s.SchemaID,
                Surface:  s.Name,
                Lane:     s.Lane,
                Sections: s.Sections,
            }, true
        }
    }
    return nil, false
}
```

---

## 7. File Layout

```
internal/robot/
├── registry/
│   ├── registry.go       // Core types and global registry
│   ├── surfaces.go       // Surface definitions
│   ├── sections.go       // Section definitions
│   ├── validation.go     // Registry validation
│   └── registry_test.go  // Consistency tests
├── capabilities.go       // Uses registry
├── help.go               // Uses registry
└── schema.go             // Uses registry
```

---

## 8. Migration Path

### 8.1 Phase 1: Registry Introduction

1. Create `internal/robot/registry/` package with types
2. Populate with existing surface/section metadata
3. Add `ValidateRegistry()` to CI

### 8.2 Phase 2: Capabilities Migration

1. Rewrite `GetCapabilities()` to derive from registry
2. Remove hand-maintained command list from `capabilities.go`
3. Verify output matches previous format

### 8.3 Phase 3: Help Migration

1. Create `GetHelp()` deriving from registry
2. Replace inline help text
3. Add `--robot-help=topic` support

### 8.4 Phase 4: Schema Migration

1. Link `SchemaCommand` map to registry
2. Generate schema IDs from registry
3. Validate schemas match registry declarations

---

## 9. Success Criteria

This registry is successful when:

1. Capabilities, help, and schema outputs derive from registry alone
2. Adding a new surface requires only registry entry + implementation
3. Registry validation catches inconsistencies before deployment
4. Documentation generation uses registry as source

---

## References

- [Robot Surface Taxonomy](robot-surface-taxonomy.md) (bd-j9jo3.1.1)
- [Projection Section Model](projection-section-model.md) (bd-j9jo3.1.2)
- [Robot Schema Versioning](robot-schema-versioning.md) (bd-j9jo3.1.3)
- [Freshness Contract](freshness-degraded-state-contract.md) (bd-j9jo3.1.4)
- [Incident Taxonomy](incident-taxonomy.md) (bd-j9jo3.5.1)

---

## Changelog

| Date | Change |
|------|--------|
| 2026-03-22 | Initial registry design ratified (bd-j9jo3.7.1) |
