// Package importer turns a file into commands.
//
// CSV and JSON Lines are interchangeable TRANSPORTS for one typed contract, not
// two formats with two meanings. The pair keys are the columns and the JSON
// keys are the same names, so there is nothing extra to learn for either -- and
// a row and its record must Bind to an IDENTICAL Command, which is asserted
// rather than hoped for.
//
// Nothing here resolves a name or parses a value. It produces RawCommand, which
// is untrusted text, and Bind is the one place that stops being true.
package importer

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"home-management-system/internal/command"
)

// ErrMalformed reports a file that is not readable as commands at all, as
// distinct from a row that will not bind. The difference matters: the first is
// a file to fix, the second is a row to fix.
var ErrMalformed = errors.New("importer: cannot read")

// Row is one command as it arrived, with where it came from.
//
// Line is kept because an import reports per row, and "row 4" is the only
// address a person has for a line in a file they wrote somewhere else.
type Row struct {
	Raw  command.RawCommand
	Line int
	// Unknown lists column names this row's op has no field for. They are
	// reported rather than ignored: a typo'd header would otherwise vanish
	// silently and the row would do something subtly different from what was
	// written.
	Unknown []string
}

// ReadCSV parses a CSV whose header names the fields.
func ReadCSV(r io.Reader) ([]Row, error) {
	reader := csv.NewReader(r)
	// Rows legitimately differ in width -- an `acquire` row fills different
	// columns from a `new item` row -- so the field count is not fixed.
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("%w: the file is empty", ErrMalformed)
	}

	header := make([]string, len(records[0]))
	for i, name := range records[0] {
		header[i] = strings.ToLower(strings.TrimSpace(name))
	}
	if !contains(header, "op") {
		return nil, fmt.Errorf("%w: no `op` column, so nothing says what the rows do", ErrMalformed)
	}

	var out []Row
	for n, record := range records[1:] {
		// +2: one for the header, one because a person counts from 1.
		line := n + 2
		fields := map[string]string{}
		for i, value := range record {
			if i >= len(header) {
				break
			}
			if value = strings.TrimSpace(value); value != "" {
				fields[header[i]] = value
			}
		}
		if blank(fields) {
			continue
		}
		out = append(out, newRow(fields, line))
	}
	return out, nil
}

// ReadJSONL parses JSON Lines, one record per line.
func ReadJSONL(r io.Reader) ([]Row, error) {
	decoder := json.NewDecoder(r)
	var out []Row
	for line := 1; ; line++ {
		var record map[string]any
		err := decoder.Decode(&record)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: line %d: %w", ErrMalformed, line, err)
		}

		fields := map[string]string{}
		for key, value := range record {
			if text := scalar(value); text != "" {
				fields[strings.ToLower(strings.TrimSpace(key))] = text
			}
		}
		if blank(fields) {
			continue
		}
		out = append(out, newRow(fields, line))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: no records", ErrMalformed)
	}
	return out, nil
}

// newRow lifts a bag of strings into a RawCommand and notes what did not belong.
func newRow(fields map[string]string, line int) Row {
	op := fields["op"]
	delete(fields, "op")

	row := Row{Raw: command.RawCommand{Op: op, Fields: fields, Line: line}, Line: line}
	spec, known := command.SpecOf(command.Op(strings.ToLower(op)))
	if !known {
		// An unknown op is Bind's to report, in its own words. Every column
		// looks unknown here and saying so twice would bury the real problem.
		return row
	}
	for key := range fields {
		if _, ok := spec.Field(key); !ok {
			row.Unknown = append(row.Unknown, key)
		}
	}
	sort.Strings(row.Unknown)
	return row
}

// scalar renders a JSON value as the text a CSV cell would have held.
//
// Numbers come back from encoding/json as float64, and 2000 must not become
// "2000.000000" -- a package size that reads as a fraction is a package size
// that will not parse.
func scalar(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case bool:
		return strconv.FormatBool(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return fmt.Sprint(value)
}

func blank(fields map[string]string) bool { return len(fields) == 0 }

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
