// Surface selection for the Go table emitter (docs/SPEC-TABLES.md §8.4): a
// generate may name the surfaces it wants — wire, fixed, json, cook, block,
// view, message, retain — and the emitter then emits only the files and
// functions the list needs. The default is all of them, byte-identical to a
// generate that names none.
package gotable

import (
	"fmt"
	"strings"
)

// SurfaceSet is the enabled surface set. The zero value means nothing is
// enabled; ParseSurfaces builds one from a --surface list.
type SurfaceSet struct {
	Wire    bool
	Fixed   bool
	Json    bool
	Cook    bool
	Block   bool
	View    bool
	Message bool
	Retain  bool
}

// allSurfaces is the default: every surface, byte-identical to a generate with
// no --surface.
func allSurfaces() SurfaceSet {
	return SurfaceSet{Wire: true, Fixed: true, Json: true, Cook: true, Block: true, View: true, Message: true, Retain: true}
}

// allow reports whether the named surface is enabled.
func (s SurfaceSet) allow(name string) bool {
	switch name {
	case "wire":
		return s.Wire
	case "fixed":
		return s.Fixed
	case "json":
		return s.Json
	case "cook":
		return s.Cook
	case "block":
		return s.Block
	case "view":
		return s.View
	case "message":
		return s.Message
	case "retain":
		return s.Retain
	}
	return false
}

// WireOrDependents reports whether the wire surface or any surface that depends
// on it is enabled — the condition under which the wire codecs, descriptors and
// wire runtime are emitted.
func (s SurfaceSet) WireOrDependents() bool {
	return s.Wire || s.Json || s.View || s.Message || s.Cook || s.Retain || s.Fixed
}

// ParseSurfaces parses a --surface list. An empty or all-whitespace list means
// every surface. Unknown names are an error naming the flag, and every surface
// that depends on another pulls its dependencies in — json, view, message, cook
// and retain each take wire, and retain takes message and cook too.
func ParseSurfaces(list string) (SurfaceSet, error) {
	if strings.TrimSpace(list) == "" {
		return allSurfaces(), nil
	}
	var s SurfaceSet
	for _, name := range strings.Split(list, ",") {
		switch name = strings.TrimSpace(name); name {
		case "all":
			return allSurfaces(), nil
		case "wire":
			s.Wire = true
		case "fixed":
			s.Fixed = true
		case "json":
			s.Json = true
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
			return SurfaceSet{}, fmt.Errorf("unknown --surface %q — want one of wire, fixed, json, cook, block, view, message, retain", name)
		}
	}
	if s.Json || s.View || s.Message || s.Cook || s.Retain {
		s.Wire = true
	}
	if s.Retain {
		s.Message = true
	}
	return s, nil
}
