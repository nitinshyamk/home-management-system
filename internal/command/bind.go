package command

import (
	"fmt"
	"strings"
	"time"

	"home-management-system/internal/domain"
	"home-management-system/internal/resolve"
)

// Bind is the single boundary between untrusted text and the domain -- the role
// hydrateEvent plays for storage. Everything downstream is typed.
//
// Its result is binary, and that binary IS the plan screen:
//
//	ready ⟺ Command != nil
//
// A row that became a Command needs no confirmation because there is nothing
// left to decide; a row that did not carries exactly the Issues explaining why.
// The three row states are not a UI invention layered on top -- they are the
// shape of this result. Which also means ops.Plan never reasons about
// ambiguity: by the time it runs there is none.

// BindResult is one row's outcome.
type BindResult struct {
	// Command is non-nil if and only if every reference resolved and every
	// value parsed.
	Command Command
	// Issues say why not, one per field, in field order so a review screen
	// reads top to bottom.
	Issues []Issue
}

// Ready reports whether there is nothing left to decide.
func (r BindResult) Ready() bool { return r.Command != nil }

// Issue is one field that could not be settled.
type Issue struct {
	Field string
	// Outcome is the resolver's verdict when the field was a name: Suggested
	// needs one keystroke, Ambiguous and Missing block. It is nil when the
	// problem was the value rather than the reference.
	Outcome resolve.Outcome
	// Problem describes a value that could not be read, or a required field
	// that was not given.
	Problem string
}

func (i Issue) String() string {
	if i.Problem != "" {
		return i.Field + ": " + Humanise(i.Problem)
	}
	// The wording belongs to resolve, which is where the verdict is made. This
	// adds the field it was made about, which is the only thing an Issue knows
	// that the outcome does not.
	return i.Field + ": " + resolve.Describe(i.Outcome)
}

// Humanise strips the package prefixes a Go error accumulates on the way up.
//
// "command: cannot read value: \"lots\" has no number in it" is a sentence
// wearing a call stack. The layers are useful in a log and are noise to a
// person, who wants the last clause -- the one that says what actually
// happened.
//
// It lives here rather than in the interface because an Issue is already the
// thing a person reads: an importer, a plan screen, and a command line all
// render these, and three copies of this list would drift.
func Humanise(text string) string {
	for _, prefix := range []string{
		"ops: ", "command: ", "app: ", "tui: ", "importer: ",
		"ledger: ", "origin: ", "annotate: ", "query: ", "db: ",
		"invalid request: ", "cannot read line: ", "cannot read value: ",
	} {
		for strings.HasPrefix(text, prefix) {
			text = strings.TrimPrefix(text, prefix)
		}
	}
	return text
}

// Bind turns one RawCommand into a Command, or into the reasons it could not.
//
// An error is returned only for a raw command that is not a command at all --
// an unknown op. Everything a person could reasonably have meant comes back as
// Issues instead, because a plan screen can act on those and cannot act on an
// error.
func Bind(v *Vocabulary, raw RawCommand) (BindResult, error) {
	op := Op(strings.ToLower(strings.TrimSpace(raw.Op)))
	spec, ok := SpecOf(op)
	if !ok {
		return BindResult{}, fmt.Errorf("%w: %q is not a command", ErrSyntax, raw.Op)
	}

	b := binding{v: v, spec: spec, raw: raw}
	cmd := b.build(op)
	if len(b.issues) > 0 {
		return BindResult{Issues: b.issues}, nil
	}
	return BindResult{Command: cmd}, nil
}

// binding accumulates issues as it reads fields, so one pass reports EVERY
// problem with a row rather than the first. A person fixing a receipt row wants
// to see all of it at once.
type binding struct {
	v      *Vocabulary
	spec   Spec
	raw    RawCommand
	issues []Issue
}

func (b *binding) fail(field, problem string, args ...any) {
	b.issues = append(b.issues, Issue{Field: field, Problem: fmt.Sprintf(problem, args...)})
}

// text returns a field's raw value, reporting a required field that is absent.
func (b *binding) text(key string) (string, bool) {
	f, ok := b.spec.Field(key)
	if !ok {
		return "", false
	}
	s := strings.TrimSpace(b.raw.Fields[key])
	if s == "" {
		if f.Required {
			b.fail(key, "is required (%s)", f.What)
		}
		return "", false
	}
	return s, true
}

// ref resolves a name to an identifier, recording the resolver's verdict when
// it is anything but certain.
//
// Suggested is an issue rather than an answer, and that is the whole design: a
// wrong Suggested is far worse than a block, because a block stops and asks
// while a wrong Suggested quietly files the receipt against the wrong thing.
func (b *binding) ref(key string) (resolve.Candidate, bool) {
	f, ok := b.spec.Field(key)
	if !ok {
		return resolve.Candidate{}, false
	}
	s, ok := b.text(key)
	if !ok {
		return resolve.Candidate{}, false
	}
	outcome := b.v.Names.Resolve(s, f.Kinds...)
	if exact, ok := outcome.(resolve.Exact); ok {
		return exact.Candidate, true
	}
	b.issues = append(b.issues, Issue{Field: key, Outcome: outcome})
	return resolve.Candidate{}, false
}

func (b *binding) item(key string) (domain.ItemID, bool) {
	c, ok := b.ref(key)
	return domain.ItemID(c.ID), ok
}

func (b *binding) location(key string) (domain.LocationID, bool) {
	c, ok := b.ref(key)
	return domain.LocationID(c.ID), ok
}

func (b *binding) category(key string) (domain.CategoryID, bool) {
	c, ok := b.ref(key)
	return domain.CategoryID(c.ID), ok
}

func (b *binding) holding(key string) (domain.HoldingID, bool) {
	c, ok := b.ref(key)
	return domain.HoldingID(c.ID), ok
}

func (b *binding) optionalLocation(key string) *domain.LocationID {
	if _, given := b.raw.Fields[key]; !given || strings.TrimSpace(b.raw.Fields[key]) == "" {
		return nil
	}
	if id, ok := b.location(key); ok {
		return &id
	}
	return nil
}

func (b *binding) optionalCategory(key string) *domain.CategoryID {
	if _, given := b.raw.Fields[key]; !given || strings.TrimSpace(b.raw.Fields[key]) == "" {
		return nil
	}
	if id, ok := b.category(key); ok {
		return &id
	}
	return nil
}

// where resolves the `at` field, which disambiguates when an Item is kept in
// several PLACES.
//
// Places, not Holdings, and the distinction is the whole of this function. One
// Item in one place is routinely two Holdings -- a sealed bag and an opened one
// -- and that is not an ambiguity about where anything is. Counting Holdings
// here made `consume rice 100` refuse a perfectly ordinary pantry with "it is
// kept in 2 places (Left Pantry, Left Pantry)", which is the shape of the most
// common command in the system failing on its most common input.
//
// Omitted with exactly one place, it resolves; with several it is an issue and
// blocks. Guessing would be the single most damaging thing this layer could do
// -- stock removed from the wrong shelf is invisible until someone looks.
func (b *binding) where(item domain.ItemID, itemOK bool) (domain.LocationID, bool) {
	if s := strings.TrimSpace(b.raw.Fields["at"]); s != "" {
		return b.location("at")
	}
	if !itemOK {
		return 0, false
	}

	var places []domain.LocationID
	seen := map[domain.LocationID]bool{}
	for _, h := range b.v.liveHoldings(item) {
		if !seen[h.Location] {
			seen[h.Location] = true
			places = append(places, h.Location)
		}
	}

	switch len(places) {
	case 0:
		b.fail("at", "%s is not kept anywhere yet, so say where this is",
			b.v.Names.Label(domain.EntityItem, int64(item)))
		return 0, false
	case 1:
		return places[0], true
	}
	var names []string
	for _, id := range places {
		names = append(names, b.v.Names.Label(domain.EntityLocation, int64(id)))
	}
	b.fail("at", "it is kept in %d places (%s), so say which", len(places), strings.Join(names, ", "))
	return 0, false
}

// holdingOf picks the Holding an Item has at a place, narrowed by `basis` when
// one was given.
//
// This is how the commands that act on ONE Holding reach it without a person
// naming a Holding directly -- which they would struggle to do, since a Holding
// has no name of its own.
func (b *binding) holdingOf(item domain.ItemID, at domain.LocationID, ok bool) (domain.HoldingID, bool) {
	if !ok {
		return 0, false
	}
	want, narrowed := b.basis()
	var found []HoldingFacts
	for _, h := range b.v.liveHoldings(item) {
		if h.Location != at {
			continue
		}
		if narrowed && h.Basis != want {
			continue
		}
		found = append(found, h)
	}
	switch len(found) {
	case 0:
		b.fail("at", "%s is not kept at %s",
			b.v.Names.Label(domain.EntityItem, int64(item)),
			b.v.Names.Label(domain.EntityLocation, int64(at)))
		return 0, false
	case 1:
		return found[0].ID, true
	}
	// Same item, same place, several Holdings: they differ by basis or expiry,
	// and only a person can say which is meant. Unlike where(), this one really
	// is ambiguous -- throwing away from the sealed bag and from the open one
	// are different acts.
	//
	// The message names what to type. A message that says "say which" without
	// saying how is the state this command was in before `basis` existed.
	var which []string
	for _, h := range found {
		which = append(which, b.v.Names.Label(domain.EntityHolding, int64(h.ID)))
	}
	b.fail("basis", "%s at %s is %s -- say `basis sealed` or `basis loose`",
		b.v.Names.Label(domain.EntityItem, int64(item)),
		b.v.Names.Label(domain.EntityLocation, int64(at)),
		strings.Join(which, " and "))
	return 0, false
}

// amount reads a quantity and resolves it against the Item it is about.
//
// This is where the three spellings become one number, and where H7 surfaces at
// the input boundary: `2bag` against an item with no package size is rejected
// here rather than by the schema, which is the difference between a clear
// message and a constraint violation.
func (b *binding) amount(key string, item domain.ItemID, itemOK bool) (domain.Quantity, domain.UnitBasis, bool) {
	s, ok := b.text(key)
	if !ok {
		return domain.Zero, "", false
	}
	written, err := ParseAmount(s)
	if err != nil {
		b.fail(key, "%v", err)
		return domain.Zero, "", false
	}
	if !itemOK {
		// Without the Item there is nothing to interpret it against, and the
		// item field has already reported why.
		return domain.Zero, "", false
	}

	it, known := b.v.item(item)
	if !known {
		b.fail(key, "nothing is known about that item's units")
		return domain.Zero, "", false
	}
	bulk, measured := it.(domain.BulkItem)
	if !measured {
		if written.Unit != "" || written.Packages {
			b.fail(key, "%s is one of a kind, so %q means nothing", bulk.Name, s)
			return domain.Zero, "", false
		}
		return written.Value, "", true
	}

	switch {
	case written.Packages:
		// H7 at the input boundary.
		if bulk.PackageSize == nil {
			b.fail(key, "%s has no package size, so it cannot be counted in packages", bulk.Name)
			return domain.Zero, "", false
		}
		return written.Value, domain.BasisPackage, true

	case written.Unit == "" || written.Unit == bulk.ContentUnit:
		return written.Value, domain.BasisContent, true
	}

	from, err := b.v.unit(written.Unit)
	if err != nil {
		b.fail(key, "%v", err)
		return domain.Zero, "", false
	}
	to, err := b.v.unit(bulk.ContentUnit)
	if err != nil {
		b.fail(key, "%v", err)
		return domain.Zero, "", false
	}
	// Rejected if inexact rather than rounded: every quantity here is exact, and
	// H10 compares replayed state to stored state for equality.
	converted, err := domain.Convert(written.Value, from, to)
	if err != nil {
		b.fail(key, "%v", err)
		return domain.Zero, "", false
	}
	return converted, domain.BasisContent, true
}

func (b *binding) date(key string) (*time.Time, bool) {
	s, ok := b.text(key)
	if !ok {
		return nil, true // absent is not an error unless the field is required
	}
	t, err := ParseDate(s)
	if err != nil {
		b.fail(key, "%v", err)
		return nil, false
	}
	return &t, true
}

func (b *binding) money(key string) *int64 {
	s, ok := b.text(key)
	if !ok {
		return nil
	}
	cents, err := ParseMoney(s)
	if err != nil {
		b.fail(key, "%v", err)
		return nil
	}
	return &cents
}

// basis reads the sealed/loose narrowing, reporting whether one was given at
// all -- absent is not a default, it is "do not narrow".
func (b *binding) basis() (domain.UnitBasis, bool) {
	s, ok := b.text("basis")
	if !ok {
		return "", false
	}
	switch strings.ToLower(s) {
	case "sealed", "package", "packages":
		return domain.BasisPackage, true
	case "loose", "content", "contents", "open", "opened":
		return domain.BasisContent, true
	}
	b.fail("basis", "%q is not sealed or loose", s)
	return "", false
}

func (b *binding) plain(key string) string {
	s, _ := b.text(key)
	return s
}

func (b *binding) target(key string) Target {
	c, ok := b.ref(key)
	if !ok {
		return Target{}
	}
	return Target{Kind: c.Kind, ID: c.ID}
}
