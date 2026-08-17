// Command hms is the home management system.
//
// Stage 1: opens the database, applies migrations, and reports what it found.
// The TUI arrives in Stage 7.
package main

import (
	"flag"
	"fmt"
	"os"

	"home-management-system/internal/db"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "hms: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	dbPath := flag.String("db-path", "", "path to the SQLite database (overrides HMS_DB_PATH)")
	flag.Parse()

	cfg := db.DefaultConfig()
	if *dbPath != "" {
		cfg.DSN = *dbPath
	}

	conn, err := db.Open(cfg)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := db.Migrate(conn); err != nil {
		return err
	}

	version, err := db.Version(conn)
	if err != nil {
		return err
	}

	var generation int
	if err := conn.QueryRow("SELECT generation FROM schema_info WHERE id = 1").Scan(&generation); err != nil {
		return fmt.Errorf("reading schema generation: %w", err)
	}

	fmt.Printf("home-management-system\n")
	fmt.Printf("  database:         %s\n", cfg.DSN)
	fmt.Printf("  migration version: %d\n", version)
	fmt.Printf("  schema generation: %d\n", generation)
	return nil
}
