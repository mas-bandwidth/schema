package gotable

import (
	"fmt"
	"sort"
	"strings"
)

// SurfaceSet selects which of the Go table surfaces a generate emits
// (docs/SPEC-TABLES.md §8.4.1): wire, fixed, json, cook, block, view,
// message and retain. The zero value is empty; AllSurfaces is the default.
type SurfaceSet struct {
	Wire, Fixed, Json, Cook, Block, View, Message, Retain bool
}

// AllSurfaces is the default selection: everything, byte-identical to a
// generate that names no surface.
var AllSurfaces = SurfaceSet{
	Wire: true, Fixed: true, Json: true, Cook: true,
	Block: true, View: true, Message: true, Retain: true,
}

// surfaceNames maps a --surface spelling to its bit.
var surfaceNames = map[string]func(*SurfaceSet) bool{
	"wire":    func(s *SurfaceSet) bool { return s.Wire },
	"fixed":   func(s *SurfaceSet) bool { return s.Fixed },
	"json":    func(s *SurfaceSet) bool { return s.Json },
	"cook":    func(s *SurfaceSet) bool { return s.Cook },
	"block":   func(s *SurfaceSet) bool { return s.Block },
	"view":    func(s *SurfaceSet) bool { return s.View },
	"message": func(s *SurfaceSet) bool { return s.Message },
	"retain":  func(s *SurfaceSet) bool { return s.Retain },
}

// ParseSurfaces builds a SurfaceSet from --surface spellings. An empty list is
// AllSurfaces; an unknown spelling is refused by name.
func ParseSurfaces(names []string) (SurfaceSet, error) {
	if len(names) == 0 {
		return AllSurfaces, nil
	}
	var out SurfaceSet
	for _, n := range names {
		n = strings.TrimSpace(n)
		switch n {
		case "wire":
			out.Wire = true
		case "fixed":
			out.Fixed = true
		case "json":
			out.Json = true
		case "cook":
			out.Cook = true
		case "block":
			out.Block = true
		case "view":
			out.View = true
		case "message":
			out.Message = true
		case "retain":
			out.Retain = true
		default:
			return out, fmt.Errorf("surface %q is not one of wire, fixed, json, cook, block, view, message, retain", n)
		}
	}
	resolveSurfaces(&out)
	return out, nil
}

// resolveSurfaces honors the documented dependencies: the json, message, cook,
// view and fixed surfaces read the reflection descriptors, so they pull wire
// in behind them.
func resolveSurfaces(s *SurfaceSet) {
	if s.Json || s.Message || s.Cook || s.View || s.Fixed {
		s.Wire = true
	}
}

// surfaceList is the sorted spelling of a set, for diagnostics.
func (s SurfaceSet) surfaceList() []string {
	var out []string
	for n, has := range surfaceNames {
		if has(&s) {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}
