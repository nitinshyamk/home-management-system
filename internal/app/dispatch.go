package app

import (
	"context"
	"fmt"

	"home-management-system/internal/command"
	"home-management-system/internal/ops"
)

// Plan turns a Command into a Batch.
//
// This lives in app rather than in either neighbour, and the placement is the
// architecture rather than a convenience. internal/command must not import
// internal/ops -- binding resolves names, it does not decide what will happen --
// and internal/ops must not import internal/command, or the operations would
// depend on the vocabulary that describes them. app is the layer that
// legitimately knows both, so the conversion is here.
//
// The price is that the Command variants mirror the ops requests field for
// field. The compensation is that this switch will not compile if they drift,
// and that a test walks the generated registry to prove the switch is total.
//
// Nothing here decides anything. Every judgement -- what is legal, what merges,
// what a plan implies -- belongs to the Planner, and this function's only job is
// to hand it the right request.
func Plan(ctx context.Context, p *ops.Planner, c command.Command) (ops.Batch, error) {
	switch v := c.(type) {

	// Origination
	case command.NewItem:
		return p.NewItem(ctx, ops.NewItemRequest{
			Name: v.Name, Category: v.Category, Counting: ops.Counting(v.Counting),
			ContentUnit: v.ContentUnit, PackageSize: v.PackageSize, Notes: v.Notes,
		})
	case command.NewCategory:
		return p.NewCategory(ctx, ops.NewCategoryRequest{
			Name: v.Name, Parent: v.Parent, Description: v.Description,
		})
	case command.NewLocation:
		return p.NewLocation(ctx, ops.NewLocationRequest{
			Name: v.Name, Parent: v.Parent, Description: v.Description,
		})
	case command.NewHolding:
		return p.NewHolding(ctx, ops.NewHoldingRequest{
			Item: v.Item, Location: v.Location, Basis: v.Basis,
			ExpiresOn: v.ExpiresOn, Label: v.Label,
		})

	// Recording -- stock
	case command.Receive:
		return p.Receive(ctx, ops.ReceiveRequest{
			Item: v.Item, Location: v.Location, Amount: v.Amount, Basis: v.Basis,
			ExpiresOn: v.ExpiresOn, Source: v.Source, Price: v.Price,
		})
	case command.Consume:
		return p.Consume(ctx, ops.ConsumeRequest{
			Item: v.Item, Location: v.Location, Amount: v.Amount, Reason: v.Reason,
		})
	case command.Discard:
		return p.Discard(ctx, ops.DiscardRequest{
			Holding: v.Holding, Amount: v.Amount, Reason: v.Reason,
		})
	case command.Open:
		return p.Open(ctx, ops.OpenRequest{Item: v.Item, Location: v.Location})
	case command.Count:
		return p.Count(ctx, ops.CountRequest{Holding: v.Holding, Observed: v.Observed})
	case command.Move:
		return p.Move(ctx, ops.MoveRequest{Holding: v.Holding, To: v.To, Amount: v.Amount})

	// Recording -- custody, lifecycle, typing, the Location tree
	case command.CheckOut:
		return p.CheckOut(ctx, ops.CheckOutRequest{Holding: v.Holding, To: v.To})
	case command.Return:
		return p.Return(ctx, ops.ReturnRequest{Holding: v.Holding})
	case command.MarkLost:
		return p.MarkLost(ctx, ops.MarkLostRequest{Holding: v.Holding})
	case command.Found:
		return p.Found(ctx, ops.FoundRequest{Holding: v.Holding})
	case command.Verify:
		return p.Verify(ctx, ops.VerifyRequest{Holding: v.Holding, Present: v.Present})
	case command.Retire:
		return p.Retire(ctx, ops.RetireRequest{Holding: v.Holding, Reason: v.Reason})
	case command.Rehome:
		return p.Rehome(ctx, ops.RehomeRequest{Holding: v.Holding, To: v.To})
	case command.Promote:
		return p.Promote(ctx, ops.PromoteRequest{Item: v.Item})
	case command.Demote:
		return p.Demote(ctx, ops.DemoteRequest{
			Item: v.Item, ContentUnit: v.ContentUnit, PackageSize: v.PackageSize,
		})
	case command.ReparentLocation:
		return p.ReparentLocation(ctx, ops.ReparentLocationRequest{
			Location: v.Location, Parent: v.Parent,
		})
	case command.ArchiveLocation:
		return p.ArchiveLocation(ctx, ops.ArchiveLocationRequest{
			Location: v.Location, Resolution: v.Resolution, MoveTo: v.MoveTo,
		})
	case command.RestoreLocation:
		return p.RestoreLocation(ctx, ops.RestoreLocationRequest{Location: v.Location})

	// Annotation
	case command.Rename:
		return p.Rename(ctx, ops.RenameRequest{Target: target(v.Target), Name: v.Name})
	case command.Describe:
		return p.Describe(ctx, ops.DescribeRequest{
			Target: target(v.Target), Description: v.Description,
		})
	case command.Note:
		return p.Note(ctx, ops.NoteRequest{Item: v.Item, Notes: v.Notes})
	case command.Reclassify:
		return p.Reclassify(ctx, ops.ReclassifyRequest{Item: v.Item, Category: v.Category})
	case command.Confirm:
		return p.Confirm(ctx, ops.ConfirmRequest{Item: v.Item})
	case command.SetExpiry:
		return p.SetExpiry(ctx, ops.SetExpiryRequest{Holding: v.Holding, On: v.On})
	case command.Label:
		return p.Label(ctx, ops.LabelRequest{Holding: v.Holding, Label: v.Label})
	case command.Snooze:
		return p.Snooze(ctx, ops.SnoozeRequest{Holding: v.Holding, Until: v.Until})
	case command.ArchiveItem:
		return p.ArchiveItem(ctx, ops.ArchiveItemRequest{Item: v.Item})
	case command.ArchiveCategory:
		return p.ArchiveCategory(ctx, ops.ArchiveCategoryRequest{
			Category: v.Category, Resolution: v.Resolution, MoveTo: v.MoveTo,
		})
	case command.RestoreCategory:
		return p.RestoreCategory(ctx, ops.RestoreCategoryRequest{Category: v.Category})
	case command.ReparentCategory:
		return p.ReparentCategory(ctx, ops.ReparentCategoryRequest{
			Category: v.Category, Parent: v.Parent,
		})
	}
	// Unreachable while TestEveryCommandPlans passes, which walks the generated
	// registry. It is here so that a new variant fails loudly at runtime too,
	// rather than silently doing nothing.
	return ops.Batch{}, fmt.Errorf("app: no plan for command %T (%s)", c, c.Op())
}

func target(t command.Target) ops.Target {
	return ops.Target{Kind: t.Kind, ID: t.ID}
}
