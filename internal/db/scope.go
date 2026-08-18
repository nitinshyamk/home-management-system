package db

import (
	"context"
	"database/sql"
)

// Handle is what a query layer binds to. It is structurally identical to
// sqlc.DBTX, declared here so internal/db does not depend on generated code;
// Go's interface assignability makes the two interchangeable at the call site.
type Handle interface {
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
	PrepareContext(context.Context, string) (*sql.Stmt, error)
	QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
}

// Scope is a unit of work: either a transaction already in flight, or the pool
// with permission to begin one.
//
// It exists because the three write paths each need to be usable two ways. Used
// alone, origin.CreateBulkItem must be atomic by itself, so it begins a
// transaction. Used inside an operation that also records events, it must join
// the transaction the operation already opened -- otherwise "add the item, put
// it somewhere, record what arrived" is three units of work that can half-fail,
// which is the gap v01 left open.
//
// Scope is the ONE place that decision is made. Every write path calls
// Scope.Run and never inspects which case it is in, so no path can accidentally
// open a nested transaction (SQLite has no such thing) or accidentally commit
// early inside someone else's.
type Scope struct {
	conn *sql.DB
	tx   *sql.Tx
}

// Pool scopes work to the connection pool, beginning a transaction per unit.
func Pool(conn *sql.DB) Scope { return Scope{conn: conn} }

// Enlist scopes work to a transaction that is already open. The caller owns the
// commit, so Run must not.
func Enlist(tx *sql.Tx) Scope { return Scope{tx: tx} }

// Run executes fn as one unit of work.
//
// Enlisted, it hands over the running transaction and returns fn's error
// unchanged, leaving commit and rollback to whoever opened it. Pooled, it is
// InTx. The distinction is invisible to fn, which is the point.
func (s Scope) Run(ctx context.Context, fn func(*sql.Tx) error) error {
	if s.tx != nil {
		return fn(s.tx)
	}
	return InTx(ctx, s.conn, fn)
}

// Handle returns what queries outside a unit of work should bind to: the
// transaction when enlisted, so reads see the work in flight, and the pool
// otherwise.
func (s Scope) Handle() Handle {
	if s.tx != nil {
		return s.tx
	}
	return s.conn
}
