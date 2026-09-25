package testing_test

import (
	"testing"

	"home-management-system/internal/domain"
	sim "home-management-system/internal/tui/testing"
)

// The walk: checking a subtree against the house it claims to describe.
//
// The claim under test everywhere here is the one the ledger's design turns
// on -- an observation is not a correction. Counting a shelf and finding it
// right is not a no-op: it is the evidence that somebody looked.

// TestAWalkAsksAboutEveryHolding under the node it was started from.
func TestAWalkAsksAboutEveryHolding(t *testing.T) {
	s := sim.New(t)
	stocked(t, s)
	s.ByPlace()
	s.OnRail()

	s.Send(sim.Press("V"))
	s.ShowsText("WALK")
	s.ShowsText("the ledger says")
	s.ShowsText("1 of 2")
	s.ShowsText("0 of 2 checked")
}

// TestConfirmingACountIsStillWritten.
//
// The whole reason the walk exists. "Still 500 g" records Counted and
// nothing else -- no Adjusted, because nothing changed -- and a system that
// wrote nothing at all could never tell a shelf somebody checked from one
// nobody has looked at since it was filled.
func TestConfirmingACountIsStillWritten(t *testing.T) {
	s := sim.New(t)
	rice, _ := stocked(t, s)
	s.ByPlace()
	s.OnRail()

	s.Send(sim.Press("V"))
	s.Send(sim.Press("y"))
	s.Send(sim.Press("y"))
	s.ShowsText("files it")
	s.Send(sim.Press("A"))

	s.ShowsText("one transaction")
	s.OnHand(rice, 500*domain.Scale)

	// Counted, and NOT Adjusted: the observation was recorded, and nothing
	// was corrected, because nothing was wrong. That pair is the whole
	// claim -- a single event would have had to mean both.
	events := s.EventTypes(onlyHolding(t, s, rice))
	if !has(events, "Counted") {
		t.Errorf("confirming a count wrote no observation: %v", events)
	}
	if has(events, "Adjusted") {
		t.Errorf("confirming a correct count adjusted it anyway: %v", events)
	}
}

// TestACountThatDisagreesRecordsBoth: the observation AND the correction,
// which is why they are separate events rather than one.
func TestACountThatDisagreesRecordsBoth(t *testing.T) {
	s := sim.New(t)
	rice, _ := stocked(t, s)
	s.ByPlace()
	s.OnRail()
	holding := onlyHolding(t, s, rice)

	s.Send(sim.Press("V"))
	s.Send(sim.Press("#"))
	s.Send(sim.Type("400"))
	s.Send(sim.Enter)
	s.Send(sim.Press("y"))
	s.Send(sim.Press("A"))

	s.OnHand(rice, 400*domain.Scale)
	events := s.EventTypes(holding)
	if !has(events, "Counted") || !has(events, "Adjusted") {
		t.Errorf("a count that disagreed recorded %v, want both Counted and Adjusted", events)
	}
}

// TestSomethingNotFoundIsLostRatherThanDeleted.
//
// A Unique holding you cannot find has not stopped existing -- you have
// stopped knowing where it is, and those are different claims. The ledger
// records the failed sighting and concludes from it, which is the only way
// the thing can turn up later and be Found rather than re-invented.
func TestSomethingNotFoundIsLostRatherThanDeleted(t *testing.T) {
	s := sim.New(t)
	_, cable := stocked(t, s)
	s.ByPlace()
	s.OnRail()
	holding := onlyHolding(t, s, cable)

	before := s.CountHoldings()

	s.Send(sim.Press("V"))
	// The rice first, then the cable -- the walk visits both.
	s.Send(sim.Press("y"))
	s.Send(sim.Press("n"))
	s.Send(sim.Press("A"))

	if after := s.CountHoldings(); after != before {
		t.Errorf("a missing holding was deleted: %d holdings before, %d after", before, after)
	}
	events := s.EventTypes(holding)
	if !has(events, "Verified") || !has(events, "MarkedLost") {
		t.Errorf("not finding it recorded %v, want both Verified and MarkedLost", events)
	}
}

// TestAWalkWritesNothingUntilItIsFiled.
func TestAWalkWritesNothingUntilItIsFiled(t *testing.T) {
	s := sim.New(t)
	rice, _ := stocked(t, s)
	s.ByPlace()
	s.OnRail()
	holding := onlyHolding(t, s, rice)

	s.Send(sim.Press("V"))
	s.Send(sim.Press("#"))
	s.Send(sim.Type("400"))
	s.Send(sim.Enter)

	// Said, and shown, and not written.
	s.ShowsText("1 of 2 checked")
	s.OnHand(rice, 500*domain.Scale)
	if events := s.EventTypes(holding); has(events, "Counted") {
		t.Errorf("the walk wrote before it was filed: %v", events)
	}
}

// TestStoppingAWalkFilesNothingAndSaysSo.
//
// Nothing was written, so nothing is recoverable, so the count has to be
// said -- the same rule organise mode goes by.
func TestStoppingAWalkFilesNothingAndSaysSo(t *testing.T) {
	s := sim.New(t)
	rice, _ := stocked(t, s)
	s.ByPlace()
	s.OnRail()

	s.Send(sim.Press("V"))
	s.Send(sim.Press("y"))
	s.Send(sim.Esc)

	s.ShowsText("1 row checked and not filed")
	s.HidesText("WALK")
	s.OnHand(rice, 500*domain.Scale)
}

// TestFilingHalfAWalkSaysNothingAboutTheRest.
//
// The shelves nobody reached are exactly as unverified as they were. A walk
// that quietly confirmed them would leave the ledger more confident than it
// was before somebody tried to check it, which is the one outcome worse than
// not walking at all.
func TestFilingHalfAWalkSaysNothingAboutTheRest(t *testing.T) {
	s := sim.New(t)
	rice, cable := stocked(t, s)
	s.ByPlace()
	s.OnRail()
	riceHolding := onlyHolding(t, s, rice)
	cableHolding := onlyHolding(t, s, cable)

	s.Send(sim.Press("V"))
	s.Send(sim.Press("y")) // the first, whichever it is
	s.Send(sim.Press("A")) // file without answering the second

	checked, unchecked := riceHolding, cableHolding
	if has(s.EventTypes(cableHolding), "Verified") {
		checked, unchecked = cableHolding, riceHolding
	}
	if got := s.EventTypes(checked); len(got) < 2 {
		t.Errorf("the answered holding recorded %v, want an observation", got)
	}
	for _, e := range s.EventTypes(unchecked) {
		if e == "Counted" || e == "Verified" {
			t.Errorf("filing half a walk recorded %q against a holding nobody checked", e)
		}
	}
}

// onlyHolding is the one holding of an item, which every fixture here has.
func onlyHolding(t *testing.T, s *sim.Simulator, item domain.ItemID) domain.HoldingID {
	t.Helper()
	rows, err := s.Reader().HoldingsOfItem(s.Context(), item)
	if err != nil {
		t.Fatalf("reading holdings: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("%d holdings of item %d, want 1", len(rows), item)
	}
	return rows[0].Holding.Base().ID
}

func has(all []string, want string) bool {
	for _, got := range all {
		if got == want {
			return true
		}
	}
	return false
}
