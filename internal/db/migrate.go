package db

import (
	"database/sql"
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const migrationsDir = "migrations"

func configureGoose() error {
	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("db: set goose dialect: %w", err)
	}
	return nil
}

// Migrate applies every pending migration.
func Migrate(conn *sql.DB) error {
	if err := configureGoose(); err != nil {
		return err
	}
	if err := goose.Up(conn, migrationsDir); err != nil {
		return fmt.Errorf("db: migrate up: %w", err)
	}
	return nil
}

// Rollback reverts the most recent migration.
func Rollback(conn *sql.DB) error {
	if err := configureGoose(); err != nil {
		return err
	}
	if err := goose.Down(conn, migrationsDir); err != nil {
		return fmt.Errorf("db: migrate down: %w", err)
	}
	return nil
}

// Reset reverts every migration. Used by the round-trip test that proves each
// migration's Down section actually undoes its Up.
func Reset(conn *sql.DB) error {
	if err := configureGoose(); err != nil {
		return err
	}
	if err := goose.DownTo(conn, migrationsDir, 0); err != nil {
		return fmt.Errorf("db: migrate reset: %w", err)
	}
	return nil
}

// Version reports the currently applied migration version.
func Version(conn *sql.DB) (int64, error) {
	if err := configureGoose(); err != nil {
		return 0, err
	}
	v, err := goose.GetDBVersion(conn)
	if err != nil {
		return 0, fmt.Errorf("db: read version: %w", err)
	}
	return v, nil
}
