package intake

import (
	"fmt"
	"strings"
)

// The handoff: one file that says what to do with this folder.
//
// The schema says how to write a row. It does not say which folder the receipt
// is in, where the answer goes, or that nothing may be applied -- and those are
// exactly the three things an agent pointed at a directory gets wrong. So the
// contract goes out with a covering note, generated from the same workspace the
// paths come from, and the two are one file rather than two things to keep in
// step.

// HandoffName is the covering note inside schema/.
const HandoffName = "handoff.md"

func handoff(w Workspace, schema string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# Import handoff: %s\n\n", w.Name)
	b.WriteString("Turn the contents of the input directory into a plan, and write it to the\n")
	b.WriteString("plan file. Do nothing else.\n\n")

	fmt.Fprintf(&b, "  input directory  %s\n", w.InputDir())
	fmt.Fprintf(&b, "  plan file        %s\n", w.PlanFile())
	fmt.Fprintf(&b, "  this folder      %s\n\n", w.Dir)

	b.WriteString("The plan is JSON Lines: one JSON object per line, one command per line, with\n")
	b.WriteString("the field names in the contract below. Nothing else goes in the file -- no\n")
	b.WriteString("prose, no fences, no trailing summary. A line that is not a command is a\n")
	b.WriteString("file that will not read.\n\n")

	// Said here as well as in the rules, because it is the one instruction
	// whose violation is not visible in the output. Every other mistake shows
	// up as a row somebody can see and correct; this one shows up as a house
	// that changed before anybody looked.
	b.WriteString("You are not importing anything. A person reads every row on a review screen\n")
	b.WriteString("and decides what is applied. Do not run hms, and do not touch the database.\n\n")

	b.WriteString("---\n\n")
	b.WriteString(schema)
	return b.String()
}
