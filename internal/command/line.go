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
	fillSubject(&raw, subject)
	return Bind(v, raw)
}

// fillSubject supplies the first required name field the line left out and the
// subject can answer for.
//
// The FIRST one: a command's positional fields are declared in the order they
// are written, so the earliest unfilled one is the one a person means when they
// leave the subject off the front.
func fillSubject(raw *RawCommand, subject Subject) {
	if subject.Name == "" || raw.Fields == nil {
		return
	}
	spec, ok := SpecOf(Op(raw.Op))
	if !ok {
		return
	}
	for _, field := range spec.Fields {
		if !field.Required || field.Type != FieldName {
			continue
		}
		if value, given := raw.Fields[field.Key]; given && strings.TrimSpace(value) != "" {
			continue
		}
		if !acceptsKind(field, subject.Kind) {
			continue
		}
		raw.Fields[field.Key] = subject.Name
		return
	}
}

func acceptsKind(field Field, kind string) bool {
	for _, k := range field.Kinds {
		if string(k) == kind {
			return true
		}
	}
	return false
}
