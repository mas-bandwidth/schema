// Package lobster emits the Lobster target's TYPE SURFACE: one .lobster
// module per schema file — Constants.schema -> Constants.lobster —
// deterministic to the byte (SPEC §6.1), plus the runtime modules under
// runtime/ emitted beside them. docs/PORT-LOBSTER.md is the port's design:
// THE TYPE MAPPING section is the value each field kind stores as, THE
// RUNTIME LAYOUT section is where every later surface lands.
//
// This file is the type surface and nothing else: constants, enums (with
// Count and Max), flags, unions and a constructor per type that builds the
// value at its defaults. It has no codec. A later card joins the module
// through [Register] from its own file's init (read.go, write.go), so no
// later card edits this file.
//
// Storage is the §6.1 storage principle in Lobster's value domain: a type is a
// class with every field present from construction, named by
// ir.GoExportName — the SAME PascalCase mapping as the Go, C# and JavaScript
// targets, so the checker's existing collision detection covers Lobster
// without a second registry. Lobster's one integer, `int` (signed 64-bit),
// holds every integer of 64 bits or fewer, a uint64 as its two's-complement
// bit pattern; a 128-bit value is the runtime's Int128 struct of two int
// lanes (docs/PORT-LOBSTER.md, THE NUMBER MODEL).
package lobster

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

const ext = "lobster"

// RuntimeModule is the shared runtime every generated module imports: the
// Int128 struct, the byte-buffer constructors and the number model's helpers
// (docs/PORT-LOBSTER.md, THE RUNTIME LAYOUT).
const RuntimeModule = "schema_runtime"

// RuntimeNamespace is the namespace every runtime module declares its names
// in; a generated module reaches them as schema_rt.<name>.
const RuntimeNamespace = "schema_rt"

//go:embed runtime/*.lobster
var runtimeFS embed.FS

// Runtime returns every runtime/*.lobster file, keyed by its basename —
// emitted beside the generated modules, so a module imports its runtime as
// `import .schema_runtime` (docs/PORT-LOBSTER.md, THE RUNTIME LAYOUT).
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
// order, so the output does not depend on file order (docs/PORT-LOBSTER.md,
// THE RUNTIME LAYOUT).
func Register(name string, fn Part) {
	parts = append(parts, part{name, fn})
	sort.Slice(parts, func(i, j int) bool { return parts[i].name < parts[j].name })
}

// Generate returns basename.lobster -> file contents for every file of the
// unit, plus the runtime modules. Declarations emit in ir.EmissionOrder per
// file, value declarations (constants, enums, flags) first.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	if err := CheckNames(u); err != nil {
		return nil, err
	}
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
				return nil, fmt.Errorf("lobster %s: %s: %w", p.name, f.Base, err)
			}
		}
		out[f.Base+"."+ext] = g.Assemble()
	}
	maps.Copy(out, Runtime())
	return out, nil
}

// keywords is every Lobster keyword (docs/language_reference.html, the pin's
// lexical definition) and the predeclared type names a declaration could
// shadow.
var keywords = map[string]bool{}

func init() {
	for k := range strings.FieldsSeq(`nil return class struct import int float string any void def fn is from
		program private resource enum enum_flags typeof var let pakfile switch case default
		out_of_range namespace not and or attribute if for while super constructor guard
		abstract union member member_frame static static_frame operator elif else break
		continue bool true false`) {
		keywords[k] = true
	}
}

// CheckNames refuses, by name, a unit whose package is no Lobster namespace
// name (a keyword, or the runtime's own namespace) and a declaration whose
// name is a Lobster keyword (docs/PORT-LOBSTER.md, THE RUNTIME LAYOUT).
func CheckNames(u *ir.Unit) error {
	if keywords[u.Package] || u.Package == RuntimeNamespace {
		return fmt.Errorf("lobster: package %s cannot be the Lobster namespace of the unit (it is a Lobster keyword or the runtime namespace %s); rename the package", u.Package, RuntimeNamespace)
	}
	for _, f := range u.Files {
		for _, d := range f.Decls {
			var name string
			switch d := d.(type) {
			case *ir.Const:
				name = d.Name
			case *ir.Enum:
				name = d.Name
			case *ir.Flags:
				name = d.Name
			case *ir.Struct:
				name = d.Name
			case *ir.Union:
				name = d.Name
			}
			if keywords[name] {
				return fmt.Errorf("lobster: %s.schema declares %s, a Lobster keyword; rename the declaration", f.Base, name)
			}
		}
	}
	return nil
}

// CheckBases refuses a schema file whose module name would collide with a
// runtime module emitted beside it. The comparison folds case, because the
// filesystems the port runs on (Windows, macOS) do (docs/PORT-LOBSTER.md,
// THE RUNTIME LAYOUT).
func CheckBases(u *ir.Unit, runtime map[string][]byte) error {
	for _, f := range u.Files {
		for name := range runtime {
			if strings.EqualFold(f.Base+"."+ext, name) {
				return fmt.Errorf("schema file %s.schema collides with the Lobster runtime module %s the target emits beside the generated modules; rename the file", f.Base, name)
			}
		}
	}
	return nil
}

// CheckAcyclic refuses a unit whose files reference each other in a cycle:
// Lobster merges an import at its first occurrence and skips the second, so
// an import cycle declares a type after its first use; the port names the
// cycle at generation instead (docs/PORT-LOBSTER.md, THE RUNTIME LAYOUT).
// Constants fold to literals, so only named types are import edges.
func CheckAcyclic(u *ir.Unit) error {
	var bases []string
	for _, f := range u.Files {
		bases = append(bases, f.Base)
	}
	return ImportCycle(typeDeps(u), bases, "schema files")
}

// ImportCycle refuses an import graph (module -> the modules it imports)
// with a cycle, naming it.
func ImportCycle(deps map[string]map[string]bool, order []string, what string) error {
	state := map[string]int{}
	var visit func(string, []string) error
	visit = func(b string, stack []string) error {
		switch state[b] {
		case 1:
			return fmt.Errorf("lobster: the modules of %s import each other in a cycle (%s -> %s); a Lobster import cycle declares a type after its use — move the shared declarations into one file", what, strings.Join(stack, " -> "), b)
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
// in — the import edges of the type modules.
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

	body    strings.Builder
	imports map[string]bool // module basenames this module imports
	classes []string        // classes this module defines, forward-declared
	// Kind names the module family in its header ("type surface",
	// "table surface").
	Kind string
	// Suffix is the module's basename suffix: "" for the type modules,
	// "Table" for lobstertable's.
	Suffix string
	// Runtimes is the runtime modules this module imports, in order.
	Runtimes []string
}

// NewGen starts the type-surface module of one schema file.
func NewGen(u *ir.Unit, f *ir.File) *Gen {
	return &Gen{Unit: u, File: f, imports: map[string]bool{}, Kind: "type surface", Runtimes: []string{RuntimeModule}}
}

// Pf writes into the module body.
func (g *Gen) Pf(format string, args ...any) {
	fmt.Fprintf(&g.body, format, args...)
}

// Import records that this module imports another module of the unit (by
// its basename, without .lobster). Every module of a unit declares the same
// namespace, the package, so an imported name is spelled bare.
func (g *Gen) Import(module string) {
	if module == g.File.Base+g.Suffix {
		return
	}
	g.imports[module] = true
}

// Imports is the modules this module imports so far, sorted.
func (g *Gen) Imports() []string {
	out := make([]string, 0, len(g.imports))
	for m := range g.imports {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// Assemble returns the module: header, imports, namespace, forward class
// declarations, body.
func (g *Gen) Assemble() []byte {
	var h strings.Builder
	fmt.Fprintf(&h, "// Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", g.File.Base)
	h.WriteString("// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	h.WriteString("// your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	h.WriteString("// AGPL-3.0, its output is not.\n")
	fmt.Fprintf(&h, "// package %s — protocol id 0x%016x — the Lobster %s (docs/PORT-LOBSTER.md)\n\n", g.Unit.Package, g.Unit.ProtocolId, g.Kind)
	// every import is `.`-relative: it resolves against this file's own
	// directory and never the modules directories (docs/PORT-LOBSTER.md,
	// THE RUNTIME LAYOUT)
	for _, r := range g.Runtimes {
		fmt.Fprintf(&h, "import .%s\n", r)
	}
	for _, m := range g.Imports() {
		fmt.Fprintf(&h, "import .%s\n", m)
	}
	fmt.Fprintf(&h, "\nnamespace %s\n\n", g.Unit.Package)
	if len(g.classes) > 0 {
		h.WriteString("// forward declarations: a class may name a class defined below it\n")
		for _, c := range g.classes {
			fmt.Fprintf(&h, "class %s\n", c)
		}
		h.WriteString("\n")
	}
	h.WriteString(g.body.String())
	return []byte(strings.TrimRight(h.String(), "\n") + "\n")
}

func (g *Gen) emitFile(carriesProtocolId bool) {
	if carriesProtocolId {
		g.Pf("// The unit's protocol id — the hash of its wire shape (SPEC §3.1). Two\n")
		g.Pf("// sides at the same id speak identical bits; there is no other versioning.\n")
		g.Pf("let ProtocolId = %s\n\n", IntLit(new(big.Int).SetUint64(g.Unit.ProtocolId)))
	}
	order := ir.EmissionOrder(g.File)
	// the value declarations first: a Lobster enum is declared before a
	// class names it
	for _, d := range order {
		switch d := d.(type) {
		case *ir.Const:
			g.emitConst(d)
		case *ir.Enum:
			g.emitEnum(d)
		case *ir.Flags:
			g.emitFlags(d)
		}
	}
	for _, d := range order {
		switch d := d.(type) {
		case *ir.Struct:
			g.EmitStruct(d)
		case *ir.Union:
			g.EmitUnion(d)
		}
	}
}

// emitConst emits a schema const (docs/PORT-LOBSTER.md, THE TYPE MAPPING): an
// int for every integer of 64 bits or fewer, a float for a float, the
// runtime's Int128 for 128-bit storage. Every value is folded to its
// literal, with the schema spelling kept as a trailing comment.
func (g *Gen) emitConst(d *ir.Const) {
	g.Pf("%s", ir.DocComment(d.Doc, "", "//"))
	var lit string
	switch {
	case d.IsFloat && d.Storage == "float32":
		lit = FormatFloat32(d.Float)
	case d.IsFloat:
		lit = FormatFloat(d.Float)
	case d.Storage == "int128" || d.Storage == "uint128":
		lit = Int128Lit(d.Int)
	default:
		lit = IntLit(d.Int)
	}
	if e := d.Expr; e != nil {
		if r := ir.RenderExpr(e); r != lit && r != d.Int.String() {
			g.Pf("let %s = %s // = %s\n\n", d.Name, lit, r)
			return
		}
	}
	g.Pf("let %s = %s\n\n", d.Name, lit)
}

// VariantName is the Lobster spelling of an enum or tag variant: the Go
// target's, the type name then the variant (WeaponRailgun).
func VariantName(enum, variant string) string {
	return enum + ir.GoExportName(variant)
}

func (g *Gen) emitEnum(d *ir.Enum) {
	g.Pf("%s", ir.DocComment(d.Doc, "", "//"))
	g.Pf("// %s — None = 0 implicit, variants dense from 1, wire range [0, %d] (SPEC §4.2);\n", d.Name, d.Max)
	g.Pf("// a Lobster enum; a | max = ... headroom value is %s(v), a value the enum does not name\n", d.Name)
	g.emitEnumDecl(d.Name, d.Variants, d.VariantDocs, d.Max)
}

func (g *Gen) emitEnumDecl(name string, variants []string, docs []string, max int64) {
	g.Pf("enum %s:\n", name)
	g.Pf("    %s = 0\n", VariantName(name, "None"))
	for i, v := range variants {
		if docs != nil {
			g.Pf("%s", ir.DocComment(docs[i], "    ", "//"))
		}
		g.Pf("    %s = %d\n", VariantName(name, v), i+1)
	}
	g.Pf("let %sMax = %d // the exported extent (SPEC §4.2); an enum has no Count\n\n", name, max)
	g.Pf("// EnumName%s: debug/log/tooling name for any %s wire value; a value\n", name, name)
	g.Pf("// the enum does not name (wire-legal up to the declared max) names as \"???\"\n")
	g.Pf("def EnumName%s(value:int) -> string:\n", name)
	g.emitNameSwitch(append([]string{"None"}, variants...))
}

// emitNameSwitch is a name function's body: one case per value, from 0, and
// "???" for every other value.
func (g *Gen) emitNameSwitch(names []string) {
	g.Pf("    return switch value:\n")
	for i, n := range names {
		g.Pf("        case %d: %s\n", i, StringLit([]byte(n)))
	}
	g.Pf("        default: \"???\"\n\n")
}

func (g *Gen) emitFlags(d *ir.Flags) {
	g.Pf("%s", ir.DocComment(d.Doc, "", "//"))
	g.Pf("// %s — one bit per variant, consumed as masks; flags-typed fields store\n", d.Name)
	g.Pf("// uint64 in every target, so an int here, wire %d bits (SPEC §4.2). Masks\n", d.WireBits)
	g.Pf("// are flat PascalCase — the Go target's spelling exactly.\n")
	for i, v := range d.Variants {
		g.Pf("%s", ir.DocComment(d.VariantDocs[i], "", "//"))
		g.Pf("let %s%s = %s\n", d.Name, v, IntLit(new(big.Int).Lsh(big.NewInt(1), uint(i))))
	}
	g.Pf("let %sCount = %d // the declared variant count (SPEC §4.2)\n\n", d.Name, len(d.Variants))
	g.Pf("// FlagName%s: debug/log/tooling name for bit i of %s\n", d.Name, d.Name)
	g.Pf("def FlagName%s(value:int) -> string:\n", d.Name)
	if len(d.Variants) == 0 {
		g.Pf("    return \"???\"\n\n")
		return
	}
	g.emitNameSwitch(d.Variants)
}

// EmitStruct emits a `type` (or, from lobstertable, a `table`): its class
// and New<Name>, which builds the value at its defaults
// (docs/PORT-LOBSTER.md, THE TYPE MAPPING; SPEC §5).
func (g *Gen) EmitStruct(d *ir.Struct) {
	var slots [][]Slot
	for _, f := range d.Fields {
		slots = append(slots, g.Slots(f))
	}
	g.Pf("%s", ir.DocComment(d.Doc, "", "//"))
	g.Pf("// %s %s\n", map[bool]string{false: "type", true: "table"}[d.IsTable], d.Name)
	g.emitClass(d.Name, slots, func(i int) string { return ir.DocComment(d.Fields[i].Doc, "        ", "//") }, func(i int) []string {
		return g.ByteDefault(d.Fields[i], "value."+ir.GoExportName(d.Fields[i].Name))
	})
}

// emitClass writes one class and its constructor from its slots.
func (g *Gen) emitClass(name string, slots [][]Slot, doc func(int) string, after func(int) []string) {
	g.classes = append(g.classes, name)
	g.Pf("class %s:\n", name)
	n := 0
	for _, ss := range slots {
		for _, s := range ss {
			g.Pf("    %s:%s\n", s.Name, s.Type)
			n++
		}
	}
	g.Pf("def New%s() -> %s:\n", name, name)
	if n == 0 {
		g.Pf("    // empty body — presence is the payload (SPEC §4.6)\n")
		g.Pf("    return %s {}\n\n", name)
		return
	}
	g.Pf("    let value = %s {\n", name)
	var fills []string
	k := 0
	for i, ss := range slots {
		g.Pf("%s", doc(i))
		for _, s := range ss {
			k++
			sep := ","
			if k == n {
				sep = ""
			}
			comment := ""
			if s.Comment != "" {
				comment = " // " + s.Comment
			}
			g.Pf("        %s: %s%s%s\n", s.Name, s.Init, sep, comment)
			for _, fl := range s.Fill {
				fills = append(fills, fmt.Sprintf(fl, "value."+s.Name))
			}
		}
		fills = append(fills, after(i)...)
	}
	g.Pf("    }\n")
	for _, line := range fills {
		g.Pf("    %s\n", line)
	}
	g.Pf("    return value\n\n")
}

// EmitUnion emits a first-class one-of (SPEC §4.8): the <Name>Type tag enum,
// then the class — the tag beside one pre-allocated arm per variant — and
// its constructor, the empty union (None).
func (g *Gen) EmitUnion(d *ir.Union) {
	members := make([]string, len(d.Variants))
	docs := make([]string, len(d.Variants))
	for i, v := range d.Variants {
		members[i] = ir.GoExportName(v.Name)
		docs[i] = v.Doc
	}
	tag := d.Name + "Type"
	g.Pf("// %s: union %s's tag — None = 0, then each variant in declared order (SPEC §4.8)\n", tag, d.Name)
	g.emitEnumDecl(tag, members, docs, int64(len(members)))

	slots := [][]Slot{{{Name: "Type", Type: tag, Init: VariantName(tag, "None")}}}
	var arms []ir.UnionVariant
	for _, v := range d.Variants {
		if v.Void() {
			continue // a PAYLOAD-FREE arm has no storage (SPEC §4.8)
		}
		slots = append(slots, g.armSlots(v))
		arms = append(arms, v)
	}
	g.Pf("%s", ir.DocComment(d.Doc, "", "//"))
	g.Pf("// %s — at most one of the arms; Type says which. Construction is the empty\n", d.Name)
	g.Pf("// union (None); every arm is pre-allocated, and unselected arms keep what\n")
	g.Pf("// they last held — the reused-storage discipline.\n")
	g.emitClass(d.Name, slots, func(int) string { return "" }, func(i int) []string {
		if i == 0 {
			return nil
		}
		return g.ByteDefault(arms[i-1].F, "value."+ir.GoExportName(arms[i-1].Name))
	})
}

// armSlots is an arm's storage: an arm is a field line (SPEC §4.8,
// docs/SPEC-TABLES.md §2.6), stored under the arm's own name.
func (g *Gen) armSlots(v ir.UnionVariant) []Slot {
	f := *v.F
	f.Name = v.Name
	return g.Slots(&f)
}

// A Slot is one storage member a field takes: its Lobster name, its type,
// its construction expression, the statements that finish it after the
// value is built (each a format with one %s, the slot's lvalue) and a
// comment.
type Slot struct {
	Name, Type, Init, Comment string
	Fill                      []string
}

// fillN is the statement that grows a vector slot to n elements of init.
func fillN(n int64, init string) []string {
	return []string{fmt.Sprintf("for(%d): push(%%s, %s)", n, init)}
}

// Slots is a field's storage (docs/PORT-LOBSTER.md, THE TYPE MAPPING): the
// value slot, then the used-length or count slot a byte buffer or counted
// array carries beside it, then an optional field's presence bool.
func (g *Gen) Slots(f *ir.Field) []Slot {
	name := ir.GoExportName(f.Name)
	t := f.Type
	var out []Slot
	switch {
	case f.IsMap() || f.Array == ir.ArrayList:
		// an unbounded array or a map: its elements live in the holder's
		// extent on the wire; in Lobster storage a vector, born empty
		elem, _ := g.elem(f)
		if f.IsMap() {
			elem = g.TypeRef(f.MapEntry.Name, true)
		}
		out = append(out, Slot{Name: name, Type: "[" + elem + "]", Init: "[]"})
	case f.Array != ir.ArrayNone && (t.Pointer || t.TextUnbounded()):
		// an array of pointers or byte buffers: nil-filled slots
		elem, _ := g.elem(f)
		out = append(out, Slot{Name: name, Type: "[" + elem + "?]", Init: "[]", Fill: fillN(f.ArrayBound, "nil"), Comment: "nil is null (docs/SPEC-TABLES.md §2.4)"})
		if f.Array == ir.ArrayCounted {
			out = append(out, Slot{Name: name + "Count", Type: "int", Init: strconv.FormatInt(f.BornCount(), 10)})
		}
	case t.Pointer && t.Blob(), t.TextUnbounded():
		out = append(out, Slot{Name: name, Type: "string?", Init: "nil", Comment: "a byte buffer: nil is absent (docs/SPEC-TABLES.md §2.5)"})
	case t.Pointer:
		out = append(out, Slot{Name: name, Type: g.TypeRef(t.Name, true) + "?", Init: "nil", Comment: "a pointer: nil is null (docs/SPEC-TABLES.md §2.4)"})
	case t.Kind == ir.TString || t.Kind == ir.TBytes || t.Kind == ir.TWString:
		if f.Array != ir.ArrayNone {
			out = append(out, Slot{Name: name, Type: "[string]", Init: "[]", Fill: fillN(f.ArrayBound, bufferInit(t))})
			out = append(out, Slot{Name: name + "Length", Type: "[int]", Init: "[]", Fill: fillN(f.ArrayBound, "0")})
			if f.Array == ir.ArrayCounted {
				out = append(out, Slot{Name: name + "Count", Type: "int", Init: strconv.FormatInt(f.BornCount(), 10)})
			}
			break
		}
		out = append(out, Slot{Name: name, Type: "string", Init: bufferInit(t), Comment: fmt.Sprintf("%s: max length, used length beside it (SPEC §4.7)", typeWord(t))})
		out = append(out, Slot{Name: name + "Length", Type: "int", Init: "0"})
	case f.Array != ir.ArrayNone:
		elem, init := g.elem(f)
		bound := f.ArrayBound
		if isByteElem(t) {
			// uint8 arrays store as a byte string — the bulk path's own type
			out = append(out, Slot{Name: name, Type: "string", Init: fmt.Sprintf("%s.zeros(%d)", RuntimeNamespace, bound), Comment: "[N]uint8: a byte string, 0-based"})
		} else {
			out = append(out, Slot{Name: name, Type: "[" + elem + "]", Init: "[]", Fill: fillN(bound, init)})
		}
		if f.Array == ir.ArrayCounted {
			// a [A..B] count is born at A (SPEC §4.6)
			out = append(out, Slot{Name: name + "Count", Type: "int", Init: strconv.FormatInt(f.BornCount(), 10)})
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
		out = append(out, Slot{Name: ir.GoExportName(f.Name + "_present"), Type: "bool", Init: "false", Comment: "?T presence (docs/SPEC-TABLES.md §2.3)"})
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

// bufferInit is a string/bytes/wstring value's storage: a byte string of its
// maximum size (a wstring's is two bytes a code unit), every byte zero.
func bufferInit(t ir.FieldType) string {
	if t.Kind == ir.TWString {
		return fmt.Sprintf("%s.zeros(%d)", RuntimeNamespace, 2*t.Size)
	}
	return fmt.Sprintf("%s.zeros(%d)", RuntimeNamespace, t.Size)
}

// ByteDefault is the statements that write a string/bytes field's specified
// default into its fresh buffer and set its used length (SPEC §4.2).
func (g *Gen) ByteDefault(f *ir.Field, slot string) []string {
	if !f.HasDefault || len(f.DefBytes) == 0 || f.Array != ir.ArrayNone {
		return nil
	}
	return []string{
		fmt.Sprintf("%s = %s.put(%s, %s)", slot, RuntimeNamespace, slot, StringLit(f.DefBytes)),
		fmt.Sprintf("%sLength = %d", slot, len(f.DefBytes)),
	}
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
		return "string", "nil"
	}
	if f.Type.Pointer {
		return g.TypeRef(f.Type.Name, true), "nil"
	}
	if f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString {
		return "string", bufferInit(f.Type)
	}
	return g.scalar(&e)
}

// scalar is a non-array field's type and its construction (the default when
// one is specified, else the §5 zero form).
func (g *Gen) scalar(f *ir.Field) (typ, init string) {
	t := f.Type
	switch t.Kind {
	case ir.TBool:
		return "bool", strconv.FormatBool(f.HasDefault && f.DefBool)
	case ir.TFloat32:
		if f.HasDefault {
			return "float", FormatFloat32(f.DefFloat)
		}
		return "float", "0.0"
	case ir.TFloat64:
		if f.HasDefault {
			return "float", FormatFloat(f.DefFloat)
		}
		return "float", "0.0"
	case ir.TInt, ir.TBits, ir.TFixed:
		v := new(big.Int)
		if f.HasDefault && f.DefInt != nil {
			v = f.DefInt
		}
		if t.Width > 64 {
			return RuntimeNamespace + ".Int128", Int128Lit(v)
		}
		return "int", IntLit(v)
	case ir.TNamed:
		switch r := t.Ref.(type) {
		case *ir.Enum:
			v := "None"
			for _, variant := range r.Variants {
				if f.HasDefault && variant == f.DefVariant {
					v = variant
				}
			}
			return g.TypeRef(t.Name, false), VariantName(t.Name, v)
		case *ir.Flags:
			v := new(big.Int)
			if f.HasDefault && f.DefInt != nil {
				v = f.DefInt
			}
			g.TypeRef(t.Name, false)
			return "int", IntLit(v)
		case *ir.Struct:
			return g.TypeRef(t.Name, r.IsTable), "New" + t.Name + "()"
		case *ir.Union:
			table := g.Unit.TableUnions[t.Name] != nil
			return g.TypeRef(t.Name, table), "New" + t.Name + "()"
		}
	}
	return "int", "0"
}

// TypeRef is the Lobster spelling of a named declaration's type from this
// module — bare, since every module of the unit shares its namespace — and
// it imports the module that declares it. table selects the table-surface
// module family (lobstertable's <Base>Table.lobster).
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
	return g.File.Base + g.Suffix
}

var two64 = new(big.Int).Lsh(big.NewInt(1), 64)
var mask64 = new(big.Int).Sub(two64, big.NewInt(1))

// IntLit renders a value of 64 bits or fewer as a Lobster int literal
// (docs/PORT-LOBSTER.md, THE NUMBER MODEL): decimal while it fits int64
// above its minimum, else the two's-complement bit pattern in hex — `int`
// holds bits, and a uint64 past 2^63 is the same 64 bits (measured on the
// pin: 0xFFFFFFFFFFFFFFFF is -1, and -9223372036854775808 is refused as an
// int constant overflow).
func IntLit(v *big.Int) string {
	if v.IsInt64() && v.Int64() > math.MinInt64 {
		return v.String()
	}
	bits := new(big.Int).And(v, mask64)
	return fmt.Sprintf("0x%016x", bits)
}

// Int128Lit renders a 128-bit value as the runtime's Int128 struct: the low
// and high 64-bit lanes of its two's-complement bits.
func Int128Lit(v *big.Int) string {
	m128 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1))
	bits := new(big.Int).And(v, m128)
	lo := new(big.Int).And(bits, mask64)
	hi := new(big.Int).Rsh(bits, 64)
	return fmt.Sprintf("%s.Int128 { lo: 0x%016x, hi: 0x%016x }", RuntimeNamespace, lo, hi)
}

// FormatFloat renders a float64 literal: the shortest decimal that reads back
// to the same double, in positional form (Lobster has no exponent literal),
// always with a point; the non-finite values as expressions.
func FormatFloat(v float64) string {
	switch {
	case math.IsInf(v, 1):
		return "(1.0 / 0.0)"
	case math.IsInf(v, -1):
		return "(-1.0 / 0.0)"
	case math.IsNaN(v):
		return fmt.Sprintf("%s.f64frombits(0x%016x)", RuntimeNamespace, math.Float64bits(v))
	}
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

// FormatFloat32 renders a float32 value as the Lobster literal of float32(v)
// held as a double (f32 ⊂ f64), so the stored value is already the rounded
// one, as Go's float32(v) computes it (docs/PORT-LOBSTER.md, FLOAT32).
func FormatFloat32(v float64) string {
	return FormatFloat(float64(float32(v)))
}

// StringLit renders bytes as a Lobster string literal: letters, digits and
// a few safe punctuation bytes as themselves, every other byte as \xNN —
// so no `{`, `}`, quote or backslash reaches the literal unescaped (a brace
// starts string interpolation in Lobster).
func StringLit(b []byte) string {
	var s strings.Builder
	s.WriteByte('"')
	for _, c := range b {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == ' ', c == '.', c == '-':
			s.WriteByte(c)
		default:
			fmt.Fprintf(&s, "\\x%02x", c)
		}
	}
	s.WriteByte('"')
	return s.String()
}
