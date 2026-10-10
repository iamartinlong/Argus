package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/storage/dbexec"
)

type NotificationChannelStore struct {
	db dbexec.SQLDB
}

func NewNotificationChannelStore(db dbexec.SQLDB) *NotificationChannelStore {
	return &NotificationChannelStore{db: db}
}

const channelColumns = `id, name, type, config_enc, enabled, created_at, updated_at`

func scanChannel(row interface{ Scan(...any) error }) (*domain.NotificationChannelRecord, error) {
	var c domain.NotificationChannelRecord
	var enabled int
	if err := row.Scan(&c.ID, &c.Name, &c.Type, &c.ConfigEnc, &enabled, &c.CreatedAt, &c.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	c.Enabled = enabled != 0
	return &c, nil
}

func (s *NotificationChannelStore) Create(ctx context.Context, c *domain.NotificationChannelRecord) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO notification_channels
        (id, name, type, config_enc, enabled, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.Type, c.ConfigEnc, boolToInt(c.Enabled), c.CreatedAt, c.UpdatedAt)
	return err
}

func (s *NotificationChannelStore) GetByID(ctx context.Context, id string) (*domain.NotificationChannelRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+channelColumns+` FROM notification_channels WHERE id = ?`, id)
	return scanChannel(row)
}

func (s *NotificationChannelStore) List(ctx context.Context) ([]*domain.NotificationChannelRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+channelColumns+` FROM notification_channels ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*domain.NotificationChannelRecord, 0)
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *NotificationChannelStore) Update(ctx context.Context, c *domain.NotificationChannelRecord) error {
	res, err := s.db.ExecContext(ctx, `UPDATE notification_channels SET
        name = ?, type = ?, config_enc = ?, enabled = ?, updated_at = ? WHERE id = ?`,
		c.Name, c.Type, c.ConfigEnc, boolToInt(c.Enabled), time.Now().UTC(), c.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *NotificationChannelStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM notification_channels WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *NotificationChannelStore) Count(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM notification_channels`).Scan(&n)
	return n, err
}
