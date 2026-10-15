package gotable

import (
	"fmt"
	"strings"
)

// Surfaces selects which of the Go table surfaces a generate emits
// (docs/SPEC-TABLES.md §8.4). The zero value emits nothing; All emits
// everything and is byte-identical to generating before the flag existed.
// A surface's dependencies are pulled in by parseSurfaces: json, cook,
// message and retain each depend on wire.
type Surfaces struct {
	Wire, Fixed, JSON, Cook, Block, View, Message, Retain bool
}

// All is the default surface set: every surface, byte-for-byte what the Go
// target emitted before --surface existed.
var All = Surfaces{Wire: true, Fixed: true, JSON: true, Cook: true, Block: true, View: true, Message: true, Retain: true}

// ParseSurfaces turns a comma-separated surface list into a Surfaces value,
// pulling in each surface's dependencies and refusing unknown names.
func ParseSurfaces(list string) (Surfaces, error) {
	s := Surfaces{}
	if strings.TrimSpace(list) == "" {
		return All, nil
	}
	for _, raw := range strings.Split(list, ",") {
		name := strings.TrimSpace(raw)
		switch name {
		case "wire":
			s.Wire = true
		case "fixed":
			s.Fixed = true
		case "json":
			s.JSON = true
			s.Wire = true
		case "cook":
			s.Cook = true
			s.Wire = true
		case "block":
			s.Block = true
		case "view":
			s.View = true
		case "message":
			s.Message = true
			s.Wire = true
		case "retain":
			s.Retain = true
			s.Wire = true
		default:
			return Surfaces{}, fmt.Errorf("--surface names one of wire,fixed,json,cook,block,view,message,retain — %q is not one", name)
		}
	}
	return s, nil
}

// Has reports whether surface s is selected.
func (s Surfaces) Has(name string) bool {
	switch name {
	case "wire":
		return s.Wire
	case "fixed":
		return s.Fixed
	case "json":
		return s.JSON
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
