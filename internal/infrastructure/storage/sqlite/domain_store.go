package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/storage/dbexec"
)

type DomainStore struct {
	db dbexec.SQLDB
}

func NewDomainStore(db dbexec.SQLDB) *DomainStore {
	return &DomainStore{db: db}
}

const domainColumns = `id, name, expected_status, source, note, tags, created_at, updated_at`

func scanDomain(row interface{ Scan(...any) error }) (*domain.Domain, error) {
	var d domain.Domain
	var tags string
	if err := row.Scan(&d.ID, &d.Name, &d.ExpectedStatus, &d.Source, &d.Note, &tags, &d.CreatedAt, &d.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	_ = json.Unmarshal([]byte(tags), &d.Tags)
	if d.Tags == nil {
		d.Tags = []string{}
	}
	return &d, nil
}

func encodeTags(v any) string {
	b, _ := json.Marshal(v)
	if b == nil {
		return "[]"
	}
	return string(b)
}

func (s *DomainStore) Create(ctx context.Context, d *domain.Domain) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO domains
        (id, name, expected_status, source, note, tags, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.Name, string(d.ExpectedStatus), string(d.Source), d.Note, encodeTags(d.Tags), d.CreatedAt, d.UpdatedAt)
	return err
}

func (s *DomainStore) GetByID(ctx context.Context, id string) (*domain.Domain, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+domainColumns+` FROM domains WHERE id = ?`, id)
	return scanDomain(row)
}

func (s *DomainStore) GetByName(ctx context.Context, name string) (*domain.Domain, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+domainColumns+` FROM domains WHERE name = ?`, name)
	return scanDomain(row)
}

func (s *DomainStore) List(ctx context.Context) ([]*domain.Domain, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+domainColumns+` FROM domains ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Domain
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *DomainStore) Update(ctx context.Context, d *domain.Domain) error {
	res, err := s.db.ExecContext(ctx, `UPDATE domains SET
        name = ?, expected_status = ?, source = ?, note = ?, tags = ?, updated_at = ?
        WHERE id = ?`,
		d.Name, string(d.ExpectedStatus), string(d.Source), d.Note, encodeTags(d.Tags), d.UpdatedAt, d.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *DomainStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM domains WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}
