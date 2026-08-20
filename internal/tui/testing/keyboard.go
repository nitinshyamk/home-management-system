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
)

// Key is one keyboard event.
type Key struct{ msg tea.KeyMsg }

// The keys with no printable form.
var (
	Up        = Key{tea.KeyMsg{Type: tea.KeyUp}}
	Down      = Key{tea.KeyMsg{Type: tea.KeyDown}}
	Left      = Key{tea.KeyMsg{Type: tea.KeyLeft}}
	Right     = Key{tea.KeyMsg{Type: tea.KeyRight}}
	Enter     = Key{tea.KeyMsg{Type: tea.KeyEnter}}
	Esc       = Key{tea.KeyMsg{Type: tea.KeyEscape}}
	Tab       = Key{tea.KeyMsg{Type: tea.KeyTab}}
	ShiftTab  = Key{tea.KeyMsg{Type: tea.KeyShiftTab}}
	Backspace = Key{tea.KeyMsg{Type: tea.KeyBackspace}}
	Delete    = Key{tea.KeyMsg{Type: tea.KeyDelete}}
	Space     = Key{tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}}
	CtrlA     = Key{tea.KeyMsg{Type: tea.KeyCtrlA}}
	CtrlC     = Key{tea.KeyMsg{Type: tea.KeyCtrlC}}
	CtrlD     = Key{tea.KeyMsg{Type: tea.KeyCtrlD}}
	CtrlU     = Key{tea.KeyMsg{Type: tea.KeyCtrlU}}
	CtrlP     = Key{tea.KeyMsg{Type: tea.KeyCtrlP}}
	CtrlN     = Key{tea.KeyMsg{Type: tea.KeyCtrlN}}
	CtrlF     = Key{tea.KeyMsg{Type: tea.KeyCtrlF}}
	CtrlB     = Key{tea.KeyMsg{Type: tea.KeyCtrlB}}
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
