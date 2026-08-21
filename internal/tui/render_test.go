package tui

import (
	"strings"
	"testing"
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

// TestEveryKeyTheInterfaceUsesIsExpressible walks the keys the plan binds and
// checks the script format can say each one.
//
// Two of them collided with the format's own syntax -- `:` with the label
// separator and `#` with the comment marker -- and both failed silently,
// producing a frame of something else. A format that cannot express the keys
// the interface uses is a format that cannot photograph it.
func TestEveryKeyTheInterfaceUsesIsExpressible(t *testing.T) {
	keys := []string{
		"j", "k", "h", "l", "g", "G", "z", "a", "R", "M", "n", "N", "s", "v", "V",
		"o", "O", "e", "E", "c", "m", "t", "d", "y", "p", "r", "q",
		"1", "2", "3", "4", "5",
		"/", ":", "#", "*", ">", "?",
		"enter", "esc", "tab", "space", "up", "down", "ctrl+d", "ctrl+u", "ctrl+p",
	}
	for _, key := range keys {
		steps, err := parseScript(strings.NewReader(key + "\n"))
		if err != nil {
			t.Errorf("%q: %v", key, err)
			continue
		}
		if len(steps) != 1 {
			t.Errorf("%q parsed to %d steps, want 1 -- it was taken for syntax", key, len(steps))
			continue
		}
		// tea renders a space keystroke as " ", which is the same key by a
		// different name rather than a different key.
		want := key
		if key == "space" {
			want = " "
		}
		if got := steps[0].key.String(); got != want {
			t.Errorf("%q parsed as %q", key, got)
		}
	}
}

// And a note is still a note.
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
