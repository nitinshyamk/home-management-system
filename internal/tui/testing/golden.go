package testing

import (
	"flag"
	"os"
	"path/filepath"
	"strings"

	"home-management-system/internal/tui/text"
)

// Golden frames.
//
// A golden is not a tier of its own -- it is one more assertion inside a
// Simulator test. And it means UNCHANGED, never good, which is why a frame is
// only captured after a person has signed the screen off: the alarm has to be
// anchored to a layout somebody approved rather than to the first one that
// compiled.
//
// Colour is stripped, so a frame is diffable plain text. A golden that differs
// only by escape sequences is a golden nobody reads, and a golden nobody reads
// is rubber-stamped -- which is worse than not having one.

var updateGolden = flag.Bool("update-golden", false,
	"rewrite the golden frames from the current output (only after a screen is signed off)")

// AssertFrame compares the current view against a checked-in frame.
//
//	go test ./internal/tui/testing -update-golden
//
// rewrites them. That is deliberately a flag rather than automatic: a golden
// that regenerates itself on mismatch is an elaborate way of asserting nothing.
func (s *Simulator) AssertFrame(name string) {
	s.t.Helper()
	path := filepath.Join("testdata", "golden", name+".txt")
	got := text.StripANSI(s.View()) + "\n"

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			s.t.Fatalf("golden: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			s.t.Fatalf("golden: %v", err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		s.t.Fatalf("golden %s is missing; capture it with -update-golden AFTER the screen is signed off: %v",
			name, err)
	}
	if string(want) == got {
		return
	}
	s.t.Errorf("the %s frame changed.\n\n%s\n\nIf this is the intended layout, "+
		"re-review the screen and then re-capture with -update-golden.",
		name, sideBySide(string(want), got))
}

// sideBySide renders the difference as the two frames, line by line, marking
// the lines that differ.
//
// A unified diff of a rendered table is unreadable -- every line differs by
// whitespace and none of it says what MOVED. The point of a frame is that it is
// looked at, so the failure has to be lookable-at too.
func sideBySide(want, got string) string {
	wantLines := strings.Split(strings.TrimRight(want, "\n"), "\n")
	gotLines := strings.Split(strings.TrimRight(got, "\n"), "\n")

	var b strings.Builder
	b.WriteString("--- approved ---\n")
	for i, line := range wantLines {
		b.WriteString(mark(i, gotLines, line) + line + "\n")
	}
	b.WriteString("\n--- now ---\n")
	for i, line := range gotLines {
		b.WriteString(mark(i, wantLines, line) + line + "\n")
	}
	return b.String()
}

func mark(i int, other []string, line string) string {
	if i < len(other) && other[i] == line {
		return "  "
	}
	return "! "
}

// PlainView is the view with colour stripped, for assertions that read the
// layout rather than look at it.
func (s *Simulator) PlainView() string { return text.StripANSI(s.View()) }
