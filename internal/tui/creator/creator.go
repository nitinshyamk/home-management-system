// Package creator is the one creation panel.
//
// One panel for Item, Category, and Location, and the SAME panel the import
// plan screen opens for a receipt row that would create something. That sharing
// is not an optimisation: it is what keeps the interactive and bulk flows from
// drifting into different products. If the plan screen ever needs this
// reworked, the two have already diverged.
//
// It builds a `:` line rather than a Command. Everything the interface writes
// goes through Parse and Bind, so a panel cannot become a second way to create
// things that validates differently from the first.
package creator

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Kind is what the panel is making.
type Kind string

const (
	KindItem     Kind = "item"
	KindCategory Kind = "category"
	KindLocation Kind = "location"
)

// Counting is the Item variant choice: three presets, never two booleans.
// Nobody reads the word "fungible".
type Counting struct {
	Value string
	Label string
}

var countings = []Counting{
	{"unique", "one of a kind"},
	{"pile", "a pile I count"},
	{"measured", "something I measure"},
}

// field is one input in the panel.
type field struct {
	key   string
	label string
	value string
	// hint is what the field is for, shown while it is focused. A label alone
	// says what to type; the hint says why.
	hint string
	// measured marks the fields that exist only for a measured or counted Item.
	// They appear BECAUSE of the counting choice -- a unit box that is always
	// visible and sometimes meaningless is the nullable-column muddle this
	// project spent months eliminating, drawn on a screen.
	measured bool
}

// Model is the panel.
type Model struct {
	open     bool
	kind     Kind
	fields   []field
	focus    int
	counting int
	width    int
	// parent is the thing the cursor was on when the panel opened, offered as
	// the default parent or category. Creating inside what you are looking at
	// is what o means.
	parent string
}

var (
	frameStyle   = lipgloss.NewStyle().Faint(true)
	labelStyle   = lipgloss.NewStyle().Faint(true)
	valueStyle   = lipgloss.NewStyle().Bold(true)
	focusStyle   = lipgloss.NewStyle().Reverse(true)
	hintStyle    = lipgloss.NewStyle().Faint(true)
	chosenStyle  = lipgloss.NewStyle().Bold(true)
	unchosenStyl = lipgloss.NewStyle().Faint(true)
)

func New() Model { return Model{width: 80} }

func (m Model) SetWidth(w int) Model { m.width = w; return m }

// Open starts a fresh panel.
//
// Fresh every time: a panel that remembered an abandoned attempt will
// eventually create it.
func (m Model) Open(kind Kind, parent string) Model {
	m = Model{open: true, kind: kind, width: m.width, parent: parent, counting: 2}
	switch kind {
	case KindItem:
		m.fields = []field{
			{key: "name", label: "name", hint: "what it is called"},
			{key: "counting", label: "counting", hint: "how it is counted -- PERMANENT"},
			{key: "unit", label: "unit", hint: "what it is measured in -- PERMANENT", measured: true},
			{key: "package", label: "per package", hint: "how much is in one package", measured: true},
			{key: "category", label: "category", value: parent, hint: "where to file it"},
		}
	case KindCategory:
		m.fields = []field{
			{key: "name", label: "name", hint: "what it is called"},
			{key: "under", label: "under", value: parent, hint: "the classification it belongs to"},
			{key: "describe", label: "describe", hint: "what belongs in it"},
		}
	case KindLocation:
		m.fields = []field{
			{key: "name", label: "name", hint: "what it is called"},
			{key: "under", label: "under", value: parent, hint: "the place it is inside"},
			{key: "describe", label: "describe", hint: "what it is"},
		}
	}
	return m
}

func (m Model) IsOpen() bool { return m.open }
func (m Model) Kind() Kind   { return m.kind }
func (m Model) Close() Model { m.open = false; return m }

// Value returns a field's contents.
func (m Model) Value(key string) string {
	for _, f := range m.fields {
		if f.key == key {
			return strings.TrimSpace(f.value)
		}
	}
	return ""
}

// Counting is the chosen preset.
func (m Model) Counting() string { return countings[m.counting].Value }

// measures reports whether the chosen preset needs a unit at all.
func (m Model) measures() bool { return m.Counting() != "unique" }

// visible is the fields the current choice actually needs.
func (m Model) visible() []int {
	var out []int
	for i, f := range m.fields {
		if f.measured && !m.measures() {
			continue
		}
		out = append(out, i)
	}
	return out
}

// Line renders the panel as the `:` command it stands for.
//
// A command rather than a Command: everything the interface writes goes through
// Parse and Bind, so the panel cannot validate differently from the typed line
// or from a CSV row. It is the same contract with a nicer way to fill it in.
func (m Model) Line() string {
	var b strings.Builder
	fmt.Fprintf(&b, "new %s %s", m.kind, quote(m.Value("name")))
	if m.kind == KindItem {
		fmt.Fprintf(&b, " counting %s", m.Counting())
	}
	for _, i := range m.visible() {
		f := m.fields[i]
		if f.key == "name" || f.key == "counting" {
			continue
		}
		if value := strings.TrimSpace(f.value); value != "" {
			fmt.Fprintf(&b, " %s %s", f.key, quote(value))
		}
	}
	return b.String()
}

func quote(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, " \t\"") {
		return `"` + strings.ReplaceAll(s, `"`, "") + `"`
	}
	return s
}

// Update handles a keystroke, reporting whether it was consumed.
func (m Model) Update(msg tea.KeyMsg) (Model, bool) {
	if !m.open {
		return m, false
	}
	visible := m.visible()
	if len(visible) == 0 {
		return m, false
	}
	onCounting := m.fields[visible[m.focus]].key == "counting"

	switch msg.Type {
	case tea.KeyTab, tea.KeyDown:
		m.focus = (m.focus + 1) % len(visible)
		return m, true
	case tea.KeyShiftTab, tea.KeyUp:
		m.focus = (m.focus - 1 + len(visible)) % len(visible)
		return m, true
	case tea.KeyLeft:
		if onCounting {
			m.counting = (m.counting - 1 + len(countings)) % len(countings)
			m.focus = m.clampFocus()
		}
		return m, true
	case tea.KeyRight:
		if onCounting {
			m.counting = (m.counting + 1) % len(countings)
			m.focus = m.clampFocus()
		}
		return m, true
	case tea.KeyBackspace:
		if !onCounting {
			at := visible[m.focus]
			if r := []rune(m.fields[at].value); len(r) > 0 {
				m.fields[at].value = string(r[:len(r)-1])
			}
		}
		return m, true
	case tea.KeySpace:
		if !onCounting {
			m.fields[visible[m.focus]].value += " "
		}
		return m, true
	case tea.KeyRunes:
		if onCounting {
			// h and l choose, matching the motion keys everywhere else. Any
			// other letter would silently vanish, so it moves instead.
			switch string(msg.Runes) {
			case "h":
				m.counting = (m.counting - 1 + len(countings)) % len(countings)
			case "l":
				m.counting = (m.counting + 1) % len(countings)
			}
			m.focus = m.clampFocus()
			return m, true
		}
		m.fields[visible[m.focus]].value += string(msg.Runes)
		return m, true
	}
	return m, false
}

// clampFocus keeps the focus in range after a choice changes which fields exist.
func (m Model) clampFocus() int {
	if n := len(m.visible()); m.focus >= n {
		return n - 1
	}
	return m.focus
}

// Lines renders the panel for splicing into the list.
func (m Model) Lines() []string {
	if !m.open {
		return nil
	}
	visible := m.visible()
	head := "  new " + string(m.kind) + " "
	if pad := m.width - len([]rune(head)) - 2; pad > 0 {
		head += strings.Repeat("─", pad)
	}
	out := []string{frameStyle.Render(head)}

	for n, i := range visible {
		f := m.fields[i]
		focused := n == m.focus
		label := labelStyle.Render(fmt.Sprintf("  %-11s ", f.label))
		if f.key == "counting" {
			out = append(out, label+m.countingLine())
			continue
		}
		value := valueStyle.Render(f.value)
		if focused {
			value += focusStyle.Render(" ")
		}
		out = append(out, label+value)
	}

	if hint := m.fields[visible[m.focus]].hint; hint != "" {
		out = append(out, hintStyle.Render("  "+strings.Repeat(" ", 11)+" "+hint))
	}
	out = append(out, hintStyle.Render("  tab next   enter continue   esc discard"))
	return out
}

// countingLine renders the three presets as a choice rather than a field.
//
// All three are always shown. A choice you can only see one option of is a
// field with legal values, and people type into those.
func (m Model) countingLine() string {
	parts := make([]string, 0, len(countings))
	for i, c := range countings {
		if i == m.counting {
			parts = append(parts, chosenStyle.Render("(o) "+c.Label))
			continue
		}
		parts = append(parts, unchosenStyl.Render("( ) "+c.Label))
	}
	return strings.Join(parts, "  ")
}

func (m Model) View() string { return strings.Join(m.Lines(), "\n") }

// Height is how many lines the panel occupies.
func (m Model) Height() int { return len(m.Lines()) }
