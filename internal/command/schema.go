package command

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The agent contract.
//
// `hms import` writes this into an import's schema/ directory, and `hmsdev
// schema` prints it. Either way it is the vocabulary, so that an agent targets
// a FIXED spec rather than a remembered one. It is generated from the same
// specs Bind reads, so it
// cannot drift: a command whose schema says one thing and whose binder does
// another is the failure this exists to prevent, and it is unrepresentable
// here rather than merely tested for.

// SchemaField is one field, as an agent sees it.
type SchemaField struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Required   bool     `json:"required"`
	Positional bool     `json:"positional"`
	Refers     []string `json:"refers,omitempty"`
	Choices    []string `json:"choices,omitempty"`
	What       string   `json:"what"`
}

// SchemaCommand is one command.
type SchemaCommand struct {
	Op      string        `json:"op"`
	What    string        `json:"what"`
	Creates string        `json:"creates,omitempty"`
	Fields  []SchemaField `json:"fields"`
}

// Schema is the whole vocabulary.
type Schema struct {
	Commands []SchemaCommand `json:"commands"`
	Quantity []QuantityRule  `json:"quantity"`
	Rules    []string        `json:"rules"`
	// House is what the vocabulary can be written against TODAY: the categories
	// and the places that already exist. It is the half of the contract that
	// the code cannot know -- every other part of this is a property of the
	// program, and this part is a property of the household.
	House House `json:"house"`
}

// House is the existing classification and the existing places, by path.
//
// Exported WITH the schema, in one document, because the two are useless apart.
// A schema alone tells an agent how to write a row and nothing about where the
// row should point, so it invents "Pantry" when the house calls it
// "Kitchen > Left Pantry" -- and every one of those rows comes back as a name
// that does not resolve, or worse, as an offer to create a second Pantry. The
// paths are here so that naming what exists is the easy thing to do.
type House struct {
	Categories []string `json:"categories"`
	Locations  []string `json:"locations"`
}

// QuantityRule is one spelling of an amount and what it means.
//
// Included because quantity is where most ambiguity lives, and an agent that
// guesses here produces a receipt that is wrong by a factor of a thousand
// without looking wrong at all.
type QuantityRule struct {
	Written string `json:"written"`
	Means   string `json:"means"`
}

// NewSchema builds the vocabulary from the registry.
func NewSchema() Schema {
	schema := Schema{
		Quantity: []QuantityRule{
			{"100g, 100 g, 1.5kg", "that amount in a named unit; converted, and REJECTED if inexact"},
			{"100", "100 of the item's own unit -- not a guess, a different claim"},
			{"2bag, 2pkg, 2package", "2 WHOLE packages; refused if the item has no package size"},
		},
		Rules: []string{
			"Never emit an identifier. Names and paths only -- you cannot fabricate a reference to something that does not exist.",
			"One row per action. Do not combine, so a row's failure stays that row's.",
			"Leave a field blank rather than guessing. Blank blocks and asks; a guess applies.",
			"Prefer acquire over `new item`. The system asks about creation; guessing `counting` is the one error that is permanent.",
			"Never assume a unit. Copy what the receipt says: 100 and 100g mean different things, and only one is on the receipt.",
			"A path is written with ` > ` between its parts: Kitchen > Left Pantry > Shelf 1.",
		},
	}
	for _, spec := range Specs() {
		schema.Commands = append(schema.Commands, describeSpec(spec))
	}
	return schema
}

// WithHouse adds what already exists to the vocabulary.
//
// Separate from NewSchema, and a value rather than a lookup, because the
// schema is otherwise a property of the CODE: the vocabulary can be answered
// without a database, and the tests that walk the vocabulary must not need a
// household to walk it against.
func (s Schema) WithHouse(house House) Schema {
	s.House = house
	return s
}

func describeSpec(spec Spec) SchemaCommand {
	out := SchemaCommand{Op: string(spec.Op), What: spec.What, Creates: string(spec.Creates)}
	for _, field := range spec.Fields {
		described := SchemaField{
			Name: field.Key, Type: string(field.Type), What: field.What,
			Required: field.Required, Positional: field.Positional,
			Choices: field.Choices,
		}
		for _, kind := range field.Kinds {
			described.Refers = append(described.Refers, string(kind))
		}
		out.Fields = append(out.Fields, described)
	}
	return out
}

// WriteJSON emits the schema for a program to read.
func (s Schema) WriteJSON(w io.Writer) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(s)
}

// WritePrompt emits the schema as instructions to paste into a system prompt.
//
// The same content in the same order as the JSON, because two renderings that
// could disagree would mean an agent reading one and a person checking the
// other were looking at different contracts.
func (s Schema) WritePrompt(w io.Writer) error {
	var b strings.Builder

	b.WriteString("You are writing rows for a household inventory. Emit CSV or JSON Lines.\n")
	b.WriteString("Every row is one command. The column names and the JSON keys are the\n")
	b.WriteString("field names below -- there is nothing else to learn.\n\n")

	b.WriteString("RULES\n")
	for _, rule := range s.Rules {
		fmt.Fprintf(&b, "  - %s\n", rule)
	}

	b.WriteString("\nQUANTITIES\n")
	for _, rule := range s.Quantity {
		fmt.Fprintf(&b, "  %-24s %s\n", rule.Written, rule.Means)
	}

	b.WriteString("\nCOMMANDS\n")
	for _, cmd := range s.Commands {
		fmt.Fprintf(&b, "\n  %s -- %s\n", cmd.Op, cmd.What)
		if cmd.Creates != "" {
			fmt.Fprintf(&b, "    creates a %s, so it always stops and asks\n", strings.ToLower(cmd.Creates))
		}
		for _, field := range cmd.Fields {
			b.WriteString("    " + describeField(field) + "\n")
		}
	}

	s.writeHouse(&b)

	_, err := io.WriteString(w, b.String())
	return err
}

// writeHouse lists what exists, as paths, under each heading.
//
// Written out in full rather than summarised. A list of forty shelves is a few
// hundred words and it is the difference between a row that resolves and a row
// somebody has to fix by hand; there is nothing to be gained by making an agent
// guess at the half we could simply have told it.
func (s Schema) writeHouse(b *strings.Builder) {
	write := func(heading, kind string, paths []string) {
		fmt.Fprintf(b, "\n%s  (write one of these paths exactly)\n", heading)
		if len(paths) == 0 {
			fmt.Fprintf(b, "  -- there are none yet, so every %s a row names will be created and reviewed\n", kind)
			return
		}
		for _, path := range paths {
			fmt.Fprintf(b, "  %s\n", path)
		}
	}
	write("CATEGORIES THAT EXIST", "classification", s.House.Categories)
	write("LOCATIONS THAT EXIST", "place", s.House.Locations)
}

// ExportName is what an exported schema is called inside the directory it is
// written to. One name, so a second export replaces the first rather than
// leaving two files that differ by a date and disagree about the house.
func ExportName(format string) string {
	if format == "json" {
		return "import-schema.json"
	}
	return "import-schema.txt"
}

// Export writes the schema into a directory, and reports the file it wrote.
//
// A directory rather than a stream, because this is the one output of the
// program that is meant to be handed to something else. `hms import` writes it
// into the schema/ of the import it just made, next to the photograph of the
// receipt -- and asking somebody to redirect stdout into the right place is
// asking them to get it wrong once.
//
// The directory is created if it is not there. Writing the schema is not a
// destructive act -- it is generated, and generating it again produces the same
// bytes unless the house has changed -- so it replaces a file of the same name
// without asking.
func (s Schema) Export(dir, format string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", fmt.Errorf("schema: no directory to write to")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("schema: %w", err)
	}

	var b strings.Builder
	var err error
	if format == "json" {
		err = s.WriteJSON(&b)
	} else {
		err = s.WritePrompt(&b)
	}
	if err != nil {
		return "", err
	}

	path := filepath.Join(dir, ExportName(format))
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", fmt.Errorf("schema: %w", err)
	}
	return path, nil
}

// namesA reads as English. "names a item" on a contract an agent is told to
// follow exactly reads as carelessness about the rest of it.
func namesA(kinds []string) string {
	out := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		lower := strings.ToLower(kind)
		article := "a "
		if strings.ContainsAny(lower[:1], "aeiou") {
			article = "an "
		}
		out = append(out, article+lower)
	}
	return strings.Join(out, " or ")
}

func describeField(field SchemaField) string {
	notes := []string{field.Type}
	if field.Required {
		notes = append(notes, "required")
	}
	if len(field.Refers) > 0 {
		notes = append(notes, "names "+namesA(field.Refers))
	}
	if len(field.Choices) > 0 {
		notes = append(notes, "one of "+strings.Join(field.Choices, "|"))
	}
	return fmt.Sprintf("%-12s %-44s %s", field.Name, "("+strings.Join(notes, ", ")+")", field.What)
}

// Ops lists every op the schema covers, sorted, for a test that walks them.
func (s Schema) Ops() []string {
	out := make([]string, 0, len(s.Commands))
	for _, cmd := range s.Commands {
		out = append(out, cmd.Op)
	}
	sort.Strings(out)
	return out
}
