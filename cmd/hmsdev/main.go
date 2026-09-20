// Command hmsdev is the test rig: the things you do TO hms rather than with it.
//
//	hmsdev render SCRIPT [--width N] [--height N] [--color]
//	hmsdev render SCRIPT --plan FILE      the import screen, rather than browse
//	hmsdev schema [--format json|prompt] [--out DIR]
//	hmsdev plan FILE                      bind a plan and print it as JSON
//
// These were flags on hms. They are not things a person does to their house --
// they are how a design review gets frames, how a test fixture gets written,
// and how an agent iterates against exactly what a person would review -- and
// carrying them on the production command line meant `hms --help` answered a
// question nobody asked and `hms -out schema` silently opened the browser.
//
// mise runs this. Nothing installs it.
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"home-management-system/internal/app"
	"home-management-system/internal/command"
	"home-management-system/internal/config"
	"home-management-system/internal/db"
	"home-management-system/internal/importer"
	"home-management-system/internal/tui"
)

const usage = `hmsdev -- the hms test rig, run by mise

  hmsdev render SCRIPT [--plan FILE] [--width N] [--height N] [--color]
      replay a .keys script and print each frame. --plan replays against the
      import screen instead of the browser.

  hmsdev schema [--format json|prompt] [--out DIR]
      print the agent contract, or write it into a directory.

  hmsdev plan FILE
      bind a plan and print the result as JSON, without a terminal.

  --db-path FILE    open this database instead of the configured one
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "hmsdev: %v\n", err)
		os.Exit(1)
	}
}

func run(argv []string) error {
	args, flags, err := split(argv)
	if err != nil {
		return err
	}
	if len(args) == 0 || args[0] == "help" {
		fmt.Print(usage)
		return nil
	}

	source, err := config.Resolve(flags["db-path"])
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

	switch args[0] {
	case "render":
		if len(args) != 2 {
			return fmt.Errorf("render takes one script\n\n%s", usage)
		}
		width, height := number(flags, "width", 100), number(flags, "height", 30)
		_, colour := flags["color"]
		if plan := flags["plan"]; plan != "" {
			return tui.RenderImport(ctx, ctrl, plan, args[1], os.Stdout, width, height, colour)
		}
		return tui.RenderFile(ctx, ctrl, args[1], os.Stdout, width, height, colour)

	case "schema":
		format := flags["format"]
		if format == "" {
			format = "prompt"
		}
		return writeSchema(ctx, ctrl, format, flags["out"])

	case "plan":
		if len(args) != 2 {
			return fmt.Errorf("plan takes one file\n\n%s", usage)
		}
		return printPlan(ctx, ctrl, args[1])

	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usage)
	}
}

// split separates positionals from flags, wherever they are written. Unlike the
// production command line this one is forgiving on purpose: it is driven by
// mise task definitions and by whoever is looking at a frame, not by somebody
// who has to remember it.
func split(argv []string) ([]string, map[string]string, error) {
	takesValue := map[string]bool{
		"db-path": true, "width": true, "height": true,
		"format": true, "out": true, "plan": true,
	}

	var positional []string
	flags := map[string]string{}
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if !strings.HasPrefix(arg, "-") {
			positional = append(positional, arg)
			continue
		}
		name, value, joined := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if !joined && takesValue[name] {
			if i+1 >= len(argv) {
				return nil, nil, fmt.Errorf("--%s needs a value", name)
			}
			i++
			value = argv[i]
		}
		flags[name] = value
	}
	return positional, flags, nil
}

func number(flags map[string]string, name string, fallback int) int {
	n, err := strconv.Atoi(flags[name])
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

// writeSchema emits the command vocabulary against this house.
func writeSchema(ctx context.Context, ctrl app.Controller, format, dir string) error {
	house, err := app.House(ctx, ctrl)
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

// printPlan binds a file and reports it, without a terminal and without
// applying anything.
//
// An agent never applies anything. It produces a file; a person approves it.
func printPlan(ctx context.Context, ctrl app.Controller, path string) error {
	rows, err := importer.ReadFile(path)
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
