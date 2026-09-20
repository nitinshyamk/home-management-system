// Command hms is the home management system.
//
//	hms                 open the house
//	hms import [NAME]   walk a bulk import
//	hms verify          check stored state against the ledger
//	hms checkpoint      record replay checkpoints
//	hms info            say which database is open
//
// That is the whole of it. Everything else hms can be made to do -- replaying a
// key script into frames, printing the schema, binding a plan without a
// terminal -- is a way of TESTING hms rather than a way of using it, and lives
// in cmd/hmsdev, which only mise runs. A production command line that also
// carries the test rig is a command line where the important verbs are three
// entries down a list of nine.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"

	"home-management-system/internal/app"
	"home-management-system/internal/config"
	"home-management-system/internal/db"
	"home-management-system/internal/ledger"
	"home-management-system/internal/tui"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, errStopped) {
			return
		}
		fmt.Fprintf(os.Stderr, "hms: %v\n", err)
		os.Exit(1)
	}
}

func run(argv []string) error {
	inv, err := Parse(argv)
	if err != nil {
		return err
	}
	if inv.Verb == VerbHelp {
		fmt.Println(Usage())
		return nil
	}

	source, err := config.Resolve(inv.DBPath)
	if err != nil {
		return err
	}

	conn, err := db.Open(db.Config{DSN: source.Path})
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := db.Migrate(conn); err != nil {
		return err
	}

	ctx := context.Background()
	ctrl := app.Open(conn)

	switch inv.Verb {
	case VerbInfo:
		return runInfo(conn, source)
	case VerbCheckpoint:
		n, err := ledger.New(conn).CheckpointAll(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("checkpointed %d holdings\n", n)
		return nil
	case VerbVerify:
		return runVerify(ctx, ledger.New(conn))
	case VerbImport:
		return runImport(ctx, ctrl, inv.Arg)
	default:
		return tui.Run(ctx, ctrl)
	}
}

// runInfo reports where the data is and what decided that, so the thing to edit
// is named rather than guessed at.
func runInfo(conn *sql.DB, source config.Resolution) error {
	version, err := db.Version(conn)
	if err != nil {
		return err
	}
	var generation int
	if err := conn.QueryRow("SELECT generation FROM schema_info WHERE id = 1").Scan(&generation); err != nil {
		return fmt.Errorf("reading schema generation: %w", err)
	}

	fmt.Printf("home-management-system\n")
	fmt.Printf("  database:          %s\n", source.Path)
	fmt.Printf("  configured by:     %s\n", source.Source)
	fmt.Printf("  migration version: %d\n", version)
	fmt.Printf("  schema generation: %d\n", generation)
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
