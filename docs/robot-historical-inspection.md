# Robot Historical Inspection Semantics

This document defines bounded as-of inspection and incident-replay semantics for ntm robot mode surfaces.

## Overview

Operators and agents need to answer historical questions: "What did ntm believe when this incident opened?" or "What did this section look like 10 minutes ago?" This contract defines how ntm supports bounded historical inspection without becoming a full analytics warehouse.

## Design Principles

1. **Operator-focused**: Support debugging and post-mortem, not analytics
2. **Bounded scope**: Recent history only, not arbitrary time ranges
3. **Incident-centric**: Incidents are primary replay anchors
4. **Confidence-aware**: Always report staleness and completeness
5. **On-demand**: Reconstruct when needed, cache when efficient

## Historical Query Types

### As-Of Queries

Request state at a specific recent timestamp:

```json
{
  "action": "inspect",
  "request_id": "req_20260322040000_x7k",
  "as_of": {
    "type": "timestamp",
    "value": "2026-03-22T03:50:00Z"
  },
  "sections": ["quota", "alerts", "agents"]
}
```

### Incident-Centric Queries

Request state around incident boundaries:

```json
{
  "action": "inspect",
  "request_id": "req_20260322040000_x7k",
  "incident_replay": {
    "incident_ref": "incident:inc-20260322-abc",
    "boundary": "opened",
    "window_before_ms": 300000,
    "window_after_ms": 60000
  }
}
```

### Range Queries

Request events within a bounded time range:

```json
{
  "action": "inspect",
  "request_id": "req_20260322040000_x7k",
  "range": {
    "start": "2026-03-22T03:45:00Z",
    "end": "2026-03-22T04:00:00Z",
    "sections": ["attention"],
    "limit": 100
  }
}
```

## Supported Time Windows

### Retention Limits

| Data Type | Default Retention | Max Retention |
|-----------|-------------------|---------------|
| Projection snapshots | 1 hour | 24 hours |
| Attention events | 24 hours | 7 days |
| Incident state | 7 days | 30 days |
| Agent state changes | 6 hours | 24 hours |
| Pane output samples | 30 minutes | 2 hours |
| Quota readings | 1 hour | 6 hours |

### Window Boundaries

```json
{
  "historical_limits": {
    "as_of_max_age_ms": 86400000,
    "incident_replay_max_window_ms": 3600000,
    "range_query_max_span_ms": 900000,
    "output_sample_retention_ms": 1800000
  }
}
```

## Replay Targets

### Target Reference Types

| Reference | Format | Example |
|-----------|--------|---------|
| Incident | `incident:<id>` | `incident:inc-20260322-abc` |
| Attention item | `attention:<fingerprint>` | `attention:fp-20260322040000` |
| Session | `session:<name>` | `session:main` |
| Agent | `agent:<name>` | `agent:PeachPond` |
| Alert | `alert:<id>` | `alert:alt-20260322-xyz` |

### Target Resolution

```json
{
  "replay_target": {
    "ref": "incident:inc-20260322-abc",
    "resolved": true,
    "resolution": {
      "type": "incident",
      "id": "inc-20260322-abc",
      "opened_at": "2026-03-22T03:45:00Z",
      "closed_at": "2026-03-22T04:15:00Z",
      "state_at_open": "available",
      "state_at_close": "available"
    }
  }
}
```

## Incident Replay

### Incident Boundaries

```
    window_before          incident           window_after
    ◄────────────────►◄───────────────────►◄───────────────►

    ┌─────────┐    ┌─────────────────────────┐    ┌─────────┐
    │ CONTEXT │    │       INCIDENT          │    │ OUTCOME │
    │  STATE  │    │                         │    │  STATE  │
    └─────────┘    └─────────────────────────┘    └─────────┘
         ▲              ▲               ▲              ▲
      as_of:        opened_at      resolved_at     as_of:
    opened-5m                                    resolved+1m
```

### Replay Request

```json
{
  "action": "incident_replay",
  "request_id": "req_20260322040000_x7k",
  "incident_ref": "incident:inc-20260322-abc",
  "include": {
    "state_at_open": true,
    "state_at_close": true,
    "events_during": true,
    "context_window": {
      "before_ms": 300000,
      "after_ms": 60000
    }
  },
  "sections": ["quota", "alerts", "agents", "attention"]
}
```

### Replay Response

```json
{
  "incident_replay": {
    "incident": {
      "ref": "incident:inc-20260322-abc",
      "type": "agent_crashed",
      "severity": "P1",
      "opened_at": "2026-03-22T03:45:00Z",
      "resolved_at": "2026-03-22T04:15:00Z"
    },
    "state_at_open": {
      "timestamp": "2026-03-22T03:45:00Z",
      "reconstruction": "snapshot",
      "confidence": "high",
      "sections": {
        "quota": {...},
        "alerts": {...},
        "agents": {...}
      }
    },
    "state_at_close": {
      "timestamp": "2026-03-22T04:15:00Z",
      "reconstruction": "snapshot",
      "confidence": "high",
      "sections": {...}
    },
    "events_during": {
      "count": 47,
      "events": [...],
      "truncated": false
    },
    "context": {
      "before": {...},
      "after": {...}
    }
  }
}
```

## Reconstruction Methods

### Snapshot vs Event Replay

| Method | When Used | Accuracy | Performance |
|--------|-----------|----------|-------------|
| Snapshot | Recent, cached | Exact | Fast |
| Event replay | Older, uncached | Reconstructed | Slower |
| Hybrid | Gap in snapshots | Best-effort | Variable |

### Reconstruction Metadata

```json
{
  "reconstruction": {
    "method": "event_replay",
    "source": "attention_journal",
    "events_replayed": 127,
    "gaps_detected": 0,
    "interpolations": 0,
    "started_at": "2026-03-22T04:00:00Z",
    "completed_at": "2026-03-22T04:00:00.500Z",
    "duration_ms": 500
  }
}
```

### Reconstruction Confidence

| Level | Meaning | Conditions |
|-------|---------|------------|
| `high` | Exact state known | Snapshot available or full event replay |
| `medium` | Mostly accurate | Minor gaps filled by interpolation |
| `low` | Approximate | Significant gaps or stale sources |
| `unavailable` | Cannot reconstruct | Data expired or corrupted |

## Staleness Reporting

### Staleness Metadata

```json
{
  "staleness": {
    "requested_at": "2026-03-22T03:50:00Z",
    "actual_at": "2026-03-22T03:50:00Z",
    "drift_ms": 0,
    "age_at_request_ms": 600000,
    "source_staleness": {
      "quota": {"stale": false, "age_ms": 5000},
      "alerts": {"stale": false, "age_ms": 2000},
      "agents": {"stale": true, "age_ms": 120000, "reason": "poll_failed"}
    },
    "confidence": "high",
    "warnings": []
  }
}
```

### Staleness Warnings

| Warning | Meaning |
|---------|---------|
| `STALE_SOURCE` | One or more sources were stale at requested time |
| `RECONSTRUCTED` | State was reconstructed, not from snapshot |
| `INTERPOLATED` | Gaps filled by interpolation |
| `PARTIAL_DATA` | Some sections unavailable |
| `NEAR_RETENTION_EDGE` | Data near retention boundary |
| `POLICY_CHANGED` | Redaction policy changed since event time |

## Partial History

### Unavailable Data

```json
{
  "partial_history": {
    "requested_sections": ["quota", "alerts", "agents", "panes"],
    "available_sections": ["quota", "alerts", "agents"],
    "unavailable_sections": [
      {
        "section": "panes",
        "reason": "retention_expired",
        "last_available_at": "2026-03-22T03:30:00Z",
        "retention_limit_ms": 1800000
      }
    ],
    "completeness_pct": 75
  }
}
```

### Degraded Reconstruction

```json
{
  "degraded_reconstruction": {
    "section": "agents",
    "reason": "source_gap",
    "gap_start": "2026-03-22T03:48:00Z",
    "gap_end": "2026-03-22T03:52:00Z",
    "gap_duration_ms": 240000,
    "interpolation_applied": true,
    "confidence": "low"
  }
}
```

## Post-Mortem Export

### Export Request

```json
{
  "action": "export_postmortem",
  "request_id": "req_20260322040000_x7k",
  "incident_ref": "incident:inc-20260322-abc",
  "format": "json",
  "include": {
    "incident_summary": true,
    "state_snapshots": true,
    "event_timeline": true,
    "attention_trail": true,
    "action_log": true,
    "diagnostic_evidence": true
  },
  "redaction": {
    "apply_current_policy": true,
    "include_hashes": true
  }
}
```

### Export Response

```json
{
  "export": {
    "format": "json",
    "generated_at": "2026-03-22T04:30:00Z",
    "incident_ref": "incident:inc-20260322-abc",
    "size_bytes": 45000,
    "sections_included": 6,
    "redactions_applied": 3,
    "retention_until": "2026-03-29T04:30:00Z",
    "download_url": null,
    "inline_content": {...}
  }
}
```

### Export Formats

| Format | Use Case |
|--------|----------|
| `json` | Machine consumption, further processing |
| `markdown` | Human-readable reports |
| `timeline` | Chronological event view |

## API Integration

### CLI

```bash
# As-of inspection
ntm inspect --robot --as-of "2026-03-22T03:50:00Z" --section quota,alerts

# Incident replay
ntm inspect --robot --incident inc-20260322-abc --replay

# Range query
ntm inspect --robot --from "2026-03-22T03:45:00Z" --to "2026-03-22T04:00:00Z"

# Post-mortem export
ntm export --robot --incident inc-20260322-abc --format markdown
```

### REST

```http
GET /api/robot/inspect?as_of=2026-03-22T03:50:00Z&sections=quota,alerts HTTP/1.1

GET /api/robot/incidents/inc-20260322-abc/replay HTTP/1.1

POST /api/robot/export HTTP/1.1
Content-Type: application/json
{
  "incident_ref": "incident:inc-20260322-abc",
  "format": "json"
}
```

## Contract Integration

### Identity Contract

Replay uses stable references:

```json
{
  "replay_target": {
    "ref": "incident:inc-20260322-abc",
    "resolved_via": "identity_registry"
  }
}
```

### Explainability Contract

Reconstruction includes explanation:

```json
{
  "explanation": {
    "type": "reconstruction",
    "short": "State reconstructed from event replay",
    "code": "EXPLAIN_RECONSTRUCTED",
    "evidence": {
      "method": "event_replay",
      "events_replayed": 127,
      "gaps": 0
    }
  }
}
```

### Sensitivity Contract

Historical data respects current policy:

```json
{
  "sensitivity": {
    "policy_at_event": "v2.0",
    "policy_current": "v2.1",
    "retroactive_redactions": 2,
    "disclosure_note": "Additional patterns redacted by v2.1"
  }
}
```

### Boundedness Contract

Historical queries respect limits:

```json
{
  "boundedness": {
    "requested_range_ms": 900000,
    "allowed_range_ms": 900000,
    "events_requested": "unlimited",
    "events_limit": 1000,
    "truncated": false
  }
}
```

## Error Handling

### Historical Query Errors

| Code | Meaning | Recovery |
|------|---------|----------|
| `TIMESTAMP_TOO_OLD` | Requested time beyond retention | Query within limits |
| `INCIDENT_NOT_FOUND` | Incident reference invalid | Check incident ID |
| `RECONSTRUCTION_FAILED` | Cannot rebuild state | Use partial data |
| `RANGE_TOO_WIDE` | Query span exceeds limit | Narrow range |
| `SOURCE_UNAVAILABLE` | Historical source missing | Accept partial |

### Error Response

```json
{
  "error": {
    "code": "TIMESTAMP_TOO_OLD",
    "message": "Requested timestamp 2026-03-21T00:00:00Z is beyond 24h retention limit",
    "requested_at": "2026-03-21T00:00:00Z",
    "oldest_available": "2026-03-21T04:00:00Z",
    "retention_limit_ms": 86400000,
    "suggestion": "Query for more recent timestamp"
  }
}
```

## Design Rationale

1. **Bounded, not unbounded**: Explicit retention limits prevent storage bloat
2. **Incident-centric**: Incidents are natural replay anchors for debugging
3. **Confidence-aware**: Always report reconstruction quality
4. **Retroactive policy**: Current sensitivity rules apply to history
5. **On-demand first**: Reconstruct when needed, cache for efficiency
6. **Graceful degradation**: Partial data better than no data
