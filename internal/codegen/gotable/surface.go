package gotable

import (
	"fmt"
	"sort"
	"strings"
)

// A surface is one named slice of the Go table output (docs/SPEC-TABLES.md
// §8.4). The eight surfaces are wire, fixed, json, cook, block, view, message
// and retain. "wire" is the base codec surface; the others are additive. A
// surface that reads or writes the wire form pulls wire in behind it (json,
// message, cook, block), so its absence never leaves a requested surface
// uncompilable.
type surfaceSet struct {
	wire    bool
	fixed   bool
	json    bool
	cook    bool
	block   bool
	view    bool
	message bool
	retain  bool
}

func allSurfaces() surfaceSet {
	return surfaceSet{wire: true, fixed: true, json: true, cook: true, block: true, view: true, message: true, retain: true}
}

func (s surfaceSet) empty() bool {
	return !s.wire && !s.fixed && !s.json && !s.cook && !s.block && !s.view && !s.message && !s.retain
}

func (s surfaceSet) withWire() surfaceSet {
	s.wire = true
	return s
}

// parseSurfaces reads a --surface list and resolves its dependencies. A list
// is a comma-separated set of the eight surface names; "all" is shorthand for
// every surface. An empty list means the default: all surfaces. An unknown
// name is an error. Resolved: json, message, cook and block each require the
// wire surface, so selecting them selects wire whether or not it is named.
func parseSurfaces(list string) (surfaceSet, error) {
	ss := surfaceSet{}
	if list == "" {
		return allSurfaces(), nil
	}
	for _, tok := range strings.Split(list, ",") {
		name := strings.TrimSpace(tok)
		if name == "" {
			continue
		}
		switch name {
		case "all":
			return allSurfaces(), nil
		case "wire":
			ss.wire = true
		case "fixed":
			ss.fixed = true
		case "json":
			ss.json = true
			ss = ss.withWire()
		case "cook":
			ss.cook = true
			ss = ss.withWire()
		case "block":
			ss.block = true
			ss = ss.withWire()
		case "view":
			ss.view = true
		case "message":
			ss.message = true
			ss = ss.withWire()
		case "retain":
			ss.retain = true
		default:
			return surfaceSet{}, fmt.Errorf("unknown --surface %q: surfaces are wire, fixed, json, cook, block, view, message, retain (or all)", name)
		}
	}
	if ss.empty() {
		return surfaceSet{}, fmt.Errorf("--surface names no surface")
	}
	return ss, nil
}

func (s surfaceSet) names() []string {
	var out []string
	if s.wire {
		out = append(out, "wire")
	}
	if s.fixed {
		out = append(out, "fixed")
	}
	if s.json {
		out = append(out, "json")
	}
	if s.cook {
		out = append(out, "cook")
	}
	if s.block {
		out = append(out, "block")
	}
	if s.view {
		out = append(out, "view")
	}
	if s.message {
		out = append(out, "message")
	}
	if s.retain {
		out = append(out, "retain")
	}
	sort.Strings(out)
	return out
}
