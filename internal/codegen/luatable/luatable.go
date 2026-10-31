// Package luatable emits the Lua target's TABLE SURFACE (docs/SPEC-TABLES.md;
// the design is docs/PORT-LUA.md): one self-contained module per unit,
// <Package>Tables.lua, holding ONE DESCRIPTOR per table and per table-side
// union — the facts a codec needs, as DATA a single runtime walks (the
// jstable/variableruntime.go shape) — and a constructor New<Table>() per
// table that builds the value at its declared defaults.
//
// Lua has no annotations and no declared field types, so a field's kind lives
// in its descriptor: its name, its JSON key (§16.3), its wire id (§5), its
// ordinal, its wire kind (§3), its storage shape, its bounds and its default.
// A codec card reads the descriptor; a value of the wrong Lua type on write is
// that card's NAMED refusal, never a coercion.
//
// There is NO CODEC here. The forms (fixedform.go, messageform.go) join
// Generate by the pass hook below, so no later card edits this file, and every
// card brings its runtime/*.lua file, which the glob below embeds and emits
// beside the module (one file per card).
//
// STORAGE (docs/PORT-LUA.md, THE TYPE MAPPING, table column): a table value is
// a Lua table keyed by the schema field name. Scalars are as the type surface
// stores them (internal/codegen/lua); an ENUM is its variant NAME as a Lua
// string and nil for None, because a table enum's identity is the name's hash
// (§5) and not an ordinal; an optional ?T is the value plus <field>_present
// (§2.3); a pointer and a byte buffer are nil until set (§2.1, §2.5); a list
// is a sequence and a map a Lua table keyed by the key (§2.8, §2.9); a union
// is { tag = 0 } with the selected arm under its name (§2.6).
package luatable

import (
	"embed"
	"fmt"
	"io/fs"
	"math/big"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Module is the unit's table module while it is being emitted; a pass
// appends to Body.
type Module struct {
	Unit    *ir.Unit
	Closure []*ir.Struct // every table and map entry the unit's tables reach, by name
	Unions  []*ir.Union  // every table-side union, by name
	Body    strings.Builder
}

// Printf appends formatted text to the module body.
func (m *Module) Printf(format string, args ...any) { fmt.Fprintf(&m.Body, format, args...) }

// Pass is a later form's emission step, run once per unit after the
// descriptors and constructors, in registration order.
type Pass func(m *Module) error

var passes []Pass

// RegisterPass adds a pass. Called from init() only.
func RegisterPass(p Pass) { passes = append(passes, p) }

// runtime is the table runtime, runtime/*.lua, embedded BY GLOB so a later
// card adds its file (load.lua, save.lua, json.lua, ...) and nothing here
// changes. descriptor.lua, the one descriptor walk, is the file the glob
// starts from.
//
//go:embed runtime/*.lua
var runtime embed.FS

// ModuleName is the unit's table module basename: its package, capitalized.
func ModuleName(u *ir.Unit) string {
	if u.Package == "" {
		return "Tables"
	}
	return strings.ToUpper(u.Package[:1]) + u.Package[1:] + "Tables"
}

// Generate returns <Package>Tables.lua and the registered runtime files when
// the unit declares tables, and nothing when it does not. The table descriptors
// name the fixed-point and 128-bit kinds (docs/SPEC-TABLES.md §3, §15): a wide
// value rides as its declared raw storage image, so nothing is refused here.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	if len(u.Tables) == 0 {
		return map[string][]byte{}, nil
	}
	return generate(u)
}

// generate is Generate's body. It carries every table-wire kind, including the
// fixed-point and 128-bit families (docs/SPEC-TABLES.md §3, §15).
func generate(u *ir.Unit) (map[string][]byte, error) {
	name := ModuleName(u)
	for _, f := range u.Files {
		if f.Base == name {
			return nil, fmt.Errorf("schema file %s.schema collides with the Lua table module %s.lua the unit's tables are emitted into; rename the file (docs/PORT-LUA.md)", f.Base, name)
		}
	}
	m := &Module{Unit: u}
	m.Closure, m.Unions = closure(u)
	emit(m)
	for _, p := range passes {
		if err := p(m); err != nil {
			return nil, err
		}
	}
	emitTail(m)
	out := map[string][]byte{name + ".lua": lua.Format([]byte(m.Body.String()))}
	names, err := fs.Glob(runtime, "runtime/*.lua")
	if err != nil {
		return nil, err
	}
	for _, n := range names {
		data, err := fs.ReadFile(runtime, n)
		if err != nil {
			return nil, err
		}
		base := n[len("runtime/"):]
		for _, f := range u.Files {
			if f.Base+".lua" == base {
				return nil, fmt.Errorf("schema file %s.schema collides with the Lua table runtime %s emitted beside the unit's modules; rename the file (docs/PORT-LUA.md)", f.Base, base)
			}
		}
		// A form no table in the unit carries emits no surface file at all
		// (docs/SPEC-TABLES.md §2.12), and a declared unit's runtime refuses
		// a call for a form its table left out by that form's name. The
		// surfaces outside the forms vocabulary (fixed, view, message,
		// retain) are governed by the surface list alone and always stay.
		if !runtimeCarriesForm(u, base) {
			continue
		}
		if unitDeclaresForms(u) {
			data = gateDeclaredFormsRuntime(base, data)
		}
		out[base] = data
	}
	return out, nil
}

// emitTail closes the module: the keyed accessor's entry (docs/PORTING.md M5),
// then the seal (M13), which builds every derived descriptor fact once and
// freezes the module's data against new keys. It is the last thing the module
// runs before it returns, so every pass's descriptors are in it.
func emitTail(m *Module) {
	m.Printf("-- the accessor of an enum-keyed array field (docs/PORTING.md M5): get, set and\n")
	m.Printf("-- each take the key and refuse None and any key past the enum's last variant\n")
	m.Printf("function _M.Keyed(table_name, field_name)\n")
	m.Printf("  return require(\"descriptor\").keyed(_M, table_name, field_name)\n")
	m.Printf("end\n\n")
	m.Printf("-- THE DESCRIPTORS ARE CONSTANTS (docs/PORTING.md M13): every derived fact is\n")
	m.Printf("-- built here, once, and the module's data is frozen against new keys\n")
	m.Printf("require(\"descriptor\").seal(_M)\n\n")
	m.Printf("return _M\n")
}

// unitDeclaresForms reports whether any declared table of u carries an
// explicit `forms` declaration (docs/SPEC-TABLES.md §2.12). A synthesized map
// entry inherits its owner's declaration and is not counted on its own.
func unitDeclaresForms(u *ir.Unit) bool {
	for _, st := range u.Tables {
		if !st.IsMapEntry() && st.DeclaredForms() {
			return true
		}
	}
	return false
}

// unitCarriesForm reports whether any declared table of u carries form
// (docs/SPEC-TABLES.md §2.12). With no declaration every form is carried;
// the declaration only ever restricts.
func unitCarriesForm(u *ir.Unit, form string) bool {
	declared := false
	for _, st := range u.Tables {
		if st.IsMapEntry() {
			continue
		}
		if st.DeclaredForms() {
			declared = true
		}
		if st.CarriesForm(form) {
			return true
		}
	}
	return !declared
}

// runtimeCarriesForm reports whether the named runtime file belongs to a form
// the unit carries (docs/SPEC-TABLES.md §2.12). The JSON form owns the text
// forms (§16.9), the block form owns its builder and reader (§19), and the
// cook form owns the write, read and uncook passes (§7); every other runtime
// file is part of the wire or of a surface outside the forms vocabulary.
func runtimeCarriesForm(u *ir.Unit, name string) bool {
	switch name {
	case "json.lua", "textform.lua", "textformwrite.lua":
		return unitCarriesForm(u, "json")
	case "block.lua", "blockbuild.lua":
		return unitCarriesForm(u, "block")
	case "cook.lua", "uncook.lua", "cookwrite.lua":
		return unitCarriesForm(u, "cook")
	}
	return true
}

// gateDeclaredFormsRuntime wires a table's declaration into an emitted
// runtime's entry points, so a call for a form the table leaves out is
// refused by the form's name (docs/SPEC-TABLES.md §2.12). The emitted
// runtime files themselves are untouched: a unit with no declaration keeps
// exactly today's bytes.
func gateDeclaredFormsRuntime(name string, data []byte) []byte {
	s := string(data)
	replaceOnce := func(old, repl string) {
		if !strings.Contains(s, old) {
			panic("luatable: missing forms runtime anchor in " + name)
		}
		s = strings.Replace(s, old, repl, 1)
	}
	switch name {
	case "json.lua":
		replaceOnce(
			"function J.FromJson(T, name, text, report)\n",
			"function J.FromJson(T, name, text, report)\n  local desc = T.Tables[name]\n  if desc ~= nil and desc.forms ~= nil and desc.forms.json ~= true then\n    error(\"json: table \" .. tostring(name) .. \" does not carry json (docs/SPEC-TABLES.md §2.12)\", 0)\n  end\n",
		)
		replaceOnce(
			"function J.ToJson(T, name, v)\n  local desc = T.Tables[name]\n",
			"function J.ToJson(T, name, v)\n  local desc = T.Tables[name]\n  if desc ~= nil and desc.forms ~= nil and desc.forms.json ~= true then\n    error(\"json: table \" .. tostring(name) .. \" does not carry json (docs/SPEC-TABLES.md §2.12)\", 0)\n  end\n",
		)
	case "block.lua":
		replaceOnce(
			"function B.BlockOpen(T, name, bytes, build_version, base_offset)\n",
			"function B.BlockOpen(T, name, bytes, build_version, base_offset)\n  local bd = T.Tables[name]\n  if bd ~= nil and bd.forms ~= nil and bd.forms.block ~= true then\n    error(\"block: table \" .. tostring(name) .. \" does not carry block (docs/SPEC-TABLES.md §2.12)\", 0)\n  end\n",
		)
	case "blockbuild.lua":
		replaceOnce(
			"function B.Begin(T, root, counts)\n",
			"function B.Begin(T, root, counts)\n  local bd = T.Tables[root]\n  if bd ~= nil and bd.forms ~= nil and bd.forms.block ~= true then\n    error(\"block: table \" .. tostring(root) .. \" does not carry block (docs/SPEC-TABLES.md §2.12)\", 0)\n  end\n",
		)
	case "cook.lua":
		replaceOnce(
			"function C.Open(T, name, bytes, base_offset)\n",
			"function C.Open(T, name, bytes, base_offset)\n  local cd = T.Tables[name]\n  if cd ~= nil and cd.forms ~= nil and cd.forms.cook ~= true then\n    error(\"cook: table \" .. tostring(name) .. \" does not carry cook (docs/SPEC-TABLES.md §2.12)\", 0)\n  end\n",
		)
	case "cookwrite.lua":
		replaceOnce(
			"function C.CookMeasure(T, name, v)\n",
			"function C.CookMeasure(T, name, v)\n  local cd = T.Tables[name]\n  if cd ~= nil and cd.forms ~= nil and cd.forms.cook ~= true then\n    error(\"cook: table \" .. tostring(name) .. \" does not carry cook (docs/SPEC-TABLES.md §2.12)\", 0)\n  end\n",
		)
		replaceOnce(
			"function C.Cook(T, name, v, capacity, order)\n",
			"function C.Cook(T, name, v, capacity, order)\n  local cd = T.Tables[name]\n  if cd ~= nil and cd.forms ~= nil and cd.forms.cook ~= true then\n    error(\"cook: table \" .. tostring(name) .. \" does not carry cook (docs/SPEC-TABLES.md §2.12)\", 0)\n  end\n",
		)
	}
	return []byte(s)
}

// closure is every struct the unit's tables reach — the tables themselves,
// their map entries and text-list elements — and every table-side union, each
// sorted by name so the module is deterministic (SPEC §6.1).
func closure(u *ir.Unit) ([]*ir.Struct, []*ir.Union) {
	structs := map[string]*ir.Struct{}
	unions := map[string]*ir.Union{}
	var visitField func(f *ir.Field)
	visit := func(st *ir.Struct) {
		if st == nil || structs[st.Name] != nil {
			return
		}
		structs[st.Name] = st
		for _, f := range st.Fields {
			visitField(f)
		}
	}
	visitField = func(f *ir.Field) {
		if f == nil {
			return
		}
		if f.MapEntry != nil {
			visit(f.MapEntry)
		}
		if f.Type.Kind != ir.TNamed {
			return
		}
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			visit(r)
		case *ir.Union:
			if unions[r.Name] == nil {
				unions[r.Name] = r
				for _, v := range r.Variants {
					visitField(v.F)
				}
			}
		}
	}
	for _, st := range u.Tables {
		visit(st)
	}
	for _, un := range u.TableUnions {
		if unions[un.Name] == nil {
			unions[un.Name] = un
			for _, v := range un.Variants {
				visitField(v.F)
			}
		}
	}
	ss := make([]*ir.Struct, 0, len(structs))
	for _, st := range structs {
		ss = append(ss, st)
	}
	sort.Slice(ss, func(i, j int) bool { return ss[i].Name < ss[j].Name })
	us := make([]*ir.Union, 0, len(unions))
	for _, un := range unions {
		us = append(us, un)
	}
	sort.Slice(us, func(i, j int) bool { return us[i].Name < us[j].Name })
	return ss, us
}

func emit(m *Module) {
	u := m.Unit
	m.Printf("-- Code generated by the schema compiler for package %s. DO NOT EDIT.\n", u.Package)
	m.Printf("-- SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	m.Printf("-- your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	m.Printf("-- AGPL-3.0, its output is not.\n")
	m.Printf("-- package %s — the table surface (docs/SPEC-TABLES.md; docs/PORT-LUA.md): one\n", u.Package)
	m.Printf("-- descriptor per table and per table-side union, and a constructor per table.\n")
	m.Printf("-- Field descriptors: name, key (the JSON key, §16.3), id (the wire id, §5),\n")
	m.Printf("-- ordinal, kind (the wire kind, §3), shape, then what the shape needs.\n\n")
	regionAlign := ir.TableRegionAlign(u)
	m.Printf("local _M = {}\n\n")
	m.Printf("_M.Package = %q\n", u.Package)
	m.Printf("_M.RegionAlign = %d\n", regionAlign)
	// the cook's Open needs the build version it was written for and the two
	// reserved blob type ids a directory names (§7.1, §7.4, §6.3)
	m.Printf("_M.BuildVersion = %s\n", idLit(ir.BuildVersion(u)))
	m.Printf("_M.BytesBlobTypeId = %s\n", idLit(ir.BytesTypeId))
	m.Printf("_M.StringBlobTypeId = %s\n\n", idLit(ir.StringTypeId))
	// every id this build can name (ir.TableWireIds): a retaining save tells a
	// nameable id from a retained one by it, and only a retained one takes an
	// entry of the caller's id list (docs/SPEC-TABLES.md §6.6, THE RETAINED IDS)
	m.Printf("-- every id this build can name (docs/SPEC-TABLES.md §6.6, THE RETAINED IDS)\n")
	m.Printf("_M.WireIds = {\n")
	for _, id := range ir.TableWireIds(u) {
		m.Printf("  [%s] = true,\n", idLit(id))
	}
	m.Printf("}\n\n")
	if unitDeprecatesVariant(u) {
		// the writers' §2.15 walk runs only where a variant is retired
		m.Printf("-- an enum variant is retired in place (docs/SPEC-TABLES.md §2.15)\n")
		m.Printf("_M.DeprecatedVariants = true\n\n")
	}
	m.Printf("-- the table descriptors, by name (docs/SPEC-TABLES.md §8.1)\n")
	m.Printf("_M.Tables = {}\n\n")
	variable := ir.VariableTables(u)
	for _, st := range m.Closure {
		emitDescriptor(m, st, variable[st.Name])
	}
	m.Printf("-- the table-side union descriptors, by name (docs/SPEC-TABLES.md §2.6)\n")
	m.Printf("_M.Unions = {}\n\n")
	for _, un := range m.Unions {
		emitUnionDescriptor(m, un)
	}
	for _, st := range m.Closure {
		emitConstructor(m, st)
	}
	m.Printf("-- the unit registry (docs/SPEC-TABLES.md §8.3): every table, by name\n")
	m.Printf("_M.Registry = { package = %q, region_align = %d, tables = {", u.Package, regionAlign)
	for i, st := range m.Closure {
		if i > 0 {
			m.Printf(",")
		}
		m.Printf(" %q", st.Name)
	}
	m.Printf(" } }\n\n")
}

// emitDescriptor emits one table's descriptor (docs/SPEC-TABLES.md §8.1): its
// name, its type id (§3.1), whether it is variable by declaration (§2.2), and
// its fields in declaration order. A field inside an `if` branch carries its
// branch condition as guard = { { field = <bool field>, want = <bool> }, ... },
// the conjunction composed through nesting (structGuards, re-derived here from
// the IR as the C++ emitter composes it; an emitter never imports the engine):
// a field under a false guard is elided and is not an edge (§3, §3.1).
func emitDescriptor(m *Module, st *ir.Struct, variable bool) {
	layout := ir.RecordLayout(m.Unit, st)
	regionAlign := ir.TableRegionAlign(m.Unit)
	m.Printf("_M.Tables[%q] = {\n", st.Name)
	m.Printf("  name = %q,\n", st.Name)
	m.Printf("  type_id = %s,\n", idLit(ir.TableWireId(st.WireName())))
	m.Printf("  table = %v,\n", st.IsTable)
	// A table's `forms` declaration is part of its declaration, not of one
	// generate call (docs/SPEC-TABLES.md §2.12); an undeclared table keeps the
	// old descriptor bytes, so the runtime gate above is the only change its
	// output sees.
	if st.IsTable && st.DeclaredForms() {
		m.Printf("  forms = {")
		for _, form := range st.Forms {
			m.Printf(" %s = true,", form)
		}
		m.Printf(" },\n")
	}
	m.Printf("  variable = %v,\n", variable)
	if st.Integrity != "" {
		m.Printf("  integrity = %q,\n", st.Integrity)
	}
	m.Printf("  map_entry = %v,\n", st.MapEntryOf != "")
	if st.JsonArray {
		// THE ARRAY ROOT (docs/SPEC-TABLES.md §16.10): the table's text IS
		// its one field's array, written and read with no object around it
		// and no key. A text-form fact only; it moves no wire byte.
		m.Printf("  array_root = true,\n")
	}
	m.Printf("  size = %d,\n", layout.Size)
	m.Printf("  align = %d,\n", layout.Align)
	m.Printf("  region_align = %d,\n", regionAlign)
	m.Printf("  fields = {\n")
	guards := structGuards(st)
	for i, f := range st.Fields {
		fl := layout.FieldByName(f.Name)
		var pieces []ir.BlockFieldPiece
		if fl != nil {
			pieces = ir.FieldPieces(m.Unit, f, fl.Offset)
		}
		m.Printf("    %s,\n", guardedFieldDescriptorAt(m.Unit, f, i+1, guards[f.Name], pieces))
	}
	m.Printf("  },\n")
	m.Printf("}\n\n")
}

// emitUnionDescriptor emits a table-side union (docs/SPEC-TABLES.md §2.6):
// each arm's tag (its declared position, 1-based; 0 is the empty union), its
// wire id (the hash of its name, or of its `was` name, §5), and its field
// line's descriptor, nil for a payload-free arm.
func emitUnionDescriptor(m *Module, un *ir.Union) {
	size, align, tag, armOffset := ir.UnionLayout(m.Unit, un)
	m.Printf("_M.Unions[%q] = {\n", un.Name)
	m.Printf("  name = %q,\n", un.Name)
	m.Printf("  size = %d,\n", size)
	m.Printf("  align = %d,\n", align)
	m.Printf("  tag = %d,\n", tag)
	m.Printf("  arm_offset = %d,\n", armOffset)
	m.Printf("  arms = {\n")
	slots := armSlots(un)
	for i, v := range un.Variants {
		wire := v.Name
		if v.WasName != "" {
			wire = v.WasName
		}
		field := "nil"
		if v.F != nil {
			field = fieldDescriptorAt(m.Unit, v.F, i+1, fieldPieces(m.Unit, v.F))
		}
		m.Printf("    { name = %q, slot = %q, tag = %d, id = %s, field = %s },\n", v.Name, slots[i], i+1, idLit(ir.TableWireId(wire)), field)
	}
	m.Printf("  },\n")
	m.Printf("}\n\n")
}

// armSlots answers, per arm, the key of the union VALUE that holds the arm's
// payload. A Lua union value is { tag = <arm index>, <slot> = <payload> }, so
// an arm named like the discriminator (`tag`) must not store its payload under
// the discriminator's key: its slot is the name with underscores appended
// until it names no arm (docs/SPEC-TABLES.md §2.6). Every other arm's slot is
// its name; the wire, the text and the JSON still name the arm, never the slot.
func armSlots(un *ir.Union) []string {
	taken := map[string]bool{"tag": true}
	for _, v := range un.Variants {
		taken[v.Name] = true
	}
	out := make([]string, len(un.Variants))
	for i, v := range un.Variants {
		slot := v.Name
		for slot == "tag" {
			slot += "_"
			for taken[slot] {
				slot += "_"
			}
		}
		out[i] = slot
	}
	return out
}

// guardTerm is one conjunct of a guarded field's branch condition: a bool
// field of the same body, and the value it must hold.
type guardTerm struct {
	Field string
	Want  bool
}

// structGuards maps each guarded field's name to its branch condition,
// composed through nesting exactly as the C++ emitter composes it. A field
// with no entry is unguarded and always rides.
func structGuards(st *ir.Struct) map[string][]guardTerm {
	out := map[string][]guardTerm{}
	var walk func(items []ir.Item, cond []guardTerm)
	walk = func(items []ir.Item, cond []guardTerm) {
		for _, item := range items {
			switch item := item.(type) {
			case *ir.FieldItem:
				if len(cond) > 0 {
					out[item.F.Name] = append([]guardTerm(nil), cond...)
				}
			case *ir.Branch:
				walk(item.Then, append(cond[:len(cond):len(cond)], guardTerm{item.Cond, !item.Neg}))
				walk(item.Else, append(cond[:len(cond):len(cond)], guardTerm{item.Cond, item.Neg}))
			}
		}
	}
	walk(st.Items, nil)
	return out
}

// FieldDescriptor renders one field's descriptor as a Lua table constructor.
func FieldDescriptor(f *ir.Field, ordinal int) string {
	return fieldDescriptor(nil, f, ordinal)
}

func fieldDescriptor(u *ir.Unit, f *ir.Field, ordinal int) string {
	return guardedFieldDescriptor(u, f, ordinal, nil)
}

func fieldDescriptorAt(u *ir.Unit, f *ir.Field, ordinal int, pieces []ir.BlockFieldPiece) string {
	return guardedFieldDescriptorAt(u, f, ordinal, nil, pieces)
}

// guardedFieldDescriptor is fieldDescriptor with the field's `if` branch
// condition (docs/SPEC-TABLES.md §3: a field under a false guard is elided;
// SPEC §4.5), nil for an unguarded field.
func guardedFieldDescriptor(u *ir.Unit, f *ir.Field, ordinal int, guard []guardTerm) string {
	return guardedFieldDescriptorAt(u, f, ordinal, guard, nil)
}

// guardedFieldDescriptorAt adds the layout facts a cook's check reads
// (docs/SPEC-TABLES.md §7.4): where the field's storage begins and, for a
// count companion, how far past that beginning it sits. pieces are the
// contiguous storage members ir.FieldPieces names, in order.
func guardedFieldDescriptorAt(u *ir.Unit, f *ir.Field, ordinal int, guard []guardTerm, pieces []ir.BlockFieldPiece) string {
	var b strings.Builder
	fmt.Fprintf(&b, "{ name = %q, key = %q, id = %s, ordinal = %d, kind = %d, ",
		f.Name, ir.TableFieldJsonKey(f), idLit(ir.TableFieldWireId(f)), ordinal, ir.TableWireFieldKind(f))
	if len(pieces) > 0 {
		fmt.Fprintf(&b, "at = %d, ", pieces[0].Offset)
		if needsCompanion(f) && len(pieces) > 1 {
			fmt.Fprintf(&b, "companion = %d, ", pieces[1].Offset-pieces[0].Offset)
		}
	}
	// The view's descriptor columns (docs/SPEC-TABLES.md §8.1), under the
	// names the standard gives them. They sit beside the runtime's own keys
	// below, which stay exactly where they were.
	fmt.Fprintf(&b, "type_name = %q, elem_kind = %d, elem_size = %d, counted = %v, UnboundedContainer = %v, Unbounded = %v, array_bound = %d, ",
		ir.TableTypeSpelling(f), ir.TableWireElemKind(f), descriptorElemSize(u, f), descriptorCounted(f),
		descriptorUnboundedContainer(f), f.Type.TextUnbounded(), descriptorArrayBound(f))
	if f.KeyEnum != "" {
		fmt.Fprintf(&b, "key_type_name = %q, keys = %s, ", f.KeyEnum, variants(f.KeyEnumRef))
	} else {
		b.WriteString("key_type_name = \"\", keys = {}, ")
	}
	fmt.Fprintf(&b, "optional = %v, present = %q, ", f.Type.Optional, presentCompanion(f))
	if len(guard) > 0 {
		b.WriteString("guard = {")
		for i, t := range guard {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, " { field = %q, want = %v }", t.Field, t.Want)
		}
		b.WriteString(" }, ")
	}
	if f.Required {
		b.WriteString("required = true, ")
	}
	if f.Deprecated {
		b.WriteString("deprecated = true, ")
	}
	switch {
	case f.MapEntry != nil:
		fmt.Fprintf(&b, "shape = \"map\", entry = %q, key_field = %s, value_field = %s }",
			f.MapEntry.Name, fieldDescriptorAt(u, f.MapEntry.Fields[0], 1, fieldPieces(u, f.MapEntry.Fields[0])),
			fieldDescriptorAt(u, f.MapEntry.Fields[1], 2, fieldPieces(u, f.MapEntry.Fields[1])))
	case f.Array != ir.ArrayNone:
		_, elemAlign := ir.ListElementLayout(u, f)
		fmt.Fprintf(&b, "shape = \"array\", list = %v, fixed = %v, min = %d, bound = %d, elem_align = %d, elem_step = %d, ",
			f.Array == ir.ArrayList, f.Array == ir.ArrayFixed, f.ArrayMin, f.ArrayBound, elemAlign, elementStep(u, f))
		if f.KeyEnumRef != nil {
			fmt.Fprintf(&b, "keyed = %s, ", variants(f.KeyEnumRef))
		}
		if f.TextTotal > 0 {
			fmt.Fprintf(&b, "total = %d, ", f.TextTotal)
		}
		fmt.Fprintf(&b, "elem = %s }", elemDescriptor(u, f))
	default:
		b.WriteString(valueDescriptor(f, true))
		b.WriteString(" }")
	}
	return b.String()
}

// descriptorElemSize is the pitch a walker steps for the view (§8.1): the
// element's storage size for an array or map, the scalar's wire width
// otherwise. Lua has no addressable record, so this is the element size the
// descriptor can name, not an offset into a C struct.
func descriptorElemSize(u *ir.Unit, f *ir.Field) int64 {
	switch {
	case f.IsMap():
		return ir.RecordLayout(u, f.MapEntry).Size
	case f.Array != ir.ArrayNone:
		size, _ := ir.ListElementLayout(u, f)
		return size
	case f.Type.Kind == ir.TBytes, f.Type.Kind == ir.TString:
		return 1
	case f.Type.Kind == ir.TWString:
		return 2
	}
	if f.Type.Kind == ir.TNamed {
		if r, ok := f.Type.Ref.(*ir.Struct); ok && !f.Type.Pointer {
			return ir.RecordLayout(u, r).Size
		}
	}
	return int64(ir.TableKindWidth(ir.TableWireFieldKind(f)))
}

// descriptorCounted is the §8.1 `counted` column: a fixed array has no count
// companion, every other array-shaped field (a counted array, a list, a map,
// a string or a byte buffer) does.
func descriptorCounted(f *ir.Field) bool {
	if f.IsList() || f.IsMap() {
		return true
	}
	return f.Array == ir.ArrayCounted ||
		(!f.Type.Pointer && !f.Type.TextUnbounded() &&
			(f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString))
}

// descriptorUnboundedContainer is the ONE TELL of a container with no declared
// bound (docs/SPEC-TABLES.md §8.1): a map or an unbounded array, with
// array_bound 0 beside it. It is not Unbounded, which marks unbounded text
// (§2.13.1).
func descriptorUnboundedContainer(f *ir.Field) bool {
	return f.IsList() || f.IsMap()
}

// descriptorArrayBound is the §8.1 `array_bound`: the declared storage extent,
// 0 for a map or list (the unbounded-container tell), a keyed array's key
// extent, and a string's or buffer's maximum length.
func descriptorArrayBound(f *ir.Field) int64 {
	switch {
	case f.IsMap() || f.IsList():
		return 0
	case f.KeyEnum != "" && f.KeyEnumRef != nil:
		return f.KeyEnumRef.Max
	case f.Array != ir.ArrayNone:
		return f.ArrayBound
	case f.Type.Kind == ir.TBytes, f.Type.Kind == ir.TString, f.Type.Kind == ir.TWString:
		return f.Type.Size
	}
	return 0
}

// presentCompanion is the §8.1 presence companion's name: the `<field>_present`
// bool a ?T stores, empty on a field that is not optional.
func presentCompanion(f *ir.Field) string {
	if f.Type.Optional {
		return f.Name + "_present"
	}
	return ""
}

// elemDescriptor is an array's element: the facts a field of the element's
// type carries, at its zero (§2.9), with the element-local layout a cook's
// check reads (§7.4).
func elemDescriptor(u *ir.Unit, f *ir.Field) string {
	pieces := fieldPieces(u, elementField(f))
	var b strings.Builder
	b.WriteString("{ ")
	b.WriteString(valueDescriptor(f, false))
	if f.Type.Kind == ir.TFixed || (f.Type.Kind == ir.TInt && f.Type.Width == 128) {
		// A wide element's TEXT is its exact spelling (docs/SPEC-TABLES.md
		// §16.2): a fixed/ufixed in whole units of its Q I.F, a 128-bit integer
		// as a decimal integer. The JSON reader and writer take F from
		// type_name and the wide path from kind, exactly as for a scalar field,
		// so an element carries both, or its exact fractions read as the wrong
		// shape (json-hostile row fixed-array-elements) and a 128-bit element
		// is written as a float (hostile rows conformance-uint128-*).
		fmt.Fprintf(&b, ", kind = %d, type_name = %q", ir.TableWireElemKind(f), ir.TableTypeSpelling(f))
	}
	if len(pieces) > 0 {
		fmt.Fprintf(&b, ", at = %d", pieces[0].Offset)
		if needsCompanion(elementField(f)) && len(pieces) > 1 {
			fmt.Fprintf(&b, ", companion = %d", pieces[1].Offset-pieces[0].Offset)
		}
	}
	b.WriteString(" }")
	return b.String()
}

// elementField is f as one VALUE: no array, key or optional wrapper, which is
// the storage an array element takes (docs/SPEC-TABLES.md §2.9).
func elementField(f *ir.Field) *ir.Field {
	single := *f
	single.Array = ir.ArrayNone
	single.ArrayBound = 0
	single.KeyEnum = ""
	single.KeyEnumRef = nil
	single.Type.Optional = false
	return &single
}

// elementStep is the pitch of an array's elements: the full storage of one
// element, companions included (tablecook's elementBytes).
func elementStep(u *ir.Unit, f *ir.Field) int64 {
	var end int64
	for _, p := range fieldPieces(u, elementField(f)) {
		if p.Offset+p.Size > end {
			end = p.Offset + p.Size
		}
	}
	return end
}

func fieldPieces(u *ir.Unit, f *ir.Field) []ir.BlockFieldPiece {
	if u == nil {
		return nil
	}
	return ir.FieldPieces(u, f, 0)
}

// needsCompanion reports a field whose storage carries a count companion the
// cook's check bounds (§7.4): a by-value string, bytes or wstring, and a
// counted array.
func needsCompanion(f *ir.Field) bool {
	if f.Array == ir.ArrayCounted {
		return true
	}
	if f.Array != ir.ArrayNone || f.Type.Pointer {
		return false
	}
	switch f.Type.Kind {
	case ir.TString, ir.TWString, ir.TBytes:
		return true
	}
	return false
}

// valueDescriptor is the shape-specific part of a field's or an element's
// descriptor; withDefault adds the declared default (an element has none).
func valueDescriptor(f *ir.Field, withDefault bool) string {
	t := f.Type
	switch {
	case t.Pointer && t.Blob() && t.Kind == ir.TString:
		return "shape = \"string_blob\""
	case t.Pointer && t.Blob():
		return "shape = \"bytes_blob\""
	case t.Pointer:
		return fmt.Sprintf("shape = \"pointer\", target = %q, target_id = %s", t.Name, idLit(ir.TableTypeId(ir.PointeeWireName(f))))
	}
	switch t.Kind {
	case ir.TBool:
		if withDefault {
			return fmt.Sprintf("shape = \"bool\", default = %v", f.HasDefault && f.DefBool)
		}
		return "shape = \"bool\""
	case ir.TFloat32, ir.TFloat64:
		s := fmt.Sprintf("shape = \"float\", f32 = %v", t.Kind == ir.TFloat32)
		if withDefault {
			v := 0.0
			if f.HasDefault {
				v = f.DefFloat
				if t.Kind == ir.TFloat32 {
					v = float64(float32(v))
				}
			}
			s += ", default = " + lua.FloatLiteral(v)
		}
		return s
	case ir.TString:
		if t.TextUnbounded() {
			// UNBOUNDED TEXT (docs/SPEC-TABLES.md §2.13.1): a text-list
			// element or a map key with no length bound, stored as one blob
			// slot. No `bound =` rides, so no reader clamps and no map key is
			// dropped for its length.
			return "shape = \"string\", unbounded = true"
		}
		s := fmt.Sprintf("shape = \"string\", bound = %d", t.Size)
		if withDefault {
			s += ", default = " + lua.ByteString(f.DefBytes)
		}
		return s
	case ir.TWString:
		return fmt.Sprintf("shape = \"wstring\", bound = %d", t.Size)
	case ir.TBytes:
		s := fmt.Sprintf("shape = \"bytes\", bound = %d", t.Size)
		if withDefault {
			s += ", default = " + lua.ByteString(f.DefBytes)
		}
		return s
	case ir.TNamed:
		switch r := t.Ref.(type) {
		case *ir.Enum:
			s := fmt.Sprintf("shape = \"enum\", enum = %q, variants = %s", r.Name, variants(r))
			if withDefault && f.HasDefault && f.DefVariant != "" && f.DefVariant != "None" {
				s += fmt.Sprintf(", default = %q", f.DefVariant)
			}
			return s
		case *ir.Flags:
			s := fmt.Sprintf("shape = \"flags\", flags = %q, width = 8, bits = %d, variants = %s",
				r.Name, r.WireBits, names(r.Variants))
			if withDefault {
				s += ", default = " + lua.IntLiteral(defInt(f))
			}
			return s
		case *ir.Struct:
			return fmt.Sprintf("shape = \"table\", target = %q", r.Name)
		case *ir.Union:
			return fmt.Sprintf("shape = \"union\", target = %q", r.Name)
		}
	}
	// an integer or fixed-point scalar: intN/uintN, fixed(I, F)/ufixed(I, F), bits(N), a timestamp
	kind := ir.TableWireScalarKind(f)
	if (t.Kind == ir.TInt || t.Kind == ir.TFixed) && t.Width == 128 {
		// int128/uint128 and fixed128/ufixed128 are { lo = <int>, hi = <int> },
		// two's complement (docs/PORT-LUA.md, 64-BIT AND 128-BIT INTEGERS,
		// FIXED-POINT): their declared default and range are raw halves,
		// exactly as the type wire carries them (docs/SPEC-TABLES.md §3 kinds
		// 18/19 and 24/29).
		s := fmt.Sprintf("shape = \"wide\", width = %d, signed = %v", ir.TableKindWidth(kind), t.Signed)
		if lo, hi, ok := ir.TableRawRange(f); ok {
			s += fmt.Sprintf(", lo = %s, hi = %s", lua.Int128Literal(lo), lua.Int128Literal(hi))
		}
		if withDefault {
			s += ", default = " + lua.Int128Literal(defInt(f))
		}
		return s
	}
	s := fmt.Sprintf("shape = \"int\", width = %d, signed = %v", ir.TableKindWidth(kind), (t.Kind == ir.TInt || t.Kind == ir.TFixed) && t.Signed)
	if t.Kind == ir.TBits {
		s += fmt.Sprintf(", bits = %d", t.Width)
	}
	if t.Timestamp {
		s += ", timestamp = true"
	}
	if lo, hi, ok := ir.TableRawRange(f); ok {
		s += fmt.Sprintf(", lo = %s, hi = %s", lua.IntLiteral(lo), lua.IntLiteral(hi))
	}
	if withDefault {
		s += ", default = " + lua.IntLiteral(defInt(f))
	}
	return s
}

func defInt(f *ir.Field) *big.Int {
	if f.HasDefault && f.DefInt != nil {
		return f.DefInt
	}
	return big.NewInt(0)
}

// names renders a declared vocabulary's names in bit order, the form a flags
// descriptor's JSON writer needs (docs/SPEC-TABLES.md §16.2).
func names(vs []string) string {
	parts := make([]string, len(vs))
	for i, v := range vs {
		parts[i] = fmt.Sprintf("%q", v)
	}
	return lua.TableOf(parts)
}

// variants renders an enum's variants as { name, wire id } pairs in declared
// order: identity is the variant NAME's hash (docs/SPEC-TABLES.md §5). A
// variant retired in place keeps its pair, name and id for life, and is
// marked `deprecated = true` — the Lua idiom for the §2.15 marker: the
// constant still names the variant, and the runtime reads it as the default
// and refuses it on write.
func variants(e *ir.Enum) string {
	parts := make([]string, len(e.Variants))
	for i, v := range e.Variants {
		if variantDeprecated(e, i) {
			parts[i] = fmt.Sprintf("{ %q, %s, deprecated = true }", v, idLit(ir.TableWireId(e.VariantWireName(i))))
			continue
		}
		parts[i] = fmt.Sprintf("{ %q, %s }", v, idLit(ir.TableWireId(e.VariantWireName(i))))
	}
	return lua.TableOf(parts)
}

// variantDeprecated is the variant's `| deprecated` marker
// (docs/SPEC-TABLES.md §2.15).
func variantDeprecated(e *ir.Enum, i int) bool {
	return i < len(e.VariantDeprecated) && e.VariantDeprecated[i]
}

// unitDeprecatesVariant reports whether any enum of the unit retires a
// variant in place (docs/SPEC-TABLES.md §2.15). Only such a unit's writers
// walk a value for one, so every other module is emitted as before.
func unitDeprecatesVariant(u *ir.Unit) bool {
	for _, e := range u.Enums {
		for i := range e.Variants {
			if variantDeprecated(e, i) {
				return true
			}
		}
	}
	return false
}

// idLit renders a 64-bit wire id as a wrapping hex literal: the same 64 bits
// in a Lua integer (docs/PORT-LUA.md, 64-BIT AND 128-BIT INTEGERS).
func idLit(id uint64) string { return fmt.Sprintf("0x%016x", id) }

// emitConstructor emits New<Table>(): every field at its declared default
// (SPEC.md §4.2; docs/SPEC-TABLES.md §3, §4 — the value the writer elides
// against and the value an absent field reads).
func emitConstructor(m *Module, st *ir.Struct) {
	m.Printf("function _M.New%s()\n", st.Name)
	if len(st.Fields) == 0 {
		m.Printf("  return {}\nend\n\n")
		return
	}
	m.Printf("  return {\n")
	for _, f := range st.Fields {
		v := defaultExpr(f)
		if v == "nil" {
			m.Printf("    -- %s: nil until set\n", f.Name)
		} else {
			m.Printf("    %s = %s,\n", key(f.Name), v)
		}
		if f.Type.Optional {
			m.Printf("    %s = false,\n", key(f.Name+"_present"))
		}
	}
	m.Printf("  }\nend\n\n")
}

func key(name string) string {
	if lua.IsName(name) {
		return name
	}
	return fmt.Sprintf("[%q]", name)
}

// defaultExpr is a field's declared default in Lua storage.
func defaultExpr(f *ir.Field) string {
	t := f.Type
	switch {
	case f.MapEntry != nil:
		return "{}"
	case f.Array == ir.ArrayFixed:
		return lua.TableOf(repeatList(zeroExpr(f), f.ArrayBound))
	case f.Array == ir.ArrayCounted:
		return lua.TableOf(repeatList(zeroExpr(f), f.BornCount()))
	case f.Array == ir.ArrayList:
		return "{}"
	case t.Pointer:
		return "nil"
	}
	if t.Kind == ir.TNamed {
		switch r := t.Ref.(type) {
		case *ir.Enum:
			if f.HasDefault && f.DefVariant != "" && f.DefVariant != "None" {
				return fmt.Sprintf("%q", f.DefVariant)
			}
			return "nil"
		case *ir.Struct:
			return "_M.New" + r.Name + "()"
		case *ir.Union:
			return "{ tag = 0 }"
		case *ir.Flags:
			return lua.IntLiteral(defInt(f))
		}
	}
	switch t.Kind {
	case ir.TBool:
		return fmt.Sprintf("%v", f.HasDefault && f.DefBool)
	case ir.TFloat32:
		if f.HasDefault {
			return lua.FloatLiteral(float64(float32(f.DefFloat)))
		}
		return "0.0"
	case ir.TFloat64:
		if f.HasDefault {
			return lua.FloatLiteral(f.DefFloat)
		}
		return "0.0"
	case ir.TString, ir.TBytes:
		return lua.ByteString(f.DefBytes)
	case ir.TWString:
		return "{}"
	}
	if (t.Kind == ir.TInt || t.Kind == ir.TFixed) && t.Width == 128 {
		return lua.Int128Literal(defInt(f))
	}
	return lua.IntLiteral(defInt(f))
}

// zeroExpr is an array element's zero (§2.9): elements carry no default.
func zeroExpr(f *ir.Field) string {
	t := f.Type
	if t.Pointer {
		return "false" // a sequence cannot hold nil; false is the null element
	}
	if t.Kind == ir.TNamed {
		switch r := t.Ref.(type) {
		case *ir.Enum:
			return "false" // None, in a sequence
		case *ir.Struct:
			return "_M.New" + r.Name + "()"
		case *ir.Union:
			return "{ tag = 0 }"
		}
		return "0"
	}
	switch t.Kind {
	case ir.TBool:
		return "false"
	case ir.TFloat32, ir.TFloat64:
		return "0.0"
	case ir.TString, ir.TBytes:
		return `""`
	case ir.TWString:
		return "{}"
	}
	if (t.Kind == ir.TInt || t.Kind == ir.TFixed) && t.Width == 128 {
		return "{ lo = 0, hi = 0 }"
	}
	return "0"
}

func repeatList(expr string, n int64) []string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = expr
	}
	return parts
}

// init registers the closure-type storage pass: the fixed form's per-field
// widths and a nested type's record size are emitted for every ROOT by the
// fixed form's own pass, and a root's by-value `type` fields are walked
// through descriptors whose size and field widths have to be there too
// (docs/FIXED-FORM-ALGORITHM.md §3). The wide kinds reach the fixed form
// through such a type (docs/SPEC-TABLES.md §3.4, §15): the fixed form names no
// kind, so an int128 is a sixteen-byte store and a fixed-point value its raw
// declared-width integer.
func init() { RegisterPass(emitFixedClosureTypes) }

func emitFixedClosureTypes(m *Module) error {
	if len(fixedRoots(m.Unit)) == 0 {
		return nil
	}
	for _, st := range m.Closure {
		if st.IsTable || st.IsMapEntry() {
			continue
		}
		m.Printf("_M.Tables[%q].fixed_size = %d\n", st.Name, fixedTypeBytesAt(st, 0))
		m.Printf("_M.FixedFields[%q] = {", st.Name)
		for i, f := range st.Fields {
			if i > 0 {
				m.Printf(",")
			}
			m.Printf(" { size = %d, elem = %d }", fixedFieldBytesAt(f, 0), fixedElementBytesAt(f, 0))
		}
		m.Printf(" }\n")
	}
	return nil
}
