// Package postgres PostgreSQL 驱动实现（storage/dbexec.SQLDB）
package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/iamartinlong/Argus/internal/infrastructure/storage/dbexec"
)

// DB PostgreSQL 实现：SQL 参数占位符统一为 ?，内部重写为 $N
type DB struct {
	*sql.DB
}

type tx struct {
	*sql.Tx
}

func (t *tx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.Tx.ExecContext(ctx, rewriteQuery(query), args...)
}

func (t *tx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.Tx.QueryContext(ctx, rewriteQuery(query), args...)
}

func (t *tx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return t.Tx.QueryRowContext(ctx, rewriteQuery(query), args...)
}

func (s *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (dbexec.Tx, error) {
	t, err := s.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &tx{Tx: t}, nil
}

func Open(dsn string) (*DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	s := &DB{DB: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

// ExecContext 参数化执行：? → $N；INSERT OR IGNORE → ON CONFLICT DO NOTHING
func (s *DB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return s.DB.ExecContext(ctx, rewriteQuery(query), args...)
}

func (s *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return s.DB.QueryContext(ctx, rewriteQuery(query), args...)
}

func (s *DB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return s.DB.QueryRowContext(ctx, rewriteQuery(query), args...)
}

// rewriteQuery 方言转换：? → $1..$n；INSERT OR IGNORE → INSERT ... ON CONFLICT DO NOTHING
func rewriteQuery(query string) string {
	q := query
	if strings.Contains(q, "INSERT OR IGNORE") {
		q = rewriteInsertOrIgnore(q)
	}
	if !strings.Contains(q, "?") {
		return q
	}
	var sb strings.Builder
	sb.Grow(len(q))
	n := 0
	for i := 0; i < len(q); i++ {
		if q[i] == '?' {
			n++
			sb.WriteString(fmt.Sprintf("$%d", n))
		} else {
			sb.WriteByte(q[i])
		}
	}
	return sb.String()
}

// rewriteInsertOrIgnore INSERT OR IGNORE INTO x (...) VALUES (...) → INSERT ... ON CONFLICT DO NOTHING
func rewriteInsertOrIgnore(query string) string {
	q := strings.Replace(query, "INSERT OR IGNORE", "INSERT", 1)
	idx := strings.Index(q, "VALUES (")
	if idx < 0 {
		return q
	}
	depth := 0
	for i := idx + len("VALUES"); i < len(q); i++ {
		switch q[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return q[:i+1] + " ON CONFLICT DO NOTHING" + q[i+1:]
			}
		}
	}
	return q
}
