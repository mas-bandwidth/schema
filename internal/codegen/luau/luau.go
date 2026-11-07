// Package luau emits the Luau target's TYPE SURFACE: one .luau module per
// schema file — Constants.schema -> Constants.luau — deterministic to the byte
// (SPEC §6.1), plus the runtime modules under runtime/ emitted beside them.
// docs/PORT-LUAU.md is the port's design: THE TYPE MAPPING section is the
// value each field kind stores as, THE RUNTIME LAYOUT section is where every
// later surface lands.
//
// This file is the type surface and nothing else: constants, enums (with
// Max, their only exported number: SPEC §4.2), flags, unions and a
// constructor per type that builds the value at its defaults. It has no codec. A later card joins the module
// through [Register] from its own file's init (read.go, write.go), so no
// later card edits this file.
//
// Storage is the §6.1 storage principle in Luau's value domain: a type is a
// table with every field present from construction, named by
// ir.GoExportName — the SAME PascalCase mapping as the Go, C# and JavaScript
// targets, so the checker's existing collision detection covers Luau without
// a second registry. Numbers (doubles) hold every width of 32 bits or fewer;
// Luau's native `integer` holds 64-bit values as their two's-complement bit
// pattern; a 128-bit value is the runtime's Int128 pair of integers.
package luau

import (
	"embed"
	"fmt"
	"maps"
	"math"
	"math/big"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const ext = "luau"

// RuntimeModule is the shared runtime every generated module requires: the
// integer gate, the Int128 type and the array helper (docs/PORT-LUAU.md, THE
// RUNTIME LAYOUT). The type-wire runtime's later files (bitreader.luau,
// bitwriter.luau) sit beside it under runtime/.
const RuntimeModule = "schema_runtime"

//go:embed runtime/*.luau
var runtimeFS embed.FS

// Runtime returns every runtime/*.luau file, keyed by its basename — emitted
// beside the generated modules, so a module requires its runtime as
// require("./schema_runtime") (docs/PORT-LUAU.md, THE RUNTIME LAYOUT).
func Runtime() map[string][]byte {
	out := map[string][]byte{}
	entries, err := runtimeFS.ReadDir("runtime")
	if err != nil {
		panic(err) // the embed is compiled in; a read failure is a build defect
	}
	for _, e := range entries {
		data, err := runtimeFS.ReadFile(path.Join("runtime", e.Name()))
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
// order, so the output does not depend on file order (docs/PORT-LUAU.md, THE
// RUNTIME LAYOUT).
func Register(name string, fn Part) {
	parts = append(parts, part{name, fn})
	sort.Slice(parts, func(i, j int) bool { return parts[i].name < parts[j].name })
}

// Generate returns basename.luau -> file contents for every file of the
// unit that has a type surface, plus the runtime modules. Declarations emit in
// ir.EmissionOrder per file. A file whose declarations are all tables emits no
// type-surface module: its module is <Base>Table.luau, the table surface's own
// name for the unit (docs/SPEC-TABLES.md §11).
func Generate(u *ir.Unit) (map[string][]byte, error) {
	if err := CheckBases(u, Runtime()); err != nil {
		return nil, err
	}
	if err := CheckAcyclic(u); err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	home := ir.ProtocolIdHome(u)
	for _, f := range u.Files {
		g := NewGen(u, f)
		g.emitFile(f.Base == home)
		for _, p := range parts {
			if err := p.fn(g); err != nil {
				return nil, fmt.Errorf("luau %s: %s: %w", p.name, f.Base, err)
			}
		}
		if EmitsTypeModule(f) {
			out[f.Base+"."+ext] = g.Assemble()
		}
	}
	maps.Copy(out, Runtime())
	return out, nil
}

// EmitsTypeModule reports whether the type surface writes <Base>.luau for a
// file: it does when the file declares packet-side declarations (types,
// constants, enums, flags, unions), and for a file that declares nothing at
// all (the skeleton module). A file whose declarations are all tables has no
// type surface — its module is <Base>Table.luau (docs/SPEC-TABLES.md §11).
func EmitsTypeModule(f *ir.File) bool {
	return len(f.Decls) > 0 || (len(f.Tables) == 0 && len(f.TableUnions) == 0)
}

// CheckBases refuses a unit whose emitted file names, runtime modules
// included, are equal ignoring case: macOS and Windows write one file for
// such a pair, so the refusal is on the names alone and independent of the
// host file system (docs/SPEC-TABLES.md §11). It names both names and the
// schema file or runtime module that claims each.
func CheckBases(u *ir.Unit, runtime map[string][]byte) error {
	a, b, collides := collidingNames(u, runtime, strings.EqualFold)
	if !collides {
		return nil
	}
	return fmt.Errorf("luau: %s (claimed by %s) and %s (claimed by %s) are equal ignoring case; a case-insensitive file system writes one file for the pair — rename one (docs/SPEC-TABLES.md §11)", a.name, a.owner, b.name, b.owner)
}

// A nameClaim is one emitted file name and what claims it: a schema file's
// module or a runtime module.
type nameClaim struct{ name, owner string }

// collidingNames returns the first two emitted names equal under same, with
// the injected comparison so a test can plant the exact one.
func collidingNames(u *ir.Unit, runtime map[string][]byte, same func(a, b string) bool) (nameClaim, nameClaim, bool) {
	var claims []nameClaim
	for _, f := range u.Files {
		if EmitsTypeModule(f) {
			claims = append(claims, nameClaim{f.Base + "." + ext, f.Base + ".schema"})
		}
		if emitsTableModule(u, f) {
			claims = append(claims, nameClaim{f.Base + "Table." + ext, f.Base + ".schema"})
		}
	}
	names := make([]string, 0, len(runtime))
	for name := range runtime {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		claims = append(claims, nameClaim{name, "the Luau runtime"})
	}
	sort.Slice(claims, func(i, j int) bool {
		if claims[i].name != claims[j].name {
			return claims[i].name < claims[j].name
		}
		return claims[i].owner < claims[j].owner
	})
	for i := range claims {
		for j := i + 1; j < len(claims); j++ {
			a, b := claims[i], claims[j]
			if same(a.name, b.name) || same(b.name, a.name) {
				return a, b, true
			}
		}
	}
	return nameClaim{}, nameClaim{}, false
}

// emitsTableModule reports whether the table surface writes <Base>Table.luau
// for a file: a file with a table or a table-arm union, or a file holding a
// declaration the table closure reaches (docs/SPEC-TABLES.md §11).
func emitsTableModule(u *ir.Unit, f *ir.File) bool {
	if len(f.Tables) > 0 || len(f.TableUnions) > 0 {
		return true
	}
	closure := ir.TableClosure(u)
	for _, d := range f.Decls {
		if st, ok := d.(*ir.Struct); ok && closure[st.Name] {
			return true
		}
	}
	return false
}

// CheckAcyclic refuses a unit whose files reference each other in a cycle:
// a Luau require cycle is a load error, so the port names it at generation
// instead (docs/PORT-LUAU.md, THE RUNTIME LAYOUT). Constants fold to
// literals, so only named types are require edges.
func CheckAcyclic(u *ir.Unit) error {
	var bases []string
	for _, f := range u.Files {
		bases = append(bases, f.Base)
	}
	return RequireCycle(typeDeps(u), bases, "schema files")
}

// RequireCycle refuses a require graph (module -> the modules it requires)
// with a cycle, naming it: Luau does not load a require cycle.
func RequireCycle(deps map[string]map[string]bool, order []string, what string) error {
	state := map[string]int{}
	var visit func(string, []string) error
	visit = func(b string, stack []string) error {
		switch state[b] {
		case 1:
			return fmt.Errorf("luau: the modules of %s require each other in a cycle (%s -> %s); a Luau require cycle does not load — move the shared declarations into one file", what, strings.Join(stack, " -> "), b)
		case 2:
			return nil
		}
		state[b] = 1
		var next []string
		for d := range deps[b] {
			next = append(next, d)
		}
		sort.Strings(next)
		for _, d := range next {
			if err := visit(d, append(stack, b)); err != nil {
				return err
			}
		}
		state[b] = 2
		return nil
	}
	for _, b := range order {
		if err := visit(b, nil); err != nil {
			return err
		}
	}
	return nil
}

// typeDeps is, per file, the other files its named field and arm types live
// in — the require edges of the packet modules.
func typeDeps(u *ir.Unit) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, f := range u.Files {
		set := map[string]bool{}
		add := func(name string) {
			if b, ok := u.DeclFile[name]; ok && b != f.Base {
				set[b] = true
			}
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ir.Struct:
				for _, fl := range d.Fields {
					if fl.Type.Kind == ir.TNamed {
						add(fl.Type.Name)
					}
				}
			case *ir.Union:
				for _, v := range d.Variants {
					if v.F != nil && v.F.Type.Kind == ir.TNamed {
						add(v.F.Type.Name)
					}
				}
			}
		}
		out[f.Base] = set
	}
	return out
}

// Gen is one module being written. A Part writes through it.
type Gen struct {
	Unit *ir.Unit
	File *ir.File

	body     strings.Builder
	requires map[string]bool // file bases this module requires
	// Self is the module alias a module's own declarations hang from, and
	// the require alias of another file is "_" + its base: no schema name
	// starts with an underscore, so neither can shadow a declaration.
	Self string
	// Kind names the module family in its header ("type surface",
	// "table surface").
	Kind string
	// BulkBytes is the [N]uint8 fields of the struct being emitted whose
	// bytes provably start on a byte boundary (ir.AlignedFixedByteArrays):
	// only those may ride serializeBytes, whose align would otherwise insert
	// padding the wire does not have. The codec sets it per struct.
	BulkBytes map[*ir.Field]bool
	// Suffix is the module's basename suffix: "" for the type modules,
	// "Table" for luautable's. A require of a declaration in another file
	// names that file's module with the suffix of the declaration's family.
	Suffix string
}

// NewGen starts the type-surface module of one schema file.
func NewGen(u *ir.Unit, f *ir.File) *Gen {
	return &Gen{Unit: u, File: f, requires: map[string]bool{}, Self: "_M", Kind: "type surface"}
}

// Pf writes into the module body.
func (g *Gen) Pf(format string, args ...any) {
	fmt.Fprintf(&g.body, format, args...)
}

// Require records that this module requires another module (by its
// basename, without .luau) and returns the local alias it is bound to.
func (g *Gen) Require(module string) string {
	if module == g.File.Base+g.Suffix {
		return g.Self
	}
	g.requires[module] = true
	return "_" + module
}

// Requires is the modules this module requires so far, sorted.
func (g *Gen) Requires() []string {
	out := make([]string, 0, len(g.requires))
	for m := range g.requires {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// Assemble returns the module: header, requires, body, return.
func (g *Gen) Assemble() []byte {
	body := pruneUnusedReadScaffolding(g.body.String())
	var h strings.Builder
	h.WriteString("--!strict\n")
	fmt.Fprintf(&h, "-- Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", g.File.Base)
	h.WriteString("-- SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	h.WriteString("-- your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	h.WriteString("-- AGPL-3.0, its output is not.\n")
	fmt.Fprintf(&h, "-- package %s — protocol id 0x%016x — the Luau %s (docs/PORT-LUAU.md)\n\n", g.Unit.Package, g.Unit.ProtocolId, g.Kind)
	mods := make([]string, 0, len(g.requires)+1)
	for m := range g.requires {
		mods = append(mods, m)
	}
	sort.Strings(mods)
	fmt.Fprintf(&h, "local _Rt = require(\"./%s\")\n", RuntimeModule)
	for _, m := range mods {
		fmt.Fprintf(&h, "local _%s = require(\"./%s\")\n", m, m)
	}
	fmt.Fprintf(&h, "\nlocal %s = {}\n\n", g.Self)
	h.WriteString(body)
	fmt.Fprintf(&h, "return %s\n", g.Self)
	return []byte(h.String())
}

// readScaffolding is the module-local state read.go emits into every module:
// the holder tables and the UTF-8 validator. Which of them a module uses
// depends on the schema's field kinds, so a module that reads none of a kind
// would carry an unused declaration. pruneUnusedReadScaffolding drops a
// top-level declaration whose name occurs nowhere else in the body; a
// declaration the read surface uses is left untouched.
var readScaffolding = []string{"BITS", "BIG", "BOOL", "STR", "WIDE", "utf8valid"}

func pruneUnusedReadScaffolding(body string) string {
	declared := map[string]bool{}
	for _, name := range readScaffolding {
		declared[name] = true
	}
	count := func(name string) int {
		return len(regexp.MustCompile(`\b`+regexp.QuoteMeta(name)+`\b`).FindAllStringIndex(body, -1))
	}
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		name, isFunc, ok := readScaffoldingDecl(line)
		if !ok || !declared[name] || count(name) > 1 {
			out = append(out, line)
			continue
		}
		if isFunc {
			// The function's closing end is the next line at column 0.
			for i++; i < len(lines) && lines[i] != "end"; i++ {
			}
			continue
		}
	}
	return strings.Join(out, "\n")
}

// readScaffoldingDecl reports the name a top-level local declaration binds.
func readScaffoldingDecl(line string) (name string, isFunc, ok bool) {
	if rest, found := strings.CutPrefix(line, "local function "); found {
		if at := strings.IndexByte(rest, '('); at > 0 {
			return rest[:at], true, true
		}
		return "", false, false
	}
	if rest, found := strings.CutPrefix(line, "local "); found {
		if at := strings.Index(rest, " = "); at > 0 {
			return rest[:at], false, true
		}
	}
	return "", false, false
}

func (g *Gen) emitFile(carriesProtocolId bool) {
	if carriesProtocolId {
		g.Pf("-- The unit's protocol id — the hash of its wire shape (SPEC §3.1). Two\n")
		g.Pf("-- sides at the same id speak identical bits; there is no other versioning.\n")
		g.Pf("%s.ProtocolId = %s\n\n", g.Self, IntegerLit(new(big.Int).SetUint64(g.Unit.ProtocolId)))
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

// emitConst emits a schema const (docs/PORT-LUAU.md, THE TYPE MAPPING):
// a number for a float and for an integer that is neither explicitly 64-bit
// nor past ±(2^53 − 1); an integer otherwise; an Int128 for 128-bit storage.
// Every value is folded to its literal: Luau has no constant expressions to
// keep the schema spelling in, and a fold is always exact.
func (g *Gen) emitConst(d *ir.Const) {
	g.Pf("%s", ir.DocComment(d.Doc, "", "--"))
	var lit string
	switch {
	case d.IsFloat && d.Storage == "float32":
		lit = FormatFloat32(d.Float)
	case d.IsFloat:
		lit = FormatFloat(d.Float)
	case d.Storage == "int128" || d.Storage == "uint128":
		lit = Int128Lit(d.Int)
	case d.Explicit && (d.Storage == "int64" || d.Storage == "uint64"), !fitsSafeInteger(d.Int):
		lit = IntegerLit(d.Int)
	default:
		lit = d.Int.String()
	}
	if e := d.Expr; e != nil {
		if r := ir.RenderExpr(e); r != lit && r != d.Int.String() {
			g.Pf("%s.%s = %s -- = %s\n\n", g.Self, d.Name, lit, r)
			return
		}
	}
	g.Pf("%s.%s = %s\n\n", g.Self, d.Name, lit)
}

func (g *Gen) emitEnum(d *ir.Enum) {
	g.Pf("%s", ir.DocComment(d.Doc, "", "--"))
	g.Pf("-- %s — None = 0 implicit, variants dense from 1, wire range [0, %d] (SPEC §4.2);\n", d.Name, d.Max)
	g.Pf("-- a frozen table of numbers; | max = ... headroom values are plain numbers\n")
	g.Pf("export type %s = number\n", d.Name)
	g.Pf("%s.%s = table.freeze({\n", g.Self, d.Name)
	g.Pf("\tNone = 0,\n")
	for i, v := range d.Variants {
		g.Pf("%s", ir.DocComment(d.VariantDocs[i], "\t", "--"))
		g.Pf("\t%s = %d,\n", v, i+1)
	}
	g.Pf("\tMax = %d, -- the exported extent, the enum's only number (SPEC §4.2)\n", d.Max)
	g.Pf("})\n\n")
	g.emitEnumName(d.Name, d.Variants)
}

// emitEnumName is the debug/log name of any wire value of an enum or a tag
// set; out-of-set values (wire-legal up to the declared max) name as "???".
func (g *Gen) emitEnumName(name string, members []string) {
	g.Pf("-- EnumName%s: debug/log/tooling name for any %s wire value; the names\n", name, name)
	g.Pf("-- table is the function's own upvalue, built once.\n")
	g.Pf("%s.EnumName%s = (function()\n", g.Self, name)
	g.Pf("\tlocal names = table.freeze({ [0] = \"None\"")
	for i, m := range members {
		g.Pf(", [%d] = %q", i+1, m)
	}
	g.Pf(" })\n")
	g.Pf("\treturn function(value: number): string\n")
	g.Pf("\t\treturn names[value] or \"???\"\n")
	g.Pf("\tend\n")
	g.Pf("end)()\n\n")
}

func (g *Gen) emitFlags(d *ir.Flags) {
	g.Pf("%s", ir.DocComment(d.Doc, "", "--"))
	g.Pf("-- %s — one bit per variant, consumed as masks; flags-typed fields store\n", d.Name)
	g.Pf("-- uint64 in every target, so an integer here, wire %d bits (SPEC §4.2). Masks\n", d.WireBits)
	g.Pf("-- are flat PascalCase — the Go target's spelling exactly.\n")
	g.Pf("export type %s = integer\n", d.Name)
	for i, v := range d.Variants {
		g.Pf("%s", ir.DocComment(d.VariantDocs[i], "", "--"))
		g.Pf("%s.%s%s = %s\n", g.Self, d.Name, v, IntegerLit(new(big.Int).Lsh(big.NewInt(1), uint(i))))
	}
	g.Pf("%s.%sCount = %d -- the declared variant count (SPEC §4.2)\n\n", g.Self, d.Name, len(d.Variants))
	g.Pf("-- FlagName%s: debug/log/tooling name for bit i of %s; the names table is\n", d.Name, d.Name)
	g.Pf("-- the function's own upvalue, built once.\n")
	g.Pf("%s.FlagName%s = (function()\n", g.Self, d.Name)
	g.Pf("\tlocal names = table.freeze({")
	for i, v := range d.Variants {
		if i > 0 {
			g.Pf(",")
		}
		g.Pf(" [%d] = %q", i, v)
	}
	g.Pf(" })\n")
	g.Pf("\treturn function(bit: number): string\n")
	g.Pf("\t\treturn names[bit] or \"???\"\n")
	g.Pf("\tend\n")
	g.Pf("end)()\n\n")
}

// EmitStruct emits a `type` (or, from luautable, a `table`): its exported
// Luau type and New<Name>, which builds the value at its defaults
// (docs/PORT-LUAU.md, THE TYPE MAPPING; SPEC §5).
func (g *Gen) EmitStruct(d *ir.Struct) {
	g.Pf("%s", ir.DocComment(d.Doc, "", "--"))
	g.Pf("-- %s %s\n", map[bool]string{false: "type", true: "table"}[d.IsTable], d.Name)
	g.Pf("export type %s = {\n", d.Name)
	for _, f := range d.Fields {
		guard := ""
		if f.Guard != "" && !d.IsTable {
			// the guard in the descriptors' own spelling (SPEC §4.5)
			guard = " -- " + f.Guard + " — wire branch; storage holds both sides, a read zeroes the untaken one"
		}
		for _, s := range g.Slots(f) {
			g.Pf("\t%s: %s,%s\n", s.Name, s.Type, guard)
		}
	}
	g.Pf("}\n")
	g.Pf("function %s.New%s(): %s\n", g.Self, d.Name, d.Name)
	if len(d.Fields) == 0 {
		g.Pf("\t-- empty body — presence is the payload (SPEC §4.6)\n")
	}
	g.Pf("\tlocal value: %s = {\n", d.Name)
	var after []string
	for _, f := range d.Fields {
		g.Pf("%s", ir.DocComment(f.Doc, "\t\t", "--"))
		for _, s := range g.Slots(f) {
			comment := ""
			if s.Comment != "" {
				comment = " -- " + s.Comment
			}
			g.Pf("\t\t%s = %s,%s\n", s.Name, s.Init, comment)
		}
		after = append(after, g.ByteDefault(f, "value."+ir.GoExportName(f.Name))...)
	}
	g.Pf("\t}\n")
	for _, line := range after {
		g.Pf("\t%s\n", line)
	}
	g.Pf("\treturn value\n")
	g.Pf("end\n\n")
}

// EmitUnion emits a first-class one-of (SPEC §4.8): the <Name>Type tag table,
// then the type — the tag beside one pre-allocated arm per variant — and its
// constructor, the empty union (None).
func (g *Gen) EmitUnion(d *ir.Union) {
	members := make([]string, len(d.Variants))
	for i, v := range d.Variants {
		members[i] = ir.GoExportName(v.Name)
	}
	tag := d.Name + "Type"
	g.Pf("-- %s: union %s's tag — None = 0, then each variant in declared order (SPEC §4.8)\n", tag, d.Name)
	g.Pf("export type %s = number\n", tag)
	g.Pf("%s.%s = table.freeze({\n\tNone = 0,\n", g.Self, tag)
	for i, m := range members {
		g.Pf("%s", ir.DocComment(d.Variants[i].Doc, "\t", "--"))
		g.Pf("\t%s = %d,\n", m, i+1)
	}
	g.Pf("\tMax = %d, -- the exported extent, the tag's only number (SPEC §4.2)\n", len(members))
	g.Pf("})\n\n")
	g.emitEnumName(tag, members)

	g.Pf("%s", ir.DocComment(d.Doc, "", "--"))
	g.Pf("-- %s — at most one of the arms; Type says which. Construction is the empty\n", d.Name)
	g.Pf("-- union (None); every arm is pre-allocated, and unselected arms keep what\n")
	g.Pf("-- they last held — the reused-storage discipline.\n")
	g.Pf("export type %s = {\n\tType: number,\n", d.Name)
	for _, v := range d.Variants {
		if v.Void() {
			continue // a PAYLOAD-FREE arm has no storage (SPEC §4.8)
		}
		for _, s := range g.armSlots(v) {
			g.Pf("\t%s: %s,\n", s.Name, s.Type)
		}
	}
	g.Pf("}\n")
	g.Pf("function %s.New%s(): %s\n", g.Self, d.Name, d.Name)
	g.Pf("\tlocal value: %s = {\n\t\tType = 0,\n", d.Name)
	var after []string
	for _, v := range d.Variants {
		if v.Void() {
			continue
		}
		for _, s := range g.armSlots(v) {
			g.Pf("\t\t%s = %s,\n", s.Name, s.Init)
		}
		after = append(after, g.ByteDefault(v.F, "value."+ir.GoExportName(v.Name))...)
	}
	g.Pf("\t}\n")
	for _, line := range after {
		g.Pf("\t%s\n", line)
	}
	g.Pf("\treturn value\n")
	g.Pf("end\n\n")
}

// armSlots is an arm's storage: an arm is a field line (SPEC §4.8,
// docs/SPEC-TABLES.md §2.6), stored under the arm's own name.
func (g *Gen) armSlots(v ir.UnionVariant) []Slot {
	f := *v.F
	f.Name = v.Name
	return g.Slots(&f)
}

// A Slot is one storage member a field takes: its Luau name, its type
// annotation and its construction expression.
type Slot struct {
	Name, Type, Init, Comment string
}

// Slots is a field's storage (docs/PORT-LUAU.md, THE TYPE MAPPING): the value
// slot, then the used-length or count slot a buffer or counted array carries
// beside it, then an optional field's presence bool.
func (g *Gen) Slots(f *ir.Field) []Slot {
	name := ir.GoExportName(f.Name)
	t := f.Type
	var out []Slot
	switch {
	case f.IsMap() || f.Array == ir.ArrayList:
		// an unbounded array or a map: its elements live in the holder's
		// extent on the wire; in Luau storage a plain array, born empty
		elem, _ := g.elem(f)
		if f.IsMap() {
			elem = g.TypeRef(f.MapEntry.Name, true)
		}
		out = append(out, Slot{Name: name, Type: "{" + elem + "}", Init: "{}"})
	case f.Array != ir.ArrayNone && (t.Pointer || t.TextUnbounded()):
		// an array of pointers or byte buffers: nil-filled slots
		elem, _ := g.elem(f)
		out = append(out, Slot{Name: name, Type: "{" + elem + "?}", Init: fmt.Sprintf("(table.create(%d) :: any)", f.ArrayBound), Comment: "nil is null (docs/SPEC-TABLES.md §2.4)"})
		if f.Array == ir.ArrayCounted {
			out = append(out, Slot{Name: name + "Count", Type: "number", Init: strconv.FormatInt(f.BornCount(), 10)})
		}
	case t.Pointer && t.Blob(), t.TextUnbounded():
		out = append(out, Slot{Name: name, Type: "buffer?", Init: "nil", Comment: "a byte buffer: nil is absent (docs/SPEC-TABLES.md §2.5)"})
	case t.Pointer:
		out = append(out, Slot{Name: name, Type: g.TypeRef(t.Name, true) + "?", Init: "nil", Comment: "a pointer: nil is null (docs/SPEC-TABLES.md §2.4)"})
	case t.Kind == ir.TString || t.Kind == ir.TBytes || t.Kind == ir.TWString:
		if f.Array != ir.ArrayNone {
			elemInit := bufferInit(t)
			out = append(out, Slot{Name: name, Type: "{buffer}", Init: fmt.Sprintf("_Rt.array(%d, function() return %s end)", f.ArrayBound, elemInit)})
			out = append(out, Slot{Name: name + "Length", Type: "{number}", Init: fmt.Sprintf("table.create(%d, 0)", f.ArrayBound)})
			break
		}
		out = append(out, Slot{Name: name, Type: "buffer", Init: bufferInit(t), Comment: fmt.Sprintf("%s: max length, used length beside it (SPEC §4.7)", typeWord(t))})
		out = append(out, Slot{Name: name + "Length", Type: "number", Init: "0"})
	case f.Array != ir.ArrayNone:
		elem, init := g.elem(f)
		bound := f.ArrayBound
		switch {
		case isByteElem(t):
			// uint8 arrays store as a buffer — the bulk path's own type
			out = append(out, Slot{Name: name, Type: "buffer", Init: fmt.Sprintf("buffer.create(%d)", bound), Comment: "[N]uint8: a buffer, 0-based"})
		case isFresh(t):
			out = append(out, Slot{Name: name, Type: "{" + elem + "}", Init: fmt.Sprintf("_Rt.array(%d, function() return %s end)", bound, init)})
		default:
			out = append(out, Slot{Name: name, Type: "{" + elem + "}", Init: fmt.Sprintf("table.create(%d, %s)", bound, init)})
		}
		if f.Array == ir.ArrayCounted {
			// a [A..B] count is born at A (SPEC §4.6)
			out = append(out, Slot{Name: name + "Count", Type: "number", Init: strconv.FormatInt(f.BornCount(), 10)})
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
		out = append(out, Slot{Name: ir.GoExportName(f.Name + "_present"), Type: "boolean", Init: "false", Comment: "?T presence (docs/SPEC-TABLES.md §2.3)"})
	}
	return out
}

func typeWord(t ir.FieldType) string {
	switch t.Kind {
	case ir.TString:
		return fmt.Sprintf("string(%d)", t.Size)
	case ir.TWString:
		return fmt.Sprintf("wstring(%d), UTF-16 code units", t.Size)
	}
	return fmt.Sprintf("bytes(%d)", t.Size)
}

// bufferInit is a string/bytes/wstring value's storage: a buffer of its
// maximum size (a wstring's is two bytes a code unit).
func bufferInit(t ir.FieldType) string {
	if t.Kind == ir.TWString {
		return fmt.Sprintf("buffer.create(%d)", 2*t.Size)
	}
	return fmt.Sprintf("buffer.create(%d)", t.Size)
}

// ByteDefault is the statements that write a string/bytes field's specified
// default into its fresh buffer (SPEC §4.2).
func (g *Gen) ByteDefault(f *ir.Field, slot string) []string {
	if !f.HasDefault || len(f.DefBytes) == 0 || f.Array != ir.ArrayNone {
		return nil
	}
	var b strings.Builder
	for _, c := range f.DefBytes {
		fmt.Fprintf(&b, "\\x%02x", c)
	}
	return []string{
		fmt.Sprintf("buffer.writestring(%s, 0, \"%s\")", slot, b.String()),
		fmt.Sprintf("%sLength = %d", slot, len(f.DefBytes)),
	}
}

// isFresh reports an element type that is a table built per element.
func isFresh(t ir.FieldType) bool {
	if t.Kind == ir.TNamed {
		switch t.Ref.(type) {
		case *ir.Struct, *ir.Union:
			return true
		}
	}
	return t.Kind == ir.TInt && t.Width == 128 || t.Kind == ir.TFixed && t.Width == 128
}

func isByteElem(t ir.FieldType) bool {
	return t.Kind == ir.TInt && !t.Signed && t.Width == 8
}

// elem is an array element's type and its zero construction.
func (g *Gen) elem(f *ir.Field) (typ, init string) {
	e := *f
	e.Array = ir.ArrayNone
	e.HasDefault = false
	e.Type.Optional = false
	if f.Type.Pointer && f.Type.Blob() || f.Type.TextUnbounded() {
		return "buffer", "nil"
	}
	if f.Type.Pointer {
		return g.TypeRef(f.Type.Name, true), "nil"
	}
	return g.scalar(&e)
}

// scalar is a non-array field's type and its construction (the default when
// one is specified, else the §5 zero form).
func (g *Gen) scalar(f *ir.Field) (typ, init string) {
	t := f.Type
	switch t.Kind {
	case ir.TBool:
		return "boolean", strconv.FormatBool(f.HasDefault && f.DefBool)
	case ir.TFloat32:
		if f.HasDefault {
			return "number", FormatFloat32(f.DefFloat)
		}
		return "number", "0"
	case ir.TFloat64:
		if f.HasDefault {
			return "number", FormatFloat(f.DefFloat)
		}
		return "number", "0"
	case ir.TInt, ir.TBits, ir.TFixed:
		v := new(big.Int)
		if f.HasDefault && f.DefInt != nil {
			v = f.DefInt
		}
		switch {
		case t.Width > 64:
			return "_Rt.Int128", Int128Lit(v)
		case t.Width > 32:
			return "integer", IntegerLit(v)
		}
		return "number", v.String()
	case ir.TNamed:
		switch r := t.Ref.(type) {
		case *ir.Enum:
			v := 0
			for i, variant := range r.Variants {
				if f.HasDefault && variant == f.DefVariant {
					v = i + 1
				}
			}
			return g.TypeRef(t.Name, false), strconv.Itoa(v)
		case *ir.Flags:
			v := new(big.Int)
			if f.HasDefault && f.DefInt != nil {
				v = f.DefInt
			}
			return g.TypeRef(t.Name, false), IntegerLit(v)
		case *ir.Struct:
			return g.TypeRef(t.Name, r.IsTable), g.ctor(t.Name, r.IsTable)
		case *ir.Union:
			table := g.Unit.TableUnions[t.Name] != nil
			return g.TypeRef(t.Name, table), g.ctor(t.Name, table)
		}
	}
	return "any", "nil"
}

// TypeRef is the Luau spelling of a named declaration's type from this
// module: bare in its own module, through the require alias otherwise. table
// selects the table-surface module family (luautable's <Base>Table.luau).
func (g *Gen) TypeRef(name string, table bool) string {
	mod := g.moduleOf(name, table)
	if mod == g.File.Base+g.Suffix {
		return name
	}
	return g.Require(mod) + "." + name
}

func (g *Gen) ctor(name string, table bool) string {
	mod := g.moduleOf(name, table)
	if mod == g.File.Base+g.Suffix {
		return g.Self + ".New" + name + "()"
	}
	return g.Require(mod) + ".New" + name + "()"
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
	return g.File.Base + g.Suffix
}

var maxSafeInteger = big.NewInt(1<<53 - 1)
var minSafeInteger = big.NewInt(-(1<<53 - 1))

func fitsSafeInteger(v *big.Int) bool {
	return v != nil && v.Cmp(minSafeInteger) >= 0 && v.Cmp(maxSafeInteger) <= 0
}

var two64 = new(big.Int).Lsh(big.NewInt(1), 64)
var mask64 = new(big.Int).Sub(two64, big.NewInt(1))

// IntegerLit renders a 64-bit value as a Luau integer literal
// (docs/PORT-LUAU.md, 64-BIT AND 128-BIT INTEGERS): decimal while it fits
// int64 above its minimum, else the two's-complement bit pattern in hex —
// the integer type holds bits, and uint64 past 2^63 is the same 64 bits.
func IntegerLit(v *big.Int) string {
	if v.IsInt64() && v.Int64() > math.MinInt64 {
		return v.String() + "i"
	}
	bits := new(big.Int).And(v, mask64)
	return fmt.Sprintf("0x%016xi", bits)
}

// Int128Lit renders a 128-bit value as the runtime's Int128 pair: the low
// and high 64-bit halves of its two's-complement bits.
func Int128Lit(v *big.Int) string {
	m128 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1))
	bits := new(big.Int).And(v, m128)
	lo := new(big.Int).And(bits, mask64)
	hi := new(big.Int).Rsh(bits, 64)
	return fmt.Sprintf("_Rt.int128(0x%016xi, 0x%016xi)", lo, hi)
}

// FormatFloat renders a float64 literal: the shortest exact form.
func FormatFloat(v float64) string {
	switch {
	case math.IsInf(v, 1):
		return "math.huge"
	case math.IsInf(v, -1):
		return "-math.huge"
	case math.IsNaN(v):
		return "(0 / 0)"
	}
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eEn") {
		s += ".0"
	}
	return s
}

// FormatFloat32 renders a float32 value as the Luau literal of float32(v)
// held as a double (f32 ⊂ f64): the shortest form that parses AS A DOUBLE to
// exactly float32(v), so the stored value is already the rounded one, as
// Go's float32(v) computes it (docs/PORT-LUAU.md, BYTE BUFFERS). The
// shortest float32 form ("0.1") would parse in Luau to the unrounded double.
func FormatFloat32(v float64) string {
	return FormatFloat(float64(float32(v)))
}
