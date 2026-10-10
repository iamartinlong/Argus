package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/storage/dbexec"
)

type NotificationSendLogStore struct {
	db dbexec.SQLDB
}

func NewNotificationSendLogStore(db dbexec.SQLDB) *NotificationSendLogStore {
	return &NotificationSendLogStore{db: db}
}

func (s *NotificationSendLogStore) Append(ctx context.Context, log *domain.NotificationSendLog) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO notification_send_logs
        (id, cert_id, rule_id, rule_name, channel_id, channel_name, event, status, detail, sent_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.ID, log.CertID, log.RuleID, log.RuleName, log.ChannelID, log.ChannelName,
		log.Event, log.Status, log.Detail, log.SentAt)
	return err
}

// sendLogWhere 构建筛选 WHERE（与 List 一致）
func sendLogWhere(filter domain.NotificationSendLogQuery) (string, []any) {
	query := ` WHERE 1=1`
	args := make([]any, 0, 4)
	if filter.ChannelID != "" {
		query += ` AND channel_id = ?`
		args = append(args, filter.ChannelID)
	}
	if filter.Status != "" {
		query += ` AND status = ?`
		args = append(args, filter.Status)
	}
	if filter.Keyword != "" {
		query += ` AND (rule_name LIKE ? ESCAPE '\' OR channel_name LIKE ? ESCAPE '\' OR cert_id LIKE ? ESCAPE '\')`
		kw := "%" + escapeLike(filter.Keyword) + "%"
		args = append(args, kw, kw, kw)
	}
	return query, args
}

func (s *NotificationSendLogStore) List(ctx context.Context, filter domain.NotificationSendLogQuery, limit, offset int) ([]*domain.NotificationSendLog, error) {
	where, args := sendLogWhere(filter)
	query := `SELECT id, cert_id, rule_id, rule_name, channel_id, channel_name, event, status, detail, sent_at
        FROM notification_send_logs` + where + ` ORDER BY sent_at DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*domain.NotificationSendLog, 0)
	for rows.Next() {
		l, err := scanSendLog(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *NotificationSendLogStore) Count(ctx context.Context, filter domain.NotificationSendLogQuery) (int64, error) {
	where, args := sendLogWhere(filter)
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM notification_send_logs`+where, args...).Scan(&n)
	return n, err
}

func (s *NotificationSendLogStore) DeleteOlderThan(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM notification_send_logs WHERE sent_at < ?`, before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *NotificationSendLogStore) CountByStatus(ctx context.Context, status string) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM notification_send_logs WHERE status = ?`, status).Scan(&n)
	return n, err
}

func scanSendLog(row interface{ Scan(...any) error }) (*domain.NotificationSendLog, error) {
	var l domain.NotificationSendLog
	if err := row.Scan(&l.ID, &l.CertID, &l.RuleID, &l.RuleName, &l.ChannelID, &l.ChannelName,
		&l.Event, &l.Status, &l.Detail, &l.SentAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &l, nil
}
