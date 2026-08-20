package ops

import (
	"context"
	"fmt"
	"time"

	"home-management-system/internal/domain"
	"home-management-system/internal/ledger"
	"home-management-system/internal/origin"
)

// The Origination variants.
//
// Each carries two strings and the difference between them matters. Describe is
// prose for a plan line; Permanent is the FACTS that cannot be changed
// afterwards, which the confirmation panel sets out on their own.
//
// Separate accessors rather than one string the panel picks apart, because the
// confirmation is the highest-stakes text in the application and string surgery
// on prose is how it would quietly start saying the wrong thing.

// NewCategory creates a classification node.
type NewCategory struct {
	Name        string
	Parent      *domain.CategoryID
	Description string
}

func (NewCategory) isOrigination()          {}
func (NewCategory) NeedsConfirmation() bool { return true }

// A Category has nothing permanent: name, description, and parent are all
// revisable. It is confirmed anyway, because a second one created by accident
// is the hazard rather than anything about it being fixed.
func (NewCategory) Permanent() string { return "" }

// Describe is short because a Category has no permanent fields worth warning
// about: name, description, and parent are all revisable. It is originated
// rather than recorded only because Category has no ledger at all.
func (n NewCategory) Describe() string { return fmt.Sprintf("category %q", n.Name) }

func (n NewCategory) originate(ctx context.Context, o *origin.Originator, _ *ledger.Processor, c *Created) error {
	id, err := o.CreateCategory(ctx, origin.CreateCategoryInput{
		Name: n.Name, Parent: n.Parent, Description: n.Description,
	})
	if err != nil {
		return err
	}
	c.Categories = append(c.Categories, id)
	return nil
}

// NewUniqueItem creates an Item whose holdings are individually tracked.
type NewUniqueItem struct {
	Name     string
	Category domain.CategoryID
	Notes    string
}

func (NewUniqueItem) isOrigination()          {}
func (NewUniqueItem) NeedsConfirmation() bool { return true }

// Kind is the one thing about a Unique Item that can never change: making it
// countable later means Promote's opposite, which replaces every Holding.
func (NewUniqueItem) Permanent() string { return "kind = Unique" }

func (n NewUniqueItem) Describe() string {
	return fmt.Sprintf("item %q as one of a kind (permanent: kind = Unique)", n.Name)
}

func (n NewUniqueItem) originate(ctx context.Context, o *origin.Originator, _ *ledger.Processor, c *Created) error {
	id, err := o.CreateUniqueItem(ctx, origin.CreateUniqueItemInput{
		Name: n.Name, Category: n.Category, Notes: n.Notes,
	})
	if err != nil {
		return err
	}
	c.Items = append(c.Items, id)
	return nil
}

// NewBulkItem creates an Item whose holdings are measured amounts.
type NewBulkItem struct {
	Name        string
	Category    domain.CategoryID
	ContentUnit domain.UnitCode
	PackageSize *domain.Quantity
	Notes       string
}

func (NewBulkItem) isOrigination()          {}
func (NewBulkItem) NeedsConfirmation() bool { return true }

// Describe carries all three permanent fields. This is the confirmation that
// matters most in the system: changing kind later means Promote, which replaces
// every Holding, and changing content unit is not expressible as an event at
// all (E7).
// All three, because this is the confirmation that matters most in the system:
// changing kind later means Promote, which replaces every Holding, and changing
// the content unit is not expressible as an event at all (E7).
func (n NewBulkItem) Permanent() string {
	pkg := "none"
	if n.PackageSize != nil {
		pkg = n.PackageSize.String()
	}
	return fmt.Sprintf("kind = Bulk   unit = %s   package = %s", n.ContentUnit, pkg)
}

func (n NewBulkItem) Describe() string {
	pkg := "none"
	if n.PackageSize != nil {
		pkg = n.PackageSize.String()
	}
	return fmt.Sprintf("item %q measured in %s (permanent: kind = Bulk, unit = %s, package = %s)",
		n.Name, n.ContentUnit, n.ContentUnit, pkg)
}

func (n NewBulkItem) originate(ctx context.Context, o *origin.Originator, _ *ledger.Processor, c *Created) error {
	id, err := o.CreateBulkItem(ctx, origin.CreateBulkItemInput{
		Name: n.Name, Category: n.Category, ContentUnit: n.ContentUnit,
		PackageSize: n.PackageSize, Notes: n.Notes,
	})
	if err != nil {
		return err
	}
	c.Items = append(c.Items, id)
	return nil
}

// NewLocation creates a place. Recorded rather than originated -- a Location's
// containment is verified, so its creation must be an event.
type NewLocation struct {
	Name        string
	Parent      *domain.LocationID
	Description string
}

func (NewLocation) isOrigination()          {}
func (NewLocation) NeedsConfirmation() bool { return true }

// A Location has nothing permanent either -- its name is annotation and its
// parent is recorded, so both can change.
func (NewLocation) Permanent() string { return "" }

func (n NewLocation) Describe() string { return fmt.Sprintf("location %q", n.Name) }

func (n NewLocation) originate(ctx context.Context, _ *origin.Originator, l *ledger.Processor, c *Created) error {
	id, err := l.CreateLocation(ctx, n.Name, n.Parent, n.Description)
	if err != nil {
		return err
	}
	c.Locations = append(c.Locations, id)
	return nil
}

// NewBulkHolding puts a measured Item somewhere, at quantity zero.
//
// Item is a pointer into this Step's Created when nil, which is how "buy
// something you have never had" links the Item it just made to the Holding of
// it without a dependency graph.
type NewBulkHolding struct {
	Item      *domain.ItemID // nil means "the first Item this Step created"
	Location  domain.LocationID
	UnitBasis domain.UnitBasis
	ExpiresOn *time.Time
}

func (NewBulkHolding) isOrigination()          {}
func (NewBulkHolding) NeedsConfirmation() bool { return false }

// A Holding's unit basis is immutable, but a Holding is not confirmed -- it is
// implied by acquiring stock rather than chosen -- so there is nothing here for
// a panel to set out.
func (NewBulkHolding) Permanent() string { return "" }

func (n NewBulkHolding) Describe() string {
	return fmt.Sprintf("holding counted by %s (permanent: unit basis = %s)", n.UnitBasis, n.UnitBasis)
}

func (n NewBulkHolding) originate(ctx context.Context, _ *origin.Originator, l *ledger.Processor, c *Created) error {
	item, err := resolveItem(n.Item, c)
	if err != nil {
		return err
	}
	id, err := l.CreateBulkHolding(ctx, ledger.CreateBulkHoldingInput{
		Item: item, Location: n.Location, UnitBasis: n.UnitBasis, ExpiresOn: n.ExpiresOn,
	})
	if err != nil {
		return err
	}
	c.Holdings = append(c.Holdings, id)
	return nil
}

// NewUniqueHolding puts an individually-tracked Item somewhere.
type NewUniqueHolding struct {
	Item      *domain.ItemID // nil means "the first Item this Step created"
	Location  domain.LocationID
	Label     string
	ExpiresOn *time.Time
}

func (NewUniqueHolding) isOrigination()          {}
func (NewUniqueHolding) NeedsConfirmation() bool { return false }

func (NewUniqueHolding) Permanent() string { return "" }

func (n NewUniqueHolding) Describe() string { return "holding, individually tracked" }

func (n NewUniqueHolding) originate(ctx context.Context, _ *origin.Originator, l *ledger.Processor, c *Created) error {
	item, err := resolveItem(n.Item, c)
	if err != nil {
		return err
	}
	id, err := l.CreateUniqueHolding(ctx, ledger.CreateUniqueHoldingInput{
		Item: item, Location: n.Location, Label: n.Label, ExpiresOn: n.ExpiresOn,
	})
	if err != nil {
		return err
	}
	c.Holdings = append(c.Holdings, id)
	return nil
}

// resolveItem reads an Item reference that may point at one this Step created.
func resolveItem(ref *domain.ItemID, c *Created) (domain.ItemID, error) {
	if ref != nil {
		return *ref, nil
	}
	return c.Item(0)
}
