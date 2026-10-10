package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/storage/dbexec"
)

type ContactStore struct {
	db dbexec.SQLDB
}

func NewContactStore(db dbexec.SQLDB) *ContactStore { return &ContactStore{db: db} }

const contactColumns = `id, name, email, phone, created_at, updated_at`

func scanContact(row interface{ Scan(...any) error }) (*domain.ContactPerson, error) {
	var c domain.ContactPerson
	if err := row.Scan(&c.ID, &c.Name, &c.Email, &c.Phone, &c.CreatedAt, &c.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

func (s *ContactStore) Create(ctx context.Context, c *domain.ContactPerson) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO contact_persons
        (id, name, email, phone, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.Email, c.Phone, c.CreatedAt, c.UpdatedAt)
	return err
}

func (s *ContactStore) GetByID(ctx context.Context, id string) (*domain.ContactPerson, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+contactColumns+` FROM contact_persons WHERE id = ?`, id)
	return scanContact(row)
}

func (s *ContactStore) List(ctx context.Context) ([]*domain.ContactPerson, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+contactColumns+` FROM contact_persons ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.ContactPerson
	for rows.Next() {
		c, err := scanContact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *ContactStore) Update(ctx context.Context, c *domain.ContactPerson) error {
	res, err := s.db.ExecContext(ctx, `UPDATE contact_persons SET
        name = ?, email = ?, phone = ?, updated_at = ?
        WHERE id = ?`,
		c.Name, c.Email, c.Phone, c.UpdatedAt, c.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *ContactStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM contact_persons WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}