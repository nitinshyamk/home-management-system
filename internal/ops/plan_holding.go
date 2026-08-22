package ops

import (
	"context"
	"fmt"

	"home-management-system/internal/domain"
)

// The thin operations: each wraps a single event, or a single event plus the
// one consequence the domain says always accompanies it.
//
// They compose nothing, which is the point. They establish the Plan pattern --
// pure function, snapshot in, events out, no database anywhere -- against the
// cases where the only interesting content is which states permit the
// transition. The composing operations arrive next and reuse the shape.

// ---------------------------------------------------------------------------
// Custody: the Unique state machine
// ---------------------------------------------------------------------------

// CheckOutRequest displaces a Unique holding with intent to return.
type CheckOutRequest struct {
	Holding domain.HoldingID
	// To is where it went. Nil is legal and meaningful: it makes the holding
	// derived-Missing -- out, whereabouts unknown -- which is transient
	// ignorance rather than the conclusion MarkedLost draws.
	To *domain.LocationID
}

func PlanCheckOut(s Snapshot, req CheckOutRequest) ([]domain.Event, error) {
	h, err := s.UniqueHolding(req.Holding)
	if err != nil {
		return nil, err
	}
	if h.Custody != domain.CustodyAtRest {
		return nil, fmt.Errorf("%w: holding %d is %s, so there is nothing to take",
			ErrCustody, req.Holding, h.Custody)
	}
	return []domain.Event{domain.CheckedOut{
		EventBase: s.base(), Holding: req.Holding, DisplacedTo: req.To,
	}}, nil
}

// ReturnRequest restores a Unique holding to where it is kept.
type ReturnRequest struct{ Holding domain.HoldingID }

func PlanReturn(s Snapshot, req ReturnRequest) ([]domain.Event, error) {
	h, err := s.UniqueHolding(req.Holding)
	if err != nil {
		return nil, err
	}
	// Lost is excluded on purpose: something concluded lost that turns up is
	// Found, not Returned. Both land at AtRest, and collapsing them would lose
	// the distinction between "brought back" and "turned up".
	if h.Custody != domain.CustodyOut {
		return nil, fmt.Errorf("%w: holding %d is %s, not Out", ErrCustody, req.Holding, h.Custody)
	}
	return []domain.Event{domain.Returned{EventBase: s.base(), Holding: req.Holding}}, nil
}

// MarkLostRequest concludes that a Unique holding is not findable.
type MarkLostRequest struct{ Holding domain.HoldingID }

func PlanMarkLost(s Snapshot, req MarkLostRequest) ([]domain.Event, error) {
	h, err := s.UniqueHolding(req.Holding)
	if err != nil {
		return nil, err
	}
	// Losing something that was sitting at rest is entirely ordinary, so
	// AtRest and Out both permit this. Only Lost does not: it is already the
	// conclusion.
	if h.Custody == domain.CustodyLost {
		return nil, fmt.Errorf("%w: holding %d is already Lost", ErrCustody, req.Holding)
	}
	return []domain.Event{domain.MarkedLost{EventBase: s.base(), Holding: req.Holding}}, nil
}

// FoundRequest reverses a conclusion that something was lost.
//
// At is where it turned up, and it is the common case rather than the exotic
// one: things are rarely lost and then found exactly where they were supposed
// to be. Omitted, the Holding is simply no longer lost.
type FoundRequest struct {
	Holding domain.HoldingID
	At      *domain.LocationID
}

// PlanFound is one intent and, when the thing turned up somewhere else, two
// events.
//
// That is not a compromise. The ledger has exactly ONE way to say "it is here
// now" -- Moved -- and putting a location on Found as well would give it two,
// which is how a replay ends up depending on which spelling was used. The event
// vocabulary stays minimal; the OPERATION composes. Consume already emits three
// events for one keystroke, for the same reason.
//
// Moved rather than Rehomed: the system believed the thing was at its stowed
// location and it is not, so it moved. Rehomed would claim the object stayed
// put and only its address changed, which is the opposite of what happened.
func PlanFound(s Snapshot, req FoundRequest) ([]domain.Event, error) {
	h, err := s.UniqueHolding(req.Holding)
	if err != nil {
		return nil, err
	}
	if h.Custody != domain.CustodyLost {
		return nil, fmt.Errorf("%w: holding %d is %s, not Lost", ErrCustody, req.Holding, h.Custody)
	}

	events := []domain.Event{domain.Found{EventBase: s.base(), Holding: req.Holding}}
	// Found first: it stopped being lost, and then it is somewhere. The reverse
	// order would record a Lost thing moving, which is a claim about a location
	// the system had just admitted it did not know.
	if req.At != nil && *req.At != h.StowedLocation {
		events = append(events, domain.Moved{
			EventBase: s.base(), Holding: req.Holding, From: h.StowedLocation, To: *req.At,
		})
	}
	return events, nil
}

// VerifyRequest records looking for a Unique holding and reports what was seen.
type VerifyRequest struct {
	Holding domain.HoldingID
	Present bool
}

// PlanVerify records the observation, and draws the conclusion the observation
// implies.
//
// Verified alone changes nothing -- it is an observation, and the fold treats
// it as such. Looking for something and not finding it is what MarkedLost is
// for, so a negative verification records both: what was observed, then what
// was concluded. Keeping them separate means the conclusion can be reversed by
// Found without erasing the observation that prompted it.
func PlanVerify(s Snapshot, req VerifyRequest) ([]domain.Event, error) {
	h, err := s.UniqueHolding(req.Holding)
	if err != nil {
		return nil, err
	}
	events := []domain.Event{domain.Verified{
		EventBase: s.base(), Holding: req.Holding, Present: req.Present,
	}}
	if !req.Present && h.Custody != domain.CustodyLost {
		events = append(events, domain.MarkedLost{EventBase: s.base(), Holding: req.Holding})
	}
	// A positive verification of something believed Lost is a find, for the
	// same reason: the observation and the conclusion are different records.
	if req.Present && h.Custody == domain.CustodyLost {
		events = append(events, domain.Found{EventBase: s.base(), Holding: req.Holding})
	}
	return events, nil
}

// ---------------------------------------------------------------------------
// Placement
// ---------------------------------------------------------------------------

// RehomeRequest changes where a Holding belongs without the object moving --
// "this lives in the garage now".
type RehomeRequest struct {
	Holding domain.HoldingID
	To      domain.LocationID
}

// PlanRehome is distinct from Move for a reason worth keeping: Move says the
// stuff went somewhere, Rehome says its home changed. Conflating them would
// make a reorganisation look like a physical relocation in the history.
//
// Unique only, and the restriction was learned rather than designed. The
// distinction Rehome draws needs somewhere for the thing to be while its home
// changes, and only a Unique Holding has one -- custody plus displaced_to. A
// Bulk Holding IS its stowed location, so for bulk the two operations mean the
// same thing and Move is the honest one.
//
// Allowing it for bulk was also unsound: stowed_location is part of H8's key,
// and a thin operation snapshots one Holding, so it could not see that the
// destination slot was already taken. It quietly produced two active Holdings
// that the schema says are one -- found by the H8 check in VerifyAll, sixteen
// property sequences at a time, long after 8c called this operation thin.
func PlanRehome(s Snapshot, req RehomeRequest) ([]domain.Event, error) {
	h, err := s.UniqueHolding(req.Holding)
	if err != nil {
		return nil, err
	}
	from := h.StowedLocation
	if from == req.To {
		return nil, fmt.Errorf("%w: holding %d is already kept at %d", ErrInvalidRequest, req.Holding, req.To)
	}
	return []domain.Event{domain.Rehomed{
		EventBase: s.base(), Holding: req.Holding, From: from, To: req.To,
	}}, nil
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// RetireRequest ends a Holding's life. The record persists in history.
type RetireRequest struct {
	Holding domain.HoldingID
	Reason  string
}

func PlanRetire(s Snapshot, req RetireRequest) ([]domain.Event, error) {
	if _, err := s.LiveHolding(req.Holding); err != nil {
		return nil, err
	}
	return []domain.Event{domain.Gone{
		EventBase: s.base(), Holding: req.Holding, Reason: req.Reason,
	}}, nil
}

// DiscardRequest removes stock for a stated reason.
type DiscardRequest struct {
	Holding domain.HoldingID
	Amount  domain.Quantity
	Reason  string
}

// PlanDiscard debits a measured Holding.
//
// Discarding everything does NOT retire the Holding. An empty jar is still the
// jar you keep turmeric in, and the next Acquired refills it; retiring it would
// make the next purchase create a second one. Gone is for the container being
// done with, which is a separate decision a person makes.
func PlanDiscard(s Snapshot, req DiscardRequest) ([]domain.Event, error) {
	h, err := s.BulkHolding(req.Holding)
	if err != nil {
		return nil, err
	}
	if !req.Amount.IsPositive() {
		return nil, fmt.Errorf("%w: discard amount must be positive, got %s", ErrInvalidRequest, req.Amount)
	}
	if req.Amount.Milli() > h.Quantity.Milli() {
		return nil, fmt.Errorf("%w: holding %d has %s, cannot discard %s",
			ErrInsufficient, req.Holding, h.Quantity, req.Amount)
	}
	delta, err := req.Amount.Neg()
	if err != nil {
		return nil, err
	}
	return []domain.Event{domain.Discarded{
		EventBase: s.base(), Holding: req.Holding, Delta: delta, Reason: req.Reason,
	}}, nil
}

// ---------------------------------------------------------------------------
// Planner surface
// ---------------------------------------------------------------------------

// planOne gathers the snapshot for a single Holding, plans against it, and
// wraps the result as a one-Step Batch.
//
// Generic over the request type so each operation names its own plan function
// at the call site: the wrapper is uniform, but which pure function runs is
// still written down, and adding an operation without wiring it is a compile
// error rather than a silent omission.
func planOne[R any](
	ctx context.Context,
	p *Planner,
	subject domain.HoldingID,
	req R,
	plan func(Snapshot, R) ([]domain.Event, error),
	summary string,
) (Batch, error) {
	s, err := p.SnapshotHoldings(ctx, subject)
	if err != nil {
		return Batch{}, err
	}
	events, err := plan(s, req)
	if err != nil {
		return Batch{}, err
	}
	return Batch{Steps: []Step{{
		Summary: summary, Records: fixed(events), Ends: endsSomething(events),
	}}}, nil
}

func (p *Planner) CheckOut(ctx context.Context, req CheckOutRequest) (Batch, error) {
	where := "somewhere unrecorded"
	if req.To != nil {
		where = fmt.Sprintf("location %d", *req.To)
	}
	return planOne(ctx, p, req.Holding, req, PlanCheckOut,
		fmt.Sprintf("take holding %d to %s", req.Holding, where))
}

func (p *Planner) Return(ctx context.Context, req ReturnRequest) (Batch, error) {
	return planOne(ctx, p, req.Holding, req, PlanReturn,
		fmt.Sprintf("put holding %d back", req.Holding))
}

func (p *Planner) MarkLost(ctx context.Context, req MarkLostRequest) (Batch, error) {
	return planOne(ctx, p, req.Holding, req, PlanMarkLost,
		fmt.Sprintf("give up on finding holding %d", req.Holding))
}

func (p *Planner) Found(ctx context.Context, req FoundRequest) (Batch, error) {
	return planOne(ctx, p, req.Holding, req, PlanFound,
		fmt.Sprintf("holding %d turned up", req.Holding))
}

func (p *Planner) Verify(ctx context.Context, req VerifyRequest) (Batch, error) {
	seen := "saw"
	if !req.Present {
		seen = "could not find"
	}
	return planOne(ctx, p, req.Holding, req, PlanVerify,
		fmt.Sprintf("%s holding %d", seen, req.Holding))
}

func (p *Planner) Rehome(ctx context.Context, req RehomeRequest) (Batch, error) {
	return planOne(ctx, p, req.Holding, req, PlanRehome,
		fmt.Sprintf("holding %d lives at location %d now", req.Holding, req.To))
}

func (p *Planner) Retire(ctx context.Context, req RetireRequest) (Batch, error) {
	return planOne(ctx, p, req.Holding, req, PlanRetire,
		fmt.Sprintf("done with holding %d", req.Holding))
}

func (p *Planner) Discard(ctx context.Context, req DiscardRequest) (Batch, error) {
	return planOne(ctx, p, req.Holding, req, PlanDiscard,
		fmt.Sprintf("throw away %s from holding %d", req.Amount, req.Holding))
}
