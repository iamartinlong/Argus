package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/storage/dbexec"
)

type OrderStore struct {
	db dbexec.SQLDB
}

func NewOrderStore(db dbexec.SQLDB) *OrderStore { return &OrderStore{db: db} }

const orderColumns = `id, certificate_id, type, status, error, attempts, started_at, finished_at`

func scanOrder(row interface{ Scan(...any) error }) (*domain.Order, error) {
	var o domain.Order
	var fa sql.NullTime
	if err := row.Scan(&o.ID, &o.CertificateID, &o.Type, &o.Status, &o.Error,
		&o.Attempts, &o.StartedAt, &fa); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	if fa.Valid {
		o.FinishedAt = &fa.Time
	}
	return &o, nil
}

func (s *OrderStore) Create(ctx context.Context, o *domain.Order) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO orders
        (id, certificate_id, type, status, error, attempts, started_at, finished_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		o.ID, o.CertificateID, o.Type, o.Status, o.Error, o.Attempts, o.StartedAt,
		nullTimePtr(o.FinishedAt))
	return err
}

func (s *OrderStore) GetByID(ctx context.Context, id string) (*domain.Order, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+orderColumns+` FROM orders WHERE id = ?`, id)
	return scanOrder(row)
}

func (s *OrderStore) ListByCertificate(ctx context.Context, certID string) ([]*domain.Order, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+orderColumns+` FROM orders
        WHERE certificate_id = ? ORDER BY started_at DESC`, certID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *OrderStore) Update(ctx context.Context, o *domain.Order) error {
	_, err := s.db.ExecContext(ctx, `UPDATE orders SET
        type = ?, status = ?, error = ?, attempts = ?, started_at = ?, finished_at = ?
        WHERE id = ?`,
		o.Type, o.Status, o.Error, o.Attempts, o.StartedAt, nullTimePtr(o.FinishedAt), o.ID)
	return err
}

func (s *OrderStore) RenewStats(ctx context.Context, since time.Time) (success, total int64, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT
        COALESCE(SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END), 0),
        COUNT(1)
        FROM orders WHERE type = 'renew' AND started_at >= ?`, since).Scan(&success, &total)
	return success, total, err
}

// RenewalTrend 按月统计续期（跨方言：查原始记录后在 Go 中按月分组）
func (s *OrderStore) RenewalTrend(ctx context.Context, since time.Time) ([]*domain.RenewalTrendPoint, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT started_at, status FROM orders
        WHERE type = 'renew' AND started_at >= ?`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byMonth := map[string]*domain.RenewalTrendPoint{}
	order := []string{}
	for rows.Next() {
		var started time.Time
		var status string
		if err := rows.Scan(&started, &status); err != nil {
			return nil, err
		}
		month := started.UTC().Format("2006-01")
		pt, ok := byMonth[month]
		if !ok {
			pt = &domain.RenewalTrendPoint{Month: firstOfMonth(started)}
			byMonth[month] = pt
			order = append(order, month)
		}
		pt.Total++
		if status == "success" {
			pt.Success++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// 按时间排序 + 空月补零
	out := make([]*domain.RenewalTrendPoint, 0)
	now := time.Now().UTC()
	for t := firstOfMonth(since); !t.After(now); t = t.AddDate(0, 1, 0) {
		key := t.Format("2006-01")
		if pt, ok := byMonth[key]; ok {
			out = append(out, pt)
		} else {
			out = append(out, &domain.RenewalTrendPoint{Month: t})
		}
	}
	return out, nil
}

func firstOfMonth(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func nullTimePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}
