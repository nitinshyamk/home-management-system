package main

import (
	"fmt"
	"sort"
	"strings"
)

// The command line.
//
// hms has four things to do and one setting, so the command line is written out
// rather than assembled from a flag library. The version before this one
// declared nine flags on a package-level FlagSet, parsed them BEFORE the
// subcommand, and then parsed a second set of flags out of the leftovers by
// hand -- with the result that `hms -out schema` set the export directory to
// "schema", left no subcommand at all, and opened the browser. Every flag was
// accepted everywhere and most of them meant nothing where they were written.
//
// So: a fixed set of verbs, one global setting, and anything else is an error
// that says what hms takes. A command line that quietly does something other
// than what was typed is worse than one that refuses.

// Verb is one thing hms does.
type Verb string

const (
	// VerbBrowse is the default: no arguments, open the house.
	VerbBrowse Verb = ""
	// VerbImport is the guided bulk import.
	VerbImport Verb = "import"
	// VerbVerify and VerbCheckpoint are the jobs a schedule runs.
	VerbVerify     Verb = "verify"
	VerbCheckpoint Verb = "checkpoint"
	// VerbInfo reports where the data is and what decided that.
	VerbInfo Verb = "info"
	// VerbHelp prints the usage.
	VerbHelp Verb = "help"
)

// verbs is every verb, with what it does and what it takes.
//
// One table, so usage text and the parser cannot disagree about what exists --
// the previous CLI documented `--render` in a doc comment and accepted
// `--format` on a command that ignored it.
type verbSpec struct {
	verb  Verb
	arg   string
	what  string
	takes bool
}

var verbs = []verbSpec{
	{VerbImport, "[NAME]", "walk a bulk import: a folder, a contract, your files, a plan to review", true},
	{VerbVerify, "", "check stored state against the ledger and report discrepancies", false},
	{VerbCheckpoint, "", "record a replay checkpoint for every holding", false},
	{VerbInfo, "", "print which database is open and what decided that", false},
	{VerbHelp, "", "print this", false},
}

// Invocation is a parsed command line.
type Invocation struct {
	Verb Verb
	// Arg is the verb's one positional argument, or "". Only import takes one,
	// and even there it is optional: an import with no name asks for one.
	Arg string
	// DBPath overrides the configured database. It is the only setting on the
	// command line, because it is the only one that has to be decided before
	// anything is read.
	DBPath string
}

// Parse reads the arguments after the program name.
func Parse(argv []string) (Invocation, error) {
	var inv Invocation
	var positional []string

	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if !strings.HasPrefix(arg, "-") {
			positional = append(positional, arg)
			continue
		}

		name, value, joined := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		switch name {
		case "db-path":
			if !joined {
				if i+1 >= len(argv) {
					return Invocation{}, fmt.Errorf("--db-path needs a file")
				}
				i++
				value = argv[i]
			}
			inv.DBPath = value
		case "h", "help":
			inv.Verb = VerbHelp
			return inv, nil
		default:
			return Invocation{}, fmt.Errorf("unknown option %s\n\n%s", arg, Usage())
		}
	}

	if len(positional) == 0 {
		return inv, nil
	}

	spec, ok := lookup(positional[0])
	if !ok {
		return Invocation{}, fmt.Errorf("unknown command %q\n\n%s", positional[0], Usage())
	}
	inv.Verb = spec.verb

	rest := positional[1:]
	switch {
	case len(rest) == 0:
	case !spec.takes:
		return Invocation{}, fmt.Errorf("hms %s takes no arguments, and was given %q", spec.verb, rest[0])
	case len(rest) > 1:
		return Invocation{}, fmt.Errorf("hms %s takes one argument, and was given %d", spec.verb, len(rest))
	default:
		inv.Arg = rest[0]
	}
	return inv, nil
}

func lookup(name string) (verbSpec, bool) {
	for _, spec := range verbs {
		if string(spec.verb) == name {
			return spec, true
		}
	}
	return verbSpec{}, false
}

// Usage is the whole of the command line, in the order somebody meets it.
func Usage() string {
	var b strings.Builder
	b.WriteString("hms -- a terminal inventory of a house\n\n")
	b.WriteString("  hms                     open the house\n")

	lines := make([]string, 0, len(verbs))
	for _, spec := range verbs {
		invocation := "hms " + string(spec.verb)
		if spec.arg != "" {
			invocation += " " + spec.arg
		}
		lines = append(lines, fmt.Sprintf("  %-22s  %s", invocation, spec.what))
	}
	sort.Strings(lines)
	b.WriteString(strings.Join(lines, "\n"))

	b.WriteString("\n\n  --db-path FILE          open this database instead of the configured one\n")
	b.WriteString("\nEverything hms keeps lives in ~/hms. $HMS_HOME moves it.\n")
	return b.String()
}
