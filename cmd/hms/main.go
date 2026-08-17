// Command hms is the home management system.
//
// Stage 1: opens the database, applies migrations, and reports what it found.
// The TUI arrives in Stage 7.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"home-management-system/internal/db"
	"home-management-system/internal/ledger"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "hms: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	dbPath := flag.String("db-path", "", "path to the SQLite database (overrides HMS_DB_PATH)")
	verify := flag.Bool("verify", false, "run the integrity check and report discrepancies")
	checkpoint := flag.Bool("checkpoint", false, "record a replay checkpoint for every holding")
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
	fmt.Printf("  database:          %s\n", cfg.DSN)
	fmt.Printf("  migration version: %d\n", version)
	fmt.Printf("  schema generation: %d\n", generation)

	ctx := context.Background()
	proc := ledger.New(conn)

	if *checkpoint {
		n, err := proc.CheckpointAll(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("\ncheckpointed %d holdings\n", n)
	}

	if *verify {
		return runVerify(ctx, proc)
	}
	return nil
}

// runVerify reports and never repairs. Silently correcting stored state would
// destroy the only signal that a write skipped its event.
func runVerify(ctx context.Context, proc *ledger.Processor) error {
	report, err := proc.VerifyAll(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("\nintegrity: %d holdings checked\n", report.HoldingsChecked)
	if report.Clean() {
		fmt.Println("  no discrepancies")
		return nil
	}

	for _, d := range report.Discrepancies {
		fmt.Printf("  DISCREPANCY %s\n", d)
	}
	for _, id := range report.Orphans.Holdings {
		fmt.Printf("  ORPHAN holding %d has no creation event\n", id)
	}
	for _, id := range report.Orphans.Locations {
		fmt.Printf("  ORPHAN location %d has no creation event\n", id)
	}
	return fmt.Errorf("integrity check found %d discrepancies and %d orphans",
		len(report.Discrepancies), len(report.Orphans.Holdings)+len(report.Orphans.Locations))
}
