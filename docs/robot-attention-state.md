# Robot Attention State Semantics

This document defines operator attention state, acknowledgment, snooze, pin, and override semantics for ntm robot mode.

## Overview

Attention items surface through the attention feed and digest surfaces. Once surfaced, operators and agents can intentionally shape the attention loop through explicit state transitions. This contract defines what each state means, how transitions work, and how state affects resurfacing.

## Attention States

Each attention item has an attention state that tracks operator/agent interaction:

```
┌─────────┐     view      ┌────────┐
│   NEW   │──────────────>│  SEEN  │
└─────────┘               └────────┘
     │                         │
     │         ┌───────────────┤
     │         │               │
     v         v               v
┌─────────────────┐    ┌─────────────┐
│   SNOOZED       │    │ ACKNOWLEDGED│
│ (until: time/   │    └─────────────┘
│  condition)     │           │
└─────────────────┘           │
     │                        v
     │                 ┌─────────────┐
     └────────────────>│  DISMISSED  │
                       └─────────────┘

           ┌──────────┐
           │  PINNED  │  (orthogonal to other states)
           └──────────┘
```

### State Definitions

| State | Code | Meaning |
|-------|------|---------|
| `new` | `ATT_STATE_NEW` | Item never seen by any consumer |
| `seen` | `ATT_STATE_SEEN` | Item rendered/fetched but not explicitly acknowledged |
| `acknowledged` | `ATT_STATE_ACK` | Operator confirmed awareness; suppress re-raise for this instance |
| `snoozed` | `ATT_STATE_SNOOZED` | Temporarily hidden until time/condition |
| `dismissed` | `ATT_STATE_DISMISSED` | Explicitly removed from attention; do not resurface unless re-triggered |
| `pinned` | `ATT_STATE_PINNED` | Elevated priority; always show regardless of ranking |

### State Properties

```json
{
  "attention_state": {
    "state": "snoozed",
    "state_code": "ATT_STATE_SNOOZED",
    "transitioned_at": "2026-03-22T04:00:00Z",
    "transitioned_by": "PeachPond",
    "actor_type": "agent",
    "snooze_until": "2026-03-22T05:00:00Z",
    "snooze_condition": null,
    "pinned": false,
    "pin_reason": null,
    "muted_classes": [],
    "override_priority": null,
    "resurfacing_policy": "on_change"
  }
}
```

## State Transitions

### Automatic Transitions

| Trigger | From | To | Condition |
|---------|------|----|-----------|
| Item created | - | `new` | Always |
| Surface fetched | `new` | `seen` | Item included in response |
| Snooze expired | `snoozed` | `new` | `now >= snooze_until` |
| Snooze condition met | `snoozed` | `new` | Condition evaluates true |
| Item changed | `acknowledged` | `new` | Change fingerprint differs AND `resurfacing_policy != "never"` |

### Explicit Transitions

Operators and agents trigger these via attention commands:

| Action | Valid From | To | Command |
|--------|------------|----|---------|
| `acknowledge` | `new`, `seen` | `acknowledged` | `ntm attention ack <ref>` |
| `snooze` | `new`, `seen`, `acknowledged` | `snoozed` | `ntm attention snooze <ref> --until <time>` |
| `dismiss` | any | `dismissed` | `ntm attention dismiss <ref>` |
| `pin` | any | (same) + pinned | `ntm attention pin <ref>` |
| `unpin` | pinned | (same) - pinned | `ntm attention unpin <ref>` |
| `escalate` | any | (same) + priority override | `ntm attention escalate <ref>` |
| `restore` | `dismissed`, `snoozed` | `new` | `ntm attention restore <ref>` |

## Acknowledgment Semantics

Acknowledgment (`ack`) signals operator awareness without dismissal:

```json
{
  "action": "acknowledge",
  "ref": "alert:agent_stuck:pane-abc123",
  "fingerprint": "fp-2026032204000-abc",
  "scope": "instance",
  "request_id": "req_20260322040000_x7k"
}
```

### Acknowledgment Scopes

| Scope | Meaning | Resurface When |
|-------|---------|----------------|
| `instance` | This specific occurrence | New occurrence with different fingerprint |
| `class` | All items matching type/source | Never for this class (use `mute` instead) |

### Resurfacing After Acknowledgment

An acknowledged item resurfaces when:

1. **Fingerprint changes**: The underlying state changed materially
2. **Severity escalates**: Item moved to higher severity tier
3. **Duration threshold**: Item exceeded its "still happening" threshold
4. **Related incident**: A new incident references this item

## Snooze Semantics

Snooze temporarily hides items from attention surfaces.

### Snooze Until Time

```json
{
  "action": "snooze",
  "ref": "alert:disk_low:pane-abc123",
  "until": "2026-03-22T08:00:00Z",
  "reason": "will clean up after standup"
}
```

The item reappears at or after the specified time. Time is always UTC.

### Snooze Until Condition

```json
{
  "action": "snooze",
  "ref": "alert:disk_low:pane-abc123",
  "until_condition": {
    "type": "metric_threshold",
    "metric": "disk_usage_pct",
    "operator": "lt",
    "value": 80
  },
  "reason": "will resurface if disk fills further"
}
```

Supported condition types:

| Type | Meaning |
|------|---------|
| `metric_threshold` | Resurface when metric crosses threshold |
| `state_change` | Resurface when underlying state changes |
| `incident_opened` | Resurface when related incident opens |
| `time_elapsed` | Resurface after duration since snooze |

### Snooze Escalation Override

Items automatically unsnooze when:

1. **Severity escalates** to critical
2. **Related incident** is opened
3. **Repeat count** exceeds threshold (default: 3)

```json
{
  "snooze_override": {
    "reason": "severity_escalation",
    "original_severity": "warning",
    "current_severity": "critical",
    "override_at": "2026-03-22T04:30:00Z"
  }
}
```

## Pin Semantics

Pinning elevates priority and ensures visibility regardless of ranking.

### Pin Properties

```json
{
  "pinned": true,
  "pin_metadata": {
    "pinned_at": "2026-03-22T04:00:00Z",
    "pinned_by": "PeachPond",
    "actor_type": "agent",
    "pin_reason": "tracking deployment progress",
    "pin_expires": null,
    "pin_scope": "session"
  }
}
```

### Pin Scope

| Scope | Visibility |
|-------|------------|
| `session` | Pinned in current session only |
| `project` | Pinned for all consumers in project |
| `global` | Pinned across all sessions (rare) |

### Pin Effects

1. **Always visible**: Pinned items appear in attention/digest regardless of ranking
2. **Top positioning**: Pinned items sort before unpinned at same severity
3. **No auto-dismiss**: Pinned items never auto-dismiss due to age or resolution
4. **Explicit unpin required**: Must be explicitly unpinned

## Mute Semantics

Muting suppresses a class of items by type, source, or fingerprint pattern.

```json
{
  "action": "mute",
  "mute_rule": {
    "type": "alert:disk_low",
    "source": "pane-*",
    "duration": "4h",
    "reason": "known issue during migration"
  }
}
```

### Mute Scope

| Field | Pattern Support | Example |
|-------|-----------------|---------|
| `type` | Exact or prefix | `alert:disk_*` |
| `source` | Glob pattern | `pane-abc*` |
| `severity` | Exact | `warning` |
| `fingerprint` | Exact | `fp-2026032204000-abc` |

### Mute vs Dismiss

| Aspect | Mute | Dismiss |
|--------|------|---------|
| Scope | Class/pattern | Single item |
| Duration | Time-bounded | Permanent |
| New items | Suppressed | Not affected |
| Unmute | Required to see again | Resurface on change |

## Manual Escalation

Operators can override automatic priority:

```json
{
  "action": "escalate",
  "ref": "alert:context_warning:pane-abc123",
  "override_priority": "critical",
  "escalation_reason": "agent about to hit context limit on critical task",
  "escalation_expires": "2026-03-22T06:00:00Z"
}
```

### Escalation Effects

1. **Priority override**: Item ranks as specified priority
2. **Incident candidate**: Escalated items may auto-promote to incident
3. **Notification trigger**: May trigger additional notifications
4. **Time-bounded**: Escalation expires to prevent stale overrides

## Bulk Operations

Operations can target multiple items:

```json
{
  "action": "acknowledge_bulk",
  "refs": ["alert:*:pane-abc123"],
  "pattern_type": "glob",
  "dry_run": false
}
```

### Supported Bulk Actions

| Action | Meaning |
|--------|---------|
| `acknowledge_bulk` | Acknowledge all matching items |
| `dismiss_bulk` | Dismiss all matching items |
| `snooze_bulk` | Snooze all matching items |
| `restore_bulk` | Restore all matching dismissed/snoozed items |
| `mute` | Create mute rule for pattern |

## Persistence

Attention state persists across:

1. **Session restarts**: State stored in SQLite
2. **Transport switches**: State shared across CLI/REST/SSE/WebSocket
3. **Agent handoffs**: State visible to all consumers

### Persistence Schema

```sql
CREATE TABLE attention_state (
  ref TEXT PRIMARY KEY,
  state TEXT NOT NULL DEFAULT 'new',
  state_code TEXT NOT NULL DEFAULT 'ATT_STATE_NEW',
  transitioned_at TEXT NOT NULL,
  transitioned_by TEXT,
  actor_type TEXT, -- 'human' | 'agent'
  pinned INTEGER NOT NULL DEFAULT 0,
  pin_metadata TEXT, -- JSON
  snooze_until TEXT,
  snooze_condition TEXT, -- JSON
  mute_rule_id TEXT REFERENCES mute_rules(id),
  override_priority TEXT,
  escalation_metadata TEXT, -- JSON
  resurfacing_policy TEXT DEFAULT 'on_change',
  fingerprint TEXT,
  updated_at TEXT NOT NULL
);

CREATE TABLE mute_rules (
  id TEXT PRIMARY KEY,
  type_pattern TEXT,
  source_pattern TEXT,
  severity TEXT,
  fingerprint_pattern TEXT,
  created_at TEXT NOT NULL,
  created_by TEXT,
  expires_at TEXT,
  reason TEXT
);
```

## Surface Integration

### Attention Feed

Items filtered by attention state:

```json
{
  "attention_feed": {
    "items": [...],
    "filter": {
      "exclude_states": ["dismissed", "snoozed"],
      "include_pinned": true,
      "respect_mutes": true
    }
  }
}
```

### Digest

Includes attention state summary:

```json
{
  "digest": {
    "attention_summary": {
      "new": 3,
      "seen": 5,
      "acknowledged": 12,
      "snoozed": 2,
      "pinned": 1,
      "muted_active_rules": 1
    }
  }
}
```

### Status

Brief attention state in status:

```json
{
  "status": {
    "attention": {
      "requires_action": 3,
      "pinned": 1
    }
  }
}
```

## Explainability

Each item includes explanation of why it's shown/hidden:

```json
{
  "explanation": {
    "type": "attention_state",
    "short": "Item pinned by operator",
    "code": "EXPLAIN_ATT_PINNED",
    "evidence": {
      "pinned_by": "PeachPond",
      "pinned_at": "2026-03-22T04:00:00Z",
      "pin_reason": "tracking deployment"
    }
  }
}
```

### Explanation Codes

| Code | Meaning |
|------|---------|
| `EXPLAIN_ATT_NEW` | Item is new, never seen |
| `EXPLAIN_ATT_SEEN` | Item was seen but not acknowledged |
| `EXPLAIN_ATT_ACK` | Item acknowledged, showing due to change |
| `EXPLAIN_ATT_SNOOZED` | Item snoozed until time/condition |
| `EXPLAIN_ATT_DISMISSED` | Item dismissed (not normally shown) |
| `EXPLAIN_ATT_PINNED` | Item pinned, always shown |
| `EXPLAIN_ATT_MUTED` | Item matches active mute rule |
| `EXPLAIN_ATT_ESCALATED` | Item manually escalated |
| `EXPLAIN_ATT_RESURFACED` | Item resurfaced after change |

## Transport Consistency

All transports expose the same attention state semantics:

| Transport | State Read | State Write |
|-----------|------------|-------------|
| CLI | `--robot-attention` | `ntm attention ack/snooze/pin/...` |
| REST | `GET /api/attention/state` | `POST /api/attention/actions` |
| SSE | `attention_state` events | N/A (read-only) |
| WebSocket | `attention_state` messages | `attention_action` messages |

## Design Rationale

1. **Explicit over implicit**: Operators should know why items appear or disappear
2. **Bounded complexity**: Five core states, not a preference engine
3. **Resurface safety**: Changed items reappear by default; explicit dismiss to suppress
4. **Actor attribution**: Track who/what made each transition
5. **Transport neutrality**: Same semantics everywhere
