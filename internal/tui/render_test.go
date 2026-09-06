package tui

import (
	"strings"
	"testing"

	"home-management-system/internal/tui/keys"
)

// The script format has to be able to express every key the interface uses,
// including the ones that collide with its own syntax. It could not express a
// bare `:` -- the command leader -- and the failure was silent enough to commit
// an empty frames file.
func TestAScriptCanExpressAColon(t *testing.T) {
	steps, err := parseScript(strings.NewReader(
		"4: the holdings table\n" +
			"/\n" +
			":\n" +
			"# a comment\n" +
			"\n" +
			"enter: apply\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(steps) != 4 {
		t.Fatalf("got %d steps, want 4", len(steps))
	}
	if got := steps[0].label; got != "the holdings table" {
		t.Errorf("label = %q", got)
	}
	if got := steps[1].key.String(); got != "/" {
		t.Errorf("step 2 = %q, want /", got)
	}
	if got := steps[2].key.String(); got != ":" {
		t.Errorf("step 3 = %q, want a colon", got)
	}
	if got := steps[3].key.String(); got != "enter" {
		t.Errorf("step 4 = %q", got)
	}
}

// TestEveryKeyTheInterfaceUsesIsExpressible walks the KEYMAP and checks the
// script format can say every key in it.
//
// It used to walk a list written out by hand here, which is the kind of list
// that is correct on the day it is written. Two keys collided with the format's
// own syntax -- `:` with the label separator and `#` with the comment marker --
// and both failed silently, producing a frame of something else. A format that
// cannot express the keys the interface uses is a format that cannot photograph
// it, and a hand-written list cannot notice a key that was added after it.
func TestEveryKeyTheInterfaceUsesIsExpressible(t *testing.T) {
	for _, ctx := range keys.Contexts() {
		for _, group := range keys.Bindings(ctx) {
			for _, key := range group {
				// Written the way a script writes it: a key whose name is a
				// literal space is a line the parser reads as blank.
				name := keys.Script(key)
				steps, err := parseScript(strings.NewReader(name + "\n"))
				if err != nil {
					t.Errorf("%q: %v", name, err)
					continue
				}
				if len(steps) != 1 {
					t.Errorf("%q parsed to %d steps, want 1 -- it was taken for syntax",
						name, len(steps))
					continue
				}
				// A key comes back under the name tea prints for it, which for
				// the readable aliases is not the name that was written: space
				// is " " and ctrl+space is ctrl+@. Same keystroke, one name for
				// scripts to read and one for tea to print.
				if got := steps[0].key.String(); got != keys.Canonical(name) {
					t.Errorf("%q parsed as %q", name, got)
				}
			}
		}
	}
}

// The punctuation the format has to survive, whether or not anything binds it
// today. These are the two that broke it, so they stay named.
func TestTheFormatSyntaxIsStillTypable(t *testing.T) {
	for _, key := range []string{"#", ":", "/", "*", ">", "?"} {
		steps, err := parseScript(strings.NewReader(key + "\n"))
		if err != nil || len(steps) != 1 || steps[0].key.String() != key {
			t.Errorf("%q is not expressible: %v", key, err)
		}
	}
}

// A note is still a note, and a bare hash is still a KEY.
//
// That last part is the trap, and it is worth stating as a test because it
// reads backwards: `#` counts, so a line holding nothing but a hash presses the
// count key. Anybody writing a script will reach for one as a blank separator
// between comment paragraphs -- every script this project had did exactly that,
// and had been doing it since `#` became a binding, opening the count prompt
// twice in each header against whatever the starting view had selected while
// the digits that followed went to the application as view switches.
//
// It failed silently, producing a frame of something else, which is the second
// time this format has failed that way after the colon. `##` is the blank
// comment line.
func TestCommentsAreStillComments(t *testing.T) {
	steps, err := parseScript(strings.NewReader(
		"# a note about the script\n" +
			"## a heading\n" +
			"4\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 {
		t.Errorf("got %d steps, want just the 4: %v", len(steps), steps)
	}

	steps, err = parseScript(strings.NewReader("#\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].key.String() != "#" {
		t.Errorf("a bare # parsed to %v, want the count key -- if this is ever "+
			"changed to a comment, `#` becomes a key no script can press", steps)
	}
}

func TestAnUnknownKeyIsRefusedWithItsLine(t *testing.T) {
	_, err := parseScript(strings.NewReader("4\nnotakey\n"))
	if err == nil {
		t.Fatal("an unknown key parsed")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("the error does not say where: %v", err)
	}
}

// The help line is cut to the terminal rather than allowed to wrap.
//
// A line wider than the screen wraps, and one wrapped line shifts every row
// below it -- the same reason the table drops columns instead of overflowing.
// The help line grew past 80 columns when the bindings became emacs (C-n/C-p
// says more than j/k did), and nothing was watching it: the harness's width
// checks never see it, because it only renders when there is no status to
// report and the seeded house always has a count to say.
func TestTheHelpLineFitsTheTerminal(t *testing.T) {
	parts := []string{
		keys.Hint(keys.Table,
			[]keys.Action{keys.MoveDown, keys.MoveUp},
			[]keys.Action{keys.MoveLeft, keys.MoveRight}),
		"1-5 views",
		keys.Hint(keys.Browse, []keys.Action{keys.Confirm}),
		keys.Hint(keys.Table, []keys.Action{keys.ToggleSelect}, []keys.Action{keys.Sort}),
		keys.Hint(keys.Browse, []keys.Action{keys.Quit}),
	}
	for _, width := range []int{40, 60, 80, 100, 120} {
		got := fit(width, parts)
		if n := len([]rune(got)); n > width {
			t.Errorf("the help line is %d columns in a %d-column terminal: %q", n, width, got)
		}
		if got == "" {
			t.Errorf("the help line said nothing at all in %d columns", width)
		}
	}
}
