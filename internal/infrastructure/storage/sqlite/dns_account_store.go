package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/storage/dbexec"
)

type DnsProviderAccountStore struct {
	db dbexec.SQLDB
}

func NewDnsProviderAccountStore(db dbexec.SQLDB) *DnsProviderAccountStore {
	return &DnsProviderAccountStore{db: db}
}

const dnsAccountColumns = `id, name, provider, credentials_enc, status, last_error, created_at, updated_at`

func scanDnsAccount(row interface{ Scan(...any) error }) (*domain.DnsProviderAccount, error) {
	var a domain.DnsProviderAccount
	if err := row.Scan(&a.ID, &a.Name, &a.Provider, &a.CredentialsEnc, &a.Status, &a.LastError, &a.CreatedAt, &a.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &a, nil
}

func (s *DnsProviderAccountStore) Create(ctx context.Context, a *domain.DnsProviderAccount) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO dns_provider_accounts
        (id, name, provider, credentials_enc, status, last_error, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.Name, string(a.Provider), a.CredentialsEnc, a.Status, a.LastError, a.CreatedAt, a.UpdatedAt)
	return err
}

func (s *DnsProviderAccountStore) GetByID(ctx context.Context, id string) (*domain.DnsProviderAccount, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+dnsAccountColumns+` FROM dns_provider_accounts WHERE id = ?`, id)
	return scanDnsAccount(row)
}

func (s *DnsProviderAccountStore) List(ctx context.Context) ([]*domain.DnsProviderAccount, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+dnsAccountColumns+` FROM dns_provider_accounts ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.DnsProviderAccount
	for rows.Next() {
		a, err := scanDnsAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *DnsProviderAccountStore) Update(ctx context.Context, a *domain.DnsProviderAccount) error {
	res, err := s.db.ExecContext(ctx, `UPDATE dns_provider_accounts SET
        name = ?, provider = ?, credentials_enc = ?, status = ?, last_error = ?, updated_at = ?
        WHERE id = ?`,
		a.Name, string(a.Provider), a.CredentialsEnc, a.Status, a.LastError, time.Now().UTC(), a.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *DnsProviderAccountStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM dns_provider_accounts WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}
