// Package intake is the guided bulk import: the folder an import happens in,
// and the walk through it.
//
// A bulk import is not one act. A contract goes out, a photograph of a receipt
// comes back, something turns the one into rows against the other, and only
// then is there a plan for a person to approve. Those steps happen minutes or
// days apart and each produces a file, so the import IS a directory -- named,
// kept under the hms home, and still there tomorrow to be looked at when a row
// turns out to have been wrong.
//
// Three subdirectories, because the three files have three different authors.
// schema/ is written by hms and read by an agent. input/ is written by a person
// and read by an agent. plan/ is written by an agent and read by hms. Putting
// them in one folder would leave nobody able to say which of the files in it
// was the one to review.
package intake

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// SchemaDirName holds the contract: the command vocabulary and this house.
	SchemaDirName = "schema"

	// InputDirName holds what a person put in -- a text file, a photograph of a
	// receipt, a note. hms never reads it; it is there to be handed on.
	InputDirName = "input"

	// PlanDirName holds the rows that came back, which is the one file hms
	// actually imports.
	PlanDirName = "plan"

	// PlanFileName is the plan hms looks for. One name, so "which file is the
	// plan" is not a question an import has to answer twice.
	PlanFileName = "plan.jsonl"
)

// Workspace is one named import, and the directory it lives in.
type Workspace struct {
	// Name is what the import was called.
	Name string

	// Dir is the directory, which is always Root/Name.
	Dir string
}

// SchemaDir, InputDir and PlanDir are the three subdirectories.
func (w Workspace) SchemaDir() string { return filepath.Join(w.Dir, SchemaDirName) }
func (w Workspace) InputDir() string  { return filepath.Join(w.Dir, InputDirName) }
func (w Workspace) PlanDir() string   { return filepath.Join(w.Dir, PlanDirName) }

// PlanFile is where a plan is written when hms is the one writing it.
func (w Workspace) PlanFile() string { return filepath.Join(w.PlanDir(), PlanFileName) }

// Create opens the named import under root, making the directory and its three
// subdirectories if they are not there.
//
// Creating an import that already exists is not an error, and is in fact the
// point: an import is resumed by naming it again, which is the only way a
// workflow with a pause in the middle of it can be picked up the next day.
func Create(root, name string) (Workspace, error) {
	clean, err := CleanName(name)
	if err != nil {
		return Workspace{}, err
	}
	w := Workspace{Name: clean, Dir: filepath.Join(root, clean)}
	for _, dir := range []string{w.Dir, w.SchemaDir(), w.InputDir(), w.PlanDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Workspace{}, fmt.Errorf("intake: creating %s: %w", dir, err)
		}
	}
	return w, nil
}

// Existed reports whether the named import was already on disk, asked BEFORE
// Create makes it. A workflow that resumes has to be able to tell the
// difference, and after Create there is no longer one to tell.
func Existed(root, name string) bool {
	clean, err := CleanName(name)
	if err != nil {
		return false
	}
	info, err := os.Stat(filepath.Join(root, clean))
	return err == nil && info.IsDir()
}

// List names the imports under root, oldest name first, for a prompt that can
// show what is already there rather than asking into the dark.
func List(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("intake: reading %s: %w", root, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// nonEmpty is a file with something in it. An agent that failed halfway leaves
// a zero-byte file behind, and a zero-byte plan is not a plan to review -- it
// is a review of nothing, presented as though there were nothing to import.
func nonEmpty(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}

// Input lists the files a person dropped in, by name.
//
// Directories are ignored rather than walked. A folder of photographs is one
// thing a person might drop in, but counting its contents here would mean the
// count on screen and the thing being handed on disagreed about what "three
// files" meant.
func (w Workspace) Input() ([]string, error) {
	return visible(w.InputDir())
}

// Plan is the plan file to import, if one is there.
//
// PlanFileName is preferred, so a second run that regenerates the plan replaces
// what it reviews rather than adding to it. Any other single file in plan/ is
// accepted too: a plan written by hand, or by an agent that chose its own name,
// is still a plan, and refusing it over its extension would be refusing the
// thing the directory exists to hold.
func (w Workspace) Plan() (string, bool, error) {
	if canonical := w.PlanFile(); nonEmpty(canonical) {
		return canonical, true, nil
	}
	files, err := visible(w.PlanDir())
	if err != nil {
		return "", false, err
	}
	for _, name := range files {
		if path := filepath.Join(w.PlanDir(), name); nonEmpty(path) {
			return path, true, nil
		}
	}
	return "", false, nil
}

// visible lists the non-hidden files directly inside dir.
func visible(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("intake: reading %s: %w", dir, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

// CleanName turns what somebody typed into a directory name, or says why it
// cannot be one.
//
// Spaces become dashes rather than being refused, because "kitchen receipt
// march" is what a person types and a workflow that rejects it over a space has
// spent its first interaction telling somebody off. A separator is refused
// outright: an import called ../../etc is not a name with a problem, it is a
// different request entirely.
func CleanName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", fmt.Errorf("intake: an import needs a name")
	}
	if strings.ContainsAny(trimmed, `/\`) || strings.Contains(trimmed, "..") {
		return "", fmt.Errorf("intake: %q is a path, not a name -- an import is one folder under the imports directory", name)
	}
	if strings.HasPrefix(trimmed, ".") {
		return "", fmt.Errorf("intake: a name may not start with a dot")
	}

	var b strings.Builder
	for _, r := range trimmed {
		switch {
		case r == ' ' || r == '\t':
			b.WriteByte('-')
		case r < 0x20 || r == 0x7f:
			return "", fmt.Errorf("intake: %q contains a control character", name)
		default:
			b.WriteRune(r)
		}
	}
	// Runs of whitespace became runs of dashes, which is not what anybody meant.
	out := b.String()
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	out = strings.Trim(out, "-")
	if out == "" {
		return "", fmt.Errorf("intake: %q is not a name", name)
	}
	return out, nil
}
