package gotable

import (
	"fmt"
	"strings"
)

// Surface is the set of table surfaces a Go generate emits (docs/SPEC-TABLES.md
// §8.4): `generate --lang go --surface <list>`. The zero value is NOT the
// default; SurfaceAll is, and a generate that names no surface is byte-identical
// to one that names all eight.
type Surface uint8

const (
	// SurfaceWire is the TABLE wire: the type definitions, the descriptors and
	// the Measure/Save/Load codecs in <Base>Table.go. It is the base every other
	// surface stands on, so it is emitted for every list.
	SurfaceWire Surface = 1 << iota
	SurfaceFixed
	SurfaceJSON
	SurfaceCook
	SurfaceBlock
	SurfaceView
	SurfaceMessage
	SurfaceRetain

	SurfaceAll = SurfaceWire | SurfaceFixed | SurfaceJSON | SurfaceCook | SurfaceBlock | SurfaceView | SurfaceMessage | SurfaceRetain
)

var surfaceNames = []struct {
	name string
	bit  Surface
}{
	{"wire", SurfaceWire}, {"fixed", SurfaceFixed}, {"json", SurfaceJSON}, {"cook", SurfaceCook},
	{"block", SurfaceBlock}, {"view", SurfaceView}, {"message", SurfaceMessage}, {"retain", SurfaceRetain},
}

// ParseSurface reads a comma-separated list of surface names. An empty list is
// SurfaceAll (the default). An unknown or empty name is an error that names it
// and the eight that exist. Dependencies are resolved by Resolve, not here.
func ParseSurface(list string) (Surface, error) {
	if strings.TrimSpace(list) == "" {
		return SurfaceAll, nil
	}
	var s Surface
	for _, name := range strings.Split(list, ",") {
		name = strings.TrimSpace(name)
		found := false
		for _, n := range surfaceNames {
			if n.name == name {
				s |= n.bit
				found = true
			}
		}
		if !found {
			var all []string
			for _, n := range surfaceNames {
				all = append(all, n.name)
			}
			return 0, fmt.Errorf("unknown surface %q in --surface: the surfaces are %s", name, strings.Join(all, ", "))
		}
	}
	return s.Resolve(), nil
}

// Resolve closes the set under the dependencies SPEC-TABLES.md §8.4 names:
// every surface stands on wire; block rides on the cook columns the descriptors
// carry, so block implies cook; the cook writer reuses the text walk's raw
// reader and guard evaluator, so cook implies json. fixed, view, message and
// retain add to wire and need no other surface.
func (s Surface) Resolve() Surface {
	s |= SurfaceWire
	if s&SurfaceBlock != 0 {
		s |= SurfaceCook
	}
	if s&SurfaceCook != 0 {
		s |= SurfaceJSON
	}
	return s
}

// Has reports whether the set names the surface. The zero Surface is read as
// the default, so a caller that never sets it gets today's output.
func (s Surface) Has(bit Surface) bool {
	if s == 0 {
		s = SurfaceAll
	}
	return s&bit != 0
}

// ResolveFor is Resolve for a unit whose tables are or are not VARIABLE. A
// variable table's row type (<Name>Row), its container and map machinery and
// the retention-carrying descriptor all live in the cook, block, json and
// retain output, so a unit with a variable table emits those four whatever the
// list names (§8.4). fixed, message and view stay optional in every unit.
func (s Surface) ResolveFor(variable bool) Surface {
	s = s.Resolve()
	if variable {
		s |= SurfaceCook | SurfaceBlock | SurfaceJSON | SurfaceRetain
	}
	return s
}
