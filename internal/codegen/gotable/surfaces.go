// Surfaces names the generated surfaces a Go `generate` emit may select
// (docs/SPEC-TABLES.md §8.4, docs/USAGE.md). The default is all of them, and
// the default emit is byte-identical to what the Go target always emitted.
//
// The WIRE surface is the irreducible base: the TABLE-wire codecs, the storage
// structs, and the reflection descriptors live in <Base>Table.go and are
// emitted for every list, because every other surface leans on them. The rest
// are optional and add their own file or functions on top — json, cook, block,
// view, message and retain all read the wire's storage or descriptors, so
// naming one of them brings the wire with it automatically (the dependency is
// resolved, not an error). Only "wire" alone is meaningful without it.
package gotable

import (
	"fmt"
	"strings"
)

// Surfaces is a set of the Go target's generated surfaces.
type Surfaces struct {
	Wire    bool
	Fixed   bool
	JSON    bool
	Cook    bool
	Block   bool
	View    bool
	Message bool
	Retain  bool
}

// surfaceNames is the canonical order, used by the docs and by error text.
var surfaceNames = []string{"wire", "fixed", "json", "cook", "block", "view", "message", "retain"}

// allSurfaces is the default: everything. It must stay byte-identical to the
// emit the Go target produced before the flag existed.
func allSurfaces() Surfaces {
	return Surfaces{Wire: true, Fixed: true, JSON: true, Cook: true, Block: true, View: true, Message: true, Retain: true}
}

func (s *Surfaces) setName(name string) bool {
	switch name {
	case "wire":
		s.Wire = true
	case "fixed":
		s.Fixed = true
	case "json":
		s.JSON = true
	case "cook":
		s.Cook = true
	case "block":
		s.Block = true
	case "view":
		s.View = true
	case "message":
		s.Message = true
	case "retain":
		s.Retain = true
	default:
		return false
	}
	return true
}

// ParseSurfaces parses a comma-separated surface list. Empty means the default
// (all). An unknown name is an error.
func ParseSurfaces(list string) (Surfaces, error) {
	if strings.TrimSpace(list) == "" {
		return allSurfaces(), nil
	}
	s := Surfaces{}
	for _, part := range strings.Split(list, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		if !s.setName(name) {
			return s, fmt.Errorf("unknown surface %q (docs/SPEC-TABLES.md §8.4; %s are the surfaces)", name, strings.Join(surfaceNames, ", "))
		}
	}
	// Every optional surface leans on the wire's storage and codecs, so the
	// dependency is resolved rather than refused: naming one brings the wire.
	if s.Fixed || s.JSON || s.Cook || s.Block || s.View || s.Message || s.Retain {
		s.Wire = true
	}
	return s, nil
}

// has returns whether the set includes any of the named optional surfaces.
func (s Surfaces) hasOptional() bool {
	return s.Fixed || s.JSON || s.Cook || s.Block || s.View || s.Message || s.Retain
}
