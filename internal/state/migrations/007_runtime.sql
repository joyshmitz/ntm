-- NTM State Store: Runtime Projection Layer
-- Version: 007
-- Description: Creates tables for runtime projections, source health, attention events, incidents, and audit trail
-- Bead: bd-j9jo3.2.1, bd-j9jo3.2.2, bd-j9jo3.2.5

-- =============================================================================
-- Runtime Session Projections
-- =============================================================================

CREATE TABLE IF NOT EXISTS runtime_sessions (
    name TEXT PRIMARY KEY,
    label TEXT,
    project_path TEXT,
    attached INTEGER NOT NULL DEFAULT 0,
    window_count INTEGER NOT NULL DEFAULT 0,
    pane_count INTEGER NOT NULL DEFAULT 0,
    agent_count INTEGER NOT NULL DEFAULT 0,
    active_agents INTEGER NOT NULL DEFAULT 0,
    idle_agents INTEGER NOT NULL DEFAULT 0,
    error_agents INTEGER NOT NULL DEFAULT 0,
    health_status TEXT NOT NULL DEFAULT 'unknown',
    health_reason TEXT,
    created_at TIMESTAMP,
    last_attached_at TIMESTAMP,
    last_activity_at TIMESTAMP,
    collected_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    stale_after TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_runtime_sessions_health ON runtime_sessions(health_status);
CREATE INDEX IF NOT EXISTS idx_runtime_sessions_collected ON runtime_sessions(collected_at);

-- =============================================================================
-- Runtime Agent Projections
-- =============================================================================

CREATE TABLE IF NOT EXISTS runtime_agents (
    id TEXT PRIMARY KEY,
    session_name TEXT NOT NULL,
    pane TEXT NOT NULL,
    agent_type TEXT NOT NULL,
    variant TEXT,
    type_confidence REAL NOT NULL DEFAULT 0.0,
    type_method TEXT NOT NULL DEFAULT 'unknown',
    state TEXT NOT NULL DEFAULT 'unknown',
    state_reason TEXT,
    previous_state TEXT,
    state_changed_at TIMESTAMP,
    last_output_at TIMESTAMP,
    last_output_age_sec INTEGER NOT NULL DEFAULT -1,
    output_tail_lines INTEGER NOT NULL DEFAULT 0,
    current_bead TEXT,
    pending_mail INTEGER NOT NULL DEFAULT 0,
    agent_mail_name TEXT,
    health_status TEXT NOT NULL DEFAULT 'unknown',
    health_reason TEXT,
    collected_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    stale_after TIMESTAMP NOT NULL,
    FOREIGN KEY (session_name) REFERENCES runtime_sessions(name) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_runtime_agents_session ON runtime_agents(session_name);
CREATE INDEX IF NOT EXISTS idx_runtime_agents_type ON runtime_agents(agent_type);
CREATE INDEX IF NOT EXISTS idx_runtime_agents_state ON runtime_agents(state);
CREATE INDEX IF NOT EXISTS idx_runtime_agents_health ON runtime_agents(health_status);
CREATE INDEX IF NOT EXISTS idx_runtime_agents_collected ON runtime_agents(collected_at);

-- =============================================================================
-- Runtime Work Projections (Beads)
-- =============================================================================

CREATE TABLE IF NOT EXISTS runtime_work (
    bead_id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    status TEXT NOT NULL,
    priority INTEGER NOT NULL DEFAULT 2,
    bead_type TEXT NOT NULL DEFAULT 'task',
    assignee TEXT,
    claimed_at TIMESTAMP,
    blocked_by_count INTEGER NOT NULL DEFAULT 0,
    unblocks_count INTEGER NOT NULL DEFAULT 0,
    labels TEXT,
    score REAL,
    score_reason TEXT,
    collected_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    stale_after TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_runtime_work_status ON runtime_work(status);
CREATE INDEX IF NOT EXISTS idx_runtime_work_priority ON runtime_work(priority);
CREATE INDEX IF NOT EXISTS idx_runtime_work_assignee ON runtime_work(assignee);
CREATE INDEX IF NOT EXISTS idx_runtime_work_score ON runtime_work(score);

-- =============================================================================
-- Runtime Coordination (Agent Mail)
-- =============================================================================

CREATE TABLE IF NOT EXISTS runtime_coordination (
    agent_name TEXT PRIMARY KEY,
    session_name TEXT,
    pane TEXT,
    unread_count INTEGER NOT NULL DEFAULT 0,
    pending_ack_count INTEGER NOT NULL DEFAULT 0,
    urgent_count INTEGER NOT NULL DEFAULT 0,
    last_message_at TIMESTAMP,
    last_sent_at TIMESTAMP,
    last_received_at TIMESTAMP,
    collected_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    stale_after TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_runtime_coord_session ON runtime_coordination(session_name);
CREATE INDEX IF NOT EXISTS idx_runtime_coord_unread ON runtime_coordination(unread_count);
CREATE INDEX IF NOT EXISTS idx_runtime_coord_urgent ON runtime_coordination(urgent_count);

-- =============================================================================
-- Runtime Quota (API Rate Limits)
-- =============================================================================

CREATE TABLE IF NOT EXISTS runtime_quota (
    provider TEXT NOT NULL,
    account TEXT NOT NULL,
    limit_hit INTEGER NOT NULL DEFAULT 0,
    used_pct REAL NOT NULL DEFAULT 0.0,
    resets_at TIMESTAMP,
    is_active INTEGER NOT NULL DEFAULT 0,
    healthy INTEGER NOT NULL DEFAULT 1,
    health_reason TEXT,
    collected_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    stale_after TIMESTAMP NOT NULL,
    PRIMARY KEY (provider, account)
);

CREATE INDEX IF NOT EXISTS idx_runtime_quota_provider ON runtime_quota(provider);
CREATE INDEX IF NOT EXISTS idx_runtime_quota_limit_hit ON runtime_quota(limit_hit);
CREATE INDEX IF NOT EXISTS idx_runtime_quota_active ON runtime_quota(is_active);

-- =============================================================================
-- Source Health
-- =============================================================================

CREATE TABLE IF NOT EXISTS source_health (
    source_name TEXT PRIMARY KEY,
    available INTEGER NOT NULL DEFAULT 0,
    healthy INTEGER NOT NULL DEFAULT 0,
    reason TEXT,
    last_success_at TIMESTAMP,
    last_failure_at TIMESTAMP,
    last_check_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    latency_ms INTEGER,
    avg_latency_ms INTEGER,
    version TEXT,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    last_error_code TEXT
);

CREATE INDEX IF NOT EXISTS idx_source_health_available ON source_health(available);
CREATE INDEX IF NOT EXISTS idx_source_health_healthy ON source_health(healthy);
CREATE INDEX IF NOT EXISTS idx_source_health_last_check ON source_health(last_check_at);

-- =============================================================================
-- Attention Events
-- =============================================================================

CREATE TABLE IF NOT EXISTS attention_events (
    cursor INTEGER PRIMARY KEY AUTOINCREMENT,
    ts TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    session_name TEXT,
    pane TEXT,
    category TEXT NOT NULL,
    event_type TEXT NOT NULL,
    source TEXT NOT NULL,
    actionability TEXT NOT NULL DEFAULT 'background',
    severity TEXT NOT NULL DEFAULT 'info',
    summary TEXT NOT NULL,
    details TEXT,
    next_actions TEXT,
    dedup_key TEXT,
    dedup_count INTEGER NOT NULL DEFAULT 1,
    expires_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_attention_events_ts ON attention_events(ts);
CREATE INDEX IF NOT EXISTS idx_attention_events_session ON attention_events(session_name);
CREATE INDEX IF NOT EXISTS idx_attention_events_category ON attention_events(category);
CREATE INDEX IF NOT EXISTS idx_attention_events_actionability ON attention_events(actionability);
CREATE INDEX IF NOT EXISTS idx_attention_events_severity ON attention_events(severity);
CREATE INDEX IF NOT EXISTS idx_attention_events_expires ON attention_events(expires_at);
CREATE INDEX IF NOT EXISTS idx_attention_events_dedup ON attention_events(dedup_key);

-- =============================================================================
-- Incidents
-- =============================================================================

CREATE TABLE IF NOT EXISTS incidents (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'open',
    severity TEXT NOT NULL DEFAULT 'medium',
    session_names TEXT,
    agent_ids TEXT,
    alert_count INTEGER NOT NULL DEFAULT 0,
    event_count INTEGER NOT NULL DEFAULT 0,
    first_event_cursor INTEGER,
    last_event_cursor INTEGER,
    started_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_event_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    acknowledged_at TIMESTAMP,
    acknowledged_by TEXT,
    resolved_at TIMESTAMP,
    resolved_by TEXT,
    muted_at TIMESTAMP,
    muted_by TEXT,
    muted_reason TEXT,
    root_cause TEXT,
    resolution TEXT,
    notes TEXT
);

CREATE INDEX IF NOT EXISTS idx_incidents_status ON incidents(status);
CREATE INDEX IF NOT EXISTS idx_incidents_severity ON incidents(severity);
CREATE INDEX IF NOT EXISTS idx_incidents_started_at ON incidents(started_at);
CREATE INDEX IF NOT EXISTS idx_incidents_last_event ON incidents(last_event_at);

-- =============================================================================
-- Incident Events (Link Table)
-- =============================================================================

CREATE TABLE IF NOT EXISTS incident_events (
    incident_id TEXT NOT NULL,
    event_cursor INTEGER NOT NULL,
    added_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (incident_id, event_cursor),
    FOREIGN KEY (incident_id) REFERENCES incidents(id) ON DELETE CASCADE,
    FOREIGN KEY (event_cursor) REFERENCES attention_events(cursor) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_incident_events_incident ON incident_events(incident_id);
CREATE INDEX IF NOT EXISTS idx_incident_events_cursor ON incident_events(event_cursor);

-- =============================================================================
-- Output Watermarks
-- =============================================================================

CREATE TABLE IF NOT EXISTS output_watermarks (
    watermark_type TEXT NOT NULL,
    scope TEXT NOT NULL,
    last_cursor INTEGER NOT NULL DEFAULT 0,
    last_ts TIMESTAMP,
    baseline_cursor INTEGER,
    baseline_ts TIMESTAMP,
    baseline_hash TEXT,
    consumer TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (watermark_type, scope)
);

CREATE INDEX IF NOT EXISTS idx_watermarks_type ON output_watermarks(watermark_type);
CREATE INDEX IF NOT EXISTS idx_watermarks_updated ON output_watermarks(updated_at);

-- =============================================================================
-- Audit Events (bd-j9jo3.2.5)
-- =============================================================================

CREATE TABLE IF NOT EXISTS audit_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ts TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    actor_type TEXT NOT NULL,
    actor_id TEXT,
    actor_origin TEXT,
    request_id TEXT,
    correlation_id TEXT,
    category TEXT NOT NULL,
    event_type TEXT NOT NULL,
    severity TEXT NOT NULL DEFAULT 'info',
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    previous_state TEXT,
    new_state TEXT,
    change_summary TEXT NOT NULL,
    reason TEXT,
    evidence TEXT,
    disclosure_state TEXT,
    retention_class TEXT NOT NULL DEFAULT 'standard',
    expires_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_audit_ts ON audit_events(ts);
CREATE INDEX IF NOT EXISTS idx_audit_actor ON audit_events(actor_type, actor_id);
CREATE INDEX IF NOT EXISTS idx_audit_request ON audit_events(request_id);
CREATE INDEX IF NOT EXISTS idx_audit_correlation ON audit_events(correlation_id);
CREATE INDEX IF NOT EXISTS idx_audit_category ON audit_events(category);
CREATE INDEX IF NOT EXISTS idx_audit_entity ON audit_events(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_audit_entity_ts ON audit_events(entity_type, entity_id, ts DESC);
CREATE INDEX IF NOT EXISTS idx_audit_actor_ts ON audit_events(actor_type, actor_id, ts DESC);
CREATE INDEX IF NOT EXISTS idx_audit_expires ON audit_events(expires_at);

-- =============================================================================
-- Audit Actors
-- =============================================================================

CREATE TABLE IF NOT EXISTS audit_actors (
    actor_type TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    display_name TEXT,
    description TEXT,
    first_seen_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    event_count INTEGER NOT NULL DEFAULT 0,
    known_origins TEXT,
    PRIMARY KEY (actor_type, actor_id)
);

CREATE INDEX IF NOT EXISTS idx_audit_actors_last_seen ON audit_actors(last_seen_at);

-- =============================================================================
-- Audit Decision Log
-- =============================================================================

CREATE TABLE IF NOT EXISTS audit_decision_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    decision_type TEXT NOT NULL,
    decision_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    actor_type TEXT NOT NULL,
    actor_id TEXT,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    reason TEXT,
    expires_at TIMESTAMP,
    audit_event_id INTEGER,
    FOREIGN KEY (audit_event_id) REFERENCES audit_events(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_decision_type ON audit_decision_log(decision_type);
CREATE INDEX IF NOT EXISTS idx_decision_entity ON audit_decision_log(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_decision_entity_at ON audit_decision_log(entity_type, entity_id, decision_at DESC);
CREATE INDEX IF NOT EXISTS idx_decision_actor ON audit_decision_log(actor_type, actor_id);
CREATE INDEX IF NOT EXISTS idx_decision_at ON audit_decision_log(decision_at);
CREATE INDEX IF NOT EXISTS idx_decision_expires ON audit_decision_log(expires_at);
CREATE INDEX IF NOT EXISTS idx_decision_event ON audit_decision_log(audit_event_id);
