package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/storage/dbexec"
)

type DeployTargetStore struct {
	db dbexec.SQLDB
}

func NewDeployTargetStore(db dbexec.SQLDB) *DeployTargetStore { return &DeployTargetStore{db: db} }

const targetColumns = `id, name, type, config_enc, enabled, last_status, last_error, created_at, updated_at`

func scanTarget(row interface{ Scan(...any) error }) (*domain.DeployTarget, error) {
	var t domain.DeployTarget
	var enabled int
	if err := row.Scan(&t.ID, &t.Name, &t.Type, &t.ConfigEnc, &enabled, &t.LastStatus,
		&t.LastError, &t.CreatedAt, &t.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	t.Enabled = enabled != 0
	return &t, nil
}

func (s *DeployTargetStore) Create(ctx context.Context, t *domain.DeployTarget) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO deploy_targets
        (id, name, type, config_enc, enabled, last_status, last_error, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Name, t.Type, t.ConfigEnc, boolToInt(t.Enabled), t.LastStatus, t.LastError,
		t.CreatedAt, t.UpdatedAt)
	return err
}

func (s *DeployTargetStore) GetByID(ctx context.Context, id string) (*domain.DeployTarget, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+targetColumns+` FROM deploy_targets WHERE id = ?`, id)
	return scanTarget(row)
}

func (s *DeployTargetStore) List(ctx context.Context) ([]*domain.DeployTarget, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+targetColumns+` FROM deploy_targets ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*domain.DeployTarget, 0)
	for rows.Next() {
		t, err := scanTarget(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *DeployTargetStore) Update(ctx context.Context, t *domain.DeployTarget) error {
	res, err := s.db.ExecContext(ctx, `UPDATE deploy_targets SET
        name = ?, type = ?, config_enc = ?, enabled = ?, last_status = ?, last_error = ?, updated_at = ?
        WHERE id = ?`,
		t.Name, t.Type, t.ConfigEnc, boolToInt(t.Enabled), t.LastStatus, t.LastError,
		time.Now().UTC(), t.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *DeployTargetStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM deploy_targets WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}
