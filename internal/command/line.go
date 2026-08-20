package command

import "strings"

// The contextual `:` line.
//
// It lives here rather than in the layer above so that RawCommand never leaves
// this package -- archlint asserts that, and the assertion is what keeps the
// keystroke path honest: a keystroke already holds an identifier, and anything
// that could round-trip it through a name could resolve it to a DIFFERENT row
// than the one under the cursor.

// Subject is what the cursor is on, for the lines that leave it out.
type Subject struct {
	Kind string // "Item", "Location", "Category", "Holding", or "" for none
	Name string
	// At is where the row under the cursor keeps the thing, when the view has
	// such a column. A holdings row is about an Item IN A PLACE, and leaving
	// the place behind would make `:consume 100g` on a specific row ask which
	// of the three shelves you meant -- while pointing at one of them.
	At string
}

// BindLine parses a typed line, fills in the subject it omitted, and binds it.
//
// The contextual fill is what makes `:consume 100g` and the `c` keystroke the
// same Command rather than two ways of saying nearly the same thing. It fills by
// NAME rather than by identifier, so the line binds through exactly the same
// path a CSV row does -- handing Bind an identifier here would make the typed
// line a second contract that merely resembles the first.
func BindLine(v *Vocabulary, line string, subject Subject) (BindResult, error) {
	raw, err := Parse(line)
	if err != nil {
		return BindResult{}, err
	}

	// Whether the line MEANT to leave the subject out is a counting question:
	// a line that filled every positional slot named everything it wanted, and
	// one that came up short left the front of the sentence to context.
	//
	// So `:consume 100g` re-parses with the item slot skipped and takes the
	// subject from the cursor, while `:consume "Basmati Rice" 100g` is left
	// alone -- and a person who names the item explicitly gets the item they
	// named, even when the cursor is on something else.
	if field, ok := subjectField(raw.Op, subject); ok && !allPositionalsGiven(raw) {
		reparsed, err := parse(line, field)
		if err != nil {
			return BindResult{}, err
		}
		reparsed.Fields[field] = subject.Name
		raw = reparsed
	}
	fillPlace(&raw, subject)
	return Bind(v, raw)
}

// fillPlace supplies `at` from the row under the cursor.
//
// Only when the line left it out, so naming a different place still wins. `at`
// is optional rather than required -- Bind infers it when an Item is kept in
// exactly one place -- which is why it is filled here rather than by
// subjectField.
func fillPlace(raw *RawCommand, subject Subject) {
	if subject.At == "" || raw.Fields == nil {
		return
	}
	spec, ok := SpecOf(Op(raw.Op))
	if !ok {
		return
	}
	field, has := spec.Field("at")
	if !has || field.Type != FieldName {
		return
	}
	if value, given := raw.Fields["at"]; given && strings.TrimSpace(value) != "" {
		return
	}
	raw.Fields["at"] = subject.At
}

// subjectField is the first required name field the subject could answer for.
//
// The FIRST: a command's positional fields are declared in the order they are
// written, so the earliest one is what a person means by leaving the front of
// the line off.
func subjectField(op string, subject Subject) (string, bool) {
	if subject.Name == "" {
		return "", false
	}
	spec, ok := SpecOf(Op(op))
	if !ok {
		return "", false
	}
	for _, field := range spec.Fields {
		if field.Required && field.Type == FieldName && acceptsKind(field, subject.Kind) {
			return field.Key, true
		}
	}
	return "", false
}

// allPositionalsGiven reports whether the line filled every positional slot.
func allPositionalsGiven(raw RawCommand) bool {
	spec, ok := SpecOf(Op(raw.Op))
	if !ok {
		return true
	}
	for _, field := range spec.Positional() {
		if strings.TrimSpace(raw.Fields[field.Key]) == "" {
			return false
		}
	}
	return true
}

func acceptsKind(field Field, kind string) bool {
	for _, k := range field.Kinds {
		if string(k) == kind {
			return true
		}
	}
	return false
}
