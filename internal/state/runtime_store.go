// Package state provides durable SQLite-backed storage for NTM orchestration state.
// runtime_store.go implements store methods for the runtime projection layer.
//
// These methods provide typed access to runtime projections, source health,
// attention events, incidents, and audit trails. They abstract SQL details
// from consuming packages.
//
// Bead: bd-j9jo3.2.2
package state

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// =============================================================================
// Runtime Session Operations
// =============================================================================

// UpsertRuntimeSession inserts or updates a runtime session projection.
func (s *Store) UpsertRuntimeSession(sess *RuntimeSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO runtime_sessions (
			name, label, project_path, attached, window_count, pane_count,
			agent_count, active_agents, idle_agents, error_agents,
			health_status, health_reason, created_at, last_attached_at,
			last_activity_at, collected_at, stale_after
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			label = excluded.label,
			project_path = excluded.project_path,
			attached = excluded.attached,
			window_count = excluded.window_count,
			pane_count = excluded.pane_count,
			agent_count = excluded.agent_count,
			active_agents = excluded.active_agents,
			idle_agents = excluded.idle_agents,
			error_agents = excluded.error_agents,
			health_status = excluded.health_status,
			health_reason = excluded.health_reason,
			created_at = excluded.created_at,
			last_attached_at = excluded.last_attached_at,
			last_activity_at = excluded.last_activity_at,
			collected_at = excluded.collected_at,
			stale_after = excluded.stale_after`,
		sess.Name, sess.Label, sess.ProjectPath, sess.Attached,
		sess.WindowCount, sess.PaneCount, sess.AgentCount,
		sess.ActiveAgents, sess.IdleAgents, sess.ErrorAgents,
		sess.HealthStatus, sess.HealthReason, sess.CreatedAt,
		sess.LastAttachedAt, sess.LastActivityAt, sess.CollectedAt, sess.StaleAfter,
	)
	if err != nil {
		return fmt.Errorf("upsert runtime session: %w", err)
	}
	return nil
}

// GetRuntimeSession retrieves a runtime session by name.
func (s *Store) GetRuntimeSession(name string) (*RuntimeSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess := &RuntimeSession{}
	var attached int
	err := s.db.QueryRow(`
		SELECT name, COALESCE(label, ''), COALESCE(project_path, ''),
			attached, window_count, pane_count, agent_count, active_agents,
			idle_agents, error_agents, health_status, COALESCE(health_reason, ''),
			created_at, last_attached_at, last_activity_at, collected_at, stale_after
		FROM runtime_sessions WHERE name = ?`, name,
	).Scan(
		&sess.Name, &sess.Label, &sess.ProjectPath, &attached,
		&sess.WindowCount, &sess.PaneCount, &sess.AgentCount,
		&sess.ActiveAgents, &sess.IdleAgents, &sess.ErrorAgents,
		&sess.HealthStatus, &sess.HealthReason, &sess.CreatedAt,
		&sess.LastAttachedAt, &sess.LastActivityAt, &sess.CollectedAt, &sess.StaleAfter,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get runtime session: %w", err)
	}
	sess.Attached = attached == 1
	return sess, nil
}

// GetFreshRuntimeSessions returns all sessions that are not stale.
func (s *Store) GetFreshRuntimeSessions() ([]RuntimeSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT name, COALESCE(label, ''), COALESCE(project_path, ''),
			attached, window_count, pane_count, agent_count, active_agents,
			idle_agents, error_agents, health_status, COALESCE(health_reason, ''),
			created_at, last_attached_at, last_activity_at, collected_at, stale_after
		FROM runtime_sessions
		WHERE stale_after > datetime('now')
		ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list fresh runtime sessions: %w", err)
	}
	defer rows.Close()

	var sessions []RuntimeSession
	for rows.Next() {
		var sess RuntimeSession
		var attached int
		if err := rows.Scan(
			&sess.Name, &sess.Label, &sess.ProjectPath, &attached,
			&sess.WindowCount, &sess.PaneCount, &sess.AgentCount,
			&sess.ActiveAgents, &sess.IdleAgents, &sess.ErrorAgents,
			&sess.HealthStatus, &sess.HealthReason, &sess.CreatedAt,
			&sess.LastAttachedAt, &sess.LastActivityAt, &sess.CollectedAt, &sess.StaleAfter,
		); err != nil {
			return nil, fmt.Errorf("scan runtime session: %w", err)
		}
		sess.Attached = attached == 1
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

// DeleteRuntimeSession removes a runtime session projection.
func (s *Store) DeleteRuntimeSession(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec("DELETE FROM runtime_sessions WHERE name = ?", name)
	if err != nil {
		return fmt.Errorf("delete runtime session: %w", err)
	}
	return nil
}

// UpsertRuntimeSession inserts or updates a runtime session projection in an existing transaction.
func (tx *Tx) UpsertRuntimeSession(sess *RuntimeSession) error {
	_, err := tx.tx.Exec(`
		INSERT INTO runtime_sessions (
			name, label, project_path, attached, window_count, pane_count,
			agent_count, active_agents, idle_agents, error_agents,
			health_status, health_reason, created_at, last_attached_at,
			last_activity_at, collected_at, stale_after
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			label = excluded.label,
			project_path = excluded.project_path,
			attached = excluded.attached,
			window_count = excluded.window_count,
			pane_count = excluded.pane_count,
			agent_count = excluded.agent_count,
			active_agents = excluded.active_agents,
			idle_agents = excluded.idle_agents,
			error_agents = excluded.error_agents,
			health_status = excluded.health_status,
			health_reason = excluded.health_reason,
			created_at = excluded.created_at,
			last_attached_at = excluded.last_attached_at,
			last_activity_at = excluded.last_activity_at,
			collected_at = excluded.collected_at,
			stale_after = excluded.stale_after`,
		sess.Name, sess.Label, sess.ProjectPath, sess.Attached,
		sess.WindowCount, sess.PaneCount, sess.AgentCount,
		sess.ActiveAgents, sess.IdleAgents, sess.ErrorAgents,
		sess.HealthStatus, sess.HealthReason, sess.CreatedAt,
		sess.LastAttachedAt, sess.LastActivityAt, sess.CollectedAt, sess.StaleAfter,
	)
	if err != nil {
		return fmt.Errorf("upsert runtime session: %w", err)
	}
	return nil
}

// DeleteRuntimeSession removes a runtime session projection in an existing transaction.
func (tx *Tx) DeleteRuntimeSession(name string) error {
	_, err := tx.tx.Exec("DELETE FROM runtime_sessions WHERE name = ?", name)
	if err != nil {
		return fmt.Errorf("delete runtime session: %w", err)
	}
	return nil
}

// =============================================================================
// Runtime Agent Operations
// =============================================================================

// UpsertRuntimeAgent inserts or updates a runtime agent projection.
func (s *Store) UpsertRuntimeAgent(agent *RuntimeAgent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO runtime_agents (
			id, session_name, pane, agent_type, variant, type_confidence, type_method,
			state, state_reason, previous_state, state_changed_at,
			last_output_at, last_output_age_sec, output_tail_lines,
			current_bead, pending_mail, agent_mail_name,
			health_status, health_reason, collected_at, stale_after
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			session_name = excluded.session_name,
			pane = excluded.pane,
			agent_type = excluded.agent_type,
			variant = excluded.variant,
			type_confidence = excluded.type_confidence,
			type_method = excluded.type_method,
			state = excluded.state,
			state_reason = excluded.state_reason,
			previous_state = excluded.previous_state,
			state_changed_at = excluded.state_changed_at,
			last_output_at = excluded.last_output_at,
			last_output_age_sec = excluded.last_output_age_sec,
			output_tail_lines = excluded.output_tail_lines,
			current_bead = excluded.current_bead,
			pending_mail = excluded.pending_mail,
			agent_mail_name = excluded.agent_mail_name,
			health_status = excluded.health_status,
			health_reason = excluded.health_reason,
			collected_at = excluded.collected_at,
			stale_after = excluded.stale_after`,
		agent.ID, agent.SessionName, agent.Pane, agent.AgentType, agent.Variant,
		agent.TypeConfidence, agent.TypeMethod, agent.State, agent.StateReason,
		agent.PreviousState, agent.StateChangedAt, agent.LastOutputAt,
		agent.LastOutputAgeSec, agent.OutputTailLines, agent.CurrentBead,
		agent.PendingMail, agent.AgentMailName, agent.HealthStatus, agent.HealthReason,
		agent.CollectedAt, agent.StaleAfter,
	)
	if err != nil {
		return fmt.Errorf("upsert runtime agent: %w", err)
	}
	return nil
}

// GetRuntimeAgentsBySession returns fresh agents for a session.
func (s *Store) GetRuntimeAgentsBySession(sessionName string) ([]RuntimeAgent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT id, session_name, pane, agent_type, COALESCE(variant, ''),
			type_confidence, type_method, state, COALESCE(state_reason, ''),
			COALESCE(previous_state, ''), state_changed_at, last_output_at,
			last_output_age_sec, output_tail_lines, COALESCE(current_bead, ''),
			pending_mail, COALESCE(agent_mail_name, ''), health_status,
			COALESCE(health_reason, ''), collected_at, stale_after
		FROM runtime_agents
		WHERE session_name = ? AND stale_after > datetime('now')
		ORDER BY pane`, sessionName)
	if err != nil {
		return nil, fmt.Errorf("list runtime agents: %w", err)
	}
	defer rows.Close()

	var agents []RuntimeAgent
	for rows.Next() {
		var agent RuntimeAgent
		if err := rows.Scan(
			&agent.ID, &agent.SessionName, &agent.Pane, &agent.AgentType, &agent.Variant,
			&agent.TypeConfidence, &agent.TypeMethod, &agent.State, &agent.StateReason,
			&agent.PreviousState, &agent.StateChangedAt, &agent.LastOutputAt,
			&agent.LastOutputAgeSec, &agent.OutputTailLines, &agent.CurrentBead,
			&agent.PendingMail, &agent.AgentMailName, &agent.HealthStatus,
			&agent.HealthReason, &agent.CollectedAt, &agent.StaleAfter,
		); err != nil {
			return nil, fmt.Errorf("scan runtime agent: %w", err)
		}
		agents = append(agents, agent)
	}
	return agents, rows.Err()
}

// GetRuntimeAgent retrieves a fresh runtime agent by ID.
func (s *Store) GetRuntimeAgent(id string) (*RuntimeAgent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	agent := &RuntimeAgent{}
	err := s.db.QueryRow(`
		SELECT id, session_name, pane, agent_type, COALESCE(variant, ''),
			type_confidence, type_method, state, COALESCE(state_reason, ''),
			COALESCE(previous_state, ''), state_changed_at, last_output_at,
			last_output_age_sec, output_tail_lines, COALESCE(current_bead, ''),
			pending_mail, COALESCE(agent_mail_name, ''), health_status,
			COALESCE(health_reason, ''), collected_at, stale_after
		FROM runtime_agents
		WHERE id = ?`, id,
	).Scan(
		&agent.ID, &agent.SessionName, &agent.Pane, &agent.AgentType, &agent.Variant,
		&agent.TypeConfidence, &agent.TypeMethod, &agent.State, &agent.StateReason,
		&agent.PreviousState, &agent.StateChangedAt, &agent.LastOutputAt,
		&agent.LastOutputAgeSec, &agent.OutputTailLines, &agent.CurrentBead,
		&agent.PendingMail, &agent.AgentMailName, &agent.HealthStatus,
		&agent.HealthReason, &agent.CollectedAt, &agent.StaleAfter,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get runtime agent: %w", err)
	}
	return agent, nil
}

// DeleteRuntimeAgent removes a runtime agent projection.
func (s *Store) DeleteRuntimeAgent(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec("DELETE FROM runtime_agents WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete runtime agent: %w", err)
	}
	return nil
}

// UpsertRuntimeAgent inserts or updates a runtime agent projection in an existing transaction.
func (tx *Tx) UpsertRuntimeAgent(agent *RuntimeAgent) error {
	_, err := tx.tx.Exec(`
		INSERT INTO runtime_agents (
			id, session_name, pane, agent_type, variant, type_confidence, type_method,
			state, state_reason, previous_state, state_changed_at,
			last_output_at, last_output_age_sec, output_tail_lines,
			current_bead, pending_mail, agent_mail_name,
			health_status, health_reason, collected_at, stale_after
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			session_name = excluded.session_name,
			pane = excluded.pane,
			agent_type = excluded.agent_type,
			variant = excluded.variant,
			type_confidence = excluded.type_confidence,
			type_method = excluded.type_method,
			state = excluded.state,
			state_reason = excluded.state_reason,
			previous_state = excluded.previous_state,
			state_changed_at = excluded.state_changed_at,
			last_output_at = excluded.last_output_at,
			last_output_age_sec = excluded.last_output_age_sec,
			output_tail_lines = excluded.output_tail_lines,
			current_bead = excluded.current_bead,
			pending_mail = excluded.pending_mail,
			agent_mail_name = excluded.agent_mail_name,
			health_status = excluded.health_status,
			health_reason = excluded.health_reason,
			collected_at = excluded.collected_at,
			stale_after = excluded.stale_after`,
		agent.ID, agent.SessionName, agent.Pane, agent.AgentType, agent.Variant,
		agent.TypeConfidence, agent.TypeMethod, agent.State, agent.StateReason,
		agent.PreviousState, agent.StateChangedAt, agent.LastOutputAt,
		agent.LastOutputAgeSec, agent.OutputTailLines, agent.CurrentBead,
		agent.PendingMail, agent.AgentMailName, agent.HealthStatus, agent.HealthReason,
		agent.CollectedAt, agent.StaleAfter,
	)
	if err != nil {
		return fmt.Errorf("upsert runtime agent: %w", err)
	}
	return nil
}

// DeleteRuntimeAgent removes a runtime agent projection in an existing transaction.
func (tx *Tx) DeleteRuntimeAgent(id string) error {
	_, err := tx.tx.Exec("DELETE FROM runtime_agents WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete runtime agent: %w", err)
	}
	return nil
}

// =============================================================================
// Runtime Work Operations
// =============================================================================

// UpsertRuntimeWork inserts or updates a runtime work projection.
func (s *Store) UpsertRuntimeWork(work *RuntimeWork) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO runtime_work (
			bead_id, title, status, priority, bead_type, assignee, claimed_at,
			blocked_by_count, unblocks_count, labels, score, score_reason,
			collected_at, stale_after
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(bead_id) DO UPDATE SET
			title = excluded.title,
			status = excluded.status,
			priority = excluded.priority,
			bead_type = excluded.bead_type,
			assignee = excluded.assignee,
			claimed_at = excluded.claimed_at,
			blocked_by_count = excluded.blocked_by_count,
			unblocks_count = excluded.unblocks_count,
			labels = excluded.labels,
			score = excluded.score,
			score_reason = excluded.score_reason,
			collected_at = excluded.collected_at,
			stale_after = excluded.stale_after`,
		work.BeadID, work.Title, work.Status, work.Priority, work.BeadType,
		nullableString(work.Assignee), work.ClaimedAt, work.BlockedByCount, work.UnblocksCount,
		nullableString(work.Labels), work.Score, nullableString(work.ScoreReason),
		work.CollectedAt, work.StaleAfter,
	)
	if err != nil {
		return fmt.Errorf("upsert runtime work: %w", err)
	}
	return nil
}

// GetRuntimeWork retrieves a runtime work projection by bead ID.
func (s *Store) GetRuntimeWork(beadID string) (*RuntimeWork, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	work := &RuntimeWork{}
	err := s.db.QueryRow(`
		SELECT bead_id, title, status, priority, bead_type, COALESCE(assignee, ''),
			claimed_at, blocked_by_count, unblocks_count, COALESCE(labels, ''),
			score, COALESCE(score_reason, ''), collected_at, stale_after
		FROM runtime_work
		WHERE bead_id = ?`, beadID,
	).Scan(
		&work.BeadID, &work.Title, &work.Status, &work.Priority, &work.BeadType,
		&work.Assignee, &work.ClaimedAt, &work.BlockedByCount, &work.UnblocksCount,
		&work.Labels, &work.Score, &work.ScoreReason, &work.CollectedAt, &work.StaleAfter,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get runtime work: %w", err)
	}
	return work, nil
}

// ListFreshRuntimeWork returns fresh runtime work projections, optionally filtered by status.
func (s *Store) ListFreshRuntimeWork(status string, limit int) ([]RuntimeWork, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = -1
	}

	query := `
		SELECT bead_id, title, status, priority, bead_type, COALESCE(assignee, ''),
			claimed_at, blocked_by_count, unblocks_count, COALESCE(labels, ''),
			score, COALESCE(score_reason, ''), collected_at, stale_after
		FROM runtime_work
		WHERE stale_after > datetime('now')`
	args := []interface{}{}
	if status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY priority ASC, COALESCE(score, -1) DESC, bead_id ASC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list runtime work: %w", err)
	}
	defer rows.Close()

	var items []RuntimeWork
	for rows.Next() {
		var work RuntimeWork
		if err := rows.Scan(
			&work.BeadID, &work.Title, &work.Status, &work.Priority, &work.BeadType,
			&work.Assignee, &work.ClaimedAt, &work.BlockedByCount, &work.UnblocksCount,
			&work.Labels, &work.Score, &work.ScoreReason, &work.CollectedAt, &work.StaleAfter,
		); err != nil {
			return nil, fmt.Errorf("scan runtime work: %w", err)
		}
		items = append(items, work)
	}
	return items, rows.Err()
}

// DeleteRuntimeWork removes a runtime work projection.
func (s *Store) DeleteRuntimeWork(beadID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec("DELETE FROM runtime_work WHERE bead_id = ?", beadID)
	if err != nil {
		return fmt.Errorf("delete runtime work: %w", err)
	}
	return nil
}

// UpsertRuntimeWork inserts or updates a runtime work projection in an existing transaction.
func (tx *Tx) UpsertRuntimeWork(work *RuntimeWork) error {
	_, err := tx.tx.Exec(`
		INSERT INTO runtime_work (
			bead_id, title, status, priority, bead_type, assignee, claimed_at,
			blocked_by_count, unblocks_count, labels, score, score_reason,
			collected_at, stale_after
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(bead_id) DO UPDATE SET
			title = excluded.title,
			status = excluded.status,
			priority = excluded.priority,
			bead_type = excluded.bead_type,
			assignee = excluded.assignee,
			claimed_at = excluded.claimed_at,
			blocked_by_count = excluded.blocked_by_count,
			unblocks_count = excluded.unblocks_count,
			labels = excluded.labels,
			score = excluded.score,
			score_reason = excluded.score_reason,
			collected_at = excluded.collected_at,
			stale_after = excluded.stale_after`,
		work.BeadID, work.Title, work.Status, work.Priority, work.BeadType,
		nullableString(work.Assignee), work.ClaimedAt, work.BlockedByCount, work.UnblocksCount,
		nullableString(work.Labels), work.Score, nullableString(work.ScoreReason),
		work.CollectedAt, work.StaleAfter,
	)
	if err != nil {
		return fmt.Errorf("upsert runtime work: %w", err)
	}
	return nil
}

// DeleteRuntimeWork removes a runtime work projection in an existing transaction.
func (tx *Tx) DeleteRuntimeWork(beadID string) error {
	_, err := tx.tx.Exec("DELETE FROM runtime_work WHERE bead_id = ?", beadID)
	if err != nil {
		return fmt.Errorf("delete runtime work: %w", err)
	}
	return nil
}

// =============================================================================
// Runtime Coordination Operations
// =============================================================================

// UpsertRuntimeCoordination inserts or updates a runtime coordination projection.
func (s *Store) UpsertRuntimeCoordination(coord *RuntimeCoordination) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO runtime_coordination (
			agent_name, session_name, pane, unread_count, pending_ack_count, urgent_count,
			last_message_at, last_sent_at, last_received_at, collected_at, stale_after
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(agent_name) DO UPDATE SET
			session_name = excluded.session_name,
			pane = excluded.pane,
			unread_count = excluded.unread_count,
			pending_ack_count = excluded.pending_ack_count,
			urgent_count = excluded.urgent_count,
			last_message_at = excluded.last_message_at,
			last_sent_at = excluded.last_sent_at,
			last_received_at = excluded.last_received_at,
			collected_at = excluded.collected_at,
			stale_after = excluded.stale_after`,
		coord.AgentName, nullableString(coord.SessionName), nullableString(coord.Pane),
		coord.UnreadCount, coord.PendingAckCount, coord.UrgentCount,
		coord.LastMessageAt, coord.LastSentAt, coord.LastReceivedAt,
		coord.CollectedAt, coord.StaleAfter,
	)
	if err != nil {
		return fmt.Errorf("upsert runtime coordination: %w", err)
	}
	return nil
}

// GetRuntimeCoordination retrieves a runtime coordination projection by agent name.
func (s *Store) GetRuntimeCoordination(agentName string) (*RuntimeCoordination, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	coord := &RuntimeCoordination{}
	err := s.db.QueryRow(`
		SELECT agent_name, COALESCE(session_name, ''), COALESCE(pane, ''),
			unread_count, pending_ack_count, urgent_count,
			last_message_at, last_sent_at, last_received_at, collected_at, stale_after
		FROM runtime_coordination
		WHERE agent_name = ?`, agentName,
	).Scan(
		&coord.AgentName, &coord.SessionName, &coord.Pane,
		&coord.UnreadCount, &coord.PendingAckCount, &coord.UrgentCount,
		&coord.LastMessageAt, &coord.LastSentAt, &coord.LastReceivedAt,
		&coord.CollectedAt, &coord.StaleAfter,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get runtime coordination: %w", err)
	}
	return coord, nil
}

// ListFreshRuntimeCoordination returns fresh coordination projections, optionally filtered by session.
func (s *Store) ListFreshRuntimeCoordination(sessionName string) ([]RuntimeCoordination, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT agent_name, COALESCE(session_name, ''), COALESCE(pane, ''),
			unread_count, pending_ack_count, urgent_count,
			last_message_at, last_sent_at, last_received_at, collected_at, stale_after
		FROM runtime_coordination
		WHERE stale_after > datetime('now')`
	args := []interface{}{}
	if sessionName != "" {
		query += ` AND session_name = ?`
		args = append(args, sessionName)
	}
	query += ` ORDER BY urgent_count DESC, unread_count DESC, agent_name ASC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list runtime coordination: %w", err)
	}
	defer rows.Close()

	var items []RuntimeCoordination
	for rows.Next() {
		var coord RuntimeCoordination
		if err := rows.Scan(
			&coord.AgentName, &coord.SessionName, &coord.Pane,
			&coord.UnreadCount, &coord.PendingAckCount, &coord.UrgentCount,
			&coord.LastMessageAt, &coord.LastSentAt, &coord.LastReceivedAt,
			&coord.CollectedAt, &coord.StaleAfter,
		); err != nil {
			return nil, fmt.Errorf("scan runtime coordination: %w", err)
		}
		items = append(items, coord)
	}
	return items, rows.Err()
}

// DeleteRuntimeCoordination removes a runtime coordination projection.
func (s *Store) DeleteRuntimeCoordination(agentName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec("DELETE FROM runtime_coordination WHERE agent_name = ?", agentName)
	if err != nil {
		return fmt.Errorf("delete runtime coordination: %w", err)
	}
	return nil
}

// UpsertRuntimeCoordination inserts or updates a runtime coordination projection in an existing transaction.
func (tx *Tx) UpsertRuntimeCoordination(coord *RuntimeCoordination) error {
	_, err := tx.tx.Exec(`
		INSERT INTO runtime_coordination (
			agent_name, session_name, pane, unread_count, pending_ack_count, urgent_count,
			last_message_at, last_sent_at, last_received_at, collected_at, stale_after
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(agent_name) DO UPDATE SET
			session_name = excluded.session_name,
			pane = excluded.pane,
			unread_count = excluded.unread_count,
			pending_ack_count = excluded.pending_ack_count,
			urgent_count = excluded.urgent_count,
			last_message_at = excluded.last_message_at,
			last_sent_at = excluded.last_sent_at,
			last_received_at = excluded.last_received_at,
			collected_at = excluded.collected_at,
			stale_after = excluded.stale_after`,
		coord.AgentName, nullableString(coord.SessionName), nullableString(coord.Pane),
		coord.UnreadCount, coord.PendingAckCount, coord.UrgentCount,
		coord.LastMessageAt, coord.LastSentAt, coord.LastReceivedAt,
		coord.CollectedAt, coord.StaleAfter,
	)
	if err != nil {
		return fmt.Errorf("upsert runtime coordination: %w", err)
	}
	return nil
}

// DeleteRuntimeCoordination removes a runtime coordination projection in an existing transaction.
func (tx *Tx) DeleteRuntimeCoordination(agentName string) error {
	_, err := tx.tx.Exec("DELETE FROM runtime_coordination WHERE agent_name = ?", agentName)
	if err != nil {
		return fmt.Errorf("delete runtime coordination: %w", err)
	}
	return nil
}

// =============================================================================
// Runtime Quota Operations
// =============================================================================

// UpsertRuntimeQuota inserts or updates a runtime quota projection.
func (s *Store) UpsertRuntimeQuota(quota *RuntimeQuota) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO runtime_quota (
			provider, account, limit_hit, used_pct, resets_at, is_active,
			healthy, health_reason, collected_at, stale_after
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider, account) DO UPDATE SET
			limit_hit = excluded.limit_hit,
			used_pct = excluded.used_pct,
			resets_at = excluded.resets_at,
			is_active = excluded.is_active,
			healthy = excluded.healthy,
			health_reason = excluded.health_reason,
			collected_at = excluded.collected_at,
			stale_after = excluded.stale_after`,
		quota.Provider, quota.Account, quota.LimitHit, quota.UsedPct, quota.ResetsAt,
		quota.IsActive, quota.Healthy, nullableString(quota.HealthReason),
		quota.CollectedAt, quota.StaleAfter,
	)
	if err != nil {
		return fmt.Errorf("upsert runtime quota: %w", err)
	}
	return nil
}

// GetRuntimeQuota retrieves a runtime quota projection by provider/account.
func (s *Store) GetRuntimeQuota(provider, account string) (*RuntimeQuota, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	quota := &RuntimeQuota{}
	var limitHit, isActive, healthy int
	err := s.db.QueryRow(`
		SELECT provider, account, limit_hit, used_pct, resets_at, is_active,
			healthy, COALESCE(health_reason, ''), collected_at, stale_after
		FROM runtime_quota
		WHERE provider = ? AND account = ?`, provider, account,
	).Scan(
		&quota.Provider, &quota.Account, &limitHit, &quota.UsedPct, &quota.ResetsAt,
		&isActive, &healthy, &quota.HealthReason, &quota.CollectedAt, &quota.StaleAfter,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get runtime quota: %w", err)
	}
	quota.LimitHit = limitHit == 1
	quota.IsActive = isActive == 1
	quota.Healthy = healthy == 1
	return quota, nil
}

// ListFreshRuntimeQuota returns fresh quota projections, optionally filtered by provider.
func (s *Store) ListFreshRuntimeQuota(provider string) ([]RuntimeQuota, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT provider, account, limit_hit, used_pct, resets_at, is_active,
			healthy, COALESCE(health_reason, ''), collected_at, stale_after
		FROM runtime_quota
		WHERE stale_after > datetime('now')`
	args := []interface{}{}
	if provider != "" {
		query += ` AND provider = ?`
		args = append(args, provider)
	}
	query += ` ORDER BY is_active DESC, used_pct DESC, provider ASC, account ASC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list runtime quota: %w", err)
	}
	defer rows.Close()

	var items []RuntimeQuota
	for rows.Next() {
		var quota RuntimeQuota
		var limitHit, isActive, healthy int
		if err := rows.Scan(
			&quota.Provider, &quota.Account, &limitHit, &quota.UsedPct, &quota.ResetsAt,
			&isActive, &healthy, &quota.HealthReason, &quota.CollectedAt, &quota.StaleAfter,
		); err != nil {
			return nil, fmt.Errorf("scan runtime quota: %w", err)
		}
		quota.LimitHit = limitHit == 1
		quota.IsActive = isActive == 1
		quota.Healthy = healthy == 1
		items = append(items, quota)
	}
	return items, rows.Err()
}

// DeleteRuntimeQuota removes a runtime quota projection.
func (s *Store) DeleteRuntimeQuota(provider, account string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`DELETE FROM runtime_quota WHERE provider = ? AND account = ?`, provider, account)
	if err != nil {
		return fmt.Errorf("delete runtime quota: %w", err)
	}
	return nil
}

// UpsertRuntimeQuota inserts or updates a runtime quota projection in an existing transaction.
func (tx *Tx) UpsertRuntimeQuota(quota *RuntimeQuota) error {
	_, err := tx.tx.Exec(`
		INSERT INTO runtime_quota (
			provider, account, limit_hit, used_pct, resets_at, is_active,
			healthy, health_reason, collected_at, stale_after
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider, account) DO UPDATE SET
			limit_hit = excluded.limit_hit,
			used_pct = excluded.used_pct,
			resets_at = excluded.resets_at,
			is_active = excluded.is_active,
			healthy = excluded.healthy,
			health_reason = excluded.health_reason,
			collected_at = excluded.collected_at,
			stale_after = excluded.stale_after`,
		quota.Provider, quota.Account, quota.LimitHit, quota.UsedPct, quota.ResetsAt,
		quota.IsActive, quota.Healthy, nullableString(quota.HealthReason),
		quota.CollectedAt, quota.StaleAfter,
	)
	if err != nil {
		return fmt.Errorf("upsert runtime quota: %w", err)
	}
	return nil
}

// DeleteRuntimeQuota removes a runtime quota projection in an existing transaction.
func (tx *Tx) DeleteRuntimeQuota(provider, account string) error {
	_, err := tx.tx.Exec(`DELETE FROM runtime_quota WHERE provider = ? AND account = ?`, provider, account)
	if err != nil {
		return fmt.Errorf("delete runtime quota: %w", err)
	}
	return nil
}

// =============================================================================
// Source Health Operations
// =============================================================================

// UpsertSourceHealth inserts or updates a source health record.
func (s *Store) UpsertSourceHealth(health *SourceHealth) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO source_health (
			source_name, available, healthy, reason,
			last_success_at, last_failure_at, last_check_at,
			latency_ms, avg_latency_ms, version,
			consecutive_failures, last_error, last_error_code
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source_name) DO UPDATE SET
			available = excluded.available,
			healthy = excluded.healthy,
			reason = excluded.reason,
			last_success_at = excluded.last_success_at,
			last_failure_at = excluded.last_failure_at,
			last_check_at = excluded.last_check_at,
			latency_ms = excluded.latency_ms,
			avg_latency_ms = excluded.avg_latency_ms,
			version = excluded.version,
			consecutive_failures = excluded.consecutive_failures,
			last_error = excluded.last_error,
			last_error_code = excluded.last_error_code`,
		health.SourceName, health.Available, health.Healthy, health.Reason,
		health.LastSuccessAt, health.LastFailureAt, health.LastCheckAt,
		health.LatencyMs, health.AvgLatencyMs, health.Version,
		health.ConsecutiveFailures, health.LastError, health.LastErrorCode,
	)
	if err != nil {
		return fmt.Errorf("upsert source health: %w", err)
	}
	return nil
}

// UpsertSourceHealth inserts or updates a source health record in an existing transaction.
func (tx *Tx) UpsertSourceHealth(health *SourceHealth) error {
	_, err := tx.tx.Exec(`
		INSERT INTO source_health (
			source_name, available, healthy, reason,
			last_success_at, last_failure_at, last_check_at,
			latency_ms, avg_latency_ms, version,
			consecutive_failures, last_error, last_error_code
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source_name) DO UPDATE SET
			available = excluded.available,
			healthy = excluded.healthy,
			reason = excluded.reason,
			last_success_at = excluded.last_success_at,
			last_failure_at = excluded.last_failure_at,
			last_check_at = excluded.last_check_at,
			latency_ms = excluded.latency_ms,
			avg_latency_ms = excluded.avg_latency_ms,
			version = excluded.version,
			consecutive_failures = excluded.consecutive_failures,
			last_error = excluded.last_error,
			last_error_code = excluded.last_error_code`,
		health.SourceName, health.Available, health.Healthy, health.Reason,
		health.LastSuccessAt, health.LastFailureAt, health.LastCheckAt,
		health.LatencyMs, health.AvgLatencyMs, health.Version,
		health.ConsecutiveFailures, health.LastError, health.LastErrorCode,
	)
	if err != nil {
		return fmt.Errorf("upsert source health: %w", err)
	}
	return nil
}

// DeleteSourceHealth removes a source health record.
func (s *Store) DeleteSourceHealth(sourceName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec("DELETE FROM source_health WHERE source_name = ?", sourceName)
	if err != nil {
		return fmt.Errorf("delete source health: %w", err)
	}
	return nil
}

// DeleteSourceHealth removes a source health record in an existing transaction.
func (tx *Tx) DeleteSourceHealth(sourceName string) error {
	_, err := tx.tx.Exec("DELETE FROM source_health WHERE source_name = ?", sourceName)
	if err != nil {
		return fmt.Errorf("delete source health: %w", err)
	}
	return nil
}

// GetSourceHealth retrieves health for a specific source.
func (s *Store) GetSourceHealth(sourceName string) (*SourceHealth, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	health := &SourceHealth{}
	var available, healthy int
	err := s.db.QueryRow(`
		SELECT source_name, available, healthy, COALESCE(reason, ''),
			last_success_at, last_failure_at, last_check_at,
			latency_ms, avg_latency_ms, COALESCE(version, ''),
			consecutive_failures, COALESCE(last_error, ''), COALESCE(last_error_code, '')
		FROM source_health WHERE source_name = ?`, sourceName,
	).Scan(
		&health.SourceName, &available, &healthy, &health.Reason,
		&health.LastSuccessAt, &health.LastFailureAt, &health.LastCheckAt,
		&health.LatencyMs, &health.AvgLatencyMs, &health.Version,
		&health.ConsecutiveFailures, &health.LastError, &health.LastErrorCode,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get source health: %w", err)
	}
	health.Available = available == 1
	health.Healthy = healthy == 1
	return health, nil
}

// GetAllSourceHealth returns health for all sources.
func (s *Store) GetAllSourceHealth() ([]SourceHealth, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT source_name, available, healthy, COALESCE(reason, ''),
			last_success_at, last_failure_at, last_check_at,
			latency_ms, avg_latency_ms, COALESCE(version, ''),
			consecutive_failures, COALESCE(last_error, ''), COALESCE(last_error_code, '')
		FROM source_health
		ORDER BY source_name`)
	if err != nil {
		return nil, fmt.Errorf("list source health: %w", err)
	}
	defer rows.Close()

	var results []SourceHealth
	for rows.Next() {
		var health SourceHealth
		var available, healthy int
		if err := rows.Scan(
			&health.SourceName, &available, &healthy, &health.Reason,
			&health.LastSuccessAt, &health.LastFailureAt, &health.LastCheckAt,
			&health.LatencyMs, &health.AvgLatencyMs, &health.Version,
			&health.ConsecutiveFailures, &health.LastError, &health.LastErrorCode,
		); err != nil {
			return nil, fmt.Errorf("scan source health: %w", err)
		}
		health.Available = available == 1
		health.Healthy = healthy == 1
		results = append(results, health)
	}
	return results, rows.Err()
}

// GetDegradedSources returns sources that are unavailable or unhealthy.
func (s *Store) GetDegradedSources() ([]SourceHealth, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT source_name, available, healthy, COALESCE(reason, ''),
			last_success_at, last_failure_at, last_check_at,
			latency_ms, avg_latency_ms, COALESCE(version, ''),
			consecutive_failures, COALESCE(last_error, ''), COALESCE(last_error_code, '')
		FROM source_health
		WHERE available = 0 OR healthy = 0
		ORDER BY source_name`)
	if err != nil {
		return nil, fmt.Errorf("list degraded sources: %w", err)
	}
	defer rows.Close()

	var results []SourceHealth
	for rows.Next() {
		var health SourceHealth
		var available, healthy int
		if err := rows.Scan(
			&health.SourceName, &available, &healthy, &health.Reason,
			&health.LastSuccessAt, &health.LastFailureAt, &health.LastCheckAt,
			&health.LatencyMs, &health.AvgLatencyMs, &health.Version,
			&health.ConsecutiveFailures, &health.LastError, &health.LastErrorCode,
		); err != nil {
			return nil, fmt.Errorf("scan degraded source: %w", err)
		}
		health.Available = available == 1
		health.Healthy = healthy == 1
		results = append(results, health)
	}
	return results, rows.Err()
}

// =============================================================================
// Attention Event Operations
// =============================================================================

// AppendAttentionEvent inserts an attention event and returns its cursor.
func (s *Store) AppendAttentionEvent(event *StoredAttentionEvent) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`
		INSERT INTO attention_events (
			ts, session_name, pane, category, event_type, source,
			actionability, severity, reason_code, summary, details, next_actions,
			dedup_key, dedup_count, expires_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.Ts, event.SessionName, event.Pane, event.Category,
		event.EventType, event.Source, event.Actionability, event.Severity, event.ReasonCode,
		event.Summary, event.Details, event.NextActions,
		event.DedupKey, event.DedupCount, event.ExpiresAt,
	)
	if err != nil {
		return 0, fmt.Errorf("append attention event: %w", err)
	}

	cursor, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get event cursor: %w", err)
	}
	return cursor, nil
}

// AppendAttentionEvent inserts an attention event and returns its cursor in an existing transaction.
func (tx *Tx) AppendAttentionEvent(event *StoredAttentionEvent) (int64, error) {
	result, err := tx.tx.Exec(`
		INSERT INTO attention_events (
			ts, session_name, pane, category, event_type, source,
			actionability, severity, reason_code, summary, details, next_actions,
			dedup_key, dedup_count, expires_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.Ts, event.SessionName, event.Pane, event.Category,
		event.EventType, event.Source, event.Actionability, event.Severity, event.ReasonCode,
		event.Summary, event.Details, event.NextActions,
		event.DedupKey, event.DedupCount, event.ExpiresAt,
	)
	if err != nil {
		return 0, fmt.Errorf("append attention event: %w", err)
	}

	cursor, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get event cursor: %w", err)
	}
	return cursor, nil
}

// GetAttentionEventsSince returns events with cursor > sinceCursor.
func (s *Store) GetAttentionEventsSince(sinceCursor int64, limit int) ([]StoredAttentionEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.Query(`
		SELECT cursor, ts, COALESCE(session_name, ''), COALESCE(pane, ''),
			category, event_type, source, actionability, severity, COALESCE(reason_code, ''),
			summary, COALESCE(details, ''), COALESCE(next_actions, ''),
			COALESCE(dedup_key, ''), dedup_count, expires_at
		FROM attention_events
		WHERE cursor > ?
		  AND (expires_at IS NULL OR expires_at >= datetime('now'))
		ORDER BY cursor ASC
		LIMIT ?`, sinceCursor, limit)
	if err != nil {
		return nil, fmt.Errorf("get events since cursor: %w", err)
	}
	defer rows.Close()

	var events []StoredAttentionEvent
	for rows.Next() {
		var event StoredAttentionEvent
		if err := rows.Scan(
			&event.Cursor, &event.Ts, &event.SessionName, &event.Pane,
			&event.Category, &event.EventType, &event.Source,
			&event.Actionability, &event.Severity, &event.ReasonCode, &event.Summary,
			&event.Details, &event.NextActions, &event.DedupKey,
			&event.DedupCount, &event.ExpiresAt,
		); err != nil {
			return nil, fmt.Errorf("scan attention event: %w", err)
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

// GetAttentionReplayWindow returns the currently replayable attention-event range.
func (s *Store) GetAttentionReplayWindow() (AttentionReplayWindow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	window := AttentionReplayWindow{}
	var oldest, newest sql.NullInt64
	err := s.db.QueryRow(`
		SELECT
			(SELECT MIN(cursor) FROM attention_events WHERE expires_at IS NULL OR expires_at >= datetime('now')),
			(SELECT MAX(cursor) FROM attention_events),
			(SELECT COUNT(*) FROM attention_events WHERE expires_at IS NULL OR expires_at >= datetime('now'))`,
	).Scan(&oldest, &newest, &window.EventCount)
	if err != nil {
		return AttentionReplayWindow{}, fmt.Errorf("get attention replay window: %w", err)
	}
	if oldest.Valid {
		window.OldestCursor = oldest.Int64
	}
	if newest.Valid {
		window.NewestCursor = newest.Int64
	}
	return window, nil
}

// GetLatestEventCursor returns the most recent event cursor.
func (s *Store) GetLatestEventCursor() (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var cursor sql.NullInt64
	err := s.db.QueryRow("SELECT MAX(cursor) FROM attention_events").Scan(&cursor)
	if err != nil {
		return 0, fmt.Errorf("get latest cursor: %w", err)
	}
	if !cursor.Valid {
		return 0, nil
	}
	return cursor.Int64, nil
}

// GCExpiredEvents deletes events past their expiration time.
// Returns the number of events deleted.
func (s *Store) GCExpiredEvents() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`DELETE FROM attention_events WHERE expires_at < datetime('now')`)
	if err != nil {
		return 0, fmt.Errorf("gc expired events: %w", err)
	}
	return result.RowsAffected()
}

// =============================================================================
// Incident Operations
// =============================================================================

// CreateIncident inserts a new incident.
func (s *Store) CreateIncident(incident *Incident) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO incidents (
			id, title, status, severity, session_names, agent_ids,
			alert_count, event_count, first_event_cursor, last_event_cursor,
			started_at, last_event_at, acknowledged_at, acknowledged_by,
			resolved_at, resolved_by, muted_at, muted_by, muted_reason,
			root_cause, resolution, notes
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		incident.ID, incident.Title, incident.Status, incident.Severity,
		incident.SessionNames, incident.AgentIDs, incident.AlertCount,
		incident.EventCount, incident.FirstEventCursor, incident.LastEventCursor,
		incident.StartedAt, incident.LastEventAt, incident.AcknowledgedAt,
		incident.AcknowledgedBy, incident.ResolvedAt, incident.ResolvedBy,
		incident.MutedAt, incident.MutedBy, incident.MutedReason,
		incident.RootCause, incident.Resolution, incident.Notes,
	)
	if err != nil {
		return fmt.Errorf("create incident: %w", err)
	}
	return nil
}

// CreateIncident inserts a new incident in an existing transaction.
func (tx *Tx) CreateIncident(incident *Incident) error {
	_, err := tx.tx.Exec(`
		INSERT INTO incidents (
			id, title, status, severity, session_names, agent_ids,
			alert_count, event_count, first_event_cursor, last_event_cursor,
			started_at, last_event_at, acknowledged_at, acknowledged_by,
			resolved_at, resolved_by, muted_at, muted_by, muted_reason,
			root_cause, resolution, notes
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		incident.ID, incident.Title, incident.Status, incident.Severity,
		incident.SessionNames, incident.AgentIDs, incident.AlertCount,
		incident.EventCount, incident.FirstEventCursor, incident.LastEventCursor,
		incident.StartedAt, incident.LastEventAt, incident.AcknowledgedAt,
		incident.AcknowledgedBy, incident.ResolvedAt, incident.ResolvedBy,
		incident.MutedAt, incident.MutedBy, incident.MutedReason,
		incident.RootCause, incident.Resolution, incident.Notes,
	)
	if err != nil {
		return fmt.Errorf("create incident: %w", err)
	}
	return nil
}

// GetIncident retrieves an incident by ID.
func (s *Store) GetIncident(id string) (*Incident, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	incident := &Incident{}
	err := s.db.QueryRow(`
		SELECT id, title, status, severity, COALESCE(session_names, ''),
			COALESCE(agent_ids, ''), alert_count, event_count,
			first_event_cursor, last_event_cursor, started_at, last_event_at,
			acknowledged_at, COALESCE(acknowledged_by, ''), resolved_at,
			COALESCE(resolved_by, ''), muted_at, COALESCE(muted_by, ''),
			COALESCE(muted_reason, ''), COALESCE(root_cause, ''),
			COALESCE(resolution, ''), COALESCE(notes, '')
		FROM incidents WHERE id = ?`, id,
	).Scan(
		&incident.ID, &incident.Title, &incident.Status, &incident.Severity,
		&incident.SessionNames, &incident.AgentIDs, &incident.AlertCount,
		&incident.EventCount, &incident.FirstEventCursor, &incident.LastEventCursor,
		&incident.StartedAt, &incident.LastEventAt, &incident.AcknowledgedAt,
		&incident.AcknowledgedBy, &incident.ResolvedAt, &incident.ResolvedBy,
		&incident.MutedAt, &incident.MutedBy, &incident.MutedReason,
		&incident.RootCause, &incident.Resolution, &incident.Notes,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get incident: %w", err)
	}
	return incident, nil
}

// ListOpenIncidents returns all non-resolved incidents.
func (s *Store) ListOpenIncidents() ([]Incident, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT id, title, status, severity, COALESCE(session_names, ''),
			COALESCE(agent_ids, ''), alert_count, event_count,
			first_event_cursor, last_event_cursor, started_at, last_event_at,
			acknowledged_at, COALESCE(acknowledged_by, ''), resolved_at,
			COALESCE(resolved_by, ''), muted_at, COALESCE(muted_by, ''),
			COALESCE(muted_reason, ''), COALESCE(root_cause, ''),
			COALESCE(resolution, ''), COALESCE(notes, '')
		FROM incidents
		WHERE status NOT IN ('resolved', 'muted')
		ORDER BY started_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list open incidents: %w", err)
	}
	defer rows.Close()

	var incidents []Incident
	for rows.Next() {
		var incident Incident
		if err := rows.Scan(
			&incident.ID, &incident.Title, &incident.Status, &incident.Severity,
			&incident.SessionNames, &incident.AgentIDs, &incident.AlertCount,
			&incident.EventCount, &incident.FirstEventCursor, &incident.LastEventCursor,
			&incident.StartedAt, &incident.LastEventAt, &incident.AcknowledgedAt,
			&incident.AcknowledgedBy, &incident.ResolvedAt, &incident.ResolvedBy,
			&incident.MutedAt, &incident.MutedBy, &incident.MutedReason,
			&incident.RootCause, &incident.Resolution, &incident.Notes,
		); err != nil {
			return nil, fmt.Errorf("scan incident: %w", err)
		}
		incidents = append(incidents, incident)
	}
	return incidents, rows.Err()
}

// UpdateIncidentStatus updates an incident's status and related fields.
func (s *Store) UpdateIncidentStatus(id string, status IncidentStatus, by string, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	var err error

	switch status {
	case IncidentStatusInvestigating:
		_, err = s.db.Exec(`UPDATE incidents SET status = ?, acknowledged_at = ?, acknowledged_by = ? WHERE id = ?`,
			status, now, by, id)
	case IncidentStatusResolved:
		_, err = s.db.Exec(`UPDATE incidents SET status = ?, resolved_at = ?, resolved_by = ?, resolution = ? WHERE id = ?`,
			status, now, by, reason, id)
	case IncidentStatusMuted:
		_, err = s.db.Exec(`UPDATE incidents SET status = ?, muted_at = ?, muted_by = ?, muted_reason = ? WHERE id = ?`,
			status, now, by, reason, id)
	default:
		_, err = s.db.Exec(`UPDATE incidents SET status = ? WHERE id = ?`, status, id)
	}

	if err != nil {
		return fmt.Errorf("update incident status: %w", err)
	}
	return nil
}

// LinkEventToIncident associates an attention event with an incident.
func (s *Store) LinkEventToIncident(incidentID string, eventCursor int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Insert link
	_, err := s.db.Exec(`
		INSERT OR IGNORE INTO incident_events (incident_id, event_cursor)
		VALUES (?, ?)`, incidentID, eventCursor)
	if err != nil {
		return fmt.Errorf("link event to incident: %w", err)
	}

	// Update incident counters
	_, err = s.db.Exec(`
		UPDATE incidents SET
			event_count = (SELECT COUNT(*) FROM incident_events WHERE incident_id = ?),
			last_event_cursor = ?,
			last_event_at = datetime('now')
		WHERE id = ?`, incidentID, eventCursor, incidentID)
	if err != nil {
		return fmt.Errorf("update incident counters: %w", err)
	}
	return nil
}

// LinkEventToIncident associates an attention event with an incident in an existing transaction.
func (tx *Tx) LinkEventToIncident(incidentID string, eventCursor int64) error {
	_, err := tx.tx.Exec(`
		INSERT OR IGNORE INTO incident_events (incident_id, event_cursor)
		VALUES (?, ?)`, incidentID, eventCursor)
	if err != nil {
		return fmt.Errorf("link event to incident: %w", err)
	}

	_, err = tx.tx.Exec(`
		UPDATE incidents SET
			event_count = (SELECT COUNT(*) FROM incident_events WHERE incident_id = ?),
			last_event_cursor = ?,
			last_event_at = datetime('now')
		WHERE id = ?`, incidentID, eventCursor, incidentID)
	if err != nil {
		return fmt.Errorf("update incident counters: %w", err)
	}
	return nil
}

// =============================================================================
// Watermark Operations
// =============================================================================

// GetWatermark retrieves a watermark by type and scope.
func (s *Store) GetWatermark(watermarkType, scope string) (*OutputWatermark, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	wm := &OutputWatermark{}
	err := s.db.QueryRow(`
		SELECT watermark_type, scope, last_cursor, last_ts,
			baseline_cursor, baseline_ts, COALESCE(baseline_hash, ''),
			COALESCE(consumer, ''), created_at, updated_at
		FROM output_watermarks WHERE watermark_type = ? AND scope = ?`,
		watermarkType, scope,
	).Scan(
		&wm.WatermarkType, &wm.Scope, &wm.LastCursor, &wm.LastTs,
		&wm.BaselineCursor, &wm.BaselineTs, &wm.BaselineHash,
		&wm.Consumer, &wm.CreatedAt, &wm.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get watermark: %w", err)
	}
	return wm, nil
}

// SetWatermark inserts or updates a watermark.
func (s *Store) SetWatermark(wm *OutputWatermark) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO output_watermarks (
			watermark_type, scope, last_cursor, last_ts,
			baseline_cursor, baseline_ts, baseline_hash, consumer, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(watermark_type, scope) DO UPDATE SET
			last_cursor = excluded.last_cursor,
			last_ts = excluded.last_ts,
			baseline_cursor = excluded.baseline_cursor,
			baseline_ts = excluded.baseline_ts,
			baseline_hash = excluded.baseline_hash,
			consumer = excluded.consumer,
			updated_at = excluded.updated_at`,
		wm.WatermarkType, wm.Scope, wm.LastCursor, wm.LastTs,
		wm.BaselineCursor, wm.BaselineTs, wm.BaselineHash,
		wm.Consumer, wm.CreatedAt, wm.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("set watermark: %w", err)
	}
	return nil
}

// SetWatermark inserts or updates a watermark in an existing transaction.
func (tx *Tx) SetWatermark(wm *OutputWatermark) error {
	_, err := tx.tx.Exec(`
		INSERT INTO output_watermarks (
			watermark_type, scope, last_cursor, last_ts,
			baseline_cursor, baseline_ts, baseline_hash, consumer, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(watermark_type, scope) DO UPDATE SET
			last_cursor = excluded.last_cursor,
			last_ts = excluded.last_ts,
			baseline_cursor = excluded.baseline_cursor,
			baseline_ts = excluded.baseline_ts,
			baseline_hash = excluded.baseline_hash,
			consumer = excluded.consumer,
			updated_at = excluded.updated_at`,
		wm.WatermarkType, wm.Scope, wm.LastCursor, wm.LastTs,
		wm.BaselineCursor, wm.BaselineTs, wm.BaselineHash,
		wm.Consumer, wm.CreatedAt, wm.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("set watermark: %w", err)
	}
	return nil
}

// =============================================================================
// Audit Operations
// =============================================================================

func defaultAuditExpiry(class RetentionClass, now time.Time) *time.Time {
	var expires time.Time
	switch class {
	case RetentionClassStandard, "":
		expires = now.Add(7 * 24 * time.Hour)
	case RetentionClassExtended:
		expires = now.Add(30 * 24 * time.Hour)
	case RetentionClassPermanent:
		return nil
	default:
		expires = now.Add(7 * 24 * time.Hour)
	}
	return &expires
}

func nullableString(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}

func mergeKnownOrigins(existingJSON, origin string) string {
	if existingJSON == "" && origin == "" {
		return ""
	}

	seen := make(map[string]struct{})
	if existingJSON != "" {
		var existing []string
		if err := DecodeJSON(existingJSON, &existing); err == nil {
			for _, candidate := range existing {
				if candidate == "" {
					continue
				}
				seen[candidate] = struct{}{}
			}
		}
	}
	if origin != "" {
		seen[origin] = struct{}{}
	}
	if len(seen) == 0 {
		return ""
	}

	origins := make([]string, 0, len(seen))
	for candidate := range seen {
		origins = append(origins, candidate)
	}
	sort.Strings(origins)
	return EncodeJSON(origins)
}

func upsertAuditActorTx(tx *sql.Tx, event *AuditEvent, now time.Time) error {
	var existingOrigins string
	err := tx.QueryRow(`
		SELECT COALESCE(known_origins, '')
		FROM audit_actors
		WHERE actor_type = ? AND actor_id = ?`,
		event.ActorType, event.ActorID,
	).Scan(&existingOrigins)

	mergedOrigins := mergeKnownOrigins(existingOrigins, event.ActorOrigin)

	switch {
	case err == sql.ErrNoRows:
		_, err = tx.Exec(`
			INSERT INTO audit_actors (
				actor_type, actor_id, first_seen_at, last_seen_at, event_count, known_origins
			) VALUES (?, ?, ?, ?, ?, ?)`,
			event.ActorType, event.ActorID, now, now, 1, nullableString(mergedOrigins),
		)
	case err != nil:
		return fmt.Errorf("get audit actor: %w", err)
	default:
		_, err = tx.Exec(`
			UPDATE audit_actors
			SET last_seen_at = ?, event_count = event_count + 1, known_origins = ?
			WHERE actor_type = ? AND actor_id = ?`,
			now, nullableString(mergedOrigins), event.ActorType, event.ActorID,
		)
	}
	if err != nil {
		return fmt.Errorf("upsert audit actor: %w", err)
	}
	return nil
}

// RecordAuditEvent inserts an audit event and returns its ID.
func (s *Store) RecordAuditEvent(event *AuditEvent) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	if event.Ts.IsZero() {
		event.Ts = now
	}
	if event.RetentionClass == "" {
		event.RetentionClass = RetentionClassStandard
	}
	if event.ExpiresAt == nil {
		event.ExpiresAt = defaultAuditExpiry(event.RetentionClass, now)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin audit event transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	result, err := tx.Exec(`
		INSERT INTO audit_events (
			ts, actor_type, actor_id, actor_origin, request_id, correlation_id,
			category, event_type, severity, entity_type, entity_id,
			previous_state, new_state, change_summary, reason, evidence,
			disclosure_state, retention_class, expires_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.Ts, event.ActorType, event.ActorID, event.ActorOrigin,
		event.RequestID, event.CorrelationID, event.Category, event.EventType,
		event.Severity, event.EntityType, event.EntityID, event.PreviousState,
		event.NewState, event.ChangeSummary, event.Reason, event.Evidence,
		event.DisclosureState, event.RetentionClass, event.ExpiresAt,
	)
	if err != nil {
		return 0, fmt.Errorf("record audit event: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get audit event id: %w", err)
	}

	if err := upsertAuditActorTx(tx, event, now); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit audit event transaction: %w", err)
	}
	committed = true

	return id, nil
}

// UpsertAuditActor records display metadata for a known actor.
func (s *Store) UpsertAuditActor(actor *AuditActor) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if actor == nil {
		return fmt.Errorf("upsert audit actor: nil actor")
	}
	if actor.FirstSeenAt.IsZero() {
		actor.FirstSeenAt = time.Now().UTC()
	}
	if actor.LastSeenAt.IsZero() {
		actor.LastSeenAt = actor.FirstSeenAt
	}

	_, err := s.db.Exec(`
		INSERT INTO audit_actors (
			actor_type, actor_id, display_name, description,
			first_seen_at, last_seen_at, event_count, known_origins
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(actor_type, actor_id) DO UPDATE SET
			display_name = COALESCE(NULLIF(excluded.display_name, ''), audit_actors.display_name),
			description = COALESCE(NULLIF(excluded.description, ''), audit_actors.description),
			first_seen_at = MIN(audit_actors.first_seen_at, excluded.first_seen_at),
			last_seen_at = MAX(audit_actors.last_seen_at, excluded.last_seen_at),
			event_count = MAX(audit_actors.event_count, excluded.event_count),
			known_origins = COALESCE(NULLIF(excluded.known_origins, ''), audit_actors.known_origins)`,
		actor.ActorType, actor.ActorID, nullableString(actor.DisplayName), nullableString(actor.Description),
		actor.FirstSeenAt, actor.LastSeenAt, actor.EventCount, nullableString(actor.KnownOrigins),
	)
	if err != nil {
		return fmt.Errorf("upsert audit actor: %w", err)
	}
	return nil
}

// GetAuditActor retrieves actor metadata by actor identity.
func (s *Store) GetAuditActor(actorType, actorID string) (*AuditActor, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	actor := &AuditActor{}
	err := s.db.QueryRow(`
		SELECT actor_type, actor_id, COALESCE(display_name, ''), COALESCE(description, ''),
			first_seen_at, last_seen_at, event_count, COALESCE(known_origins, '')
		FROM audit_actors
		WHERE actor_type = ? AND actor_id = ?`, actorType, actorID,
	).Scan(
		&actor.ActorType, &actor.ActorID, &actor.DisplayName, &actor.Description,
		&actor.FirstSeenAt, &actor.LastSeenAt, &actor.EventCount, &actor.KnownOrigins,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get audit actor: %w", err)
	}
	return actor, nil
}

// GetActorActivity returns audit events for a specific actor since the cutoff.
func (s *Store) GetActorActivity(actorType, actorID string, since time.Time) ([]AuditEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if since.IsZero() {
		since = time.Unix(0, 0).UTC()
	}

	rows, err := s.db.Query(`
		SELECT id, ts, actor_type, COALESCE(actor_id, ''), COALESCE(actor_origin, ''),
			COALESCE(request_id, ''), COALESCE(correlation_id, ''),
			category, event_type, severity, entity_type, entity_id,
			COALESCE(previous_state, ''), COALESCE(new_state, ''), change_summary,
			COALESCE(reason, ''), COALESCE(evidence, ''), COALESCE(disclosure_state, ''),
			retention_class, expires_at
		FROM audit_events
		WHERE actor_type = ? AND actor_id = ? AND ts >= ?
		ORDER BY ts DESC, id DESC`, actorType, actorID, since)
	if err != nil {
		return nil, fmt.Errorf("get actor activity: %w", err)
	}
	defer rows.Close()

	var events []AuditEvent
	for rows.Next() {
		var event AuditEvent
		if err := rows.Scan(
			&event.ID, &event.Ts, &event.ActorType, &event.ActorID, &event.ActorOrigin,
			&event.RequestID, &event.CorrelationID, &event.Category, &event.EventType,
			&event.Severity, &event.EntityType, &event.EntityID, &event.PreviousState,
			&event.NewState, &event.ChangeSummary, &event.Reason, &event.Evidence,
			&event.DisclosureState, &event.RetentionClass, &event.ExpiresAt,
		); err != nil {
			return nil, fmt.Errorf("scan actor activity: %w", err)
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

// GetAuditHistory returns audit events for an entity.
func (s *Store) GetAuditHistory(entityType, entityID string, limit int) ([]AuditEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT id, ts, actor_type, COALESCE(actor_id, ''), COALESCE(actor_origin, ''),
			COALESCE(request_id, ''), COALESCE(correlation_id, ''),
			category, event_type, severity, entity_type, entity_id,
			COALESCE(previous_state, ''), COALESCE(new_state, ''), change_summary,
			COALESCE(reason, ''), COALESCE(evidence, ''), COALESCE(disclosure_state, ''),
			retention_class, expires_at
		FROM audit_events
		WHERE entity_type = ? AND entity_id = ?
		ORDER BY ts DESC
		LIMIT ?`, entityType, entityID, limit)
	if err != nil {
		return nil, fmt.Errorf("get audit history: %w", err)
	}
	defer rows.Close()

	var events []AuditEvent
	for rows.Next() {
		var event AuditEvent
		if err := rows.Scan(
			&event.ID, &event.Ts, &event.ActorType, &event.ActorID, &event.ActorOrigin,
			&event.RequestID, &event.CorrelationID, &event.Category, &event.EventType,
			&event.Severity, &event.EntityType, &event.EntityID, &event.PreviousState,
			&event.NewState, &event.ChangeSummary, &event.Reason, &event.Evidence,
			&event.DisclosureState, &event.RetentionClass, &event.ExpiresAt,
		); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

// RecordAuditDecision inserts a compact decision-history record.
func (s *Store) RecordAuditDecision(decision *AuditDecision) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if decision == nil {
		return 0, fmt.Errorf("record audit decision: nil decision")
	}
	if decision.DecisionAt.IsZero() {
		decision.DecisionAt = time.Now().UTC()
	}

	result, err := s.db.Exec(`
		INSERT INTO audit_decision_log (
			decision_type, decision_at, actor_type, actor_id,
			entity_type, entity_id, reason, expires_at, audit_event_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		decision.DecisionType, decision.DecisionAt, decision.ActorType, nullableString(decision.ActorID),
		decision.EntityType, decision.EntityID, nullableString(decision.Reason), decision.ExpiresAt, decision.AuditEventID,
	)
	if err != nil {
		return 0, fmt.Errorf("record audit decision: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get audit decision id: %w", err)
	}
	return id, nil
}

// GetDecisionHistory returns compact decision history for an entity.
func (s *Store) GetDecisionHistory(entityType, entityID string, limit int) ([]AuditDecision, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT id, decision_type, decision_at, actor_type, COALESCE(actor_id, ''),
			entity_type, entity_id, COALESCE(reason, ''), expires_at, audit_event_id
		FROM audit_decision_log
		WHERE entity_type = ? AND entity_id = ?
		ORDER BY decision_at DESC, id DESC
		LIMIT ?`, entityType, entityID, limit)
	if err != nil {
		return nil, fmt.Errorf("get decision history: %w", err)
	}
	defer rows.Close()

	var decisions []AuditDecision
	for rows.Next() {
		var decision AuditDecision
		var auditEventID sql.NullInt64
		if err := rows.Scan(
			&decision.ID, &decision.DecisionType, &decision.DecisionAt, &decision.ActorType,
			&decision.ActorID, &decision.EntityType, &decision.EntityID, &decision.Reason,
			&decision.ExpiresAt, &auditEventID,
		); err != nil {
			return nil, fmt.Errorf("scan audit decision: %w", err)
		}
		if auditEventID.Valid {
			decision.AuditEventID = &auditEventID.Int64
		}
		decisions = append(decisions, decision)
	}
	return decisions, rows.Err()
}

// GetRequestTrace returns all audit events for a request ID.
func (s *Store) GetRequestTrace(requestID string) ([]AuditEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT id, ts, actor_type, COALESCE(actor_id, ''), COALESCE(actor_origin, ''),
			COALESCE(request_id, ''), COALESCE(correlation_id, ''),
			category, event_type, severity, entity_type, entity_id,
			COALESCE(previous_state, ''), COALESCE(new_state, ''), change_summary,
			COALESCE(reason, ''), COALESCE(evidence, ''), COALESCE(disclosure_state, ''),
			retention_class, expires_at
		FROM audit_events
		WHERE request_id = ?
		ORDER BY ts ASC`, requestID)
	if err != nil {
		return nil, fmt.Errorf("get request trace: %w", err)
	}
	defer rows.Close()

	var events []AuditEvent
	for rows.Next() {
		var event AuditEvent
		if err := rows.Scan(
			&event.ID, &event.Ts, &event.ActorType, &event.ActorID, &event.ActorOrigin,
			&event.RequestID, &event.CorrelationID, &event.Category, &event.EventType,
			&event.Severity, &event.EntityType, &event.EntityID, &event.PreviousState,
			&event.NewState, &event.ChangeSummary, &event.Reason, &event.Evidence,
			&event.DisclosureState, &event.RetentionClass, &event.ExpiresAt,
		); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

// GCExpiredAuditEvents deletes expired audit events.
func (s *Store) GCExpiredAuditEvents() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`
		DELETE FROM audit_events
		WHERE retention_class != 'permanent' AND expires_at < datetime('now')`)
	if err != nil {
		return 0, fmt.Errorf("gc expired audit events: %w", err)
	}
	return result.RowsAffected()
}

// GCExpiredAuditDecisions deletes expired decision-history entries.
func (s *Store) GCExpiredAuditDecisions() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`
		DELETE FROM audit_decision_log
		WHERE expires_at IS NOT NULL AND expires_at < datetime('now')`)
	if err != nil {
		return 0, fmt.Errorf("gc expired audit decisions: %w", err)
	}
	return result.RowsAffected()
}

// CompactAuditDecisionLog keeps only the most recent maxRows decisions.
func (s *Store) CompactAuditDecisionLog(maxRows int) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if maxRows <= 0 {
		return 0, nil
	}

	result, err := s.db.Exec(`
		DELETE FROM audit_decision_log
		WHERE id NOT IN (
			SELECT id
			FROM audit_decision_log
			ORDER BY decision_at DESC, id DESC
			LIMIT ?
		)`, maxRows)
	if err != nil {
		return 0, fmt.Errorf("compact audit decision log: %w", err)
	}
	return result.RowsAffected()
}

// =============================================================================
// Projection Cleanup
// =============================================================================

// ClearStaleProjections removes projections past their staleness threshold.
func (s *Store) ClearStaleProjections(olderThan time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-olderThan)

	tables := []string{
		"runtime_sessions",
		"runtime_agents",
		"runtime_work",
		"runtime_coordination",
		"runtime_quota",
	}

	for _, table := range tables {
		_, err := s.db.Exec(
			fmt.Sprintf("DELETE FROM %s WHERE stale_after < ?", table),
			cutoff,
		)
		if err != nil {
			return fmt.Errorf("clear stale %s: %w", table, err)
		}
	}
	return nil
}

// =============================================================================
// Helper: JSON Encoding for Details
// =============================================================================

// EncodeJSON marshals a value to JSON string, or returns empty string on error.
func EncodeJSON(v interface{}) string {
	if v == nil {
		return ""
	}
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(data)
}

// DecodeJSON unmarshals a JSON string into the provided pointer.
func DecodeJSON(data string, v interface{}) error {
	if data == "" {
		return nil
	}
	return json.Unmarshal([]byte(data), v)
}
