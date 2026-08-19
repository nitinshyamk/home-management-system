package command

import (
	"home-management-system/internal/domain"
)

// build assembles the typed Command, one case per op.
//
// It reads every field even after something has failed, so a person fixing a
// row sees all of it at once rather than one problem per attempt. The result is
// discarded if anything went wrong -- what matters is the issues, not a
// half-built command.
func (b *binding) build(op Op) Command {
	switch op {

	// -----------------------------------------------------------------------
	// Origination
	// -----------------------------------------------------------------------
	case OpNewItem:
		cmd := NewItem{Name: b.plain("name"), Notes: b.plain("notes")}
		if s, ok := b.text("counting"); ok {
			c, err := ParseCounting(s)
			if err != nil {
				b.fail("counting", "%v", err)
			}
			cmd.Counting = c
		}
		if s, ok := b.text("unit"); ok {
			if u, err := b.v.unit(domain.UnitCode(s)); err != nil {
				b.fail("unit", "%v", err)
			} else {
				cmd.ContentUnit = u.Code
			}
		}
		if s, ok := b.text("package"); ok {
			written, err := ParseAmount(s)
			switch {
			case err != nil:
				b.fail("package", "%v", err)
			case written.Unit != "" || written.Packages:
				b.fail("package", "a package size is a plain amount in the item's own unit")
			default:
				size := written.Value
				cmd.PackageSize = &size
			}
		}
		if id, ok := b.category("category"); ok {
			cmd.Category = id
		}
		return cmd

	case OpNewCategory:
		return NewCategory{
			Name: b.plain("name"), Parent: b.optionalCategory("under"),
			Description: b.plain("describe"),
		}

	case OpNewLocation:
		return NewLocation{
			Name: b.plain("name"), Parent: b.optionalLocation("under"),
			Description: b.plain("describe"),
		}

	case OpNewHolding:
		item, _ := b.item("item")
		at, _ := b.location("at")
		cmd := NewHolding{Item: item, Location: at, Label: b.plain("label")}
		cmd.ExpiresOn, _ = b.date("expires")
		if s, ok := b.text("basis"); ok {
			switch s {
			case "content":
				cmd.Basis = domain.BasisContent
			case "package":
				cmd.Basis = domain.BasisPackage
			default:
				b.fail("basis", "%q is not content or package", s)
			}
		} else if _, unique := b.v.items[item].(domain.UniqueItem); !unique {
			// A measured Holding has to count in something, and guessing is
			// permanent: unit_basis is immutable, and Opened creates a NEW
			// Holding rather than converting one.
			cmd.Basis = domain.BasisContent
		}
		return cmd

	// -----------------------------------------------------------------------
	// Recording -- stock
	// -----------------------------------------------------------------------
	case OpReceive:
		item, itemOK := b.item("item")
		amount, basis, _ := b.amount("qty", item, itemOK)
		cmd := Receive{Item: item, Amount: amount, Basis: basis, Source: b.plain("from")}
		// Receiving is the one stock command that may name a place with nothing
		// in it yet, so it resolves `at` directly rather than through where().
		if s := b.plain("at"); s != "" {
			cmd.Location, _ = b.location("at")
		} else if at, ok := b.where(item, itemOK); ok {
			cmd.Location = at
		}
		cmd.ExpiresOn, _ = b.date("expires")
		cmd.Price = b.money("price")
		return cmd

	case OpConsume:
		item, itemOK := b.item("item")
		amount, _, _ := b.amount("qty", item, itemOK)
		at, _ := b.where(item, itemOK)
		return Consume{Item: item, Location: at, Amount: amount, Reason: b.plain("reason")}

	case OpDiscard:
		item, itemOK := b.item("item")
		amount, _, _ := b.amount("qty", item, itemOK)
		at, atOK := b.where(item, itemOK)
		holding, _ := b.holdingOf(item, at, atOK)
		return Discard{Holding: holding, Amount: amount, Reason: b.plain("reason")}

	case OpOpen:
		item, itemOK := b.item("item")
		at, _ := b.where(item, itemOK)
		return Open{Item: item, Location: at}

	case OpCount:
		item, itemOK := b.item("item")
		observed, _, _ := b.amount("observed", item, itemOK)
		at, atOK := b.where(item, itemOK)
		holding, _ := b.holdingOf(item, at, atOK)
		return Count{Holding: holding, Observed: observed}

	case OpMove:
		item, itemOK := b.item("item")
		at, atOK := b.where(item, itemOK)
		holding, _ := b.holdingOf(item, at, atOK)
		to, _ := b.location("to")
		cmd := Move{Holding: holding, To: to}
		if s := b.plain("qty"); s != "" {
			if amount, _, ok := b.amount("qty", item, itemOK); ok {
				cmd.Amount = &amount
			}
		}
		return cmd

	// -----------------------------------------------------------------------
	// Recording -- custody, lifecycle, typing, the Location tree
	// -----------------------------------------------------------------------
	case OpCheckOut:
		holding, _ := b.holding("holding")
		return CheckOut{Holding: holding, To: b.optionalLocation("to")}

	case OpReturn:
		holding, _ := b.holding("holding")
		return Return{Holding: holding}

	case OpMarkLost:
		holding, _ := b.holding("holding")
		return MarkLost{Holding: holding}

	case OpFound:
		holding, _ := b.holding("holding")
		return Found{Holding: holding}

	case OpVerify:
		holding, _ := b.holding("holding")
		cmd := Verify{Holding: holding}
		if s, ok := b.text("present"); ok {
			present, err := ParseBool(s)
			if err != nil {
				b.fail("present", "%v", err)
			}
			cmd.Present = present
		}
		return cmd

	case OpRetire:
		holding, _ := b.holding("holding")
		return Retire{Holding: holding, Reason: b.plain("reason")}

	case OpRehome:
		holding, _ := b.holding("holding")
		to, _ := b.location("to")
		return Rehome{Holding: holding, To: to}

	case OpPromote:
		item, _ := b.item("item")
		return Promote{Item: item}

	case OpDemote:
		item, _ := b.item("item")
		cmd := Demote{Item: item}
		if s, ok := b.text("unit"); ok {
			if u, err := b.v.unit(domain.UnitCode(s)); err != nil {
				b.fail("unit", "%v", err)
			} else {
				cmd.ContentUnit = u.Code
			}
		}
		if s, ok := b.text("package"); ok {
			written, err := ParseAmount(s)
			if err != nil {
				b.fail("package", "%v", err)
			} else {
				size := written.Value
				cmd.PackageSize = &size
			}
		}
		return cmd

	case OpReparentLocation:
		location, _ := b.location("location")
		return ReparentLocation{Location: location, Parent: b.optionalLocation("under")}

	case OpArchiveLocation:
		location, _ := b.location("location")
		cmd := ArchiveLocation{Location: location, MoveTo: b.optionalLocation("to")}
		cmd.Resolution = b.resolution()
		return cmd

	case OpRestoreLocation:
		location, _ := b.location("location")
		return RestoreLocation{Location: location}

	// -----------------------------------------------------------------------
	// Annotation
	// -----------------------------------------------------------------------
	case OpRename:
		return Rename{Target: b.target("target"), Name: b.plain("name")}

	case OpDescribe:
		return Describe{Target: b.target("target"), Description: b.plain("text")}

	case OpNote:
		item, _ := b.item("item")
		return Note{Item: item, Notes: b.plain("text")}

	case OpReclassify:
		item, _ := b.item("item")
		to, _ := b.category("to")
		return Reclassify{Item: item, Category: to}

	case OpConfirm:
		item, _ := b.item("item")
		return Confirm{Item: item}

	case OpSetExpiry:
		holding, _ := b.holding("holding")
		on, _ := b.date("date")
		return SetExpiry{Holding: holding, On: on}

	case OpLabel:
		holding, _ := b.holding("holding")
		return Label{Holding: holding, Label: b.plain("text")}

	case OpSnooze:
		holding, _ := b.holding("holding")
		until, _ := b.date("until")
		return Snooze{Holding: holding, Until: until}

	case OpArchiveItem:
		item, _ := b.item("target")
		return ArchiveItem{Item: item}

	case OpArchiveCategory:
		category, _ := b.category("target")
		cmd := ArchiveCategory{Category: category, MoveTo: b.optionalCategory("to")}
		cmd.Resolution = b.resolution()
		return cmd

	case OpRestoreCategory:
		category, _ := b.category("target")
		return RestoreCategory{Category: category}

	case OpReparentCategory:
		category, _ := b.category("target")
		return ReparentCategory{Category: category, Parent: b.optionalCategory("under")}
	}

	// Unreachable while TestEveryCommandBinds passes, which walks the generated
	// registry. Reporting it as an issue rather than returning nil keeps the
	// caller's contract -- a result is either ready or explains itself.
	b.fail("op", "%q has no binding", op)
	return nil
}

// resolution reads the archive disposition, which is required rather than
// defaulted: what happens to the contents is the caller's decision, and
// silently picking one is how things end up somewhere nobody chose.
func (b *binding) resolution() domain.Resolution {
	s, ok := b.text("resolution")
	if !ok {
		return ""
	}
	r, err := ParseResolution(s)
	if err != nil {
		b.fail("resolution", "%v", err)
	}
	return r
}
