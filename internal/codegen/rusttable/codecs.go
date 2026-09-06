// TABLE-wire storage, codec and descriptor emission for Rust
// (docs/SPEC-TABLES.md), mirroring internal/codegen/cpptable — the reference.
// Readers restore declared defaults then overlay, skip unknown ids, skip kind
// mismatches, clamp out-of-range values, and count every event.
package rusttable

import (
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// tableModule assembles <base>_table.rs: the storage structs a file's table
// declarations own, the per-enum wire identity, the resets, the codecs, the
// reflection descriptors and the text form's entry points.
func (g *gen) tableModule() []byte {
	members := g.members()
	if len(members) == 0 {
		return nil
	}

	for _, st := range members {
		if st.IsTable {
			g.emitStorage(st)
		}
	}
	for _, e := range g.tableEnums(members) {
		g.emitEnumIdentity(e)
	}
	for _, st := range members {
		g.owner = st
		g.emitReset(st)
	}
	for _, st := range members {
		g.owner = st
		g.emitMeasure(st)
		g.emitSave(st)
		g.emitLoad(st)
	}
	g.owner = nil
	g.emitRelocatabilityAsserts(members)
	g.emitUnionInfos(members)
	g.emitDescriptors(members)
	g.emitJson(members)

	var b strings.Builder
	b.WriteString(header(g.file.Base, g.unit.Package,
		fmt.Sprintf("the TABLE wire (docs/SPEC-TABLES.md); protocol id 0x%016x names packets only, and a table versions by field id", g.unit.ProtocolId)))
	b.WriteString(tableModuleBanner)
	// the crate root glob re-exports every module, the shared runtime included,
	// so one import reaches the whole unit
	b.WriteString("use crate::*;\n\n")
	b.WriteString(g.body.String())
	return []byte(b.String())
}

const tableModuleBanner = `//
// Measure/Save/Load are name-first free functions: <name>_measure gives the
// exact wire size, <name>_save writes exactly that many bytes into the
// caller's slice, <name>_load overlays a value in place and reports every
// tolerance event. Nothing here allocates: the caller owns the value, the
// slice and the report.

#![allow(clippy::needless_range_loop)]
#![allow(clippy::too_many_arguments)]
#![allow(clippy::manual_range_contains)]
#![allow(clippy::collapsible_else_if)]
// a GUARDED field's test is two facts, not one: the branch guard is the wire's
// (§4) and the inner test is the elision's, and collapsing them would put a
// reader one step further from the declaration
#![allow(clippy::collapsible_if)]
#![allow(clippy::unnecessary_cast)]
#![allow(clippy::float_cmp)]
#![allow(clippy::excessive_precision)]

`

// members is the file's table declarations, ordered so a same-file table
// precedes its by-value users, plus every closure type declared in the file.
func (g *gen) members() []*ir.Struct {
	var members []*ir.Struct
	members = append(members, orderTables(g.file.Tables)...)
	for _, d := range g.file.Decls {
		if st, ok := d.(*ir.Struct); ok && g.closure[st.Name] {
			members = append(members, st)
		}
	}
	return members
}

// orderTables returns a file's tables with every same-file table preceding
// its by-value users. Stable: declaration order survives wherever no
// dependency forces otherwise.
func orderTables(tables []*ir.Struct) []*ir.Struct {
	n := len(tables)
	byName := map[string]int{}
	for i, st := range tables {
		byName[st.Name] = i
	}
	adj := make([][]int, n)
	indeg := make([]int, n)
	for i, st := range tables {
		for _, f := range st.Fields {
			if f.Type.Kind != ir.TNamed {
				continue
			}
			if ref, ok := f.Type.Ref.(*ir.Struct); ok && ref.IsTable {
				if j, ok := byName[ref.Name]; ok && j != i {
					adj[j] = append(adj[j], i)
					indeg[i]++
				}
			}
		}
	}
	order := make([]*ir.Struct, 0, n)
	done := make([]bool, n)
	for len(order) < n {
		pick := -1
		for i := range n {
			if !done[i] && indeg[i] == 0 {
				pick = i
				break
			}
		}
		if pick == -1 {
			for i := range n {
				if !done[i] {
					pick = i
					break
				}
			}
		}
		done[pick] = true
		order = append(order, tables[pick])
		for _, t := range adj[pick] {
			indeg[t]--
		}
	}
	return order
}

// tableEnums is every enum a file's closure members reach, sorted, so the
// per-enum wire identity is emitted once per file that needs it.
func (g *gen) tableEnums(members []*ir.Struct) []*ir.Enum {
	seen := map[string]*ir.Enum{}
	for _, st := range members {
		for _, f := range st.Fields {
			if e := enumOf(f); e != nil {
				seen[e.Name] = e
			}
			if f.KeyEnumRef != nil {
				seen[f.KeyEnum] = f.KeyEnumRef
			}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]*ir.Enum, 0, len(names))
	for _, name := range names {
		// the identity pair belongs to the file that DECLARES the enum, so a
		// unit with several files defines it once
		if home, ok := g.unit.DeclFile[name]; ok && home != g.file.Base {
			continue
		}
		out = append(out, seen[name])
	}
	return out
}

// ---- storage ----

func (g *gen) emitStorage(st *ir.Struct) {
	g.pf("%s", ir.DocComment(st.Doc, "", "///"))
	g.pf("// table %s — TABLE-wire storage: relocatable, bounded, declared defaults\n", st.Name)
	g.pf("// in the Default impl (docs/SPEC-TABLES.md)\n")
	g.pf("#[repr(C)]\n#[derive(Clone, Copy, PartialEq, Debug)]\n")
	if len(st.Fields) == 0 {
		g.pf("pub struct %s {}\n\n", st.Name)
	} else {
		g.pf("pub struct %s {\n", st.Name)
		prevGuard := ""
		for _, f := range st.Fields {
			if f.Guard != prevGuard {
				if f.Guard != "" {
					g.pf("\n    // %s — guarded fields stay off the wire when the guard says so;\n", f.Guard)
					g.pf("    // a read's restored defaults stand in for the untaken side\n")
				} else {
					g.pf("\n")
				}
				prevGuard = f.Guard
			}
			g.pf("%s", ir.DocComment(f.Doc, "    ", "///"))
			g.emitStorageField(f)
		}
		g.pf("}\n\n")
	}

	// Default IS the declared defaults for a table's storage, which is what
	// the C++ reference's member initializers give: the zero form is built
	// first and the reset lays the defaults over it, so one function is the
	// only place a default is spelled.
	g.pf("impl Default for %s {\n", st.Name)
	g.pf("    fn default() -> Self {\n")
	if len(st.Fields) == 0 {
		g.pf("        %s {}\n", st.Name)
	} else {
		g.pf("        let mut value = %s {\n", st.Name)
		for _, f := range st.Fields {
			g.emitZeroInit(f, "            ")
		}
		g.pf("        };\n")
		g.pf("        %s(&mut value);\n", fn(st.Name, "reset"))
		g.pf("        value\n")
	}
	g.pf("    }\n}\n\n")
}

func (g *gen) emitStorageField(f *ir.Field) {
	typ := rustFieldType(f.Type)
	switch {
	case f.Type.Kind == ir.TString:
		g.pf("    pub %s: [u8; %d], // string(%s): max length, used length beside it\n",
			f.Name, f.Type.Size, ir.RenderExpr(f.Type.SizeExpr))
		g.pf("    pub %s_length: i32,\n", f.Name)
	case f.Type.Kind == ir.TBytes:
		g.pf("    pub %s: [u8; %d], // bytes(%s): fixed buffer, used length beside it\n",
			f.Name, f.Type.Size, ir.RenderExpr(f.Type.SizeExpr))
		g.pf("    pub %s_length: i32,\n", f.Name)
	case f.KeyEnum != "":
		g.pf("    pub %s: %s, // [%s]: one slot per named variant, keyed by the value\n",
			f.Name, keyedType(f), f.KeyEnum)
	case f.Array == ir.ArrayFixed:
		g.pf("    pub %s: [%s; %d],\n", f.Name, typ, f.ArrayBound)
	case f.Array == ir.ArrayCounted:
		g.pf("    pub %s: [%s; %d], // used count beside it; count in [0, %s]\n",
			f.Name, typ, f.ArrayBound, ir.RenderExpr(f.ArrayExpr))
		g.pf("    pub %s_count: i32,\n", f.Name)
	default:
		g.pf("    pub %s: %s,\n", f.Name, typ)
		if f.Type.Optional {
			g.pf("    pub %s_present: bool, // ?%s: absent until set\n", f.Name, tableFieldTypeName(f))
		}
	}
}

// emitZeroInit writes one field's ZERO-form struct-literal entry. The
// declared defaults are the reset's business; this is the storage the reset
// lays them over.
func (g *gen) emitZeroInit(f *ir.Field, ind string) {
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		g.pf("%s%s: [0; %d],\n", ind, f.Name, f.Type.Size)
		g.pf("%s%s_length: 0,\n", ind, f.Name)
	case f.KeyEnum != "":
		g.pf("%s%s: %s::default(),\n", ind, f.Name, keyedTypeExpr(f))
	case f.Array != ir.ArrayNone:
		g.pf("%s%s: [%s; %d],\n", ind, f.Name, zeroScalar(f), f.ArrayBound)
		if f.Array == ir.ArrayCounted {
			g.pf("%s%s_count: 0,\n", ind, f.Name)
		}
	default:
		g.pf("%s%s: %s,\n", ind, f.Name, zeroScalar(f))
		if f.Type.Optional {
			g.pf("%s%s_present: false,\n", ind, f.Name)
		}
	}
}

func zeroScalar(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		return "false"
	case ir.TFloat32, ir.TFloat64:
		return "0.0"
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			return f.Type.Name + "::NONE"
		case *ir.Flags:
			return "0"
		case *ir.Struct:
			return f.Type.Name + "::default()"
		case *ir.Union:
			return f.Type.Name + "::None"
		}
	}
	return "0"
}

// ---- the per-enum wire identity (docs/SPEC-TABLES.md §5) ----

func (g *gen) emitEnumIdentity(e *ir.Enum) {
	g.pf("// %s on the TABLE wire: a value rides as the u16 hash of its VARIANT\n", e.Name)
	g.pf("// NAME, so a variant may be added anywhere, removed, or reordered and old\n")
	g.pf("// data still reads (docs/SPEC-TABLES.md §5). None is the one reserved id, 0.\n")
	g.pf("impl TableEnum for %s {\n", e.Name)
	g.pf("    fn table_id(self) -> Option<u16> {\n")
	g.pf("        match self.0 {\n")
	g.pf("            0 => Some(0),\n")
	for i, v := range e.Variants {
		g.pf("            %d => Some(0x%04x),\n", i+1, ir.VariantId(v))
	}
	g.pf("            _ => None, // no variant names this value: no wire identity\n")
	g.pf("        }\n    }\n\n")
	g.pf("    fn table_value(id: u16) -> Option<%s> {\n", e.Name)
	g.pf("        match id {\n")
	g.pf("            0 => Some(%s::NONE),\n", e.Name)
	for i, v := range e.Variants {
		g.pf("            0x%04x => Some(%s(%d)),\n", ir.VariantId(v), e.Name, i+1)
	}
	g.pf("            _ => None, // an id this build cannot name\n")
	g.pf("        }\n    }\n}\n\n")
}

// ---- reset ----

func (g *gen) emitReset(st *ir.Struct) {
	g.pf("// %s restores %s's declared defaults in place, reusing every buffer the\n", fn(st.Name, "reset"), st.Name)
	g.pf("// value already owns. The reader calls it before overlaying.\n")
	if len(st.Fields) == 0 {
		g.pf("pub fn %s(_value: &mut %s) {}\n\n", fn(st.Name, "reset"), st.Name)
		return
	}
	g.pf("pub fn %s(value: &mut %s) {\n", fn(st.Name, "reset"), st.Name)
	for _, f := range st.Fields {
		g.emitResetField(f)
	}
	g.pf("}\n\n")
}

func (g *gen) emitResetField(f *ir.Field) {
	name := f.Name
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		g.pf("    value.%s.fill(0);\n", name)
		g.pf("    value.%s_length = 0;\n", name)
	case f.KeyEnum != "":
		if isStruct(f) {
			g.pf("    for slot in %s.iter_mut() {\n", g.keyedSlots(f))
			g.pf("        %s(slot);\n", fn(f.Type.Name, "reset"))
			g.pf("    }\n")
		} else {
			g.pf("    %s.fill(%s);\n", g.keyedSlots(f), zeroScalar(f))
		}
	case f.Array != ir.ArrayNone:
		switch {
		case isStruct(f):
			g.pf("    for element in value.%s.iter_mut() {\n", name)
			g.pf("        %s(element);\n", fn(f.Type.Name, "reset"))
			g.pf("    }\n")
		case f.HasDefault:
			g.pf("    value.%s.fill(%s);\n", name, g.defaultValue(f))
		default:
			g.pf("    value.%s.fill(%s);\n", name, zeroScalar(f))
		}
		if f.Array == ir.ArrayCounted {
			g.pf("    value.%s_count = 0;\n", name)
		}
	case isStruct(f):
		g.pf("    %s(&mut value.%s);\n", fn(f.Type.Name, "reset"), name)
		if f.Type.Optional {
			g.pf("    value.%s_present = false;\n", name)
		}
	case isUnion(f):
		g.pf("    value.%s = %s::None;\n", name, f.Type.Name)
	default:
		g.pf("    value.%s = %s;\n", name, g.defaultValue(f))
		if f.Type.Optional {
			g.pf("    value.%s_present = false;\n", name)
		}
	}
}

// defaultValue is a field's declared default as a Rust expression — the value
// the write side elides against and the reset restores.
func (g *gen) defaultValue(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		if f.HasDefault && f.DefBool {
			return "true"
		}
		return "false"
	case ir.TFloat32:
		if f.HasDefault {
			return formatFloat(f.DefFloat, true)
		}
		return "0.0"
	case ir.TFloat64:
		if f.HasDefault {
			return formatFloat(f.DefFloat, false)
		}
		return "0.0"
	case ir.TInt, ir.TBits:
		if f.HasDefault && f.DefInt != nil {
			return intLit(f.DefInt, rustFieldType(f.Type))
		}
		return "0"
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			if f.HasDefault && f.DefVariant != "" {
				return f.Type.Name + "::" + ir.RustConstName(f.DefVariant)
			}
			return f.Type.Name + "::NONE"
		case *ir.Flags:
			return "0"
		}
	}
	return "0"
}

// ---- guards ----

// guardExprs composes each guarded field's branch condition from the wire
// tree, so the writer and measurer keep untaken-branch fields off the wire.
func guardExprs(st *ir.Struct) map[string]string { return guardWalk(st, "value.") }

// guardStrings is the value-free twin for the reflection descriptors.
func guardStrings(st *ir.Struct) map[string]string { return guardWalk(st, "") }

func guardWalk(st *ir.Struct, prefix string) map[string]string {
	guards := map[string]string{}
	var walk func(items []ir.Item, cond string)
	walk = func(items []ir.Item, cond string) {
		for _, item := range items {
			switch item := item.(type) {
			case *ir.FieldItem:
				if cond != "" {
					guards[item.F.Name] = cond
				}
			case *ir.Branch:
				pos, neg := prefix+item.Cond, "!"+prefix+item.Cond
				if item.Neg {
					pos, neg = neg, pos
				}
				and := func(a, b string) string {
					if a == "" {
						return b
					}
					return a + " && " + b
				}
				walk(item.Then, and(cond, pos))
				walk(item.Else, and(cond, neg))
			}
		}
	}
	walk(st.Items, "")
	return guards
}

// ---- measure ----

func (g *gen) emitMeasure(st *ir.Struct) {
	g.pf("// %s is the EXACT encoded size of a value, with no writing — the\n", fn(st.Name, "measure"))
	g.pf("// parallel-generation lever. Every nested table on the wire is\n")
	g.pf("// length-prefixed, so a caller can measure subtables in parallel,\n")
	g.pf("// prefix-sum offsets and scatter-write disjoint ranges from N workers.\n")
	g.pf("// A value violating its storage invariants measures as -1, exactly as the\n")
	g.pf("// write side refuses it.\n")
	if len(st.Fields) == 0 {
		g.pf("pub fn %s(_value: &%s) -> i64 {\n    2 // terminator: an empty type's presence is its payload\n}\n\n",
			fn(st.Name, "measure"), st.Name)
		return
	}
	g.pf("pub fn %s(value: &%s) -> i64 {\n", fn(st.Name, "measure"), st.Name)
	g.pf("    let mut bytes: i64 = 2; // terminator\n")
	guards := guardExprs(st)
	for _, f := range st.Fields {
		if cond, guarded := guards[f.Name]; guarded {
			g.pf("    if %s {\n", cond)
			g.indent = "    "
			g.emitMeasureField(f)
			g.indent = ""
			g.pf("    }\n")
			continue
		}
		g.emitMeasureField(f)
	}
	g.pf("    bytes\n}\n\n")
}

func (g *gen) emitMeasureField(f *ir.Field) {
	kind := ir.TableScalarKind(f)
	width := tableKindWidth(kind)
	switch {
	case f.Type.Optional:
		// an optional's PRESENCE is the payload: it rides even when the value
		// is entirely default, so absent and present-at-default stay distinct
		g.pf("    if value.%s_present { // ?%s: presence decides, not content\n", f.Name, tableFieldTypeName(f))
		switch {
		case kind == tkTable:
			g.pf("        let body = %s(&value.%s);\n", fn(f.Type.Name, "measure"), f.Name)
			g.pf("        if body < 0 {\n            return -1;\n        }\n")
			g.pf("        bytes += 3 + 4 + body; // %s\n", f.Name)
		case isEnum(f):
			g.pf("        if value.%s.table_id().is_none() {\n            return -1; // no variant names this value\n        }\n", f.Name)
			g.pf("        bytes += 3 + 2; // %s: the variant's name hash\n", f.Name)
		default:
			g.pf("        bytes += 3 + %d; // %s\n", width, f.Name)
		}
		g.pf("    }\n")
	case f.Type.Kind == ir.TString:
		g.pf("    if value.%s_length < 0 || value.%s_length > %d {\n        return -1; // storage invariant\n    }\n",
			f.Name, f.Name, f.Type.Size)
		g.pf("    if value.%s_length > 0 {\n        bytes += 3 + 4 + value.%s_length as i64; // %s\n    }\n",
			f.Name, f.Name, f.Name)
	case f.Type.Kind == ir.TBytes:
		g.pf("    if value.%s_length < 0 || value.%s_length > %d {\n        return -1; // storage invariant\n    }\n",
			f.Name, f.Name, f.Type.Size)
		g.pf("    if value.%s_length > 0 {\n        bytes += 3 + 4 + 5 + value.%s_length as i64; // %s\n    }\n",
			f.Name, f.Name, f.Name)
	case f.KeyEnum != "":
		g.emitMeasureKeyed(f, kind, width)
	case f.Array == ir.ArrayCounted:
		g.pf("    if value.%s_count < 0 || value.%s_count > %d {\n        return -1; // storage invariant\n    }\n",
			f.Name, f.Name, f.ArrayBound)
		g.pf("    if value.%s_count > 0 {\n", f.Name)
		g.indent = "    "
		g.emitMeasureArrayBody(f, kind, width, fmt.Sprintf("value.%s_count as usize", f.Name), false)
		g.indent = ""
		g.pf("    }\n")
	case f.Array == ir.ArrayFixed:
		if kind == tkTable {
			// a fixed array of TABLES always rides: an all-default test would
			// have to measure every element to answer, so the form is the
			// same shape a counted one takes
			g.pf("    {\n")
			g.indent = "    "
			g.emitMeasureArrayBody(f, kind, width, fmt.Sprintf("%d", f.ArrayBound), true)
			g.indent = ""
			g.pf("    }\n")
			break
		}
		g.pf("    {\n")
		g.pf("        let mut all_default = true;\n")
		g.pf("        for i in 0..%d {\n            if value.%s[i] != %s {\n                all_default = false;\n                break;\n            }\n        }\n",
			f.ArrayBound, f.Name, g.arrayElementDefault(f))
		g.pf("        if !all_default {\n")
		g.indent = "        "
		if isEnum(f) {
			g.pf("    for i in 0..%d {\n        // %s: every element must be nameable\n        if value.%s[i].table_id().is_none() {\n            return -1;\n        }\n    }\n",
				f.ArrayBound, f.Name, f.Name)
		}
		g.pf("    bytes += 3 + 4 + 5 + %d; // %s\n", f.ArrayBound*int64(width), f.Name)
		g.indent = ""
		g.pf("        }\n    }\n")
	case kind == tkTable:
		g.pf("    {\n")
		g.pf("        let body = %s(&value.%s);\n", fn(f.Type.Name, "measure"), f.Name)
		g.pf("        if body < 0 {\n            return -1;\n        }\n")
		g.pf("        if body > 2 {\n            bytes += 3 + 4 + body; // %s: all-default nested elides\n        }\n", f.Name)
		g.pf("    }\n")
	case kind == tkUnion:
		g.emitMeasureUnion(f)
	case isEnum(f):
		g.pf("    if value.%s != %s {\n", f.Name, g.defaultValue(f))
		g.pf("        if value.%s.table_id().is_none() {\n            return -1; // no variant names this value\n        }\n", f.Name)
		g.pf("        bytes += 3 + 2; // %s: the variant's name hash\n    }\n", f.Name)
	default:
		g.pf("    if %s {\n        bytes += 3 + %d; // %s\n    }\n", g.nonDefaultTest(f), width, f.Name)
	}
}

// nonDefaultTest is the condition a scalar field rides under: it is not at its
// declared default. A BOOL reads as itself rather than as a comparison against
// a literal, which is the same test written the way the language writes it.
func (g *gen) nonDefaultTest(f *ir.Field) string {
	if f.Type.Kind == ir.TBool {
		if f.HasDefault && f.DefBool {
			return "!value." + f.Name
		}
		return "value." + f.Name
	}
	return fmt.Sprintf("value.%s != %s", f.Name, g.defaultValue(f))
}

// arrayElementDefault is the value a fixed array's slots are compared against
// for the all-default elision.
func (g *gen) arrayElementDefault(f *ir.Field) string {
	if f.HasDefault {
		return g.defaultValue(f)
	}
	return zeroScalar(f)
}

func (g *gen) emitMeasureArrayBody(f *ir.Field, kind, width int, count string, fixed bool) {
	note := ""
	if fixed {
		note = fmt.Sprintf(" (fixed [%d])", f.ArrayBound)
	}
	if kind == tkTable {
		g.pf("    bytes += 3 + 4 + 5; // %s%s\n", f.Name, note)
		g.pf("    for i in 0..%s {\n", count)
		g.pf("        let element = %s(&value.%s[i]);\n", fn(f.Type.Name, "measure"), f.Name)
		g.pf("        if element < 0 {\n            return -1;\n        }\n")
		g.pf("        bytes += 4 + element;\n    }\n")
		return
	}
	if isEnum(f) {
		g.pf("    for i in 0..%s {\n        // %s: every element must be nameable\n        if value.%s[i].table_id().is_none() {\n            return -1;\n        }\n    }\n",
			count, f.Name, f.Name)
	}
	g.pf("    bytes += 3 + 4 + 5 + (%s as i64) * %d; // %s%s\n", count, width, f.Name, note)
}

func (g *gen) emitMeasureUnion(f *ir.Field) {
	un := unionOf(f)
	g.pf("    match &value.%s {\n", f.Name)
	g.pf("        %s::None => {} // None elides — TLV absence is the None\n", f.Type.Name)
	for _, v := range un.Variants {
		g.pf("        %s::%s(arm) => {\n", f.Type.Name, ir.GoExportName(v.Name))
		g.pf("            let body = %s(arm);\n", fn(v.Type, "measure"))
		g.pf("            if body < 0 {\n                return -1;\n            }\n")
		g.pf("            // the u16 ARM ID, then the arm length-prefixed\n")
		g.pf("            bytes += 3 + 2 + 4 + body;\n")
		g.pf("        }\n")
	}
	g.pf("    }\n")
}

func (g *gen) emitMeasureKeyed(f *ir.Field, kind, width int) {
	g.pf("    {\n")
	g.pf("        let mut pairs: i64 = 0;\n")
	g.pf("        let mut keyed_bytes: i64 = 0;\n")
	g.pf("        // [%s]: every stored slot is a named variant's; i is the STORAGE\n", f.KeyEnum)
	g.pf("        // index and the key it holds is i + 1\n")
	g.pf("        for i in 0..%s {\n", arrayLen(f))
	switch {
	case kind == tkTable:
		g.pf("            let element = %s(&%s[i]);\n", fn(f.Type.Name, "measure"), g.keyedSlots(f))
		g.pf("            if element < 0 {\n                return -1;\n            }\n")
		g.pf("            if element <= 2 {\n                continue; // an all-default slot elides\n            }\n")
	case isEnum(f):
		g.pf("            if %s[i] == %s {\n                continue; // a default slot elides\n            }\n", g.keyedSlots(f), zeroScalar(f))
		g.pf("            if %s[i].table_id().is_none() {\n                return -1; // no variant names this value\n            }\n", g.keyedSlots(f))
	default:
		g.pf("            if %s[i] == %s {\n                continue; // a default slot elides\n            }\n", g.keyedSlots(f), zeroScalar(f))
	}
	g.pf("            if %s.table_id().is_none() {\n                return -1;\n            }\n", keyOfSlot(f, "i"))
	g.pf("            pairs += 1;\n")
	if kind == tkTable {
		g.pf("            keyed_bytes += 2 + 4 + element; // key, length, body\n")
	} else {
		g.pf("            keyed_bytes += 2 + 4 + %d; // key, length, element\n", width)
	}
	g.pf("        }\n")
	g.pf("        if pairs > 0 {\n            bytes += 3 + 4 + 5 + keyed_bytes; // %s\n        }\n", f.Name)
	g.pf("    }\n")
}

// ---- save ----

func (g *gen) emitSave(st *ir.Struct) {
	// FORCE-INLINED, and the C++ reference carries the same rule (#343): out of
	// line a writer's cursor round-trips through memory at every put, and only
	// a body with no call boundary in it lets it stay in registers and lets
	// adjacent constant framing bytes merge into one store.
	//
	// THE RECURSION GUARD IS THE CLASS LINE. A fixed table nests BY VALUE, so
	// its save/load call graph is a DAG — a by-value cycle would be an
	// infinite size_of, which the checker refuses — and forcing it flat always
	// terminates. The VARIABLE class reaches a pointee through a recursive
	// walk, and this backend emits no wire surface for it at all (§11), so
	// there is no recursive body here to force.
	g.pf("#[inline(always)]\n")
	g.pf("pub fn %s(w: &mut TableWriter, value: &%s) -> bool {\n", fn(st.Name, "save_body"), st.Name)
	guards := guardExprs(st)
	for _, f := range st.Fields {
		if cond, guarded := guards[f.Name]; guarded {
			g.pf("    if %s {\n", cond)
			g.indent = "    "
			g.emitSaveField(f)
			g.indent = ""
			g.pf("    }\n")
			continue
		}
		g.emitSaveField(f)
	}
	g.pf("    w.put16(0); // terminator\n")
	g.pf("    !w.overflow\n}\n\n")

	g.pf("// %s writes exactly %s(value) bytes into the caller's slice.\n", fn(st.Name, "save"), fn(st.Name, "measure"))
	g.pf("pub fn %s(value: &%s, buffer: &mut [u8]) -> i64 {\n", fn(st.Name, "save"), st.Name)
	g.pf("    let mut w = TableWriter::new(buffer);\n")
	g.pf("    if !%s(&mut w, value) {\n        return -1;\n    }\n", fn(st.Name, "save_body"))
	g.pf("    w.offset as i64 // == %s(value)\n}\n\n", fn(st.Name, "measure"))
}

func (g *gen) emitSaveField(f *ir.Field) {
	kind := ir.TableScalarKind(f)
	fieldKind := ir.TableFieldKind(f)
	id := ir.TableFieldId(f)
	switch {
	case f.Type.Optional:
		g.pf("    if value.%s_present { // ?%s\n", f.Name, tableFieldTypeName(f))
		switch {
		case kind == tkTable:
			g.pf("        let body = %s(&value.%s);\n", fn(f.Type.Name, "measure"), f.Name)
			g.pf("        if body < 0 {\n            return false; // storage invariant, refused as measure refuses it\n        }\n")
			g.pf("        w.put16(0x%04x);\n        w.put8(%d); // %s\n", id, fieldKind, f.Name)
			g.pf("        w.put32(body as u32);\n")
			g.pf("        if !%s(w, &value.%s) {\n            return false;\n        }\n", fn(f.Type.Name, "save_body"), f.Name)
		case isEnum(f):
			g.pf("        let variant_id = match value.%s.table_id() {\n            Some(id) => id,\n            None => return false,\n        };\n", f.Name)
			g.pf("        w.put16(0x%04x);\n        w.put8(%d); // %s\n", id, fieldKind, f.Name)
			g.pf("        w.put16(variant_id);\n")
		default:
			g.pf("        w.put16(0x%04x);\n        w.put8(%d); // %s\n", id, fieldKind, f.Name)
			g.pf("        w.%s(%s);\n", putFn(tableKindWidth(kind)), g.scalarToWire(f, "value."+f.Name))
		}
		g.pf("    }\n")
	case f.Type.Kind == ir.TString:
		g.pf("    if value.%s_length < 0 || value.%s_length > %d {\n        return false; // storage invariant\n    }\n",
			f.Name, f.Name, f.Type.Size)
		g.pf("    if value.%s_length > 0 {\n", f.Name)
		g.pf("        w.put16(0x%04x);\n        w.put8(%d); // %s\n", id, fieldKind, f.Name)
		g.pf("        w.put32(value.%s_length as u32);\n", f.Name)
		g.pf("        w.raw(&value.%s[..value.%s_length as usize]);\n    }\n", f.Name, f.Name)
	case f.Type.Kind == ir.TBytes:
		g.pf("    if value.%s_length < 0 || value.%s_length > %d {\n        return false; // storage invariant\n    }\n",
			f.Name, f.Name, f.Type.Size)
		g.pf("    if value.%s_length > 0 {\n", f.Name)
		g.pf("        w.put16(0x%04x);\n        w.put8(%d); // %s\n", id, fieldKind, f.Name)
		g.pf("        w.put32((5 + value.%s_length) as u32);\n", f.Name)
		g.pf("        w.put8(%d);\n        w.put32(value.%s_length as u32);\n", ir.TableElemKind(f), f.Name)
		g.pf("        w.raw(&value.%s[..value.%s_length as usize]);\n    }\n", f.Name, f.Name)
	case f.KeyEnum != "":
		g.emitSaveKeyed(f, kind, id)
	case f.Array == ir.ArrayCounted:
		g.pf("    if value.%s_count < 0 || value.%s_count > %d {\n        return false; // storage invariant\n    }\n",
			f.Name, f.Name, f.ArrayBound)
		g.pf("    if value.%s_count > 0 {\n", f.Name)
		g.indent = "    "
		g.emitSaveArrayBody(f, kind, id, fmt.Sprintf("value.%s_count as usize", f.Name), fmt.Sprintf("value.%s_count as u32", f.Name), "")
		g.indent = ""
		g.pf("    }\n")
	case f.Array == ir.ArrayFixed:
		note := fmt.Sprintf(" (fixed [%d])", f.ArrayBound)
		if kind == tkTable {
			g.pf("    {\n")
			g.indent = "    "
			g.emitSaveArrayBody(f, kind, id, fmt.Sprintf("%d", f.ArrayBound), fmt.Sprintf("%d", f.ArrayBound), note)
			g.indent = ""
			g.pf("    }\n")
			break
		}
		g.pf("    {\n")
		g.pf("        let mut all_default = true;\n")
		g.pf("        for i in 0..%d {\n            if value.%s[i] != %s {\n                all_default = false;\n                break;\n            }\n        }\n",
			f.ArrayBound, f.Name, g.arrayElementDefault(f))
		g.pf("        if !all_default {\n")
		g.indent = "        "
		g.emitSaveArrayBody(f, kind, id, fmt.Sprintf("%d", f.ArrayBound), fmt.Sprintf("%d", f.ArrayBound), note)
		g.indent = ""
		g.pf("        }\n    }\n")
	case kind == tkTable:
		g.pf("    {\n")
		g.pf("        let body = %s(&value.%s);\n", fn(f.Type.Name, "measure"), f.Name)
		g.pf("        if body < 0 {\n            return false; // storage invariant, refused as measure refuses it\n        }\n")
		g.pf("        if body > 2 { // all-default nested elides\n")
		g.pf("            w.put16(0x%04x);\n            w.put8(%d); // %s\n", id, fieldKind, f.Name)
		g.pf("            w.put32(body as u32);\n")
		g.pf("            if !%s(w, &value.%s) {\n                return false;\n            }\n", fn(f.Type.Name, "save_body"), f.Name)
		g.pf("        }\n    }\n")
	case kind == tkUnion:
		g.emitSaveUnion(f, id, fieldKind)
	case isEnum(f):
		g.pf("    if value.%s != %s {\n", f.Name, g.defaultValue(f))
		g.pf("        let variant_id = match value.%s.table_id() {\n            Some(id) => id,\n            None => return false,\n        };\n", f.Name)
		g.pf("        w.put16(0x%04x);\n        w.put8(%d); // %s\n", id, fieldKind, f.Name)
		g.pf("        w.put16(variant_id);\n    }\n")
	default:
		g.pf("    if %s {\n", g.nonDefaultTest(f))
		g.pf("        w.put16(0x%04x);\n        w.put8(%d); // %s\n", id, fieldKind, f.Name)
		g.pf("        w.%s(%s);\n    }\n", putFn(tableKindWidth(kind)), g.scalarToWire(f, "value."+f.Name))
	}
}

func (g *gen) emitSaveArrayBody(f *ir.Field, kind int, id uint16, count, countExpr, note string) {
	g.pf("    w.put16(0x%04x);\n    w.put8(%d); // %s%s\n", id, ir.TableKindArray, f.Name, note)
	g.pf("    let len_at = w.offset;\n    w.put32(0);\n")
	g.pf("    w.put8(%d);\n    w.put32(%s);\n", ir.TableElemKind(f), countExpr)
	g.pf("    for i in 0..%s {\n", count)
	switch {
	case kind == tkTable:
		g.pf("        let element_len_at = w.offset;\n        w.put32(0);\n")
		g.pf("        if !%s(w, &value.%s[i]) {\n            return false;\n        }\n", fn(f.Type.Name, "save_body"), f.Name)
		g.pf("        w.patch32(element_len_at, (w.offset - element_len_at - 4) as u32);\n")
	case isEnum(f):
		g.pf("        let element_id = match value.%s[i].table_id() {\n            Some(id) => id,\n            None => return false,\n        };\n", f.Name)
		g.pf("        w.put16(element_id);\n")
	default:
		g.pf("        w.%s(%s);\n", putFn(tableKindWidth(kind)), g.scalarToWire(f, fmt.Sprintf("value.%s[i]", f.Name)))
	}
	g.pf("    }\n")
	g.pf("    w.patch32(len_at, (w.offset - len_at - 4) as u32);\n")
}

func (g *gen) emitSaveUnion(f *ir.Field, id uint16, fieldKind int) {
	un := unionOf(f)
	g.pf("    if value.%s != %s::None {\n", f.Name, f.Type.Name)
	g.pf("        w.put16(0x%04x);\n        w.put8(%d); // %s\n", id, fieldKind, f.Name)
	g.pf("        // the ARM ID is the hash of the arm's NAME (docs/SPEC-TABLES.md §5), so\n")
	g.pf("        // arms may be added anywhere, removed and reordered\n")
	g.pf("        match &value.%s {\n", f.Name)
	g.pf("            %s::None => {}\n", f.Type.Name)
	for _, v := range un.Variants {
		g.pf("            %s::%s(_) => w.put16(0x%04x),\n", f.Type.Name, ir.GoExportName(v.Name), ir.VariantId(v.Name))
	}
	g.pf("        }\n")
	g.pf("        let len_at = w.offset;\n        w.put32(0);\n")
	g.pf("        match &value.%s {\n", f.Name)
	g.pf("            %s::None => {}\n", f.Type.Name)
	for _, v := range un.Variants {
		g.pf("            %s::%s(arm) => {\n", f.Type.Name, ir.GoExportName(v.Name))
		g.pf("                if !%s(w, arm) {\n                    return false;\n                }\n", fn(v.Type, "save_body"))
		g.pf("            }\n")
	}
	g.pf("        }\n")
	g.pf("        w.patch32(len_at, (w.offset - len_at - 4) as u32);\n")
	g.pf("    }\n")
}

func (g *gen) emitSaveKeyed(f *ir.Field, kind int, id uint16) {
	g.pf("    {\n")
	g.pf("        let mut pairs: u32 = 0;\n")
	g.pf("        for i in 0..%s {\n", arrayLen(f))
	g.emitKeyedSlotSkip(f, kind, "            ")
	g.pf("            pairs += 1;\n")
	g.pf("        }\n")
	g.pf("        if pairs > 0 {\n")
	g.pf("            // KIND 16, not 14: a keyed body and a positional one are\n")
	g.pf("            // incompatible, so a reader of the other kind must see a kind\n")
	g.pf("            // mismatch and skip, never misdecode (docs/SPEC-TABLES.md §3.2)\n")
	g.pf("            w.put16(0x%04x);\n            w.put8(%d); // %s (keyed by %s)\n", id, ir.TableKindKeyed, f.Name, f.KeyEnum)
	g.pf("            let len_at = w.offset;\n            w.put32(0);\n")
	g.pf("            w.put8(%d);\n            w.put32(pairs);\n", ir.TableScalarKind(f))
	g.pf("            // ASCENDING BY VARIANT ORDINAL, which is slot order — this\n")
	g.pf("            // writer's choice, and a reader must not rely on it: every slot\n")
	g.pf("            // is found by its key (docs/SPEC-TABLES.md §3.2)\n")
	g.pf("            for i in 0..%s {\n", arrayLen(f))
	g.emitKeyedSlotSkip(f, kind, "                ")
	g.pf("                let key_id = match %s.table_id() {\n                    Some(id) => id,\n                    None => return false,\n                };\n", keyOfSlot(f, "i"))
	g.pf("                w.put16(key_id); // the slot's VARIANT id, not its position\n")
	g.pf("                let element_len_at = w.offset;\n                w.put32(0);\n")
	switch {
	case kind == tkTable:
		g.pf("                if !%s(w, &%s[i]) {\n                    return false;\n                }\n", fn(f.Type.Name, "save_body"), g.keyedSlots(f))
	case isEnum(f):
		g.pf("                let element_id = match %s[i].table_id() {\n                    Some(id) => id,\n                    None => return false,\n                };\n", g.keyedSlots(f))
		g.pf("                w.put16(element_id);\n")
	default:
		g.pf("                w.%s(%s);\n", putFn(tableKindWidth(kind)), g.scalarToWire(f, fmt.Sprintf("%s[i]", g.keyedSlots(f))))
	}
	g.pf("                w.patch32(element_len_at, (w.offset - element_len_at - 4) as u32);\n")
	g.pf("            }\n")
	g.pf("            w.patch32(len_at, (w.offset - len_at - 4) as u32);\n")
	g.pf("        }\n    }\n")
}

// emitKeyedSlotSkip writes the two elision guards a keyed slot takes on the
// write side: an all-default slot elides, and a slot whose key names no
// variant refuses.
func (g *gen) emitKeyedSlotSkip(f *ir.Field, kind int, ind string) {
	if kind == tkTable {
		g.pf("%slet element = %s(&%s[i]);\n", ind, fn(f.Type.Name, "measure"), g.keyedSlots(f))
		g.pf("%sif element < 0 {\n%s    return false;\n%s}\n", ind, ind, ind)
		g.pf("%sif element <= 2 {\n%s    continue; // an all-default slot elides\n%s}\n", ind, ind, ind)
		return
	}
	g.pf("%sif %s[i] == %s {\n%s    continue; // a default slot elides\n%s}\n",
		ind, g.keyedSlots(f), zeroScalar(f), ind, ind)
	if isEnum(f) {
		g.pf("%sif %s[i].table_id().is_none() {\n%s    return false; // no variant names this value\n%s}\n",
			ind, g.keyedSlots(f), ind, ind)
	}
}

// scalarToWire renders the cast that puts one scalar into its wire word.
func (g *gen) scalarToWire(f *ir.Field, expr string) string {
	switch f.Type.Kind {
	case ir.TBool:
		return expr + " as u8"
	case ir.TFloat32:
		return "table_float_to_bits(" + expr + ")"
	case ir.TFloat64:
		return "table_double_to_bits(" + expr + ")"
	}
	if isFlags(f) {
		return expr
	}
	width := tableKindWidth(ir.TableScalarKind(f))
	return fmt.Sprintf("%s as u%d", expr, width*8)
}

// ---- load ----

func (g *gen) emitLoad(st *ir.Struct) {
	g.pf("#[inline(always)]\n")
	g.pf("pub fn %s(r: &mut TableReader, report: &mut TableReport, value: &mut %s) -> bool {\n",
		fn(st.Name, "load_body"), st.Name)
	g.pf("    %s(value); // restore declared defaults in place, then overlay\n", fn(st.Name, "reset"))
	g.pf("    loop {\n")
	g.pf("        if !r.has(2) {\n            report.malformed = true;\n            return false;\n        }\n")
	g.pf("        let field_id = r.get16();\n")
	g.pf("        if field_id == 0 {\n            return true;\n        }\n")
	g.pf("        if !r.has(1) {\n            report.malformed = true;\n            return false;\n        }\n")
	g.pf("        let kind = r.get8();\n")
	g.pf("        match field_id {\n")
	for _, f := range st.Fields {
		g.emitLoadField(f)
	}
	g.pf("            _ => {\n")
	g.pf("                report.unknown += 1;\n")
	g.pf("                if !r.skip(kind) {\n                    report.malformed = true;\n                    return false;\n                }\n")
	g.pf("            }\n")
	g.pf("        }\n")
	g.pf("    }\n}\n\n")

	g.pf("pub fn %s(value: &mut %s, bytes: &[u8], report: &mut TableReport) -> bool {\n", fn(st.Name, "load"), st.Name)
	g.pf("    let mut r = TableReader::new(bytes);\n")
	g.pf("    %s(&mut r, report, value)\n}\n\n", fn(st.Name, "load_body"))
}

func (g *gen) emitLoadField(f *ir.Field) {
	kind := ir.TableScalarKind(f)
	fieldKind := ir.TableFieldKind(f)
	id := ir.TableFieldId(f)
	g.pf("            0x%04x => {\n                // %s\n", id, f.Name)
	g.pf("                if kind != %d {\n", fieldKind)
	g.pf("                    report.kind_mismatch += 1;\n")
	g.pf("                    if !r.skip(kind) {\n                        report.malformed = true;\n                        return false;\n                    }\n")
	g.pf("                } else {\n")
	g.indent = "                    "
	g.emitLoadPayload(f, kind)
	if f.Type.Optional {
		g.pf("value.%s_present = true;\n", f.Name)
	}
	g.indent = ""
	g.pf("                }\n            }\n")
}

func (g *gen) emitLoadPayload(f *ir.Field, kind int) {
	switch {
	case f.Type.Kind == ir.TString:
		g.pf("if !r.has(4) {\n    report.malformed = true;\n    return false;\n}\n")
		g.pf("let len = r.get32();\n")
		g.pf("if !r.has(len as u64) {\n    report.malformed = true;\n    return false;\n}\n")
		g.pf("let mut keep = len as usize;\n")
		g.pf("if keep > %d {\n    keep = %d;\n    report.clamped += 1;\n}\n", f.Type.Size, f.Type.Size)
		g.pf("value.%s[..keep].copy_from_slice(&r.buffer[r.offset..r.offset + keep]);\n", f.Name)
		g.pf("value.%s_length = keep as i32;\n", f.Name)
		g.pf("r.offset += len as usize;\n")
	case f.Type.Kind == ir.TBytes:
		g.emitLoadArray(f, ir.TableElemKind(f), f.Type.Size, f.Name+"_length")
	case f.KeyEnum != "":
		g.emitLoadKeyed(f, kind)
	case f.Array == ir.ArrayCounted:
		g.emitLoadArray(f, kind, f.ArrayBound, f.Name+"_count")
	case f.Array == ir.ArrayFixed:
		g.emitLoadArray(f, kind, f.ArrayBound, "")
	case kind == tkTable:
		g.pf("if !r.has(4) {\n    report.malformed = true;\n    return false;\n}\n")
		g.pf("let body_len = r.get32() as usize;\n")
		g.pf("if !r.has(body_len as u64) {\n    report.malformed = true;\n    return false;\n}\n")
		g.pf("{\n")
		g.pf("    let mut sub = r.sub(body_len);\n")
		g.pf("    %s(&mut sub, report, &mut value.%s);\n", fn(f.Type.Name, "load_body"), f.Name)
		g.pf("}\n")
		g.pf("r.offset += body_len;\n")
	case kind == tkUnion:
		g.emitLoadUnion(f)
	case isEnum(f):
		g.pf("if !r.has(2) {\n    report.malformed = true;\n    return false;\n}\n")
		g.pf("{\n")
		g.pf("    let variant = r.get16();\n")
		g.pf("    value.%s = match %s::table_value(variant) {\n", f.Name, f.Type.Name)
		g.pf("        Some(v) => v,\n")
		g.pf("        None => {\n            report.unknown += 1;\n            %s::NONE\n        }\n", f.Type.Name)
		g.pf("    };\n")
		g.pf("}\n")
	default:
		width := tableKindWidth(kind)
		g.pf("if !r.has(%d) {\n    report.malformed = true;\n    return false;\n}\n", width)
		g.pf("{\n")
		g.indent += "    "
		g.emitScalarDecode(f, kind, "r", fmt.Sprintf("value.%s", f.Name))
		g.indent = strings.TrimSuffix(g.indent, "    ")
		g.pf("}\n")
	}
}

// emitScalarDecode reads one scalar out of `reader` and stores it at `target`,
// clamping to the field's declared range on the way.
func (g *gen) emitScalarDecode(f *ir.Field, kind int, reader, target string) {
	width := tableKindWidth(kind)
	switch f.Type.Kind {
	case ir.TBool:
		g.pf("%s = %s.get8() != 0;\n", target, reader)
		return
	case ir.TFloat32:
		g.pf("%s = table_bits_to_float(%s.get32());\n", target, reader)
		return
	case ir.TFloat64:
		g.pf("%s = table_bits_to_double(%s.get64());\n", target, reader)
		return
	}
	typ := rustFieldType(f.Type)
	if f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString {
		typ = "u8"
	}
	if isFlags(f) {
		g.pf("%s = %s.get64();\n", target, reader)
		return
	}
	// A declared bound sitting ON the decode local's own storage limit is a
	// comparison no decoded value can satisfy, so it is not emitted — the same
	// "this check cannot fire" rule the C++ reference applies, and here it is
	// also what keeps the generated code clear of clippy's
	// absurd_extreme_comparisons, which is DENY by default and would fail a
	// consumer's build for something they did not write (#342).
	low, high := false, false
	if f.HasIntRange {
		low, high = clampEnds(f, width)
	}
	mutable := ""
	if low || high {
		mutable = "mut "
	}
	g.pf("let %sdecoded = %s.%s() as %s;\n", mutable, reader, getFn(width), typ)
	switch {
	case low && high:
		lo, hi := intLit(f.IntMin, typ), intLit(f.IntMax, typ)
		g.pf("if decoded < %s {\n    decoded = %s;\n    report.clamped += 1;\n", lo, lo)
		g.pf("} else if decoded > %s {\n    decoded = %s;\n    report.clamped += 1;\n}\n", hi, hi)
	case low:
		lo := intLit(f.IntMin, typ)
		g.pf("if decoded < %s {\n    decoded = %s;\n    report.clamped += 1;\n}\n", lo, lo)
	case high:
		hi := intLit(f.IntMax, typ)
		g.pf("if decoded > %s {\n    decoded = %s;\n    report.clamped += 1;\n}\n", hi, hi)
	}
	g.pf("%s = decoded;\n", target)
}

// emitLoadArray reads an array body: the element kind and count, then the
// bounded prefix. `countField` is the storage count companion, "" for a fixed
// array which keeps every slot.
func (g *gen) emitLoadArray(f *ir.Field, kind int, bound int64, countField string) {
	elemKind := ir.TableElemKind(f)
	g.pf("if !r.has(4) {\n    report.malformed = true;\n    return false;\n}\n")
	g.pf("let body_len = r.get32() as usize;\n")
	g.pf("if !r.has(body_len as u64) {\n    report.malformed = true;\n    return false;\n}\n")
	g.pf("let body_end = r.offset + body_len;\n")
	g.pf("if body_len >= 5 {\n")
	g.indent += "    "
	g.pf("let element_kind = r.get8();\n")
	g.pf("let count = r.get32();\n")
	g.pf("if element_kind != %d {\n", elemKind)
	g.pf("    report.kind_mismatch += 1;\n")
	g.pf("    r.offset = body_end;\n")
	g.pf("} else {\n")
	g.indent += "    "
	g.pf("let mut keep = count as usize;\n")
	g.pf("if keep > %d {\n    keep = %d;\n    report.clamped += 1;\n}\n", bound, bound)
	g.pf("// elements are BOUNDED by the field body: a count the length cannot\n")
	g.pf("// cover keeps the decoded prefix, flags malformed, and the parent\n")
	g.pf("// continues at the next field — following fields' bytes are never\n")
	g.pf("// fabricated into elements\n")
	g.pf("let mut sub = r.sub(body_end - r.offset);\n")
	g.pf("let mut decoded = 0usize;\n")
	g.pf("for i in 0..keep {\n")
	g.indent += "    "
	switch {
	case kind == tkTable:
		g.pf("if !sub.has(4) {\n    report.malformed = true;\n    break;\n}\n")
		g.pf("let element_len = sub.get32() as usize;\n")
		g.pf("if !sub.has(element_len as u64) {\n    report.malformed = true;\n    break;\n}\n")
		g.pf("{\n")
		g.pf("    let mut element = sub.sub(element_len);\n")
		g.pf("    %s(&mut element, report, &mut value.%s[i]);\n", fn(f.Type.Name, "load_body"), f.Name)
		g.pf("}\n")
		g.pf("sub.offset += element_len;\n")
	case isEnum(f):
		g.pf("if !sub.has(2) {\n    report.malformed = true;\n    break;\n}\n")
		g.pf("{\n")
		g.pf("    let variant = sub.get16();\n")
		g.pf("    value.%s[i] = match %s::table_value(variant) {\n", f.Name, f.Type.Name)
		g.pf("        Some(v) => v,\n")
		g.pf("        None => {\n            report.unknown += 1;\n            %s::NONE\n        }\n", f.Type.Name)
		g.pf("    };\n")
		g.pf("}\n")
	default:
		width := tableKindWidth(kind)
		g.pf("if !sub.has(%d) {\n    report.malformed = true;\n    break;\n}\n", width)
		g.pf("{\n")
		g.indent += "    "
		g.emitScalarDecode(f, kind, "sub", fmt.Sprintf("value.%s[i]", f.Name))
		g.indent = strings.TrimSuffix(g.indent, "    ")
		g.pf("}\n")
	}
	g.pf("decoded = i + 1;\n")
	g.indent = strings.TrimSuffix(g.indent, "    ")
	g.pf("}\n")
	if countField != "" {
		g.pf("value.%s = decoded as i32;\n", countField)
	} else {
		g.pf("let _ = decoded; // a fixed array keeps every slot: the prefill holds the tail\n")
	}
	g.indent = strings.TrimSuffix(g.indent, "    ")
	g.pf("}\n")
	g.indent = strings.TrimSuffix(g.indent, "    ")
	g.pf("}\n")
	g.pf("r.offset = body_end; // excess elements and slack skip via the length\n")
}

func (g *gen) emitLoadUnion(f *ir.Field) {
	un := unionOf(f)
	g.pf("if !r.has(2) {\n    report.malformed = true;\n    return false;\n}\n")
	g.pf("let arm_id = r.get16();\n")
	g.pf("if arm_id == 0 {\n")
	g.pf("    value.%s = %s::None; // empty: the id is the whole payload\n", f.Name, f.Type.Name)
	g.pf("} else {\n")
	g.indent += "    "
	g.pf("if !r.has(4) {\n    report.malformed = true;\n    return false;\n}\n")
	g.pf("let body_len = r.get32() as usize;\n")
	g.pf("if !r.has(body_len as u64) {\n    report.malformed = true;\n    return false;\n}\n")
	g.pf("{\n")
	g.pf("    let mut sub = r.sub(body_len);\n")
	g.pf("    match arm_id {\n")
	for _, v := range un.Variants {
		g.pf("        0x%04x => {\n            // %s\n", ir.VariantId(v.Name), v.Name)
		g.pf("            let mut arm = %s::default();\n", v.Type)
		g.pf("            %s(&mut arm);\n", fn(v.Type, "reset"))
		g.pf("            %s(&mut sub, report, &mut arm);\n", fn(v.Type, "load_body"))
		g.pf("            value.%s = %s::%s(arm);\n", f.Name, f.Type.Name, ir.GoExportName(v.Name))
		g.pf("        }\n")
	}
	g.pf("        _ => {\n")
	g.pf("            // an arm this reader cannot name: the value reads EMPTY and the\n")
	g.pf("            // body is skipped by its length, never misdecoded. The reset is\n")
	g.pf("            // explicit, not the prefill's: a repeated field id must not leave\n")
	g.pf("            // an arm decoded by an earlier occurrence standing (§4).\n")
	g.pf("            value.%s = %s::None;\n", f.Name, f.Type.Name)
	g.pf("            report.unknown += 1;\n")
	g.pf("        }\n")
	g.pf("    }\n")
	g.pf("}\n")
	g.pf("r.offset += body_len;\n")
	g.indent = strings.TrimSuffix(g.indent, "    ")
	g.pf("}\n")
}

func (g *gen) emitLoadKeyed(f *ir.Field, kind int) {
	g.pf("if !r.has(4) {\n    report.malformed = true;\n    return false;\n}\n")
	g.pf("let body_len = r.get32() as usize;\n")
	g.pf("if !r.has(body_len as u64) {\n    report.malformed = true;\n    return false;\n}\n")
	g.pf("let body_end = r.offset + body_len;\n")
	g.pf("if body_len >= 5 {\n")
	g.indent += "    "
	g.pf("let element_kind = r.get8();\n")
	g.pf("let count = r.get32();\n")
	g.pf("if element_kind != %d {\n    report.kind_mismatch += 1;\n    r.offset = body_end;\n} else {\n", ir.TableScalarKind(f))
	g.indent += "    "
	g.pf("let mut sub = r.sub(body_end - r.offset);\n")
	g.pf("for _ in 0..count {\n")
	g.indent += "    "
	g.pf("if !sub.has(2) {\n    report.malformed = true;\n    break;\n}\n")
	g.pf("let key = sub.get16();\n")
	g.pf("if !sub.has(4) {\n    report.malformed = true;\n    break;\n}\n")
	g.pf("let element_len = sub.get32() as usize;\n")
	g.pf("if !sub.has(element_len as u64) {\n    report.malformed = true;\n    break;\n}\n")
	g.pf("if key == 0 {\n")
	g.pf("    // None is the NULL KEY: 0 is the reserved id no declared name can\n")
	g.pf("    // fold to, so a body carrying one is DAMAGED, not merely foreign.\n")
	g.pf("    // Framing damage stops this body, keeps what it decoded, and the\n")
	g.pf("    // parent reads on past the length (docs/SPEC-TABLES.md §3.2, §4).\n")
	g.pf("    report.malformed = true;\n    break;\n}\n")
	g.pf("let slot = match %s::table_value(key) {\n", f.KeyEnum)
	g.pf("    Some(k) => k.0 as usize - 1,\n")
	g.pf("    None => {\n")
	g.pf("        report.unknown += 1; // a slot this reader cannot name\n")
	g.pf("        sub.offset += element_len;\n")
	g.pf("        continue;\n")
	g.pf("    }\n")
	g.pf("};\n")
	g.pf("{\n")
	g.indent += "    "
	g.pf("let mut element = sub.sub(element_len);\n")
	switch {
	case kind == tkTable:
		g.pf("%s(&mut element, report, &mut %s[slot]);\n", fn(f.Type.Name, "load_body"), g.keyedSlots(f))
	case isEnum(f):
		g.pf("if element.has(2) {\n")
		g.pf("    let variant = element.get16();\n")
		g.pf("    %s[slot] = match %s::table_value(variant) {\n", g.keyedSlots(f), f.Type.Name)
		g.pf("        Some(v) => v,\n")
		g.pf("        None => {\n            report.unknown += 1;\n            %s::NONE\n        }\n", f.Type.Name)
		g.pf("    };\n")
		g.pf("} else {\n    report.malformed = true;\n}\n")
	default:
		width := tableKindWidth(kind)
		g.pf("if element.has(%d) {\n", width)
		g.indent += "    "
		g.emitScalarDecode(f, kind, "element", fmt.Sprintf("%s[slot]", g.keyedSlots(f)))
		g.indent = strings.TrimSuffix(g.indent, "    ")
		g.pf("} else {\n    report.malformed = true;\n}\n")
	}
	g.indent = strings.TrimSuffix(g.indent, "    ")
	g.pf("}\n")
	g.pf("sub.offset += element_len;\n")
	g.indent = strings.TrimSuffix(g.indent, "    ")
	g.pf("}\n")
	g.indent = strings.TrimSuffix(g.indent, "    ")
	g.pf("}\n")
	g.indent = strings.TrimSuffix(g.indent, "    ")
	g.pf("}\n")
	g.pf("r.offset = body_end; // unread pairs and slack skip via the length\n")
}

// ---- relocatability ----

func (g *gen) emitRelocatabilityAsserts(members []*ir.Struct) {
	g.pf("// RELOCATABLE STORAGE, enforced (docs/SPEC-TABLES.md §9): every closure\n")
	g.pf("// member is #[repr(C)] and Copy, so a table value may be memcpy'd,\n")
	g.pf("// mmapped or handed across a process boundary as bytes. The Copy bound\n")
	g.pf("// below is a compile error the day a member grows something that is not.\n")
	g.pf("const fn table_relocatable<T: Copy>() {}\n")
	for _, st := range members {
		g.pf("const _: () = table_relocatable::<%s>();\n", st.Name)
	}
	g.pf("\n")
}

// tableFieldTypeName is the schema type name a descriptor and a comment
// carry for one field.
func tableFieldTypeName(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		return "bool"
	case ir.TFloat32:
		return "float32"
	case ir.TFloat64:
		return "float64"
	case ir.TString:
		return "string"
	case ir.TBytes:
		return "bytes"
	case ir.TBits:
		return fmt.Sprintf("bits(%d)", f.Type.Width)
	case ir.TInt:
		if f.Type.Signed {
			return fmt.Sprintf("int%d", f.Type.Width)
		}
		return fmt.Sprintf("uint%d", f.Type.Width)
	case ir.TNamed:
		return f.Type.Name
	}
	return "?"
}

// storageRange is the inclusive range an integer storage of the given width
// can hold.
func storageRange(signed bool, bits int) (*big.Int, *big.Int) {
	one := big.NewInt(1)
	if signed {
		hi := new(big.Int).Lsh(one, uint(bits-1))
		return new(big.Int).Neg(hi), new(big.Int).Sub(hi, one)
	}
	return big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(one, uint(bits)), one)
}

// clampEnds answers which ends of a declared min/max range a read can actually
// clamp at. The decode local is the wire kind's own width, so a bound sitting
// ON that width's limit is a comparison no decoded value can satisfy and the
// emitter drops it (#342, docs/SPEC-TABLES.md §4: an elided end is one that
// could never have clamped or counted, so no read report moves).
func clampEnds(f *ir.Field, widthBytes int) (low, high bool) {
	signed := f.Type.Kind == ir.TInt && f.Type.Signed
	lo, hi := storageRange(signed, widthBytes*8)
	return f.IntMin.Cmp(lo) > 0, f.IntMax.Cmp(hi) < 0
}

// keyOfSlot renders the KEY a storage slot holds: the storage shifts left, so
// slot i holds the key i + 1, at the key enum's own storage width.
func keyOfSlot(f *ir.Field, index string) string {
	return fmt.Sprintf("%s((%s + 1) as %s)", f.KeyEnum, index, rustUint(f.KeyEnumRef.StorageBits))
}
