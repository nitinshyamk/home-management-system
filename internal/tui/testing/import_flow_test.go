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

// ---------------------------------------------------------------------------
// The two stages
//
// A file written from a photograph routinely proposes a CLASSIFICATION and the
// things filed under it at the same time, and the two cannot be reviewed as one
// list: a row filed under a category the same file is about to create has
// nothing to resolve against. So the categories go first, as their own screen
// and their own transaction, and the rest is bound again afterwards.
// ---------------------------------------------------------------------------

// proposal is a file that says something about the classification and something
// about the house.
const proposal = `op,name,under,item,qty,at
new category,Baking,,,,
acquire,,,Basmati Rice,100,Shelf 1
`

// The categories are their own stage, and the screen says which one you are on.
func TestAFileThatProposesCategoriesIsReviewedInTwoStages(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	s.Import(receipt(t, proposal))

	s.ShowsText("STAGE 1 OF 2 - CATEGORIES")
	// Only the category row is on this screen. The acquire row is not something
	// to review yet -- it will be bound again once this stage has been settled.
	s.ShowsText("CATEGORIES   1 row")
	s.ShowsText("1 need confirming")
	s.ShowsText("S skip the stage")
}

// Creation is never silent, and this is what makes it not silent: the row
// stands at "needs confirming" until a person agrees, and is then applied in
// the same transaction as the rest of its stage.
func TestTheCategoryStageAppliesAndThenHandsOverToTheReview(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	rice := s.HasItem("Basmati Rice")
	s.Import(receipt(t, proposal))

	s.Send(sim.Enter) // agree to the category this row would create
	s.ShowsText("1 ready")
	s.Send(sim.Press("A"))

	// The second stage is on screen, and it says what the first one did.
	s.ShowsText("STAGE 2 OF 2 - ITEMS AND HOLDINGS")
	s.ShowsText("stage 1 (categories) applied 1 row in one transaction")
	s.HasCategory("Baking")

	// And the row that was waiting behind it is an ordinary ready row.
	s.ShowsText("1 ready")
	s.Send(sim.Press("A"))
	s.OnHand(rice, 600*domain.Scale)
	s.ShowsText("after 1 row earlier -- one transaction each")
}

// The bulk category upload can be skipped, and skipping it writes nothing.
//
// A proposed classification is the part of a file an agent is most likely to
// get wrong, and the review screen can already file a row into a category that
// exists -- so the fastest path through a bad one is not to fix it row by row.
func TestTheCategoryStageCanBeSkipped(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	rice := s.HasItem("Basmati Rice")
	s.Import(receipt(t, proposal))

	s.Send(sim.Press("S"))
	s.ShowsText("STAGE 2 OF 2")
	s.ShowsText("skipped")
	s.ShowsText("the house is as it was")
	if got := countCategoriesNamed(t, s, "Baking"); got != 0 {
		t.Errorf("%d categories called Baking after skipping the stage, want none", got)
	}

	// The rest of the file is still there to review and apply.
	s.ShowsText("1 ready")
	s.Send(sim.Press("A"))
	s.OnHand(rice, 600*domain.Scale)
}

// The point of applying the categories first: a row that names one of them is
// bound AGAIN afterwards, against a house that now has it.
//
// Before the first stage was applied this row could not resolve its category.
// Nobody retypes anything for it.
func TestTheSecondStageIsBoundAgainstWhatTheFirstStageMade(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	s.Import(receipt(t, `op,name,under,counting,unit,category
new category,Baking,,,,
new item,Flour,,measured,g,Baking
`))

	s.Send(sim.Enter) // the category
	s.Send(sim.Press("A"))
	s.ShowsText("STAGE 2 OF 2")

	// The item row resolves its category now, so all that is left is agreeing
	// to the item itself.
	s.ShowsText("1 need confirming")
	s.ShowsText("would create an item")
	s.Send(sim.Enter)
	s.ShowsText("1 ready")
	s.Send(sim.Press("A"))

	if got := countItemsNamed(t, s, "Flour"); got != 1 {
		t.Errorf("%d items called Flour, want 1", got)
	}
}

// Two rows creating one category must create it once. The domain permits two
// categories with one name -- sibling uniqueness was never an integrity rule --
// so the review screen is the only place that can see it.
func TestASecondRowCreatingTheSameCategoryIsRefused(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	s.Import(receipt(t, `op,name,under
new category,Baking,
new category,baking,
`))
	s.ShowsText("2 need confirming")

	s.Send(sim.Enter)
	s.Send(sim.CtrlN)
	s.Send(sim.Enter)
	s.ShowsText("already creates that")
	s.ShowsText("1 need confirming")

	// Dropping it is what the refusal suggests, and then the stage applies.
	s.Send(sim.Press("d"))
	s.Send(sim.Press("A"))
	if got := countCategoriesNamed(t, s, "Baking"); got != 1 {
		t.Errorf("%d categories called Baking, want 1", got)
	}
}

// A file that says nothing about the classification is one stage, and says
// nothing about stages. "Stage 1 of 1" describes the screen rather than the
// file, which is chrome nobody asked for.
func TestAFileWithoutCategoriesHasNoStageChrome(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	s.Import(receipt(t, messy))

	s.HidesText("STAGE")
	s.HidesText("skip the stage")
	s.ShowsText("5 rows")
}

// Skipping is offered only where it means something. On the last stage there is
// nothing behind it to go on to, and a key that quietly did nothing would be
// the third such key this screen has had.
func TestTheReviewStageCannotBeSkipped(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	rice := s.HasItem("Basmati Rice")
	s.Import(receipt(t, proposal))

	s.Send(sim.Press("S")) // past the categories
	s.ShowsText("STAGE 2 OF 2")
	s.Send(sim.Press("S")) // and again, which must do nothing at all
	s.ShowsText("STAGE 2 OF 2")
	s.ShowsText("1 ready")
	s.OnHand(rice, 500*domain.Scale)
}

func countCategoriesNamed(t *testing.T, s *sim.Simulator, name string) int {
	t.Helper()
	nodes, err := s.Reader().CategoryForest(s.Context())
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, node := range nodes {
		if strings.EqualFold(node.Category.Name, name) && !node.Category.IsArchived() {
			n++
		}
	}
	return n
}

// A proposed tree is built from the top down.
//
// A row filed under a category another row of the same file creates cannot bind
// until that row has been APPLIED -- a Command holds an identifier, and the
// category does not have one yet. So the category stage applies in passes: the
// ready rows now, the rest bound again against what was just made.
func TestACategoryTreeIsBuiltFromTheTopDown(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	s.Import(receipt(t, `op,name,under,item,qty,at
new category,Baking,,,,
new category,Flours,Baking,,,
acquire,,,Basmati Rice,100,Shelf 1
`))

	// The second row is waiting for the first, and says so instead of offering
	// to make a second Baking.
	s.ShowsText("waits for row 2")

	s.Send(sim.Enter) // agree to Baking
	s.ShowsText("A applies the 1 row ready now")
	s.Send(sim.Press("A"))

	// Still stage 1, with the row that was waiting now bound against the
	// category the pass created.
	s.ShowsText("STAGE 1 OF 2")
	s.ShowsText("applied 1 row in one transaction")
	s.ShowsText(`create category "Flours" under Baking`)

	s.Send(sim.Enter)
	s.Send(sim.Press("A"))

	// Both categories exist, one under the other, and the stage reports what it
	// did as a whole rather than what its last pass did.
	s.ShowsText("STAGE 2 OF 2")
	s.ShowsText("applied 2 rows in 2 transactions")
	parent, child := s.HasCategory("Baking"), s.HasCategory("Flours")
	if under := parentOf(t, s, child); under != parent {
		t.Errorf("Flours is filed under %d, want Baking (%d)", under, parent)
	}
}

// The row that is waiting must not offer to make the thing it is waiting for.
//
// Opening the creation panel there made a SECOND category of the same name --
// immediately, in its own transaction -- and left both rows still proposing
// one, which is the two-Turmerics failure wearing a hat.
func TestARowWaitingForAnotherRowDoesNotOfferToMakeItAgain(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	s.Import(receipt(t, `op,name,under
new category,Baking,
new category,Flours,Baking
`))

	s.Send(sim.CtrlN) // onto the row that is waiting
	s.Send(sim.Enter)

	s.HidesText("what it is called") // the creation panel did not open
	s.ShowsText("row 2 creates that")
	if got := countCategoriesNamed(t, s, "Baking"); got != 0 {
		t.Errorf("%d categories called Baking were made by a keystroke that should have refused", got)
	}
}

func parentOf(t *testing.T, s *sim.Simulator, id domain.CategoryID) domain.CategoryID {
	t.Helper()
	nodes, err := s.Reader().CategoryForest(s.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range nodes {
		if node.Category.ID != id {
			continue
		}
		if node.Category.Parent == nil {
			return 0
		}
		return *node.Category.Parent
	}
	t.Fatalf("no category %d", id)
	return 0
}

// A file of nothing but categories is one stage, and it still builds its tree
// from the top down.
//
// It has no stage chrome -- "stage 1 of 1" describes the screen rather than the
// file -- but the pass is the same pass, and the import is not over until the
// rows are.
func TestAFileOfOnlyCategoriesStillAppliesInPasses(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	s.Import(receipt(t, `op,name,under
new category,Baking,
new category,Flours,Baking
`))
	s.HidesText("STAGE")
	s.ShowsText("waits for row 2")

	s.Send(sim.Enter)
	s.Send(sim.Press("A"))

	// Still the import: the row that was waiting is here, bound against the
	// category the pass created.
	s.ShowsText(`create category "Flours" under Baking`)
	s.Send(sim.Enter)
	s.Send(sim.Press("A"))

	// And now it is over, back on the house, with what the whole import did.
	s.ShowsText("applied 1 row, after 1 row earlier")
	s.HidesText("IMPORT")
	parent, child := s.HasCategory("Baking"), s.HasCategory("Flours")
	if under := parentOf(t, s, child); under != parent {
		t.Errorf("Flours is filed under %d, want Baking (%d)", under, parent)
	}
}
