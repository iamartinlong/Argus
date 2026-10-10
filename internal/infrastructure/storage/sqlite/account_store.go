package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/storage/dbexec"
)

type ProviderAccountStore struct {
	db dbexec.SQLDB
}

func NewProviderAccountStore(db dbexec.SQLDB) *ProviderAccountStore { return &ProviderAccountStore{db: db} }

const accountColumns = `id, provider, name, directory_url, contact, eab_kid, eab_hmac_enc,
    credentials_enc, status, last_error, created_at, updated_at`

func scanAccount(row interface{ Scan(...any) error }) (*domain.ProviderAccount, error) {
	var a domain.ProviderAccount
	if err := row.Scan(&a.ID, &a.Provider, &a.Name, &a.DirectoryURL, &a.Contact, &a.EABKID,
		&a.EABHMACKeyEnc, &a.CredentialsEnc, &a.Status, &a.LastError, &a.CreatedAt, &a.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &a, nil
}

func (s *ProviderAccountStore) Create(ctx context.Context, a *domain.ProviderAccount) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO provider_accounts
        (id, provider, name, directory_url, contact, eab_kid, eab_hmac_enc,
         credentials_enc, status, last_error, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.Provider, a.Name, a.DirectoryURL, a.Contact, a.EABKID, a.EABHMACKeyEnc,
		a.CredentialsEnc, a.Status, a.LastError, a.CreatedAt, a.UpdatedAt)
	return err
}

func (s *ProviderAccountStore) GetByID(ctx context.Context, id string) (*domain.ProviderAccount, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM provider_accounts WHERE id = ?`, id)
	return scanAccount(row)
}

func (s *ProviderAccountStore) List(ctx context.Context) ([]*domain.ProviderAccount, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+accountColumns+` FROM provider_accounts ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.ProviderAccount
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *ProviderAccountStore) Update(ctx context.Context, a *domain.ProviderAccount) error {
	res, err := s.db.ExecContext(ctx, `UPDATE provider_accounts SET
        provider = ?, name = ?, directory_url = ?, contact = ?, eab_kid = ?, eab_hmac_enc = ?,
        credentials_enc = ?, status = ?, last_error = ?, updated_at = ?
        WHERE id = ?`,
		a.Provider, a.Name, a.DirectoryURL, a.Contact, a.EABKID, a.EABHMACKeyEnc,
		a.CredentialsEnc, a.Status, a.LastError, time.Now().UTC(), a.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *ProviderAccountStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM provider_accounts WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}
