package command

import (
	"reflect"
	"testing"
)

// sample is a plausible value for a field, chosen so the awkward cases are
// always exercised: names hold spaces and a ">" separator, and text holds
// several words. A corpus of single bare words would round-trip trivially and
// prove nothing.
func sample(f Field) string {
	switch f.Type {
	case FieldName:
		return "Kitchen > Left Pantry > Shelf 1"
	case FieldQuantity:
		return "2bag"
	case FieldDate:
		return "2026-12-01"
	case FieldMoney:
		return "3.50"
	case FieldYesNo:
		return "yes"
	case FieldUnit:
		return "g"
	case FieldChoice:
		if len(f.Choices) > 0 {
			return f.Choices[0]
		}
		return "lift"
	}
	return "a few words"
}

// The property the plan screen's row editing rests on: opening a row as a line
// and pressing enter unchanged must leave the row unchanged.
func TestLineParsesBackToTheSameCommand(t *testing.T) {
	for _, spec := range Specs() {
		t.Run(string(spec.Op), func(t *testing.T) {
			raw := RawCommand{Op: string(spec.Op), Fields: map[string]string{}}
			for _, f := range spec.Fields {
				raw.Fields[f.Key] = sample(f)
			}

			line := Line(raw)
			back, err := Parse(line)
			if err != nil {
				t.Fatalf("Line produced something Parse refuses:\n  %s\n  %v", line, err)
			}
			if back.Op != raw.Op {
				t.Errorf("op: %q became %q via %s", raw.Op, back.Op, line)
			}
			if !reflect.DeepEqual(back.Fields, raw.Fields) {
				t.Errorf("fields changed via %s\n  want %v\n  got  %v", line, raw.Fields, back.Fields)
			}
		})
	}
}

// A gap in the positionals is the case that shifts values into the wrong slot
// if the renderer keeps writing bare after one is missing.
func TestLineSurvivesAGapInThePositionals(t *testing.T) {
	// consume takes item then qty positionally. The gap must be the FIRST of
	// them: with the gap last, a renderer that wrongly keeps writing bare
	// produces the same line as one that stops, and the test proves nothing.
	// Here a wrong renderer writes "consume 100g", which reads as an item
	// literally called "100g".
	raw := RawCommand{Op: "consume", Fields: map[string]string{
		"qty":    "100g",
		"reason": "dinner",
	}}
	back, err := Parse(Line(raw))
	if err != nil {
		t.Fatalf("%s: %v", Line(raw), err)
	}
	if !reflect.DeepEqual(back.Fields, raw.Fields) {
		t.Errorf("via %s\n  want %v\n  got  %v", Line(raw), raw.Fields, back.Fields)
	}
}

// A value that spells one of the command's own keys must not be read as
// starting a pair.
func TestLineQuotesAValueThatLooksLikeAKey(t *testing.T) {
	// The value sits in a POSITIONAL slot, which is the only place it is
	// dangerous: after an explicit key, parse takes the next token as the value
	// whatever it spells. Unquoted, "acquire at 2bag" reads "at" as a key and
	// puts the quantity in the location.
	raw := RawCommand{Op: "acquire", Fields: map[string]string{
		"item": "at",
		"qty":  "2bag",
	}}
	back, err := Parse(Line(raw))
	if err != nil {
		t.Fatalf("%s: %v", Line(raw), err)
	}
	if back.Fields["item"] != "at" {
		t.Errorf("via %s: item = %q, fields %v", Line(raw), back.Fields["item"], back.Fields)
	}
}

func TestFieldAtNamesTheSlotBeingTyped(t *testing.T) {
	for _, tc := range []struct {
		line    string
		want    string
		partial string
	}{
		{"acquire Bas", "item", "Bas"},
		{"acquire \"Basmati Rice\" ", "qty", ""},
		{"acquire \"Basmati Rice\" 2bag at Kit", "at", "Kit"},
		{"acquire \"Basmati Rice\" 2bag at \"Kitchen\" from cor", "from", "cor"},
		{"consume rice 100g reason din", "reason", "din"},
	} {
		f, partial, ok := FieldAt(tc.line)
		if !ok {
			t.Errorf("%q: no field", tc.line)
			continue
		}
		if f.Key != tc.want || partial != tc.partial {
			t.Errorf("%q: got %s/%q, want %s/%q", tc.line, f.Key, partial, tc.want, tc.partial)
		}
	}
}
