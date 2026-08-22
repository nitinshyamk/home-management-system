// Command hms is the home management system.
//
// With no flags it opens the read-only browser. --verify runs the integrity job
// and --checkpoint records replay checkpoints; both are also what a scheduled
// job would call. --render replays a key script and prints the frames, which is
// what a design review looks at.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"home-management-system/internal/app"
	"home-management-system/internal/db"
	"home-management-system/internal/ledger"
	"home-management-system/internal/tui"
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
	info := flag.Bool("info", false, "print database details and exit")
	render := flag.String("render", "", "replay a .keys script and print each frame (for design review)")
	width := flag.Int("width", 100, "terminal width for --render")
	height := flag.Int("height", 30, "terminal height for --render")
	colour := flag.Bool("color", false, "keep colour in --render output (pipe to less -R)")
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

	ctx := context.Background()
	proc := ledger.New(conn)

	if *info {
		fmt.Printf("home-management-system\n")
		fmt.Printf("  database:          %s\n", cfg.DSN)
		fmt.Printf("  migration version: %d\n", version)
		fmt.Printf("  schema generation: %d\n", generation)
		return nil
	}

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

	ctrl := app.Open(conn)

	// --render replays a key script and prints each frame, so a design review
	// is about a specific screen rather than a description of one. It is the
	// Simulator with a main() around it: same model, same keys, same loop.
	if *render != "" {
		return tui.RenderFile(ctx, ctrl, *render, os.Stdout, *width, *height, *colour)
	}

	// `hms import FILE` opens the plan screen. It is a positional argument
	// rather than a flag because it is a different thing to do, not a different
	// way of browsing.
	if args := flag.Args(); len(args) == 2 && args[0] == "import" {
		model, err := tui.Import(ctx, ctrl, args[1])
		if err != nil {
			return err
		}
		return tui.RunModel(model)
	}

	// No flags: browse.
	return tui.Run(ctx, ctrl)
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
