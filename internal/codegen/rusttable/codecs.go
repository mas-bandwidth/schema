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
	unions, enums := g.unionMembers(), g.allTableEnums()
	if len(members) == 0 && len(unions) == 0 && len(enums) == 0 {
		return nil
	}

	for _, st := range members {
		if st.IsTable {
			g.emitStorage(st)
		}
	}
	for _, u := range unions {
		if g.unit.TableUnions[u.Name] != nil {
			g.emitUnionStorage(u)
		}
		g.emitUnionWire(u)
		g.emitMessageUnion(u)
	}
	for _, e := range enums {
		g.emitEnumIdentity(e)
	}
	for _, st := range members {
		g.owner = st
		g.emitReset(st)
	}
	for _, st := range members {
		g.owner = st
		if g.variable[st.Name] {
			g.emitVariableSurface(st)
		} else {
			g.emitMeasure(st)
		}
		g.emitRecordRuntime(st)
		g.emitSave(st)
		g.emitMessageSave(st)
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
// tolerance event. Fixed codecs and region loads use caller-owned storage;
// graph authoring and save numbering allocate temporary maps.

#![allow(clippy::needless_range_loop)]
#![allow(clippy::needless_late_init)]
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
	case f.IsList() || f.IsMap():
		g.pf("pub %s: %s<%s>,\n", f.Name, sequenceSlotType(f), sequenceType(f))
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		g.pf("    pub %s: %s,\n", f.Name, typ)
	case f.Type.Kind == ir.TWString:
		g.pf("    pub %s: [u16; %d],\n    pub %s_length: i32,\n", f.Name, f.Type.Size, f.Name)
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
	}
	if f.Type.Optional {
		g.pf("    pub %s_present: bool,\n", f.Name)
	}
}

// emitZeroInit writes one field's ZERO-form struct-literal entry. The
// declared defaults are the reset's business; this is the storage the reset
// lays them over.
func (g *gen) emitZeroInit(f *ir.Field, ind string) {
	switch {
	case f.IsList() || f.IsMap():
		g.pf("%s%s: %s::default(),\n", ind, f.Name, sequenceSlotType(f))
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		g.pf("%s%s: TableRef::NULL,\n", ind, f.Name)
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString || f.Type.Kind == ir.TBytes:
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
	}
	if f.Type.Optional {
		g.pf("%s%s_present: false,\n", ind, f.Name)
	}
}

func zeroScalar(f *ir.Field) string {
	if f.Type.Pointer {
		return "TableRef::NULL"
	}
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
	g.pf("// %s on the TABLE wire: a value rides as the 64-bit hash of its VARIANT\n", e.Name)
	g.pf("// NAME, so a variant may be added anywhere, removed, or reordered and old\n")
	g.pf("// data still reads (docs/SPEC-TABLES.md §5). None takes reference 0.\n")
	g.pf("impl TableEnum for %s {\n", e.Name)
	g.pf("    fn table_id(self) -> Option<u64> {\n")
	g.pf("        match self.0 {\n")
	g.pf("            0 => Some(0),\n")
	for i := range e.Variants {
		g.pf("            %d => Some(0x%016x),\n", i+1, ir.TableWireId(e.VariantWireName(i)))
	}
	g.pf("            _ => None, // no variant names this value: no wire identity\n")
	g.pf("        }\n    }\n\n")
	g.pf("    fn table_value(id: u64) -> Option<%s> {\n", e.Name)
	g.pf("        match id {\n")
	for i := range e.Variants {
		g.pf("            0x%016x => Some(%s(%d)),\n", ir.TableWireId(e.VariantWireName(i)), e.Name, i+1)
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
	case f.IsList() || f.IsMap():
		g.pf("value.%s=%s::default();\n", name, sequenceSlotType(f))
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		g.pf("    value.%s = TableRef::NULL;\n", name)
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString || f.Type.Kind == ir.TBytes:
		g.pf("    value.%s.fill(0);\n", name)
		g.pf("    value.%s_length = %d;\n", name, len(f.DefBytes))
		if len(f.DefBytes) > 0 {
			g.pf("    value.%s[..%d].copy_from_slice(&%s);\n", name, len(f.DefBytes), rustBytes(f.DefBytes))
		}
	case f.KeyEnum != "":
		if isStruct(f) && !f.Type.Pointer {
			g.pf("    for slot in %s.iter_mut() {\n", g.keyedSlots(f))
			g.pf("        %s(slot);\n", fn(f.Type.Name, "reset"))
			g.pf("    }\n")
		} else {
			g.pf("    %s.fill(%s);\n", g.keyedSlots(f), zeroScalar(f))
		}
	case f.Array != ir.ArrayNone:
		switch {
		case isStruct(f) && !f.Type.Pointer:
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
	case isStruct(f) && !f.Type.Pointer:
		g.pf("    %s(&mut value.%s);\n", fn(f.Type.Name, "reset"), name)
	case isUnion(f):
		g.pf("    value.%s = %s::None;\n", name, f.Type.Name)
	default:
		g.pf("    value.%s = %s;\n", name, g.defaultValue(f))
	}
	if f.Type.Optional {
		g.pf("    value.%s_present = false;\n", name)
	}
}

// defaultValue is a field's declared default as a Rust expression — the value
// the write side elides against and the reset restores.
func (g *gen) defaultValue(f *ir.Field) string {
	if f.Type.Pointer {
		return "TableRef::NULL"
	}
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
	case ir.TInt, ir.TBits, ir.TFixed:
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
			if f.HasDefault && f.DefInt != nil {
				return f.DefInt.String()
			}
			return "0"
		}
	}
	return "0"
}

func rustBytes(bytes []byte) string {
	values := make([]string, len(bytes))
	for i, b := range bytes {
		values[i] = fmt.Sprint(b)
	}
	return "[" + strings.Join(values, ",") + "]"
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

// emitScalarDecode reads one scalar out of `reader` and stores it at `target`,
// clamping to the field's declared range on the way.
func (g *gen) emitScalarDecode(f *ir.Field, kind int, reader, target string) {
	if f.Type.Kind == ir.TFixed {
		copy := *f
		copy.IntMin, copy.IntMax, copy.HasIntRange = ir.TableRawRange(f)
		f = &copy
	}
	width := tableKindWidth(kind)
	switch f.Type.Kind {
	case ir.TBool:
		g.pf("%s = %s.get8() != 0;\n", target, reader)
		return
	case ir.TFloat32, ir.TFloat64:
		conv := "table_bits_to_float"
		if f.Type.Kind == ir.TFloat64 {
			conv = "table_bits_to_double"
		}
		if f.HasFloatRange {
			g.pf("let mut decoded = %s(%s.%s());\n", conv, reader, getFn(width))
			g.pf("if decoded < %s { decoded = %s; report.clamped += 1; } else if decoded > %s { decoded = %s; report.clamped += 1; }\n", formatFloat(f.FMin, width == 4), formatFloat(f.FMin, width == 4), formatFloat(f.FMax, width == 4), formatFloat(f.FMax, width == 4))
			g.pf("%s = decoded;\n", target)
		} else {
			g.pf("%s = %s(%s.%s());\n", target, conv, reader, getFn(width))
		}
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
	if low || high || (f.Type.Kind == ir.TBits && f.Type.Width < width*8) {
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
	if f.Type.Kind == ir.TBits && f.Type.Width < width*8 {
		max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
		g.pf("if decoded > %s { decoded = %s; report.clamped += 1; }\n", intLit(max, typ), intLit(max, typ))
	}
	g.pf("%s = decoded;\n", target)
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
	if f.IsMap() {
		return ir.TableTypeSpelling(f)
	}
	switch f.Type.Kind {
	case ir.TFixed:
		return ir.TableTypeSpelling(f)
	case ir.TWString:
		return "wstring"
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
	signed := (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) && f.Type.Signed
	lo, hi := storageRange(signed, widthBytes*8)
	return f.IntMin.Cmp(lo) > 0, f.IntMax.Cmp(hi) < 0
}

// keyOfSlot renders the KEY a storage slot holds: the storage shifts left, so
// slot i holds the key i + 1, at the key enum's own storage width.
func keyOfSlot(f *ir.Field, index string) string {
	return fmt.Sprintf("%s((%s + 1) as %s)", f.KeyEnum, index, rustUint(f.KeyEnumRef.StorageBits))
}
