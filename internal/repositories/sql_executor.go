package repositories

import (
	"context"
	"database/sql"
)

// sqlExecutor is the common subset used by repositories that must be usable
// both through the connection pool and inside one caller-owned transaction.
// Keeping transaction ownership in the handler lets a multi-row metadata
// publication commit atomically without teaching individual repositories to
// begin or commit transactions themselves.
type sqlExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
