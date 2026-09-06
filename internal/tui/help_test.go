package tui

import (
	"strings"
	"testing"

	"home-management-system/internal/command"
	"home-management-system/internal/tui/keys"
)

func helpText(topic string) string {
	m := Model{width: 100}
	return strings.Join(m.helpLines(topic), "\n")
}

// Every command is listed, because the listing walks the vocabulary.
//
// A hand-written list of commands is a list that is wrong one release after it
// is written -- and it is the one kind of documentation nobody re-reads,
// because the people who would spot the drift already know the answer.
func TestTheGeneralHelpListsEveryCommand(t *testing.T) {
	got := helpText("")
	for _, spec := range command.Specs() {
		if !strings.Contains(got, string(spec.Op)) {
			t.Errorf("the help does not mention %q", spec.Op)
		}
	}
	// Including the one that is not a Command.
	if !strings.Contains(got, "help") {
		t.Error("the help does not mention itself")
	}
}

// And every key it names is a key something actually binds.
//
// The keys are read from the keymap rather than written beside the words, so
// this cannot drift -- but the test is what says so, and it is what would catch
// a section left behind after a rebinding.
// Styling is off in a test (lipgloss falls back to the Ascii profile with no
// TTY), so the lines here are the plain text the terminal would print.
func TestTheGeneralHelpNamesOnlyRealKeys(t *testing.T) {
	bound := map[string]bool{"1 - 5": true}
	for _, ctx := range keys.Contexts() {
		for _, group := range keys.Bindings(ctx) {
			for _, key := range group {
				bound[keys.Display(key)] = true
			}
		}
	}
	m := Model{width: 100}
	for _, line := range m.helpLines("") {
		if !strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "     ") {
			continue // a heading, or a wrapped description
		}
		// A key row is a key column padded out and then two spaces before what
		// it means, so the two spaces are what says there is a key column at
		// all. Prose sits at the same indent and has none, and reading a whole
		// sentence as a keystroke is how this test first reported that "What is
		// in hand is named at the top of the screen" is not bound to anything.
		column, _, isRow := strings.Cut(strings.TrimSpace(line), "  ")
		if !isRow {
			continue
		}
		for _, key := range strings.Split(column, " / ") {
			if key == "" || bound[key] {
				continue
			}
			// Command names share the column with keys; those are checked by
			// the test above.
			if _, isCommand := command.SpecOf(command.Op(key)); isCommand || key == "help" {
				continue
			}
			t.Errorf("the help names %q, which nothing binds", key)
		}
	}
}

// One command's help says what it is for, how it is written, and what each of
// its fields will accept.
func TestCommandHelpDescribesTheFields(t *testing.T) {
	got := helpText("acquire")
	for _, want := range []string{
		"acquire <item>", // how it is written, positionals in order
		"[at <name>]",    // and the pair-only ones as key and value
		"(required)",     // which of them Bind will insist on
		"names an Item",  // what a name field refers to
		"quantity",       // and what the others parse as
	} {
		if !strings.Contains(got, want) {
			t.Errorf("help for acquire is missing %q:\n%s", want, got)
		}
	}
}

// A command that brings something into existence says so, because that is the
// fact that makes it permanent.
func TestCommandHelpSaysWhatItCreates(t *testing.T) {
	got := helpText("new item")
	if !strings.Contains(got, "IT CREATES") {
		t.Errorf("help for `new item` does not say it creates anything:\n%s", got)
	}
}

// A near miss is answered with the near names, through the same ranked matcher
// the completion dropdowns use.
func TestUnknownTopicsOfferTheNearestNames(t *testing.T) {
	got := helpText("consu")
	if !strings.Contains(got, "consume") {
		t.Errorf("help for a near miss does not offer consume:\n%s", got)
	}
	if !strings.Contains(got, "nothing called") {
		t.Errorf("help for a near miss does not say it is not a command:\n%s", got)
	}
}

// help alone is everything; help <name> is one thing; anything else is a topic
// rather than a line for the parser -- which is the right place for somebody
// asking what the commands are.
func TestHelpAsked(t *testing.T) {
	for line, want := range map[string]string{
		"help":            "",
		"  help  ":        "",
		"HELP":            "",
		"help acquire":    "acquire",
		"help new item":   "new item",
		"help frobnicate": "frobnicate",
	} {
		topic, ok := helpAsked(line)
		if !ok {
			t.Errorf("%q was not read as a request for help", line)
			continue
		}
		if topic != want {
			t.Errorf("%q asked about %q, want %q", line, topic, want)
		}
	}
	for _, line := range []string{"", "consume 100", "helpful 2"} {
		if _, ok := helpAsked(line); ok {
			t.Errorf("%q was read as a request for help", line)
		}
	}
}

// Nothing in the help runs past the terminal. A line wider than the screen
// wraps, and one wrapped line shifts every row below it.
func TestTheHelpFitsTheTerminal(t *testing.T) {
	for _, width := range []int{60, 80, 100, 120} {
		m := Model{width: width}
		for _, topic := range []string{"", "acquire", "new item", "frobnicate"} {
			for _, line := range m.helpLines(topic) {
				if n := len([]rune(line)); n > width {
					t.Errorf("a %s help line is %d columns in a %d-column terminal: %q",
						topic, n, width, line)
				}
			}
		}
	}
}
