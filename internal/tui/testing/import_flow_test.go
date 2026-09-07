package testing_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"home-management-system/internal/domain"
	"home-management-system/internal/ops"
	sim "home-management-system/internal/tui/testing"
)

// 11b: the import plan screen.
//
// The highest-stakes screen in the system. Everywhere else one thing happens
// and you watch it happen; here a file you did not write proposes a batch of
// changes, and the only thing between it and the house is whether this screen
// told the truth about what it was going to do.

// kitchen is a house a receipt can be about.
func kitchen(t *testing.T, s *sim.Simulator) {
	t.Helper()
	p, ctx := s.Planner(), s.Context()

	s.Apply(p.NewLocation(ctx, ops.NewLocationRequest{Name: "Shelf 1"}))
	shelf := s.HasLocation("Shelf 1")
	s.Apply(p.NewCategory(ctx, ops.NewCategoryRequest{Name: "Spices"}))
	spices := s.HasCategory("Spices")

	for _, name := range []string{"Basmati Rice", "Cumin"} {
		s.Apply(p.NewItem(ctx, ops.NewItemRequest{
			Name: name, Category: spices, Counting: ops.CountingMeasured, ContentUnit: "g",
		}))
		s.Apply(p.Receive(ctx, ops.ReceiveRequest{
			Item: s.HasItem(name), Location: shelf, Basis: domain.BasisContent,
			Amount: domain.FromMilli(500 * domain.Scale), Source: "shop",
		}))
	}
}

// receipt writes a file and returns its path.
func receipt(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "receipt.csv")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const messy = `op,item,qty,at,reason,name,counting,unit,category
acquire,Basmati Rice,100,Shelf 1,,,,,
consume,Cumin,10,Shelf 1,dinner,,,,
acquire,Cumn,20,Shelf 1,,,,,
acquire,Cardamom,50,Shelf 1,,,,,
consume,Basmati Rice,lots,Shelf 1,dinner,,,,
`

// The three states are legible before they are read, and the arithmetic adds up
// to the file.
func TestThePlanScreenCountsTheWholeFile(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	s.Import(receipt(t, messy))

	s.ShowsText("5 rows")
	s.ShowsText("2 ready")
	s.ShowsText("2 need confirming")
	s.ShowsText("1 blocked")
}

// A is refused with a reason rather than being a key that does nothing.
func TestApplyIsRefusedWithAReason(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	rice := s.HasItem("Basmati Rice")
	s.Import(receipt(t, messy))

	s.Send(sim.Press("A"))
	s.ShowsText("A is unavailable")
	s.ShowsText("blocked")
	s.OnHand(rice, 500*domain.Scale) // nothing happened
}

// A suggestion is offered and never applied, so accepting it is a keystroke
// rather than something the screen did on your behalf.
func TestAcceptingASuggestion(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	s.Import(receipt(t, messy))

	s.ShowsText("did you mean")
	s.Send(sim.CtrlN, sim.CtrlN)
	s.Send(sim.Enter)

	s.HidesText("did you mean")
	s.ShowsText("3 ready")
}

// Dropping settles a row without fixing it, and is reversible right up until
// the apply.
func TestDroppingAndUndropping(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	s.Import(receipt(t, messy))

	s.Send(sim.AltGreat) // the blocked row
	s.Send(sim.Press("d"))
	s.ShowsText("1 dropped")
	s.ShowsText("0 blocked")

	s.Send(sim.Press("u"))
	s.ShowsText("1 blocked")
	s.HidesText("dropped")
}

// All-or-nothing, and it applies in ONE transaction.
func TestApplyingTheWholeFile(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	rice, cumin := s.HasItem("Basmati Rice"), s.HasItem("Cumin")
	s.Import(receipt(t, `op,item,qty,at,reason
acquire,Basmati Rice,100,Shelf 1,
consume,Cumin,10,Shelf 1,dinner
`))

	s.ShowsText("2 ready")
	s.ShowsText("A applies all of it")
	s.Send(sim.Press("A"))

	s.OnHand(rice, 600*domain.Scale)
	s.OnHand(cumin, 490*domain.Scale)
	s.ShowsText("applied 2 rows in one transaction")
}

// A row that binds but cannot WORK is named before anything is applied, rather
// than rolling back a transaction and naming nothing.
func TestARowThatCannotWorkIsNamedBeforeApplying(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	rice := s.HasItem("Basmati Rice")
	s.Import(receipt(t, `op,item,qty,at,reason
acquire,Basmati Rice,100,Shelf 1,
consume,Basmati Rice,99999,Shelf 1,dinner
`))

	s.Send(sim.Press("A"))
	s.ShowsText("row 3")
	s.ShowsText("not enough")
	// And nothing was applied, including the row that would have worked.
	s.OnHand(rice, 500*domain.Scale)
}

// Cancelling leaves nothing behind, which is all-or-nothing seen from the
// other end.
func TestCancellingAppliesNothing(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	rice := s.HasItem("Basmati Rice")
	s.Import(receipt(t, messy))
	s.Send(sim.Press("q"))
	s.OnHand(rice, 500*domain.Scale)
}

// TestTwoRowsNamingOneNewItemCreateItOnce is the property the whole design is
// arranged around: a receipt cannot produce two Turmerics.
//
// Settling the first row creates the item; the second must then FIND it rather
// than offering to make it again.
func TestTwoRowsNamingOneNewItemCreateItOnce(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	s.Import(receipt(t, `op,item,qty,at
acquire,Cardamom,50,Shelf 1
acquire,Cardamom,25,Shelf 1
`))
	s.ShowsText("2 need confirming")

	// Settle the first: the creation panel opens, pre-named from the row.
	s.Send(sim.Enter)
	s.ShowsText("new item")
	s.ShowsText("Cardamom")
	s.Send(sim.Tab, sim.Tab) // past counting, onto unit
	s.Send(sim.Type("g"))
	s.Send(sim.Tab, sim.Tab) // onto category
	s.Send(sim.Type("Spices"))
	s.Send(sim.Enter) // the permanent-fields confirmation
	s.ShowsText("permanent")
	s.Send(sim.Enter)

	// Both rows are now ready, and there is exactly one Cardamom.
	s.ShowsText("2 ready")
	s.ShowsText("0 need confirming")
	if got := countItemsNamed(t, s, "Cardamom"); got != 1 {
		t.Errorf("%d items called Cardamom, want 1", got)
	}
}

func countItemsNamed(t *testing.T, s *sim.Simulator, name string) int {
	t.Helper()
	items, err := s.Reader().Items(s.Context())
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, it := range items {
		if strings.EqualFold(it.Base().Name, name) && it.Base().ArchivedAt == nil {
			n++
		}
	}
	return n
}

// A row that needs confirming stops the apply just as a blocked one does.
//
// Its own test, because the messy receipt has a blocked row too -- and a plan
// refused for two reasons cannot tell you whether it would have been refused
// for either.
func TestApplyIsRefusedForAnUnconfirmedRowAlone(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	rice := s.HasItem("Basmati Rice")
	s.Import(receipt(t, `op,item,qty,at
acquire,Basmati Rice,100,Shelf 1
acquire,Cardamom,50,Shelf 1
`))
	s.ShowsText("1 ready")
	s.ShowsText("1 need confirming")
	s.ShowsText("0 blocked")

	s.Send(sim.Press("A"))
	s.ShowsText("A is unavailable")
	s.ShowsText("confirming")
	// Not even the ready row, because it is all or nothing.
	s.OnHand(rice, 500*domain.Scale)
}

// TestFixingARowInPlace is what the walkthrough asked for and nothing did.
//
// A blocked row is usually one word away from working, and the word is right
// there -- so the field opens AT the row, like every other field in this
// interface.
// left is n presses of the left arrow, for walking back into a line.
func left(n int) []sim.Key {
	out := make([]sim.Key, n)
	for i := range out {
		out[i] = sim.Left
	}
	return out
}

// The row opens as the command it is, and the cursor reaches the word that is
// wrong without disturbing the rest of the line.
//
// Without a cursor this is not editing but retyping: the field only appended
// and backspaced, so correcting "lots" meant destroying everything after it.
func TestFixingARowInPlace(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	rice := s.HasItem("Basmati Rice")
	s.Import(receipt(t, `op,item,qty,at,reason
consume,Basmati Rice,lots,Shelf 1,dinner
`))
	s.ShowsText("1 blocked")

	s.Send(sim.Press("e"))
	s.ShowsText("command")
	// The whole row, as a line, quoted so it means what the row meant.
	s.ShowsText(`consume "Basmati Rice" lots at "Shelf 1" reason dinner`)

	// Walk back over the tail, so the cursor sits just after the bad word,
	// then replace only that word.
	tail := ` at "Shelf 1" reason dinner`
	s.Send(left(len(tail)))
	s.Send(sim.Backspace, sim.Backspace, sim.Backspace, sim.Backspace)
	s.Send(sim.Type("20"))
	// The tail is still there, untouched, which is the whole point.
	s.ShowsText(`consume "Basmati Rice" 20 at "Shelf 1" reason dinner`)
	s.Send(sim.Enter)

	s.ShowsText("1 ready")
	s.ShowsText("0 blocked")
	s.Send(sim.Press("A"))
	s.OnHand(rice, 480*domain.Scale)
}

// ctrl+w takes back a token at a time, which is how the last word of a line
// gets replaced.
func TestCtrlWTakesBackOneTokenAtATime(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	s.Import(receipt(t, `op,item,qty,at,reason
consume,Basmati Rice,lots,Shelf 1,dinner
`))
	s.Send(sim.Press("e"))
	s.Send(sim.CtrlW)
	s.ShowsText(`consume "Basmati Rice" lots at "Shelf 1" reason`)
	s.HidesText(`reason dinner`)
}

// The bug this whole path was rebuilt for: `e` refused on any row that had no
// blocking issue, which on a typical receipt is most of them -- every ready row
// and every row whose only business is creating something. The cursor starts on
// one, so the first `e` a person ever pressed was refused.
func TestARowThatIsAlreadyReadyCanStillBeEdited(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	rice := s.HasItem("Basmati Rice")
	s.Import(receipt(t, `op,item,qty,at,reason
consume,Basmati Rice,10,Shelf 1,dinner
`))
	s.ShowsText("1 ready")

	s.Send(sim.Press("e"))
	s.HidesText("d drops it")
	s.ShowsText(`consume "Basmati Rice" 10 at "Shelf 1" reason dinner`)

	s.Send(sim.CtrlU)
	s.Send(sim.Type("consume \"Basmati Rice\" 25 at \"Shelf 1\" reason dinner"))
	s.Send(sim.Enter)
	s.ShowsText("1 ready")

	s.Send(sim.Press("A"))
	s.OnHand(rice, 475*domain.Scale)
}

// A row whose only business is creating something has no issue to fix, and was
// refused for the same reason a ready row was. Editing it is how you say "I
// meant the thing that already exists".
func TestARowThatWouldCreateCanBeEditedOntoAnExistingItem(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	cumin := s.HasItem("Cumin")
	s.Import(receipt(t, `op,item,qty,at
acquire,Cardamom,50,Shelf 1
`))
	s.ShowsText("1 need confirming")

	s.Send(sim.Press("e"))
	s.HidesText("d drops it")
	s.Send(sim.CtrlU)
	s.Send(sim.Type("acquire Cumin 50 at \"Shelf 1\""))
	s.Send(sim.Enter)

	s.ShowsText("1 ready")
	s.Send(sim.Press("A"))
	s.OnHand(cumin, 550*domain.Scale)
}

// A corrected row goes through Bind exactly as it would have if it had arrived
// that way. A fixed row and a right-first-time row must not take different
// paths, or only one of them is the path everything else is tested against.
func TestAFixedRowIsRefusedLikeAnyOther(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	s.Import(receipt(t, `op,item,qty,at,reason
consume,Basmati Rice,lots,Shelf 1,dinner
`))
	s.Send(sim.Press("e"))
	s.Send(sim.CtrlU)
	s.Send(sim.Type("consume \"Basmati Rice\" nonsense at \"Shelf 1\""))
	s.Send(sim.Enter)

	s.ShowsText("1 blocked")
	s.ShowsText("no number in it")
}

// A line that is not a command at all leaves the row exactly as it was, rather
// than writing back a third state that is neither what the file said nor what
// was typed.
func TestALineThatCannotBeReadLeavesTheRowAlone(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	s.Import(receipt(t, `op,item,qty,at,reason
consume,Basmati Rice,lots,Shelf 1,dinner
`))
	s.Send(sim.Press("e"))
	s.Send(sim.CtrlU)
	s.Send(sim.Type("frobnicate the whole kitchen"))
	s.Send(sim.Enter)

	s.ShowsText("is not a command")
	s.ShowsText("1 blocked")
	s.ShowsText("lots")
}

// Escaping an edit leaves the row exactly as it was.
func TestAbandoningAFixChangesNothing(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	s.Import(receipt(t, `op,item,qty,at,reason
consume,Basmati Rice,lots,Shelf 1,dinner
`))
	s.Send(sim.Press("e"))
	s.Send(sim.CtrlU)
	s.Send(sim.Type("consume \"Basmati Rice\" 20 at \"Shelf 1\""))
	s.Send(sim.Esc)

	s.ShowsText("1 blocked")
	s.ShowsText("lots")
}

// A token that names something completes, through the same completer as
// everywhere else -- and against the field the PARSER says it is in, not a
// second guess at which slot the cursor is sitting in.
func TestEditingARowCompletesTheNameBeingTyped(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	s.Import(receipt(t, `op,item,qty,at
acquire,Basmati Rice,100,Nowhere At All
`))
	s.Send(sim.Press("e"))
	s.Send(sim.CtrlU)
	s.Send(sim.Type("acquire \"Basmati Rice\" 100 at shel"))
	s.ShowsText("Shelf 1")
	s.ShowsText("TAB to take it")

	s.Send(sim.Tab)
	s.Send(sim.Enter)
	s.ShowsText("1 ready")
}

// The completion offered is for the slot the parser will read the token in.
// "at" takes a Location, so an Item that also matches must not be offered --
// which is the failure a completer with its own idea of the grammar produces.
func TestCompletionFollowsTheFieldNotTheText(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	s.Import(receipt(t, `op,item,qty,at
acquire,Basmati Rice,100,Shelf 1
`))
	s.Send(sim.Press("e"))
	s.Send(sim.CtrlU)
	// "c" in the ITEM slot offers the item.
	s.Send(sim.Type("acquire Cum"))
	s.ShowsText("Cumin")

	s.Send(sim.CtrlU)
	// The same letters in the AT slot offer no item, because `at` is a Location.
	s.Send(sim.Type("acquire \"Basmati Rice\" 100 at Cum"))
	s.HidesText("TAB to take it")
}

// While a field is open, the plan's own keys are not the plan's. `ctrl+u` is
// the table's page-up and `enter` settles a row -- and both were eating
// keystrokes meant for the field, because the plan saw them first.
func TestThePlanDoesNotEatKeysMeantForTheField(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	s.Import(receipt(t, `op,item,qty,at,reason
consume,Basmati Rice,lots,Shelf 1,dinner
`))
	rice := s.HasItem("Basmati Rice")
	s.Send(sim.Press("e"))
	s.Send(sim.CtrlU)
	s.Send(sim.Type("consume \"Basmati Rice\" 5 at \"Shelf 1\""))
	s.Send(sim.Enter)

	// enter saved the field rather than settling the row underneath it, and
	// ctrl+u cleared the field rather than paging the table -- which shows in
	// the AMOUNT: an uncleared field would have left the old line in place.
	s.ShowsText("1 ready")
	s.HidesText("enter re-check the row")
	s.Send(sim.Press("A"))
	s.OnHand(rice, 495*domain.Scale)
}

// The review screen consumes every key it does not use.
//
// It used to fall through to the browse keystrokes, so `#` opened a count
// prompt, `c` a consume prompt and C-k a retirement -- each against whatever
// the browse view had selected, which is not what the person is looking at. A
// review screen that can write outside its own plan is not a review screen.
func TestThePlanScreenDoesNotFallThroughToBrowseKeys(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	rice := s.HasItem("Basmati Rice")
	s.Import(receipt(t, `op,item,qty,at,reason
consume,Basmati Rice,10,Shelf 1,dinner
`))

	for _, key := range []string{"#", "c", "m", "t", "o", "O", "E", "v", "V", "y", "p"} {
		s.Send(sim.Press(key))
		s.HidesText("how much is actually there")
		s.HidesText("new item")
		s.ShowsText("enter settle")
	}
	s.Send(sim.CtrlK)

	// Nothing was applied, and nothing was written outside the plan.
	s.OnHand(rice, 500*domain.Scale)
}

// TestEscapingASettleLeavesTheRowSettleable: a cancelled settle has to end as
// completely as a confirmed one.
//
// The panel's row index used to be cleared only when a creation succeeded, so
// escaping left the plan believing a row was mid-settle. Nothing visible went
// wrong at the time, which is why it survived -- the damage was that the next
// applied change would have been routed into rebinding a row nobody was
// looking at.
func TestEscapingASettleLeavesTheRowSettleable(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	s.Import(receipt(t, `op,item,qty,at
acquire,Cardamom,50,Shelf 1
`))
	s.ShowsText("1 need confirming")

	// Open the creation panel for the row, then abandon it.
	s.Send(sim.Enter)
	s.ShowsText("new item")
	s.Send(sim.Esc)
	s.HidesText("new item")
	s.ShowsText("1 need confirming")

	// The row is still settleable, and settling it now works normally.
	s.Send(sim.Enter)
	s.ShowsText("new item")
	s.Send(sim.Tab, sim.Tab)
	s.Send(sim.Type("g"))
	s.Send(sim.Tab, sim.Tab)
	s.Send(sim.Type("Spices"))
	s.Send(sim.Enter)
	s.ShowsText("permanent")
	s.Send(sim.Enter)

	s.ShowsText("1 ready")
	if got := countItemsNamed(t, s, "Cardamom"); got != 1 {
		t.Errorf("%d items called Cardamom, want 1", got)
	}
}
