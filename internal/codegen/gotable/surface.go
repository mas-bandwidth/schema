// The surfaces a Go generate can ask for (docs/SPEC-TABLES.md §8.4): the
// named slices of the emitted tree the `--surface` flag selects. Each
// surface names the files and functions it needs; dependencies are resolved
// by ParseSurfaces. The default (an empty list) is the full set, byte
// identical to a generate with no `--surface` at all.
package gotable

import (
	"fmt"
	"strings"
)

// Surf is one selectable surface.
type Surf string

const (
	SurfWire   Surf = "wire"
	SurfFixed  Surf = "fixed"
	SurfJSON   Surf = "json"
	SurfCook   Surf = "cook"
	SurfBlock  Surf = "block"
	SurfView   Surf = "view"
	SurfMsg    Surf = "message"
	SurfRetain Surf = "retain"
)

var allSurfaces = []Surf{SurfWire, SurfFixed, SurfJSON, SurfCook, SurfBlock, SurfView, SurfMsg, SurfRetain}

// surfDeps lists what each surface pulls in. A surface whose readers and
// writers walk a table on the wire needs the wire itself (§8.4).
var surfDeps = map[Surf][]Surf{
	SurfJSON:   {SurfWire},
	SurfMsg:    {SurfWire},
	SurfRetain: {SurfWire},
	SurfView:   {SurfWire},
	SurfCook:   {SurfWire},
}

func surfaceNames() []string {
	out := make([]string, len(allSurfaces))
	for i, s := range allSurfaces {
		out[i] = string(s)
	}
	return out
}

func isSurface(s Surf) bool {
	for _, a := range allSurfaces {
		if a == s {
			return true
		}
	}
	return false
}

// ParseSurfaces turns a requested surface list into the effective set, adding
// dependencies. An empty or nil list means ALL surfaces (the default). An
// unknown name is an error, listed by name.
func ParseSurfaces(list []string) (map[string]bool, error) {
	want := map[Surf]bool{}
	for _, raw := range list {
		s := Surf(strings.TrimSpace(raw))
		if !isSurface(s) {
			return nil, fmt.Errorf("unknown table surface %q — surfaces are %s", raw, strings.Join(surfaceNames(), ", "))
		}
		want[s] = true
	}
	for changed := true; changed; {
		changed = false
		for s := range want {
			for _, d := range surfDeps[s] {
				if !want[d] {
					want[d] = true
					changed = true
				}
			}
		}
	}
	out := make(map[string]bool, len(want))
	for s := range want {
		out[string(s)] = true
	}
	return out, nil
}

// AllSurfaces returns the full surface set, the default.
func AllSurfaces() map[string]bool {
	out := make(map[string]bool, len(allSurfaces))
	for _, s := range allSurfaces {
		out[string(s)] = true
	}
	return out
}
