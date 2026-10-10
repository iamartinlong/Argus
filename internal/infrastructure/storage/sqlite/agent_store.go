package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/storage/dbexec"
)

type AgentStore struct {
	db dbexec.SQLDB
}

func NewAgentStore(db dbexec.SQLDB) *AgentStore { return &AgentStore{db: db} }

const agentColumns = `id, name, token_hash, status, last_seen_at, version, os,
    upgrade_version, upgrade_sha256, upgrade_requested_at, enabled, created_at, updated_at`

func scanAgent(row interface{ Scan(...any) error }) (*domain.Agent, error) {
	var a domain.Agent
	var last, upgradeReq sql.NullTime
	var enabled int
	if err := row.Scan(&a.ID, &a.Name, &a.TokenHash, &a.Status, &last, &a.Version, &a.OS,
		&a.UpgradeVersion, &a.UpgradeSHA256, &upgradeReq, &enabled, &a.CreatedAt, &a.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	if last.Valid {
		a.LastSeenAt = &last.Time
	}
	if upgradeReq.Valid {
		a.UpgradeRequestedAt = &upgradeReq.Time
	}
	a.Enabled = enabled != 0
	return &a, nil
}

func (s *AgentStore) Create(ctx context.Context, a *domain.Agent) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO agents
        (id, name, token_hash, status, last_seen_at, version, os, enabled, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.Name, a.TokenHash, a.Status, nullTimePtr(a.LastSeenAt), a.Version, a.OS,
		boolToInt(a.Enabled), a.CreatedAt, a.UpdatedAt)
	return err
}

func (s *AgentStore) GetByID(ctx context.Context, id string) (*domain.Agent, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+agentColumns+` FROM agents WHERE id = ?`, id)
	return scanAgent(row)
}

func (s *AgentStore) GetByHash(ctx context.Context, tokenHash string) (*domain.Agent, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+agentColumns+` FROM agents WHERE token_hash = ?`, tokenHash)
	return scanAgent(row)
}

func (s *AgentStore) List(ctx context.Context) ([]*domain.Agent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+agentColumns+` FROM agents ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*domain.Agent, 0)
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *AgentStore) Update(ctx context.Context, a *domain.Agent) error {
	res, err := s.db.ExecContext(ctx, `UPDATE agents SET
        name = ?, status = ?, last_seen_at = ?, version = ?, os = ?,
        upgrade_version = ?, upgrade_sha256 = ?, upgrade_requested_at = ?, enabled = ?, updated_at = ? WHERE id = ?`,
		a.Name, a.Status, nullTimePtr(a.LastSeenAt), a.Version, a.OS,
		a.UpgradeVersion, a.UpgradeSHA256, nullTimePtr(a.UpgradeRequestedAt),
		boolToInt(a.Enabled), time.Now().UTC(), a.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *AgentStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM agents WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *AgentStore) CountByStatus(ctx context.Context, status string) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM agents WHERE status = ?`, status).Scan(&n)
	return n, err
}

// MarkStaleOffline 将心跳超时（last_seen_at 早于 cutoff）且启用中的在线 Agent 置为 offline
func (s *AgentStore) MarkStaleOffline(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE agents SET status = 'offline', updated_at = ? WHERE enabled = 1
        AND status = 'online' AND last_seen_at < ?`, time.Now().UTC(), cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ---------------- AgentTaskStore ----------------

type AgentTaskStore struct {
	db dbexec.SQLDB
}

func NewAgentTaskStore(db dbexec.SQLDB) *AgentTaskStore { return &AgentTaskStore{db: db} }

const taskColumns = `id, agent_id, certificate_id, certificate_domain, target_id, status, checksum, message, claimed_at, finished_at, created_at`

func scanAgentTask(row interface{ Scan(...any) error }) (*domain.AgentTask, error) {
	var t domain.AgentTask
	var claimed, finished sql.NullTime
	if err := row.Scan(&t.ID, &t.AgentID, &t.CertificateID, &t.CertificateDomain, &t.TargetID, &t.Status, &t.Checksum,
		&t.Message, &claimed, &finished, &t.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	if claimed.Valid {
		t.ClaimedAt = &claimed.Time
	}
	if finished.Valid {
		t.FinishedAt = &finished.Time
	}
	return &t, nil
}

func (s *AgentTaskStore) Create(ctx context.Context, t *domain.AgentTask) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO agent_tasks
        (id, agent_id, certificate_id, certificate_domain, target_id, status, checksum, message, claimed_at, finished_at, created_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.AgentID, t.CertificateID, t.CertificateDomain, t.TargetID, t.Status, t.Checksum, t.Message,
		nullTimePtr(t.ClaimedAt), nullTimePtr(t.FinishedAt), t.CreatedAt)
	return err
}

func (s *AgentTaskStore) GetByID(ctx context.Context, id string) (*domain.AgentTask, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM agent_tasks WHERE id = ?`, id)
	return scanAgentTask(row)
}

func (s *AgentTaskStore) ListByAgent(ctx context.Context, agentID string, limit int) ([]*domain.AgentTask, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+taskColumns+` FROM agent_tasks
        WHERE agent_id = ? ORDER BY created_at DESC LIMIT ?`, agentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*domain.AgentTask, 0)
	for rows.Next() {
		t, err := scanAgentTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ClaimNext 原子领取 pending 任务（SQLite 单写连接保证原子性）
func (s *AgentTaskStore) ClaimNext(ctx context.Context, agentID string) (*domain.AgentTask, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var row *sql.Row
	row = tx.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM agent_tasks
        WHERE agent_id = ? AND status = 'pending' ORDER BY created_at LIMIT 1`, agentID)
	task, err := scanAgentTask(row)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil // 无任务
		}
		return nil, err
	}
	now := time.Now().UTC()
	// 原子领取：UPDATE 影响行数为 0 说明已被其它实例领取（并发安全）
	res, err := tx.ExecContext(ctx, `UPDATE agent_tasks SET status = 'running', claimed_at = ?
        WHERE id = ? AND status = 'pending'`, now, task.ID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, nil // 已被其它 Agent 领取
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	task.Status = domain.AgentTaskRunning
	task.ClaimedAt = &now
	return task, nil
}

func (s *AgentTaskStore) Update(ctx context.Context, t *domain.AgentTask) error {
	_, err := s.db.ExecContext(ctx, `UPDATE agent_tasks SET
        status = ?, checksum = ?, message = ?, claimed_at = ?, finished_at = ? WHERE id = ?`,
		t.Status, t.Checksum, t.Message, nullTimePtr(t.ClaimedAt), nullTimePtr(t.FinishedAt), t.ID)
	return err
}

func (s *AgentTaskStore) MarkStaleTasks(ctx context.Context, olderThan time.Duration) ([]*domain.AgentTask, error) {
	cutoff := time.Now().UTC().Add(-olderThan)
	// 超时任务分两类：
	//  - running 且 claimed_at 超时（已领取未回执）
	//  - pending 且 created_at 超时（从未被任何 agent 领取）
	rows, err := s.db.QueryContext(ctx, `SELECT `+taskColumns+` FROM agent_tasks
        WHERE (status = 'running' AND claimed_at < ?) OR (status = 'pending' AND created_at < ?)`,
		cutoff, cutoff)
	if err != nil {
		return nil, err
	}
	affected := make([]*domain.AgentTask, 0)
	for rows.Next() {
		t, err := scanAgentTask(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		affected = append(affected, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(affected) == 0 {
		return nil, nil
	}
	_, err = s.db.ExecContext(ctx, `UPDATE agent_tasks SET status = 'failed',
        message = CASE WHEN status = 'pending' THEN 'agent 未领取超时' ELSE 'agent 超时未回执' END, finished_at = ?
        WHERE (status = 'running' AND claimed_at < ?) OR (status = 'pending' AND created_at < ?)`,
		time.Now().UTC(), cutoff, cutoff)
	if err != nil {
		return nil, err
	}
	return affected, nil
}

// DeleteByTarget 删除指向某部署目标的所有任务（目标删除时级联清理）
func (s *AgentTaskStore) DeleteByTarget(ctx context.Context, targetID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM agent_tasks WHERE target_id = ?`, targetID)
	return err
}
