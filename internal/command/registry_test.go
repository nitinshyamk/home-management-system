package command_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"home-management-system/internal/command"
	"home-management-system/internal/ops"
)

// The registry's whole purpose is exhaustiveness, so these are the tests that
// make the generated list worth generating.
//
// This is an external test package importing internal/ops. That does NOT
// violate the boundary archlint enforces: `go list` reports test imports
// separately from a package's own, so the production package graph still has no
// edge from command to ops. The test needs both because what it checks is the
// correspondence BETWEEN them, which is exactly the kind of claim no single
// package can make about itself.

// TestEveryCommandHasADistinctOp is the cheap half: a variant that forgot its
// Op() answers with the zero value, and two that share one make the vocabulary
// ambiguous.
func TestEveryCommandHasADistinctOp(t *testing.T) {
	seen := map[command.Op]string{}
	for _, c := range command.AllCommands {
		name := reflect.TypeOf(c).Name()
		op := c.Op()
		if op == "" {
			t.Errorf("%s has no Op", name)
			continue
		}
		if prior, dup := seen[op]; dup {
			t.Errorf("%s and %s both answer to %q", prior, name, op)
			continue
		}
		seen[op] = name
	}
	if len(seen) != len(command.AllCommands) {
		t.Errorf("%d distinct ops for %d commands", len(seen), len(command.AllCommands))
	}
}

// TestEveryCommandHasAPlannerMethod is the load-bearing one, and it is the
// exit criterion for this substage: a command with nothing behind it is a
// vocabulary that lies.
//
// The correspondence is derived rather than declared. A Command variant's TYPE
// NAME is the name of the ops.Planner method that carries it out, so there is
// no table to keep in step -- adding a variant without an operation fails here,
// and so does renaming an operation out from under one.
//
// The signature is checked too, because that is what the dispatcher relies on:
// every planner method takes (context.Context, SomeRequest) and returns
// (ops.Batch, error). A method that did something else would be a command that
// cannot be executed generically.
func TestEveryCommandHasAPlannerMethod(t *testing.T) {
	planner := reflect.TypeOf(&ops.Planner{})
	var (
		ctxType   = reflect.TypeOf((*context.Context)(nil)).Elem()
		batchType = reflect.TypeOf(ops.Batch{})
		errType   = reflect.TypeOf((*error)(nil)).Elem()
	)

	for _, c := range command.AllCommands {
		name := reflect.TypeOf(c).Name()
		method, ok := planner.MethodByName(name)
		if !ok {
			t.Errorf("command %s (%q) has no ops.Planner.%s to carry it out", name, c.Op(), name)
			continue
		}
		sig := method.Type
		// In[0] is the receiver.
		if sig.NumIn() != 3 || sig.In(1) != ctxType {
			t.Errorf("ops.Planner.%s takes %v; want (context.Context, request)", name, signature(sig))
			continue
		}
		if sig.NumOut() != 2 || sig.Out(0) != batchType || sig.Out(1) != errType {
			t.Errorf("ops.Planner.%s returns %v; want (ops.Batch, error)", name, signature(sig))
		}
	}
}

// TestEveryCommandPlans proves the dispatcher's switch is total, which the
// compiler cannot: a missing case falls through to the default and would only
// be noticed by whoever typed that command.
//
// It plans a ZERO command of every variant against an empty database, so almost
// all of them fail -- and that is fine. What is being checked is that none of
// them reaches the "no plan for command" fallthrough. A planner refusing a
// nonsensical request is the planner working.
func TestEveryCommandPlans(t *testing.T) {
	for _, c := range command.AllCommands {
		name := reflect.TypeOf(c).Name()
		_, err := plan(t, c)
		if err != nil && isUnhandled(err) {
			t.Errorf("no dispatch case for %s (%q)", name, c.Op())
		}
	}
}

func signature(t reflect.Type) string {
	var in []string
	for i := 1; i < t.NumIn(); i++ {
		in = append(in, t.In(i).String())
	}
	var out []string
	for i := 0; i < t.NumOut(); i++ {
		out = append(out, t.Out(i).String())
	}
	return "(" + join(in) + ") (" + join(out) + ")"
}

func join(xs []string) string {
	s := ""
	for i, x := range xs {
		if i > 0 {
			s += ", "
		}
		s += x
	}
	return s
}

// TestEveryCreatingCommandSaysSo pins the declaration, because it cannot be
// inferred: a `new …` command BINDS perfectly well -- its name is text, not a
// reference -- so nothing about the result says a thing is about to exist.
//
// A plan screen that waited for a failure to tell it would let creation
// through silently, which is the one thing an import must never do.
func TestEveryCreatingCommandSaysSo(t *testing.T) {
	for _, c := range command.AllCommands {
		spec, ok := command.SpecOf(c.Op())
		if !ok {
			continue
		}
		creates := strings.HasPrefix(string(c.Op()), "new ")
		if creates && spec.Creates == "" {
			t.Errorf("%q creates something and does not say so", c.Op())
		}
		if !creates && spec.Creates != "" {
			t.Errorf("%q says it creates a %s and its name says otherwise", c.Op(), spec.Creates)
		}
	}
}
