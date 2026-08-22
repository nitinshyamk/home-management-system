package importer_test

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"home-management-system/internal/command"
	"home-management-system/internal/importer"
)

// TestTheTwoTransportsAgree is the claim the package exists to make true.
//
// CSV and JSON Lines are interchangeable transports for ONE typed contract. The
// moment a row and its record parse differently, that claim is false -- and the
// difference would show up much later, as a receipt that behaved differently
// depending on which format somebody happened to export.
func TestTheTwoTransportsAgree(t *testing.T) {
	fromCSV := read(t, "receipt.csv", importer.ReadCSV)
	fromJSONL := read(t, "receipt.jsonl", importer.ReadJSONL)

	if len(fromCSV) != len(fromJSONL) {
		t.Fatalf("%d rows from CSV, %d from JSON Lines", len(fromCSV), len(fromJSONL))
	}
	for i := range fromCSV {
		if fromCSV[i].Raw.Op != fromJSONL[i].Raw.Op {
			t.Errorf("row %d: op %q vs %q", i+1, fromCSV[i].Raw.Op, fromJSONL[i].Raw.Op)
		}
		if !reflect.DeepEqual(fromCSV[i].Raw.Fields, fromJSONL[i].Raw.Fields) {
			t.Errorf("row %d:\n  csv   %v\n  jsonl %v", i+1, fromCSV[i].Raw.Fields, fromJSONL[i].Raw.Fields)
		}
	}
}

// A number in JSON must arrive as the text a CSV cell would have held. 2000
// becoming "2000.000000" is a package size that reads as a fraction, and a
// package size that will not parse.
func TestJSONNumbersArriveAsWrittenNumbers(t *testing.T) {
	rows, err := importer.ReadJSONL(strings.NewReader(
		`{"op":"new item","name":"Turmeric","package":2000,"unit":"g"}` + "\n" +
			`{"op":"consume","item":"rice","qty":1.5}` + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := rows[0].Raw.Fields["package"]; got != "2000" {
		t.Errorf("package = %q, want %q", got, "2000")
	}
	if got := rows[1].Raw.Fields["qty"]; got != "1.5" {
		t.Errorf("qty = %q, want %q", got, "1.5")
	}
}

// Empty cells are ABSENT rather than empty, because Bind distinguishes "not
// given" from "given as nothing" -- an `at` left blank is inferred, and an `at`
// given as "" would be a location called nothing.
func TestBlankCellsAreAbsent(t *testing.T) {
	rows := read(t, "receipt.csv", importer.ReadCSV)
	if _, given := rows[1].Raw.Fields["at"]; given {
		t.Errorf("a blank cell arrived as a field: %v", rows[1].Raw.Fields)
	}
	// And a row of nothing but commas is not a row.
	all, err := importer.ReadCSV(strings.NewReader("op,item\nacquire,rice\n,\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Errorf("%d rows, want 1 -- a blank line is not a command", len(all))
	}
}

// A typo'd header would otherwise vanish silently and the row would do
// something subtly different from what was written.
func TestUnknownColumnsAreReported(t *testing.T) {
	rows, err := importer.ReadCSV(strings.NewReader(
		"op,item,qty,resaon\nconsume,rice,100,dinner\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := rows[0].Unknown; len(got) != 1 || got[0] != "resaon" {
		t.Errorf("unknown columns = %v, want the misspelt one", got)
	}
	// A column the op DOES have is not unknown, even when other rows do not
	// use it -- rows legitimately differ in which columns they fill.
	full := read(t, "receipt.csv", importer.ReadCSV)
	for _, row := range full {
		if len(row.Unknown) != 0 {
			t.Errorf("row %d reports %v as unknown", row.Line, row.Unknown)
		}
	}
}

// An unknown op is Bind's to report, in its own words. Saying so twice buries
// the real problem under a list of columns.
func TestAnUnknownOpIsLeftForBind(t *testing.T) {
	rows, err := importer.ReadCSV(strings.NewReader("op,item\nfrobnicate,rice\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows[0].Unknown) != 0 {
		t.Errorf("an unknown op reported %v as unknown columns", rows[0].Unknown)
	}
	if rows[0].Raw.Op != "frobnicate" {
		t.Errorf("op = %q", rows[0].Raw.Op)
	}
}

// The line number is the only address a person has for a row in a file they
// wrote somewhere else.
func TestRowsCarryTheirLine(t *testing.T) {
	rows := read(t, "receipt.csv", importer.ReadCSV)
	for i, want := range []int{2, 3, 4} {
		if rows[i].Line != want {
			t.Errorf("row %d says line %d, want %d", i+1, rows[i].Line, want)
		}
	}
}

// A file that is not commands at all is a different problem from a row that
// will not bind: the first is a file to fix, the second is a row to fix.
func TestAFileThatIsNotCommands(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"empty", ""},
		{"no op column", "item,qty\nrice,100\n"},
		{"ragged quoting", "op,item\nacquire,\"unclosed\n"},
	} {
		if _, err := importer.ReadCSV(strings.NewReader(tc.body)); err == nil {
			t.Errorf("%s: parsed", tc.name)
		}
	}
	if _, err := importer.ReadJSONL(strings.NewReader("not json\n")); err == nil {
		t.Error("parsed something that is not JSON")
	}
}

func read(t *testing.T, name string, parse func(io.Reader) ([]importer.Row, error)) []importer.Row {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := parse(f)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return rows
}

// TestEveryOpSurvivesBothTransports is the exit criterion, over the whole
// vocabulary rather than over one fixture.
//
// It compares RawCommands rather than Commands, and that is not a weaker claim:
// Bind is a pure function of a RawCommand, so two identical RawCommands bind to
// identical Commands necessarily. Comparing after Bind would need valid values
// for every field of every op -- and would then be testing Bind rather than the
// transports.
func TestEveryOpSurvivesBothTransports(t *testing.T) {
	for _, spec := range command.Specs() {
		t.Run(string(spec.Op), func(t *testing.T) {
			fields := plausible(spec)

			fromCSV, err := importer.ReadCSV(strings.NewReader(asCSV(spec.Op, fields)))
			if err != nil {
				t.Fatalf("csv: %v", err)
			}
			fromJSONL, err := importer.ReadJSONL(strings.NewReader(asJSONL(spec.Op, fields)))
			if err != nil {
				t.Fatalf("jsonl: %v", err)
			}
			if len(fromCSV) != 1 || len(fromJSONL) != 1 {
				t.Fatalf("%d csv rows, %d jsonl rows", len(fromCSV), len(fromJSONL))
			}
			if fromCSV[0].Raw.Op != fromJSONL[0].Raw.Op {
				t.Errorf("op %q vs %q", fromCSV[0].Raw.Op, fromJSONL[0].Raw.Op)
			}
			if !reflect.DeepEqual(fromCSV[0].Raw.Fields, fromJSONL[0].Raw.Fields) {
				t.Errorf("the two transports disagree:\n  csv   %v\n  jsonl %v",
					fromCSV[0].Raw.Fields, fromJSONL[0].Raw.Fields)
			}
			// And neither invented a column the op does not have.
			if got := fromCSV[0].Unknown; len(got) != 0 {
				t.Errorf("csv reports %v as unknown for its own op's fields", got)
			}
		})
	}
}

// plausible fills every field of a spec with something of roughly the right
// shape. The values do not have to be resolvable -- what is under test is the
// transport, not the vocabulary.
func plausible(spec command.Spec) map[string]string {
	out := map[string]string{}
	for _, f := range spec.Fields {
		switch f.Type {
		case command.FieldQuantity:
			out[f.Key] = "100"
		case command.FieldDate:
			out[f.Key] = "2027-03-01"
		case command.FieldMoney:
			out[f.Key] = "3.50"
		case command.FieldYesNo:
			out[f.Key] = "yes"
		case command.FieldChoice:
			out[f.Key] = f.Choices[0]
		case command.FieldUnit:
			out[f.Key] = "g"
		default:
			// A value with a comma and a space in it, because those are exactly
			// the characters a transport gets wrong.
			out[f.Key] = "Kitchen > Left Pantry, upper"
		}
	}
	return out
}

func asCSV(op command.Op, fields map[string]string) string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	header := append([]string{"op"}, keys...)
	record := append([]string{string(op)}, valuesOf(fields, keys)...)

	var b strings.Builder
	w := csv.NewWriter(&b)
	_ = w.Write(header)
	_ = w.Write(record)
	w.Flush()
	return b.String()
}

func asJSONL(op command.Op, fields map[string]string) string {
	record := map[string]string{"op": string(op)}
	for key, value := range fields {
		record[key] = value
	}
	encoded, _ := json.Marshal(record)
	return string(encoded) + "\n"
}

func valuesOf(fields map[string]string, keys []string) []string {
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, fields[key])
	}
	return out
}
