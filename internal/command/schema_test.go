package command_test

import (
	"encoding/json"
	"strings"
	"testing"

	"home-management-system/internal/command"
)

// TestTheSchemaCoversEveryOp is the exit criterion: an agent targets a fixed
// spec, and a spec that omits a command is a command the agent will never use
// or, worse, will invent a syntax for.
//
// Generated from the same specs Bind reads, so it cannot drift -- a command
// whose schema says one thing and whose binder does another is unrepresentable
// here rather than merely tested for.
func TestTheSchemaCoversEveryOp(t *testing.T) {
	schema := command.NewSchema()
	covered := map[string]bool{}
	for _, op := range schema.Ops() {
		covered[op] = true
	}
	for _, c := range command.AllCommands {
		if !covered[string(c.Op())] {
			t.Errorf("%q is a command and is not in the schema", c.Op())
		}
	}
	if len(schema.Ops()) != len(command.AllCommands) {
		t.Errorf("%d ops in the schema, %d commands", len(schema.Ops()), len(command.AllCommands))
	}
}

// Every field of every command appears, with the things an agent has to know to
// fill it in: whether it is required, what it refers to, and what its legal
// values are.
func TestEveryFieldIsDescribed(t *testing.T) {
	for _, cmd := range command.NewSchema().Commands {
		spec, ok := command.SpecOf(command.Op(cmd.Op))
		if !ok {
			t.Fatalf("%q has no spec", cmd.Op)
		}
		if len(cmd.Fields) != len(spec.Fields) {
			t.Errorf("%q: %d fields in the schema, %d in the spec", cmd.Op, len(cmd.Fields), len(spec.Fields))
		}
		for _, field := range cmd.Fields {
			if field.What == "" {
				t.Errorf("%q.%s has no description", cmd.Op, field.Name)
			}
			if field.Type == string(command.FieldName) && len(field.Refers) == 0 {
				t.Errorf("%q.%s names something and does not say what", cmd.Op, field.Name)
			}
			if field.Type == string(command.FieldChoice) && len(field.Choices) == 0 {
				t.Errorf("%q.%s is a choice with nothing to choose from", cmd.Op, field.Name)
			}
		}
	}
}

// The creating commands say so, because guessing `counting` is the one error
// that is permanent and an agent has to know which rows carry that risk.
func TestTheSchemaSaysWhichCommandsCreate(t *testing.T) {
	creating := 0
	for _, cmd := range command.NewSchema().Commands {
		if cmd.Creates != "" {
			creating++
			if !strings.HasPrefix(cmd.Op, "new ") {
				t.Errorf("%q says it creates a %s", cmd.Op, cmd.Creates)
			}
		}
	}
	if creating != 4 {
		t.Errorf("%d commands create something, want the four `new …` ones", creating)
	}
}

// The two renderings carry the same content in the same order. An agent reading
// one and a person checking the other must not be looking at different
// contracts.
func TestBothRenderingsAgree(t *testing.T) {
	schema := command.NewSchema()

	var asJSON strings.Builder
	if err := schema.WriteJSON(&asJSON); err != nil {
		t.Fatal(err)
	}
	var decoded command.Schema
	if err := json.Unmarshal([]byte(asJSON.String()), &decoded); err != nil {
		t.Fatalf("the JSON is not readable: %v", err)
	}
	if len(decoded.Commands) != len(schema.Commands) {
		t.Errorf("%d commands survived the round trip, want %d", len(decoded.Commands), len(schema.Commands))
	}

	var asPrompt strings.Builder
	if err := schema.WritePrompt(&asPrompt); err != nil {
		t.Fatal(err)
	}
	prompt := asPrompt.String()
	for _, cmd := range schema.Commands {
		if !strings.Contains(prompt, cmd.Op) {
			t.Errorf("the prompt form omits %q", cmd.Op)
		}
		for _, field := range cmd.Fields {
			if !strings.Contains(prompt, field.Name) {
				t.Errorf("the prompt form omits %q.%s", cmd.Op, field.Name)
			}
		}
	}
	// And the rules an agent must follow, which are the whole reason a prompt
	// form exists rather than just the JSON.
	for _, want := range []string{
		"Never emit an identifier", "Leave a field blank rather than guessing",
		"Never assume a unit", "Prefer acquire",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt form omits the rule %q", want)
		}
	}
}

// Quantity is where most ambiguity lives, and an agent that guesses here
// produces a receipt wrong by a factor of a thousand without looking wrong.
func TestTheQuantityGrammarIsSpeltOut(t *testing.T) {
	var b strings.Builder
	if err := command.NewSchema().WritePrompt(&b); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"100g", "2bag", "WHOLE packages", "own unit"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("the quantity grammar omits %q", want)
		}
	}
}
