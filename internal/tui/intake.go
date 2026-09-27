package tui

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/app"
	"home-management-system/internal/command"
	"home-management-system/internal/config"
	"home-management-system/internal/intake"
	"home-management-system/internal/tui/editor"
	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/style"
	"home-management-system/internal/tui/table"
)

// The import walk, in the house.
//
// It used to be a conversation on stdin: hms printed where the folder was and
// blocked on `press enter when it is there`. The reasoning was sound as far
// as it went -- the pause in the middle of an import is measured in hours, a
// person spends it in a file manager, and a full-screen interface would be
// covering that up.
//
// What the reasoning missed is that blocking is not the only way to wait. The
// drawer waits by staying drawn: the house is still behind it, the folder is
// still named on screen, and `r` looks again. That is the same pause without
// the program being unusable for the duration of it -- and it is what makes
// "you are importing against THIS house" true rather than asserted, because
// the house is right there while you do it.
//
// So `i` opens every import folder and what each one needs. Not only the ones
// with plans: the list that showed those could not answer the question people
// actually have, which is "what was I in the middle of".
//
// # Three phases, one drawer
//
// choosing -> an import, with its state; or a new one
// naming   -> what to call it
// filling  -> where the folder is, what is in it, and `r` to look again
//
// and then the review drawer opens on top, which is where it was always
// going.

// intaking is the import walk as a drawer.
type intaking struct {
	phase intakePhase

	root string
	// found is every import folder and what it needs.
	found []intake.Status
	tbl   table.Model

	// at is the folder being filled, once one has been chosen or made.
	at intake.Status
	// pending is a folder named on the command line, waiting to be opened
	// once the program is running. Opening one writes the contract, which
	// reads the house -- a command, not something done while building a
	// model.
	pending string
	// instructions is the handoff, kept because the planner is given it and
	// the planner may be run more than once.
	instructions string
	// said is what the last look, or the last planner run, reported.
	said string
	// running is the planner working in the background.
	running bool
	// output is the tail of what the planner said. An agent explaining why it
	// could not read a receipt is the most useful thing on the screen at that
	// moment, so it is shown rather than swallowed.
	output []string
}

type intakePhase int

const (
	intakeChoosing intakePhase = iota
	intakeNaming
	intakeFilling
)

// newRow is the key of the "start a new one" row. Negative, because every
// real row is keyed by its index and a sentinel that could collide with one
// would open a folder somebody did not choose.
const newRow = -1

// ImportWalk is `hms import`: the interface, with the walk already open.
//
// On a Model that does not exist yet, like tui.Import -- so a broken imports
// directory fails as a message on the command line rather than as a blank
// screen. A name goes straight to that folder; without one the list opens,
// which is the answer to "what was I in the middle of".
func ImportWalk(ctx context.Context, ctrl app.Controller, name string) (Model, error) {
	m := New(ctx, ctrl)
	root, err := m.importsRoot()
	if err != nil {
		return Model{}, err
	}
	found, err := intake.Survey(root)
	if err != nil {
		return Model{}, err
	}
	in := &intaking{phase: intakeChoosing, root: root, found: found}
	in.refresh(100)
	m = m.open(in)
	if strings.TrimSpace(name) != "" {
		clean, err := intake.CleanName(name)
		if err != nil {
			return Model{}, err
		}
		// The folder itself is opened once the program is running, because
		// opening it writes the contract -- and writing the contract needs to
		// read the house, which is a command rather than something done while
		// building a model.
		in.phase, in.pending = intakeFilling, clean
	}
	return m, nil
}

// openIntake lists what is there. It reads the filesystem, so it is a command
// rather than something done while handling a keystroke.
func (m Model) openIntake() (Model, tea.Cmd) {
	if m.top() != nil {
		return m.refuse("finish what is open first"), nil
	}
	m.say = m.say.Working("looking for imports ...")
	return m, m.surveyImports()
}

func (m Model) surveyImports() tea.Cmd {
	return func() tea.Msg {
		root, err := m.importsRoot()
		if err != nil {
			return intakeMsg{err: err}
		}
		found, err := intake.Survey(root)
		if err != nil {
			return intakeMsg{err: err}
		}
		return intakeMsg{root: root, found: found}
	}
}

// intakeMsg carries the survey.
type intakeMsg struct {
	root  string
	found []intake.Status
	err   error
}

// showIntake puts the list up.
func (m Model) showIntake(msg intakeMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		return m.refuse("%v", humanise(msg.err.Error())), nil
	}
	in := &intaking{phase: intakeChoosing, root: msg.root, found: msg.found}
	in.refresh(m.width)
	// Already open: a rescan replaces the list without reopening the drawer,
	// so the cursor does not jump and the region does not flicker shut.
	if was, ok := m.top().(*intaking); ok {
		in.phase, in.at, in.instructions, in.said = was.phase, was.at, was.instructions, was.said
		in.output, in.running = was.output, was.running
		m.drawers[len(m.drawers)-1] = in
		return m, nil
	}
	return m.open(in), nil
}

// refresh rebuilds the list of folders.
func (in *intaking) refresh(width int) {
	rows := make([]table.Row, 0, len(in.found)+1)
	for i, s := range in.found {
		tone := table.ToneAttention
		what := s.State.String()
		switch s.State {
		case intake.Ready:
			tone = table.ToneGood
			what = fmt.Sprintf("%s to review", rowsPhrase(s.Rows))
		case intake.NeedsPlan:
			what = fmt.Sprintf("%s in, waiting for a plan", filesPhrase(len(s.Input)))
		}
		rows = append(rows, table.Row{
			Key: int64(i), Tone: tone,
			Cells: []string{s.Workspace.Name, what},
		})
	}
	// Last, because it is the least common thing to want: most of the time
	// you are coming back to one you started.
	rows = append(rows, table.Row{
		Key:   newRow,
		Cells: []string{"start a new import", "names a folder and writes the contract into it"},
	})
	in.tbl = table.New([]table.Column{
		{Title: "IMPORT", Min: 12, Grow: true},
		{Title: "WHAT IT NEEDS", Min: 18, Drop: 2},
	}).Fixed().SetRows(rows).SetSize(width, len(rows)+2)
}

// chooseImport acts on whatever the cursor is on.
func (m Model) chooseImport(in *intaking) (Model, tea.Cmd) {
	row, ok := in.tbl.Current()
	if !ok {
		return m, nil
	}
	if row.Key == newRow {
		in.phase = intakeNaming
		m.editor = m.editor.
			OpenFor(editor.ImportName, "", 0, "what shall this import be called?", "").
			WithVerb("import").SetWidth(m.width)
		return m, nil
	}
	if int(row.Key) >= len(in.found) {
		return m, nil
	}
	chosen := in.found[row.Key]
	// A ready one goes straight to the review, because that is what choosing
	// it meant. Anything else opens the folder it is waiting on.
	if chosen.State == intake.Ready {
		return m.close().reviewFile(chosen.Plan)
	}
	return m.fill(in, chosen.Workspace.Name)
}

// namedImport takes the name the field came back with.
func (m Model) namedImport(in *intaking, name string) (Model, tea.Cmd) {
	clean, err := intake.CleanName(name)
	if err != nil {
		return m.refuse("%v", humanise(err.Error())), nil
	}
	return m.fill(in, clean)
}

// fill opens a folder: making it if it is new, writing the contract into it
// either way, and showing what it is waiting for.
func (m Model) fill(in *intaking, name string) (Model, tea.Cmd) {
	in.phase = intakeFilling
	m.say = m.say.Working("opening " + name + " ...")
	return m, func() tea.Msg {
		// The contract is built HERE, against the house as it is at this
		// moment, rather than held on the model from startup. That is the
		// whole reason the folder carries one: a plan written against a
		// contract from last month describes a house that no longer exists.
		house, err := app.House(m.ctx, m.ctrl)
		if err != nil {
			return intakeOpenedMsg{err: err}
		}
		w, instructions, resumed, err := intake.Start(in.root, name,
			command.NewSchema().WithHouse(house))
		if err != nil {
			return intakeOpenedMsg{err: err}
		}
		return intakeOpenedMsg{
			at: intake.Look(w), instructions: instructions, resumed: resumed,
		}
	}
}

// intakeOpenedMsg carries an opened folder.
type intakeOpenedMsg struct {
	at           intake.Status
	instructions string
	resumed      bool
	err          error
}

// opened records the folder and says what state it arrived in.
func (m Model) opened(in *intaking, msg intakeOpenedMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		return m.refuse("%v", humanise(msg.err.Error())), nil
	}
	in.at, in.instructions = msg.at, msg.instructions
	in.phase = intakeFilling
	in.output = nil
	verb := "created"
	if msg.resumed {
		verb = "resumed"
	}
	// The folder is already in the heading, so this says only which of the two
	// happened -- which is the thing a person cannot see for themselves.
	in.said = verb
	m.say = m.say.Clear()
	// Ready already -- a plan was sitting there from yesterday. OFFERED
	// rather than opened: naming an import the morning after is as likely to
	// mean "the receipt I forgot" as "show me yesterday's plan".
	if msg.at.State == intake.Ready {
		in.said = fmt.Sprintf("a plan is already here (%s) -- %s reviews it",
			shortName(msg.at.Plan), keys.Show(keys.Intake, keys.Confirm))
	}
	return m, nil
}

// lookAgain re-reads the folder, which is what the pause is answered with.
func (m Model) lookAgain(in *intaking) (Model, tea.Cmd) {
	if in.phase != intakeFilling {
		return m, m.surveyImports()
	}
	was := in.at.State
	in.at = intake.Look(in.at.Workspace)
	switch {
	case in.at.State == intake.Ready:
		in.said = fmt.Sprintf("%s to review -- %s opens it",
			rowsPhrase(in.at.Rows), keys.Show(keys.Intake, keys.Confirm))
	case in.at.State != was:
		in.said = filesPhrase(len(in.at.Input)) + " in: " + strings.Join(in.at.Input, ", ")
	default:
		in.said = "nothing new yet"
	}
	return m, nil
}

// runPlanner hands the folder to whatever is configured to read it.
//
// A background job, not a blocking call. It is a shell command somebody
// configured to read a photograph, so it takes as long as it takes -- and a
// program frozen for the duration could not show the house the plan is being
// written against, which is the one thing it is for.
func (m Model) runPlanner(in *intaking) (Model, tea.Cmd) {
	if !m.hasPlanner() {
		return m.refuse("no planner is configured -- set %q in %s, or write the plan yourself",
			"import_planner", m.plannerHint), nil
	}
	if in.running {
		return m.refuse("the planner is already running"), nil
	}
	in.running = true
	in.output = nil
	in.said = "reading input/ with " + m.plannerDescribed()
	w, instructions := in.at.Workspace, in.instructions
	return m, func() tea.Msg {
		// Captured rather than written to stdout, because stdout is the
		// screen now. The tail of it is shown: an agent explaining why it
		// could not read a receipt is the most useful thing on the screen at
		// that moment, and swallowing it entirely was never an option.
		var out strings.Builder
		planner := intake.NewShell(m.plannerCommand, &out)
		err := planner.Plan(m.ctx, w, instructions)
		return plannedMsg{output: out.String(), err: err}
	}
}

// hasPlanner reports a planner being configured, which is the uncommon case.
func (m Model) hasPlanner() bool { return strings.TrimSpace(m.plannerCommand) != "" }

// plannerDescribed names the command, shortened, so the screen says what it
// is about to run before it runs it.
func (m Model) plannerDescribed() string {
	return intake.Shell{Command: m.plannerCommand}.Describe()
}

// importsRoot is the imports directory.
func (m Model) importsRoot() (string, error) { return config.ImportsDir() }

// reviewFile opens a plan file in the drawer, on the house already loaded.
//
// tui.Import builds a whole Model because it is called before the terminal
// exists. Here one is already running, so only the flow is replaced -- which
// is the point: the house you were looking at is the house the plan is
// reviewed against.
func (m Model) reviewFile(path string) (Model, tea.Cmd) {
	flow, err := m.flowFor(path)
	if err != nil {
		return m.refuse("%v", err), nil
	}
	m = m.withFlow(flow)
	m, _ = m.steer()
	return m, m.load(viewShell)
}

// plannedMsg reports the planner having finished, however it finished.
type plannedMsg struct {
	output string
	err    error
}

// planned takes what the planner left behind.
//
// A failure does NOT end the import. It leaves the workflow exactly where it
// would have been without a planner at all, which is recoverable: the folder
// and the contract are still there and somebody can still do that step by
// hand. Ending the import here would throw both away over one step.
func (m Model) planned(in *intaking, msg plannedMsg) (Model, tea.Cmd) {
	in.running = false
	in.output = tailLines(msg.output, 4)
	in.at = intake.Look(in.at.Workspace)
	switch {
	case msg.err != nil:
		in.said = "the planner failed -- the folder is still here, and the plan can be written by hand"
		m.say = m.say.Refuse(humanise(msg.err.Error()))
	case in.at.State == intake.Ready:
		in.said = fmt.Sprintf("wrote %s: %s to review -- %s opens it",
			shortName(in.at.Plan), rowsPhrase(in.at.Rows), keys.Show(keys.Intake, keys.Confirm))
	default:
		in.said = "the planner wrote no plan hms can read"
	}
	return m, nil
}

// tailLines is the last few lines of something, for a region that has room
// for a few. The END, because a command that failed says why last.
func tailLines(out string, most int) []string {
	var kept []string
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			kept = append(kept, line)
		}
	}
	if len(kept) > most {
		kept = kept[len(kept)-most:]
	}
	return kept
}

// filesPhrase counts files in English.
func filesPhrase(n int) string {
	if n == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", n)
}

// shortName is a file's own name, not the path to it. The directory is mostly
// the import's name again, which filled the line and said nothing.
func shortName(path string) string {
	if at := strings.LastIndex(path, "/"); at >= 0 {
		return path[at+1:]
	}
	return path
}

// shorten writes a path under the home directory with a tilde, the way a
// person would say it.
//
// An absolute path to an import ran to 84 columns on an 80-column screen for
// an ordinary home directory, and a line wider than the screen wraps -- which
// shifts every row above it.
func shorten(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if rest, ok := strings.CutPrefix(path, home); ok {
		return "~" + rest
	}
	return path
}

// ---------------------------------------------------------------------------
// The walk as a drawer
// ---------------------------------------------------------------------------

func (in *intaking) name() string {
	switch in.phase {
	case intakeNaming:
		return "naming an import"
	case intakeFilling:
		return "filling an import"
	}
	return "imports"
}

func (in *intaking) height(Model) int {
	if in.phase == intakeChoosing {
		return min(len(in.found)+3, 12) + 1
	}
	// The three paths, what is in the folder, and what was last said -- plus
	// whatever the planner had to say for itself.
	return 7 + len(in.output)
}

func (in *intaking) lines(m Model) []string {
	if in.phase == intakeChoosing {
		folders := " folders"
		if len(in.found) == 1 {
			folders = " folder"
		}
		head := style.Dim.Render("IMPORT  ") +
			style.Strong.Render(fmt.Sprintf("%d", len(in.found))) +
			style.Dim.Render(folders)
		return append([]string{head},
			lines(in.tbl.SetSize(m.width, in.height(m)-1).View())...)
	}

	w := in.at.Workspace
	head := style.Dim.Render("IMPORT  ") + style.Strong.Render(w.Name) +
		style.Dim.Render("   "+shorten(w.Dir))
	if in.running {
		head += style.Warn.Render("   reading ...")
	}
	// The folder once, in the heading, and then the three places INSIDE it by
	// name. Writing each path out in full put three copies of the same
	// directory on screen and ran every one of them past the terminal -- and
	// the part that differs, which is the only part being pointed at, was the
	// last thing on each line.
	out := []string{
		head,
		"  " + style.Dim.Render("put what you want imported into ") + intake.InputDirName + "/",
		"  " + style.Dim.Render("the plan comes back at          ") +
			intake.PlanDirName + "/" + intake.PlanFileName,
		"  " + style.Dim.Render("hand this to whatever writes it  ") +
			intake.SchemaDirName + "/" + intake.HandoffName,
		"",
	}
	if in.said != "" {
		out = append(out, "  "+style.Focus.Render(in.said))
	} else {
		out = append(out, "")
	}
	for _, line := range in.output {
		out = append(out, "    "+style.Dim.Render(line))
	}
	return out
}

func (in *intaking) facts(Model) []string {
	if in.phase == intakeChoosing {
		var ready int
		for _, s := range in.found {
			if s.State == intake.Ready {
				ready++
			}
		}
		return []string{
			style.Ready.Render(fmt.Sprintf("%d ready to review", ready)),
			style.Dim.Render(fmt.Sprintf("%d in all", len(in.found))),
		}
	}
	return []string{
		style.Strong.Render(in.at.State.String()),
		style.Dim.Render(filesPhrase(len(in.at.Input)) + " in input/"),
	}
}

func (in *intaking) keys(m Model) string {
	if in.phase == intakeChoosing {
		return keys.Hint(keys.Intake,
			[]keys.Action{keys.Confirm}, []keys.Action{keys.Cancel})
	}
	groups := [][]keys.Action{
		{keys.Refresh},
	}
	if m.hasPlanner() {
		groups = append(groups, []keys.Action{keys.RunPlanner})
	}
	if in.at.State == intake.Ready {
		groups = append([][]keys.Action{{keys.Confirm}}, groups...)
	}
	return keys.Hint(keys.Intake, append(groups, []keys.Action{keys.Cancel})...)
}

func (in *intaking) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch keys.Lookup(keys.Intake, msg) {
	case keys.Cancel, keys.Quit:
		if in.phase == intakeFilling {
			// Back to the list rather than out of the drawer. Leaving an
			// import is a different intention from leaving the folder you
			// were looking at, and one esc should not do both.
			in.phase = intakeChoosing
			in.said = ""
			return m, m.surveyImports()
		}
		return m.close(), nil
	case keys.Confirm:
		if in.phase == intakeChoosing {
			return m.chooseImport(in)
		}
		if in.at.State == intake.Ready {
			return m.close().reviewFile(in.at.Plan)
		}
		return m.refuse("there is no plan in %s yet", in.at.Workspace.PlanDir()), nil
	case keys.Refresh:
		return m.lookAgain(in)
	case keys.RunPlanner:
		if in.phase != intakeFilling {
			return m, nil
		}
		return m.runPlanner(in)
	}
	if in.phase == intakeChoosing {
		next, _ := in.tbl.Update(msg)
		in.tbl = next
	}
	return m, nil
}
