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
// the unit declares tables, and nothing when it does not. A unit carrying the
// fixed-point or 128-bit table kinds is refused by name (docs/SPEC-TABLES.md
// §15, schema#366): the descriptors have no kind to name them by yet.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	if len(u.Tables) == 0 {
		return map[string][]byte{}, nil
	}
	if err := ir.RefuseWideTableKinds(u, "Lua"); err != nil {
		return nil, err
	}
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
	m.Body.WriteString("return _M\n")
	out := map[string][]byte{name + ".lua": []byte(m.Body.String())}
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
		out[base] = data
	}
	return out, nil
}

// closure is every struct the unit's tables reach — the tables themselves,
// their map entries and text-list elements — and every table-side union, each
// sorted by name so the module is deterministic (SPEC §6.1).
func closure(u *ir.Unit) ([]*ir.Struct, []*ir.Union) {
	structs := map[string]*ir.Struct{}
	unions := map[string]*ir.Union{}
	var visitField func(f *ir.Field)
	var visit func(st *ir.Struct)
	visit = func(st *ir.Struct) {
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
	m.Printf("local _M = {}\n\n")
	m.Printf("_M.Package = %q\n\n", u.Package)
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
	m.Printf("_M.Registry = { package = %q, tables = {", u.Package)
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
// its fields in declaration order.
func emitDescriptor(m *Module, st *ir.Struct, variable bool) {
	m.Printf("_M.Tables[%q] = {\n", st.Name)
	m.Printf("  name = %q,\n", st.Name)
	m.Printf("  type_id = %s,\n", idLit(ir.TableWireId(st.WireName())))
	m.Printf("  table = %v,\n", st.IsTable)
	m.Printf("  variable = %v,\n", variable)
	m.Printf("  map_entry = %v,\n", st.MapEntryOf != "")
	m.Printf("  fields = {\n")
	for i, f := range st.Fields {
		m.Printf("    %s,\n", FieldDescriptor(f, i+1))
	}
	m.Printf("  },\n")
	m.Printf("}\n\n")
}

// emitUnionDescriptor emits a table-side union (docs/SPEC-TABLES.md §2.6):
// each arm's tag (its declared position, 1-based; 0 is the empty union), its
// wire id (the hash of its name, or of its `was` name, §5), and its field
// line's descriptor, nil for a payload-free arm.
func emitUnionDescriptor(m *Module, un *ir.Union) {
	m.Printf("_M.Unions[%q] = {\n", un.Name)
	m.Printf("  name = %q,\n", un.Name)
	m.Printf("  arms = {\n")
	for i, v := range un.Variants {
		wire := v.Name
		if v.WasName != "" {
			wire = v.WasName
		}
		field := "nil"
		if v.F != nil {
			field = FieldDescriptor(v.F, i+1)
		}
		m.Printf("    { name = %q, tag = %d, id = %s, field = %s },\n", v.Name, i+1, idLit(ir.TableWireId(wire)), field)
	}
	m.Printf("  },\n")
	m.Printf("}\n\n")
}

// FieldDescriptor renders one field's descriptor as a Lua table constructor.
func FieldDescriptor(f *ir.Field, ordinal int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "{ name = %q, key = %q, id = %s, ordinal = %d, kind = %d, ",
		f.Name, ir.TableFieldJsonKey(f), idLit(ir.TableFieldWireId(f)), ordinal, ir.TableWireFieldKind(f))
	if f.Type.Optional {
		b.WriteString("optional = true, ")
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
			f.MapEntry.Name, FieldDescriptor(f.MapEntry.Fields[0], 1), FieldDescriptor(f.MapEntry.Fields[1], 2))
	case f.Array != ir.ArrayNone:
		fmt.Fprintf(&b, "shape = \"array\", list = %v, fixed = %v, min = %d, bound = %d, elem_kind = %d, ",
			f.Array == ir.ArrayList, f.Array == ir.ArrayFixed, f.ArrayMin, f.ArrayBound, ir.TableWireElemKind(f))
		if f.KeyEnumRef != nil {
			fmt.Fprintf(&b, "keyed = %s, ", variants(f.KeyEnumRef))
		}
		if f.TextTotal > 0 {
			fmt.Fprintf(&b, "total = %d, ", f.TextTotal)
		}
		fmt.Fprintf(&b, "elem = %s }", elemDescriptor(f))
	default:
		b.WriteString(valueDescriptor(f, true))
		b.WriteString(" }")
	}
	return b.String()
}

// elemDescriptor is an array's element: the facts a field of the element's
// type carries, at its zero (§2.9).
func elemDescriptor(f *ir.Field) string {
	return "{ " + valueDescriptor(f, false) + " }"
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
		return fmt.Sprintf("shape = \"pointer\", target = %q", t.Name)
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
			s := fmt.Sprintf("shape = \"flags\", flags = %q, width = 8, bits = %d", r.Name, r.WireBits)
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
	// an integer: intN/uintN, bits(N), a timestamp
	s := fmt.Sprintf("shape = \"int\", width = %d, signed = %v", ir.TableKindWidth(ir.TableWireScalarKind(f)), t.Kind == ir.TInt && t.Signed)
	if t.Kind == ir.TBits {
		s += fmt.Sprintf(", bits = %d", t.Width)
	}
	if t.Timestamp {
		s += ", timestamp = true"
	}
	if f.HasIntRange && f.IntMin != nil && f.IntMax != nil {
		s += fmt.Sprintf(", lo = %s, hi = %s", lua.IntLiteral(f.IntMin), lua.IntLiteral(f.IntMax))
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

// variants renders an enum's variants as { name, wire id } pairs in declared
// order: identity is the variant NAME's hash (docs/SPEC-TABLES.md §5).
func variants(e *ir.Enum) string {
	parts := make([]string, len(e.Variants))
	for i, v := range e.Variants {
		parts[i] = fmt.Sprintf("{ %q, %s }", v, idLit(ir.TableWireId(e.VariantWireName(i))))
	}
	return "{ " + strings.Join(parts, ", ") + " }"
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
		return "{ " + repeat(zeroExpr(f), f.ArrayBound) + " }"
	case f.Array == ir.ArrayCounted:
		return "{ " + repeat(zeroExpr(f), f.BornCount()) + " }"
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
	return "0"
}

func repeat(expr string, n int64) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = expr
	}
	return strings.Join(parts, ", ")
}
