package ops

import (
	"home-management-system/internal/domain"
)

// ref is a Holding reference that may point at one the plan has not created
// yet.
//
// Consuming from a sealed bag has to record Opened against a content-basis
// Holding that may not exist until this very operation runs. Its identifier is
// therefore not knowable at planning time, and pretending otherwise -- a zero,
// a negative sentinel -- is how a plan ends up writing to holding 0.
type ref struct {
	id   domain.HoldingID
	slot int // index among the Holdings this plan creates; -1 when id is real
}

func existing(id domain.HoldingID) ref { return ref{id: id, slot: -1} }

// Plan accumulates a Step under construction.
//
// Events are stored as builders rather than values because a builder can be
// handed the identifier its subject turned out to get. The alternative -- emit
// events with placeholder ids and rewrite them afterwards -- needs a 24-way
// switch to reach each event's Holding field, and nothing would fail to compile
// when a 25th arrived.
type Plan struct {
	originates []Origination
	builders   []func(Created) (domain.Event, error)

	// holdings counts Holdings originated so far, so each ref knows its slot.
	holdings int

	// shifts tracks quantity changes this plan has already made but that are
	// not yet in the snapshot. Opening two packages must see the first one's
	// contents when deciding whether a second is needed.
	shifts map[ref]int64

	// filled records which H8 slots this plan has already put a Holding into.
	//
	// Without it, a plan enforces H8 against the snapshot but not against
	// itself: opening a second package finds no content-basis Holding in the
	// snapshot -- correctly, there is none -- and creates a second one, landing
	// two active Holdings on the same key. The snapshot is a photograph taken
	// before the plan began, so anything the plan does is invisible to it.
	filled map[slot]ref
}

// holdingFor returns a reference to the Holding occupying a slot: the one the
// snapshot shows, the one this plan has already created, or a new origination.
//
// Every operation that may create a Holding goes through here, so H8 is upheld
// against the snapshot and against the plan's own earlier steps in one place.
func (p *Plan) holdingFor(s Snapshot, k slot, mk func() Origination) ref {
	if r, ok := p.filled[k]; ok {
		return r
	}
	if h, ok := s.findSlot(k); ok {
		r := existing(h.ID)
		p.fill(k, r)
		return r
	}
	r := p.originateHolding(mk())
	p.fill(k, r)
	return r
}

func (p *Plan) fill(k slot, r ref) {
	if p.filled == nil {
		p.filled = map[slot]ref{}
	}
	p.filled[k] = r
}

// originateHolding schedules a Holding creation and returns a reference to it.
func (p *Plan) originateHolding(o Origination) ref {
	p.originates = append(p.originates, o)
	r := ref{slot: p.holdings}
	p.holdings++
	return r
}

// record appends an event against a subject that may not exist yet.
func (p *Plan) record(r ref, mk func(domain.HoldingID) domain.Event) {
	p.builders = append(p.builders, func(c Created) (domain.Event, error) {
		id, err := r.resolve(c)
		if err != nil {
			return nil, err
		}
		return mk(id), nil
	})
}

func (r ref) resolve(c Created) (domain.HoldingID, error) {
	if r.slot < 0 {
		return r.id, nil
	}
	return c.Holding(r.slot)
}

// shift records a quantity change this plan has made, and shifted reads it
// back. Together they are the plan's view of state it has already altered.
func (p *Plan) shift(r ref, milli int64) {
	if p.shifts == nil {
		p.shifts = map[ref]int64{}
	}
	p.shifts[r] += milli
}

func (p *Plan) shifted(r ref) int64 { return p.shifts[r] }

// AsStep converts the accumulated Plan into a Step.
func (p Plan) AsStep(summary string) Step {
	builders := p.builders
	return Step{
		Summary:    summary,
		Originates: p.originates,
		Records: func(c Created) ([]domain.Event, error) {
			events := make([]domain.Event, 0, len(builders))
			for _, build := range builders {
				e, err := build(c)
				if err != nil {
					return nil, err
				}
				events = append(events, e)
			}
			return events, nil
		},
	}
}

// Batch wraps a Plan as a one-Step Batch.
func (p Plan) Batch(summary string) Batch {
	return Batch{Steps: []Step{p.AsStep(summary)}}
}
