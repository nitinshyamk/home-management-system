package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/app"
	"home-management-system/internal/domain"
	"home-management-system/internal/tui/editor"
	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/review"
	"home-management-system/internal/tui/style"
	"home-management-system/internal/tui/walk"
)

// The walk: checking a subtree against the house it claims to describe.
//
// `V` on any node walks everything under it, one holding at a time, set
// large, with the ledger's claim under it and three answers -- it is right,
// it is this much instead, it is not here. When every stop has been answered
// the same drawer turns into the review of what you said, and one keystroke
// files the lot in a single transaction.
//
// Two faces, one drawer, because they are two halves of one job. A review
// that opened somewhere else would make the walk a thing you do and then go
// looking for the results of.
//
// # This assumes you brought the terminal to the shelf
//
// It is designed for a laptop on the garage floor, and that is a real
// assumption rather than an oversight. If the honest ergonomics turn out to
// be "phone in hand, laptop upstairs", the right shape is not this screen
// with a smaller font: it is a printed checklist and a filled one read back
// -- which is an import, and the import already exists. Nothing here blocks
// that; the walk compiles to Commands like every other producer, so a
// checklist reader would join at the same seam.

// walking is a walk in progress, and then the review of it.
type walking struct {
	w walk.Model
	// asking is the first face. It goes false when every stop has been
	// answered, or when the person asks to see what they have said.
	asking bool
	// screen is the review of what was said. Derived from the walk, always.
	screen review.Model
}

// startWalk walks everything under whatever the rail is pointing at.
func (m Model) startWalk() (Model, tea.Cmd) {
	if m.top() != nil {
		return m.refuse("finish what is open first"), nil
	}
	if m.view != viewShell {
		return m.refuse("a walk goes through the house; press %s first",
			keys.Show(keys.Browse, keys.Refresh)), nil
	}
	m.say = m.say.Working("gathering what is under here ...")
	return m, m.gatherWalk()
}

// gatherWalk reads what is under the rail's node, deeply.
//
// Deeply whatever the here-only toggle says, because a walk is a physical
// trip: you are standing in front of the crate, and the things inside the
// boxes inside it are in front of you too.
func (m Model) gatherWalk() tea.Cmd {
	where, under := m.walkRoot()
	return func() tea.Msg {
		rows, err := m.ctrl.HoldingsUnder(m.ctx, under, true)
		if err != nil {
			return errMsg{err}
		}
		return walkMsg{where: where, rows: rows}
	}
}

// walkRoot is what the rail is pointing at, and what to call it.
func (m Model) walkRoot() (string, *domain.LocationID) {
	sh, ok := m.shell()
	if !ok || m.lens != lensPlace {
		return "the house", nil
	}
	node, ok := sh.railNode()
	if !ok || m.atRailRoot() {
		return "the house", nil
	}
	at := domain.LocationID(node.ID)
	return node.Name, &at
}

// walkMsg carries the holdings a walk will visit.
type walkMsg struct {
	where string
	rows  []app.HoldingRow
}

// asStops turns rows into the questions a walk asks.
//
// Retired holdings are left out. They are done with -- there is nothing on
// the shelf to look for, and asking would be asking a person to confirm the
// absence of something the ledger already knows is gone.
func asStops(rows []app.HoldingRow) []walk.Stop {
	out := make([]walk.Stop, 0, len(rows))
	for _, r := range rows {
		if r.Retired {
			continue
		}
		out = append(out, walk.Stop{
			Holding: r.ID, Item: r.Item, Where: r.LocationPath,
			Claim: r.State, Bulk: r.Bulk(), OnHand: r.OnHand, Unit: r.Unit,
		})
	}
	return out
}

// openWalk puts the gathered walk up.
func (m Model) openWalk(msg walkMsg) (Model, tea.Cmd) {
	stops := asStops(msg.rows)
	if len(stops) == 0 {
		return m.refuse("there is nothing under %s to check", msg.where), nil
	}
	wk := &walking{w: walk.New(msg.where, stops), asking: true}
	wk.screen = review.New("CHECKED", msg.where, "#", nil).Focused(false)
	wk.rebuild()
	m = m.open(wk)
	// What there is to do, and NOT how to do it: the three answers are on
	// the permanent line at the bottom already, and saying them twice ran
	// this report onto a second row -- which pushes the house up by one and
	// makes the walk start by moving the thing it is about.
	m.say = m.say.Report(fmt.Sprintf("walking %s -- %s to check",
		msg.where, rowsPhrase(len(stops))))
	return m, nil
}

// rebuild renders what has been said onto the review face.
func (wk *walking) rebuild() {
	wk.screen = wk.screen.
		WithChanges(asWalked(wk.w.Stops())).
		WithVerdict(wk.w.WhyNot())
}

// fileWalk writes what the walk found, or none of it.
func (m Model) fileWalk(wk *walking) (Model, tea.Cmd) {
	if why := wk.w.WhyNot(); why != "" {
		return m.refuse("%s", why), nil
	}
	commands := wk.w.Commands()
	m.say = m.say.Working("filing what the walk found ...")
	return m, func() tea.Msg {
		combined, at, err := m.ctrl.PlanAll(m.ctx, commands)
		if err != nil {
			return issuesMsg{issues: []string{walkFailure(wk.w, at, err)}}
		}
		if err := m.ctrl.ApplyPlan(m.ctx, combined); err != nil {
			return issuesMsg{issues: []string{err.Error()}}
		}
		return walkedMsg{checked: len(commands), of: wk.w.Len(), where: wk.w.Where()}
	}
}

// walkedMsg reports the walk filed.
type walkedMsg struct {
	checked, of int
	where       string
}

// walkFailure names which stop the plan refused, by the thing on the shelf.
func walkFailure(w walk.Model, at int, err error) string {
	said := 0
	for _, s := range w.Stops() {
		if _, ok := s.Command(); !ok {
			continue
		}
		if said == at {
			return fmt.Sprintf("%s: %s", s.Item, humanise(err.Error()))
		}
		said++
	}
	return humanise(err.Error())
}

// askAmount opens the field for "there is this much instead".
func (m Model) askAmount(wk *walking) (Model, tea.Cmd) {
	stop, ok := wk.w.Current()
	if !ok {
		return m, nil
	}
	if !stop.Bulk {
		return m.refuse("%q is one of a kind -- it is either there or it is not", stop.Item), nil
	}
	m.editor = m.editor.
		OpenFor(editor.WalkCount, "Holding", int64(stop.Holding),
			fmt.Sprintf("how much %s is actually there?", stop.Item), "").
		WithVerb("count").SetWidth(m.width)
	return m, nil
}

// countedOnWalk takes the figure the field came back with.
func (m Model) countedOnWalk(wk *walking, text string) (Model, tea.Cmd) {
	stop, ok := wk.w.Current()
	if !ok {
		return m, nil
	}
	observed, err := m.amountFor(app.HoldingRow{ID: stop.Holding}, text)
	if err != nil {
		return m.refuse("%v", err), nil
	}
	wk.w = wk.w.Say(walk.Amount, observed)
	wk.rebuild()
	return m.afterAnswer(wk), nil
}

// afterAnswer moves the house to the next stop, and turns the drawer over to
// its review face once there is nothing left to ask.
func (m Model) afterAnswer(wk *walking) Model {
	if wk.w.Done() {
		wk.asking = false
		m.say = m.say.Report(fmt.Sprintf("that is all of %s -- %s files it",
			wk.w.Where(), keys.Show(keys.Walk, keys.ApplyAll)))
		return m
	}
	m.say = m.say.Clear()
	return m
}

// answer records one and moves on.
func (m Model) answer(wk *walking, a walk.Answer) (Model, tea.Cmd) {
	stop, ok := wk.w.Current()
	if !ok {
		return m, nil
	}
	if a == walk.Amount && !stop.Bulk {
		return m.askAmount(wk)
	}
	wk.w = wk.w.Say(a, domain.Zero)
	wk.rebuild()
	return m.afterAnswer(wk), nil
}

// ---------------------------------------------------------------------------
// The walk as a drawer
// ---------------------------------------------------------------------------

func (wk *walking) name() string {
	if wk.asking {
		return "walking"
	}
	return "what the walk found"
}

func (wk *walking) height(m Model) int {
	if wk.asking {
		// Fixed, and generous. The stop is the whole point of the screen and
		// it is being read at arm's length from a shelf, so it gets room
		// rather than as little as it can be squeezed into.
		return 6
	}
	const leastHouse = 6
	return max(6, min(wk.screen.Wants(), m.room()-leastHouse-1))
}

func (wk *walking) lines(m Model) []string {
	if !wk.asking {
		return lines(wk.screen.SetSize(m.width, wk.height(m)).View())
	}
	stop, ok := wk.w.Current()
	if !ok {
		return nil
	}
	head := style.Dim.Render("WALK  ") + style.Strong.Render(wk.w.Where()) +
		style.Dim.Render(fmt.Sprintf("   %d of %d   %d answered",
			wk.w.At()+1, wk.w.Len(), wk.w.Answered()))
	// The thing, then where it is, then what the ledger claims -- in that
	// order, because that is the order you need them in while looking for it.
	// The claim is LAST and on its own line: it is the thing being disputed,
	// and putting it beside the name invites reading the two as one label.
	out := []string{
		head,
		"  " + style.Strong.Render(stop.Item),
		"  " + style.Dim.Render("in ") + stop.Where,
		"  " + style.Dim.Render("the ledger says ") + style.Focus.Render(stop.Claim),
	}
	if said := wk.w.Stops()[wk.w.At()].Says(); said != "" {
		out = append(out, "  "+style.Ready.Render("you said: ")+style.Dim.Render(said))
	} else {
		out = append(out, "")
	}
	return out
}

// facts is the walk's own arithmetic: how much of it has been done.
func (wk *walking) facts(Model) []string {
	if !wk.asking {
		return wk.screen.Facts()
	}
	return []string{
		style.Strong.Render(fmt.Sprintf("%d of %d checked", wk.w.Answered(), wk.w.Len())),
		style.Dim.Render("nothing is written until you file it"),
	}
}

func (wk *walking) keys(Model) string {
	if !wk.asking {
		return keys.Hint(keys.Walk,
			[]keys.Action{keys.ApplyAll},
			[]keys.Action{keys.MoveUp, keys.MoveDown},
			[]keys.Action{keys.Quit})
	}
	return keys.Hint(keys.Walk,
		[]keys.Action{keys.Right},
		[]keys.Action{keys.Amount},
		[]keys.Action{keys.Missing},
		[]keys.Action{keys.ApplyAll},
		[]keys.Action{keys.Quit})
}

func (wk *walking) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch keys.Lookup(keys.Walk, msg) {
	case keys.Right:
		return m.answer(wk, walk.Right)
	case keys.Amount:
		return m.askAmount(wk)
	case keys.Missing:
		return m.answer(wk, walk.Missing)
	case keys.ApplyAll:
		// Filing early is allowed, and files what was CHECKED. The shelves
		// nobody reached are exactly as unverified as they were, and a walk
		// that quietly confirmed them would leave the ledger more confident
		// than before somebody tried to check it.
		return m.fileWalk(wk)
	case keys.MoveDown:
		wk.w = wk.w.Step(1)
		wk.asking = true
		return m, nil
	case keys.MoveUp:
		wk.w = wk.w.Step(-1)
		wk.asking = true
		return m, nil
	case keys.Quit, keys.Cancel:
		m = m.close()
		if n := wk.w.Answered(); n > 0 {
			m.say = m.say.Report(fmt.Sprintf(
				"stopped walking %s; %s checked and not filed", wk.w.Where(), rowsPhrase(n)))
			return m, nil
		}
		m.say = m.say.Report("stopped walking " + wk.w.Where())
		return m, nil
	}
	return m, nil
}

// asWalked renders what was said as changes to review.
//
// The third adapter, after a file and a staged batch, and the shortest of
// the three: a Stop already knows how to say what it decided, because the
// wording depends on which kind of holding it is and that is knowledge the
// walk has and the screen does not.
func asWalked(stops []walk.Stop) []review.Change {
	out := make([]review.Change, 0, len(stops))
	for i, s := range stops {
		state := review.Confirmable
		why := "not checked"
		if s.Said() {
			state, why = review.Ready, ""
		}
		what := s.Says()
		if what == "" {
			what = strings.TrimSpace(s.Item + "  " + s.Claim)
		}
		out = append(out, review.Change{
			State: state, At: fmt.Sprintf("%d", i+1), What: what, Why: why,
		})
	}
	return out
}
