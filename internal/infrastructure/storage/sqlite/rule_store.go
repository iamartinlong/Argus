package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/storage/dbexec"
)

type NotificationRuleStore struct {
	db dbexec.SQLDB
}

func NewNotificationRuleStore(db dbexec.SQLDB) *NotificationRuleStore { return &NotificationRuleStore{db: db} }

const ruleColumns = `id, name, event, channels, days_before, channel_ids, enabled, created_at, updated_at`

func scanRule(row interface{ Scan(...any) error }) (*domain.NotificationRule, error) {
	var r domain.NotificationRule
	var channels, days, channelIDs string
	var enabled int
	if err := row.Scan(&r.ID, &r.Name, &r.Event, &channels, &days, &channelIDs, &enabled, &r.CreatedAt, &r.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	_ = json.Unmarshal([]byte(channels), &r.Channels)
	_ = json.Unmarshal([]byte(days), &r.DaysBefore)
	_ = json.Unmarshal([]byte(channelIDs), &r.ChannelIDs)
	// 历史数据可能为 "null"：保证非 nil（前端与发送逻辑直接 .length/.range）
	if r.Channels == nil {
		r.Channels = []domain.NotificationChannel{}
	}
	if r.DaysBefore == nil {
		r.DaysBefore = []int{}
	}
	if r.ChannelIDs == nil {
		r.ChannelIDs = []string{}
	}
	r.Enabled = enabled != 0
	return &r, nil
}

// encodeJSON 序列化；nil 切片输出 "[]" 而非 "null"（避免读回 nil 导致前端崩溃）
func encodeJSON(v any) string {
	if v == nil {
		return "[]"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func (s *NotificationRuleStore) Create(ctx context.Context, r *domain.NotificationRule) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO notification_rules
        (id, name, event, channels, days_before, channel_ids, enabled, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.Name, r.Event, encodeJSON(r.Channels), encodeJSON(r.DaysBefore),
		encodeJSON(r.ChannelIDs), boolToInt(r.Enabled), r.CreatedAt, r.UpdatedAt)
	return err
}

func (s *NotificationRuleStore) GetByID(ctx context.Context, id string) (*domain.NotificationRule, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+ruleColumns+` FROM notification_rules WHERE id = ?`, id)
	return scanRule(row)
}

func (s *NotificationRuleStore) List(ctx context.Context) ([]*domain.NotificationRule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+ruleColumns+` FROM notification_rules ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.NotificationRule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *NotificationRuleStore) ListByEvent(ctx context.Context, event domain.NotificationEvent) ([]*domain.NotificationRule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+ruleColumns+` FROM notification_rules WHERE event = ?`, event)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.NotificationRule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *NotificationRuleStore) Update(ctx context.Context, r *domain.NotificationRule) error {
	_, err := s.db.ExecContext(ctx, `UPDATE notification_rules SET
        name = ?, event = ?, channels = ?, days_before = ?, channel_ids = ?, enabled = ?, updated_at = ?
        WHERE id = ?`,
		r.Name, r.Event, encodeJSON(r.Channels), encodeJSON(r.DaysBefore),
		encodeJSON(r.ChannelIDs), boolToInt(r.Enabled), time.Now().UTC(), r.ID)
	return err
}

func (s *NotificationRuleStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM notification_rules WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}
