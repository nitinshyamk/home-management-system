package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/app"
	"home-management-system/internal/resolve"
	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/style"
	"home-management-system/internal/tui/table"
)

// What needs answering: a count you cannot miss, and the list a keystroke
// away.
//
// It used to be a whole screen you had to know to go to -- `!` loaded a page
// of prose that reported the integrity job and then, underneath, the
// classification nudges, while the expiry and out-too-long flags lived
// somewhere else entirely, in a column of the table. Four things wanting
// attention, in three shapes, none of which said anything unless you went
// looking.
//
// Now the count sits in the chrome whenever it is not zero, and `!` opens
// the queue in the drawer like everything else. Enter goes TO the thing,
// which is the difference between a report and a queue: a list you can read
// is not the same as a list you can work down.

// nagging is the queue, as a drawer.
type nagging struct {
	rows []app.Nagging
	tbl  table.Model
}

func newNagging(rows []app.Nagging, width int) *nagging {
	out := make([]table.Row, 0, len(rows))
	for i, r := range rows {
		tone := table.ToneAttention
		if r.Level == app.AttentionOver {
			tone = table.ToneStop
		}
		out = append(out, table.Row{
			Key: int64(i), Tone: tone,
			Cells: []string{r.What, r.Where},
		})
	}
	return &nagging{
		rows: rows,
		tbl: table.New([]table.Column{
			{Title: "WHAT WANTS ANSWERING", Min: 24, Grow: true},
			{Title: "WHERE", Min: 10, Drop: 2},
		}).Fixed().SetRows(out).SetSize(width, len(out)+2),
	}
}

// openAttention asks for the queue.
func (m Model) openAttention() (Model, tea.Cmd) {
	if m.top() != nil {
		return m.refuse("finish what is open first"), nil
	}
	m.say = m.say.Working("looking for what needs answering ...")
	return m, func() tea.Msg {
		rows, err := m.ctrl.Attention(m.ctx)
		if err != nil {
			return errMsg{err}
		}
		return attentionMsg{rows: rows}
	}
}

// attentionMsg carries the queue.
type attentionMsg struct{ rows []app.Nagging }

// showAttention puts the queue up, or says there is nothing in it.
func (m Model) showAttention(msg attentionMsg) (Model, tea.Cmd) {
	m.nags = len(msg.rows)
	m.pressing = app.Pressing(msg.rows)
	if len(msg.rows) == 0 {
		m.say = m.say.Report("nothing wants answering")
		return m, nil
	}
	return m.open(newNagging(msg.rows, m.width)), nil
}

// goToNag points the house at whatever the cursor is on and closes the queue.
//
// The queue's whole reason for existing over a report: you are not reading
// about the cable that has been out three weeks, you are going to deal with
// it. A list that could only be read would leave the going-there as the
// part nobody automated, which is the part that stops people doing it.
func (m Model) goToNag(n *nagging) (Model, tea.Cmd) {
	row, ok := n.tbl.Current()
	if !ok || int(row.Key) >= len(n.rows) {
		return m, nil
	}
	nag := n.rows[row.Key]
	if nag.Kind == "" {
		// A fact about the ledger rather than about a thing on a shelf. There
		// is nowhere to go, and pretending otherwise by landing somewhere
		// arbitrary is worse than saying so.
		return m.refuse("that one is about the ledger itself, not a shelf"), nil
	}
	m = m.close()
	// Through the SAME mechanism a jump uses: land the view, then put the
	// cursor on the row once it has arrived. A second way to go somewhere
	// would be a second thing to keep in step with how the rail is keyed.
	m.lens = lensPlace
	m.pending = &resolve.Candidate{Kind: resolve.KindHolding, ID: nag.ID}
	m.say = m.say.Report(nag.What)
	return m, m.load(viewShell)
}

// ---------------------------------------------------------------------------
// The queue as a drawer
// ---------------------------------------------------------------------------

func (n *nagging) name() string { return "what wants answering" }

// shown is what the table gets: a line per nag, its heading, and the line the
// table keeps for itself. The queue never scrolls until it is long, because
// a list of four things you have to page through is a list that reads as
// longer than it is.
func (n *nagging) shown() int { return min(len(n.rows)+2, 12) }

func (n *nagging) height(Model) int { return n.shown() + 1 }

func (n *nagging) lines(m Model) []string {
	want := " want answering"
	if len(n.rows) == 1 {
		want = " wants answering"
	}
	head := style.Dim.Render("ATTENTION  ") +
		style.Strong.Render(fmt.Sprintf("%d", len(n.rows))) +
		style.Dim.Render(want)
	// Reported, never repaired. It was the last line of the screen this
	// queue replaced, and it is not decoration: silently correcting a
	// discrepancy would destroy the only signal that a write skipped its
	// event, so the one place that shows them has to say it does not.
	if n.aboutTheLedger() {
		head += style.Dim.Render("   the ledger's own disagreements are reported, not repaired")
	}
	return append([]string{head}, lines(n.tbl.SetSize(m.width, n.shown()).View())...)
}

// aboutTheLedger reports the queue holding something that is about the
// records rather than about a shelf.
func (n *nagging) aboutTheLedger() bool {
	for _, r := range n.rows {
		if r.Kind == "" && r.Level == app.AttentionOver {
			return true
		}
	}
	return false
}

func (n *nagging) facts(Model) []string {
	over, soon := 0, 0
	for _, r := range n.rows {
		if r.Level == app.AttentionOver {
			over++
		} else {
			soon++
		}
	}
	return []string{
		style.Error.Render(fmt.Sprintf("%d past it", over)),
		style.Warn.Render(fmt.Sprintf("%d coming up", soon)),
	}
}

func (n *nagging) keys(Model) string {
	return keys.Show(keys.Browse, keys.Confirm) + " go to it - " +
		keys.Show(keys.Browse, keys.Cancel) + " close"
}

func (n *nagging) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch keys.Lookup(keys.Browse, msg) {
	case keys.Confirm:
		return m.goToNag(n)
	case keys.Cancel, keys.Quit, keys.ViewAttention:
		return m.close(), nil
	}
	next, _ := n.tbl.Update(msg)
	n.tbl = next
	return m, nil
}

// ---------------------------------------------------------------------------
// The one-line banner
// ---------------------------------------------------------------------------

// attentionLine is the count, where it cannot be missed, or nothing.
//
// One line, and only when there is something in it. It says how many and how
// to see them, and deliberately not WHAT they are: the specific sentence is
// in the queue, and a banner that tried to fit one of four onto a row would
// have to choose which -- so three of them would go unmentioned while the
// line claimed to be the place that mentions things.
func (m Model) attentionLine() string {
	if m.nags == 0 || m.dismissed {
		return ""
	}
	said := fmt.Sprintf("%d want answering", m.nags)
	if m.nags == 1 {
		said = "1 wants answering"
	}
	lead := style.Warn
	if m.pressing > 0 {
		lead = style.Error
		said = fmt.Sprintf("%d past it, %d in all", m.pressing, m.nags)
	}
	return lead.Render("! ") + style.Dim.Render(said+" -- "+
		keys.Show(keys.Browse, keys.ViewAttention)+" to see them")
}

// countAttention refreshes the banner's number after a write.
//
// Asked on every load rather than kept up to date by hand, because the
// number is derived from the house and a copy of it maintained alongside
// would be wrong exactly when it mattered -- after the write that answered
// the last one.
func (m Model) countAttention() tea.Cmd {
	return func() tea.Msg {
		rows, err := m.ctrl.Attention(m.ctx)
		if err != nil {
			// Silently, because this is a count in the corner of a screen
			// somebody is using for something else. A failure to count is
			// not worth interrupting them with; the queue itself will say so
			// when they open it.
			return countedAttentionMsg{}
		}
		return countedAttentionMsg{n: len(rows), pressing: app.Pressing(rows)}
	}
}

type countedAttentionMsg struct{ n, pressing int }

// reload loads a view AND recounts what wants answering.
//
// Used where something was WRITTEN, and nowhere else. Counting reads every
// holding plus both reports, so doing it on every load would put that behind
// every press of C-n on the rail; doing it only when the house changed is
// the same answer for a fraction of the work, because nothing but a write
// can change it.
func (m Model) reload(v view) tea.Cmd {
	return tea.Batch(m.load(v), m.countAttention())
}
