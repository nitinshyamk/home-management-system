// Package db owns the SQLite connection and the migration runner. It contains
// no domain concepts — those arrive in internal/domain and the migrations that
// follow 0001_bootstrap.
package db

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

const (
	defaultPath = "./hms.db"
	envPath     = "HMS_DB_PATH"
)

// Config describes how to reach the database.
type Config struct {
	// DSN is a SQLite data source name: a file path, or a file: URI for
	// shared-cache in-memory databases.
	DSN string

	// MaxOpenConns caps the connection pool. Shared-cache in-memory databases
	// must use 1, because the database lives only as long as a connection to it
	// does. Zero leaves the pool unbounded, which is correct for file databases.
	MaxOpenConns int
}

// DefaultConfig reads HMS_DB_PATH, falling back to ./hms.db.
func DefaultConfig() Config {
	path := os.Getenv(envPath)
	if path == "" {
		path = defaultPath
	}
	return Config{DSN: path}
}

// Open connects to SQLite and verifies the connection is usable.
//
// Foreign keys are enabled through the DSN rather than a PRAGMA statement.
// database/sql pools connections and a PRAGMA applies only to the connection
// that executed it, so issuing "PRAGMA foreign_keys = ON" after Open leaves
// every subsequently-created connection with foreign keys *off*. The DSN
// parameter applies to every connection the pool creates.
func Open(cfg Config) (*sql.DB, error) {
	if cfg.DSN == "" {
		return nil, fmt.Errorf("db: empty DSN")
	}

	conn, err := sql.Open("sqlite3", withForeignKeys(cfg.DSN))
	if err != nil {
		return nil, fmt.Errorf("db: open %q: %w", cfg.DSN, err)
	}
	if cfg.MaxOpenConns > 0 {
		conn.SetMaxOpenConns(cfg.MaxOpenConns)
	}

	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("db: ping %q: %w", cfg.DSN, err)
	}
	if err := verifyForeignKeys(conn); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

// withForeignKeys adds the foreign-key DSN parameter unless the caller set it.
func withForeignKeys(dsn string) string {
	if strings.Contains(dsn, "_foreign_keys=") || strings.Contains(dsn, "_fk=") {
		return dsn
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + "_foreign_keys=on"
}

// verifyForeignKeys asserts foreign-key enforcement is actually on.
//
// SQLite defaults it off, and every referential invariant in the schema assumes
// it is on. Checking here makes it a startup failure rather than a class of
// silent data corruption discovered later.
func verifyForeignKeys(conn *sql.DB) error {
	var enabled int
	if err := conn.QueryRow("PRAGMA foreign_keys").Scan(&enabled); err != nil {
		return fmt.Errorf("db: reading foreign_keys pragma: %w", err)
	}
	if enabled != 1 {
		return fmt.Errorf("db: foreign key enforcement is off; every referential invariant depends on it")
	}
	return nil
}
