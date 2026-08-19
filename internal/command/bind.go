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
		return i.Field + ": " + i.Problem
	}
	switch o := i.Outcome.(type) {
	case resolve.Suggested:
		return fmt.Sprintf("%s: did you mean %s? (%s)", i.Field, o.Candidate.Path, o.Basis)
	case resolve.Ambiguous:
		var names []string
		for _, c := range o.Candidates {
			names = append(names, c.Path)
		}
		return fmt.Sprintf("%s: could be %s", i.Field, strings.Join(names, ", "))
	case resolve.Missing:
		return fmt.Sprintf("%s: nothing called %q", i.Field, o.Query)
	}
	return i.Field + ": cannot be settled"
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
// several places.
//
// Omitted with exactly one candidate, it resolves; with several it is an issue
// and blocks. Guessing would be the single most damaging thing this layer could
// do -- stock removed from the wrong shelf is invisible until someone looks.
func (b *binding) where(item domain.ItemID, itemOK bool) (domain.LocationID, bool) {
	if s := strings.TrimSpace(b.raw.Fields["at"]); s != "" {
		return b.location("at")
	}
	if !itemOK {
		return 0, false
	}
	live := b.v.liveHoldings(item)
	switch len(live) {
	case 0:
		b.fail("at", "%s is not kept anywhere yet, so say where this is",
			b.v.Names.Label(domain.EntityItem, int64(item)))
		return 0, false
	case 1:
		return live[0].Location, true
	}
	var places []string
	for _, h := range live {
		places = append(places, b.v.Names.Label(domain.EntityLocation, int64(h.Location)))
	}
	b.fail("at", "it is kept in %d places (%s), so say which", len(live), strings.Join(places, ", "))
	return 0, false
}

// holdingOf picks the Holding an Item has at a place, which is how the
// stock-shaped commands reach a Holding without a person naming one.
func (b *binding) holdingOf(item domain.ItemID, at domain.LocationID, ok bool) (domain.HoldingID, bool) {
	if !ok {
		return 0, false
	}
	var found []HoldingFacts
	for _, h := range b.v.liveHoldings(item) {
		if h.Location == at {
			found = append(found, h)
		}
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
	// Same item, same place, two Holdings: they differ by basis or expiry, and
	// only a person can say which is meant.
	b.fail("at", "there are %d of those kept there; name the holding instead", len(found))
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
