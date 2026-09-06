// Package style is the interface's vocabulary of emphasis.
//
// Seven styles, named for what they MEAN rather than for what they do. Before
// this package there were thirty declarations across seven files, and the
// names had drifted apart from each other while the definitions had not:
// Faint(true) was hintStyle in three places, labelStyle in two, and frameStyle,
// dimStyle, unchosenStyle, droppedStyle, emptyStyle and headerStyle once each.
// Nine names, one appearance, and no way to restyle the interface without
// finding all nine.
//
// The rule for adding one: a new style earns a name when it means something no
// existing name means. A style that merely looks different from its neighbour
// by accident is the thing this package exists to prevent.
package style

import "github.com/charmbracelet/lipgloss"

var (
	// Dim is everything that supports the thing being read rather than being
	// it: hints, labels, frames, column headers, and rows that have been ruled
	// out. Most of this interface is deliberately quiet, which is exactly what
	// makes one loud thing readable.
	Dim = lipgloss.NewStyle().Faint(true)

	// Strong is the thing itself -- a value, a title, a heading, the option
	// under the highlight.
	Strong = lipgloss.NewStyle().Bold(true)

	// Cursor is a block where the next character goes, so a field looks like
	// somewhere text goes.
	Cursor = lipgloss.NewStyle().Reverse(true)

	// Highlight is a whole row or badge picked out, rather than a character:
	// the line the cursor is on in a report, the jump palette's badge.
	Highlight = lipgloss.NewStyle().Bold(true).Reverse(true)

	// Aside is shown-but-not-offered: existing names a new one might collide
	// with, which are a fact rather than a suggestion and must not read as
	// something takeable.
	Aside = lipgloss.NewStyle().Faint(true).Italic(true)

	// Error is a refusal. Red and bold, because it has to be distinguishable
	// from a confirmation at a glance.
	Error = lipgloss.NewStyle().Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "160", Dark: "203"})

	// Warn is a row that needs an answer before it can proceed -- amber
	// against Error's red, because "not yet" is not "no".
	Warn = lipgloss.NewStyle().Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "94", Dark: "179"})
)
