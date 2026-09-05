package keys

import "testing"

// TestEveryBindingIsExpressible walks the whole keymap and checks that each key
// can be written down and read back.
//
// This replaces a hand-written list of every key the interface used, which is
// the sort of list that is correct on the day it is written. Walking the keymap
// means a binding cannot be added without the scripts and the harness being
// able to press it.
func TestEveryBindingIsExpressible(t *testing.T) {
	for _, ctx := range Contexts() {
		for _, group := range Bindings(ctx) {
			for _, key := range group {
				msg, ok := Named(key)
				if !ok {
					t.Errorf("context %d binds %q, which nothing can press", ctx, key)
					continue
				}
				if got := msg.String(); got != Canonical(key) {
					t.Errorf("%q pressed as %q", key, got)
				}
			}
		}
	}
}

// The readable aliases are the same keystroke by a friendlier name.
func TestReadableAliases(t *testing.T) {
	for name, want := range map[string]string{
		"space":      " ",
		"ctrl+space": "ctrl+@",
	} {
		msg, ok := Named(name)
		if !ok {
			t.Fatalf("%q is not a key", name)
		}
		if got := msg.String(); got != want {
			t.Errorf("%q is %q, want %q", name, got, want)
		}
	}
}

// A Meta keystroke is not text, and that is the whole of why IsText exists: tea
// reports M-w as runes with Alt set, so a field testing msg.Type alone typed a
// literal "w" whenever somebody reached for a Meta binding.
func TestMetaIsNotText(t *testing.T) {
	for _, name := range []string{"alt+w", "alt+v", "alt+x", "alt+<"} {
		msg, ok := Named(name)
		if !ok {
			t.Fatalf("%q is not a key", name)
		}
		if IsText(msg) {
			t.Errorf("%q would be typed into a field as text", name)
		}
	}
	plain, _ := Named("w")
	if !IsText(plain) {
		t.Error("a plain w is text")
	}
}

// No surface may bind one key to two actions. A duplicate is not a conflict the
// map reports -- the second silently wins -- so it is checked here instead.
func TestNoKeyMeansTwoThingsOnOneSurface(t *testing.T) {
	for _, ctx := range Contexts() {
		seen := map[string]bool{}
		for _, group := range Bindings(ctx) {
			for _, key := range group {
				if seen[key] {
					t.Errorf("context %d binds %q twice", ctx, key)
				}
				seen[key] = true
			}
		}
	}
}
