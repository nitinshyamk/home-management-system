// Package line is the one text field.
//
// Not a widget -- a pair of functions over a value and a cursor. Three places
// in this interface hold a line of text (the inline editor, the input line, and
// every field of the creation panel) and before this package two of them held
// it badly: no cursor at all, append and backspace only, so correcting one word
// of a long command meant backspacing over everything after it.
//
// Written as functions rather than as a type because the three owners already
// have their own models with their own reasons, and a fourth model wrapped
// inside each of them would be a layer that carries nothing. What they actually
// share is the ANSWER to "what does this keystroke do to this text", which is
// exactly what a function is for.
package line

import (
	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/style"
)

// Edit applies a keystroke to a line, reporting whether it was consumed.
//
// The keys are emacs, which here means readline: C-a and C-e for the ends, C-b
// and C-f by character, M-b and M-f by word, C-d forward, C-u and C-k to the
// two ends, C-w and M-d by word.
//
// It does NOT handle enter, esc or tab. Those are the owner's: what accepting a
// line means depends entirely on what the line is for, and a field that decided
// for itself would be deciding what the whole gesture does.
func Edit(value string, cursor int, msg tea.KeyMsg) (string, int, bool) {
	r := []rune(value)
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(r) {
		cursor = len(r)
	}

	// Text first, and the Alt test is why this is not just a type switch: tea
	// reports M-w as runes with Alt set, so a field testing the type alone
	// typed a literal "w" every time somebody reached for a Meta binding.
	if keys.IsText(msg) {
		text := string(msg.Runes)
		if msg.Type == tea.KeySpace {
			text = " "
		}
		return string(r[:cursor]) + text + string(r[cursor:]), cursor + len([]rune(text)), true
	}

	switch keys.Lookup(keys.Line, msg) {
	case keys.DeleteBack:
		if cursor > 0 {
			return string(r[:cursor-1]) + string(r[cursor:]), cursor - 1, true
		}
		return value, cursor, true
	case keys.DeleteForward:
		if cursor < len(r) {
			return string(r[:cursor]) + string(r[cursor+1:]), cursor, true
		}
		return value, cursor, true
	case keys.MoveLeft:
		if cursor > 0 {
			cursor--
		}
		return value, cursor, true
	case keys.MoveRight:
		if cursor < len(r) {
			cursor++
		}
		return value, cursor, true
	case keys.WordLeft:
		return value, wordStart(r, cursor), true
	case keys.WordRight:
		return value, wordEnd(r, cursor), true
	case keys.LineStart:
		return value, 0, true
	case keys.LineEnd:
		return value, len(r), true
	case keys.KillToStart:
		// To the start of the line, as readline has it. With the cursor at the
		// end -- where it is unless someone moved it -- that clears the field,
		// which is what it has always done here.
		return string(r[cursor:]), 0, true
	case keys.KillToEnd:
		return string(r[:cursor]), cursor, true
	case keys.KillWordBack:
		// The word before the cursor, which on a command line is usually the
		// token that is wrong.
		start := wordStart(r, cursor)
		return string(r[:start]) + string(r[cursor:]), start, true
	case keys.KillWordForward:
		end := wordEnd(r, cursor)
		return string(r[:cursor]) + string(r[end:]), cursor, true
	}
	return value, cursor, false
}

// wordStart and wordEnd are one word away from here.
//
// The run of spaces is skipped FIRST, so M-b from just after a word lands at
// the start of that word rather than in the gap before it -- which is where a
// version that stopped at the first space put it, and made the key useless for
// the thing it exists for: reaching back over one wrong token.
func wordStart(r []rune, from int) int {
	at := from
	for at > 0 && r[at-1] == ' ' {
		at--
	}
	for at > 0 && r[at-1] != ' ' {
		at--
	}
	return at
}

func wordEnd(r []rune, from int) int {
	at := from
	for at < len(r) && r[at] == ' ' {
		at++
	}
	for at < len(r) && r[at] != ' ' {
		at++
	}
	return at
}

// Render draws a line with the block cursor sitting IN it rather than always
// after it, so a cursor that has been moved is visible where it is.
//
// The drawing half of what Edit is the editing half. It lived three times --
// once in the input line, once in the inline field, once per field of the
// creation panel -- as twelve identical lines under three names, which is one
// per owner of a cursor and exactly the count this package exists to reduce.
func Render(value string, cursor int) string {
	r := []rune(value)
	at := cursor
	if at < 0 {
		at = 0
	}
	if at > len(r) {
		at = len(r)
	}
	// Past the end there is no character to reverse, so the cursor is drawn on
	// the space where the next one will go.
	if at == len(r) {
		return style.Strong.Render(value) + style.Cursor.Render(" ")
	}
	return style.Strong.Render(string(r[:at])) +
		style.Cursor.Render(string(r[at])) +
		style.Strong.Render(string(r[at+1:]))
}
