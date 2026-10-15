package gotable

import (
	"fmt"
	"maps"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// A surface is one named piece of the Go table output
// (docs/SPEC-TABLES.md §8.4): the table-wire codecs, the fixed form, the JSON
// text form, the cooked form, the block form, the view, the message form and
// the retain machinery. `--surface` names which of them a generate emits;
// the default is all of them, byte-identical to today.
type SurfaceSet uint16

const (
	SurfaceWire SurfaceSet = 1 << iota
	SurfaceFixed
	SurfaceJSON
	SurfaceCook
	SurfaceBlock
	SurfaceView
	SurfaceMessage
	SurfaceRetain
	SurfaceAll = SurfaceWire | SurfaceFixed | SurfaceJSON | SurfaceCook | SurfaceBlock | SurfaceView | SurfaceMessage | SurfaceRetain
)

// surfaceNames is the canonical spelling of each surface, in the order the
// docs present them.
var surfaceNames = []struct {
	name string
	bit  SurfaceSet
}{
	{"wire", SurfaceWire},
	{"fixed", SurfaceFixed},
	{"json", SurfaceJSON},
	{"cook", SurfaceCook},
	{"block", SurfaceBlock},
	{"view", SurfaceView},
	{"message", SurfaceMessage},
	{"retain", SurfaceRetain},
}

// AllSurfaces is the default: every surface, which is what a generate that
// does not name `--surface` emits.
func AllSurfaces() SurfaceSet { return SurfaceAll }

// SurfaceSetFromOptions reads the Go target's --surface option: the "surface"
// key of the generate Options. An absent or empty value is the default (all).
func SurfaceSetFromOptions(opts map[string]string) (SurfaceSet, error) {
	s, ok := opts["surface"]
	if !ok || s == "" {
		return AllSurfaces(), nil
	}
	return SurfaceSetFromString(s)
}

// SurfaceSetFromString parses a comma-separated surface list of the spellings
// §8.4 names. An empty string is the default (all). An unknown name is an
// error naming it.
func SurfaceSetFromString(s string) (SurfaceSet, error) {
	if s == "" {
		return AllSurfaces(), nil
	}
	var set SurfaceSet
	for _, part := range strings.Split(s, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		bit := SurfaceSet(0)
		found := false
		for _, sn := range surfaceNames {
			if sn.name == name {
				bit = sn.bit
				found = true
				break
			}
		}
		if !found {
			return 0, fmt.Errorf("unknown Go surface %q (the surfaces are %s)", name, surfaceList())
		}
		set |= bit
	}
	return set.Resolve(), nil
}

func surfaceList() string {
	var names []string
	for _, sn := range surfaceNames {
		names = append(names, sn.name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// Resolve adds the dependency closure §8.4 names: json, cook, block, view,
// message and retain each need the wire codecs and descriptors, and cook also
// needs fixed.
func (s SurfaceSet) Resolve() SurfaceSet {
	out := s
	for _, dep := range []struct {
		need SurfaceSet
		give SurfaceSet
	}{
		{SurfaceJSON, SurfaceWire},
		{SurfaceCook, SurfaceWire},
		{SurfaceBlock, SurfaceWire},
		{SurfaceView, SurfaceWire},
		{SurfaceMessage, SurfaceWire},
		{SurfaceRetain, SurfaceWire},
		{SurfaceCook, SurfaceFixed},
	} {
		if s&dep.need != 0 {
			out |= dep.give
		}
	}
	if out&SurfaceWire != 0 {
		out |= SurfaceWire
	}
	return out
}

func (s SurfaceSet) has(bit SurfaceSet) bool { return s&bit != 0 }

func (s SurfaceSet) wire() bool      { return s.has(SurfaceWire) }
func (s SurfaceSet) fixed() bool     { return s.has(SurfaceFixed) }
func (s SurfaceSet) json() bool      { return s.has(SurfaceJSON) }
func (s SurfaceSet) cook() bool      { return s.has(SurfaceCook) }
func (s SurfaceSet) block() bool     { return s.has(SurfaceBlock) }
func (s SurfaceSet) view() bool      { return s.has(SurfaceView) }
func (s SurfaceSet) message() bool   { return s.has(SurfaceMessage) }
func (s SurfaceSet) retain() bool    { return s.has(SurfaceRetain) }

// GenerateLineageSelected is GenerateLineage with a surface selection: it
// emits only the files the selected surfaces need (docs/SPEC-TABLES.md §8.4).
// GenerateLineage is GenerateLineageSelected with the full set.
func GenerateLineageSelected(u *ir.Unit, lineage map[string][]FixedLineageEntry, surfaces SurfaceSet) (map[string][]byte, error) {
	if len(u.Tables) == 0 {
		return map[string][]byte{}, nil
	}
	blocks := ir.Blocks(u)
	out := map[string][]byte{}
	if surfaces.block() {
		bs, err := generateBlockFiles(u, blocks)
		if err != nil {
			return nil, err
		}
		maps.Copy(out, bs)
	}
	if surfaces.cook() {
		cooks, err := generateCookFiles(u, blocks)
		if err != nil {
			return nil, err
		}
		maps.Copy(out, cooks)
	}

	regional := len(variableTableNames(u)) > 0

	closure := ir.TableClosure(u)
	home := runtimeHome(u)
	anyKeyed := unitHasKeyedArray(u, closure)
	usedEnums := closureEnums(u, closure)
	armSlots := unionArmSlots(u, closure)
	idOrdinal := wireIdOrdinals(u)
	emitRuntime := func(g *tableGen) {
		g.needsMath = true
		g.needsUnsafe()
		g.pf("%s", tableRuntimeForUnit(regional)+tableRefusalSource+tableWireRuntime(u))
		if surfaces.message() {
			g.pf("%s", tableMessageRuntime(u))
		}
		if surfaces.fixed() {
			g.pf("%s", tableFixedRuntime)
		}
		if surfaces.cook() {
			g.pf("%s", tableCookWriteSource(u))
		}
		if regional && surfaces.retain() {
			g.emitRegionRuntime(blocks)
			g.emitRetainRuntime()
		}
		if anyKeyed {
			g.pf("%s", tableKeyedAccessor)
		}
		if len(armSlots) > 0 {
			g.pf("// One descriptor per union declaration, initialized after package data.\n")
			g.pf("var tableUnionArms = make([]TableUnionInfo, %d)\n\n", len(armSlots))
			if !regional {
				for _, file := range u.Files {
					for _, decl := range file.Decls {
						if un, ok := decl.(*ir.Union); ok {
							if _, used := armSlots[un.Name]; used {
								g.emitUnionRow(un)
							}
						}
					}
					for _, un := range file.TableUnions {
						if _, used := armSlots[un.Name]; used {
							g.emitUnionRow(un)
						}
					}
				}
			}
		}
	}
	runtimeWritten := false
	for _, f := range u.Files {
		g := &tableGen{unit: u, file: f, home: f.Base == home, anyKeyed: anyKeyed, unionArmSlot: armSlots, regional: regional, idOrdinal: idOrdinal, lineage: lineage}
		members := fileMembers(f, closure)
		if g.home {
			emitRuntime(g)
			runtimeWritten = true
		}
		for _, un := range f.TableUnions {
			g.emitTableUnion(un)
		}
		for _, st := range members {
			if st.IsTable {
				g.owner = st
				g.emitTableStruct(st)
			}
		}
		for _, e := range fileEnums(f, usedEnums) {
			g.emitEnumIdentity(e)
		}
		for _, st := range members {
			g.owner = st
			g.emitTableReset(st)
			g.emitTableMeasure(st)
			g.emitTableWrite(st)
			g.emitTableSave(st)
			g.emitTableRead(st)
			if surfaces.retain() {
				g.emitRetainMember(st)
			}
			if surfaces.cook() {
				g.emitCookWriteSurface(st)
			}
			if surfaces.message() {
				g.emitMessageWrite(st)
				g.emitMessageRead(st)
			}
			if regional {
				g.emitRegionMessage(st)
			}
			if regional {
				g.emitRegionMember(st)
				for _, f := range st.Fields {
					if f.IsList() {
						g.emitListSurface(st, f)
					} else if f.IsMap() {
						g.emitMapSurface(st, f)
					}
				}
			}
		}
		if surfaces.fixed() {
			g.emitFixedForm(members)
		}
		if len(members) > 0 {
			g.pf("// ---- reflection descriptors (tables only, docs/SPEC-TABLES.md §8) ----\n\n")
			for _, st := range members {
				g.owner = st
				g.emitTableDescriptor(st)
			}
			g.emitUnionArms()
		}
		src, err := g.assemble()
		if err != nil {
			return nil, err
		}
		out[f.Base+"Table.go"] = src
	}
	if !runtimeWritten {
		g := &tableGen{unit: u, home: true, anyKeyed: anyKeyed, unionArmSlot: armSlots, regional: regional, idOrdinal: idOrdinal, lineage: lineage}
		emitRuntime(g)
		src, err := g.assemble()
		if err != nil {
			return nil, err
		}
		out[home+"Table.go"] = src
	}

	if surfaces.json() {
		texts, err := generateJsonFiles(u, closure, home)
		if err != nil {
			return nil, err
		}
		maps.Copy(out, texts)
	}
	if surfaces.view() {
		view, err := generateViewFile(u, closure, armSlots)
		if err != nil {
			return nil, err
		}
		maps.Copy(out, view)
	}
	return out, nil
}
