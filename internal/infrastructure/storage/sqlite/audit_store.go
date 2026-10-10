package sqlite

import (
	"context"
	"strings"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/storage/dbexec"
)

type AuditStore struct {
	db dbexec.SQLDB
}

func NewAuditStore(db dbexec.SQLDB) *AuditStore { return &AuditStore{db: db} }

func (s *AuditStore) Append(ctx context.Context, log *domain.AuditLog) error {
	if log.ID == "" {
		log.ID = newUUIDHex()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_logs
        (id, user_id, username, action, resource, detail, ip, user_agent, created_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.ID, log.UserID, log.Username, log.Action, log.Resource, log.Detail,
		log.IP, log.UserAgent, log.CreatedAt)
	return err
}

func (s *AuditStore) List(ctx context.Context, filter domain.AuditQuery, limit, offset int) ([]*domain.AuditLog, error) {
	where, args := auditWhere(filter)
	query := `SELECT id, user_id, username, action, resource, detail, ip, user_agent, created_at
        FROM audit_logs` + where + ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var logs []*domain.AuditLog
	for rows.Next() {
		var l domain.AuditLog
		if err := rows.Scan(&l.ID, &l.UserID, &l.Username, &l.Action, &l.Resource, &l.Detail,
			&l.IP, &l.UserAgent, &l.CreatedAt); err != nil {
			return nil, err
		}
		logs = append(logs, &l)
	}
	return logs, rows.Err()
}

// escapeLike 转义 LIKE 通配符（% _ \）
func escapeLike(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(s)
}

// auditWhere 构建筛选 WHERE（与 List 一致）
func auditWhere(filter domain.AuditQuery) (string, []any) {
	query := ` WHERE 1=1`
	args := make([]any, 0, 3)
	if filter.Username != "" {
		query += ` AND username = ?`
		args = append(args, filter.Username)
	}
	if filter.Action != "" {
		query += ` AND action LIKE ? ESCAPE '\'`
		args = append(args, escapeLike(filter.Action)+"%")
	}
	if filter.Keyword != "" {
		query += ` AND (username LIKE ? ESCAPE '\' OR resource LIKE ? ESCAPE '\' OR detail LIKE ? ESCAPE '\')`
		kw := "%" + escapeLike(filter.Keyword) + "%"
		args = append(args, kw, kw, kw)
	}
	return query, args
}

func (s *AuditStore) Count(ctx context.Context, filter domain.AuditQuery) (int64, error) {
	where, args := auditWhere(filter)
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM audit_logs`+where, args...).Scan(&n)
	return n, err
}

func (s *AuditStore) DeleteOlderThan(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM audit_logs WHERE created_at < ?`, before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func newUUIDHex() string {
	return "audit-" + randomHex(16)
}
