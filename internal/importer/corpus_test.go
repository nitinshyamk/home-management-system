package importer_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"home-management-system/internal/command"
	"home-management-system/internal/importer"
)

// The agent corpus: a measured gate, not an assertion about what is right.
//
// The only real test of `hms schema --format prompt` is an agent reading it and
// emitting something that binds. Short of that, this is realistic input --
// receipts, grocery lists, an agent's JSON Lines -- reported as three numbers.
//
// The third number is the one that matters and the one no hand-written test
// would surface: rows that bind to the WRONG thing. A row that fails is a row
// somebody fixes. A row that quietly attaches to the wrong item is a row
// nobody ever looks at again.
//
//	go test ./internal/importer -run Corpus -v

// expectation is what a row SHOULD come to, written down for the rows where
// getting it wrong would be silent.
//
// Keyed by file and line, because a corpus that carried its answers inline
// would be a corpus somebody edited to match the output.
var expectations = map[string]string{
	// The clean receipt has to be clean. If any of it needs a person, the
	// vocabulary is harder to write than it should be.
	"01-clean-receipt.csv:2": "ready",
	"01-clean-receipt.csv:3": "ready",
	"01-clean-receipt.csv:4": "ready",

	// Receipt spellings. A dropped letter and a shouted name are the two most
	// common things a receipt does to a word.
	"02-receipt-spellings.csv:2": "needs confirmation", // Tumeric -> Turmeric, offered
	"02-receipt-spellings.csv:3": "ready",              // case is not a difference
	"02-receipt-spellings.csv:4": "ready",

	"03-grocery-list.csv:2": "ready",
	"03-grocery-list.csv:3": "ready",
	"03-grocery-list.csv:4": "ready",

	// Two rices. Only a person can say which, and guessing is the worst
	// available outcome.
	"04-ambiguous.csv:2": "blocked",
	"04-ambiguous.csv:3": "blocked",

	// Creation always stops and asks.
	"05-new-things.csv:2": "needs confirmation",
	"05-new-things.csv:3": "needs confirmation",

	// Values that are not values.
	"06-bad-values.csv:2": "blocked", // "lots"
	"06-bad-values.csv:3": "blocked", // ml into a mass
	"06-bad-values.csv:4": "blocked", // no package size

	"07-agent-jsonl.jsonl:1": "ready",
	"07-agent-jsonl.jsonl:2": "ready",
	"07-agent-jsonl.jsonl:3": "ready",

	// Plurals and partial names. A plural is a near-miss; a bare "Rice" is a tie.
	"08-plurals-and-brands.csv:2": "needs confirmation", // Turmerics -> Turmeric
	"08-plurals-and-brands.csv:3": "needs confirmation", // Basmati -> Basmati Rice
	"08-plurals-and-brands.csv:4": "blocked",            // two rices
}

// corpusHouse is the vocabulary the corpus is written against.
func corpusHouse(t *testing.T) *command.Vocabulary {
	t.Helper()
	// The rices come in bags and the turmeric does not, so `2bag` is
	// understood in one place and refused in another for the right reason.
	return houseWith(t,
		[]string{"Basmati Rice", "Brown Rice", "Turmeric"},
		[]string{"Basmati Rice", "Brown Rice"})
}

// TestCorpus is the measurement. It asserts the one thing that must never
// happen -- a row bound to the wrong thing -- and reports the rest.
func TestCorpus(t *testing.T) {
	vocabulary := corpusHouse(t)
	files, err := filepath.Glob("testdata/corpus/*")
	if err != nil || len(files) == 0 {
		t.Fatalf("no corpus: %v", err)
	}
	sort.Strings(files)

	var ready, confirmable, blocked int
	var wrong []string

	for _, path := range files {
		name := filepath.Base(path)
		plan := readCorpus(t, vocabulary, path)
		for _, entry := range plan.Entries {
			switch entry.State {
			case importer.Ready:
				ready++
			case importer.Confirmable:
				confirmable++
			case importer.Blocked:
				blocked++
			}
			key := fmt.Sprintf("%s:%d", name, entry.Row.Line)
			want, expected := expectations[key]
			if !expected {
				t.Errorf("%s has no expectation; add one rather than leaving it unmeasured", key)
				continue
			}
			if got := entry.State.String(); got != want {
				wrong = append(wrong, fmt.Sprintf("%s: %s, expected %s  (%s)",
					key, got, want, strings.Join(issueText(entry), "; ")))
			}
		}
	}

	t.Logf("%d files: %d ready, %d need confirming, %d blocked",
		len(files), ready, confirmable, blocked)

	// A row that came out somewhere other than where it should is the number
	// that matters. It is not "wrong answers" in the resolver's sense -- the
	// corpus cannot see inside a Command -- but a row that should have stopped
	// and did not is exactly the failure this measures.
	if len(wrong) > 0 {
		t.Errorf("%d rows landed somewhere other than expected:\n  %s",
			len(wrong), strings.Join(wrong, "\n  "))
	}
}

// TestCorpusNeverBindsSilentlyToTheWrongThing is the assertion the corpus
// exists for.
//
// Every row that came out READY is checked against what it was written to mean.
// A row that failed is a row somebody fixes; a row that quietly attached to the
// wrong item is a row nobody ever looks at again.
func TestCorpusNeverBindsSilentlyToTheWrongThing(t *testing.T) {
	vocabulary := corpusHouse(t)
	// The rows that name something exactly, and what they must resolve to.
	for _, tc := range []struct{ file, line, mustMention string }{
		{"02-receipt-spellings.csv", "3", "Basmati Rice"},
		{"02-receipt-spellings.csv", "4", "Turmeric"},
		{"03-grocery-list.csv", "2", "Turmeric"},
		{"03-grocery-list.csv", "3", "Basmati Rice"},
		{"03-grocery-list.csv", "4", "Brown Rice"},
	} {
		plan := readCorpus(t, vocabulary, "testdata/corpus/"+tc.file)
		for _, entry := range plan.Entries {
			if fmt.Sprintf("%d", entry.Row.Line) != tc.line || entry.Command == nil {
				continue
			}
			got := command.Summary(entry.Command, vocabulary.Names)
			if !strings.Contains(got, tc.mustMention) {
				t.Errorf("%s line %s resolved to %q, which is not %q",
					tc.file, tc.line, got, tc.mustMention)
			}
		}
	}
}

func readCorpus(t *testing.T, vocabulary *command.Vocabulary, path string) importer.Plan {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var rows []importer.Row
	if strings.HasSuffix(path, ".jsonl") {
		rows, err = importer.ReadJSONL(f)
	} else {
		rows, err = importer.ReadCSV(f)
	}
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return importer.Bind(context.Background(), vocabulary, path, rows)
}

func issueText(entry importer.Entry) []string {
	var out []string
	for _, issue := range entry.Issues {
		out = append(out, issue.String())
	}
	return out
}
