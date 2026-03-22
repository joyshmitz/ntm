# Robot Schema Versioning Contract

> **Authoritative reference** for schema identifiers, versioning rules, and checked-in artifacts.
> All robot surfaces MUST follow this contract.

**Status:** AUTHORITATIVE
**Bead:** bd-j9jo3.1.3
**Created:** 2026-03-22

---

## 1. Purpose

This document defines how robot responses advertise schema identity, how schemas evolve, and how checked-in artifacts stay aligned with code. The goal is to prevent drift between Go structs, capabilities output, documentation, and machine consumers.

**Design Goals:**
1. Machine consumers can detect schema changes without parsing content
2. Breaking changes are explicitly versioned and discoverable
3. Checked-in artifacts serve as contract anchors, not generated noise
4. Reflective generation is insufficient for long-lived stewardship

---

## 2. Schema Identity

### 2.1 Naming Convention

All schema identifiers follow this format:

```
ntm:<category>:<surface>:v<major>

Examples:
  ntm:robot:snapshot:v1
  ntm:robot:status:v1
  ntm:robot:events:v1
  ntm:section:sessions:v1
  ntm:section:alerts:v1
  ntm:envelope:response:v1
```

**Components:**
- `ntm` — Namespace prefix (always "ntm")
- `<category>` — One of: `robot`, `section`, `envelope`, `event`
- `<surface>` — Surface or section name in lowercase
- `v<major>` — Major version number only

### 2.2 Envelope vs Payload

Every robot response has TWO schema identities:

1. **Envelope schema** — The outer wrapper (`RobotResponse`)
2. **Payload schema** — The surface-specific content

```json
{
  "success": true,
  "timestamp": "2026-03-22T03:30:00Z",
  "schema_id": "ntm:robot:snapshot:v1",
  "envelope_version": "1.0.0",
  ...
}
```

**Envelope version** uses semantic versioning (MAJOR.MINOR.PATCH) for fine-grained tracking.

**Payload schema_id** uses major-version-only for surface identity.

### 2.3 Section Schemas

Each projection section (see `robot-projection-sections.md`) has its own schema:

```go
type SessionsSection struct {
    SchemaID string `json:"schema_id"` // "ntm:section:sessions:v1"
    Sessions []SessionInfo `json:"sessions"`
    ...
}
```

Sections embedded in surfaces inherit the parent surface's schema_id by default. Explicit section schema_id is OPTIONAL unless the section can be requested standalone.

---

## 3. Versioning Rules

### 3.1 Change Classification

| Change Type | Classification | Version Bump |
|-------------|---------------|--------------|
| Add optional field | Additive | MINOR |
| Add required field with default | Additive | MINOR |
| Add new enum value | Additive | MINOR |
| Rename field | Breaking | MAJOR |
| Remove field | Breaking | MAJOR |
| Change field type | Breaking | MAJOR |
| Change field semantics | Breaking | MAJOR |
| Add required field without default | Breaking | MAJOR |
| Remove enum value | Breaking | MAJOR |

### 3.2 Version Bump Rules

**PATCH (0.0.x):** Bug fixes, documentation clarifications, no schema changes.

**MINOR (0.x.0):** Additive changes only:
- New optional fields
- New surfaces
- New enum values
- New sections (optional)

**MAJOR (x.0.0):** Breaking changes:
- Field removal or rename
- Type changes
- Semantic changes
- Required field additions

### 3.3 Deprecation Protocol

1. **Announce** — Add `@deprecated` annotation in code, update CHANGELOG
2. **Warn** — Include `_deprecated` marker in JSON for deprecated fields
3. **Support** — Maintain for minimum 2 minor versions
4. **Remove** — Increment major version when removing

```json
{
  "old_field": "value",
  "_deprecated": {
    "old_field": {
      "message": "Use new_field instead",
      "remove_in": "v2",
      "alternative": "new_field"
    }
  }
}
```

---

## 4. Checked-In Artifacts

### 4.1 Artifact Layout

```
docs/
├── schemas/
│   ├── robot/
│   │   ├── snapshot.v1.json      # JSON Schema
│   │   ├── snapshot.v1.example.json
│   │   ├── status.v1.json
│   │   ├── status.v1.example.json
│   │   ├── events.v1.json
│   │   └── ...
│   ├── sections/
│   │   ├── sessions.v1.json
│   │   ├── alerts.v1.json
│   │   └── ...
│   └── envelope/
│       └── response.v1.json
├── robot-surface-taxonomy.md
├── robot-projection-sections.md
├── robot-schema-versioning.md     # This document
└── robot-api-design.md
```

### 4.2 JSON Schema Files

Each surface and section has a checked-in JSON Schema:

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://github.com/Dicklesworthstone/ntm/docs/schemas/robot/snapshot.v1.json",
  "title": "NTM Robot Snapshot v1",
  "description": "Complete system state for AI orchestration bootstrap",
  "type": "object",
  "properties": {
    "success": { "type": "boolean" },
    "timestamp": { "type": "string", "format": "date-time" },
    "schema_id": { "const": "ntm:robot:snapshot:v1" },
    "envelope_version": { "type": "string", "pattern": "^[0-9]+\\.[0-9]+\\.[0-9]+$" },
    "sessions": { "$ref": "../sections/sessions.v1.json" },
    ...
  },
  "required": ["success", "timestamp", "schema_id"]
}
```

### 4.3 Example Files

Every schema has a companion `.example.json` showing a realistic payload:

```json
// snapshot.v1.example.json
{
  "success": true,
  "timestamp": "2026-03-22T03:30:00Z",
  "schema_id": "ntm:robot:snapshot:v1",
  "envelope_version": "1.0.0",
  "sessions": [
    {
      "name": "myproject",
      "attached": true,
      "agents": [...]
    }
  ],
  ...
}
```

### 4.4 Generation vs Manual Curation

**JSON Schemas are MANUALLY curated, not auto-generated.**

Rationale:
1. Auto-generated schemas drift as code changes
2. Contract stability requires explicit decisions about what's public
3. Manual curation forces review of schema changes
4. Generated schemas lack semantic annotations

The Go code is the implementation; the JSON Schema is the contract. They may diverge intentionally (e.g., internal fields omitted from schema).

### 4.5 Validation

Schema validation runs in CI:

```bash
# Validate examples against schemas
go test -run TestSchemaCompliance ./internal/robot/...

# Regenerate capabilities output
ntm --robot-capabilities > docs/schemas/capabilities.json
git diff --exit-code docs/schemas/capabilities.json
```

---

## 5. Capabilities Output

`--robot-capabilities` returns machine-discoverable API metadata:

```json
{
  "success": true,
  "schema_id": "ntm:robot:capabilities:v1",
  "envelope_version": "1.0.0",
  "ntm_version": "0.15.2",
  "surfaces": [
    {
      "name": "snapshot",
      "schema_id": "ntm:robot:snapshot:v1",
      "lane": "bootstrap",
      "description": "Complete system state for bootstrap",
      "flags": ["--robot-snapshot"],
      "parameters": [],
      "returns": ["sessions", "beads_summary", "alerts", "attention_summary"]
    },
    ...
  ],
  "sections": [
    {
      "name": "sessions",
      "schema_id": "ntm:section:sessions:v1",
      "description": "Session and agent inventory"
    },
    ...
  ],
  "error_codes": [...],
  "schema_versions": {
    "envelope": "1.0.0",
    "snapshot": "1.0.0",
    "status": "1.0.0",
    ...
  }
}
```

---

## 6. Go Implementation

### 6.1 Schema Constants

```go
package robot

const (
    // Envelope
    EnvelopeSchemaVersion = "1.0.0"
    EnvelopeSchemaID      = "ntm:envelope:response:v1"

    // Surfaces
    SnapshotSchemaID = "ntm:robot:snapshot:v1"
    StatusSchemaID   = "ntm:robot:status:v1"
    EventsSchemaID   = "ntm:robot:events:v1"
    AttentionSchemaID = "ntm:robot:attention:v1"
    DigestSchemaID   = "ntm:robot:digest:v1"

    // Sections
    SessionsSectionSchemaID     = "ntm:section:sessions:v1"
    AlertsSectionSchemaID       = "ntm:section:alerts:v1"
    AttentionSectionSchemaID    = "ntm:section:attention:v1"
    SourceHealthSectionSchemaID = "ntm:section:source_health:v1"
)
```

### 6.2 Schema Embedding

```go
type RobotResponse struct {
    Success         bool   `json:"success"`
    Timestamp       string `json:"timestamp"`
    SchemaID        string `json:"schema_id,omitempty"`
    EnvelopeVersion string `json:"envelope_version,omitempty"`
    ...
}

func NewRobotResponse(success bool, schemaID string) RobotResponse {
    return RobotResponse{
        Success:         success,
        Timestamp:       FormatTimestamp(time.Now()),
        SchemaID:        schemaID,
        EnvelopeVersion: EnvelopeSchemaVersion,
    }
}
```

### 6.3 Schema Registration

A registry tracks all schemas for capabilities output:

```go
package robot

var schemaRegistry = map[string]SchemaInfo{
    SnapshotSchemaID: {
        Surface:     "snapshot",
        Lane:        "bootstrap",
        Description: "Complete system state for bootstrap",
        Sections:    []string{"sessions", "work", "alerts", "attention"},
    },
    ...
}

func GetSchemaInfo(id string) (SchemaInfo, bool) {
    info, ok := schemaRegistry[id]
    return info, ok
}
```

---

## 7. Evolution Examples

### 7.1 Adding an Optional Field

```go
// v1.0.0
type SnapshotOutput struct {
    Sessions []SessionInfo `json:"sessions"`
}

// v1.1.0 - MINOR bump
type SnapshotOutput struct {
    Sessions       []SessionInfo `json:"sessions"`
    Ensemble       *EnsembleInfo `json:"ensemble,omitempty"` // NEW
}
```

**Actions:**
1. Update Go struct with `omitempty`
2. Bump envelope_version to 1.1.0
3. Update JSON Schema (add property, don't add to required)
4. Update example file
5. Update CHANGELOG

### 7.2 Removing a Field

```go
// v1.2.0
type SnapshotOutput struct {
    Sessions []SessionInfo `json:"sessions"`
    OldField string        `json:"old_field"` // DEPRECATED
}

// v2.0.0 - MAJOR bump
type SnapshotOutput struct {
    Sessions []SessionInfo `json:"sessions"`
    // old_field removed
}
```

**Actions:**
1. Add deprecation marker in v1.2.0
2. Document in CHANGELOG
3. Support for 2+ minor versions
4. Remove in v2.0.0 with MAJOR bump
5. Update schema_id to `ntm:robot:snapshot:v2`
6. Create new schema file `snapshot.v2.json`
7. Keep `snapshot.v1.json` for reference

### 7.3 Semantic Change

If field semantics change (e.g., `output_age_sec` becomes `output_age_ms`):

1. Add new field with correct semantics
2. Deprecate old field
3. After deprecation period, remove old field in MAJOR bump

---

## 8. Why Not Auto-Generation?

Reflective schema generation from Go structs is tempting but insufficient:

1. **Drift without review** — Code changes silently become contract changes
2. **Internal fields exposed** — Private implementation details leak
3. **No semantic annotations** — Descriptions, examples, deprecation notes lost
4. **Version chaos** — Every commit potentially changes the schema
5. **No stability anchor** — Consumers can't rely on contract stability

Manual curation forces:
- Explicit decisions about public API
- Review of breaking changes
- Documentation alongside schema
- Intentional versioning

---

## 9. Non-Goals

1. **GraphQL/OpenAPI generation** — Robot mode uses JSON, not REST conventions
2. **Runtime schema validation** — Validation is for testing, not production
3. **Auto-versioning** — Version bumps are human decisions
4. **Per-consumer schemas** — One schema per surface, all consumers use it

---

## 10. References

- [Robot Surface Taxonomy](robot-surface-taxonomy.md) — Lane definitions
- [Robot Projection Sections](robot-projection-sections.md) — Section schemas
- [Robot API Design](robot-api-design.md) — Naming conventions

---

## Appendix: Changelog

- **2026-03-22:** Initial schema versioning contract (bd-j9jo3.1.3)

---

*Reference: bd-j9jo3.1.3*
