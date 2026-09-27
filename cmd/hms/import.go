package main

import (
	"context"
	"fmt"

	"home-management-system/internal/app"
	"home-management-system/internal/config"
	"home-management-system/internal/tui"
)

// `hms import` is the whole bulk flow, and it is one command because it is one
// intention. It used to be three: `hms schema --out DIR` to get the contract,
// something outside hms to produce a file, then `hms import FILE` to review it
// -- with nothing tying the three together, so the file being reviewed could
// have been written against a contract from a different house and nothing in
// the program would have noticed.
//
// The folder is the thread. hms makes it, writes the contract into it against
// the house as it is now, waits while a person fills it, takes the plan out of
// it, and reviews that. The steps in the middle still happen elsewhere -- they
// have to; reading a photograph is not something an inventory does -- but what
// goes in and what comes back are the same folder.
//
// # The walk is a screen now
//
// It used to be a conversation on stdin: hms printed the paths and blocked on
// `press enter when it is there`. The reasoning was that the pause in the
// middle of an import is measured in hours and a person spends it in a file
// manager, so a full-screen interface would be in the way.
//
// What that missed is that blocking is not the only way to wait. The drawer
// waits by staying drawn, with the house visible behind it and `r` to look
// again -- the same pause, without hms being unusable for the duration. And
// it makes the thing the folder is for true rather than asserted: you are
// importing against THIS house, and the house is on screen while you do it.
//
// So this command has one job left: open the interface, with the intake
// drawer up, on the import named or on the list of them.
func runImport(ctx context.Context, ctrl app.Controller, name string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	path, err := config.Path()
	if err != nil {
		return err
	}

	model, err := tui.ImportWalk(ctx, ctrl, name)
	if err != nil {
		return fmt.Errorf("import: %w", err)
	}
	return tui.RunModel(model.WithPlanner(cfg.ImportPlanner, path))
}
