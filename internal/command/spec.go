package command

import (
	"fmt"
	"sort"
	"strings"

	"home-management-system/internal/domain"
)

// The vocabulary, declared once.
//
// One grammar, three surfaces: the `:` line's pair keys, a CSV header, and a
// JSON Lines key set are the SAME field names. That is what makes the
// interfaces genuinely one thing rather than three that resemble each other,
// and it is only true because all three read this table.
//
// It is also what `hms schema` emits, so an agent targets a fixed spec rather
// than a remembered one, and it cannot drift from what Bind accepts because it
// is the same declaration.

// FieldType says how a field's text becomes a value, and it is what the schema
// output tells an agent to produce.
type FieldType string

const (
	// FieldName is resolved through the index. Nothing else is: a Command holds
	// identifiers, and this is the only kind of field that produces one.
	FieldName FieldType = "name"
	// FieldQuantity is the quantity grammar -- 100, 100g, 2bag.
	FieldQuantity FieldType = "quantity"
	FieldDate     FieldType = "date"
	FieldMoney    FieldType = "money"
	FieldYesNo    FieldType = "yes/no"
	FieldUnit     FieldType = "unit"
	FieldChoice   FieldType = "choice"
	// FieldText is taken as written. Notes, labels, reasons -- nothing to get
	// wrong, and nothing to validate.
	FieldText FieldType = "text"
)

// Field is one named input.
type Field struct {
	Key  string
	Type FieldType
	What string

	// Required means Bind reports it Missing rather than leaving it unset.
	Required bool
	// Positional means it may be written without its key, in the order the
	// fields are declared. Every positional field precedes every pair-only one.
	Positional bool

	// Kinds narrows what a FieldName may refer to, which is what lets `at` mean
	// a Location even when an Item is called the same thing.
	Kinds []domain.EntityKind
	// Choices is the closed vocabulary of a FieldChoice.
	Choices []string
}

// Spec is one command's shape.
type Spec struct {
	Op     Op
	What   string
	Fields []Field

	// Creates names what this command brings into existence, or "" when it
	// brings nothing.
	//
	// Declared rather than inferred, because it cannot be inferred: `new item`
	// BINDS perfectly well -- its name is text, not a reference -- so nothing
	// about the bind result says a thing is about to exist. A plan screen that
	// waited for a failure to tell it would let creation through silently,
	// which is the one thing an import must never do.
	Creates domain.EntityKind
}

// Field returns the named field, if the command has one.
func (s Spec) Field(key string) (Field, bool) {
	for _, f := range s.Fields {
		if f.Key == key {
			return f, true
		}
	}
	return Field{}, false
}

// Positional returns the fields that may be written without their key, in the
// order they are written.
func (s Spec) Positional() []Field {
	var out []Field
	for _, f := range s.Fields {
		if f.Positional {
			out = append(out, f)
		}
	}
	return out
}

// Keys returns every field key, for a CSV header or a schema listing.
func (s Spec) Keys() []string {
	out := make([]string, 0, len(s.Fields))
	for _, f := range s.Fields {
		out = append(out, f.Key)
	}
	return out
}

func name(key, what string, required, positional bool, kinds ...domain.EntityKind) Field {
	return Field{Key: key, Type: FieldName, What: what, Required: required, Positional: positional, Kinds: kinds}
}

func text(key, what string) Field {
	return Field{Key: key, Type: FieldText, What: what}
}

// basisField narrows to one of the two Holdings an Item routinely has in one
// place: the sealed packages and the loose contents.
//
// It sits on the commands that act on ONE Holding -- discard, count, move --
// and not on consume or open, which name an Item and a place and let the
// operation work out which Holding that implies (opening a package if it has
// to). The difference is not an inconsistency: throwing away from the sealed
// bag and from the open one are different acts, and only a person can say
// which, whereas consuming is one act however the stock is arranged.
var basisField = Field{
	Key: "basis", Type: FieldChoice, What: "sealed packages or loose contents",
	Choices: []string{"sealed", "loose"},
}

// specs is the whole vocabulary. A missing entry is a test failure, not a
// runtime surprise: TestEveryCommandHasASpec walks the generated registry.
var specs = []Spec{
	// -----------------------------------------------------------------------
	// Origination
	// -----------------------------------------------------------------------
	{Op: OpNewItem, What: "add a kind of thing you keep", Creates: domain.EntityItem, Fields: []Field{
		{Key: "name", Type: FieldText, What: "what it is called", Required: true, Positional: true},
		{Key: "counting", Type: FieldChoice, What: "how it is counted -- PERMANENT",
			Required: true, Choices: []string{"unique", "pile", "measured"}},
		{Key: "unit", Type: FieldUnit, What: "what it is measured in -- PERMANENT"},
		{Key: "package", Type: FieldQuantity, What: "how much is in one package -- PERMANENT"},
		// Required, because items.category_id is NOT NULL. Left optional it
		// reached the database and came back as "FOREIGN KEY constraint
		// failed", which is the schema talking to itself where a sentence
		// belonged.
		name("category", "where to file it", true, false, domain.EntityCategory),
		text("notes", "anything worth knowing about it"),
	}},
	{Op: OpNewCategory, What: "add a classification", Creates: domain.EntityCategory, Fields: []Field{
		{Key: "name", Type: FieldText, What: "what it is called", Required: true, Positional: true},
		name("under", "the classification it belongs to", false, false, domain.EntityCategory),
		text("describe", "what belongs in it"),
	}},
	{Op: OpNewLocation, What: "add a place", Creates: domain.EntityLocation, Fields: []Field{
		{Key: "name", Type: FieldText, What: "what it is called", Required: true, Positional: true},
		name("under", "the place it is inside", false, false, domain.EntityLocation),
		text("describe", "what it is"),
	}},
	{Op: OpNewHolding, What: "keep a thing somewhere, at nothing", Creates: domain.EntityHolding, Fields: []Field{
		name("item", "what is kept there", true, true, domain.EntityItem),
		name("at", "where it is kept", true, false, domain.EntityLocation),
		{Key: "basis", Type: FieldChoice, What: "counted as sealed packages or as contents",
			Choices: []string{"content", "package"}},
		{Key: "expires", Type: FieldDate, What: "the date printed on it"},
		text("label", "which one this is"),
	}},

	// -----------------------------------------------------------------------
	// Recording -- stock
	// -----------------------------------------------------------------------
	{Op: OpReceive, What: "stock arrived", Fields: []Field{
		name("item", "what arrived", true, true, domain.EntityItem),
		{Key: "qty", Type: FieldQuantity, What: "how much", Required: true, Positional: true},
		name("at", "where it went", false, false, domain.EntityLocation),
		text("from", "where it came from"),
		{Key: "price", Type: FieldMoney, What: "what it cost"},
		{Key: "expires", Type: FieldDate, What: "the date printed on it"},
	}},
	{Op: OpConsume, What: "stock was used", Fields: []Field{
		name("item", "what was used", true, true, domain.EntityItem),
		{Key: "qty", Type: FieldQuantity, What: "how much", Required: true, Positional: true},
		name("at", "which one, if it is kept in several places", false, false, domain.EntityLocation),
		text("reason", "what it was for"),
	}},
	{Op: OpDiscard, What: "stock was thrown away", Fields: []Field{
		name("item", "what was thrown away", true, true, domain.EntityItem),
		{Key: "qty", Type: FieldQuantity, What: "how much", Required: true, Positional: true},
		name("at", "which one, if it is kept in several places", false, false, domain.EntityLocation),
		basisField,
		text("reason", "why"),
	}},
	{Op: OpOpen, What: "open a sealed package", Fields: []Field{
		name("item", "what to open", true, true, domain.EntityItem),
		name("at", "which one, if it is kept in several places", false, false, domain.EntityLocation),
	}},
	{Op: OpCount, What: "record what is actually there", Fields: []Field{
		name("item", "what was counted", true, true, domain.EntityItem),
		{Key: "observed", Type: FieldQuantity, What: "how much was found", Required: true, Positional: true},
		name("at", "which one, if it is kept in several places", false, false, domain.EntityLocation),
		basisField,
	}},
	{Op: OpMove, What: "stock went somewhere else", Fields: []Field{
		name("item", "what moved", true, true, domain.EntityItem),
		name("to", "where it went", true, false, domain.EntityLocation),
		name("at", "which one, if it is kept in several places", false, false, domain.EntityLocation),
		basisField,
		{Key: "qty", Type: FieldQuantity, What: "how much, if not all of it"},
	}},

	// -----------------------------------------------------------------------
	// Recording -- custody, lifecycle, typing, the Location tree
	// -----------------------------------------------------------------------
	{Op: OpCheckOut, What: "take something away with you", Fields: []Field{
		name("holding", "which one", true, true, domain.EntityHolding),
		name("to", "where it is going", false, false, domain.EntityLocation),
	}},
	{Op: OpReturn, What: "bring something back", Fields: []Field{
		name("holding", "which one", true, true, domain.EntityHolding),
	}},
	{Op: OpMarkLost, What: "give up on finding something", Fields: []Field{
		name("holding", "which one", true, true, domain.EntityHolding),
	}},
	{Op: OpFound, What: "it turned up after all", Fields: []Field{
		name("holding", "which one", true, true, domain.EntityHolding),
		name("at", "where it turned up, if not where it belongs", false, false, domain.EntityLocation),
	}},
	{Op: OpVerify, What: "look for something and record what you saw", Fields: []Field{
		name("holding", "which one", true, true, domain.EntityHolding),
		{Key: "present", Type: FieldYesNo, What: "was it there", Required: true},
	}},
	{Op: OpRetire, What: "this one is done with", Fields: []Field{
		name("holding", "which one", true, true, domain.EntityHolding),
		text("reason", "why"),
	}},
	{Op: OpRehome, What: "this lives somewhere else now", Fields: []Field{
		name("holding", "which one", true, true, domain.EntityHolding),
		name("to", "where it lives now", true, false, domain.EntityLocation),
	}},
	{Op: OpPromote, What: "track each one individually from now on", Fields: []Field{
		name("item", "which kind of thing", true, true, domain.EntityItem),
	}},
	{Op: OpDemote, What: "count this as an amount from now on", Fields: []Field{
		name("item", "which kind of thing", true, true, domain.EntityItem),
		{Key: "unit", Type: FieldUnit, What: "what to measure it in -- PERMANENT", Required: true},
		{Key: "package", Type: FieldQuantity, What: "how much is in one package"},
	}},
	{Op: OpReparentLocation, What: "move a place, and everything in it", Fields: []Field{
		name("location", "which place", true, true, domain.EntityLocation),
		name("under", "the place it is inside now", false, false, domain.EntityLocation),
	}},
	{Op: OpArchiveLocation, What: "put a place away", Fields: []Field{
		name("location", "which place", true, true, domain.EntityLocation),
		{Key: "resolution", Type: FieldChoice, What: "what happens to what is in it",
			Required: true, Choices: []string{"lift", "move", "block"}},
		name("to", "where its contents go, if moving", false, false, domain.EntityLocation),
	}},
	{Op: OpRestoreLocation, What: "bring a place back", Fields: []Field{
		name("location", "which place", true, true, domain.EntityLocation),
	}},

	// -----------------------------------------------------------------------
	// Annotation
	// -----------------------------------------------------------------------
	{Op: OpRename, What: "call something else", Fields: []Field{
		name("target", "what to rename", true, true,
			domain.EntityCategory, domain.EntityLocation, domain.EntityItem),
		{Key: "name", Type: FieldText, What: "the new name", Required: true, Positional: true},
	}},
	{Op: OpDescribe, What: "say what a place or classification is for", Fields: []Field{
		name("target", "which one", true, true, domain.EntityCategory, domain.EntityLocation),
		{Key: "text", Type: FieldText, What: "the description", Required: true, Positional: true},
	}},
	{Op: OpNote, What: "record something worth knowing", Fields: []Field{
		name("item", "about what", true, true, domain.EntityItem),
		{Key: "text", Type: FieldText, What: "the note", Required: true, Positional: true},
	}},
	{Op: OpReclassify, What: "file a thing somewhere else", Fields: []Field{
		name("item", "which thing", true, true, domain.EntityItem),
		name("to", "the classification it belongs to", true, false, domain.EntityCategory),
	}},
	{Op: OpConfirm, What: "yes, it really is filed correctly", Fields: []Field{
		name("item", "which thing", true, true, domain.EntityItem),
	}},
	{Op: OpSetExpiry, What: "correct the date printed on something", Fields: []Field{
		name("holding", "which one", true, true, domain.EntityHolding),
		{Key: "date", Type: FieldDate, What: "the date, or blank to clear it", Positional: true},
	}},
	{Op: OpLabel, What: "name one of several", Fields: []Field{
		name("holding", "which one", true, true, domain.EntityHolding),
		{Key: "text", Type: FieldText, What: "the label", Required: true, Positional: true},
	}},
	{Op: OpSnooze, What: "stop nagging about this for a while", Fields: []Field{
		name("holding", "which one", true, true, domain.EntityHolding),
		{Key: "until", Type: FieldDate, What: "when to start again, or blank to stop snoozing"},
	}},
	{Op: OpArchiveItem, What: "stop stocking a kind of thing", Fields: []Field{
		name("target", "which kind of thing", true, true, domain.EntityItem),
	}},
	{Op: OpArchiveCategory, What: "put a classification away", Fields: []Field{
		name("target", "which classification", true, true, domain.EntityCategory),
		{Key: "resolution", Type: FieldChoice, What: "what happens to what is filed under it",
			Required: true, Choices: []string{"lift", "move", "block"}},
		name("to", "where its contents go, if moving", false, false, domain.EntityCategory),
	}},
	{Op: OpRestoreCategory, What: "bring a classification back", Fields: []Field{
		name("target", "which classification", true, true, domain.EntityCategory),
	}},
	{Op: OpReparentCategory, What: "file a classification somewhere else", Fields: []Field{
		name("target", "which classification", true, true, domain.EntityCategory),
		name("under", "the classification it belongs to", false, false, domain.EntityCategory),
	}},
}

var specByOp = func() map[Op]Spec {
	m := make(map[Op]Spec, len(specs))
	for _, s := range specs {
		m[s.Op] = s
	}
	return m
}()

// SpecOf returns a command's shape.
func SpecOf(op Op) (Spec, bool) {
	s, ok := specByOp[op]
	return s, ok
}

// Specs returns the whole vocabulary, ordered by op so the schema output is
// stable between runs.
func Specs() []Spec {
	out := append([]Spec(nil), specs...)
	sort.Slice(out, func(i, j int) bool { return out[i].Op < out[j].Op })
	return out
}

// Ops returns every op, longest first.
//
// Longest first is not cosmetic: several ops are two words and share a first
// one, so "archive location" must be matched before "archive" could be. There
// is no bare "archive" today, and ordering this way means adding one would not
// silently capture the others.
func Ops() []Op {
	out := make([]Op, 0, len(specs))
	for _, s := range specs {
		out = append(out, s.Op)
	}
	sort.Slice(out, func(i, j int) bool {
		li, lj := strings.Count(string(out[i]), " "), strings.Count(string(out[j]), " ")
		if li != lj {
			return li > lj
		}
		return out[i] < out[j]
	})
	return out
}

func (f Field) String() string {
	s := fmt.Sprintf("%s (%s)", f.Key, f.Type)
	if f.Required {
		s += " required"
	}
	return s
}
