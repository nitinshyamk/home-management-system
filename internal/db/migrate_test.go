package db

import (
	"database/sql"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
)

var seq atomic.Uint64

// newConn mirrors testsupport.NewEmptyDB. It is duplicated here rather than
// imported because testsupport depends on this package, and importing it back
// would be a cycle.
func newConn(t *testing.T) *sql.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:dbtest_%d?mode=memory&cache=shared", seq.Add(1))
	conn, err := Open(Config{DSN: dsn, MaxOpenConns: 1})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestOpenEnablesForeignKeys(t *testing.T) {
	conn := newConn(t)

	var enabled int
	if err := conn.QueryRow("PRAGMA foreign_keys").Scan(&enabled); err != nil {
		t.Fatalf("read pragma: %v", err)
	}
	if enabled != 1 {
		t.Errorf("foreign_keys = %d, want 1", enabled)
	}
}

// TestForeignKeysSurviveNewConnections is the regression test for the bug this
// harness exists to avoid: a PRAGMA applies only to the connection that ran it,
// so a pooled connection opened later would have foreign keys off.
func TestForeignKeysSurviveNewConnections(t *testing.T) {
	dsn := fmt.Sprintf("file:dbtest_pool_%d?mode=memory&cache=shared", seq.Add(1))
	conn, err := Open(Config{DSN: dsn})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(4)

	// Hold several connections open simultaneously so the pool is forced to
	// create new ones, then check each reports foreign keys enabled.
	const parallel = 4
	txs := make([]*sql.Tx, 0, parallel)
	for i := 0; i < parallel; i++ {
		tx, err := conn.Begin()
		if err != nil {
			t.Fatalf("begin %d: %v", i, err)
		}
		txs = append(txs, tx)

		var enabled int
		if err := tx.QueryRow("PRAGMA foreign_keys").Scan(&enabled); err != nil {
			t.Fatalf("read pragma on conn %d: %v", i, err)
		}
		if enabled != 1 {
			t.Errorf("connection %d: foreign_keys = %d, want 1", i, enabled)
		}
	}
	for _, tx := range txs {
		tx.Rollback()
	}
}

func TestOpenRejectsEmptyDSN(t *testing.T) {
	if _, err := Open(Config{}); err == nil {
		t.Fatal("expected an error for an empty DSN")
	}
}

func TestMigrateAppliesBootstrap(t *testing.T) {
	conn := newConn(t)

	if err := Migrate(conn); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var generation int
	if err := conn.QueryRow("SELECT generation FROM schema_info WHERE id = 1").Scan(&generation); err != nil {
		t.Fatalf("read schema_info: %v", err)
	}
	if generation != 1 {
		t.Errorf("generation = %d, want 1", generation)
	}

	v, err := Version(conn)
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if v != 1 {
		t.Errorf("version = %d, want 1", v)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	conn := newConn(t)

	for i := 0; i < 3; i++ {
		if err := Migrate(conn); err != nil {
			t.Fatalf("migrate pass %d: %v", i, err)
		}
	}
	var n int
	if err := conn.QueryRow("SELECT count(*) FROM schema_info").Scan(&n); err != nil {
		t.Fatalf("count schema_info: %v", err)
	}
	if n != 1 {
		t.Errorf("schema_info rows = %d, want 1", n)
	}
}

// TestMigrationRoundTrip proves every Down section actually undoes its Up.
// Comparing the full schema dump catches a Down that drops a table but leaves an
// index, trigger, or constraint behind — the usual way a down migration rots.
func TestMigrationRoundTrip(t *testing.T) {
	conn := newConn(t)

	if err := Migrate(conn); err != nil {
		t.Fatalf("initial migrate: %v", err)
	}
	before := schemaDump(t, conn)

	if err := Reset(conn); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if empty := schemaDump(t, conn); empty != "" {
		t.Errorf("schema not empty after reset:\n%s", empty)
	}

	if err := Migrate(conn); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	after := schemaDump(t, conn)

	if before != after {
		t.Errorf("schema differs after up/down/up round trip:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// schemaDump returns every object in the schema, ordered, excluding goose's own
// bookkeeping and SQLite's internal tables.
func schemaDump(t *testing.T, conn *sql.DB) string {
	t.Helper()

	rows, err := conn.Query(`
		SELECT type, name, COALESCE(sql, '')
		FROM sqlite_master
		WHERE name NOT LIKE 'sqlite_%'
		  AND name NOT LIKE 'goose_%'
		ORDER BY type, name`)
	if err != nil {
		t.Fatalf("read sqlite_master: %v", err)
	}
	defer rows.Close()

	var b strings.Builder
	for rows.Next() {
		var objType, name, ddl string
		if err := rows.Scan(&objType, &name, &ddl); err != nil {
			t.Fatalf("scan sqlite_master: %v", err)
		}
		fmt.Fprintf(&b, "%s %s\n%s\n\n", objType, name, ddl)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate sqlite_master: %v", err)
	}
	return b.String()
}
