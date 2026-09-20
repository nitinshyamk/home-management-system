package importer

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"home-management-system/internal/command"
	"home-management-system/internal/resolve"
)

// Binding a whole file at once.
//
// Once, against ONE picture of the world: every row of a receipt must be bound
// against the same vocabulary, or a row that resolved differently from the row
// above it would make an all-or-nothing import a lie about what it applied.

// State is what a row is waiting for. The three states are not a UI invention
// -- they are the shape of BindResult, which is binary.
type State int

const (
	// Ready became a Command. There is nothing left to decide.
	Ready State = iota
	// Confirmable can proceed once a person says so: it would create
	// something, or it rests on a suggestion that must be accepted rather than
	// applied.
	Confirmable
	// Blocked cannot proceed at all. Only a person can settle it.
	Blocked
	// Dropped was set aside deliberately.
	Dropped
)

func (s State) String() string {
	switch s {
	case Ready:
		return "ready"
	case Confirmable:
		return "needs confirmation"
	case Blocked:
		return "blocked"
	}
	return "dropped"
}

// Entry is one row of a file, bound.
type Entry struct {
	Row   Row
	State State

	// Command is non-nil once the row is Ready. A Confirmable row has none
	// yet: the thing it would create does not exist, so there is no identifier
	// to put in one.
	Command command.Command

	// Issues say what is left to settle, in order.
	Issues []command.Issue

	// Creates names what this row would bring into existence, so the screen can
	// say so and so two rows naming one new thing can be recognised as one.
	Creates []Creation
}

// Creation is a thing a row would make.
type Creation struct {
	Kind resolve.Kind
	Name string
}

// AsWritten renders a row that has not bound, in the words it was written in.
//
// It lives here rather than in the interface because RawCommand does not leave
// this package or internal/command -- the boundary archlint keeps, so that a
// keystroke holding an identifier can never be round-tripped through a name.
//
// The row's own words are what a person will be correcting, which makes them
// the right thing to show when there are no identifiers to render names from.
func (e Entry) AsWritten() string {
	spec, ok := command.SpecOf(command.Op(strings.ToLower(e.Row.Raw.Op)))
	if !ok {
		return e.Row.Raw.Op + " ?"
	}
	parts := []string{e.Row.Raw.Op}
	for _, field := range spec.Fields {
		if value := strings.TrimSpace(e.Row.Raw.Fields[field.Key]); value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, " ")
}

// Plan is a whole file, bound.
type Plan struct {
	Entries []Entry
	// Source is the file, for the heading.
	Source string
}

// Counts reports the arithmetic that has to add up to the file.
//
// A row that vanished from this is a row that will surprise someone, which is
// why it is computed from the entries rather than tracked alongside them.
func (p Plan) Counts() (ready, confirmable, blocked, dropped int) {
	for _, e := range p.Entries {
		switch e.State {
		case Ready:
			ready++
		case Confirmable:
			confirmable++
		case Blocked:
			blocked++
		case Dropped:
			dropped++
		}
	}
	return
}

// Applicable reports whether every row is settled one way or the other.
//
// All-or-nothing: a half-applied receipt is the worst outcome, because some of
// it happened, you do not know which, and the file no longer describes the
// house.
func (p Plan) Applicable() bool {
	ready, confirmable, blocked, _ := p.Counts()
	return blocked == 0 && confirmable == 0 && ready > 0
}

// ApplicableInPasses is the other rule, and the two belong side by side
// because the difference between them is the difference between a receipt and a
// classification.
//
// Applicable, above, is the receipt's: every row settled one way or the other,
// or none of it applies. This one is the tree's. A tree is built from the top
// down -- a row filed under a category that another row of the same file
// creates cannot bind until that row has been APPLIED, because a Command holds
// an identifier and the category does not have one yet. A stage that insisted
// on settling every row before applying any of them could therefore never
// apply a proposal two levels deep.
//
// So the ready rows apply now, as one transaction, and the rest stay on the
// stage for another pass. Nothing is weakened by it: a category's creation does
// not depend on its siblings', which is exactly why the receipt's rule cannot
// be relaxed the same way -- two rows of a receipt about one item depend on
// each other completely.
func (p Plan) ApplicableInPasses() bool {
	ready, _, blocked, _ := p.Counts()
	return blocked == 0 && ready > 0
}

// WhyNotInPasses says what is stopping a pass.
func (p Plan) WhyNotInPasses() string {
	ready, _, blocked, _ := p.Counts()
	switch {
	case blocked > 0:
		return fmt.Sprintf("%d %s blocked", blocked, plural(blocked, "row is", "rows are"))
	case ready == 0:
		return "no row is ready yet"
	}
	return ""
}

// Unapplied is what a pass would leave behind: the rows that are neither ready
// nor deliberately set aside.
//
// The rows themselves, so the stage that comes back is about the same file and
// still says which line each row was -- a remaining row renumbered from 1 would
// be a row a person cannot find in the file they wrote.
func (p Plan) Unapplied() Plan {
	out := Plan{Source: p.Source}
	for _, entry := range p.Entries {
		if entry.State == Ready || entry.State == Dropped {
			continue
		}
		out.Entries = append(out.Entries, entry)
	}
	return out
}

// Why says what is stopping an apply, for a refusal that is a reason rather
// than a key that does nothing.
func (p Plan) Why() string {
	ready, confirmable, blocked, _ := p.Counts()
	switch {
	case blocked > 0 && confirmable > 0:
		return fmt.Sprintf("%d rows are blocked and %d need confirming", blocked, confirmable)
	case blocked > 0:
		return fmt.Sprintf("%d %s blocked", blocked, plural(blocked, "row is", "rows are"))
	case confirmable > 0:
		return fmt.Sprintf("%d %s confirming", confirmable, plural(confirmable, "row needs", "rows need"))
	case ready == 0:
		return "nothing left to apply"
	}
	return ""
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// Commands are the Commands of every ready row, in file order.
func (p Plan) Commands() []command.Command {
	var out []command.Command
	for _, e := range p.Entries {
		if e.State == Ready && e.Command != nil {
			out = append(out, e.Command)
		}
	}
	return out
}

// Bind binds every row of a file against one vocabulary.
func Bind(ctx context.Context, vocabulary *command.Vocabulary, source string, rows []Row) Plan {
	plan := Plan{Source: source}
	for _, row := range rows {
		plan.Entries = append(plan.Entries, bindRow(vocabulary, row))
	}
	return plan
}

// bindRow settles one row as far as it can go on its own.
func bindRow(vocabulary *command.Vocabulary, row Row) Entry {
	entry := Entry{Row: row, State: Blocked}

	if len(row.Unknown) > 0 {
		// A column the op has no field for did something, or was meant to.
		// Either way the row does not say what it looks like it says.
		entry.Issues = append(entry.Issues, command.Issue{
			Field:   strings.Join(row.Unknown, ", "),
			Problem: "no such field for " + row.Raw.Op,
		})
		return entry
	}

	spec, known := command.SpecOf(command.Op(strings.ToLower(row.Raw.Op)))
	result, err := command.Bind(vocabulary, row.Raw)
	if err != nil {
		entry.Issues = append(entry.Issues, command.Issue{Field: "op", Problem: err.Error()})
		return entry
	}

	if result.Ready() {
		entry.Command = result.Command
		entry.State = Ready
		// A `new …` row binds perfectly well and still needs a person, because
		// creation is never silent. The op says so; nothing about the bind
		// result could.
		if known && spec.Creates != "" {
			entry.State = Confirmable
			entry.Creates = []Creation{{Kind: spec.Creates, Name: createdName(spec, row.Raw)}}
		}
		return entry
	}

	entry.Issues = result.Issues
	// A row is Confirmable when every issue is one a person can settle from
	// this screen: a suggestion to accept, or a name that is not there and
	// could be made. Anything else needs the row edited or dropped.
	entry.Creates = creations(spec, result.Issues)
	if len(entry.Creates) > 0 || allSuggested(result.Issues) {
		entry.State = Confirmable
	}
	return entry
}

// createdName is what a `new …` row would call the thing.
func createdName(spec command.Spec, raw command.RawCommand) string {
	for _, field := range spec.Fields {
		if field.Positional {
			return raw.Fields[field.Key]
		}
	}
	return ""
}

// allSuggested reports whether every issue is a suggestion waiting to be taken.
func allSuggested(issues []command.Issue) bool {
	for _, issue := range issues {
		if _, ok := issue.Outcome.(resolve.Suggested); !ok {
			return false
		}
	}
	return len(issues) > 0
}

// creations are the things a row would have to make for it to work.
//
// A Missing name is a thing that could be created -- the plan screen opens the
// creation panel for it, which collects the fields the row could not carry. An
// Ambiguous one could not: the name exists several times over, so making
// another is the last thing anybody wants.
//
// Every issue has to be creatable, or the row is blocked. A row with one
// creatable name and one genuinely broken field is not half-acceptable.
//
// What a field refers to is read off the ROW's own spec. An earlier version
// looked the field name up across the whole vocabulary, on the grounds that
// `at` means a Location in every command that has one -- but `under` does not
// work that way: it is a Category in `new category` and a Location in `new
// location`. The first spec in alphabetical order won, so every `new location
// … under Kitchen` whose parent did not exist yet said it would create a
// CATEGORY called Kitchen, opened the category panel for it, and left a row
// waiting on a sibling unable to recognise what it was waiting for.
func creations(spec command.Spec, issues []command.Issue) []Creation {
	var out []Creation
	for _, issue := range issues {
		missing, ok := issue.Outcome.(resolve.Missing)
		if !ok {
			return nil
		}
		field, ok := spec.Field(issue.Field)
		if !ok || field.Type != command.FieldName || len(field.Kinds) != 1 {
			// A field that could mean several kinds cannot be created from,
			// because nothing says WHICH to make.
			return nil
		}
		out = append(out, Creation{Kind: field.Kinds[0], Name: missing.Query})
	}
	return out
}

// MergeCreations reports the distinct things a plan would create.
//
// Two rows naming one new item create it ONCE. That is the property the whole
// design is arranged around -- a receipt cannot produce two Turmerics -- and it
// is settled here, where both rows are visible at the same time, rather than by
// hoping the operations layer notices.
func (p Plan) MergeCreations() []Creation {
	seen := map[Creation]bool{}
	var out []Creation
	for _, e := range p.Entries {
		if e.State == Dropped {
			continue
		}
		for _, c := range e.Creates {
			key := Creation{Kind: c.Kind, Name: strings.ToLower(c.Name)}
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Rebind restores a dropped row to whatever it was before it was dropped.
//
// Dropping is reversible right up until the apply, which is why it does not
// ask -- and why taking it back has to be possible without re-reading the file.
func Rebind(entry Entry) Entry {
	if entry.State != Dropped {
		return entry
	}
	switch {
	case entry.Command != nil && len(entry.Creates) == 0:
		entry.State = Ready
	case len(entry.Creates) > 0 || len(entry.Issues) > 0:
		entry.State = Confirmable
		if !allSuggested(entry.Issues) && len(entry.Creates) == 0 {
			entry.State = Blocked
		}
	default:
		entry.State = Blocked
	}
	return entry
}

// AcceptSuggestions rewrites a row with what the resolver suggested.
//
// It writes the suggestion into the ROW's text rather than reaching past it to
// the identifier, so the row still goes through Bind afterwards. Taking a
// shortcut here would mean an accepted suggestion followed a different path
// from a name that was right first time, and only one of those paths would be
// the one everything else is tested against.
func AcceptSuggestions(entry Entry) Entry {
	for _, issue := range entry.Issues {
		suggested, ok := issue.Outcome.(resolve.Suggested)
		if !ok {
			continue
		}
		entry.Row.Raw.Fields[issue.Field] = suggested.Candidate.Path
	}
	return entry
}

// AsLine renders a row as the command line a person edits.
//
// The whole row, not one field of it. A row on this screen is a COMMAND -- the
// same sentence a CSV column set and a typed `:` line both spell -- so editing
// it means editing that sentence. An earlier version opened only the field of
// the first issue, which meant `e` refused on every row that HAD no issue:
// every ready row, and every row whose only business was creating something.
// That is most of a typical receipt, and the key appeared broken.
func (e Entry) AsLine() string { return command.Line(e.Row.Raw) }

// Rewrite replaces a row's text with a re-parsed line.
//
// It writes the row's TEXT rather than reaching past it to an identifier, so
// the corrected row goes through Bind exactly as it would have if it had
// arrived that way. A fixed row and a right-first-time row must not take
// different paths, or only one of them is the path everything else is tested
// against.
func Rewrite(entry Entry, raw command.RawCommand) Entry {
	entry.Row.Raw = raw
	return entry
}

// Settle re-binds one row after its text has been corrected.
func Settle(vocabulary *command.Vocabulary, entry Entry) Entry {
	return bindRow(vocabulary, entry.Row)
}

// IsCreation reports whether the row IS a creation, as opposed to a row that
// would have to create something before it can mean anything.
//
// The difference decides what settling the row does, and it is not visible in
// Creates alone: `new category Spices` and `acquire Cardamom` both say they
// would create a category and an item respectively, but the first IS the
// creation -- it bound completely, it holds a Command, and agreeing to it is
// all that is left -- while the second cannot become a Command at all until the
// thing it names exists.
func (e Entry) IsCreation() bool {
	if e.Command == nil {
		return false
	}
	spec, ok := command.SpecOf(command.Op(strings.ToLower(strings.TrimSpace(e.Row.Raw.Op))))
	return ok && spec.Creates != ""
}

// ConfirmCreation agrees to a creation row, as written.
//
// Creation is never silent, and this is what makes it not silent: the row
// stands at "needs confirming" until a person says so, and then it is applied
// in the same transaction as everything else rather than off to one side. An
// earlier version opened the creation panel for these rows, which created the
// thing immediately -- outside the plan, in its own transaction -- and left the
// row still saying it would create one, so a file of `new category` rows could
// never be applied at all and every row of it created something twice if you
// tried.
func ConfirmCreation(entry Entry) (Entry, bool) {
	if entry.State != Confirmable || !entry.IsCreation() {
		return entry, false
	}
	entry.State = Ready
	return entry, true
}

// AlreadyCreatedBy reports the row that has already agreed to create the same
// thing this one would, if there is one.
//
// Two rows naming one new thing must create it ONCE -- the property the whole
// design is arranged around -- and for rows that ARE creations the plan is the
// only place that can see it: the domain permits two categories with one name
// (sibling uniqueness was never an integrity rule), so nothing below this will
// refuse a file that says `new category Spices` twice.
//
// Only against rows that are Ready, because those are the ones that would
// actually be applied. A second row still waiting to be confirmed conflicts
// with nothing yet, and refusing it then would mean the order you confirmed
// them in decided which row was the problem.
func (p Plan) AlreadyCreatedBy(at int) (int, bool) {
	return p.creatorOf(at, func(other Entry) bool { return other.State == Ready })
}

// WillBeCreatedBy reports the row that would bring into existence the thing
// this row is missing, whether or not it has been agreed to yet.
//
// It is what stops the plan screen offering to make something a row two lines
// up is already making. Opening the creation panel there made a SECOND category
// of the same name -- immediately, in its own transaction -- and left both rows
// still proposing one, which is the two-Turmerics failure wearing a hat.
func (p Plan) WillBeCreatedBy(at int) (int, bool) {
	return p.creatorOf(at, func(other Entry) bool { return other.State != Dropped })
}

// creatorOf finds a row that creates what the row at `at` names.
func (p Plan) creatorOf(at int, wanted func(Entry) bool) (int, bool) {
	if at < 0 || at >= len(p.Entries) {
		return 0, false
	}
	for _, creation := range p.Entries[at].Creates {
		for i, other := range p.Entries {
			if i == at || !other.IsCreation() || !wanted(other) {
				continue
			}
			for _, made := range other.Creates {
				if made.Kind == creation.Kind && strings.EqualFold(made.Name, creation.Name) {
					return i, true
				}
			}
		}
	}
	return 0, false
}
