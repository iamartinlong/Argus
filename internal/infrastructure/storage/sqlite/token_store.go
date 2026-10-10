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

type ApiTokenStore struct {
	db dbexec.SQLDB
}

func NewApiTokenStore(db dbexec.SQLDB) *ApiTokenStore { return &ApiTokenStore{db: db} }

const tokenColumns = `id, user_id, name, token_hash, scopes, expires_at, last_used_at, created_at`

func scanToken(row interface{ Scan(...any) error }) (*domain.ApiToken, error) {
	var t domain.ApiToken
	var scopes string
	var exp, last sql.NullTime
	if err := row.Scan(&t.ID, &t.UserID, &t.Name, &t.TokenHash, &scopes, &exp, &last, &t.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	_ = json.Unmarshal([]byte(scopes), &t.Scopes)
	if exp.Valid {
		t.ExpiresAt = &exp.Time
	}
	if last.Valid {
		t.LastUsedAt = &last.Time
	}
	return &t, nil
}

func (s *ApiTokenStore) Create(ctx context.Context, t *domain.ApiToken) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO api_tokens
        (id, user_id, name, token_hash, scopes, expires_at, last_used_at, created_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.UserID, t.Name, t.TokenHash, encodeJSON(t.Scopes),
		nullTimePtr(t.ExpiresAt), nullTimePtr(t.LastUsedAt), t.CreatedAt)
	return err
}

func (s *ApiTokenStore) GetByID(ctx context.Context, id string) (*domain.ApiToken, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+tokenColumns+` FROM api_tokens WHERE id = ?`, id)
	return scanToken(row)
}

func (s *ApiTokenStore) GetByHash(ctx context.Context, tokenHash string) (*domain.ApiToken, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+tokenColumns+` FROM api_tokens WHERE token_hash = ?`, tokenHash)
	return scanToken(row)
}

func (s *ApiTokenStore) ListByUser(ctx context.Context, userID string) ([]*domain.ApiToken, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+tokenColumns+` FROM api_tokens WHERE user_id = ? ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*domain.ApiToken, 0)
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *ApiTokenStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM api_tokens WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *ApiTokenStore) TouchLastUsed(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, time.Now().UTC(), id)
	return err
}
