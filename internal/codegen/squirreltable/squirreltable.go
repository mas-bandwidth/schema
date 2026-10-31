// Package squirreltable emits the Squirrel target's TABLE surface
// (docs/SPEC-TABLES.md): one module per unit that declares tables,
// <Package>Tables.nut, holding a DESCRIPTOR per table of the closure — the
// table as data one runtime walks, as internal/codegen/jstable's variable
// runtime walks its codec descriptors — and a constructor per table, with the
// runtime under runtime/ emitted beside it as schema_table_<name>.nut.
// docs/PORT-SQUIRREL.md is the port's design.
//
// THIS FILE CARRIES NO CODEC. The forms join the module through
// [RegisterSurface] from their own files' init (fixedform.go, messageform.go)
// and through runtime files of their own (load.nut, save.nut, json.nut,
// text.nut, ...), so no later card edits this file (docs/PORT-SQUIRREL.md, THE
// CARD MAP).
package squirreltable

import (
	"embed"
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// RuntimePrefix is what every table runtime file is emitted under:
// runtime/load.nut is schema_table_load.nut beside the modules.
const RuntimePrefix = "schema_table_"

// runtimeFS is the table runtime, every runtime/*.nut by glob, one file per
// later card.
//
//go:embed runtime/*.nut
var runtimeFS embed.FS

// Runtime returns the table surface's runtime files, keyed by emitted name.
func Runtime() map[string][]byte {
	return squirrel.RuntimeFiles(runtimeFS, "runtime", RuntimePrefix)
}

// Surface is a part of the table module a later file adds: Table runs after
// each table's descriptor and constructor, Module once at the end. Either may
// be nil.
type Surface struct {
	Name   string
	Table  func(m *Module, st *ir.Struct)
	Module func(m *Module)
}

var surfaces []Surface

// RegisterSurface adds a surface to every table module. Called from an init.
func RegisterSurface(s Surface) { surfaces = append(surfaces, s) }

// Module is the unit's table module while it is generated.
type Module struct {
	Unit *ir.Unit
	// Closure is every table and type the declared tables reach, in emission
	// order: the declared tables of each file in declaration order, then the
	// types they hold by value, in their files' order.
	Closure []*ir.Struct
	// Roots is the declared tables, the closure's members a file can be
	// rooted at (docs/SPEC-TABLES.md §3).
	Roots []*ir.Struct

	body     strings.Builder
	runtimes map[string]bool
}

// Printf appends to the module body.
func (m *Module) Printf(format string, args ...any) { fmt.Fprintf(&m.body, format, args...) }

// Runtime requires the table runtime module schema_table_<name> and returns
// the local that holds it.
func (m *Module) Runtime(name string) string {
	m.runtimes[name] = true
	return "_rt_" + name
}

// ModuleName is the base of the unit's table module: the package, capitalized,
// and Tables.
func ModuleName(u *ir.Unit) string {
	p := u.Package
	if p != "" {
		p = strings.ToUpper(p[:1]) + p[1:]
	}
	return p + "Tables"
}

// Generate returns the unit's table module and the table runtime, or nothing
// for a unit that declares no table. A construct the descriptors do not map
// yet is refused by name, never emitted wrong (docs/PORT-SQUIRREL.md, THE
// REGISTER).
func Generate(u *ir.Unit) (map[string][]byte, error) {
	if len(u.Tables) == 0 {
		return map[string][]byte{}, nil
	}
	name := ModuleName(u)
	for _, f := range u.Files {
		if f.Base == name {
			return nil, fmt.Errorf("schema file %s.schema collides with the Squirrel table module, which is %s.nut for package %s (docs/PORT-SQUIRREL.md, THE RUNTIME LAYOUT); rename the file", f.Base, name, u.Package)
		}
	}
	m := &Module{Unit: u, runtimes: map[string]bool{}}
	m.Closure, m.Roots = closure(u)
	desc := m.Runtime("descriptor")
	for _, un := range unions(m.Closure) {
		emitUnion(m, un)
	}
	for _, st := range m.Closure {
		emitClass(m, st)
		emitDescriptor(m, st)
		for _, s := range surfaces {
			if !tableCarriesSurface(u, st, s.Name) {
				continue
			}
			if s.Table != nil {
				s.Table(m, st)
			}
		}
	}
	m.Printf("// THE UNIT REGISTRY (docs/SPEC-TABLES.md §8.3): every descriptor by table name,\n")
	m.Printf("// and the declared tables a file can be rooted at.\n")
	m.Printf("_M.Package <- %s;\n", squirrel.StringLit([]byte(u.Package)))
	m.Printf("_M.Descriptors <- {\n")
	for _, st := range m.Closure {
		m.Printf("  %s = _M.%sDescriptor,\n", squirrel.TableKey(st.Name), st.Name)
	}
	m.Printf("};\n")
	roots := make([]string, len(m.Roots))
	for i, st := range m.Roots {
		roots[i] = squirrel.StringLit([]byte(st.Name))
	}
	m.Printf("_M.Roots <- [%s];\n", strings.Join(roots, ", "))
	m.Printf("%s.link(_M);\n\n", desc)
	if closureHasKeyed(m.Closure) {
		m.Printf("// THE KEYED ACCESSOR (docs/PORTING.md M5): Keyed(table, field) refuses None\n")
		m.Printf("// and any key past the enum's last variant by name, in every build.\n")
		m.Printf("_M.Keyed <- function(table, field) { return %s.keyed(_M, table, field); };\n\n", desc)
	}
	for _, s := range surfaces {
		if !unitCarriesSurface(u, s.Name) {
			continue
		}
		if s.Module != nil {
			s.Module(m)
		}
	}
	if err := refuseKeyCollisions(m.body.String(), name); err != nil {
		return nil, err
	}
	out := Runtime()
	out[name+".nut"] = assemble(m, name)
	return out, nil
}

func assemble(m *Module, name string) []byte {
	var h strings.Builder
	fmt.Fprintf(&h, "// Code generated by the schema compiler for package %s. DO NOT EDIT.\n", m.Unit.Package)
	h.WriteString("// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	h.WriteString("// your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	h.WriteString("// AGPL-3.0, its output is not.\n")
	fmt.Fprintf(&h, "// package %s — the TABLE surface (docs/SPEC-TABLES.md): one descriptor and one\n", m.Unit.Package)
	h.WriteString("// constructor per table, the descriptors data the table runtime walks.\n")
	h.WriteString("//\n")
	h.WriteString("// Load through the loader beside this file (docs/PORT-SQUIRREL.md):\n")
	fmt.Fprintf(&h, "//   local %s = dofile(dir + \"/%smodule.nut\", true).require(%q);\n\n", name, squirrel.RuntimePrefix, name)
	h.WriteString("local _M = vargv[0];\n")
	h.WriteString("local _require = vargv[1];\n")
	names := make([]string, 0, len(m.runtimes))
	for k := range m.runtimes {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		fmt.Fprintf(&h, "local _rt_%s = _require(%q);\n", k, RuntimePrefix+k)
	}
	h.WriteString("\n")
	h.WriteString(m.body.String())
	// ONE NEWLINE ENDS THE FILE (make/schema-squirrel-generated-lint-gate.mk,
	// the canonical-text rules): every declaration is written with a blank
	// line after it, so the last one's is trimmed here.
	return []byte(strings.TrimRight(h.String(), "\n") + "\n")
}

// closure is the table closure (ir.TableClosure) in emission order, and the
// declared tables among it.
func closure(u *ir.Unit) (all, roots []*ir.Struct) {
	in := ir.TableClosure(u)
	seen := map[string]bool{}
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if !seen[st.Name] {
				seen[st.Name] = true
				all = append(all, st)
				roots = append(roots, st)
			}
		}
	}
	for _, f := range u.Files {
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok && in[st.Name] && !seen[st.Name] {
				seen[st.Name] = true
				all = append(all, st)
			}
		}
	}
	return all, roots
}

// ---------------------------------------------------------------------------
// THE CONSTRUCTOR (docs/PORT-SQUIRREL.md, THE TYPE MAPPING, tables)
// ---------------------------------------------------------------------------

// emitClass is a table's storage class: every field at its DECLARED DEFAULT
// (SPEC.md §4.2), which is also the value the writer elides against and the
// value an absent field reads (docs/SPEC-TABLES.md §3, §4). Members are the
// field's ir.GoExportName, declared null in the class body and assigned by
// the constructor, because a class-body initializer is shared by every
// instance in Squirrel.
func emitClass(m *Module, st *ir.Struct) {
	m.Printf("%s", ir.DocComment(st.Doc, "", "//"))
	kind := "table"
	if !st.IsTable {
		kind = "type, held by a table"
	} else if st.FixedDeclared {
		kind = "fixed table"
	}
	m.Printf("// %s %s\n", kind, st.Name)
	m.Printf("_M.%s <- class {\n", st.Name)
	for _, f := range st.Fields {
		m.Printf("  %s = null;\n", squirrel.Member(f))
		if f.Type.Optional {
			m.Printf("  %sPresent = null;\n", squirrel.Member(f))
		}
	}
	m.Printf("  constructor() {\n")
	for _, f := range st.Fields {
		emitInit(m, f, "this."+squirrel.Member(f))
	}
	m.Printf("  }\n};\n\n")
}

// emitInit writes the constructor's initialization of one field (or one
// union arm) into target.
func emitInit(m *Module, f *ir.Field, target string) {
	switch {
	case f.Array != ir.ArrayNone:
		// an array is born at its born length (SPEC §4.6): a [N]T holds N,
		// a [A..B]T holds A, a [..N]T and a []T hold none — each element at
		// the element's zero (docs/SPEC-TABLES.md §2.9)
		n := bornLength(f)
		if n == 0 {
			m.Printf("    %s = [];\n", target)
			break
		}
		m.Printf("    %s = array(%d);\n", target, n)
		m.Printf("    for (local i = 0; i < %d; i++) %s[i] = %s;\n", n, target, elemZero(f))
	default:
		if f.IsMap() {
			// a map is born empty (docs/SPEC-TABLES.md §2.8)
			m.Printf("    %s = [];\n", target)
			break
		}
		m.Printf("    %s = %s;\n", target, valueDefault(f))
	}
	if f.Type.Optional {
		// ?T: the generated present bool beside the value, born absent
		// (docs/SPEC-TABLES.md §2.3)
		m.Printf("    %sPresent = false;\n", target)
	}
}

func bornLength(f *ir.Field) int64 {
	switch f.Array {
	case ir.ArrayFixed:
		return f.ArrayBound
	case ir.ArrayCounted:
		return f.ArrayMin
	}
	return 0
}

func tableOf(f *ir.Field) *ir.Struct {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	st, _ := f.Type.Ref.(*ir.Struct)
	return st
}

func unionOf(f *ir.Field) *ir.Union {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	un, _ := f.Type.Ref.(*ir.Union)
	return un
}

func enumOf(f *ir.Field) *ir.Enum {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	e, _ := f.Type.Ref.(*ir.Enum)
	return e
}

func isFlags(f *ir.Field) bool {
	if f.Type.Kind != ir.TNamed {
		return false
	}
	_, ok := f.Type.Ref.(*ir.Flags)
	return ok
}

// elemZero is an array element's zero in table storage.
func elemZero(f *ir.Field) string {
	switch {
	case f.Type.Pointer:
		return "null"
	case tableOf(f) != nil:
		return "_M." + tableOf(f).Name + "()"
	case unionOf(f) != nil:
		return "_M." + unionOf(f).Name + "()"
	case enumOf(f) != nil:
		return "null"
	case f.Type.Kind == ir.TString:
		return `""`
	case f.Type.Kind == ir.TBytes:
		return "blob(0)"
	case f.Type.Kind == ir.TWString:
		return "[]"
	}
	return scalarZero(f)
}

func scalarZero(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		return "false"
	case ir.TFloat32, ir.TFloat64:
		return "0.0"
	case ir.TInt, ir.TFixed, ir.TBits:
		if f.Type.Width > 64 {
			return "[0, 0]"
		}
	}
	return "0"
}

// valueDefault is a non-array field's declared default in table storage: an
// enum is its variant's NAME (null for None), because a variant's identity on
// the table wire is its name's hash (docs/SPEC-TABLES.md §5); a string is a
// Squirrel string, bytes a blob, a pointer null, a nested table its
// constructor.
func valueDefault(f *ir.Field) string {
	switch {
	case f.Type.Pointer:
		return "null"
	case tableOf(f) != nil:
		return "_M." + tableOf(f).Name + "()"
	case unionOf(f) != nil:
		return "_M." + unionOf(f).Name + "()"
	case enumOf(f) != nil:
		return enumDefault(f)
	case f.Type.Kind == ir.TString:
		return squirrel.StringLit(f.DefBytes)
	case f.Type.Kind == ir.TBytes:
		return "_rt_descriptor.blobOf(" + squirrel.StringLit(f.DefBytes) + ")"
	case f.Type.Kind == ir.TWString:
		// WIDE TEXT (SPEC §4.12) is its UTF-16 CODE UNITS, an array of
		// integers each in [0, 0xFFFF] whose length is the used length; it
		// takes no `= default`, so a fresh field is empty.
		return "[]"
	}
	return scalarDefault(f)
}

func enumDefault(f *ir.Field) string {
	if f.HasDefault && f.DefVariant != "" && f.DefVariant != "None" {
		return squirrel.StringLit([]byte(f.DefVariant))
	}
	return "null"
}

func scalarDefault(f *ir.Field) string {
	if !f.HasDefault {
		return scalarZero(f)
	}
	switch f.Type.Kind {
	case ir.TBool:
		if f.DefBool {
			return "true"
		}
		return "false"
	case ir.TFloat32:
		return squirrel.Float32Lit(f.DefFloat)
	case ir.TFloat64:
		return squirrel.Float64Lit(f.DefFloat)
	}
	if f.DefInt == nil {
		return scalarZero(f)
	}
	return intLit(f, f.DefInt)
}

func intLit(f *ir.Field, v *big.Int) string {
	if f.Type.Width > 64 {
		return squirrel.Int128Lit(v)
	}
	return squirrel.IntLit(v)
}

// ---------------------------------------------------------------------------
// THE DESCRIPTOR (docs/SPEC-TABLES.md §8.1)
// ---------------------------------------------------------------------------

// emitDescriptor emits one table's descriptor: its name, its node type id
// (docs/SPEC-TABLES.md §3.1, §5), its declared class (§2.2), its constructor,
// and a field descriptor per field in declaration order. Every field
// descriptor carries EVERY key, null where the shape has no such fact, so a
// runtime walk reads a slot and never asks whether it exists.
func emitDescriptor(m *Module, st *ir.Struct) {
	variable := ir.VariableTables(m.Unit)[st.Name]
	m.Printf("_M.%sDescriptor <- {\n", st.Name)
	m.Printf("  name = %s,\n", squirrel.StringLit([]byte(st.Name)))
	m.Printf("  typeId = %s,\n", squirrel.HexLit(ir.TableWireId(st.WireName())))
	m.Printf("  table = %v,\n", st.IsTable)
	m.Printf("  fixed = %v,\n", st.FixedDeclared)
	m.Printf("  variable = %v,\n", variable)
	// a map's generated entry counts its key's widening ONCE for the map
	// (docs/SPEC-TABLES.md §2.8), so its own body skips the per-field event
	m.Printf("  mapentry = %v,\n", st.IsMapEntry())
	// a table that declares `integrity` carries the integrity name its runtime
	// reads (§2.14): the table's declared check, null when none
	// (docs/SPEC-TABLES.md §2.14)
	if st.Integrity != "" {
		m.Printf("  integrity = %s,\n", squirrel.StringLit([]byte(st.Integrity)))
	}
	m.Printf("  make = function() { return _M.%s(); },\n", st.Name)
	m.Printf("  fields = [\n")
	for i, f := range st.Fields {
		m.Printf("    %s,\n", fieldDesc(m.Unit, f, i))
	}
	m.Printf("  ],\n")
	m.Printf("};\n\n")
}

// fieldDesc renders one field's descriptor. The keys:
//
//	name member key   the schema name, the storage member, the text key (§16.3)
//	id ordinal kind   the wire id (§5), the declaration ordinal, the wire kind (§3)
//	shape             bool int float enum flags str bytes tab union ptr sblob
//	                  bblob arr
//	bound min list counted   a string's, bytes' or array's declared bound
//	                  (§8.1), an array's declared minimum, whether it is a []T
//	                  (§2.9), whether it is a counted [..N]T or [A..B]T
//	signed width bits an integer's signedness, its kind's byte width, bits(N)'s N
//	lo hi             a declared integer range (§4), null when none
//	f32               a float32 field
//	def               the declared default in storage (null for a table or array)
//	table variants    the table a tab/ptr names (the union a union names); an
//	                  enum's [name, id] pairs
//	elem              an array's element descriptor (the same keys), else null
//	optional required deprecated   the field's markers (§2.3, §2.10)
//
// mapDesc renders a map field's descriptor (docs/SPEC-TABLES.md §2.8): shape
// "map", the key and value each at their own sub-descriptor, the value
// null (an empty map elides, §3).
func mapDesc(u *ir.Unit, f *ir.Field, ordinal int) string {
	d := map[string]string{
		"name":       squirrel.StringLit([]byte(f.Name)),
		"member":     squirrel.StringLit([]byte(squirrel.Member(f))),
		"key":        squirrel.StringLit([]byte(ir.TableFieldJsonKey(f))),
		"id":         squirrel.HexLit(ir.TableFieldWireId(f)),
		"ordinal":    fmt.Sprint(ordinal),
		"kind":       fmt.Sprint(ir.TableWireFieldKind(f)),
		"optional":   fmt.Sprint(f.Type.Optional),
		"required":   fmt.Sprint(f.Required),
		"deprecated": fmt.Sprint(f.Deprecated),
		"shape":      `"map"`,
		"bound":      "0", "min": "0", "list": "false", "counted": "false",
		"signed": "false", "width": "0", "bits": "0", "lo": "null", "hi": "null", "f32": "false",
		"def": "null", "table": "null", "variants": "null", "keyvariants": "null", "elem": "null",
		"entry": "null", "textlist": "0", "unbounded": "false", "nf": "null",
	}
	// the GENERATED entry is an ordinary table of the closure (§2.8): the
	// runtime decodes and writes an entry through its descriptor, so the key
	// and value take the ordinary field codecs and the entry rides whole.
	d["entry"] = squirrel.StringLit([]byte(f.MapEntry.Name))
	d["mapkey"] = fieldDesc(u, ir.MapKeyField(f), 0)
	d["mapvalue"] = fieldDesc(u, ir.MapValueField(f), 1)
	return render(d, mapKeys)
}

var mapKeys = []string{"name", "member", "key", "id", "ordinal", "kind", "shape", "bound", "min", "list", "counted",
	"signed", "width", "bits", "lo", "hi", "f32", "def", "table", "variants", "keyvariants", "elem",
	"entry", "mapkey", "mapvalue", "optional", "required", "deprecated", "textlist", "unbounded", "nf"}

func fieldDesc(u *ir.Unit, f *ir.Field, ordinal int) string {
	if f.IsMap() {
		return mapDesc(u, f, ordinal)
	}
	d := baseDesc(u, f)
	d["name"] = squirrel.StringLit([]byte(f.Name))
	d["member"] = squirrel.StringLit([]byte(squirrel.Member(f)))
	d["key"] = squirrel.StringLit([]byte(ir.TableFieldJsonKey(f)))
	d["id"] = squirrel.HexLit(ir.TableFieldWireId(f))
	d["ordinal"] = fmt.Sprint(ordinal)
	d["kind"] = fmt.Sprint(ir.TableWireFieldKind(f))
	d["optional"] = fmt.Sprint(f.Type.Optional)
	d["required"] = fmt.Sprint(f.Required)
	d["deprecated"] = fmt.Sprint(f.Deprecated)
	if f.Array != ir.ArrayNone {
		elem := baseDesc(u, f)
		elem["kind"] = fmt.Sprint(ir.TableWireElemKind(f))
		d["shape"] = `"arr"`
		d["bound"] = fmt.Sprint(f.ArrayBound)
		d["min"] = fmt.Sprint(f.ArrayMin)
		d["list"] = fmt.Sprint(f.Array == ir.ArrayList)
		d["counted"] = fmt.Sprint(f.Array == ir.ArrayCounted)
		d["def"] = "null"
		if f.IsTextList() {
			// A TEXT LIST's element is the generated record (§2.13): its text
			// form is a plain array of strings, 1 plain and 2 with an absent
			// `?string` element (§16.2).
			code := "1"
			if ir.TextListOptional(f) {
				code = "2"
			}
			d["textlist"] = code
			elem["textlist"] = code
		}
		d["elem"] = render(elem, elemKeys)
		if f.KeyEnumRef != nil {
			p := make([]string, len(f.KeyEnumRef.Variants))
			for i := range f.KeyEnumRef.Variants {
				p[i] = fmt.Sprintf("[%s, %s]", squirrel.StringLit([]byte(f.KeyEnumRef.Variants[i])),
					squirrel.HexLit(ir.TableWireId(f.KeyEnumRef.VariantWireName(i))))
			}
			d["keyvariants"] = "[" + strings.Join(p, ", ") + "]"
		}
	}
	return render(d, fieldKeys)
}

var elemKeys = []string{"kind", "shape", "bound", "signed", "width", "bits", "lo", "hi", "f32", "def", "table", "variants", "textlist", "nf", "qcount", "qmin", "qdelta", "qbits", "fmin", "fmax"}

var fieldKeys = []string{"name", "member", "key", "id", "ordinal", "kind", "shape", "bound", "min", "list", "counted",
	"signed", "width", "bits", "lo", "hi", "f32", "def", "table", "variants", "keyvariants", "elem",
	"optional", "required", "deprecated", "textlist", "unbounded", "nf", "qcount", "qmin", "qdelta", "qbits", "fmin", "fmax"}

// baseDesc is the facts of the field's VALUE type, which an array's element
// shares.
func baseDesc(u *ir.Unit, f *ir.Field) map[string]string {
	d := map[string]string{
		"kind": fmt.Sprint(ir.TableWireScalarKind(f)), "bound": "0", "min": "0", "list": "false", "counted": "false",
		"signed": "false", "width": "0", "bits": "0", "lo": "null", "hi": "null", "f32": "false",
		"def": "null", "table": "null", "variants": "null", "keyvariants": "null", "elem": "null",
		"entry": "null", "textlist": "0", "unbounded": "false", "nf": "null",
		"qcount": "0", "qmin": "0.0", "qdelta": "0.0", "qbits": "0",
		"fmin": "null", "fmax": "null",
	}
	if f.Type.TextUnbounded() {
		// UNBOUNDED TEXT (§2.13.1): the reader has no capacity to clamp at.
		d["unbounded"] = "true"
	}
	switch {
	case f.Type.Blob() && f.Type.Kind == ir.TString:
		d["shape"] = `"sblob"`
	case f.Type.Blob():
		d["shape"] = `"bblob"`
	case f.Type.Pointer:
		d["shape"] = `"ptr"`
		d["table"] = squirrel.StringLit([]byte(tableOf(f).Name))
	case tableOf(f) != nil:
		d["shape"] = `"tab"`
		d["table"] = squirrel.StringLit([]byte(tableOf(f).Name))
	case unionOf(f) != nil:
		d["shape"] = `"union"`
		d["table"] = squirrel.StringLit([]byte(unionOf(f).Name))
	case enumOf(f) != nil:
		d["shape"] = `"enum"`
		d["def"] = enumDefault(f)
		d["variants"] = enumVariantPairs(enumOf(f))
	case isFlags(f):
		d["shape"] = `"flags"`
		d["width"] = "8"
		d["def"] = scalarDefault(f)
	case f.Type.Kind == ir.TString:
		d["shape"] = `"str"`
		if f.Type.TextUnbounded() {
			d["bound"] = "0x7fffffff"
		} else {
			d["bound"] = fmt.Sprint(f.Type.Size)
		}
		d["def"] = squirrel.StringLit(f.DefBytes)
	case f.Type.Kind == ir.TBytes:
		d["shape"] = `"bytes"`
		d["bound"] = fmt.Sprint(f.Type.Size)
		d["def"] = squirrel.StringLit(f.DefBytes)
	case f.Type.Kind == ir.TWString:
		// WIDE TEXT, kind 33 (docs/SPEC-TABLES.md §3; SPEC §4.12): storage is
		// the UTF-16 code units, the bound is N in CODE UNITS, and the field
		// takes no default, so the default is the empty array. A runtime that
		// lands the default copies it, never shares this one.
		d["shape"] = `"wstr"`
		d["bound"] = fmt.Sprint(f.Type.Size)
		d["def"] = "[]"
	case f.Type.Kind == ir.TBool:
		d["shape"] = `"bool"`
		d["def"] = scalarDefault(f)
	case f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64:
		d["shape"] = `"float"`
		d["f32"] = fmt.Sprint(f.Type.Kind == ir.TFloat32)
		d["def"] = scalarDefault(f)
		// the <Owner>.<field> a non-finite read or write refuses as
		// (docs/SPEC-TABLES.md §3.3, non_finite_float)
		d["nf"] = squirrel.StringLit([]byte(FloatOwner(u, f)))
		// a COMPRESSED float32's message-form quantizer (docs/SPEC-TABLES.md
		// §3.3, SPEC.md §4.3): the step count, min, delta and index width
		// the message writer quantizes by; 0 steps for a float carried whole
		// a COMPRESSED float's declared range, as declared (a float64, not
		// rounded to float32): the variable and fixed reads clamp a finite
		// value past an end to it and count clamped (docs/SPEC-TABLES.md §3.3,
		// §4), exactly as the Go engine's scalarAt compares
		if f.HasFloatRange {
			d["fmin"] = squirrel.Float64Lit(f.FMin)
			d["fmax"] = squirrel.Float64Lit(f.FMax)
		}
		shape := ir.TableFieldShape(f)
		if shape.Inner != nil && (f.Array != ir.ArrayNone || f.KeyEnum != "") {
			shape = *shape.Inner
		}
		if shape.Packing == ir.TableMessageQuantized {
			if count, delta, ok := ir.TableMessageQuantization(shape); ok {
				d["qcount"] = fmt.Sprint(count)
				d["qmin"] = squirrel.Float32Lit(float64(shape.QMin))
				d["qdelta"] = squirrel.Float32Lit(float64(delta))
				d["qbits"] = fmt.Sprint(shape.Bits)
			}
		}
	default:
		d["shape"] = `"int"`
		d["signed"] = fmt.Sprint(f.Type.Signed && f.Type.Kind != ir.TBits)
		d["width"] = fmt.Sprint(ir.TableKindWidth(ir.TableWireScalarKind(f)))
		if f.Type.Kind == ir.TBits {
			d["bits"] = fmt.Sprint(f.Type.Width)
		}
		if rlo, rhi, ok := ir.TableRawRange(f); ok {
			d["lo"], d["hi"] = intLit(f, rlo), intLit(f, rhi)
		}
		d["def"] = scalarDefault(f)
	}
	return d
}

// enumVariantPairs renders an enum's [name, wire id, deprecated] triples in
// declaration order. The third slot is the variant's `| deprecated` marker
// (docs/SPEC-TABLES.md §2.15): a reader meeting a marked id yields the field's
// declared default, and a writer refuses it, both without changing the id.
func enumVariantPairs(e *ir.Enum) string {
	pairs := make([]string, len(e.Variants))
	for i := range e.Variants {
		deprecated := i < len(e.VariantDeprecated) && e.VariantDeprecated[i]
		pairs[i] = fmt.Sprintf("[%s, %s, %v]", squirrel.StringLit([]byte(e.Variants[i])),
			squirrel.HexLit(ir.TableWireId(e.VariantWireName(i))), deprecated)
	}
	return "[" + strings.Join(pairs, ", ") + "]"
}

func render(d map[string]string, keys []string) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + " = " + d[k]
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

// ---------------------------------------------------------------------------
// UNIONS (docs/SPEC-TABLES.md §2.6)
// ---------------------------------------------------------------------------

// unions is every union the closure holds, by value or as an element, and
// every union one of their arms holds, in first-reached order.
func unions(closure []*ir.Struct) []*ir.Union {
	var out []*ir.Union
	seen := map[*ir.Union]bool{}
	var visit func(f *ir.Field)
	visit = func(f *ir.Field) {
		un := unionOf(f)
		if un == nil || seen[un] {
			return
		}
		seen[un] = true
		out = append(out, un)
		for _, v := range un.Variants {
			if v.F != nil {
				visit(v.F)
			}
		}
	}
	for _, st := range closure {
		for _, f := range st.Fields {
			visit(f)
		}
	}
	return out
}

// emitUnion is a union in table storage: a class holding Type, the arm tag —
// 0 for none, then each arm in declared order, the packet surface's numbering
// (SPEC §4.8) — beside one pre-allocated member per arm with a payload, and
// its descriptor: the arms with their wire ids (docs/SPEC-TABLES.md §2.6, §5)
// and each payload's field facts.
func emitUnion(m *Module, un *ir.Union) {
	m.Printf("%s", ir.DocComment(un.Doc, "", "//"))
	m.Printf("// union %s — Type is the arm tag (0 none, then each arm in declared order)\n", un.Name)
	m.Printf("_M.%s <- class {\n", un.Name)
	m.Printf("  Type = null;\n")
	for _, v := range un.Variants {
		if !v.Void() {
			m.Printf("  %s = null;\n", ir.GoExportName(v.Name))
			if v.F.Type.Optional {
				m.Printf("  %sPresent = null;\n", ir.GoExportName(v.Name))
			}
		}
	}
	m.Printf("  constructor() {\n")
	m.Printf("    this.Type = 0;\n")
	for _, v := range un.Variants {
		if !v.Void() {
			emitInit(m, v.F, "this."+ir.GoExportName(v.Name))
		}
	}
	m.Printf("  }\n};\n\n")
	m.Printf("_M.%sDescriptor <- {\n", un.Name)
	m.Printf("  name = %s,\n", squirrel.StringLit([]byte(un.Name)))
	m.Printf("  make = function() { return _M.%s(); },\n", un.Name)
	m.Printf("  arms = [\n")
	for i, v := range un.Variants {
		payload := "null"
		if !v.Void() {
			payload = fieldDesc(m.Unit, v.F, i)
		}
		m.Printf("    { name = %s, member = %s, id = %s, tag = %d, payload = %s },\n",
			squirrel.StringLit([]byte(v.Name)), squirrel.StringLit([]byte(ir.GoExportName(v.Name))),
			squirrel.HexLit(ir.TableWireId(v.WireName())), i+1, payload)
	}
	m.Printf("  ],\n")
	m.Printf("};\n\n")
}

func init() {
	RegisterSurface(Surface{Name: "wire", Table: wireTable})
	RegisterSurface(Surface{Name: "json", Table: jsonEntries})
}

// surfaceForm maps a registered surface to the form a table must carry to
// emit it (docs/SPEC-TABLES.md §2.12). The cookwrite surface is the write half
// of the `cook` form (§7.6: "CookMeasure and Cook write the cooked file"), so
// it is gated by `cook`; retain is the wire read's opt-in (§6.6), gated by
// `wire`; every other form surface shares its name.
func surfaceForm(surface string) string {
	switch surface {
	case "cookwrite":
		return "cook"
	case "retain":
		// retention is the wire read's opt-in (§6.6): a table without the
		// wire form has nothing to retain from
		return "wire"
	}
	return surface
}

// tableCarriesSurface reports whether st carries the named surface
// (docs/SPEC-TABLES.md §2.12, §7.6). A surface outside the forms vocabulary
// (fixed, view, message) is always carried; a form surface (wire, json, block,
// cook) is skipped when st.DeclaredForms() leaves it out.
func tableCarriesSurface(u *ir.Unit, st *ir.Struct, surface string) bool {
	form := surfaceForm(surface)
	if !ir.TableForms[form] {
		return true
	}
	if st.IsTable {
		if !st.DeclaredForms() {
			return true
		}
		return st.CarriesForm(form)
	}
	return unitCarriesSurface(u, surface)
}

// unitCarriesSurface reports whether any declared table in u carries the named
// surface (docs/SPEC-TABLES.md §2.12, §7.6). With no `forms` declaration every
// surface is emitted; when any table declares `forms`, a form surface no table
// carries is omitted.
func unitCarriesSurface(u *ir.Unit, surface string) bool {
	form := surfaceForm(surface)
	if !ir.TableForms[form] {
		return true
	}
	declared := false
	for _, t := range u.Tables {
		if !t.IsMapEntry() && t.DeclaredForms() {
			declared = true
		}
	}
	if !declared {
		return true
	}
	for _, t := range u.Tables {
		if !t.IsMapEntry() && t.CarriesForm(form) {
			return true
		}
	}
	return false
}

// wireTable emits the per-table wire entry points (<T>Measure, <T>Save,
// <T>Load) for a declared table that carries the `wire` form
// (docs/SPEC-TABLES.md §2.12, §3, §3.1).
func wireTable(m *Module, st *ir.Struct) {
	if !st.IsTable {
		return
	}
	save := m.Runtime("save")
	load := m.Runtime("load")
	l := squirrel.StringLit([]byte(st.Name))
	m.Printf("_M.%sMeasure <- function(v) { return %s.measure(_M, %s, v); };\n", st.Name, save, l)
	m.Printf("_M.%sSave <- function(v) { return %s.save(_M, %s, v); };\n", st.Name, save, l)
	m.Printf("_M.%sLoad <- function(bytes, report = null) { return %s.load(_M, %s, bytes, %s.report(report)); };\n", st.Name, load, l, load)
}

// jsonEntries emits the per-table JSON entry points (<T>ToJson,
// <T>ToJsonMeasure, <T>FromJson) for a declared table that carries the `json`
// form (docs/SPEC-TABLES.md §2.12, §16).
func jsonEntries(m *Module, st *ir.Struct) {
	if !st.IsTable {
		return
	}
	js := m.Runtime("json")
	l := squirrel.StringLit([]byte(st.Name))
	m.Printf("_M.%sToJson <- function(v) { return %s.ToJson(_M, %s, v); };\n", st.Name, js, l)
	m.Printf("_M.%sToJsonMeasure <- function(v) { return %s.ToJsonMeasure(_M, %s, v); };\n", st.Name, js, l)
	m.Printf("_M.%sFromJson <- function(text, report = null) {\n", st.Name)
	m.Printf("  if (report == null) report = {};\n")
	m.Printf("  return %s.FromJson(_M, %s, text, report);\n};\n", js, l)
}

// FloatOwner names a float field <Owner>.<field> for the non_finite_float
// refusal (docs/SPEC-TABLES.md §3.3): the owner is the table, type or union
// that declares it, the name the Go oracle's tablewire.FloatOwner records,
// and an array element is named by its array field.
func FloatOwner(u *ir.Unit, f *ir.Field) string {
	if u != nil {
		for _, set := range []map[string]*ir.Struct{u.Tables, u.Structs} {
			for _, st := range set {
				if slices.Contains(st.Fields, f) {
					return st.Name + "." + f.Name
				}
			}
		}
		for _, set := range []map[string]*ir.Union{u.Unions, u.TableUnions} {
			for _, un := range set {
				for i := range un.Variants {
					if un.Variants[i].F == f {
						return un.Name + "." + f.Name
					}
				}
			}
		}
	}
	return f.Name
}

// closureHasKeyed reports whether a table or type the module emits declares
// an enum-keyed array, the field shape the Keyed accessor serves.
func closureHasKeyed(closure []*ir.Struct) bool {
	for _, st := range closure {
		for _, f := range st.Fields {
			if f.KeyEnumRef != nil {
				return true
			}
		}
	}
	return false
}

// moduleKey is one key the unit's table module assigns at column zero, the
// only place it defines a name (`_M.<key> <- ...`).
var moduleKey = regexp.MustCompile(`(?m)^_M\.([A-Za-z_]\w*) <-`)

// ModuleKeys is every key the unit's table module text assigns, in order,
// with repeats: a key two declarations land on is spelled twice.
func ModuleKeys(text string) []string {
	var keys []string
	for _, m := range moduleKey.FindAllStringSubmatch(text, -1) {
		keys = append(keys, m[1])
	}
	return keys
}

// refuseKeyCollisions refuses a unit whose table module would assign one key
// twice (docs/SPEC-TABLES.md §11; docs/PORTING.md I6). Every class, function
// and table the module defines lives on its one exports table, so a
// declaration named for a name the module already defines (`Roots`,
// `Descriptors`, `Package`, `Keyed`, ...) or for another declaration's
// generated function (`ShipSave` beside a table `Ship`) would silently replace
// it: the later assignment wins and the earlier name answers the wrong thing.
func refuseKeyCollisions(text, module string) error {
	seen := map[string]bool{}
	for _, k := range ModuleKeys(text) {
		if seen[k] {
			return fmt.Errorf("the Squirrel table module %s.nut would define %s twice: a declaration is named for a name the table module already spells (docs/SPEC-TABLES.md §11, docs/PORT-SQUIRREL.md, THE RUNTIME LAYOUT); rename the declaration", module, k)
		}
		seen[k] = true
	}
	return nil
}
