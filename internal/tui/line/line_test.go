package line_test

import (
	"testing"

	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/line"
)

// edit presses a sequence of named keys against a line, returning what is left
// and where the cursor is.
func edit(value string, cursor int, names ...string) (string, int) {
	for _, name := range names {
		msg, ok := keys.Named(name)
		if !ok {
			panic(name + " is not a key")
		}
		next, at, handled := line.Edit(value, cursor, msg)
		if !handled {
			panic(name + " was not consumed")
		}
		value, cursor = next, at
	}
	return value, cursor
}

func TestTypingInsertsAtTheCursor(t *testing.T) {
	// The case a field with no cursor could not do at all: correcting a word in
	// the middle without retyping everything after it.
	got, at := edit("consume rice", 0, "ctrl+e", "alt+b")
	if at != 8 {
		t.Errorf("M-b from the end left the cursor at %d, want 8", at)
	}
	got, at = edit(got, at, "b", "i", "g", " ")
	if got != "consume big rice" {
		t.Errorf("typing mid-line produced %q", got)
	}
	if at != 12 {
		t.Errorf("cursor at %d after inserting four characters, want 12", at)
	}
}

// M-b skips the spaces BEFORE the word, so from just after a word it lands at
// the start of that word rather than in the gap in front of it. A version that
// stopped at the first space made the key useless for the thing it exists for.
func TestWordMotionSkipsTheGapFirst(t *testing.T) {
	if _, at := edit("at Shelf 1", 8, "alt+b"); at != 3 {
		t.Errorf("M-b from the end of \"Shelf\" landed at %d, want 3", at)
	}
	if _, at := edit("at Shelf 1", 2, "alt+f"); at != 8 {
		t.Errorf("M-f landed at %d, want 8 -- the end of the next word", at)
	}
}

func TestTheKillsTakeTheRightHalf(t *testing.T) {
	for _, c := range []struct {
		name         string
		key          string
		cursor       int
		want         string
		wantCursorAt int
	}{
		{"C-u takes what is behind", "ctrl+u", 8, "rice", 0},
		{"C-k takes what is ahead", "ctrl+k", 8, "consume ", 8},
		{"C-w takes the word behind", "ctrl+w", 12, "consume ", 8},
		{"M-d takes the word ahead", "alt+d", 8, "consume ", 8},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, at := edit("consume rice", c.cursor, c.key)
			if got != c.want {
				t.Errorf("%s left %q, want %q", c.key, got, c.want)
			}
			if at != c.wantCursorAt {
				t.Errorf("%s left the cursor at %d, want %d", c.key, at, c.wantCursorAt)
			}
		})
	}
}

// C-u with the cursor at the end clears the field, which is what it has always
// done here -- the panel's parent fields arrive pre-filled and need a way out
// that is not fifteen backspaces.
func TestClearingAPrefilledField(t *testing.T) {
	if got, _ := edit("Garage", 6, "ctrl+u"); got != "" {
		t.Errorf("C-u at the end left %q", got)
	}
}

// A Meta keystroke is not text. tea reports M-w as runes with Alt set, so a
// field that switched on the type alone typed a literal "w" every time somebody
// reached for a Meta binding with a field open.
func TestMetaIsNotTypedIntoTheLine(t *testing.T) {
	msg, _ := keys.Named("alt+w")
	got, _, handled := line.Edit("rice", 4, msg)
	if handled {
		t.Errorf("M-w was consumed by the line, leaving %q", got)
	}
	if got != "rice" {
		t.Errorf("M-w changed the line to %q", got)
	}
}

// A cursor outside the text is clamped rather than panicking. The owners set it
// themselves -- the creator puts it at the end of a pre-filled value -- so it
// can arrive stale after the value is replaced underneath it.
func TestAnOutOfRangeCursorIsClamped(t *testing.T) {
	if got, at := edit("ab", 99, "backspace"); got != "a" || at != 1 {
		t.Errorf("backspace past the end gave %q at %d, want \"a\" at 1", got, at)
	}
	if got, at := edit("ab", -5, "ctrl+d"); got != "b" || at != 0 {
		t.Errorf("C-d before the start gave %q at %d, want \"b\" at 0", got, at)
	}
}
