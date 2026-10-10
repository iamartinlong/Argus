package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/storage/dbexec"
)

type NotificationTemplateStore struct {
	db dbexec.SQLDB
}

func NewNotificationTemplateStore(db dbexec.SQLDB) *NotificationTemplateStore {
	return &NotificationTemplateStore{db: db}
}

func (s *NotificationTemplateStore) Upsert(ctx context.Context, t *domain.NotificationTemplate) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO notification_templates (event, title, body, updated_at)
        VALUES (?, ?, ?, ?)
        ON CONFLICT(event) DO UPDATE SET title = excluded.title, body = excluded.body, updated_at = excluded.updated_at`,
		t.Event, t.Title, t.Body, t.UpdatedAt)
	return err
}

func (s *NotificationTemplateStore) Get(ctx context.Context, event domain.NotificationEvent) (*domain.NotificationTemplate, error) {
	row := s.db.QueryRowContext(ctx, `SELECT event, title, body, updated_at
        FROM notification_templates WHERE event = ?`, event)
	var t domain.NotificationTemplate
	if err := row.Scan(&t.Event, &t.Title, &t.Body, &t.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

func (s *NotificationTemplateStore) List(ctx context.Context) ([]*domain.NotificationTemplate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT event, title, body, updated_at FROM notification_templates`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*domain.NotificationTemplate, 0)
	for rows.Next() {
		var t domain.NotificationTemplate
		if err := rows.Scan(&t.Event, &t.Title, &t.Body, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

func (s *NotificationTemplateStore) Delete(ctx context.Context, event domain.NotificationEvent) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM notification_templates WHERE event = ?`, event)
	return err
}
