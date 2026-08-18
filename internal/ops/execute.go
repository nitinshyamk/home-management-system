package ops

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"home-management-system/internal/annotate"
	"home-management-system/internal/db"
	"home-management-system/internal/domain"
	"home-management-system/internal/ledger"
	"home-management-system/internal/origin"
)

var (
	// ErrPlan reports a Step that is internally inconsistent -- it refers to
	// something it did not create. A planning bug, never user input.
	ErrPlan = errors.New("ops: malformed step")
)

// Executor applies batches.
type Executor struct {
	scope db.Scope
}

// New returns an Executor over the pool.
func New(conn *sql.DB) *Executor { return &Executor{scope: db.Pool(conn)} }

// Result reports what a Batch produced, per Step and in order.
type Result struct {
	Created []Created
	Events  [][]domain.EventID
}

// Execute applies every Step of a Batch inside ONE transaction.
//
// This is the only place in the system outside internal/db that opens one, and
// the reason the whole layer exists: an intent that spans write paths either
// happens completely or leaves no trace. Nothing here decides WHAT to write --
// that was settled when the Batch was planned, against a snapshot, by a pure
// function.
func (e *Executor) Execute(ctx context.Context, b Batch) (Result, error) {
	var out Result
	err := e.scope.Run(ctx, func(tx *sql.Tx) error {
		// The three write paths, all bound to this transaction. That binding is
		// the entire fix for the gap v01 left: origin.New(conn) wrote through
		// the pool, so an origination committed on its own no matter what
		// happened to the events that were supposed to accompany it.
		o := origin.NewTx(tx)
		l := ledger.NewTx(tx)
		a := annotate.NewTx(tx)

		out = Result{
			Created: make([]Created, len(b.Steps)),
			Events:  make([][]domain.EventID, len(b.Steps)),
		}
		for i, step := range b.Steps {
			created, ids, err := applyStep(ctx, o, l, a, step)
			if err != nil {
				return fmt.Errorf("step %d (%s): %w", i+1, step.Summary, err)
			}
			out.Created[i], out.Events[i] = created, ids
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	return out, nil
}

// applyStep runs one intent in the order origination, recording, annotation.
//
// The order is forced rather than chosen: recordings and annotations may refer
// to what the originations produced, so identity has to exist first.
func applyStep(
	ctx context.Context,
	o *origin.Originator,
	l *ledger.Processor,
	a *annotate.Annotator,
	step Step,
) (Created, []domain.EventID, error) {
	var created Created

	for _, orig := range step.Originates {
		if err := orig.originate(ctx, o, l, &created); err != nil {
			return created, nil, fmt.Errorf("originate %s: %w", orig.Describe(), err)
		}
	}

	var ids []domain.EventID
	if step.Records != nil {
		events, err := step.Records(created)
		if err != nil {
			return created, nil, err
		}
		if len(events) > 0 {
			ids, err = l.ApplyBatch(ctx, events)
			if err != nil {
				return created, nil, fmt.Errorf("record: %w", err)
			}
		}
	}

	if step.Annotates != nil {
		annotations, err := step.Annotates(created)
		if err != nil {
			return created, ids, err
		}
		for _, ann := range annotations {
			if err := ann.annotate(ctx, a); err != nil {
				return created, ids, fmt.Errorf("annotate %s: %w", ann.Describe(), err)
			}
		}
	}

	return created, ids, nil
}
