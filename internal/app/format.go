package app

import (
	"fmt"
	"strings"
	"time"

	"home-management-system/internal/domain"
	"home-management-system/internal/query"
)

// Summarise renders one event for display.
//
// This is the third 24-way dispatch, after writePayload and hydrateEvent. Three
// switches over one sealed interface is exactly the cost the visitor pattern
// would have removed -- and exactly the ceremony it would have added, since each
// visitor needs all 24 methods whether or not it cares. The generated registry
// makes a missing case a test failure either way.
func Summarise(e domain.Event) string {
	switch ev := e.(type) {

	case domain.HoldingCreated:
		return fmt.Sprintf("put at location %d", ev.StowedLocation)
	case domain.Moved:
		return fmt.Sprintf("moved from %d to %d", ev.From, ev.To)
	case domain.Rehomed:
		return fmt.Sprintf("now lives at %d (was %d)", ev.To, ev.From)

	case domain.Acquired:
		out := fmt.Sprintf("acquired %s", ev.Delta)
		if ev.Source != "" {
			out += " from " + ev.Source
		}
		if ev.Price != nil {
			out += fmt.Sprintf(" for %d", *ev.Price)
		}
		return out

	case domain.Consumed:
		return withReason(fmt.Sprintf("used %s", abs(ev.Delta)), ev.Reason)
	case domain.Discarded:
		return withReason(fmt.Sprintf("discarded %s", abs(ev.Delta)), ev.Reason)
	case domain.Opened:
		return withReason(fmt.Sprintf("opened, +%s", ev.Delta), ev.Reason)
	case domain.Split:
		return withReason(fmt.Sprintf("split off %s", abs(ev.Delta)), ev.Reason)
	case domain.Merged:
		return withReason(fmt.Sprintf("merged %s", ev.Delta), ev.Reason)
	case domain.Adjusted:
		return withReason(fmt.Sprintf("adjusted by %s", ev.Delta), ev.Reason)

	case domain.CheckedOut:
		if ev.DisplacedTo != nil {
			return fmt.Sprintf("checked out to %d", *ev.DisplacedTo)
		}
		// Out with no known destination is derived-Missing: transient
		// ignorance, distinct from Lost.
		return "checked out, whereabouts unknown"
	case domain.Returned:
		return "returned"
	case domain.MarkedLost:
		return "marked lost"
	case domain.Found:
		return "found"

	case domain.Counted:
		// An observation. The correction, if any, is a separate Adjusted --
		// which is what makes the ledger trustworthy enough to derive rates from.
		return fmt.Sprintf("counted %s", ev.Observed)
	case domain.Verified:
		if ev.Present {
			return "verified present"
		}
		return "verified missing"

	case domain.Gone:
		return withReason("gone", ev.Reason)

	case domain.NodeCreated:
		if ev.Parent == nil {
			return "place created as a root"
		}
		return fmt.Sprintf("place created under %d", *ev.Parent)
	case domain.NodeReparented:
		return fmt.Sprintf("place moved from %s to %s", showLoc(ev.FromParent), showLoc(ev.ToParent))
	case domain.NodeArchived:
		return fmt.Sprintf("place archived (%s)", ev.Resolution)
	case domain.NodeRestored:
		return "place restored"

	case domain.ItemKindChanged:
		return fmt.Sprintf("kind %s -> %s", ev.FromKind, ev.ToKind)
	case domain.ItemUnitChanged:
		return fmt.Sprintf("unit %s -> %s", showUnit(ev.FromUnit), showUnit(ev.ToUnit))
	case domain.ItemPackageSizeChanged:
		return fmt.Sprintf("package size %s -> %s", showQty(ev.FromSize), showQty(ev.ToSize))

	default:
		// Unreachable while TestSummariseHandlesEveryEventType passes, which
		// walks the generated registry.
		return fmt.Sprintf("unrendered event %T", e)
	}
}

func withReason(text, reason string) string {
	if reason == "" {
		return text
	}
	return text + " (" + reason + ")"
}

func abs(q domain.Quantity) domain.Quantity {
	if q.IsNegative() {
		neg, err := q.Neg()
		if err != nil {
			return q
		}
		return neg
	}
	return q
}

func showLoc(id *domain.LocationID) string {
	if id == nil {
		return "root"
	}
	return fmt.Sprint(*id)
}

func showUnit(u *domain.UnitCode) string {
	if u == nil {
		return "none"
	}
	return string(*u)
}

func showQty(q *domain.Quantity) string {
	if q == nil {
		return "none"
	}
	return q.String()
}

// ---------------------------------------------------------------------------
// Holdings and Items
// ---------------------------------------------------------------------------

// describeState renders what a Holding currently is.
//
// The two kinds read completely differently, which is the point: a Bulk holding
// has an amount, a Unique one has a custody state. Neither has the other's.
func describeState(d query.HoldingDetail) string {
	switch h := d.Holding.(type) {
	case domain.BulkHolding:
		if h.UnitBasis == domain.BasisPackage {
			out := fmt.Sprintf("%s packages", h.Quantity)
			if d.PackageSize != nil {
				if total, err := h.Quantity.Mul(d.PackageSize.Milli() / domain.Scale); err == nil {
					out += fmt.Sprintf(" (%s %s)", total, d.ContentUnit)
				}
			}
			return out
		}
		if h.IsDepleted() {
			return fmt.Sprintf("empty (%s)", d.ContentUnit)
		}
		return fmt.Sprintf("%s %s", h.Quantity, d.ContentUnit)

	case domain.UniqueHolding:
		switch {
		case h.IsMissing():
			return "out, whereabouts unknown"
		case h.Custody == domain.CustodyOut:
			return fmt.Sprintf("out at %d", *h.DisplacedTo)
		case h.Custody == domain.CustodyLost:
			return "lost"
		default:
			if h.Label != "" {
				return "at rest: " + h.Label
			}
			return "at rest"
		}
	default:
		return "unknown"
	}
}

// describeFlags renders the knowledge and lifecycle notes -- the
// directly-mutable attributes that replay does not reconstruct and does not
// claim to.
func describeFlags(d query.HoldingDetail) string {
	var parts []string
	base := d.Holding.Base()

	if base.RetiredAt != nil {
		parts = append(parts, "gone "+base.RetiredAt.Format("2006-01-02"))
	}
	if base.ExpiresOn != nil {
		label := "expires " + base.ExpiresOn.Format("2006-01-02")
		if base.ExpiresOn.Before(time.Now()) {
			label = "EXPIRED " + base.ExpiresOn.Format("2006-01-02")
		}
		parts = append(parts, label)
	}
	if base.SnoozedUntil != nil && base.SnoozedUntil.After(time.Now()) {
		parts = append(parts, "snoozed")
	}
	if u, ok := d.Holding.(domain.UniqueHolding); ok && u.CustodySince != nil {
		days := int(time.Since(*u.CustodySince).Hours() / 24)
		if days >= 1 {
			// The out-of-place nudge: a thing out for weeks has not been "in
			// use", it has been lost or silently relocated.
			parts = append(parts, fmt.Sprintf("out %dd", days))
		}
	}
	return strings.Join(parts, ", ")
}

func describeMeasure(item domain.Item) string {
	switch typed := item.(type) {
	case domain.BulkItem:
		if typed.HasPackage() {
			return fmt.Sprintf("%s, %s per package", typed.ContentUnit, typed.PackageSize)
		}
		return string(typed.ContentUnit)
	case domain.UniqueItem:
		return "one of a kind"
	default:
		return ""
	}
}
