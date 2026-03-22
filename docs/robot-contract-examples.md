# Robot Contract Examples

This document provides normative payload examples and operator-loop transcripts that make the robot contract concrete. These examples serve as executable contract artifacts for tests, docs, and validation.

## Happy Path Examples

### Snapshot (Bootstrap Surface)

```json
{
  "schema_id": "ntm:snapshot:v1.0",
  "schema_version": "1.0.0",
  "generated_at": "2026-03-22T04:00:00.123Z",
  "request_id": "req_20260322040000_x7k",
  "meta": {
    "freshness": {
      "all_fresh": true,
      "staleness_seconds": 0
    },
    "completeness": {
      "complete": true,
      "sections_available": 6,
      "sections_total": 6
    }
  },
  "session": {
    "name": "main",
    "started_at": "2026-03-22T03:00:00Z",
    "panes_total": 4,
    "panes_active": 3,
    "panes_idle": 1
  },
  "quota": {
    "available": true,
    "accounts": [
      {
        "provider": "anthropic",
        "tier": "tier4",
        "tokens_used_pct": 45.2,
        "requests_used_pct": 32.1,
        "status": "ok",
        "reason_code": "quota:ok"
      }
    ],
    "summary": {
      "total_accounts": 3,
      "healthy": 3,
      "warning": 0,
      "critical": 0
    }
  },
  "alerts": {
    "available": true,
    "active": [
      {
        "id": "alt-20260322040000-abc",
        "type": "agent_stuck",
        "severity": "warning",
        "message": "Agent PeachPond idle for 5 minutes",
        "reason_code": "alert:agent:stuck",
        "pane": "pane-abc123",
        "since": "2026-03-22T03:55:00Z",
        "duration_ms": 300000,
        "count": 1
      }
    ],
    "summary": {
      "total_active": 1,
      "by_severity": {"warning": 1}
    }
  },
  "agents": {
    "available": true,
    "registered": [
      {
        "name": "PeachPond",
        "program": "claude-code",
        "model": "opus-4.6",
        "pane": "pane-abc123",
        "status": "idle",
        "reason_code": "health:agent:idle",
        "last_active": "2026-03-22T03:55:00Z"
      }
    ],
    "summary": {
      "total": 3,
      "active": 2,
      "idle": 1
    }
  },
  "work": {
    "available": true,
    "summary": {
      "total": 45,
      "open": 12,
      "in_progress": 3,
      "ready": 8,
      "blocked": 4
    },
    "ready": [
      {
        "id": "bd-j9jo3.1.5",
        "title": "define normative payload examples",
        "priority": 1,
        "score": 0.95
      }
    ]
  },
  "coordination": {
    "available": true,
    "mail": {
      "total_unread": 2,
      "urgent_unread": 0
    },
    "reservations": {
      "active": 1,
      "conflicts": 0
    }
  }
}
```

### Status (Cheap Summary)

```json
{
  "schema_id": "ntm:status:v1.0",
  "generated_at": "2026-03-22T04:00:00.050Z",
  "request_id": "req_20260322040001_xyz",
  "ok": true,
  "summary": {
    "session": "main",
    "uptime_minutes": 60,
    "agents": {"active": 2, "total": 3},
    "alerts": {"warning": 1, "critical": 0},
    "quota": {"healthy": 3, "warning": 0},
    "work": {"ready": 8, "in_progress": 3},
    "attention": {"requires_action": 1}
  },
  "attention_brief": {
    "top_item": {
      "type": "agent_stuck",
      "severity": "warning",
      "summary": "PeachPond idle 5m"
    }
  }
}
```

### Digest (Prioritized Action Queue)

```json
{
  "schema_id": "ntm:digest:v1.0",
  "generated_at": "2026-03-22T04:00:00.100Z",
  "request_id": "req_20260322040002_abc",
  "attention": {
    "requires_action": [
      {
        "ref": "alert:agent_stuck:pane-abc123",
        "type": "alert",
        "severity": "warning",
        "summary": "Agent PeachPond idle for 5 minutes",
        "actionability": "action_required",
        "since": "2026-03-22T03:55:00Z",
        "attention_state": "new",
        "explanation": {
          "short": "Agent not responding",
          "code": "EXPLAIN_AGENT_STUCK"
        },
        "suggested_actions": ["check_output", "send_interrupt"]
      }
    ],
    "interesting": [
      {
        "ref": "quota:warning:anthropic",
        "type": "quota",
        "severity": "info",
        "summary": "Anthropic at 45% tokens",
        "actionability": "interesting"
      }
    ],
    "summary": {
      "requires_action": 1,
      "interesting": 1,
      "snoozed": 0,
      "pinned": 0
    }
  },
  "work_brief": {
    "top_recommendation": {
      "id": "bd-j9jo3.1.5",
      "title": "define normative payload examples",
      "score": 0.95,
      "reasons": ["unblocks 14 tasks", "high priority"]
    }
  }
}
```

### Attention Feed

```json
{
  "schema_id": "ntm:attention:v1.0",
  "generated_at": "2026-03-22T04:00:00.150Z",
  "request_id": "req_20260322040003_def",
  "cursor": "cur_20260322040000",
  "items": [
    {
      "event_id": "evt_20260322035500_abc",
      "ref": "alert:agent_stuck:pane-abc123",
      "event_type": "attention:alert:agent_stuck",
      "severity": "warning",
      "reason_code": "alert:agent:stuck",
      "timestamp": "2026-03-22T03:55:00Z",
      "attention_state": {
        "state": "new",
        "state_code": "ATT_STATE_NEW"
      },
      "payload": {
        "agent": "PeachPond",
        "pane": "pane-abc123",
        "idle_duration_ms": 300000
      },
      "explanation": {
        "type": "agent_health",
        "short": "No output for 5 minutes",
        "code": "EXPLAIN_AGENT_STUCK",
        "evidence": {
          "last_output_at": "2026-03-22T03:55:00Z",
          "idle_threshold_ms": 300000
        }
      }
    }
  ],
  "meta": {
    "total_pending": 1,
    "filtered_out": 0,
    "cursor_position": "cur_20260322040000"
  }
}
```

## Degraded Source Scenarios

### Partial Source Failure

```json
{
  "schema_id": "ntm:snapshot:v1.0",
  "generated_at": "2026-03-22T04:00:00.200Z",
  "request_id": "req_20260322040004_ghi",
  "meta": {
    "freshness": {
      "all_fresh": false,
      "staleness_seconds": 120,
      "stale_sources": ["quota"]
    },
    "completeness": {
      "complete": false,
      "sections_available": 5,
      "sections_total": 6,
      "unavailable_sections": ["quota"]
    },
    "degradation": {
      "degraded": true,
      "reasons": [
        {
          "source": "caut",
          "reason": "poll_timeout",
          "reason_code": "health:source:stale",
          "last_success": "2026-03-22T03:58:00Z",
          "retry_at": "2026-03-22T04:00:30Z"
        }
      ]
    }
  },
  "quota": {
    "available": false,
    "reason": "source stale (120s)",
    "reason_code": "health:source:stale",
    "last_known": {
      "timestamp": "2026-03-22T03:58:00Z",
      "summary": {"healthy": 3}
    },
    "explanation": {
      "short": "Quota data stale",
      "code": "EXPLAIN_SOURCE_STALE",
      "evidence": {
        "source": "caut",
        "last_success": "2026-03-22T03:58:00Z",
        "staleness_ms": 120000
      }
    }
  },
  "alerts": {
    "available": true
  },
  "agents": {
    "available": true
  }
}
```

### Multiple Source Degradation

```json
{
  "meta": {
    "degradation": {
      "degraded": true,
      "severity": "warning",
      "reasons": [
        {
          "source": "caut",
          "reason": "poll_timeout",
          "severity": "warning"
        },
        {
          "source": "agentmail",
          "reason": "server_unavailable",
          "severity": "error"
        }
      ],
      "mitigation": {
        "using_cached": ["quota"],
        "omitted": ["coordination"],
        "interpolated": []
      }
    }
  }
}
```

## Cursor and Resync Scenarios

### Cursor Expiry

```json
{
  "error": {
    "code": "CURSOR_EXPIRED",
    "message": "Cursor cur_20260321000000 is beyond 24h retention",
    "cursor_requested": "cur_20260321000000",
    "oldest_available": "cur_20260321040000",
    "retention_limit_ms": 86400000,
    "recovery": {
      "action": "resync",
      "new_cursor": "cur_20260322040000",
      "events_missed_estimate": 127
    }
  }
}
```

### Resync Response

```json
{
  "schema_id": "ntm:attention:v1.0",
  "generated_at": "2026-03-22T04:00:00Z",
  "request_id": "req_20260322040005_jkl",
  "resync": {
    "performed": true,
    "reason": "cursor_expired",
    "previous_cursor": "cur_20260321000000",
    "new_cursor": "cur_20260322040000",
    "gap_duration_ms": 86400000,
    "events_in_gap": "unknown",
    "current_state_only": true
  },
  "items": [],
  "meta": {
    "cursor_position": "cur_20260322040000",
    "resync_complete": true
  }
}
```

## Watch and Progress Filter Scenarios

### Filtered Watch Subscription

```json
{
  "action": "watch",
  "request_id": "req_20260322040008_wat",
  "cursor": "cur_20260322035500",
  "subscription": {
    "surfaces": ["attention", "incidents"],
    "filters": {
      "session_refs": ["session:main"],
      "sources": ["tmux", "agentmail", "beads"],
      "event_types": ["attention:*", "incident:*", "control:heartbeat"],
      "severity_at_least": "warning"
    },
    "include_progress": true,
    "progress_interval_ms": 10000,
    "heartbeat_interval_ms": 5000
  }
}
```

```json
{
  "stream": {
    "stream_id": "watch_20260322040008_wat",
    "cursor_requested": "cur_20260322035500",
    "cursor_valid": true,
    "mode": "replay_then_live"
  },
  "subscription": {
    "surfaces": ["attention", "incidents"],
    "filters_applied": {
      "session_refs": ["session:main"],
      "sources": ["tmux", "agentmail", "beads"],
      "event_types": ["attention:*", "incident:*", "control:heartbeat"],
      "severity_at_least": "warning"
    },
    "include_progress": true,
    "progress_interval_ms": 10000,
    "heartbeat_interval_ms": 5000
  },
  "boundedness": {
    "replay_pending": 12,
    "suppressed_during_replay": 34,
    "suppression_reason": "filter_mismatch"
  }
}
```

### Progress Event with Source Progress

```json
{
  "event_class": "control",
  "event_type": "control:progress",
  "payload": {
    "stream_id": "watch_20260322040008_wat",
    "phase": "replay",
    "cursor_position": "cur_20260322035950",
    "replayed_events": 8,
    "replay_remaining": 4,
    "matched_events_since_last_progress": 2,
    "suppressed_events_since_last_progress": 7,
    "progress_interval_ms": 10000,
    "source_progress": [
      {
        "source": "tmux",
        "status": "healthy",
        "latest_event_at": "2026-03-22T03:59:49Z",
        "scan_lag_ms": 800
      },
      {
        "source": "agentmail",
        "status": "degraded",
        "latest_event_at": "2026-03-22T03:59:41Z",
        "scan_lag_ms": 9000,
        "reason_code": "health:source:busy"
      }
    ]
  }
}
```

### Quiet but Healthy Filtered Heartbeat

```json
{
  "event_class": "control",
  "event_type": "control:heartbeat",
  "payload": {
    "stream_id": "watch_20260322040008_wat",
    "cursor_position": "cur_20260322040008",
    "matched_events_since_last_heartbeat": 0,
    "suppressed_events_since_last_heartbeat": 3,
    "quiet_reason": "no_matching_events",
    "subscription_still_live": true,
    "next_progress_due_at": "2026-03-22T04:00:20Z"
  }
}
```

## Bounded Result Scenarios

### Truncated Output

```json
{
  "schema_id": "ntm:snapshot:v1.0",
  "work": {
    "available": true,
    "ready": [
      {"id": "bd-001", "title": "Task 1"},
      {"id": "bd-002", "title": "Task 2"},
      {"id": "bd-003", "title": "Task 3"}
    ],
    "boundedness": {
      "truncated": true,
      "shown": 3,
      "total": 25,
      "reason": "payload_budget",
      "continuation": {
        "available": true,
        "cursor": "work_cur_003",
        "command": "ntm inspect work --cursor work_cur_003"
      }
    }
  }
}
```

### Aggregated Summary

```json
{
  "alerts": {
    "summary": {
      "total_active": 47,
      "by_severity": {
        "critical": 2,
        "error": 5,
        "warning": 40
      },
      "by_type": {
        "agent_stuck": 15,
        "disk_low": 12,
        "context_warning": 20
      }
    },
    "top_items": [
      {"id": "alt-001", "severity": "critical", "summary": "Agent crashed"}
    ],
    "boundedness": {
      "aggregated": true,
      "showing_top": 5,
      "total": 47,
      "reason": "volume_high",
      "drill_down": {
        "ref": "inspect:alerts:all",
        "command": "ntm inspect alerts"
      }
    }
  }
}
```

## Sensitivity and Disclosure Scenarios

### Disclosure State Matrix

| State | Meaning | Typical Use |
|-------|---------|-------------|
| `visible` | Full payload and evidence shown inline | Low-sensitivity inspect/replay |
| `preview_only` | Summary visible, detailed evidence requires drill-down | Digest, attention, watch |
| `redacted` | Structure preserved but sensitive fields masked | Snapshot, inspect, export |
| `withheld` | Presence disclosed but payload omitted | Watch, replay, restricted inspect |
| `hashed_evidence` | Stable hashes shown instead of raw evidence | Replay, export, validation |

### Visible Evidence

```json
{
  "schema_id": "ntm:inspect:agent:v1.0",
  "request_id": "req_20260322040020_vis",
  "agent": {
    "name": "PeachPond",
    "status": "idle"
  },
  "disclosure": {
    "state": "visible",
    "policy_version": "v2.1"
  },
  "evidence": {
    "last_output_preview": "Reading internal/robot/attention_feed.go",
    "current_bead": "bd-j9jo3.1.5"
  }
}
```

### Preview-Only Attention Item

```json
{
  "schema_id": "ntm:digest:v1.0",
  "request_id": "req_20260322040021_prv",
  "attention": {
    "requires_action": [
      {
        "ref": "mail:msg-20260322-secret",
        "type": "mail",
        "summary": "Sensitive operator mail awaiting review",
        "attention_state": "new",
        "disclosure": {
          "state": "preview_only",
          "reason_code": "sensitivity:explicit_drilldown_required",
          "drill_down_ref": "inspect:mail:msg-20260322-secret"
        }
      }
    ]
  }
}
```

### Redacted Snapshot Section

```json
{
  "schema_id": "ntm:snapshot:v1.0",
  "request_id": "req_20260322040022_red",
  "coordination": {
    "available": true,
    "mail": {
      "total_unread": 1,
      "items": [
        {
          "ref": "mail:msg-20260322-secret",
          "subject": "[redacted]",
          "sender": "operator@[redacted]",
          "snippet": "[redacted]"
        }
      ]
    }
  },
  "disclosure": {
    "state": "redacted",
    "redacted_fields": ["coordination.mail.items[].subject", "coordination.mail.items[].sender", "coordination.mail.items[].snippet"],
    "policy_version": "v2.1"
  }
}
```

### Withheld Watch Event

```json
{
  "event_class": "attention",
  "event_type": "attention:mail:new",
  "payload": {
    "ref": "mail:msg-20260322-secret",
    "summary": "Sensitive mail event withheld from this stream",
    "disclosure": {
      "state": "withheld",
      "reason_code": "sensitivity:stream_scope_restricted",
      "operator_override_available": true
    },
    "payload_available": false
  }
}
```

### Historical Replay with Hashed Evidence

```json
{
  "schema_id": "ntm:incident_replay:v1.0",
  "request_id": "req_20260322040023_hsh",
  "incident_replay": {
    "incident_ref": "incident:inc-20260322-abc",
    "state_at_open": {
      "timestamp": "2026-03-22T04:00:00Z",
      "confidence": "high"
    },
    "evidence": [
      {
        "ref": "event:attn:9421",
        "hash": "sha256:7d0c0f3a8fbe91be874c7fd6f1e8f3cf71f9cb8d2e1d9bc8aa0104e4db8c2d4a",
        "preview": "panic: [redacted]"
      }
    ]
  },
  "disclosure": {
    "state": "hashed_evidence",
    "hash_algorithm": "sha256",
    "policy_version": "v2.1"
  }
}
```

## Retry and Idempotency Examples

### Idempotent Request

```json
{
  "action": "acknowledge",
  "request_id": "req_20260322040006_mno",
  "idempotency_key": "idem_ack_alt001_v1",
  "target": {
    "ref": "alert:agent_stuck:pane-abc123"
  }
}
```

### Idempotent Response (First Request)

```json
{
  "request_id": "req_20260322040006_mno",
  "idempotency_key": "idem_ack_alt001_v1",
  "status": "executed",
  "result": {
    "action": "acknowledge",
    "target_ref": "alert:agent_stuck:pane-abc123",
    "previous_state": "new",
    "new_state": "acknowledged",
    "executed_at": "2026-03-22T04:00:06Z"
  }
}
```

### Idempotent Response (Retry)

```json
{
  "request_id": "req_20260322040007_pqr",
  "idempotency_key": "idem_ack_alt001_v1",
  "status": "already_executed",
  "cached_result": {
    "original_request_id": "req_20260322040006_mno",
    "action": "acknowledge",
    "executed_at": "2026-03-22T04:00:06Z"
  },
  "idempotency": {
    "cache_hit": true,
    "original_timestamp": "2026-03-22T04:00:06Z",
    "result_unchanged": true
  }
}
```

## Attention Hygiene Examples

### Acknowledge

```json
{
  "action": "acknowledge",
  "request_id": "req_20260322040010_abc",
  "target": {"ref": "alert:agent_stuck:pane-abc123"},
  "scope": "instance"
}

// Response
{
  "result": {
    "action": "acknowledge",
    "target_ref": "alert:agent_stuck:pane-abc123",
    "previous_state": "new",
    "new_state": "acknowledged",
    "resurfacing_policy": "on_change"
  }
}
```

### Snooze Until Time

```json
{
  "action": "snooze",
  "request_id": "req_20260322040011_def",
  "target": {"ref": "alert:disk_low:pane-abc123"},
  "until": "2026-03-22T06:00:00Z",
  "reason": "Will clean up after standup"
}

// Response
{
  "result": {
    "action": "snooze",
    "target_ref": "alert:disk_low:pane-abc123",
    "previous_state": "new",
    "new_state": "snoozed",
    "snooze_until": "2026-03-22T06:00:00Z",
    "wake_conditions": ["time_elapsed", "severity_escalation"]
  }
}
```

### Pin

```json
{
  "action": "pin",
  "request_id": "req_20260322040012_ghi",
  "target": {"ref": "incident:inc-20260322-abc"},
  "reason": "Tracking P0 deployment issue",
  "scope": "project"
}

// Response
{
  "result": {
    "action": "pin",
    "target_ref": "incident:inc-20260322-abc",
    "pinned": true,
    "pin_scope": "project",
    "effects": {
      "always_visible": true,
      "top_priority": true,
      "no_auto_dismiss": true
    }
  }
}
```

### Mute Class

```json
{
  "action": "mute",
  "request_id": "req_20260322040013_jkl",
  "mute_rule": {
    "type_pattern": "alert:disk_low:*",
    "duration_hours": 4,
    "reason": "Known issue during migration"
  }
}

// Response
{
  "result": {
    "action": "mute",
    "rule_id": "mute_20260322040013_abc",
    "pattern": "alert:disk_low:*",
    "expires_at": "2026-03-22T08:00:00Z",
    "items_muted": 12
  }
}
```

### Resurface After Change

```json
{
  "event_type": "attention:resurfaced",
  "payload": {
    "ref": "alert:agent_stuck:pane-abc123",
    "previous_state": "acknowledged",
    "new_state": "new",
    "resurface_reason": "fingerprint_changed",
    "explanation": {
      "short": "Alert re-raised with new details",
      "code": "EXPLAIN_ATT_RESURFACED",
      "evidence": {
        "previous_fingerprint": "fp-20260322040000",
        "new_fingerprint": "fp-20260322041500",
        "change_type": "severity_escalation"
      }
    }
  }
}
```

## Incident Examples

### Incident Promotion

```json
{
  "event_type": "attention:incident:opened",
  "payload": {
    "incident": {
      "id": "inc-20260322-abc",
      "type": "agent_crashed",
      "severity": "P1",
      "title": "Agent PeachPond crashed",
      "status": "investigating",
      "opened_at": "2026-03-22T04:00:00Z",
      "detected_by": "alert_promotion"
    },
    "promotion": {
      "from_alert": "alt-20260322035500-xyz",
      "promotion_reason": "repeated_alert",
      "promotion_evidence": {
        "repeat_count": 3,
        "duration_ms": 600000
      }
    },
    "explanation": {
      "short": "Alert promoted to incident after 3 occurrences",
      "code": "EXPLAIN_INCIDENT_PROMOTED"
    }
  }
}
```

### Incident Resolution

```json
{
  "action": "resolve_incident",
  "request_id": "req_20260322043000_abc",
  "incident_ref": "incident:inc-20260322-abc",
  "resolution": "restarted_agent",
  "root_cause": "Memory leak in long-running session"
}

// Response
{
  "result": {
    "incident_ref": "incident:inc-20260322-abc",
    "previous_status": "investigating",
    "new_status": "resolved",
    "resolved_at": "2026-03-22T04:30:00Z",
    "resolution": "restarted_agent",
    "duration_ms": 1800000,
    "related_alerts_cleared": 3
  }
}
```

## Operator Loop Transcripts

### Transcript 1: Bootstrap to Triage to Action

```
┌─────────────────────────────────────────────────────────────┐
│ OPERATOR LOOP: Agent Debugging Session                       │
├─────────────────────────────────────────────────────────────┤
│ Actor: PeachPond (claude-code, opus-4.6)                    │
│ Session: main                                                │
│ Started: 2026-03-22T04:00:00Z                               │
└─────────────────────────────────────────────────────────────┘

STEP 1: Bootstrap
─────────────────
> ntm snapshot --robot

Response: [snapshot payload - all sources healthy, 1 warning alert]
Agent interpretation: "Session running, 3 agents registered, 1 alert for
stuck agent CloudyDesert"

STEP 2: Triage
──────────────
> ntm digest --robot

Response: [digest payload showing alert requires_action]
Agent interpretation: "CloudyDesert stuck for 8 minutes, suggested action:
check_output or send_interrupt"

STEP 3: Why-Explanation
───────────────────────
> ntm inspect agent CloudyDesert --robot

Response:
{
  "agent": {
    "name": "CloudyDesert",
    "status": "idle",
    "last_output": "2026-03-22T03:52:00Z",
    "pane": "pane-def456",
    "context_usage_pct": 85,
    "last_tool_call": "Read /data/projects/ntm/internal/cli/spawn.go"
  },
  "explanation": {
    "short": "Agent idle after high context usage",
    "code": "EXPLAIN_AGENT_STUCK",
    "evidence": {
      "idle_since": "2026-03-22T03:52:00Z",
      "context_at_idle": 85,
      "possible_cause": "context_pressure"
    }
  }
}

Agent interpretation: "CloudyDesert at 85% context, likely hit limit mid-task"

STEP 4: Action
──────────────
> ntm send CloudyDesert "/compact" --robot

Response:
{
  "action": "send_command",
  "request_id": "req_20260322040100_xyz",
  "target_pane": "pane-def456",
  "command": "/compact",
  "status": "delivered",
  "delivered_at": "2026-03-22T04:01:00Z"
}

STEP 5: Verify Outcome
──────────────────────
[Wait 30 seconds]
> ntm inspect agent CloudyDesert --robot

Response:
{
  "agent": {
    "name": "CloudyDesert",
    "status": "active",
    "last_output": "2026-03-22T04:01:25Z",
    "context_usage_pct": 45
  }
}

Agent interpretation: "CloudyDesert recovered, context dropped to 45%"

STEP 6: Acknowledge Alert
─────────────────────────
> ntm attention ack alert:agent_stuck:pane-def456 --robot

Response:
{
  "result": {
    "action": "acknowledge",
    "new_state": "acknowledged"
  }
}

LOOP COMPLETE
─────────────
Duration: 90 seconds
Outcome: Agent recovered, alert resolved
Audit trail: 4 actions logged
```

### Transcript 2: Incident Investigation

```
┌─────────────────────────────────────────────────────────────┐
│ OPERATOR LOOP: Incident Post-Mortem                          │
├─────────────────────────────────────────────────────────────┤
│ Actor: HumanOverseer (human)                                │
│ Incident: inc-20260322-abc                                  │
│ Started: 2026-03-22T04:30:00Z                               │
└─────────────────────────────────────────────────────────────┘

STEP 1: Incident Context
────────────────────────
> ntm inspect incident inc-20260322-abc --robot

Response:
{
  "incident": {
    "id": "inc-20260322-abc",
    "type": "agent_crashed",
    "severity": "P1",
    "title": "Agent PeachPond crashed",
    "status": "resolved",
    "opened_at": "2026-03-22T04:00:00Z",
    "resolved_at": "2026-03-22T04:25:00Z",
    "duration_ms": 1500000,
    "resolution": "restarted_agent",
    "root_cause": "Memory leak in long-running session"
  },
  "related_alerts": [
    "alt-20260322035500-xyz",
    "alt-20260322040000-abc"
  ]
}

STEP 2: Historical State at Incident Open
─────────────────────────────────────────
> ntm inspect --as-of "2026-03-22T04:00:00Z" --robot

Response:
{
  "reconstruction": {
    "method": "snapshot",
    "confidence": "high"
  },
  "state_at": "2026-03-22T04:00:00Z",
  "agents": {
    "PeachPond": {
      "status": "crashed",
      "last_output": "2026-03-22T03:59:45Z",
      "context_usage_pct": 98
    }
  }
}

Operator interpretation: "PeachPond crashed at 98% context - confirms root cause"

STEP 3: Event Timeline
──────────────────────
> ntm inspect incident inc-20260322-abc --timeline --robot

Response:
{
  "timeline": [
    {"t": "03:55:00", "event": "context_warning at 85%"},
    {"t": "03:58:00", "event": "context_warning at 95%"},
    {"t": "03:59:45", "event": "agent crashed"},
    {"t": "04:00:00", "event": "incident opened (auto-promoted)"},
    {"t": "04:05:00", "event": "assigned to HumanOverseer"},
    {"t": "04:20:00", "event": "agent restarted"},
    {"t": "04:25:00", "event": "incident resolved"}
  ]
}

STEP 4: Export Post-Mortem
──────────────────────────
> ntm export --incident inc-20260322-abc --format markdown --robot

Response:
{
  "export": {
    "format": "markdown",
    "size_bytes": 8500,
    "redactions_applied": 0,
    "content": "## Incident Report: inc-20260322-abc\n..."
  }
}

LOOP COMPLETE
─────────────
Duration: 5 minutes
Outcome: Post-mortem documented
Audit trail: 4 inspect queries logged
```

### Transcript 3: Watch Mode Recovery

```
┌─────────────────────────────────────────────────────────────┐
│ OPERATOR LOOP: Watch Reconnection                            │
├─────────────────────────────────────────────────────────────┤
│ Actor: GoldCat (claude-code, opus-4.6)                      │
│ Stream: watch_20260322030000_xyz                            │
│ Disconnected: 2026-03-22T03:45:00Z                          │
│ Reconnecting: 2026-03-22T04:00:00Z                          │
└─────────────────────────────────────────────────────────────┘

STEP 1: Reconnect with Cursor
─────────────────────────────
> ntm watch --robot --cursor cur_20260322034500

Response:
{
  "event_class": "control",
  "event_type": "control:connected",
  "payload": {
    "stream_id": "watch_20260322040000_abc",
    "cursor_requested": "cur_20260322034500",
    "cursor_valid": true,
    "replay_pending": 47
  }
}

STEP 2: Replay Events
─────────────────────
{
  "event_class": "control",
  "event_type": "control:replay_start",
  "payload": {
    "events_pending": 47
  }
}

[47 attention events with replay: true]

{
  "event_class": "control",
  "event_type": "control:replay_end",
  "payload": {
    "replayed_count": 47,
    "now_live": true,
    "cursor_position": "cur_20260322040000"
  }
}

STEP 3: Resume Live
───────────────────
[Heartbeats every 5 seconds]
{
  "event_class": "control",
  "event_type": "control:heartbeat",
  "payload": {
    "uptime_ms": 30000,
    "events_since_start": 47
  }
}

LOOP COMPLETE
─────────────
Duration: 15 seconds
Gap recovered: 15 minutes
Events replayed: 47
Now streaming live
```

## Design Notes

These examples are normative contract artifacts:

1. **Executable**: Tests can parse and validate against these structures
2. **Complete**: Cover happy path, degraded path, and error cases
3. **Versioned**: Schema IDs enable contract evolution
4. **Explainable**: Include explanation/evidence for debugging
5. **Bounded**: Show truncation, aggregation, and cursor behavior

If implementation diverges from these examples, that is a contract violation requiring either code fix or documented contract evolution.
