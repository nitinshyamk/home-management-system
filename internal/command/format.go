package command

import (
	"fmt"
	"strings"
	"time"

	"home-management-system/internal/domain"
)

// Summary renders a Command as a line a person can check.
//
// It is what the plan screen shows per row, and what a confirmation quotes
// back, so it is written in the terms of the receipt rather than the terms of
// the schema: "used 100 g of Basmati Rice", never "Consumed delta -100000 on
// holding 7".
//
// Identifiers are rendered through a Namer because a Command holds only
// identifiers, by design -- there is no way to express an unresolved name in
// one. The names have to come from somewhere, and the index that resolved them
// is the honest place.
func Summary(c Command, names Namer) string {
	n := naming{names}
	switch v := c.(type) {

	case NewItem:
		return fmt.Sprintf("create %q as %s in %s", v.Name, counting(v), n.category(v.Category))
	case NewCategory:
		return "create category " + quoteUnder(v.Name, n.optionalCategory(v.Parent))
	case NewLocation:
		return "create location " + quoteUnder(v.Name, n.optionalLocation(v.Parent))
	case NewHolding:
		return fmt.Sprintf("keep %s in %s", n.item(v.Item), n.location(v.Location))

	case Receive:
		s := fmt.Sprintf("add %s of %s to %s", v.Amount, n.item(v.Item), n.location(v.Location))
		if v.Source != "" {
			s += " from " + v.Source
		}
		return s + expiry(v.ExpiresOn)
	case Consume:
		return trailing(fmt.Sprintf("use %s of %s", v.Amount, n.item(v.Item)), v.Reason)
	case Discard:
		return trailing(fmt.Sprintf("throw away %s from %s", v.Amount, n.holding(v.Holding)), v.Reason)
	case Open:
		return fmt.Sprintf("open a package of %s in %s", n.item(v.Item), n.location(v.Location))
	case Count:
		return fmt.Sprintf("count %s and find %s", n.holding(v.Holding), v.Observed)
	case Move:
		what := "all of " + n.holding(v.Holding)
		if v.Amount != nil {
			what = fmt.Sprintf("%s of %s", *v.Amount, n.holding(v.Holding))
		}
		return fmt.Sprintf("move %s to %s", what, n.location(v.To))

	case CheckOut:
		if v.To != nil {
			return fmt.Sprintf("take %s to %s", n.holding(v.Holding), n.location(*v.To))
		}
		return "take " + n.holding(v.Holding) + " out"
	case Return:
		return "put " + n.holding(v.Holding) + " back"
	case MarkLost:
		return "give up on finding " + n.holding(v.Holding)
	case Found:
		if v.At != nil {
			return fmt.Sprintf("found %s in %s", n.holding(v.Holding), n.location(*v.At))
		}
		return "found " + n.holding(v.Holding)
	case Verify:
		if v.Present {
			return "confirm " + n.holding(v.Holding) + " is there"
		}
		return "record that " + n.holding(v.Holding) + " is not there"
	case Retire:
		return trailing("retire "+n.holding(v.Holding), v.Reason)
	case Rehome:
		return fmt.Sprintf("%s lives in %s now", n.holding(v.Holding), n.location(v.To))

	case Promote:
		return fmt.Sprintf("track each %s individually", n.item(v.Item))
	case Demote:
		return fmt.Sprintf("count %s as an amount in %s instead", n.item(v.Item), v.ContentUnit)

	case ReparentLocation:
		return fmt.Sprintf("move %s under %s", n.location(v.Location), n.optionalLocation(v.Parent))
	case ArchiveLocation:
		return fmt.Sprintf("put %s away, %s its contents",
			n.location(v.Location), resolution(v.Resolution))
	case RestoreLocation:
		return "bring back " + n.location(v.Location)

	case Rename:
		return fmt.Sprintf("rename %s to %q", n.target(v.Target), v.Name)
	case Describe:
		return "describe " + n.target(v.Target)
	case Note:
		return "note about " + n.item(v.Item)
	case Reclassify:
		return fmt.Sprintf("file %s under %s", n.item(v.Item), n.category(v.Category))
	case Confirm:
		return fmt.Sprintf("confirm %s is filed correctly", n.item(v.Item))
	case SetExpiry:
		if v.On == nil {
			return "clear the expiry on " + n.holding(v.Holding)
		}
		return fmt.Sprintf("%s expires %s", n.holding(v.Holding), v.On.Format(dateFormat))
	case Label:
		return fmt.Sprintf("label %s %q", n.holding(v.Holding), v.Label)
	case Snooze:
		if v.Until == nil {
			return "stop snoozing " + n.holding(v.Holding)
		}
		return fmt.Sprintf("snooze %s until %s", n.holding(v.Holding), v.Until.Format(dateFormat))
	case ArchiveItem:
		return "stop stocking " + n.item(v.Item)
	case ArchiveCategory:
		return fmt.Sprintf("put %s away, %s its contents",
			n.category(v.Category), resolution(v.Resolution))
	case RestoreCategory:
		return "bring back " + n.category(v.Category)
	case ReparentCategory:
		return fmt.Sprintf("file %s under %s", n.category(v.Category), n.optionalCategory(v.Parent))
	}
	// Unreachable while TestSummaryCoversEveryCommand passes. A variant with no
	// case would otherwise render as nothing at all, which on a review screen
	// means approving a row whose text is blank.
	return string(c.Op())
}

const dateFormat = "2006-01-02"

// Namer renders an identifier as something a person recognises. resolve.Index
// satisfies it; so does anything that can look a name up.
type Namer interface {
	Label(kind domain.EntityKind, id int64) string
}

// naming falls back to the identifier when a Namer has nothing, and to the kind
// alone when there is no Namer at all -- a summary is a thing to read, so it
// must never be empty and must never panic.
type naming struct{ Namer }

func (n naming) label(kind domain.EntityKind, id int64) string {
	if n.Namer != nil {
		if s := n.Namer.Label(kind, id); s != "" {
			return s
		}
	}
	return fmt.Sprintf("%s %d", strings.ToLower(string(kind)), id)
}

func (n naming) item(id domain.ItemID) string { return n.label(domain.EntityItem, int64(id)) }
func (n naming) holding(id domain.HoldingID) string {
	return n.label(domain.EntityHolding, int64(id))
}
func (n naming) location(id domain.LocationID) string {
	return n.label(domain.EntityLocation, int64(id))
}
func (n naming) category(id domain.CategoryID) string {
	return n.label(domain.EntityCategory, int64(id))
}

func (n naming) optionalLocation(id *domain.LocationID) string {
	if id == nil {
		return "the top level"
	}
	return n.location(*id)
}

func (n naming) optionalCategory(id *domain.CategoryID) string {
	if id == nil {
		return "the top level"
	}
	return n.category(*id)
}

func (n naming) target(t Target) string { return n.label(t.Kind, t.ID) }

// counting says what cannot be changed later, because that is the whole content
// of the confirmation a person gives.
func counting(v NewItem) string {
	switch v.Counting {
	case CountingUnique:
		return "one of a kind"
	case CountingPile:
		return fmt.Sprintf("a pile counted in %s", v.ContentUnit)
	case CountingMeasured:
		if v.PackageSize != nil {
			return fmt.Sprintf("measured in %s, %s per package", v.ContentUnit, v.PackageSize)
		}
		return fmt.Sprintf("measured in %s", v.ContentUnit)
	}
	return string(v.Counting)
}

func resolution(r domain.Resolution) string {
	switch r {
	case domain.ResolutionLift:
		return "lifting"
	case domain.ResolutionMove:
		return "moving"
	case domain.ResolutionBlock:
		return "refusing if it still has"
	}
	return string(r)
}

func quoteUnder(name, parent string) string {
	return fmt.Sprintf("%q under %s", name, parent)
}

func trailing(s, reason string) string {
	if reason == "" {
		return s
	}
	return s + " (" + reason + ")"
}

func expiry(on *time.Time) string {
	if on == nil {
		return ""
	}
	return ", expiring " + on.Format(dateFormat)
}
