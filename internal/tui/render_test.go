package tui

import (
	"strings"
	"testing"
)

// The script format has to be able to express every key the interface uses,
// including the ones that collide with its own syntax. It could not express a
// bare `:` -- the command leader -- and the failure was silent enough to commit
// an empty frames file.
func TestAScriptCanExpressAColon(t *testing.T) {
	steps, err := parseScript(strings.NewReader(
		"4: the holdings table\n" +
			"/\n" +
			":\n" +
			"#a comment\n" +
			"\n" +
			"enter: apply\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(steps) != 4 {
		t.Fatalf("got %d steps, want 4", len(steps))
	}
	if got := steps[0].label; got != "the holdings table" {
		t.Errorf("label = %q", got)
	}
	if got := steps[1].key.String(); got != "/" {
		t.Errorf("step 2 = %q, want /", got)
	}
	if got := steps[2].key.String(); got != ":" {
		t.Errorf("step 3 = %q, want a colon", got)
	}
	if got := steps[3].key.String(); got != "enter" {
		t.Errorf("step 4 = %q", got)
	}
}

func TestAnUnknownKeyIsRefusedWithItsLine(t *testing.T) {
	_, err := parseScript(strings.NewReader("4\nnotakey\n"))
	if err == nil {
		t.Fatal("an unknown key parsed")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("the error does not say where: %v", err)
	}
}
