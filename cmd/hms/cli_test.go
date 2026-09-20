package main

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		name string
		argv []string
		want Invocation
	}{
		{"nothing is browse", nil, Invocation{Verb: VerbBrowse}},
		{"a verb", []string{"import"}, Invocation{Verb: VerbImport}},
		{"a verb with a name", []string{"import", "groceries"}, Invocation{Verb: VerbImport, Arg: "groceries"}},
		{"verify", []string{"verify"}, Invocation{Verb: VerbVerify}},
		{"checkpoint", []string{"checkpoint"}, Invocation{Verb: VerbCheckpoint}},
		{"info", []string{"info"}, Invocation{Verb: VerbInfo}},

		// The setting, written either way and on either side of the verb,
		// because all three are how somebody writes it.
		{"a setting, spaced", []string{"--db-path", "/tmp/a.db"}, Invocation{DBPath: "/tmp/a.db"}},
		{"a setting, joined", []string{"--db-path=/tmp/a.db"}, Invocation{DBPath: "/tmp/a.db"}},
		{"one dash", []string{"-db-path", "/tmp/a.db"}, Invocation{DBPath: "/tmp/a.db"}},
		{
			"a setting after the verb",
			[]string{"import", "groceries", "--db-path", "/tmp/a.db"},
			Invocation{Verb: VerbImport, Arg: "groceries", DBPath: "/tmp/a.db"},
		},
		{
			"a setting before the verb",
			[]string{"--db-path", "/tmp/a.db", "import"},
			Invocation{Verb: VerbImport, DBPath: "/tmp/a.db"},
		},

		{"help", []string{"--help"}, Invocation{Verb: VerbHelp}},
		{"help, short", []string{"-h"}, Invocation{Verb: VerbHelp}},
		{"help, as a verb", []string{"help"}, Invocation{Verb: VerbHelp}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.argv)
			if err != nil {
				t.Fatalf("Parse(%v): %v", tc.argv, err)
			}
			if got != tc.want {
				t.Errorf("Parse(%v) = %+v, want %+v", tc.argv, got, tc.want)
			}
		})
	}
}

// The bug this command line was rewritten for. `hms -out schema` set an export
// directory to "schema", left no subcommand at all, and opened the browser --
// a command line doing something other than what was typed, with no sign that
// anything had gone wrong. Every rejection below is one of those.
func TestParseRefusesWhatItCannotDo(t *testing.T) {
	for _, tc := range []struct {
		name string
		argv []string
		says string
	}{
		{"the flag that opened the browser", []string{"-out", "schema"}, "unknown option"},
		{"a flag that moved to hmsdev", []string{"--render", "keys.txt"}, "unknown option"},
		{"a flag that moved to hmsdev", []string{"--format", "json"}, "unknown option"},
		{"a flag that is now a verb", []string{"--verify"}, "unknown option"},
		{"a verb that does not exist", []string{"improt"}, "unknown command"},
		{"a file where a name goes", []string{"verify", "plan.csv"}, "takes no arguments"},
		{"two names", []string{"import", "a", "b"}, "takes one argument"},
		{"a setting with nothing after it", []string{"--db-path"}, "needs a file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.argv)
			if err == nil {
				t.Fatalf("Parse(%v) was accepted", tc.argv)
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Errorf("Parse(%v) = %q, want it to mention %q", tc.argv, err, tc.says)
			}
		})
	}
}

// A refusal that does not say what hms takes is a refusal somebody has to go
// and look something up after.
func TestUnknownOptionPrintsTheUsage(t *testing.T) {
	_, err := Parse([]string{"-out", "schema"})
	if err == nil {
		t.Fatal("accepted")
	}
	for _, verb := range []string{"hms import", "hms verify", "--db-path"} {
		if !strings.Contains(err.Error(), verb) {
			t.Errorf("the refusal does not mention %q:\n%s", verb, err)
		}
	}
}

// Usage is generated from the verb table, so a verb that exists cannot be
// missing from the help and a verb in the help cannot be one that does not.
func TestUsageCoversEveryVerb(t *testing.T) {
	usage := Usage()
	for _, spec := range verbs {
		if !strings.Contains(usage, "hms "+string(spec.verb)) {
			t.Errorf("usage does not mention %q", spec.verb)
		}
	}
}
