package command

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// The agent contract.
//
// `hms schema` emits the vocabulary so an agent targets a FIXED spec rather
// than a remembered one. It is generated from the same specs Bind reads, so it
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

	_, err := io.WriteString(w, b.String())
	return err
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
