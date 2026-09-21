// Package style is the interface's vocabulary of emphasis.
//
// Styles are named for what they MEAN rather than for what they do. Before
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

// The palette: four hues, and every coloured thing in the interface is one of
// them.
//
// Declared apart from the styles because two packages need the colours and
// only one needs the styles -- a table row says "this needs an answer" with a
// foreground, where prose says it with a bold word, and the two have to be the
// same amber or the screen has two ambers meaning one thing.
//
// Adaptive, so one set works on a light terminal and a dark one. Written as
// hex rather than as palette indices: lipgloss degrades hex to the nearest
// available colour, and "#9C6503" says what it is where "94" says where it
// sits in somebody else's table.
var (
	// Accent is where the keyboard is. Nothing else uses it, which is what
	// makes it findable at a glance on a screen with two panes.
	Accent = lipgloss.AdaptiveColor{Light: "#2E5C8A", Dark: "#8FC0E8"}

	// Good is settled: a row that will apply, a count that reconciled.
	Good = lipgloss.AdaptiveColor{Light: "#3D7548", Dark: "#7FB585"}

	// Attention is "not yet" -- something that needs an answer before it can
	// proceed, or a fact that has been true for too long.
	Attention = lipgloss.AdaptiveColor{Light: "#9C6503", Dark: "#D6A342"}

	// Stop is "no": a refusal, a blocked row, a thing that is over.
	Stop = lipgloss.AdaptiveColor{Light: "#A03826", Dark: "#E08573"}
)

var (
	// Dim is everything that supports the thing being read rather than being
	// it: hints, labels, frames, column headers, and rows that have been ruled
	// out. Most of this interface is deliberately quiet, which is exactly what
	// makes one loud thing readable.
	Dim = lipgloss.NewStyle().Faint(true)

	// Strong is the thing itself -- a value, a title, a heading, the option
	// under the highlight.
	Strong = lipgloss.NewStyle().Bold(true)

	// Focus is where a keystroke will land: the pane that has the cursor, and
	// the mode the interface is in.
	//
	// It earns a name because Strong was doing this job as well as its own.
	// Two meanings with one appearance is what this package exists to catch,
	// and it started mattering the moment there were two panes and only one of
	// them was listening.
	Focus = lipgloss.NewStyle().Bold(true).Foreground(Accent)

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

	// Error is a refusal, and has to be distinguishable from a confirmation at
	// a glance.
	Error = lipgloss.NewStyle().Bold(true).Foreground(Stop)

	// Warn is a row that needs an answer before it can proceed -- amber
	// against Error's red, because "not yet" is not "no".
	Warn = lipgloss.NewStyle().Bold(true).Foreground(Attention)

	// Ready is the opposite of both: this will happen, and nothing is being
	// asked of you.
	Ready = lipgloss.NewStyle().Foreground(Good)
)
