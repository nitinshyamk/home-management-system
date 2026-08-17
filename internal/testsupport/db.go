// Package testsupport provides the shared test harness. Every test that touches
// data uses a real SQLite database with real migrations — never a mock, because
// the schema's constraints and triggers are a large part of what is under test.
package testsupport

import (
	"database/sql"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"home-management-system/internal/db"
)

var dbSeq atomic.Uint64

// NewDB returns a migrated, isolated in-memory database for one test.
//
// The DSN is deliberately not ":memory:". database/sql pools connections, and
// each new connection to ":memory:" gets its own *fresh, empty* database — so a
// pool that opens a second connection silently loses the schema. A named
// shared-cache URI plus a single-connection pool gives every test one database
// that actually persists across the calls it makes.
func NewDB(t *testing.T) *sql.DB {
	t.Helper()
	conn := NewEmptyDB(t)
	if err := db.Migrate(conn); err != nil {
		t.Fatalf("testsupport: migrate: %v", err)
	}
	return conn
}

// NewEmptyDB is NewDB without the migrations, for tests that drive migration
// behaviour themselves.
func NewEmptyDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s_%d?mode=memory&cache=shared", sanitize(t.Name()), dbSeq.Add(1))
	conn, err := db.Open(db.Config{DSN: dsn, MaxOpenConns: 1})
	if err != nil {
		t.Fatalf("testsupport: open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// sanitize turns a test name into something safe for a SQLite URI.
func sanitize(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '_'
		}
	}, name)
}
