package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/storage/dbexec"
)

type SessionStore struct {
	db dbexec.SQLDB
}

func NewSessionStore(db dbexec.SQLDB) *SessionStore { return &SessionStore{db: db} }

func (s *SessionStore) Create(ctx context.Context, session *domain.RefreshSession) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO refresh_sessions
        (token_hash, user_id, expires_at, revoked_at, created_at)
        VALUES (?, ?, ?, ?, ?)`,
		session.ID, session.UserID, session.ExpiresAt, session.RevokedAt, session.CreatedAt)
	return err
}

func (s *SessionStore) GetByTokenHash(ctx context.Context, tokenHash string) (*domain.RefreshSession, error) {
	var sess domain.RefreshSession
	err := s.db.QueryRowContext(ctx, `SELECT token_hash, user_id, expires_at, revoked_at, created_at
        FROM refresh_sessions WHERE token_hash = ?`, tokenHash).
		Scan(&sess.ID, &sess.UserID, &sess.ExpiresAt, &sess.RevokedAt, &sess.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &sess, nil
}

func (s *SessionStore) Revoke(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE refresh_sessions SET revoked_at = ? WHERE token_hash = ?`,
		time.Now().UTC(), tokenHash)
	return err
}

func (s *SessionStore) RevokeAllByUser(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE refresh_sessions SET revoked_at = ? WHERE user_id = ?`,
		time.Now().UTC(), userID)
	return err
}
