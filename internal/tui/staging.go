package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/command"
	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/organise"
	"home-management-system/internal/tui/review"
)

// Organise mode: the arrangement, staged.
//
// `O` starts it. From then until `A` or esc, the verbs that move things about
// -- new, rename, move, re-file, archive -- add to a batch instead of writing,
// and the region below the house fills up with what the batch would come to.
// One keystroke applies the lot, in one transaction.
//
// The verbs are the SAME verbs. `o` is still new, `m` is still move, `e` is
// still rename; the only difference is when they take effect. A second
// alphabet for organising would have been a second thing to learn for an idea
// whose whole content is "not yet".
//
// The keyboard stays in the house. Every other drawer takes it -- you are
// choosing a destination, or a verb, from a list of its own -- but here the
// thing you are editing is the tree in the rail, and a drawer that blurred it
// would hide the one cursor that matters.

// staging is organise mode: the batch, and the review screen over it.
type staging struct {
	// batch is the source of truth, exactly as the bound file is for an
	// import.
	batch organise.Model
	// screen is that, as the drawer shows it. Derived, always.
	screen review.Model
}

// newStaging opens an empty batch.
func newStaging() *staging {
	s := &staging{}
	s.screen = review.New("ORGANISE", "staged, nothing written yet", "#", nil).Focused(false)
	return s.rebuild()
}

// rebuild renders the batch onto the screen again.
func (s *staging) rebuild() *staging {
	s.screen = s.screen.
		WithChanges(asStagedChanges(s.batch.Edits())).
		WithVerdict(s.batch.WhyNot())
	return s
}

// startOrganising puts the rail into organise mode, or takes it out again.
func (m Model) startOrganising() (Model, tea.Cmd) {
	if s, ok := m.organising(); ok {
		return m.stopOrganising(s)
	}
	if m.top() != nil {
		return m.refuse("finish what is open first"), nil
	}
	m = m.open(newStaging())
	m.say = m.say.Report("organising: the verbs stage instead of writing -- " +
		keys.Show(keys.Staging, keys.ApplyAll) + " applies the lot")
	return m, nil
}

// stopOrganising leaves the mode, saying what was abandoned.
//
// It says the COUNT, because nothing was written and therefore nothing can be
// recovered: a batch of nine that vanished silently would be nine pieces of
// work a person has no way of knowing they lost.
func (m Model) stopOrganising(s *staging) (Model, tea.Cmd) {
	m = m.close()
	if n := s.batch.Len(); n > 0 {
		m.say = m.say.Report(rowsPhrase(n) + " put down; nothing was written")
		return m, nil
	}
	m.say = m.say.Report("done organising")
	return m, nil
}

// stage adds what a verb produced to the batch instead of applying it.
//
// This is the whole of the interception, and it is here rather than in each
// verb because every verb already funnels through runCommands. A verb that
// had to know about organise mode would be a verb that could forget.
func (m Model) stage(s *staging, edits []organise.Edit) Model {
	s.batch = s.batch.Stage(edits...)
	s.rebuild()
	m.say = m.say.Clear()
	return m
}

// describedForStaging works out what each command would do and whether the
// house already refuses it.
//
// Off the keystroke path, like every other read: describing a command builds
// a search index and planning one asks the database, and neither belongs in
// the middle of a redraw.
func (m Model) describedForStaging(commands []command.Command) tea.Msg {
	edits := make([]organise.Edit, 0, len(commands))
	for _, cmd := range commands {
		edit := organise.Edit{
			Command: cmd,
			What:    m.ctrl.Describe(m.ctx, cmd),
		}
		// Planned now so an edit the house will not take says so while the
		// person still remembers making it. It is not the verdict -- the
		// batch is planned again as a whole when it is applied -- which is
		// why the refusal is recorded on the row rather than thrown as an
		// error.
		if _, err := m.ctrl.PlanCommand(m.ctx, cmd); err != nil {
			edit.Why = humanise(err.Error())
		}
		edits = append(edits, edit)
	}
	return stagedEditsMsg{edits: edits}
}

// stagedEditsMsg carries described edits back to the batch.
type stagedEditsMsg struct{ edits []organise.Edit }

// applyBatch writes the whole arrangement, or none of it.
//
// PlanAll, one transaction, exactly as an import stage applies -- which is
// the point of staging commands rather than a proposed tree. There is nothing
// here to translate.
func (m Model) applyBatch(s *staging) (Model, tea.Cmd) {
	if why := s.batch.WhyNot(); why != "" {
		return m.refuse("%s", why), nil
	}
	commands := s.batch.Commands()
	m.say = m.say.Working("applying the arrangement ...")
	return m, func() tea.Msg {
		combined, at, err := m.ctrl.PlanAll(m.ctx, commands)
		if err != nil {
			return issuesMsg{issues: []string{stagedFailure(at, err)}}
		}
		if err := m.ctrl.ApplyPlan(m.ctx, combined); err != nil {
			return issuesMsg{issues: []string{err.Error()}}
		}
		return arrangedMsg{edits: len(commands)}
	}
}

// arrangedMsg reports the batch committed.
type arrangedMsg struct{ edits int }

// stagedFailure names which staged edit the plan refused.
//
// By its POSITION in the batch, which is what the drawer's first column
// shows: the person is looking at a numbered list, and "the third one" is an
// answer they can act on where the error alone is not.
func stagedFailure(at int, err error) string {
	if at < 0 {
		return humanise(err.Error())
	}
	return fmt.Sprintf("staged edit %d: %s", at+1, humanise(err.Error()))
}

// takeBack removes the last staged edit and says what it was.
func (m Model) takeBack(s *staging) (Model, tea.Cmd) {
	batch, undone, ok := s.batch.TakeBack()
	if !ok {
		return m.refuse("nothing staged to take back"), nil
	}
	s.batch = batch
	s.rebuild()
	m.say = m.say.Report("took back: " + undone.What)
	return m, nil
}

// ---------------------------------------------------------------------------
// Organise mode as a drawer
// ---------------------------------------------------------------------------

func (s *staging) name() string { return "organising" }

func (s *staging) height(Model) int { return s.screen.Wants() }

func (s *staging) lines(m Model) []string {
	return lines(s.screen.SetSize(m.width, s.height(m)).View())
}

// facts is the batch's own counts, in place of the inspector.
func (s *staging) facts(Model) []string { return s.screen.Facts() }

// shares says the house keeps the cursor. See the note at the top of the file.
func (s *staging) shares() bool { return true }

// keys names what acts on the BATCH, and says the house is still live.
//
// The browse verbs are not listed: they are unchanged, they are already on
// the line the moment this drawer closes, and naming twenty of them here
// would bury the three keys that only exist while it is open.
func (s *staging) keys(Model) string {
	return keys.Hint(keys.Staging,
		[]keys.Action{keys.ApplyAll},
		[]keys.Action{keys.TakeBack},
		[]keys.Action{keys.Quit},
	) + " - the verbs stage"
}

// update takes the keystroke: the three that act on the batch, and then the
// house for everything else.
func (s *staging) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch keys.Lookup(keys.Staging, msg) {
	case keys.ApplyAll:
		return m.applyBatch(s)
	case keys.TakeBack:
		return m.takeBack(s)
	case keys.Quit, keys.Cancel:
		return m.stopOrganising(s)
	}
	// Everything else is the house, unchanged -- the motion, the folds, the
	// lens, and the verbs, which runCommands will divert into the batch.
	return m.handleKey(msg)
}
