package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"home-management-system/internal/app"
	"home-management-system/internal/command"
	"home-management-system/internal/config"
	"home-management-system/internal/intake"
	"home-management-system/internal/tui"
)

// `hms import` is the whole bulk flow, and it is one command because it is one
// intention. It used to be three: `hms schema --out DIR` to get the contract,
// something outside hms to produce a file, then `hms import FILE` to review it
// -- with nothing tying the three together, so the file being reviewed could
// have been written against a contract from a different house and nothing in
// the program would have noticed.
//
// Now the folder is the thread. hms makes it, writes the contract into it
// against the house as it is now, waits while a person fills it, takes the plan
// out of it, and opens the review screen on that. The steps in the middle still
// happen elsewhere -- they have to; reading a photograph is not something an
// inventory does -- but what goes in and what comes back are the same folder.

// errStopped is a person ending the workflow at a prompt. main treats it as a
// clean exit: "you stopped" is not news to the person who stopped.
var errStopped = intake.ErrStopped

func runImport(ctx context.Context, ctrl app.Controller, name string) error {
	root, err := config.ImportsDir()
	if err != nil {
		return err
	}
	house, err := app.House(ctx, ctrl)
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	guide := &intake.Guide{
		Root:        root,
		Contract:    command.NewSchema().WithHouse(house),
		Planner:     intake.NewShell(cfg.ImportPlanner, os.Stdout),
		PlannerHint: config.FileName,
		In:          os.Stdin,
		Out:         os.Stdout,
	}

	plan, err := guide.Run(ctx, name)
	if err != nil {
		if errors.Is(err, intake.ErrStopped) {
			return errStopped
		}
		return err
	}

	// The review screen, on the plan the folder produced. Built before the
	// terminal is touched, so a plan that cannot be bound fails as a message on
	// the line below the walk rather than as a blank screen.
	model, err := tui.Import(ctx, ctrl, plan)
	if err != nil {
		return fmt.Errorf("%s: %w", plan, err)
	}
	return tui.RunModel(model)
}
