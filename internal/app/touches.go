package app

import (
	"home-management-system/internal/command"
	"home-management-system/internal/domain"
)

// Lands is the place a Command would take effect, where it names one.
//
// It exists so a screen reviewing a batch of proposed changes can point the
// house at what each row would touch. Reading the place out of the row's
// rendered sentence would be the round trip through text this layer refuses
// everywhere else: two shelves can share a name, and the one the command
// holds is an identifier.
//
// Not every command names a place, and that is the honest answer for the ones
// that do not -- renaming an item happens nowhere in particular. A caller
// that gets false leaves the house where it was rather than guessing.
func Lands(cmd command.Command) (domain.LocationID, bool) {
	switch c := cmd.(type) {
	case command.NewHolding:
		return c.Location, true
	case command.Receive:
		return c.Location, true
	case command.Consume:
		return c.Location, true
	case command.Open:
		return c.Location, true
	case command.Move:
		// Where it is GOING. A move's row is about its destination; the shelf
		// it is leaving is the one already on screen.
		return c.To, true
	case command.NewLocation:
		if c.Parent != nil {
			return *c.Parent, true
		}
	case command.ReparentLocation:
		if c.Parent != nil {
			return *c.Parent, true
		}
	case command.ArchiveLocation:
		return c.Location, true
	case command.RestoreLocation:
		return c.Location, true
	}
	return 0, false
}
