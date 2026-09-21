package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/app"
	"home-management-system/internal/command"
	"home-management-system/internal/importer"
	"home-management-system/internal/resolve"
	"home-management-system/internal/tui/creator"
	"home-management-system/internal/tui/editor"
	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/review"
	"home-management-system/internal/tui/tree"
)

// The import flow: a file in, a plan screen per stage, a transaction each.
//
// It reuses the plan screen's table, the creation panel, and the confirmation
// -- all unchanged. If any of them had needed reworking for this, the
// interactive and bulk flows would have started to diverge into different
// products, and the fix would be the shared piece rather than a fork.
//
// A file that proposes a SHAPE -- categories, places, or both -- AND the things
// filed into it is reviewed in stages, for the reason importer.Stages gives:
// the later rows cannot resolve against categories and places that do not exist
// yet, so each tree is applied -- or skipped -- first, and the rest is bound
// again afterwards. A stage is a transaction, which is the one thing about this
// the screen has to be honest about, and it says so on every one of them.

// Import opens a file for review, on a Model that does not exist yet.
//
// This is the command line's way in: the plan is bound before the terminal is
// touched, so a file that cannot be read fails as a message rather than as a
// blank screen. From inside a running house it is reviewFile instead, which
// replaces the flow and keeps everything else -- see imports.go.
func Import(ctx context.Context, ctrl app.Controller, path string) (Model, error) {
	m := New(ctx, ctrl)
	flow, err := m.flowFor(path)
	if err != nil {
		return Model{}, err
	}
	m = m.withFlow(flow)
	// Pointed at the first row before the terminal is touched, so the house
	// arrives showing the shelf that row is about rather than the top of the
	// tree.
	m, _ = m.steer()
	return m, nil
}

// flowFor binds a plan file into the review flow it will be shown as.
func (m Model) flowFor(path string) (flow, error) {
	rows, err := ReadRows(path)
	if err != nil {
		return flow{}, err
	}
	vocabulary, err := m.ctrl.Vocabulary(m.ctx)
	if err != nil {
		return flow{}, err
	}
	names, err := m.ctrl.SearchIndex(m.ctx)
	if err != nil {
		return flow{}, err
	}

	// Only the stages the file actually has, so the counts the screen shows are
	// about the file rather than about the vocabulary. Most files propose no
	// shape at all and are one stage, which is why a screen that announced
	// "stage 1 of 1" would be describing itself rather than the file.
	stages := importer.Bind(m.ctx, vocabulary, path, rows).Stages()
	first := stages[0]
	return newFlow().staged(
		first.Plan, names,
		stageScreen(first.Stage, 1, len(stages)),
		stages[1:]), nil
}

// stageScreen describes a stage to the screen that shows it.
//
// Both of the things a stage can do beyond applying follow from what it IS, so
// neither is decided at a call site: a structural stage builds a tree, which is
// what makes it apply in passes, and it can be skipped -- but only where there
// is something to go on TO. Skipping the last stage of an import would be a
// longer way of pressing q, and a key offered under a screen that ignores it is
// worse than a key offered nowhere.
func stageScreen(stage importer.Stage, number, of int) review.Stage {
	return review.Stage{
		Name: stage.String(), Number: number, Of: of,
		Skippable: stage.Structural() && number < of,
		InPasses:  stage.Structural(),
	}
}

// ReadRows reads a plan off disk. It defers to importer.ReadFile, which is
// where the choice between the two transports lives now that three callers
// were making it.
func ReadRows(path string) ([]importer.Row, error) { return importer.ReadFile(path) }

// handleImport takes the keystroke while a plan is on screen.
func (m Model) handleImport(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch keys.Lookup(keys.Plan, msg) {
	case keys.Quit:
		// Cancelling leaves nothing behind, which is the whole of
		// all-or-nothing seen from the other end.
		return m, tea.Quit
	case keys.ApplyAll:
		return m.applyImport()
	case keys.SkipStage:
		return m.skipStage()
	case keys.Confirm:
		return m.settleRow()
	case keys.EditInPlace:
		return m.editRow()
	case keys.Drop:
		// Dropping is how a row gets settled without being fixed. It is
		// reversible right up until the stage is applied, which is why it
		// does not ask.
		//
		// Taken HERE rather than by the review screen, with the other keys
		// that change the file. Undropping is BINDING a row again, and a
		// screen that could bind would be a screen that had to hold the
		// vocabulary -- which is how it came to hold the whole importer.
		return m.withFlow(m.flow().drop(m.flow().screen.At())), nil
	case keys.Undrop:
		return m.withFlow(m.flow().undrop(m.flow().screen.At())), nil
	}
	was, _, _ := m.flow().current()
	next, handled := m.flow().screen.Update(msg)
	m = m.withFlow(m.flow().withScreen(next))
	if handled {
		return m.follow(was)
	}
	// Anything the plan does not want is still CONSUMED. Falling through put
	// the browse keystrokes live underneath the review screen: `#` opened a
	// count prompt, `c` a consume prompt, C-k a retirement -- each against
	// whatever the browse view had selected, which is not what the person is
	// looking at. A review screen that can write outside its own plan is not a
	// review screen.
	return m, nil
}

// follow points the house at whatever the row under the cursor would touch.
//
// This is the reason the plan stopped taking the screen. "add 100 g to Shelf
// 1" is not reviewable on its own -- to what, and how much is there already
// -- so the rail goes to the place the row lands and the contents pane shows
// what is in it now.
//
// Only on a CHANGE of row, and only where the row names a place: reloading
// per keystroke would be a query for the same rows, and a row that names
// nowhere leaves the house where it was rather than moving it somewhere
// arbitrary.
func (m Model) follow(was importer.Entry) (Model, tea.Cmd) {
	entry, _, ok := m.flow().current()
	if !ok || sameRow(was, entry) {
		return m, nil
	}
	next, moved := m.steer()
	if !moved {
		return m, nil
	}
	return next, next.load(viewShell)
}

// steer points the house at the row under the cursor, reporting whether it
// had to move. Separate from follow because the plan ARRIVING is the same
// question as the cursor moving within it, and a drawer that only steered on
// the second would open showing the wrong shelf.
func (m Model) steer() (Model, bool) {
	entry, _, ok := m.flow().current()
	if !ok || entry.Command == nil {
		return m, false
	}
	at, ok := app.Lands(entry.Command)
	if !ok {
		return m, false
	}
	key := tree.Node{ID: int64(at), Kind: kindLocation}.Key()
	if m.lens == lensPlace && m.railKey[lensPlace] == key {
		return m, false
	}
	m.lens = lensPlace
	m.railKey[lensPlace] = key
	return m, true
}

// sameRow reports two entries being the same row of the file. The LINE is the
// identity: an entry is re-bound and re-stated as it is settled, so comparing
// the whole thing would say "different" every time it changed.
func sameRow(a, b importer.Entry) bool { return a.Row.Line == b.Row.Line }

// applyImport commits the stage on screen, or none of it.
//
// All-or-nothing, per stage. That is not a weakening of the promise: a stage is
// a whole screen, reviewed and applied as one unit of work, and the person is
// told before they apply the first one that a second follows.
func (m Model) applyImport() (Model, tea.Cmd) {
	plan := m.flow().bound
	if !m.flow().applicable() {
		return m.refuse("%s", m.flow().whyNot()), nil
	}
	commands := plan.Commands()
	_, more := m.flow().waiting()
	// Another pass over this stage, when it applies in passes and rows are
	// waiting for what this one is about to create. It is asked HERE rather
	// than after the fact because the answer decides whether the import is
	// over, and an import that ended with rows still on the screen would be an
	// import that dropped rows nobody dropped.
	again := m.flow().screen.Stage().InPasses && len(plan.Unapplied().Entries) > 0
	earlier := m.flow().applied
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
		if again || more {
			return stageAppliedMsg{rows: len(commands)}
		}
		return importedMsg{rows: len(commands), earlier: earlier}
	}
}

// skipStage sets the whole stage aside, applying none of it.
//
// It is what makes a bulk upload of categories or places optional rather than a
// toll gate. A proposed shape is the part of a file an agent is most likely to
// get wrong, and the review screen can already file a row into a category or a
// place that exists -- so the fastest path through a bad one is not to fix it
// row by row. Nothing is written, which is why this does not ask.
//
// One stage, never the rest of them: a file whose categories are nonsense and
// whose places are right costs one keystroke, and the places are still there to
// review afterwards. That is the whole reason the two trees are two stages.
func (m Model) skipStage() (Model, tea.Cmd) {
	stage := m.flow().screen.Stage()
	if !stage.Skippable {
		return m, nil
	}
	return m, m.nextStage(stage.Lead()+fmt.Sprintf("skipped -- %s not applied, and the house is as it was",
		rowsPhrase(len(m.flow().bound.Entries))), 0)
}

// stageApplied moves on from a stage that has just been committed -- to the
// next stage, or to another pass over this one.
//
// Another pass, because a tree is built from the top down: a row filed under a
// category or a place this pass has just created could not bind until now, and
// it is sitting on this screen waiting for exactly that. Handing it to the next
// stage instead would be handing over a row that stage is not about, and going
// on without it would be dropping a row nobody dropped.
func (m Model) stageApplied(rows int) tea.Cmd {
	stage := m.flow().screen.Stage()
	// What the STAGE has applied, not what this pass did. A pass is an
	// implementation detail of building a tree; what was written to the house
	// is not.
	note := stage.Lead() + fmt.Sprintf("applied %s in %s",
		rowsPhrase(m.flow().inStage+rows), transactions(m.flow().passes+1))

	if left := m.flow().bound.Unapplied(); stage.InPasses && len(left.Entries) > 0 {
		return m.samePass(left, note+fmt.Sprintf(" -- %s left, bound again against it",
			rowsPhrase(len(left.Entries))), rows)
	}
	return m.nextStage(note, rows)
}

// samePass brings the rows a pass left behind back to the same stage, bound
// against the vocabulary that pass created.
func (m Model) samePass(left importer.Plan, note string, applied int) tea.Cmd {
	stage := m.flow().screen.Stage()
	return m.staging(left, stage, note, applied, true)
}

// nextStage binds the waiting stage against a vocabulary read now.
//
// Now, and not when the file was read: the stage just finished with may have
// created the very categories or places these rows name, and binding them
// against the older vocabulary would leave a row blocked by something that is
// sitting in the database.
func (m Model) nextStage(note string, applied int) tea.Cmd {
	waiting, ok := m.flow().waiting()
	if !ok {
		return nil
	}
	was := m.flow().screen.Stage()
	// One further along a sequence whose length was settled when the file was
	// read. A stage that is skipped is still a stage that happened, so the
	// numbering counts it: "stage 3 of 3" after skipping stage 1 is the truth
	// about the file, and renumbering would make the screen disagree with the
	// line above it saying what was skipped.
	return m.staging(waiting.Plan,
		stageScreen(waiting.Stage, was.Number+1, was.Of), note, applied, false)
}

// staging reads the house as it now is and hands back the plan to show next.
//
// One trip for both stages and both passes, because the reason is the same
// every time: whatever was just applied may be exactly what the rows being
// shown are waiting for, and binding them against the older vocabulary would
// leave a row blocked by something that is sitting in the database.
func (m Model) staging(plan importer.Plan, stage review.Stage, note string, applied int, same bool) tea.Cmd {
	return func() tea.Msg {
		vocabulary, err := m.ctrl.Vocabulary(m.ctx)
		if err != nil {
			return errMsg{err}
		}
		names, err := m.ctrl.SearchIndex(m.ctx)
		if err != nil {
			return errMsg{err}
		}
		return stagedMsg{
			plan: plan, stage: stage, vocabulary: vocabulary, names: names,
			note: note, applied: applied, same: same,
		}
	}
}

// stagedMsg carries the plan to show next, and the vocabulary to bind it
// against.
type stagedMsg struct {
	plan       importer.Plan
	stage      review.Stage
	vocabulary *command.Vocabulary
	names      command.Namer
	// same says this is another pass over the stage already on screen rather
	// than the one behind it -- which is the difference between keeping the
	// stage that is still waiting and losing it.
	same bool
	// note is what the stage before this one did. It is shown on the new screen
	// rather than in the status line, because the plan replaces the whole
	// interface -- a status line nobody can see is a report nobody got.
	note    string
	applied int
}

// staged puts the plan that came back on screen.
func (m Model) staged(msg stagedMsg) Model {
	// A fresh stage answers whatever the last one refused: the rows it refused
	// over are behind us, and the new screen says for itself what it can do.
	m.say = m.say.Clear()
	f := m.flow()
	if msg.same {
		f = f.again(msg.plan, msg.names, msg.stage, msg.note, msg.applied)
	} else {
		f = f.onward(msg.plan, msg.names, msg.stage, msg.note, msg.applied)
	}
	// Every row that is not settled, against what the house holds NOW. A row
	// that named a category the pass before it created is an ordinary row
	// from here, and nobody had to retype anything for it.
	return m.withFlow(f.rebindUnsettled(msg.vocabulary))
}

// stageAppliedMsg reports that a stage was committed and another one follows.
type stageAppliedMsg struct{ rows int }

// rowsPhrase counts rows in English. "1 rows" in the one line reporting what
// was just written to the house reads as a screen that is not being careful,
// on the screen where care is the whole product.
func rowsPhrase(n int) string {
	if n == 1 {
		return "1 row"
	}
	return fmt.Sprintf("%d rows", n)
}

// transactions counts them the same way, because the number of them is the
// claim this screen makes and hedging it would be the one place not to.
func transactions(n int) string {
	if n == 1 {
		return "one transaction"
	}
	return fmt.Sprintf("%d transactions", n)
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
	entry, at, ok := m.flow().current()
	if !ok {
		return m, nil
	}
	switch {
	case entry.State == importer.Ready || entry.State == importer.Dropped:
		return m, nil

	case entry.IsCreation():
		// The row IS the creation -- it bound completely, and agreeing to it is
		// all that is left. So enter agrees, and the thing is made when the
		// stage is applied, in the same transaction as everything else.
		//
		// Opening the panel here instead was the bug that made a bulk category
		// upload impossible: the panel created the category immediately, in a
		// transaction of its own, and the row still said it would create one --
		// so the stage could never be applied and pressing enter twice made two.
		if other, clash := m.flow().bound.AlreadyCreatedBy(at); clash {
			return m.refuse("row %d already creates that -- drop this row, or edit it to name something else",
				m.flow().bound.Entries[other].Row.Line), nil
		}
		settled, ok := importer.ConfirmCreation(entry)
		if !ok {
			return m, nil
		}
		m = m.withFlow(m.flow().settle(at, settled))
		return m, nil

	case m.flow().screen.Stage().InPasses && waitsFor(m.flow().bound, at):
		// Another row of this file is already making the thing this one is
		// missing. Offering to make it here would make a second one --
		// immediately, in its own transaction -- and leave both rows still
		// proposing one.
		other, _ := m.flow().bound.WillBeCreatedBy(at)
		return m.refuse("row %d creates that -- %s applies this stage, and this row is bound again against it",
			m.flow().bound.Entries[other].Row.Line, keys.Show(keys.Plan, keys.ApplyAll)), nil

	case len(entry.Creates) > 0:
		// The SAME panel `o` opens, with the same confirmation behind it. A
		// second one would mean two ideas of what is permanent.
		creation := entry.Creates[0]
		m = m.withFlow(m.flow().opened(at))
		m.creator = m.creator.Open(creatorKindFor(creation.Kind), "").
			WithName(creation.Name).SetWidth(m.width)
		return m, m.loadCandidates()

	default:
		// A suggestion, offered and never applied -- so accepting it is a
		// keystroke rather than something the screen did on your behalf.
		return m, m.acceptSuggestions(entry, at)
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
	entry, at, ok := m.flow().current()
	if !ok {
		return m, nil
	}
	m = m.withFlow(m.flow().opened(at))
	m.editor = m.editor.
		OpenFor(editor.Row, "", int64(at), prompt(editor.Row).label, entry.AsLine()).
		WithVerb(prompt(editor.Row).verb).
		SetWidth(m.width)
	return m.suggestForPrompt(), m.loadCandidates()
}

// applyRowEdit re-parses the edited line and binds the row again.
func (m Model) applyRowEdit() (Model, tea.Cmd) {
	line := strings.TrimSpace(m.editor.Value())
	at, _ := m.flow().settlingRow()
	m.editor = m.editor.Close()
	m = m.settled()

	entry, _, ok := m.flow().current()
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
	return m, m.rebindRow(at, importer.Rewrite(entry, raw))
}

// acceptSuggestions rewrites the row with what the resolver suggested and binds
// it again.
func (m Model) acceptSuggestions(entry importer.Entry, at int) tea.Cmd {
	return m.rebindRow(at, importer.AcceptSuggestions(entry))
}

// waitsFor reports whether the row at `at` is waiting for another row of the
// same plan rather than for a person.
//
// Asked only on a stage that applies in passes. On one that applies all at
// once, waiting comes to nothing -- the thing is never made until the whole
// stage goes in -- so the panel is the row's only route and refusing would be
// refusing the only thing that could work.
func waitsFor(plan importer.Plan, at int) bool {
	_, ok := plan.WillBeCreatedBy(at)
	return ok
}

// importedMsg reports that the last stage of a file was applied.
//
// earlier is what the stages before it committed, so the report can be about
// the import rather than about its final screen -- and can say plainly that it
// took more than one transaction.
type importedMsg struct{ rows, earlier int }

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
func (m Model) rebindRow(at int, entry importer.Entry) tea.Cmd {
	return func() tea.Msg {
		vocabulary, err := m.ctrl.Vocabulary(m.ctx)
		if err != nil {
			return errMsg{err}
		}
		// The names too, and in the same read: settling a row may have created
		// the thing the other rows name, so the plan has to be able to say what
		// they now mean.
		names, err := m.ctrl.SearchIndex(m.ctx)
		if err != nil {
			return errMsg{err}
		}
		return reboundMsg{at: at, entry: entry, vocabulary: vocabulary, names: names}
	}
}

// reboundMsg carries a freshly read vocabulary back to the plan.
type reboundMsg struct {
	at         int
	entry      importer.Entry
	vocabulary *command.Vocabulary
	names      command.Namer
}

// rebound applies what the read came back with.
func (m Model) rebound(msg reboundMsg) Model {
	m = m.withFlow(m.flow().withNames(msg.names))
	m = m.withFlow(m.flow().settle(msg.at, importer.Settle(msg.vocabulary, msg.entry)))

	// And every OTHER row that is not settled yet, because settling this one
	// may have created something. Two rows naming one new item must create it
	// once: the first opens the panel, and the second has to find what the
	// first made rather than offering to make it again.
	//
	// Ready rows are left alone -- they already hold identifiers, and
	// re-resolving them could quietly move one onto something created since.
	m = m.withFlow(m.flow().rebindUnsettled(msg.vocabulary))
	return m
}
