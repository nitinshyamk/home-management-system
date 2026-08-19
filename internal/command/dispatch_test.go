package command_test

import (
	"context"
	"strings"
	"testing"

	"home-management-system/internal/app"
	"home-management-system/internal/command"
	"home-management-system/internal/ops"
	"home-management-system/internal/testsupport"
)

// plan runs one Command through the dispatcher against an empty database.
func plan(t *testing.T, c command.Command) (ops.Batch, error) {
	t.Helper()
	conn := testsupport.NewDB(t)
	return app.Plan(context.Background(), ops.NewPlanner(conn), c)
}

// isUnhandled distinguishes "the dispatcher has no case for this" from "the
// planner considered it and said no", which is the whole point of the check.
func isUnhandled(err error) bool {
	return strings.Contains(err.Error(), "no plan for command")
}

// There is deliberately no test that reaches the dispatcher's fallthrough with
// a made-up Command: Command is sealed by an unexported method, so no package
// outside internal/command can implement one, and an in-package test cannot
// import app without a cycle. The fallthrough is unreachable by construction.
//
// That makes TestEveryCommandPlans a test whose failure mode has to be
// demonstrated rather than triggered, which is done the way every other guard
// here is -- by deleting a case from the dispatcher and watching it fail.
