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
	entry.Creates = creations(result.Issues)
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
func creations(issues []command.Issue) []Creation {
	var out []Creation
	for _, issue := range issues {
		missing, ok := issue.Outcome.(resolve.Missing)
		if !ok {
			return nil
		}
		kinds := fieldKinds(issue.Field)
		if len(kinds) != 1 {
			// A field that could mean several kinds cannot be created from,
			// because nothing says WHICH to make.
			return nil
		}
		out = append(out, Creation{Kind: kinds[0], Name: missing.Query})
	}
	return out
}

// fieldKinds is what a named field refers to, looked up wherever it appears.
//
// By field NAME across the whole vocabulary rather than per op, because the
// issue does not carry its op -- and `at` means a Location in every command
// that has one, which is a property worth relying on rather than working
// around.
func fieldKinds(name string) []resolve.Kind {
	for _, spec := range command.Specs() {
		if field, ok := spec.Field(name); ok && field.Type == command.FieldName {
			return field.Kinds
		}
	}
	return nil
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

// Fixable is the field a person would edit to unstick a row, and what it
// currently holds.
//
// The FIRST issue's field: a row is settled one problem at a time, and the
// first is the one the reader is looking at. Reporting all of them and then
// editing an arbitrary one would be two different orders in one screen.
func (e Entry) Fixable() (field, value string, ok bool) {
	if len(e.Issues) == 0 {
		return "", "", false
	}
	field = e.Issues[0].Field
	// A multi-field issue -- unknown columns -- names them all at once and is
	// not one field to edit.
	if strings.Contains(field, ",") || field == "op" {
		return "", "", false
	}
	return field, e.Row.Raw.Fields[field], true
}

// Correct rewrites one field of a row, for a person fixing it in place.
//
// It writes the row's TEXT rather than reaching past it to an identifier, so
// the corrected row goes through Bind exactly as it would have if it had
// arrived that way. A fixed row and a right-first-time row must not take
// different paths, or only one of them is the path everything else is tested
// against.
func Correct(entry Entry, field, value string) Entry {
	if entry.Row.Raw.Fields == nil {
		entry.Row.Raw.Fields = map[string]string{}
	}
	if strings.TrimSpace(value) == "" {
		delete(entry.Row.Raw.Fields, field)
	} else {
		entry.Row.Raw.Fields[field] = strings.TrimSpace(value)
	}
	return entry
}

// FieldKind is what a named field refers to, or "" when it is free text.
// Exported so an editor can offer the right completions for it.
func FieldKind(name string) string {
	kinds := fieldKinds(name)
	if len(kinds) != 1 {
		return ""
	}
	return string(kinds[0])
}

// Settle re-binds one row after its text has been corrected.
func Settle(vocabulary *command.Vocabulary, entry Entry) Entry {
	return bindRow(vocabulary, entry.Row)
}
