package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/storage/dbexec"
)

type UserStore struct {
	db dbexec.SQLDB
}

func NewUserStore(db dbexec.SQLDB) *UserStore { return &UserStore{db: db} }

const userColumns = `id, username, password_hash, role, status, must_change_password, created_at, updated_at`

func scanUser(row interface{ Scan(...any) error }) (*domain.User, error) {
	var u domain.User
	var mustChange int
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.Status, &mustChange, &u.CreatedAt, &u.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	u.MustChangePassword = mustChange != 0
	return &u, nil
}

func (s *UserStore) GetByID(ctx context.Context, id string) (*domain.User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id)
	return scanUser(row)
}

func (s *UserStore) GetByUsername(ctx context.Context, username string) (*domain.User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE username = ?`, username)
	return scanUser(row)
}

func (s *UserStore) List(ctx context.Context) ([]*domain.User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userColumns+` FROM users ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []*domain.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *UserStore) Create(ctx context.Context, user *domain.User) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO users
        (id, username, password_hash, role, status, must_change_password, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID, user.Username, user.PasswordHash, user.Role, user.Status,
		boolToInt(user.MustChangePassword), user.CreatedAt, user.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrConflict
		}
		return err
	}
	return nil
}

func (s *UserStore) Update(ctx context.Context, user *domain.User) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET
        username = ?, password_hash = ?, role = ?, status = ?, must_change_password = ?, updated_at = ?
        WHERE id = ?`,
		user.Username, user.PasswordHash, user.Role, user.Status,
		boolToInt(user.MustChangePassword), user.UpdatedAt, user.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *UserStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *UserStore) Count(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM users`).Scan(&n)
	return n, err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
