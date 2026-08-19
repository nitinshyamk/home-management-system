package command_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"home-management-system/internal/command"
	"home-management-system/internal/domain"
	"home-management-system/internal/resolve"
)

// index is a small vocabulary for the summaries to render against, exercising
// the Namer path rather than the fallback.
func index() *resolve.Index {
	return resolve.NewIndex([]resolve.Candidate{
		{Kind: resolve.KindLocation, ID: 1, Path: "Kitchen > Left Pantry", Leaf: "Left Pantry"},
		{Kind: resolve.KindCategory, ID: 2, Path: "Food > Grains", Leaf: "Grains"},
		{Kind: resolve.KindItem, ID: 3, Path: "Food > Grains > Basmati Rice", Leaf: "Basmati Rice"},
		{Kind: resolve.KindHolding, ID: 4, Path: "Basmati Rice > Left Pantry", Leaf: "Basmati Rice"},
	})
}

// TestSummaryCoversEveryCommand walks the generated registry, so a variant with
// no case renders as its bare op and is caught here rather than on a review
// screen, where an unhelpful line is a line someone approves without reading.
func TestSummaryCoversEveryCommand(t *testing.T) {
	for _, c := range command.AllCommands {
		name := reflect.TypeOf(c).Name()
		got := command.Summary(c, index())
		if got == "" {
			t.Errorf("%s renders as nothing", name)
			continue
		}
		// The fallthrough renders the op alone, which is what a missing case
		// looks like.
		if got == string(c.Op()) {
			t.Errorf("%s renders as its bare op %q; it has no case in Summary", name, got)
		}
	}
}

// The summary is what a person checks before approving, so it is written in the
// terms of the receipt rather than the schema. These pin the phrasing of the
// ones that carry the most consequence.
func TestSummaryReadsLikeTheReceipt(t *testing.T) {
	rice := domain.ItemID(3)
	pantry := domain.LocationID(1)
	march := time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)
	size := domain.FromMilli(2_000_000)

	for _, tc := range []struct {
		cmd  command.Command
		want string
	}{
		{command.Consume{Item: rice, Location: pantry, Amount: domain.FromMilli(100_000), Reason: "dinner"},
			"use 100 of Food > Grains > Basmati Rice (dinner)"},
		{command.Receive{Item: rice, Location: pantry, Amount: domain.FromMilli(2_000), Source: "corner shop", ExpiresOn: &march},
			"add 2 of Food > Grains > Basmati Rice to Kitchen > Left Pantry from corner shop, expiring 2027-03-01"},
		{command.NewItem{Name: "Turmeric", Category: 2, Counting: command.CountingMeasured, ContentUnit: "g", PackageSize: &size},
			`create "Turmeric" as measured in g, 2000 per package in Food > Grains`},
		{command.ArchiveLocation{Location: pantry, Resolution: domain.ResolutionLift},
			"put Kitchen > Left Pantry away, lifting its contents"},
		{command.Promote{Item: rice},
			"track each Food > Grains > Basmati Rice individually"},
		{command.Rename{Target: command.Target{Kind: domain.EntityLocation, ID: 1}, Name: "Larder"},
			`rename Kitchen > Left Pantry to "Larder"`},
	} {
		if got := command.Summary(tc.cmd, index()); got != tc.want {
			t.Errorf("%T\n got %q\nwant %q", tc.cmd, got, tc.want)
		}
	}
}

// A summary must never be empty and must never panic, even with no Namer and no
// matching entry -- it is a thing to read, and a blank row on a review screen is
// worse than an ugly one.
func TestSummarySurvivesAnUnknownName(t *testing.T) {
	c := command.Consume{Item: 999, Location: 999, Amount: domain.FromMilli(1)}

	withNothing := command.Summary(c, nil)
	if !strings.Contains(withNothing, "item 999") {
		t.Errorf("with no Namer: %q, want it to fall back to the identifier", withNothing)
	}
	withEmpty := command.Summary(c, index())
	if !strings.Contains(withEmpty, "item 999") {
		t.Errorf("with an index that does not know it: %q", withEmpty)
	}
}

// Label is how a Command's identifiers become names again, and it must refuse
// to guess: a wrong name on a review screen is approved as readily as a right
// one.
func TestLabelIsExactOrEmpty(t *testing.T) {
	ix := index()
	if got := ix.Label(domain.EntityItem, 3); got != "Food > Grains > Basmati Rice" {
		t.Errorf("Label = %q", got)
	}
	// Same id, different kind: not a match.
	if got := ix.Label(domain.EntityLocation, 3); got != "" {
		t.Errorf("Label of a Location with an Item's id = %q, want empty", got)
	}
	if got := ix.Label(domain.EntityItem, 999); got != "" {
		t.Errorf("Label of an unknown id = %q, want empty", got)
	}
}
