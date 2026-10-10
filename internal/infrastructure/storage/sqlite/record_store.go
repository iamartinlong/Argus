package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/storage/dbexec"
)

type DeployRecordStore struct {
	db dbexec.SQLDB
}

func NewDeployRecordStore(db dbexec.SQLDB) *DeployRecordStore { return &DeployRecordStore{db: db} }

const recordColumns = `id, target_id, certificate_id, status, checksum, message, retries, deployed_at, created_at`

func scanRecord(row interface{ Scan(...any) error }) (*domain.DeployRecord, error) {
	var r domain.DeployRecord
	var da sql.NullTime
	if err := row.Scan(&r.ID, &r.TargetID, &r.CertificateID, &r.Status, &r.Checksum,
		&r.Message, &r.Retries, &da, &r.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	if da.Valid {
		r.DeployedAt = &da.Time
	}
	return &r, nil
}

func (s *DeployRecordStore) Create(ctx context.Context, r *domain.DeployRecord) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO deploy_records
        (id, target_id, certificate_id, status, checksum, message, retries, deployed_at, created_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.TargetID, r.CertificateID, r.Status, r.Checksum, r.Message, r.Retries,
		nullTimePtr(r.DeployedAt), r.CreatedAt)
	return err
}

func (s *DeployRecordStore) GetByID(ctx context.Context, id string) (*domain.DeployRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+recordColumns+` FROM deploy_records WHERE id = ?`, id)
	return scanRecord(row)
}

func (s *DeployRecordStore) ListByCertificate(ctx context.Context, certID string) ([]*domain.DeployRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+recordColumns+` FROM deploy_records
        WHERE certificate_id = ? ORDER BY created_at DESC`, certID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*domain.DeployRecord, 0)
	for rows.Next() {
		r, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *DeployRecordStore) Update(ctx context.Context, r *domain.DeployRecord) error {
	_, err := s.db.ExecContext(ctx, `UPDATE deploy_records SET
        status = ?, checksum = ?, message = ?, retries = ?, deployed_at = ? WHERE id = ?`,
		r.Status, r.Checksum, r.Message, r.Retries, nullTimePtr(r.DeployedAt), r.ID)
	return err
}

// UpdateResult 仅更新执行结果字段，不覆盖 retries（避免重试计数被旧值回写，
// 例如 agent 异步回执/失败重试时用读取时的快照 retries 覆盖 MarkRetrying 的递增）。
func (s *DeployRecordStore) UpdateResult(ctx context.Context, id string, status domain.DeployRecordStatus, checksum, message string, deployedAt *time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE deploy_records SET
        status = ?, checksum = ?, message = ?, deployed_at = ? WHERE id = ?`,
		status, checksum, message, nullTimePtr(deployedAt), id)
	return err
}

func (s *DeployRecordStore) ListFailedPending(ctx context.Context) ([]*domain.DeployRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+recordColumns+` FROM deploy_records
        WHERE status = 'failed' AND retries < 3 ORDER BY created_at LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*domain.DeployRecord, 0)
	for rows.Next() {
		r, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *DeployRecordStore) DeleteByCertificate(ctx context.Context, certID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM deploy_records WHERE certificate_id = ?`, certID)
	return err
}

func (s *DeployRecordStore) DeleteByTarget(ctx context.Context, targetID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM deploy_records WHERE target_id = ?`, targetID)
	return err
}

func (s *DeployRecordStore) CountByStatus(ctx context.Context, status string) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM deploy_records WHERE status = ?`, status).Scan(&n)
	return n, err
}

// MarkRetrying 原子抢占：failed→running 且 retries+1（仅 retries<3 生效；已抢占/超限返回错误）
func (s *DeployRecordStore) MarkRetrying(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE deploy_records SET
        status = 'running', retries = retries + 1 WHERE id = ? AND status = 'failed' AND retries < 3`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// MarkTimedOut 将 agent 超时/未领取对应的 running 部署记录置为 failed。
// retries 置 3（达到上限）以排除 RetryFailed 的自动重试（该场景不重试）。
func (s *DeployRecordStore) MarkTimedOut(ctx context.Context, certID, targetID, message string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE deploy_records SET
        status = 'failed', message = ?, retries = 3, deployed_at = ?
        WHERE certificate_id = ? AND target_id = ? AND status = 'running'`,
		message, time.Now().UTC(), certID, targetID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
