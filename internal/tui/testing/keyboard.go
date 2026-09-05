// Package testing is the end-to-end harness for the interface.
//
// Keys go in the front; assertions run against the DATABASE on the far side.
// That is the property worth having, and it is stronger than anything asserting
// on Model fields: the input and the assertion are on opposite sides of the
// whole stack, so a test cannot pass while the wiring between them is wrong --
// which is precisely where TUI bugs live.
//
// Ported from the legacy system, which had solved most of this, with four
// changes. Each is documented where it applies: the database (db.go), the
// BatchMsg expansion (simulator.go), the two helpers deliberately NOT ported
// (simulator.go), and seeding through ops rather than keystrokes (db.go).
package testing

import (
	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/tui/keys"
)

// Key is one keyboard event.
type Key struct{ msg tea.KeyMsg }

// named is the harness's way of saying a key, and it goes through the keymap
// rather than keeping a list beside it.
//
// The list beside it is what drifted: `ctrl+n` sat in this file for a release
// while nothing bound it, and keys the interface did bind could not be pressed
// from a test at all.
func named(name string) Key {
	msg, ok := keys.Named(name)
	if !ok {
		panic("tui/testing: " + name + " is not a key")
	}
	return Key{msg}
}

// The keys with no printable form.
var (
	Up        = named("up")
	Down      = named("down")
	Left      = named("left")
	Right     = named("right")
	Enter     = named("enter")
	Esc       = named("esc")
	Tab       = named("tab")
	ShiftTab  = named("shift+tab")
	Backspace = named("backspace")
	Delete    = named("delete")
	Space     = named("space")
	PgUp      = named("pgup")
	PgDown    = named("pgdown")
	Home      = named("home")
	End       = named("end")

	CtrlSpace = named("ctrl+space")
	CtrlA     = named("ctrl+a")
	CtrlB     = named("ctrl+b")
	CtrlC     = named("ctrl+c")
	CtrlD     = named("ctrl+d")
	CtrlE     = named("ctrl+e")
	CtrlF     = named("ctrl+f")
	CtrlG     = named("ctrl+g")
	CtrlK     = named("ctrl+k")
	CtrlL     = named("ctrl+l")
	CtrlN     = named("ctrl+n")
	CtrlP     = named("ctrl+p")
	CtrlS     = named("ctrl+s")
	CtrlU     = named("ctrl+u")
	CtrlV     = named("ctrl+v")
	CtrlW     = named("ctrl+w")
	CtrlY     = named("ctrl+y")

	AltB     = named("alt+b")
	AltN     = named("alt+n")
	AltP     = named("alt+p")
	AltD     = named("alt+d")
	AltF     = named("alt+f")
	AltG     = named("alt+g")
	AltH     = named("alt+h")
	AltV     = named("alt+v")
	AltW     = named("alt+w")
	AltX     = named("alt+x")
	AltLess  = named("alt+<")
	AltGreat = named("alt+>")
)

// Press is one printable keystroke, named for what a person does.
//
// It takes a string rather than a rune so that a key script reads as the keys
// it presses: Press("g"), Press("h").
func Press(s string) Key {
	return Key{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}}
}

// Type is a run of printable keystrokes, one event each, exactly as a person
// typing into a field would produce.
//
// A space is sent as KeySpace, not as a rune. Bubbletea distinguishes them and
// a real terminal only ever sends the former -- so a harness that sent runes
// for spaces was testing a keystroke nobody can produce. It hid a bug where
// typing a space into the command line did nothing, and every test was green
// while `:consume 100g` arrived as "consume100g".
func Type(s string) []Key {
	keys := make([]Key, 0, len(s))
	for _, r := range s {
		if r == ' ' {
			keys = append(keys, Space)
			continue
		}
		keys = append(keys, Key{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}})
	}
	return keys
}

// Keys flattens a mixed script into a single sequence, so a test can write
//
//	sim.Send(Press("o"), Type("Kitchen"), Enter)
//
// without wrapping every group.
func Keys(script ...any) []Key {
	var out []Key
	for _, item := range script {
		switch v := item.(type) {
		case Key:
			out = append(out, v)
		case []Key:
			out = append(out, v...)
		default:
			panic("tui/testing: a key script holds Key and []Key, not other things")
		}
	}
	return out
}

// String renders a key the way a script would write it, for failure messages.
func (k Key) String() string { return k.msg.String() }
