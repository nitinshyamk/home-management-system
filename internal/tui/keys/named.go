package keys

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Named turns a written key name into the keystroke it names.
//
// It is the inverse of tea.KeyMsg.String(), and it exists because two things
// outside the application have to be able to SAY a key: the `--render` scripts
// that photograph the review screens, and the end-to-end harness. Both used to
// keep their own list, and a key added to the interface but not to their lists
// failed quietly -- the script produced a frame of something else, and the
// harness could not press the key at all.
//
// Built by round-tripping rather than by hand: every control key is generated
// and then filed under the name tea itself gives it, so the two can only agree.
var named = func() map[string]tea.KeyMsg {
	out := map[string]tea.KeyMsg{}

	// The control keys, named by tea. This also files ctrl+i as "tab" and
	// ctrl+m as "enter", which is not a special case but the truth about a
	// terminal: they are the same byte.
	for c := byte('a'); c <= 'z'; c++ {
		msg := tea.KeyMsg{Type: tea.KeyType(1 + c - 'a')}
		out[msg.String()] = msg
	}

	for _, msg := range []tea.KeyMsg{
		{Type: tea.KeyCtrlAt},
		{Type: tea.KeyEnter},
		{Type: tea.KeyEscape},
		{Type: tea.KeyTab},
		{Type: tea.KeyShiftTab},
		{Type: tea.KeyBackspace},
		{Type: tea.KeyDelete},
		{Type: tea.KeySpace, Runes: []rune{' '}},
		{Type: tea.KeyUp},
		{Type: tea.KeyDown},
		{Type: tea.KeyLeft},
		{Type: tea.KeyRight},
		{Type: tea.KeyHome},
		{Type: tea.KeyEnd},
		{Type: tea.KeyPgUp},
		{Type: tea.KeyPgDown},
	} {
		out[msg.String()] = msg
	}

	// Two names a script can write that tea would never print. `ctrl+@` is what
	// a terminal calls C-space, and a review script that has to say `ctrl+@` to
	// mean "select this row" is a script nobody can read.
	out["ctrl+space"] = tea.KeyMsg{Type: tea.KeyCtrlAt}
	out["space"] = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}

	return out
}()

// Named resolves a key name. Meta keys and single characters are computed
// rather than listed, so `alt+<` and `#` need no entry.
func Named(name string) (tea.KeyMsg, bool) {
	if msg, ok := named[name]; ok {
		return msg, true
	}
	if rest, ok := strings.CutPrefix(name, "alt+"); ok {
		if r := []rune(rest); len(r) == 1 {
			return tea.KeyMsg{Type: tea.KeyRunes, Runes: r, Alt: true}, true
		}
		return tea.KeyMsg{}, false
	}
	if r := []rune(name); len(r) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: r}, true
	}
	return tea.KeyMsg{}, false
}

// Script is the name a review script writes for a key.
//
// The inverse of the two readable aliases, and it is not cosmetic: a script is
// a line-oriented file, so a key whose name is a literal space is a line the
// parser reads as blank and skips. The interface binds one -- space selects a
// row -- so without a name it can be written by, the highest-traffic key on the
// table could not appear in a frame at all.
func Script(key string) string {
	switch key {
	case " ":
		return "space"
	case "ctrl+@":
		return "ctrl+space"
	}
	return key
}

// Canonical is the name tea will print for a key that Named accepted, which is
// not always the name that was written: `space` comes back as " " and
// `ctrl+space` as `ctrl+@`. A round-trip check has to compare against this
// rather than against the input, or the readable aliases look like bugs.
func Canonical(name string) string {
	msg, ok := Named(name)
	if !ok {
		return name
	}
	return msg.String()
}
