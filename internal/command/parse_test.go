package command_test

import (
	"errors"
	"reflect"
	"testing"

	"home-management-system/internal/command"
)

func TestParseTheLine(t *testing.T) {
	for _, tc := range []struct {
		line   string
		op     string
		fields map[string]string
	}{
		{
			`consume rice 100g`, "consume",
			map[string]string{"item": "rice", "qty": "100g"},
		},
		{
			`consume rice 100g at kitchen reason dinner`, "consume",
			map[string]string{"item": "rice", "qty": "100g", "at": "kitchen", "reason": "dinner"},
		},
		{
			`acquire "Basmati Rice" 2bag at "Left Pantry > Shelf 1" from "corner shop"`, "acquire",
			map[string]string{
				"item": "Basmati Rice", "qty": "2bag",
				"at": "Left Pantry > Shelf 1", "from": "corner shop",
			},
		},
		// Two-word ops are matched before one-word ones could swallow them.
		{
			`archive location Pantry resolution lift`, "archive location",
			map[string]string{"location": "Pantry", "resolution": "lift"},
		},
		{
			`restore category Spices`, "restore category",
			map[string]string{"target": "Spices"},
		},
		{
			`new item Turmeric counting measured unit g package 2000`, "new item",
			map[string]string{"name": "Turmeric", "counting": "measured", "unit": "g", "package": "2000"},
		},
		// Pairs are order-free.
		{
			`acquire rice 2bag from shop at pantry`, "acquire",
			map[string]string{"item": "rice", "qty": "2bag", "from": "shop", "at": "pantry"},
		},
		// Case does not matter for the op or the keys, and does for the values.
		{
			`CONSUME Rice 100g AT Kitchen`, "consume",
			map[string]string{"item": "Rice", "qty": "100g", "at": "Kitchen"},
		},
	} {
		got, err := command.Parse(tc.line)
		if err != nil {
			t.Errorf("%q: %v", tc.line, err)
			continue
		}
		if got.Op != tc.op {
			t.Errorf("%q: op = %q, want %q", tc.line, got.Op, tc.op)
		}
		if !reflect.DeepEqual(got.Fields, tc.fields) {
			t.Errorf("%q:\n got %v\nwant %v", tc.line, got.Fields, tc.fields)
		}
	}
}

// A field's own key, written where a value is expected, is a value. Otherwise
// `rename to Larder` would rename nothing and set a key called "to".
func TestAKeyInAValueSlotIsAValue(t *testing.T) {
	got, err := command.Parse(`rename to Larder`)
	if err != nil {
		t.Fatal(err)
	}
	if got.Fields["target"] != "to" || got.Fields["name"] != "Larder" {
		t.Errorf("fields = %v, want target=to name=Larder", got.Fields)
	}
	// And quoting is the escape hatch when it is not the next slot.
	q, err := command.Parse(`consume rice 100g at "at"`)
	if err != nil {
		t.Fatal(err)
	}
	if q.Fields["at"] != "at" {
		t.Errorf("fields = %v, want a location literally called at", q.Fields)
	}
}

func TestParseRefusesNonsense(t *testing.T) {
	for _, line := range []string{
		``,
		`   `,
		`frobnicate rice`,                // not a command
		`consume "rice`,                  // unclosed quote
		`consume rice 100g at`,           // a key with no value
		`consume rice 100g extra 1 more`, // no slot left for it
		`consume rice 100g at a at b`,    // the same key twice
	} {
		if got, err := command.Parse(line); !errors.Is(err, command.ErrSyntax) {
			t.Errorf("%q parsed to %+v (err %v), want ErrSyntax", line, got, err)
		}
	}
}

// Every op in the registry must be typeable, or the command line is a smaller
// interface than the CSV that shares its vocabulary.
func TestEveryOpIsReachableFromTheLine(t *testing.T) {
	for _, c := range command.AllCommands {
		spec, ok := command.SpecOf(c.Op())
		if !ok {
			continue // reported by TestEveryCommandHasASpec
		}
		line := string(c.Op())
		for range spec.Positional() {
			line += ` "x"`
		}
		got, err := command.Parse(line)
		if err != nil {
			t.Errorf("%q: %v", line, err)
			continue
		}
		if got.Op != string(c.Op()) {
			t.Errorf("%q parsed as %q", line, got.Op)
		}
	}
}
