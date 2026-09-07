package tui

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/app"
	"home-management-system/internal/command"
	"home-management-system/internal/importer"
	"home-management-system/internal/resolve"
	"home-management-system/internal/tui/creator"
	"home-management-system/internal/tui/editor"
	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/planview"
)

// The import flow: a file in, a plan screen, one transaction.
//
// It reuses the plan screen's table, the creation panel, and the confirmation
// -- all unchanged. If any of them had needed reworking for this, the
// interactive and bulk flows would have started to diverge into different
// products, and the fix would be the shared piece rather than a fork.

// Import opens a file for review.
func Import(ctx context.Context, ctrl app.Controller, path string) (Model, error) {
	rows, err := ReadRows(path)
	if err != nil {
		return Model{}, err
	}
	vocabulary, err := ctrl.Vocabulary(ctx)
	if err != nil {
		return Model{}, err
	}

	m := New(ctx, ctrl)
	m.importing = true
	m.plan = planview.New(importer.Bind(ctx, vocabulary, path, rows), summariser{ctrl: ctrl, ctx: ctx})
	return m, nil
}

// ReadRows picks the transport from the file's name.
//
// The two are interchangeable, so the only thing the extension decides is which
// parser reads the bytes -- not what the rows mean.
func ReadRows(path string) ([]importer.Row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	if strings.HasSuffix(strings.ToLower(path), ".csv") {
		return importer.ReadCSV(f)
	}
	return importer.ReadJSONL(f)
}

// summariser renders a bound row in the terms of the receipt.
type summariser struct {
	ctrl app.Controller
	ctx  context.Context
}

func (s summariser) Describe(entry importer.Entry) string {
	if entry.Command != nil {
		return s.ctrl.Describe(s.ctx, entry.Command)
	}
	// Not bound yet, so there are no identifiers to render names from. The raw
	// text is what the person wrote, which is the next best thing to show and
	// is in fact what they will be correcting.
	return entry.AsWritten()
}

// handleImport takes the keystroke while a plan is on screen.
func (m Model) handleImport(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch keys.Lookup(keys.Plan, msg) {
	case keys.Quit:
		// Cancelling leaves nothing behind, which is the whole of
		// all-or-nothing seen from the other end.
		return m, tea.Quit
	case keys.ApplyAll:
		return m.applyImport()
	case keys.Confirm:
		return m.settleRow()
	case keys.EditInPlace:
		return m.editRow()
	}
	next, handled := m.plan.Update(msg)
	m.plan = next
	if handled {
		return m, nil
	}
	// Anything the plan does not want is still CONSUMED. Falling through put
	// the browse keystrokes live underneath the review screen: `#` opened a
	// count prompt, `c` a consume prompt, C-k a retirement -- each against
	// whatever the browse view had selected, which is not what the person is
	// looking at. A review screen that can write outside its own plan is not a
	// review screen.
	return m, nil
}

// applyImport commits the whole file, or none of it.
func (m Model) applyImport() (Model, tea.Cmd) {
	plan := m.plan.Plan()
	if !plan.Applicable() {
		return m.refuse("%s", plan.Why()), nil
	}
	commands := plan.Commands()
	return m, func() tea.Msg {
		// Planned as one unit BEFORE anything is applied, so a row that cannot
		// work is named here rather than rolling back a transaction and naming
		// nothing.
		combined, at, err := m.ctrl.PlanAll(m.ctx, commands)
		if err != nil {
			return issuesMsg{issues: []string{fmt.Sprintf("row %d: %v", rowOf(plan, at), err)}}
		}
		if err := m.ctrl.ApplyPlan(m.ctx, combined); err != nil {
			return issuesMsg{issues: []string{err.Error()}}
		}
		return importedMsg{rows: len(commands)}
	}
}

// rowOf maps a command's index back to the line it came from.
func rowOf(plan importer.Plan, at int) int {
	n := 0
	for _, entry := range plan.Entries {
		if entry.State != importer.Ready || entry.Command == nil {
			continue
		}
		if n == at {
			return entry.Row.Line
		}
		n++
	}
	return 0
}

// settleRow is what enter does to whatever the cursor is on.
func (m Model) settleRow() (Model, tea.Cmd) {
	entry, at, ok := m.plan.Current()
	if !ok {
		return m, nil
	}
	switch {
	case entry.State == importer.Ready || entry.State == importer.Dropped:
		return m, nil

	case len(entry.Creates) > 0:
		// The SAME panel `o` opens, with the same confirmation behind it. A
		// second one would mean two ideas of what is permanent.
		creation := entry.Creates[0]
		m.settling = at
		m.creator = m.creator.Open(creatorKindFor(creation.Kind), "").
			WithName(creation.Name).SetWidth(m.width)
		return m, m.loadCandidates()

	default:
		// A suggestion, offered and never applied -- so accepting it is a
		// keystroke rather than something the screen did on your behalf.
		return m.acceptSuggestions(entry, at), nil
	}
}

// editRow opens the row as the command line it is.
//
// Every row, whatever its state. A ready row is edited because the file said
// something true but not what you meant; a blocked row because it said
// something that does not resolve; a creating row because you would rather
// name an item that exists than make a new one. Refusing on any of those makes
// the key look broken, which is exactly how it looked.
//
// At the row, like every other field in this interface.
func (m Model) editRow() (Model, tea.Cmd) {
	entry, at, ok := m.plan.Current()
	if !ok {
		return m, nil
	}
	m.settling = at
	m.editor = m.editor.
		OpenFor(editor.Row, "", int64(at), prompt(editor.Row).label, entry.AsLine()).
		WithVerb(prompt(editor.Row).verb).
		SetWidth(m.width)
	return m.suggestForPrompt(), m.loadCandidates()
}

// applyRowEdit re-parses the edited line and binds the row again.
func (m Model) applyRowEdit() (Model, tea.Cmd) {
	line := strings.TrimSpace(m.editor.Value())
	at := m.settling
	m.editor = m.editor.Close()
	m.settling = -1

	entry, _, ok := m.plan.Current()
	if !ok || at < 0 {
		return m, nil
	}
	raw, err := command.Parse(line)
	if err != nil {
		// The row keeps exactly what it had. Writing back a line that could not
		// be read would leave a third state -- neither what the file said nor
		// what was typed -- and nothing downstream could tell which it was
		// looking at.
		return m.refuse("%s", command.Humanise(err.Error())), nil
	}
	return m.rebindRow(at, importer.Rewrite(entry, raw)), nil
}

// acceptSuggestions rewrites the row with what the resolver suggested and binds
// it again.
func (m Model) acceptSuggestions(entry importer.Entry, at int) Model {
	settled := importer.AcceptSuggestions(entry)
	return m.rebindRow(at, settled)
}

// importedMsg reports that a whole file was applied.
type importedMsg struct{ rows int }

// creatorKindFor maps what a row would create to the panel that makes one.
func creatorKindFor(kind resolve.Kind) creator.Kind {
	switch kind {
	case resolve.KindCategory:
		return creator.KindCategory
	case resolve.KindLocation:
		return creator.KindLocation
	}
	return creator.KindItem
}

// rebindRow re-binds a row whose text has been corrected, against a freshly
// read vocabulary.
//
// Freshly read, because settling a row may have CREATED something -- and the
// next row that names it has to be able to find it. A vocabulary loaded once
// and held would make the second of two rows naming one new thing fail, which
// is exactly the case the whole design is arranged around.
func (m Model) rebindRow(at int, entry importer.Entry) Model {
	vocabulary, err := m.ctrl.Vocabulary(m.ctx)
	if err != nil {
		return m.refuse("%v", err)
	}
	m.plan = m.plan.Settle(at, importer.Settle(vocabulary, entry))

	// And every OTHER row that is not settled yet, because settling this one
	// may have created something. Two rows naming one new item must create it
	// once: the first opens the panel, and the second has to find what the
	// first made rather than offering to make it again.
	//
	// Ready rows are left alone -- they already hold identifiers, and
	// re-resolving them could quietly move one onto something created since.
	m.plan = m.plan.RebindUnsettled(vocabulary)
	return m
}
