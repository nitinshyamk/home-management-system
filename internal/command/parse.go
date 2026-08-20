package command

import (
	"errors"
	"fmt"
	"strings"
)

// The `:` line: text in, RawCommand out. Syntax only -- nothing here knows what
// a name refers to or whether a quantity is legal.
//
//	command := op arg* pair*
//	arg     := value                 positional, in the order the op declares
//	pair    := key value             everything else, order-free
//	value   := bare | "quoted"
//
// The split between this and Bind is the split between "is this a sentence" and
// "is it true". Keeping them apart is what lets the CSV importer skip this
// entirely and hand Bind its columns directly, which is why a CSV row and the
// equivalent typed line cannot mean different things.

// ErrSyntax reports a line that is not a command.
var ErrSyntax = errors.New("command: cannot read line")

// Parse reads one typed line.
func Parse(line string) (RawCommand, error) { return parse(line, "") }

// parse reads a line, optionally skipping one positional slot because the
// caller is going to supply it from context.
//
// Skipping is what makes `:consume 100g` mean what it looks like. Without it
// the parser assigns positionals strictly in order, so "100g" lands in the item
// slot and the quantity is missing -- the line reads as naming an item called
// "100g", which is exactly what it said.
func parse(line string, skip string) (RawCommand, error) {
	tokens, err := tokenise(line)
	if err != nil {
		return RawCommand{}, err
	}
	if len(tokens) == 0 {
		return RawCommand{}, fmt.Errorf("%w: nothing to do", ErrSyntax)
	}

	op, rest, ok := matchOp(tokens)
	if !ok {
		return RawCommand{}, fmt.Errorf("%w: %q is not a command", ErrSyntax, tokens[0].text)
	}
	spec, _ := SpecOf(op)

	raw := RawCommand{Op: string(op), Fields: map[string]string{}}
	positional := spec.Positional()
	if skip != "" {
		remaining := positional[:0:0]
		for _, f := range positional {
			if f.Key != skip {
				remaining = append(remaining, f)
			}
		}
		positional = remaining
	}

	for len(rest) > 0 {
		// A bare token that names one of this command's pair keys starts a
		// pair, and everything after it is pairs. Quoting escapes that, which
		// is how a thing genuinely called "at" is still nameable.
		if !rest[0].quoted {
			if f, ok := spec.Field(strings.ToLower(rest[0].text)); ok && !isNextPositional(positional, f) {
				key := strings.ToLower(rest[0].text)
				if len(rest) < 2 {
					return RawCommand{}, fmt.Errorf("%w: %q has no value", ErrSyntax, key)
				}
				if _, repeated := raw.Fields[key]; repeated {
					return RawCommand{}, fmt.Errorf("%w: %q given twice", ErrSyntax, key)
				}
				raw.Fields[key] = rest[1].text
				rest = rest[2:]
				continue
			}
		}
		if len(positional) == 0 {
			return RawCommand{}, fmt.Errorf("%w: %q does not take %q here", ErrSyntax, op, rest[0].text)
		}
		raw.Fields[positional[0].Key] = rest[0].text
		positional, rest = positional[1:], rest[1:]
	}
	return raw, nil
}

// isNextPositional reports whether a field is the very next positional slot, so
// that `rename to "Larder"` puts "to" in the target slot rather than treating it
// as a key -- the token is where a value is expected, so it is one.
func isNextPositional(remaining []Field, f Field) bool {
	return len(remaining) > 0 && remaining[0].Key == f.Key
}

// matchOp takes the longest op that the line starts with. Two-word ops are
// tried before one-word ones, so "archive location" cannot be read as "archive"
// with a stray word after it.
func matchOp(tokens []token) (Op, []token, bool) {
	for _, op := range Ops() {
		words := strings.Fields(string(op))
		if len(words) > len(tokens) {
			continue
		}
		match := true
		for i, w := range words {
			if tokens[i].quoted || !strings.EqualFold(tokens[i].text, w) {
				match = false
				break
			}
		}
		if match {
			return op, tokens[len(words):], true
		}
	}
	return "", nil, false
}

// token remembers whether it was quoted, because that is what distinguishes a
// value from a key.
type token struct {
	text   string
	quoted bool
}

func tokenise(line string) ([]token, error) {
	var (
		out     []token
		current strings.Builder
		inWord  bool
		inQuote bool
	)
	flush := func(quoted bool) {
		out = append(out, token{text: current.String(), quoted: quoted})
		current.Reset()
		inWord = false
	}
	for _, r := range line {
		switch {
		case inQuote && r == '"':
			inQuote = false
			flush(true)
		case inQuote:
			current.WriteRune(r)
		case r == '"':
			// A quote mid-word ends the bare part first, so `at"x"` is two
			// tokens rather than a silently merged one.
			if inWord {
				flush(false)
			}
			inQuote = true
			inWord = true
		case r == ' ' || r == '\t':
			if inWord {
				flush(false)
			}
		default:
			inWord = true
			current.WriteRune(r)
		}
	}
	if inQuote {
		return nil, fmt.Errorf("%w: unclosed quote", ErrSyntax)
	}
	if inWord {
		flush(false)
	}
	return out, nil
}
