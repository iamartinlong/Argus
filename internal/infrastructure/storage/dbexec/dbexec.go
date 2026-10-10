// Package dbexec SQL 执行抽象（SQLite/PostgreSQL 通用；参数占位符统一使用 ?）
package dbexec

import (
	"context"
	"database/sql"
)

// SQLDB 数据库执行接口，由 sqlite.DB 与 postgres.DB 实现
type SQLDB interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	BeginTx(ctx context.Context, opts *sql.TxOptions) (Tx, error)
	Close() error
}

// Tx 事务执行接口（同样统一 ? 占位符）
type Tx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	Commit() error
	Rollback() error
}
