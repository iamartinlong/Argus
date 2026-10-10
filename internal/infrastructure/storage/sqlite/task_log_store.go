package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/storage/dbexec"
)

type NotificationLogStore struct {
	db dbexec.SQLDB
}

func NewNotificationLogStore(db dbexec.SQLDB) *NotificationLogStore { return &NotificationLogStore{db: db} }

func (s *NotificationLogStore) Append(ctx context.Context, log *domain.NotificationLog) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO notification_logs
        (id, cert_id, rule_id, days_before, sent_at) VALUES (?, ?, ?, ?, ?)`,
		log.ID, log.CertID, log.RuleID, log.DaysBefore, log.SentAt)
	return err
}

func (s *NotificationLogStore) HasSent(ctx context.Context, certID, ruleID string, daysBefore int) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM notification_logs
        WHERE cert_id = ? AND rule_id = ? AND days_before = ?`, certID, ruleID, daysBefore).Scan(&n)
	return n > 0, err
}

func (s *NotificationLogStore) DeleteOlderThan(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM notification_logs WHERE sent_at < ?`, before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

type TaskLogStore struct {
	db dbexec.SQLDB
}

func NewTaskLogStore(db dbexec.SQLDB) *TaskLogStore { return &TaskLogStore{db: db} }

func (s *TaskLogStore) Create(ctx context.Context, log *domain.TaskLog) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO task_logs
        (id, type, status, detail, summary, started_at, finished_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		log.ID, log.Type, log.Status, log.Detail, log.Summary, log.StartedAt, nullTimePtr(log.FinishedAt))
	return err
}

func (s *TaskLogStore) Update(ctx context.Context, log *domain.TaskLog) error {
	_, err := s.db.ExecContext(ctx, `UPDATE task_logs SET
        status = ?, detail = ?, summary = ?, started_at = ?, finished_at = ? WHERE id = ?`,
		log.Status, log.Detail, log.Summary, log.StartedAt, nullTimePtr(log.FinishedAt), log.ID)
	return err
}

func (s *TaskLogStore) List(ctx context.Context, limit, offset int) ([]*domain.TaskLog, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, type, status, detail, summary, started_at, finished_at
        FROM task_logs ORDER BY started_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*domain.TaskLog, 0)
	for rows.Next() {
		var l domain.TaskLog
		var fa sql.NullTime
		if err := rows.Scan(&l.ID, &l.Type, &l.Status, &l.Detail, &l.Summary, &l.StartedAt, &fa); err != nil {
			return nil, err
		}
		if fa.Valid {
			l.FinishedAt = &fa.Time
		}
		out = append(out, &l)
	}
	return out, rows.Err()
}

func (s *TaskLogStore) Count(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM task_logs`).Scan(&n)
	return n, err
}

func (s *TaskLogStore) DeleteOlderThan(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM task_logs WHERE started_at < ?`, before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
