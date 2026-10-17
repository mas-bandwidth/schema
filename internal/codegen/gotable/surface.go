package gotable

import (
	"fmt"
	"strings"
)

// Surfaces is the set of table surfaces a generate emits (docs/SPEC-TABLES.md
// §8.8). Every surface rides on the wire surface: wire is the base and cannot
// be dropped on its own, and selecting any other surface implies it.
type Surfaces struct {
	Wire    bool
	Fixed   bool
	JSON    bool
	Cook    bool
	Block   bool
	View    bool
	Message bool
	Retain  bool
	// TextForm is the hand-editable text form (docs/SPEC-TABLES.md §16.9),
	// <Base>TableTextForm.go. It is SELECTED BY NAME and never in the default
	// set, so a default generate is byte-identical with or without it; it
	// rides on the JSON text form, so naming it implies json (and so wire).
	TextForm bool
}

// DefaultSurfaces is the default set: a generate without --surface is
// byte-identical to one with --surface wire,fixed,json,cook,block,view,message,retain.
// The textform surface is the one not in it: it is emitted only when named.
func DefaultSurfaces() Surfaces {
	return Surfaces{Wire: true, Fixed: true, JSON: true, Cook: true, Block: true, View: true, Message: true, Retain: true}
}

// ParseSurfaces parses a comma-separated surface list. The empty list is the
// default full set. Dependencies resolve here: naming any surface implies wire.
func ParseSurfaces(list string) (Surfaces, error) {
	if strings.TrimSpace(list) == "" {
		return DefaultSurfaces(), nil
	}
	s := Surfaces{}
	for _, item := range strings.Split(list, ",") {
		switch strings.TrimSpace(item) {
		case "wire":
			s.Wire = true
		case "fixed":
			s.Fixed = true
			s.Wire = true
		case "json":
			s.JSON = true
			s.Wire = true
		case "cook":
			s.Cook = true
			s.Wire = true
		case "block":
			s.Block = true
			s.Wire = true
		case "view":
			s.View = true
			s.Wire = true
		case "message":
			s.Message = true
			s.Wire = true
		case "retain":
			s.Retain = true
			s.Wire = true
		case "textform":
			s.TextForm = true
			s.JSON = true
			s.Wire = true
		default:
			return s, fmt.Errorf("unknown surface %q — the nine are wire,fixed,json,cook,block,view,message,retain,textform (docs/SPEC-TABLES.md §8.8)", item)
		}
	}
	return s, nil
}
