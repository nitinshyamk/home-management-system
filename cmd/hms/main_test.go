package main

import (
	"reflect"
	"testing"
)

// Flags written AFTER the positional argument, because `hms import plan.csv
// --dry-run` and `hms schema --format json` are how a person writes them and
// Go's flag package stops at the first non-flag.
//
// The value-taking case is the one that bit: `--format json` looks exactly like
// a bare flag followed by a stray word, and the difference cannot be read off
// the text. An acceptance check that only looked at the exit code reported it
// working while it emitted the wrong format entirely.
func TestTrailingFlags(t *testing.T) {
	for _, tc := range []struct {
		name       string
		args       []string
		positional []string
		flags      map[string]string
	}{
		{
			"a bare boolean",
			[]string{"import", "a.csv", "--dry-run"},
			[]string{"import", "a.csv"},
			map[string]string{"dry-run": ""},
		},
		{
			"a value, spaced",
			[]string{"schema", "--format", "json"},
			[]string{"schema"},
			map[string]string{"format": "json"},
		},
		{
			"a value, joined",
			[]string{"schema", "--format=json"},
			[]string{"schema"},
			map[string]string{"format": "json"},
		},
		{
			"one dash",
			[]string{"schema", "-format", "json"},
			[]string{"schema"},
			map[string]string{"format": "json"},
		},
		{
			"both kinds",
			[]string{"import", "a.csv", "--dry-run", "--format", "json"},
			[]string{"import", "a.csv"},
			map[string]string{"dry-run": "", "format": "json"},
		},
		{
			"nothing trailing",
			[]string{"import", "a.csv"},
			[]string{"import", "a.csv"},
			map[string]string{},
		},
		// A value-taking flag with nothing after it must not eat a positional
		// that is not there.
		{
			"a dangling value flag",
			[]string{"schema", "--format"},
			[]string{"schema"},
			map[string]string{"format": ""},
		},
		// The directory the schema is exported into takes a value too, and it
		// is written after the positional for the same reason everything else
		// is: `hms schema --out ./handoff` is how a person writes it.
		{
			"a directory to export into",
			[]string{"schema", "--out", "./handoff"},
			[]string{"schema"},
			map[string]string{"out": "./handoff"},
		},
		{
			"a directory as a positional",
			[]string{"schema", "./handoff"},
			[]string{"schema", "./handoff"},
			map[string]string{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			positional, flags := splitTrailingFlags(tc.args)
			if !reflect.DeepEqual(positional, tc.positional) {
				t.Errorf("positional = %v, want %v", positional, tc.positional)
			}
			if !reflect.DeepEqual(flags, tc.flags) {
				t.Errorf("flags = %v, want %v", flags, tc.flags)
			}
		})
	}
}
