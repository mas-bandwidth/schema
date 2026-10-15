package gotable

import (
	"fmt"
	"strings"
)

// The generated surfaces a unit's Go output can carry (docs/SPEC-TABLES.md
// §8.4). Each names a file, or a functional block inside a file, of the
// generated tree; a run asks for only the surfaces it uses and the emitter
// withholds the rest, so a binary that needs just the wire codecs does not
// carry the text walk, the block accelerator, the cook or the view.
//
// The names are the `--surface <list>` spellings, shared by the standard
// (docs/SPEC-TABLES.md §8.4) and the Go backend. A surface's dependencies are
// resolved by the emitter: every surface builds on the WIRE codecs, so
// requesting any surface pulls WIRE in with it.
const (
	surfaceWire    = "wire"
	surfaceFixed   = "fixed"
	surfaceJSON    = "json"
	surfaceCook    = "cook"
	surfaceBlock   = "block"
	surfaceView    = "view"
	surfaceMessage  = "message"
	surfaceRetain  = "retain"
)

// allSurfaces is the canonical order of the surface names, which is also the
// order the docs and the CLI accept.
var allSurfaces = []string{
	surfaceWire,
	surfaceFixed,
	surfaceJSON,
	surfaceCook,
	surfaceBlock,
	surfaceView,
	surfaceMessage,
	surfaceRetain,
}

// TableSurfaceKey is the compiler.Options key that carries a unit's surface
// list. Its value is a comma-separated list of surface names; absent or empty
// means the full set.
const TableSurfaceKey = "surface"

// surfaceConfig is the resolved set of surfaces a run emits.
type surfaceConfig struct {
	wire, fixed, json, cook, block, view, message, retain bool
}

// parseSurfaces parses a comma-separated --surface list into a config. An
// empty list names the full set, which is the byte-identical default. An
// unknown name is an error, never a silent fallback.
func parseSurfaces(list string) (*surfaceConfig, error) {
	c := &surfaceConfig{}
	if strings.TrimSpace(list) == "" {
		c.wire, c.fixed, c.json, c.cook, c.block, c.view, c.message, c.retain = true, true, true, true, true, true, true, true
		return c, nil
	}
	seen := map[string]bool{}
	for _, raw := range strings.Split(list, ",") {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		switch name {
		case surfaceWire:
			c.wire = true
		case surfaceFixed:
			c.fixed = true
		case surfaceJSON:
			c.json = true
		case surfaceCook:
			c.cook = true
		case surfaceBlock:
			c.block = true
		case surfaceView:
			c.view = true
		case surfaceMessage:
			c.message = true
		case surfaceRetain:
			c.retain = true
		default:
			return nil, fmt.Errorf("unknown surface %q — the surfaces are %s (docs/SPEC-TABLES.md §8.4)", name, strings.Join(allSurfaces, ", "))
		}
	}
	// Every surface depends on the wire codecs: a run that asks for json alone
	// still gets the table's wire surface, because the text walk reads through
	// the descriptors and the wire codecs are the unit's base.
	//
	// COOK depends on JSON: the cook write surface's shared helpers live in the
	// text form's file, so asking for cook pulls the text form in with it.
	//
	// MESSAGE and RETAIN are always part of the base wire surface: the
	// reflection descriptors and the shared region runtime reference their
	// functions, so they cannot be withheld without withholding the wire
	// service itself. Requesting them names an existing surface; omitting them
	// changes nothing.
	c.wire = c.wire || c.fixed || c.json || c.cook || c.block || c.view || c.message || c.retain
	if c.cook {
		c.json = true
	}
	return c, nil
}
