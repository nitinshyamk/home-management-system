package resolve_test

import (
	"fmt"
	"strings"
	"testing"

	"home-management-system/internal/resolve"
)

// The corpus is a measured gate, not an assertion about what is right.
//
// The threshold between Suggested and Missing is a judgement with consequences
// both ways: too permissive and a receipt silently attaches to the wrong item,
// too strict and every row needs a keystroke. There is no principled value, so
// it is picked from realistic queries and their measured outcomes, and this
// file records both the measurement and the choice.
//
// The asymmetry is the whole point. A wrong answer is far worse than a refusal,
// because a refusal stops and asks while a wrong answer quietly files the
// receipt against the wrong thing. So the assertion is `wrong == 0`, and hits
// are a floor that may only be raised deliberately.

// household is a plausible small home: two "Shelf 1"s, two rices, two chiles,
// two oils. The near-collisions are the point -- an index with no ties would
// make any threshold look good.
func household() *resolve.Index {
	var cs []resolve.Candidate
	add := func(k resolve.Kind, paths ...string) {
		for _, p := range paths {
			cs = append(cs, candidate(k, int64(len(cs)+1), p))
		}
	}
	add(resolve.KindLocation,
		"Kitchen",
		"Kitchen > Left Pantry",
		"Kitchen > Left Pantry > Shelf 1",
		"Kitchen > Left Pantry > Shelf 2",
		"Kitchen > Spice Cabinet",
		"Kitchen > Spice Cabinet > Shelf 1",
		"Kitchen > Spice Cabinet > Shelf 2",
		"Kitchen > Fridge",
		"Kitchen > Fridge > Crisper Drawer",
		"Kitchen > Freezer",
		"Garage",
		"Garage > Overflow Shelf",
		"Garage > Tool Bench",
		"Bathroom",
		"Bathroom > Medicine Cabinet",
	)
	add(resolve.KindCategory,
		"Food",
		"Food > Grains",
		"Food > Spices",
		"Food > Spices > Dried Peppers",
		"Food > Oils and Vinegars",
		"Food > Canned Goods",
		"Household",
		"Household > Cleaning Supplies",
		"Household > Paper Goods",
		"Tools",
	)
	add(resolve.KindItem,
		"Food > Grains > Basmati Rice",
		"Food > Grains > Brown Rice",
		"Food > Grains > Rolled Oats",
		"Food > Grains > All-Purpose Flour",
		"Food > Spices > Turmeric",
		"Food > Spices > Ground Cumin",
		"Food > Spices > Ground Cinnamon",
		"Food > Spices > Dried Peppers > Ancho Chile",
		"Food > Spices > Dried Peppers > Chipotle Chile",
		"Food > Oils and Vinegars > Olive Oil",
		"Food > Oils and Vinegars > Sesame Oil",
		"Food > Oils and Vinegars > Balsamic Vinegar",
		"Food > Canned Goods > Chickpeas",
		"Food > Canned Goods > Black Beans",
		"Food > Canned Goods > Coconut Milk",
		"Household > Cleaning Supplies > Dish Soap",
		"Household > Cleaning Supplies > Laundry Detergent",
		"Household > Paper Goods > Paper Towels",
		"Household > Paper Goods > Toilet Paper",
		"Tools > Cordless Drill",
		"Tools > Tape Measure",
	)
	return resolve.NewIndex(cs)
}

// probe is one query as a person or an agent would actually write it.
//
// want is the candidate that query means. An empty want means the query is
// genuinely ambiguous and the right behaviour is to ask -- offering anything
// confidently is a wrong answer.
type probe struct {
	query string
	kind  resolve.Kind
	want  string
	note  string
}

func corpus() []probe {
	const (
		I = resolve.KindItem
		L = resolve.KindLocation
		C = resolve.KindCategory
	)
	return []probe{
		// Written exactly.
		{"Basmati Rice", I, "Food > Grains > Basmati Rice", "literal"},
		{"basmati rice", I, "Food > Grains > Basmati Rice", "literal, lowercased"},
		{"Brown Rice", I, "Food > Grains > Brown Rice", "literal"},
		{"Rolled Oats", I, "Food > Grains > Rolled Oats", "literal"},
		{"Olive Oil", I, "Food > Oils and Vinegars > Olive Oil", "literal"},
		{"Coconut Milk", I, "Food > Canned Goods > Coconut Milk", "literal"},
		{"Dish Soap", I, "Household > Cleaning Supplies > Dish Soap", "literal"},
		{"Paper Towels", I, "Household > Paper Goods > Paper Towels", "literal"},
		{"Toilet Paper", I, "Household > Paper Goods > Toilet Paper", "literal"},
		{"Cordless Drill", I, "Tools > Cordless Drill", "literal"},
		{"Tape Measure", I, "Tools > Tape Measure", "literal"},
		{"MILK", I, "Food > Canned Goods > Coconut Milk", "shouting"},

		// A distinguishing word out of the middle of a name.
		{"basmati", I, "Food > Grains > Basmati Rice", "partial"},
		{"oats", I, "Food > Grains > Rolled Oats", "partial"},
		{"flour", I, "Food > Grains > All-Purpose Flour", "partial"},
		{"cumin", I, "Food > Spices > Ground Cumin", "partial"},
		{"cinnamon", I, "Food > Spices > Ground Cinnamon", "partial"},
		{"ancho", I, "Food > Spices > Dried Peppers > Ancho Chile", "partial"},
		{"chipotle", I, "Food > Spices > Dried Peppers > Chipotle Chile", "partial"},
		{"balsamic", I, "Food > Oils and Vinegars > Balsamic Vinegar", "partial"},
		{"sesame", I, "Food > Oils and Vinegars > Sesame Oil", "partial"},
		{"detergent", I, "Household > Cleaning Supplies > Laundry Detergent", "partial"},
		{"drill", I, "Tools > Cordless Drill", "partial"},

		// Receipt spellings.
		{"Tumeric", I, "Food > Spices > Turmeric", "dropped letter"},
		{"cinamon", I, "Food > Spices > Ground Cinnamon", "dropped letter"},
		{"turmaric", I, "Food > Spices > Turmeric", "substituted letter"},
		{"chickpea", I, "Food > Canned Goods > Chickpeas", "singular"},
		{"black bean", I, "Food > Canned Goods > Black Beans", "singular"},
		{"chick peas", I, "Food > Canned Goods > Chickpeas", "split word"},
		{"measuring tape", I, "Tools > Tape Measure", "reversed words"},

		// Abbreviations.
		{"oliveoil", I, "Food > Oils and Vinegars > Olive Oil", "run together"},
		{"blk beans", I, "Food > Canned Goods > Black Beans", "abbreviated"},
		{"ap flour", I, "Food > Grains > All-Purpose Flour", "abbreviated"},
		{"tp", I, "Household > Paper Goods > Toilet Paper", "two letters"},

		// Genuinely ambiguous. Asking is the right answer; anything else is wrong.
		{"rice", I, "", "two rices"},
		{"oil", I, "", "two oils"},
		{"chile", I, "", "two chiles"},
		{"paper", I, "", "towels or toilet"},

		// Locations.
		{"Kitchen", L, "Kitchen", "literal"},
		{"Left Pantry", L, "Kitchen > Left Pantry", "literal leaf"},
		{"Overflow Shelf", L, "Garage > Overflow Shelf", "literal leaf"},
		{"Medicine Cabinet", L, "Bathroom > Medicine Cabinet", "literal leaf"},
		{"Tool Bench", L, "Garage > Tool Bench", "literal leaf"},
		{"fridge", L, "Kitchen > Fridge", "partial"},
		{"crisper", L, "Kitchen > Fridge > Crisper Drawer", "partial"},
		{"freezer", L, "Kitchen > Freezer", "partial"},
		{"spice cab", L, "Kitchen > Spice Cabinet", "abbreviated"},
		{"spicecab", L, "Kitchen > Spice Cabinet", "run together"},
		{"med cab", L, "Bathroom > Medicine Cabinet", "abbreviated"},
		{"bathrm", L, "Bathroom", "abbreviated"},
		{"garage overflow", L, "Garage > Overflow Shelf", "path without separator"},
		{"kitshelf1", L, "", "two Shelf 1s -- see TestAbbreviationFindsThePath"},
		{"shelf 1", L, "", "two of them"},
		{"shelf 2", L, "", "two of them"},

		// Categories.
		{"Spices", C, "Food > Spices", "literal leaf"},
		{"Dried Peppers", C, "Food > Spices > Dried Peppers", "literal leaf"},
		{"grains", C, "Food > Grains", "partial"},
		{"cleaning", C, "Household > Cleaning Supplies", "partial"},
		{"canned", C, "Food > Canned Goods", "partial"},
		{"oils", C, "Food > Oils and Vinegars", "partial"},
	}
}

// tally is what the corpus measures. The columns are ranked by how much they
// matter, worst first.
type tally struct {
	wrong   []string // answered confidently, and answered the wrong thing
	hit     int      // answered confidently and correctly
	refused int      // asked, and asking was right
	asked   []string // asked when it could have answered -- the acceptable failure
}

func (t tally) String() string {
	return fmt.Sprintf("wrong %2d   hit %2d   asked %2d   refused %2d",
		len(t.wrong), t.hit, len(t.asked), t.refused)
}

func measure(ix *resolve.Index, tuning resolve.Tuning, probes []probe) tally {
	var got tally
	for _, p := range probes {
		answer, confident := "", false
		switch o := ix.ResolveWith(tuning, p.query, p.kind).(type) {
		case resolve.Exact:
			answer, confident = o.Candidate.Path, true
		case resolve.Suggested:
			answer, confident = o.Candidate.Path, true
		}
		switch {
		case confident && answer == p.want:
			got.hit++
		case confident:
			got.wrong = append(got.wrong,
				fmt.Sprintf("%-16q → %s (want %q; %s)", p.query, answer, p.want, p.note))
		case p.want == "":
			got.refused++
		default:
			got.asked = append(got.asked, fmt.Sprintf("%-16q (want %s; %s)", p.query, p.want, p.note))
		}
	}
	return got
}

// TestCorpusSweep is the measurement. It asserts nothing about the sweep; it
// prints the table the chosen tuning came from, so a later change to the
// scoring or the index can be re-judged against the same queries.
//
//	go test ./internal/resolve/ -run CorpusSweep -v
//
// The table it printed when DefaultTuning was chosen:
//
//	gap     0   wrong  2   hit 49   asked  3   refused  6
//	gap     5   wrong  1   hit 48   asked  5   refused  6
//	gap    20   wrong  1   hit 47   asked  6   refused  6
//	gap   100   wrong  1   hit 47   asked  6   refused  6
//	gap   500   wrong  1   hit 47   asked  6   refused  6
//	gap  1000   wrong  1   hit 47   asked  6   refused  6
//	gap  1500   wrong  0   hit 47   asked  6   refused  7
//	gap  2000   wrong  0   hit 47   asked  6   refused  7
//	gap  5000   wrong  0   hit 47   asked  6   refused  7
//
// The single stubborn wrong answer up to gap 1000 is "kitshelf1" against a home
// with two Shelf 1s. It survives such a wide gap only because the scores are
// unnormalised -- 1848 against 631 for two equally good matches -- which is the
// measurement behind Basis carrying no percentage.
//
// 2000 was chosen over 1500 for margin, and the whole move from 5 to 2000 costs
// exactly one query: "spicecab" becomes a question instead of an answer. That
// is the asymmetry being paid for deliberately. A person pressing tab once is a
// smaller loss than a receipt filed onto the wrong shelf, and the wrong shelf
// is silent.
func TestCorpusSweep(t *testing.T) {
	ix, probes := household(), corpus()
	t.Logf("%d queries over %d candidates", len(probes), ix.Len())
	for _, gap := range []int{0, 5, 20, 100, 500, 1000, 1500, 2000, 5000} {
		tuning := resolve.Tuning{LetterMatchGap: gap, MaxCandidates: resolve.DefaultTuning.MaxCandidates}
		got := measure(ix, tuning, probes)
		t.Logf("gap %5d   %s", gap, got)
		for _, w := range got.wrong {
			t.Logf("             wrong: %s", w)
		}
	}
}

// TestCorpusAtDefaultTuning is the gate. It pins the two numbers that matter at
// the tuning the sweep chose, and fails on any regression in either.
func TestCorpusAtDefaultTuning(t *testing.T) {
	// The floor, not a target. Raising it is a deliberate act recorded in a
	// commit; the queries below it are listed by the failure so the tradeoff is
	// visible rather than a number moving.
	const floor = 47

	got := measure(household(), resolve.DefaultTuning, corpus())
	t.Logf("at %+v: %s", resolve.DefaultTuning, got)

	if len(got.wrong) > 0 {
		t.Errorf("%d queries were answered confidently and wrongly:\n  %s",
			len(got.wrong), strings.Join(got.wrong, "\n  "))
	}
	if got.hit < floor {
		t.Errorf("%d hits, floor is %d; these asked instead of answering:\n  %s",
			got.hit, floor, strings.Join(got.asked, "\n  "))
	}
	if got.hit > floor {
		t.Errorf("%d hits, which is better than the recorded floor of %d -- raise the floor",
			got.hit, floor)
	}
}
