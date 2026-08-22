package command

import (
	"sort"
	"strings"
)

// A RawCommand written back out as a line, and the inverse of parse.go.
//
// This exists because the import plan screen lets a person edit a row, and the
// thing they edit is the COMMAND -- not a form over its fields. One grammar,
// three surfaces (spec.go): a row of a CSV and a typed `:` line are the same
// sentence, so correcting one is correcting the other, and a row that was
// fixed by hand goes through exactly the Parse and Bind a right-first-time row
// went through.
//
// The property that makes it safe is Parse(Line(raw)) == raw, asserted over
// every op in render_test.go. Without it, opening a row for editing and
// pressing enter unchanged could silently alter it -- which is the one thing a
// review screen must never do.

// Line renders a RawCommand as the line a person edits.
//
// Positional fields are written bare only while they are contiguously present:
// the first gap ends it, because a later value written bare would land in the
// empty slot and mean something else. Everything after that gap is written as
// an explicit key and value, which is order-free and cannot shift.
func Line(raw RawCommand) string {
	spec, known := SpecOf(Op(strings.ToLower(raw.Op)))
	if !known {
		// Nothing declares this op's shape, so there are no positionals to
		// write bare. Parse will refuse it again -- which is right: the person
		// is looking at this line precisely because the op was not understood.
		parts := []string{raw.Op}
		for _, key := range sortedKeys(raw.Fields) {
			parts = append(parts, key, quoteFor(spec, raw.Fields[key]))
		}
		return strings.Join(parts, " ")
	}

	parts := []string{string(spec.Op)}
	written := map[string]bool{}
	for _, f := range spec.Positional() {
		value := strings.TrimSpace(raw.Fields[f.Key])
		if value == "" {
			break
		}
		parts = append(parts, quoteFor(spec, value))
		written[f.Key] = true
	}
	for _, f := range spec.Fields {
		if written[f.Key] {
			continue
		}
		if value := strings.TrimSpace(raw.Fields[f.Key]); value != "" {
			parts = append(parts, f.Key, quoteFor(spec, value))
		}
	}
	// Fields the spec does not declare are kept rather than dropped. An unknown
	// column is why some rows are on this screen at all, and a line that
	// silently discarded it would leave the person correcting something they
	// cannot see.
	for _, key := range sortedKeys(raw.Fields) {
		if _, declared := spec.Field(key); declared {
			continue
		}
		parts = append(parts, key, quoteFor(spec, raw.Fields[key]))
	}
	return strings.Join(parts, " ")
}

// Quote wraps a value so it survives tokenise as ONE token.
func Quote(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, "") + `"`
}

// quoteFor quotes when a bare value would not come back the same way: when it
// holds whitespace or a quote, and when it happens to spell one of this
// command's own field keys, which parse would otherwise read as starting a pair.
func quoteFor(spec Spec, value string) string {
	if strings.ContainsAny(value, " \t\"") {
		return Quote(value)
	}
	if _, isKey := spec.Field(strings.ToLower(value)); isKey {
		return Quote(value)
	}
	if value == "" {
		return Quote(value)
	}
	return value
}

func sortedKeys(fields map[string]string) []string {
	out := make([]string, 0, len(fields))
	for key, value := range fields {
		if strings.TrimSpace(value) != "" {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// FieldAt says which field the value being typed at the end of a line belongs
// to, so a half-written line can be completed against the right kind of thing.
//
// It walks the line exactly as parse does and stops at the token in progress.
// Sharing that walk is the point: a completer with its own idea of which slot
// the cursor is in would offer Locations where the parser will read an Item.
func FieldAt(line string) (Field, string, bool) {
	tokens, err := tokenise(line)
	if err != nil {
		return Field{}, "", false
	}
	// A trailing space means the previous token is finished and a new, empty
	// one is being started.
	partial := ""
	if len(tokens) > 0 && !strings.HasSuffix(line, " ") && !strings.HasSuffix(line, "\t") {
		partial = tokens[len(tokens)-1].text
		tokens = tokens[:len(tokens)-1]
	}

	op, rest, ok := matchOp(tokens)
	if !ok {
		return Field{}, "", false
	}
	spec, _ := SpecOf(op)
	positional := spec.Positional()

	for len(rest) > 0 {
		if !rest[0].quoted {
			if f, isKey := spec.Field(strings.ToLower(rest[0].text)); isKey && !isNextPositional(positional, f) {
				if len(rest) < 2 {
					// The key is written and its value is what is being typed.
					return f, partial, true
				}
				rest = rest[2:]
				continue
			}
		}
		if len(positional) == 0 {
			return Field{}, "", false
		}
		positional, rest = positional[1:], rest[1:]
	}
	if len(positional) > 0 {
		return positional[0], partial, true
	}
	return Field{}, "", false
}
