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

type CertificateStore struct {
	db dbexec.SQLDB
}

func NewCertificateStore(db dbexec.SQLDB) *CertificateStore { return &CertificateStore{db: db} }

const certColumns = `id, domains, primary_domain, provider, status, issuer_ca, not_before, not_after,
    certificate_pem, private_key_enc, account_id, verification, dns_config_enc, dns_provider_account_id, deploy_target_ids,
    notify_events, contact_name, contact_email, contact_phone, last_error, created_at, updated_at`

func scanCertificate(row interface{ Scan(...any) error }) (*domain.Certificate, error) {
	var c domain.Certificate
	var domains, targetIDs, notifyEvents string
	var nb, na sql.NullTime
	if err := row.Scan(&c.ID, &domains, &c.PrimaryDomain, &c.Provider, &c.Status, &c.IssuerCA,
		&nb, &na, &c.CertificatePEM, &c.PrivateKeyEnc, &c.AccountID, &c.Verification,
		&c.DNSConfigEnc, &c.DNSProviderAccountID, &targetIDs, &notifyEvents, &c.ContactName, &c.ContactEmail, &c.ContactPhone,
		&c.LastError, &c.CreatedAt, &c.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	if err := json.Unmarshal([]byte(domains), &c.Domains); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(targetIDs), &c.DeployTargetIDs)
	_ = json.Unmarshal([]byte(notifyEvents), &c.NotifyEvents)
	if c.NotifyEvents == nil {
		c.NotifyEvents = []domain.NotificationEvent{}
	}
	if nb.Valid {
		c.NotBefore = nb.Time
	}
	if na.Valid {
		c.NotAfter = na.Time
	}
	return &c, nil
}

func encodeDomains(v any) string {
	b, _ := json.Marshal(v)
	if b == nil {
		return "[]"
	}
	return string(b)
}

func (s *CertificateStore) Create(ctx context.Context, c *domain.Certificate) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO certificates
        (id, domains, primary_domain, provider, status, issuer_ca, not_before, not_after,
         certificate_pem, private_key_enc, account_id, verification, dns_config_enc, dns_provider_account_id, deploy_target_ids,
         notify_events, contact_name, contact_email, contact_phone, last_error, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, encodeDomains(c.Domains), c.PrimaryDomain, c.Provider, c.Status, c.IssuerCA,
		nullTime(c.NotBefore), nullTime(c.NotAfter), c.CertificatePEM, c.PrivateKeyEnc,
		c.AccountID, c.Verification, c.DNSConfigEnc, c.DNSProviderAccountID, encodeDomains(c.DeployTargetIDs),
		encodeDomains(c.NotifyEvents), c.ContactName, c.ContactEmail, c.ContactPhone, c.LastError, c.CreatedAt, c.UpdatedAt)
	return err
}

func (s *CertificateStore) GetByID(ctx context.Context, id string) (*domain.Certificate, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+certColumns+` FROM certificates WHERE id = ?`, id)
	return scanCertificate(row)
}

func (s *CertificateStore) List(ctx context.Context) ([]*domain.Certificate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+certColumns+` FROM certificates ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Certificate
	for rows.Next() {
		c, err := scanCertificate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *CertificateStore) Update(ctx context.Context, c *domain.Certificate) error {
	res, err := s.db.ExecContext(ctx, `UPDATE certificates SET
        domains = ?, primary_domain = ?, provider = ?, status = ?, issuer_ca = ?,
        not_before = ?, not_after = ?, certificate_pem = ?, private_key_enc = ?,
        account_id = ?, verification = ?, dns_config_enc = ?, dns_provider_account_id = ?, deploy_target_ids = ?,
        notify_events = ?, contact_name = ?, contact_email = ?, contact_phone = ?, last_error = ?, updated_at = ?
        WHERE id = ?`,
		encodeDomains(c.Domains), c.PrimaryDomain, c.Provider, c.Status, c.IssuerCA,
		nullTime(c.NotBefore), nullTime(c.NotAfter), c.CertificatePEM, c.PrivateKeyEnc,
		c.AccountID, c.Verification, c.DNSConfigEnc, c.DNSProviderAccountID, encodeDomains(c.DeployTargetIDs),
		encodeDomains(c.NotifyEvents), c.ContactName, c.ContactEmail, c.ContactPhone, c.LastError, time.Now().UTC(), c.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// UpdateStatus 仅更新状态列：调度扫描持久化 valid→expiring→expired 过渡态。
// 不整行 Update，避免与并发续期（重写 NotAfter/PEM 等）互相覆盖。
func (s *CertificateStore) UpdateStatus(ctx context.Context, id string, status domain.CertificateStatus) error {
	res, err := s.db.ExecContext(ctx, `UPDATE certificates SET status = ?, updated_at = ? WHERE id = ?`,
		status, time.Now().UTC(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *CertificateStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM certificates WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *CertificateStore) Count(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM certificates`).Scan(&n)
	return n, err
}

func (s *CertificateStore) CountExpiring(ctx context.Context, within time.Duration) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM certificates
        WHERE status IN ('valid','expiring') AND not_after IS NOT NULL AND not_after <= ?`,
		time.Now().UTC().Add(within)).Scan(&n)
	return n, err
}

func (s *CertificateStore) CountExpired(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM certificates WHERE status = 'expired'`).Scan(&n)
	return n, err
}

func (s *CertificateStore) CountByAccount(ctx context.Context, accountID string) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM certificates WHERE account_id = ?`, accountID).Scan(&n)
	return n, err
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
