// Command hms is the home management system.
//
// With no flags it opens the read-only browser. --verify runs the integrity job
// and --checkpoint records replay checkpoints; both are also what a scheduled
// job would call. --render replays a key script and prints the frames, which is
// what a design review looks at.
//
// `hms schema [DIR]` writes the contract an agent is given -- the command
// vocabulary, and the categories and places this house has -- to the terminal
// or into a directory. `hms import FILE` reviews what came back: the categories
// it proposes first, then the items and holdings.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"home-management-system/internal/app"
	"home-management-system/internal/command"
	"home-management-system/internal/db"
	"home-management-system/internal/importer"
	"home-management-system/internal/ledger"
	"home-management-system/internal/resolve"
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
	dryRun := flag.Bool("dry-run", false, "with `import FILE`, print the resolved plan as JSON instead of reviewing it")
	schemaFormat := flag.String("format", "prompt", "with `schema`, json or prompt")
	out := flag.String("out", "", "with `schema`, a directory to write the schema into instead of printing it")
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

	// Flags after the positional argument, because `hms import plan.csv
	// --dry-run` is how a person writes it and Go's flag package stops at the
	// first non-flag. Pulling them out here is less surprising than telling
	// everyone to put them first.
	args, trailing := splitTrailingFlags(flag.Args())
	if trailingFlags(trailing).has("dry-run") {
		*dryRun = true
	}
	if format, ok := trailing["format"]; ok && format != "" {
		*schemaFormat = format
	}
	if dir, ok := trailing["out"]; ok && dir != "" {
		*out = dir
	}
	if script, ok := trailing["render"]; ok && script != "" {
		*render = script
	}
	// Written after the positional too, and previously dropped there, so
	// `--width 104` silently produced a 100-column frame.
	if n, ok := trailingFlags(trailing).number("width"); ok {
		*width = n
	}
	if n, ok := trailingFlags(trailing).number("height"); ok {
		*height = n
	}

	// --render replays a key script and prints each frame, so a design review
	// is about a specific screen rather than a description of one. It is the
	// Simulator with a main() around it: same model, same keys, same loop.
	// The import screen has its own model, so it takes its own branch below;
	// this one is the browse model.
	if *render != "" && !(len(args) == 2 && args[0] == "import") {
		return tui.RenderFile(ctx, ctrl, *render, os.Stdout, *width, *height, *colour)
	}

	// `hms schema` emits the vocabulary so an agent targets a fixed spec
	// rather than a remembered one -- together with the categories and places
	// this household actually has, which is the half of the contract the code
	// cannot know.
	//
	// `hms schema DIR` and `hms schema --out DIR` write it into a directory
	// instead of printing it, because this is the one output of the program
	// meant to be handed to something else: it goes next to the photograph of
	// the receipt, or into the folder an agent is pointed at.
	if len(args) >= 1 && args[0] == "schema" {
		if len(args) >= 2 && *out == "" {
			*out = args[1]
		}
		return writeSchema(ctx, ctrl, *schemaFormat, *out)
	}

	// `hms import FILE --dry-run` reports the plan as JSON, so an agent can
	// iterate against exactly what a person would review.
	if len(args) == 2 && args[0] == "import" && *dryRun {
		return runDryRun(ctx, ctrl, args[1])
	}

	// `hms import FILE` opens the plan screen. It is a positional argument
	// rather than a flag because it is a different thing to do, not a different
	// way of browsing.
	// `hms import FILE --render script.keys` photographs the plan screen. The
	// review frames for every other screen come from --render; this one had no
	// way in, and it is the screen where being wrong costs the most.
	if len(args) == 2 && args[0] == "import" && *render != "" {
		return tui.RenderImport(ctx, ctrl, args[1], *render, os.Stdout, *width, *height, *colour)
	}

	if len(args) == 2 && args[0] == "import" {
		model, err := tui.Import(ctx, ctrl, args[1])
		if err != nil {
			return err
		}
		return tui.RunModel(model)
	}

	// No flags: browse.
	return tui.Run(ctx, ctrl)
}

// splitTrailingFlags separates positional arguments from flags written after
// them, returning the flags as name -> value ("" for a bare boolean).
func splitTrailingFlags(args []string) ([]string, map[string]string) {
	// The flags that take a value, so `--format json` is one flag and not a
	// flag plus a stray word. There is no way to know this from the text: a
	// bare `--dry-run` and a `--format` awaiting its value look identical.
	takesValue := map[string]bool{"format": true, "width": true, "height": true, "db-path": true, "render": true, "out": true}

	var positional []string
	flags := map[string]string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			positional = append(positional, arg)
			continue
		}
		key, value, joined := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if !joined && takesValue[key] && i+1 < len(args) {
			value = args[i+1]
			i++
		}
		flags[key] = value
	}
	return positional, flags
}

// trailingFlags is a tiny type so a bare `--dry-run` reads as true.
type trailingFlags map[string]string

func (f trailingFlags) has(name string) bool { _, ok := f[name]; return ok }

// number reads a numeric trailing flag, reporting whether it was given as one.
func (f trailingFlags) number(name string) (int, bool) {
	n, err := strconv.Atoi(f[name])
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// writeSchema emits the command vocabulary, against this house.
func writeSchema(ctx context.Context, ctrl app.Controller, format, dir string) error {
	house, err := readHouse(ctx, ctrl)
	if err != nil {
		return err
	}
	schema := command.NewSchema().WithHouse(house)

	if dir != "" {
		path, err := schema.Export(dir, format)
		if err != nil {
			return err
		}
		fmt.Printf("wrote %s\n", path)
		return nil
	}
	if format == "json" {
		return schema.WriteJSON(os.Stdout)
	}
	return schema.WritePrompt(os.Stdout)
}

// readHouse is the classification and the places, as the paths a row writes.
//
// Off the SAME index the importer resolves names against, so a path the schema
// offers is a path that resolves. A second walk of the two trees would be a
// second answer to keep in step, and the one thing worse than telling an agent
// nothing about the house is telling it something the resolver disagrees with.
//
// Archived candidates are left out. They stay in the index so that search can
// still find them, and nothing may be filed into one -- so offering one would
// be offering a path that is guaranteed to come back blocked.
func readHouse(ctx context.Context, ctrl app.Controller) (command.House, error) {
	index, err := ctrl.SearchIndex(ctx)
	if err != nil {
		return command.House{}, err
	}
	var house command.House
	for _, candidate := range index.All() {
		if candidate.Archived {
			continue
		}
		switch candidate.Kind {
		case resolve.KindCategory:
			house.Categories = append(house.Categories, candidate.Path)
		case resolve.KindLocation:
			house.Locations = append(house.Locations, candidate.Path)
		}
	}
	return house, nil
}

// runDryRun binds a file and reports it, without a terminal and without
// applying anything.
//
// An agent never applies anything. It produces a file; a person approves it.
func runDryRun(ctx context.Context, ctrl app.Controller, path string) error {
	rows, err := tui.ReadRows(path)
	if err != nil {
		return err
	}
	vocabulary, err := ctrl.Vocabulary(ctx)
	if err != nil {
		return err
	}
	plan := importer.Bind(ctx, vocabulary, path, rows)
	return importer.NewDryRun(plan, describer{ctx: ctx, ctrl: ctrl}).WriteJSON(os.Stdout)
}

type describer struct {
	ctx  context.Context
	ctrl app.Controller
}

func (d describer) Describe(entry importer.Entry) string {
	if entry.Command == nil {
		return entry.AsWritten()
	}
	return d.ctrl.Describe(d.ctx, entry.Command)
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
