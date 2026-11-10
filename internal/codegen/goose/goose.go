// Package goose emits the Goose target's TYPE SURFACE: one .goose module per
// schema file — Constants.schema -> Constants.goose — deterministic to the
// byte (SPEC §6.1), plus the runtime modules under runtime/ emitted beside
// them. docs/PORT-GOOSE.md is the port's design: THE TYPE MAPPING section is
// the value each field kind stores as, THE NUMBER MODEL section the integer
// and float rules, THE RUNTIME LAYOUT section where every later surface lands.
//
// This file is the type surface and nothing else: constants, enums (with
// Max, and no Count: SPEC §4.2), flags, unions and a constructor per type that builds the
// value at its defaults. It has no codec. A later card joins the module
// through [Register] from its own file's init (read.go, write.go), so no
// later card edits this file.
//
// Goose's value model is C's (fixed structs, inline arrays, no heap), so the
// storage is the Go and C targets' field for field: a type is a Goose struct
// whose every field exists from construction (the storage principle, SPEC
// §6.1), named by ir.GoExportName — the SAME PascalCase mapping as the Go, C#
// and JavaScript targets, so the checker's collision claims cover Goose. The
// defaults live in the struct declaration itself (`F: T = default`), so
// `T { .. }` IS the constructor, and New<Name>() returns it.
package goose

import (
	"embed"
	"fmt"
	"maps"
	"math"
	"math/big"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const ext = "goose"

// RuntimeModule is the shared runtime every generated module imports: the
// Int128 lanes (docs/PORT-GOOSE.md, THE RUNTIME LAYOUT). It declares into
// the namespace RuntimeNamespace.
const RuntimeModule = "schema_rt"

// RuntimeNamespace is the runtime's own namespace: no schema declaration can
// collide with a runtime one.
const RuntimeNamespace = "schema_rt"

//go:embed runtime/*.goose
var runtimeFS embed.FS

// Runtime returns every runtime/*.goose file, keyed by its basename —
// emitted beside the generated modules, so a module imports its runtime as
// `import .schema_rt;`, relative to the importing file (docs/PORT-GOOSE.md,
// THE RUNTIME LAYOUT).
func Runtime() map[string][]byte {
	return readRuntime(runtimeFS)
}

func readRuntime(fs embed.FS) map[string][]byte {
	out := map[string][]byte{}
	entries, err := fs.ReadDir("runtime")
	if err != nil {
		panic(err) // the embed is compiled in; a read failure is a build defect
	}
	for _, e := range entries {
		data, err := fs.ReadFile(path.Join("runtime", e.Name()))
		if err != nil {
			panic(err)
		}
		out[e.Name()] = data
	}
	return out
}

// A Part is a later surface's contribution to one generated module (the
// codec's read.go and write.go): it is called once per schema file, after the
// type surface, and writes into the module body through g.
type Part func(g *Gen) error

type part struct {
	name string
	fn   Part
}

var parts []part

// Register joins a later surface to every module this package emits. It is
// called from an init() in that surface's own file; the parts run in name
// order, so the output does not depend on file order (docs/PORT-GOOSE.md, THE
// RUNTIME LAYOUT).
func Register(name string, fn Part) {
	parts = append(parts, part{name, fn})
	sort.Slice(parts, func(i, j int) bool { return parts[i].name < parts[j].name })
}

// Generate returns basename.goose -> file contents for every file of the
// unit, plus the runtime modules. Declarations emit in ir.EmissionOrder per
// file.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	if err := CheckUnit(u, Runtime()); err != nil {
		return nil, err
	}
	names := map[string]string{}
	out := map[string][]byte{}
	home := ir.ProtocolIdHome(u)
	for _, f := range u.Files {
		g := NewGen(u, f, names)
		g.emitFile(f.Base == home)
		for _, p := range parts {
			if err := p.fn(g); err != nil {
				return nil, fmt.Errorf("goose %s: %s: %w", p.name, f.Base, err)
			}
		}
		if g.err != nil {
			return nil, g.err
		}
		out[f.Base+"."+ext] = g.Assemble()
	}
	maps.Copy(out, Runtime())
	return out, nil
}

// DeclaredNames is every top-level name the type surface declares into the
// unit's namespace, keyed by name, valued by the module that declares it. The
// table surface declares into the same namespace and checks its own names
// against these (docs/PORT-GOOSE.md, THE RUNTIME LAYOUT).
func DeclaredNames(u *ir.Unit) (map[string]string, error) {
	if err := CheckUnit(u, Runtime()); err != nil {
		return nil, err
	}
	names := map[string]string{}
	home := ir.ProtocolIdHome(u)
	for _, f := range u.Files {
		g := NewGen(u, f, names)
		g.emitFile(f.Base == home)
		if g.err != nil {
			return nil, g.err
		}
	}
	return names, nil
}

// Keywords is Goose's reserved words and its scalar type names (the pinned
// v2026.2 goose_spec.md): a unit whose package — the namespace every module
// declares into — is one of them is refused by name.
var Keywords = map[string]bool{
	"as": true, "block": true, "break": true, "const": true, "continue": true,
	"else": true, "enum": true, "export": true, "extern": true, "false": true,
	"fn": true, "for": true, "from": true, "guard": true, "if": true,
	"import": true, "in": true, "inline": true, "let": true, "loop": true,
	"match": true, "namespace": true, "null": true, "pad": true,
	"recursive": true, "return": true, "reusable": true, "self": true,
	"simd": true, "struct": true, "thread_fn": true, "true": true, "type": true,
	"var": true, "varint": true, "while": true,
	"bool": true, "i8": true, "i16": true, "i32": true, "i64": true,
	"u8": true, "u16": true, "u32": true, "u64": true, "f32": true, "f64": true,
	"std": true, "main": true,
}

// Namespace is the Goose namespace every module of the unit declares into:
// the schema package, as every file of a unit shares one Go package
// (docs/PORT-GOOSE.md, THE RUNTIME LAYOUT).
func Namespace(u *ir.Unit) string { return u.Package }

// CheckUnit refuses, by name, a unit the Goose surface cannot emit: a package
// that is no Goose identifier, a Goose keyword or the runtime's namespace,
// and a schema file whose module would collide with a runtime module emitted
// beside it — folding case, because the filesystems the port runs on
// (Windows, macOS) do (docs/PORT-GOOSE.md, THE RUNTIME LAYOUT).
func CheckUnit(u *ir.Unit, runtime map[string][]byte) error {
	ns := Namespace(u)
	if !isIdent(ns) {
		return fmt.Errorf("goose: package %q is no Goose identifier, and it is the namespace every module declares into; rename the package", ns)
	}
	if Keywords[ns] || ns == RuntimeNamespace {
		return fmt.Errorf("goose: package %q is reserved in Goose (a keyword, a scalar type, std, main or the runtime's namespace %s), and it is the namespace every module declares into; rename the package", ns, RuntimeNamespace)
	}
	for _, f := range u.Files {
		for name := range runtime {
			if strings.EqualFold(f.Base+"."+ext, name) {
				return fmt.Errorf("schema file %s.schema collides with the Goose runtime module %s the target emits beside the generated modules; rename the file", f.Base, name)
			}
		}
	}
	return nil
}

func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// Gen is one module being written. A Part writes through it.
type Gen struct {
	Unit *ir.Unit
	File *ir.File

	body    strings.Builder
	imports map[string]bool // modules this module imports, without .goose
	names   map[string]string
	err     error
	// Kind names the module family in its header ("type surface",
	// "table surface").
	Kind string
	// Suffix is the module's basename suffix: "" for the type modules,
	// "Table" for goosetable's.
	Suffix string
	// ForPackage marks a module the unit owns rather than one schema file:
	// its banner names the package, and File is a stand-in carrying only the
	// module's base (docs/PORTING.md J2: unit-wide data lives at a home named
	// for the package, never in whichever file sorts first).
	ForPackage bool
}

// NewGen starts the type-surface module of one schema file. names is the
// unit's namespace so far (name -> declaring module), shared by every module
// of the unit so a name declared twice is refused by name.
func NewGen(u *ir.Unit, f *ir.File, names map[string]string) *Gen {
	return &Gen{Unit: u, File: f, imports: map[string]bool{RuntimeModule: true}, names: names, Kind: "type surface"}
}

// Module is this module's basename without .goose.
func (g *Gen) Module() string { return g.File.Base + g.Suffix }

// Pf writes into the module body.
func (g *Gen) Pf(format string, args ...any) {
	fmt.Fprintf(&g.body, format, args...)
}

// Import records that this module imports another module of the unit (by
// its basename, without .goose). Goose imports load a file once and allow
// cycles, so no import order is refused.
func (g *Gen) Import(module string) {
	if module != g.Module() {
		g.imports[module] = true
	}
}

// Imports is the modules this module imports, sorted.
func (g *Gen) Imports() []string {
	out := make([]string, 0, len(g.imports))
	for m := range g.imports {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// Declare records a top-level name this module declares into the unit's
// namespace; a name declared twice (an enum's EnumName<E> beside a type
// spelled the same, a descriptor name a type already holds) refuses the unit
// by name (docs/PORT-GOOSE.md, THE TYPE MAPPING).
func (g *Gen) Declare(name string) {
	if prev, ok := g.names[name]; ok && g.err == nil {
		g.err = fmt.Errorf("goose: %s declares %s in namespace %s, which %s already declares; rename the schema declaration", g.Module(), name, Namespace(g.Unit), prev)
		return
	}
	g.names[name] = g.Module()
}

// Declared reports whether name is already declared in the unit's
// namespace.
func (g *Gen) Declared(name string) bool {
	_, ok := g.names[name]
	return ok
}

// DeclaredBySurface reports whether the target already declared name for
// itself — a derived name, not one of the schema's own declarations: the test
// an emitter that writes a shared name once per unit asks before writing it
// again. A name the schema declares is the schema's, and declaring it again
// refuses the unit by name.
func (g *Gen) DeclaredBySurface(name string) bool {
	if _, ok := g.names[name]; !ok {
		return false
	}
	if _, ok := g.Unit.DeclFile[name]; ok {
		return false
	}
	if _, ok := g.Unit.Tables[name]; ok {
		return false
	}
	_, ok := g.Unit.TableUnions[name]
	return !ok
}

// Err is the first refusal a Declare recorded.
func (g *Gen) Err() error { return g.err }

// Empty reports whether nothing has been written into the module body.
func (g *Gen) Empty() bool { return g.body.Len() == 0 }

// Assemble returns the module: header, namespace, imports, body.
func (g *Gen) Assemble() []byte {
	var h strings.Builder
	if g.ForPackage {
		fmt.Fprintf(&h, "// Code generated by the schema compiler for package %s. DO NOT EDIT.\n", g.Unit.Package)
	} else {
		fmt.Fprintf(&h, "// Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", g.File.Base)
	}
	h.WriteString("// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	h.WriteString("// your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	h.WriteString("// AGPL-3.0, its output is not.\n")
	fmt.Fprintf(&h, "// package %s — protocol id 0x%016x — the Goose %s (docs/PORT-GOOSE.md)\n\n", g.Unit.Package, g.Unit.ProtocolId, g.Kind)
	fmt.Fprintf(&h, "namespace %s;\n\n", Namespace(g.Unit))
	for _, m := range g.Imports() {
		fmt.Fprintf(&h, "import .%s;\n", m)
	}
	h.WriteString("\n")
	h.WriteString(g.body.String())
	return []byte(h.String())
}

func (g *Gen) emitFile(carriesProtocolId bool) {
	if carriesProtocolId {
		g.Declare("ProtocolId")
		g.Pf("// The unit's protocol id — the hash of its wire shape (SPEC §3.1). Two\n")
		g.Pf("// sides at the same id speak identical bits; there is no other versioning.\n")
		g.Pf("let ProtocolId: u64 = 0x%016x;\n\n", g.Unit.ProtocolId)
	}
	for _, d := range ir.EmissionOrder(g.File) {
		switch d := d.(type) {
		case *ir.Const:
			g.emitConst(d)
		case *ir.Enum:
			g.emitEnum(d)
		case *ir.Flags:
			g.emitFlags(d)
		case *ir.Struct:
			g.EmitStruct(d)
		case *ir.Union:
			g.EmitUnion(d)
		}
	}
}

// emitConst emits a schema const as a typed global `let` at its storage
// (SPEC §4.2; docs/PORT-GOOSE.md, THE NUMBER MODEL). Every value is folded to
// its literal, with the schema spelling kept as a trailing comment where it
// differs: a fold is always exact.
func (g *Gen) emitConst(d *ir.Const) {
	g.Declare(d.Name)
	g.Pf("%s", ir.DocComment(d.Doc, "", "//"))
	var typ, lit string
	switch {
	case d.IsFloat && d.Storage == "float32":
		typ, lit = "f32", FormatFloat32(d.Float)
	case d.IsFloat:
		typ, lit = "f64", FormatFloat(d.Float)
	case d.Storage == "int128" || d.Storage == "uint128":
		typ, lit = RuntimeNamespace+"::Int128", Int128Lit(d.Int)
	default:
		signed, width := constInt(d)
		typ, lit = IntType(signed, width), IntLit(d.Int)
	}
	if e := d.Expr; e != nil {
		if r := ir.RenderExpr(e); r != lit && r != d.Int.String() {
			g.Pf("let %s: %s = %s; // = %s\n\n", d.Name, typ, lit, r)
			return
		}
	}
	g.Pf("let %s: %s = %s;\n\n", d.Name, typ, lit)
}

// constInt is a const's integer storage: its declared storage, else int64, or
// uint64 for a value past int64's maximum.
func constInt(d *ir.Const) (signed bool, width int) {
	switch d.Storage {
	case "int8":
		return true, 8
	case "int16":
		return true, 16
	case "int32":
		return true, 32
	case "uint8":
		return false, 8
	case "uint16":
		return false, 16
	case "uint32":
		return false, 32
	case "uint64":
		return false, 64
	}
	if d.Int != nil && d.Int.Sign() > 0 && !d.Int.IsInt64() {
		return false, 64
	}
	return true, 64
}

func (g *Gen) emitEnum(d *ir.Enum) {
	typ := IntType(false, ir.StorageBitsFor(d.Max))
	g.Pf("%s", ir.DocComment(d.Doc, "", "//"))
	g.Pf("// %s — None = 0 implicit, variants dense from 1, wire range [0, %d] (SPEC §4.2).\n", d.Name, d.Max)
	g.Pf("// Its storage is an integer sized to the declared max, so every wire-legal\n")
	g.Pf("// value fits; the variants are typed constants, the Go target's spelling.\n")
	g.Declare(d.Name)
	g.Pf("type %s = %s;\n", d.Name, typ)
	g.Declare(d.Name + "None")
	g.Pf("let %sNone: %s = 0;\n", d.Name, d.Name)
	for i, v := range d.Variants {
		g.Pf("%s", ir.DocComment(d.VariantDocs[i], "", "//"))
		g.Declare(d.Name + v)
		g.Pf("let %s%s: %s = %d;\n", d.Name, v, d.Name, i+1)
	}
	g.Declare(d.Name + "Max")
	g.Pf("let %sMax: %s = %d; // the exported extent (SPEC §4.2)\n\n", d.Name, d.Name, d.Max)
	g.emitEnumName(d.Name, d.Variants)
}

// emitEnumName is the debug/log name of any wire value of an enum or a tag
// set; out-of-set values (wire-legal up to the declared max) name as "???".
func (g *Gen) emitEnumName(name string, members []string) {
	g.Declare("EnumName" + name)
	g.Pf("// EnumName%s: debug/log/tooling name for any %s wire value\n", name, name)
	g.Pf("fn EnumName%s(value: %s) -> const u8[:] {\n", name, name)
	g.Pf("    match value {\n        0 => \"None\",\n")
	for i, m := range members {
		g.Pf("        %d => %q,\n", i+1, m)
	}
	g.Pf("        _ => \"???\",\n    }\n}\n\n")
}

func (g *Gen) emitFlags(d *ir.Flags) {
	g.Pf("%s", ir.DocComment(d.Doc, "", "//"))
	g.Pf("// %s — one bit per variant, consumed as masks; flags-typed fields store\n", d.Name)
	g.Pf("// u64 in every target, wire %d bits (SPEC §4.2). Masks are flat PascalCase —\n", d.WireBits)
	g.Pf("// the Go target's spelling exactly.\n")
	g.Declare(d.Name)
	g.Pf("type %s = u64;\n", d.Name)
	for i, v := range d.Variants {
		g.Pf("%s", ir.DocComment(d.VariantDocs[i], "", "//"))
		g.Declare(d.Name + v)
		g.Pf("let %s%s: %s = 0x%x;\n", d.Name, v, d.Name, uint64(1)<<uint(i))
	}
	g.Declare(d.Name + "Count")
	g.Pf("let %sCount: i64 = %d; // the declared variant count (SPEC §4.2)\n\n", d.Name, len(d.Variants))
	g.Declare("FlagName" + d.Name)
	g.Pf("// FlagName%s: debug/log/tooling name for bit i of %s\n", d.Name, d.Name)
	g.Pf("fn FlagName%s(bit: i64) -> const u8[:] {\n    match bit {\n", d.Name)
	for i, v := range d.Variants {
		g.Pf("        %d => %q,\n", i, v)
	}
	g.Pf("        _ => \"???\",\n    }\n}\n\n")
}

// EmitStruct emits a `type` (or, from goosetable, a `table`): a Goose struct
// whose fields carry their defaults, and New<Name>, which builds the value at
// them (docs/PORT-GOOSE.md, THE TYPE MAPPING; SPEC §5, §6.1).
func (g *Gen) EmitStruct(d *ir.Struct) {
	g.Pf("%s", ir.DocComment(d.Doc, "", "//"))
	g.Pf("// %s %s\n", map[bool]string{false: "type", true: "table"}[d.IsTable], d.Name)
	g.Declare(d.Name)
	g.Pf("struct %s {\n", d.Name)
	if len(d.Fields) == 0 {
		g.Pf("    // empty body — presence is the payload (SPEC §4.6)\n")
	}
	prevGuard := ""
	for _, f := range d.Fields {
		if f.Guard != prevGuard {
			if f.Guard != "" {
				g.Pf("    // %s — wire branch; storage holds both sides, a read zeroes the\n", f.Guard)
				g.Pf("    // untaken side (SPEC §5)\n")
			}
			prevGuard = f.Guard
		}
		g.Pf("%s", ir.DocComment(f.Doc, "    ", "//"))
		for _, s := range g.Slots(f) {
			g.emitSlot(s)
		}
	}
	g.Pf("}\n")
	g.emitCtor(d.Name)
}

func (g *Gen) emitSlot(s Slot) {
	comment := ""
	if s.Comment != "" {
		comment = " // " + s.Comment
	}
	if s.Init != "" {
		g.Pf("    %s: %s = %s,%s\n", s.Name, s.Type, s.Init, comment)
		return
	}
	g.Pf("    %s: %s,%s\n", s.Name, s.Type, comment)
}

// emitCtor is New<Name>: the struct literal that takes every declared field
// default, and every other field at its type's own default — zero, a nested
// type at ITS defaults, an empty slice, null (SPEC §5).
func (g *Gen) emitCtor(name string) {
	g.Declare("New" + name)
	g.Pf("fn New%s() -> %s { %s { .. } }\n\n", name, name, name)
}

// EmitUnion emits a first-class one-of (SPEC §4.8): the <Name>Type tag and
// its constants, then the struct — the tag beside one pre-allocated arm per
// payload-carrying variant — and its constructor, the empty union (None).
// A union is NOT a Goose enum: a Goose enum's value is one variant's payload,
// and a schema union keeps every arm's storage, unselected arms holding what
// they last held — the reused-storage discipline every target shares.
func (g *Gen) EmitUnion(d *ir.Union) {
	members := make([]string, len(d.Variants))
	for i, v := range d.Variants {
		members[i] = ir.GoExportName(v.Name)
	}
	tag := d.Name + "Type"
	g.Pf("// %s: union %s's tag — None = 0, then each variant in declared order (SPEC §4.8)\n", tag, d.Name)
	g.Declare(tag)
	g.Pf("type %s = %s;\n", tag, IntType(false, ir.StorageBitsFor(int64(len(members)))))
	g.Declare(tag + "None")
	g.Pf("let %sNone: %s = 0;\n", tag, tag)
	for i, m := range members {
		g.Pf("%s", ir.DocComment(d.Variants[i].Doc, "", "//"))
		g.Declare(tag + m)
		g.Pf("let %s%s: %s = %d;\n", tag, m, tag, i+1)
	}
	g.Declare(tag + "Max")
	g.Pf("let %sMax: %s = %d; // the exported extent (SPEC §4.2)\n\n", tag, tag, len(members))
	g.emitEnumName(tag, members)

	g.Pf("%s", ir.DocComment(d.Doc, "", "//"))
	g.Pf("// %s — at most one of the arms; Type says which. Construction is the empty\n", d.Name)
	g.Pf("// union (None); every arm is pre-allocated, and unselected arms keep what\n")
	g.Pf("// they last held — the reused-storage discipline.\n")
	g.Declare(d.Name)
	g.Pf("struct %s {\n    Type: %s,\n", d.Name, tag)
	for _, v := range d.Variants {
		if v.Void() {
			continue // a PAYLOAD-FREE arm has no storage (SPEC §4.8)
		}
		for _, s := range g.armSlots(v) {
			g.emitSlot(s)
		}
	}
	g.Pf("}\n")
	g.emitCtor(d.Name)
}

// armSlots is an arm's storage: an arm is a field line (SPEC §4.8,
// docs/SPEC-TABLES.md §2.6), stored under the arm's own name.
func (g *Gen) armSlots(v ir.UnionVariant) []Slot {
	f := *v.F
	f.Name = v.Name
	return g.Slots(&f)
}

// A Slot is one storage member a field takes: its Goose name, its type and
// its declared default ("" when the type's own default is the field's).
type Slot struct {
	Name, Type, Init, Comment string
}

// Slots is a field's storage (docs/PORT-GOOSE.md, THE TYPE MAPPING): the
// value slot, then the used-length or count slot a byte buffer or counted
// array carries beside it, then an optional field's presence bool.
func (g *Gen) Slots(f *ir.Field) []Slot {
	name := ir.GoExportName(f.Name)
	t := f.Type
	var out []Slot
	switch {
	case f.IsMap() || f.Array == ir.ArrayList:
		// an unbounded array or a map (docs/SPEC-TABLES.md §2.8, §2.9): a
		// slice whose storage the load (or the builder) owns, born empty
		elem := g.elemType(f)
		if f.IsMap() {
			elem = g.TypeRef(f.MapEntry.Name, true)
		}
		out = append(out, Slot{Name: name, Type: elem + "[:]", Comment: "storage owned by the load or the builder (docs/SPEC-TABLES.md §2.9)"})
	case f.Array != ir.ArrayNone && (t.Pointer || t.TextUnbounded()):
		// an array of pointers or of unbounded text: null / empty slots
		out = append(out, Slot{Name: name, Type: fmt.Sprintf("%s[%d]", g.elemType(f), f.ArrayBound), Comment: "null is null (docs/SPEC-TABLES.md §2.4)"})
		if f.Array == ir.ArrayCounted {
			out = append(out, countSlot(name, f))
		}
	case t.Pointer && t.Blob(), t.TextUnbounded():
		out = append(out, Slot{Name: name, Type: "const u8[:]?", Comment: "a view of the blob's bytes: null is absent (docs/SPEC-TABLES.md §2.5)"})
	case t.Pointer:
		out = append(out, Slot{Name: name, Type: g.TypeRef(t.Name, true) + "?", Comment: "a pointer: null is null (docs/SPEC-TABLES.md §2.4)"})
	case t.Kind == ir.TString || t.Kind == ir.TBytes:
		unit, size := "u8", t.Size
		if f.Array != ir.ArrayNone {
			out = append(out, Slot{Name: name, Type: fmt.Sprintf("%s[%d][%d]", unit, size, f.ArrayBound)})
			out = append(out, Slot{Name: name + "Length", Type: fmt.Sprintf("i32[%d]", f.ArrayBound)})
			if f.Array == ir.ArrayCounted {
				out = append(out, countSlot(name, f))
			}
			break
		}
		init, n := g.byteDefault(f)
		out = append(out, Slot{Name: name, Type: fmt.Sprintf("%s[%d]", unit, size), Init: init, Comment: fmt.Sprintf("%s: max length, used length beside it (SPEC §4.7)", typeWord(t))})
		lenInit := ""
		if n > 0 {
			lenInit = strconv.Itoa(n)
		}
		out = append(out, Slot{Name: name + "Length", Type: "i32", Init: lenInit})
	case f.Array != ir.ArrayNone:
		typ, init := g.scalar(f)
		if init != "" {
			init = fmt.Sprintf("[%s; %d]", init, f.ArrayBound)
		}
		out = append(out, Slot{Name: name, Type: fmt.Sprintf("%s[%d]", typ, f.ArrayBound), Init: init})
		if f.Array == ir.ArrayCounted {
			out = append(out, countSlot(name, f))
		}
	default:
		typ, init := g.scalar(f)
		comment := ""
		if f.HasDefault {
			comment = "specified default (SPEC §5)"
		}
		out = append(out, Slot{Name: name, Type: typ, Init: init, Comment: comment})
	}
	if t.Optional {
		out = append(out, Slot{Name: ir.GoExportName(f.Name + "_present"), Type: "bool", Comment: "?T presence (docs/SPEC-TABLES.md §2.3)"})
	}
	return out
}

// countSlot is a counted array's used count, born at A for [A..B] (SPEC
// §4.6), an i32 as in the Go target.
func countSlot(name string, f *ir.Field) Slot {
	init := ""
	if n := f.BornCount(); n > 0 {
		init = strconv.FormatInt(n, 10)
	}
	return Slot{Name: name + "Count", Type: "i32", Init: init, Comment: fmt.Sprintf("used count; wire count in [%d, %d]", f.ArrayMin, f.ArrayBound)}
}

func typeWord(t ir.FieldType) string {
	switch t.Kind {
	case ir.TString:
		return fmt.Sprintf("string(%d)", t.Size)
	}
	return fmt.Sprintf("bytes(%d)", t.Size)
}

// byteDefault is a string/bytes field's specified default as the full array
// literal and its used length (SPEC §4.2).
func (g *Gen) byteDefault(f *ir.Field) (string, int) {
	if !f.HasDefault || len(f.DefBytes) == 0 {
		return "", 0
	}
	var units []int
	for _, c := range f.DefBytes {
		units = append(units, int(c))
	}
	parts := make([]string, f.Type.Size)
	for i := range parts {
		parts[i] = "0"
		if i < len(units) {
			parts[i] = strconv.Itoa(units[i])
		}
	}
	return "[" + strings.Join(parts, ", ") + "]", len(units)
}

// elemType is an array element's Goose type.
func (g *Gen) elemType(f *ir.Field) string {
	e := *f
	e.Array = ir.ArrayNone
	e.HasDefault = false
	e.Type.Optional = false
	if f.Type.Pointer && f.Type.Blob() || f.Type.TextUnbounded() {
		return "const u8[:]?"
	}
	if f.Type.Pointer {
		return g.TypeRef(f.Type.Name, true) + "?"
	}
	typ, _ := g.scalar(&e)
	return typ
}

// IntType is the Goose integer that stores a value of the given signedness
// and width: the narrowest of 8, 16, 32 and 64 bits that holds it
// (docs/PORT-GOOSE.md, THE NUMBER MODEL). Past 64 bits it is the runtime's
// Int128.
func IntType(signed bool, width int) string {
	w := 64
	switch {
	case width > 64:
		return RuntimeNamespace + "::Int128"
	case width <= 8:
		w = 8
	case width <= 16:
		w = 16
	case width <= 32:
		w = 32
	}
	if signed {
		return "i" + strconv.Itoa(w)
	}
	return "u" + strconv.Itoa(w)
}

// scalar is a non-array field's Goose type and its declared default ("" when
// the type's own default — zero, or a nested type at its defaults — is the
// field's; SPEC §5).
func (g *Gen) scalar(f *ir.Field) (typ, init string) {
	t := f.Type
	switch t.Kind {
	case ir.TBool:
		if f.HasDefault && f.DefBool {
			return "bool", "true"
		}
		return "bool", ""
	case ir.TFloat32:
		if f.HasDefault && f.DefFloat != 0 || f.HasDefault && math.Signbit(f.DefFloat) {
			return "f32", FormatFloat32(f.DefFloat)
		}
		return "f32", ""
	case ir.TFloat64:
		if f.HasDefault && f.DefFloat != 0 || f.HasDefault && math.Signbit(f.DefFloat) {
			return "f64", FormatFloat(f.DefFloat)
		}
		return "f64", ""
	case ir.TInt, ir.TBits, ir.TFixed:
		signed := t.Signed && t.Kind != ir.TBits
		width := t.Width
		if t.Kind == ir.TBits {
			width = 32
			if t.Width > 32 {
				width = 64
			}
		}
		typ := IntType(signed, width)
		if !f.HasDefault || f.DefInt == nil || f.DefInt.Sign() == 0 {
			return typ, ""
		}
		if width > 64 {
			return typ, Int128Lit(f.DefInt)
		}
		return typ, IntLit(f.DefInt)
	case ir.TNamed:
		switch r := t.Ref.(type) {
		case *ir.Enum:
			if f.HasDefault && f.DefVariant != "" {
				return g.TypeRef(t.Name, false), r.Name + f.DefVariant
			}
			return g.TypeRef(t.Name, false), ""
		case *ir.Flags:
			if f.HasDefault && f.DefInt != nil && f.DefInt.Sign() != 0 {
				return g.TypeRef(t.Name, false), IntLit(f.DefInt)
			}
			return g.TypeRef(t.Name, false), ""
		case *ir.Struct:
			return g.TypeRef(t.Name, r.IsTable), ""
		case *ir.Union:
			return g.TypeRef(t.Name, g.Unit.TableUnions[t.Name] != nil), ""
		}
	}
	return "u8", ""
}

// TypeRef is the Goose spelling of a named declaration's type from this
// module: every module of the unit declares into one namespace, so the name
// is bare, and the module that declares it is imported. table selects the
// table-surface module family (goosetable's <Base>Table.goose).
func (g *Gen) TypeRef(name string, table bool) string {
	g.Import(g.moduleOf(name, table))
	return name
}

// moduleOf is the module a declaration lives in.
func (g *Gen) moduleOf(name string, table bool) string {
	if table {
		for _, f := range g.Unit.Files {
			for _, t := range f.Tables {
				if t.Name == name {
					return f.Base + "Table"
				}
			}
			for _, un := range f.TableUnions {
				if un.Name == name {
					return f.Base + "Table"
				}
			}
		}
	}
	if b, ok := g.Unit.DeclFile[name]; ok {
		return b
	}
	return g.Module()
}

var two64 = new(big.Int).Lsh(big.NewInt(1), 64)
var mask64 = new(big.Int).Sub(two64, big.NewInt(1))

// IntLit renders an integer of 64 bits or fewer as a Goose literal
// (docs/PORT-GOOSE.md, THE NUMBER MODEL): decimal, which Goose reads for the
// full u64 range; int64's minimum, whose magnitude is no i64, is spelled as a
// constant expression.
func IntLit(v *big.Int) string {
	if v.IsInt64() && v.Int64() == math.MinInt64 {
		return "(-9223372036854775807 - 1)"
	}
	if v.Sign() < 0 {
		return "(" + v.String() + ")"
	}
	return v.String()
}

// Int128Lit renders a 128-bit value as the runtime's Int128: the low and high
// 64-bit lanes of its two's-complement bits.
func Int128Lit(v *big.Int) string {
	m128 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1))
	bits := new(big.Int).And(v, m128)
	lo := new(big.Int).And(bits, mask64)
	hi := new(big.Int).Rsh(bits, 64)
	return fmt.Sprintf("%s::Int128 { lo: 0x%016x, hi: 0x%016x }", RuntimeNamespace, lo, hi)
}

// FormatFloat renders a float64 literal: the shortest exact form, with
// infinities and NaN as the constant divisions Goose folds to them.
func FormatFloat(v float64) string {
	switch {
	case math.IsInf(v, 1):
		return "(1.0 / 0.0)"
	case math.IsInf(v, -1):
		return "(-1.0 / 0.0)"
	case math.IsNaN(v):
		return "(0.0 / 0.0)"
	}
	s := strconv.FormatFloat(v, 'g', -1, 64)
	s = strings.Replace(s, "e+", "e", 1)
	if !strings.ContainsAny(s, ".e") {
		s += ".0"
	}
	if v < 0 || math.Signbit(v) {
		return "(" + s + ")"
	}
	return s
}

// FormatFloat32 renders a float32 value as the literal of float32(v), the
// value Go's float32(v) computes (round to nearest, ties to even): the
// shortest decimal of that value held as a double, which every reader —
// a double parse then a float conversion, or a direct float parse — maps to
// exactly float32(v), so no double rounding can move it (docs/PORT-GOOSE.md,
// FLOAT32).
func FormatFloat32(v float64) string {
	return FormatFloat(float64(float32(v)))
}
