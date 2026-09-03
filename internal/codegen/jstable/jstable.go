// Package jstable emits a unit's JavaScript table surface
// (docs/SPEC-TABLES.md): the TABLE-wire codecs in <Base>Table.js, and the two
// ACCELERATORS on the side in <Base>Block.js (§19) and <Base>Cook.js (§7). One
// file per unit file, emitted only when the unit declares tables — plus the
// unit's ONE RUNTIME HOME per surface, <Package>Table.js / <Package>Block.js /
// <Package>Cook.js, where everything shared lands (runtimeHome, below).
//
// THE READING TIER. JavaScript has no struct layout of its own — no offsetof,
// no sizeof, no way to place a field — so this backend reads the two
// accelerators and never produces them: a block and a cook are opened over
// bytes another language wrote, and every field is read at the offset the
// compiler settled, through a DataView with an explicit little-endian flag at
// every call. The tolerant wire (§3) is the one form this backend both writes
// and reads, because that wire is a byte stream and not a layout.
//
// The WIRE half is the FIXED class only: storage classes for the `table`
// declarations, then measure/save/load codecs and reflection descriptors for
// the whole TABLE CLOSURE (every table plus everything one references,
// transitively).
//
// The C++ backend (internal/codegen/cpptable) is the REFERENCE and the C#
// backend (internal/codegen/cstable) is the worked second port: this one
// mirrors their framing, their elision decisions, their clamps and their
// report events byte for byte, and invents no contract of its own. Where
// JavaScript forces a different spelling the reason is stated at the site.
//
// Storage follows the JavaScript PACKET emitter's conventions exactly
// (internal/codegen/js): classes with public fields initialized in the
// constructor in declaration order, string(N) and bytes(N) as a preallocated
// Uint8Array beside a Number used length, arrays as a preallocated Array (a
// Uint8Array for uint8 elements) beside a Number used count, unions as a tag
// beside one preallocated arm per variant. That is not a free choice — a
// table's closure contains plain `type` declarations whose storage the packet
// emitter already wrote, and the table codecs decode into those very classes.
// One unit, one spelling.
//
// AND THE CASING FOLLOWS IT TOO, which is a rule and not a preference: every
// identifier this backend emits is spelled the way the JavaScript PACKET
// emitter spells the same KIND of thing, because a consumer sees one unit and
// not two halves of one. So:
//
//   - TYPES, FUNCTIONS and CONSTANTS are UpperCamelCase, as `WriteVec2`,
//     `ZeroVec2` and `Vec2MaxBits` are — every codec, every accessor, every
//     descriptor constructor here.
//
//   - DATA MEMBERS are UpperCamelCase, as `value.X` is. That covers the
//     reflection descriptors' OWN vocabulary (`Name`, `Kind`, `Fields`,
//     `ArrayBound`, `GetChild`), the reader's and writer's state (`Offset`,
//     `View`, `Bytes`, `Limit`, `Report`, `Overflow`), the read report's
//     counters, and the block and cook HANDLES (`Bytes`, `View`, `Region`,
//     `Length`, `Used`). A descriptor member holding a closure is still a
//     member, so it is `GetRaw`, not `getRaw`.
//
//   - METHODS are lowerCamelCase, because the only methods generated packet
//     code calls are the stream's — `stream.serializeDouble` — and a reader's
//     `get32` sits in exactly that position.
//
//   - LOCALS and anything closed over by an IIFE are lowerCamelCase, as the
//     packet emitter's `const used` is. They are not surface: nothing outside
//     can name them.
//
// The one thing the SCHEMA spells stays spelled the schema's way: a field's
// wire name, its JSON key, an enum variant's name. Those are DATA in a
// descriptor, not identifiers.
//
// WHAT ALLOCATES ON THE READ PATH, exactly, and why (docs/SPEC-TABLES.md's
// allocation rung: EVERY READ PATH ALLOCATES NOTHING):
//
//   - Nothing per field. A nested body is read by narrowing the SAME reader's
//     limit and restoring it afterwards rather than by slicing a sub-reader,
//     so a body of any depth costs no object. The C++ reference takes a
//     sub-span and C# a sliced ref struct; both are stack, and a JavaScript
//     slice would be heap, so the port scopes instead of slicing. The
//     behavior is identical: a nested decode cannot read past its own
//     framing.
//   - Nothing for a string or a `bytes`. Storage is BYTES — a Uint8Array with
//     a used length beside it, exactly as C++ and C# hold it — so a read is a
//     byte loop into a buffer the value already owns. It is a LOOP and not
//     `set(source.subarray(...))` because a subarray is a new TypedArray
//     object, which would be one heap allocation per string field; the type
//     wire's flat tier copies a string the same way and for the same reason.
//     There is no TextDecoder on the read path at all; a caller that wants a
//     JavaScript string calls one itself, at the boundary, where the
//     allocation is its own choice.
//   - TWO OBJECTS PER CALL, and none per field: <Name>Save builds a
//     TableWriter and <Name>Load a TableReader, each with its own DataView.
//     C++ puts both on the stack and C# spells them as ref structs, so neither
//     pays for the thing itself; JavaScript has no stack object. A caller on a
//     per-frame path builds ONE writer and ONE reader, points each at the next
//     buffer with `reset`, and calls <Name>SaveBody / <Name>LoadBody — the
//     same code the two entry points run, with the objects hoisted out of the
//     loop.
//   - A 64-BIT INTEGER ALLOCATES ON READ, and this is the one place the
//     language forces it: JavaScript's only exact 64-bit integer is BigInt,
//     and every BigInt is an object. A field declared int64/uint64/bits(N >
//     32), or a flags mask, therefore costs one allocation per field READ —
//     the DataView hands back a fresh BigInt. The packet emitter already pays
//     this on the type wire (SPEC §6.1's value-domain seam) and the table wire
//     pays it for the same reason. Every narrower field — every bool, every
//     integer of 32 bits or fewer, every float, every enum — is a Number and
//     allocates nothing.
//   - AND THE WRITE OF THAT SAME FIELD ALLOCATES NOTHING. `put64` hands the
//     stored BigInt straight to `setBigUint64`, which wraps a negative value
//     modulo 2^64 by specification without building a second BigInt; a
//     `BigInt.asUintN(64, v)` in front of it would be one allocation per
//     64-bit field WRITTEN, which is what the hoisted Load→Save loop measured
//     before the call was removed. So the loop the pages document — one
//     reader, one writer, LoadBody then SaveBody — pays exactly Load's
//     BigInts and nothing on the way back out.
//   - AND A 64-BIT NUMBER THE FORM'S OWN FRAMING CARRIES IS NOT A FIELD, so it
//     does not get that license: a block array's `offset_of` and a cook
//     reference's delta are both composed from TWO 32-BIT READS instead. Those
//     are the two hottest lines the accelerators have — once per row addressed,
//     once per edge followed — and a `getBigUint64` there would allocate per
//     row and per edge. Open has already bounded both, in BigInt, once, where a
//     forgery near 2^63 must refuse; by the time a row is addressed the value
//     fits a Number exactly.
//   - A ROW'S 64-BIT FIELD reads as a BigInt through its accessor, one
//     allocation per read, on the same license as the wire's. A `flags` field
//     carries `<Member>Has(view, at, bit)` beside it, which reads the one
//     32-bit word holding the bit and allocates nothing — the accessor a
//     per-frame consumer that only TESTS a flag reaches for.
//
// EVERY ONE OF THOSE IS A MEASURED NUMBER, not an intention:
// `make tables-js-alloc` reports bytes allocated per iteration per path —
// the three bodies alone, the hoisted Load→Save loop the pages document, a
// cook deref, a block row walk — and gates each at the floor stated for it;
// its negative control puts one extra allocation per iteration in and
// requires them to go red.
//
// The GENERIC path is the text form (§16) and the reflection walk, and it
// allocates by design: the ladder licenses exactly that ("more generic and
// does allocations if we want it for tooling, editor, whatever"). Its numeric
// currency is BigInt, which is what lets one walker carry a u64 and an i64
// through the same clamps the reference spells in `ulong` and `long`.
//
// The VARIABLE class ON THE WIRE — the arena, the builder, the region and the
// node-table codec (kind 17) — is a named follow-on, as it is in C#: a unit
// whose closure declares a pointer gets no <Base>Table.js, and the refusal is
// NAMED in every source the unit does emit rather than left as a missing
// symbol (§11). Its two ACCELERATORS are emitted in full, because neither
// needs a codec.
package jstable

import (
	"fmt"
	"maps"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// table-wire kinds (docs/SPEC-TABLES.md §3) — the numbers are the wire's, not a
// backend's, and they are duplicated from cpptable deliberately: a port that
// derived them from the reference emitter's private helpers would break the
// day the two files disagree, and this way a disagreement shows up in the
// shared golden bytes instead.
const (
	tkBool   = 1
	tkI8     = 2
	tkI16    = 3
	tkI32    = 4
	tkI64    = 5
	tkU8     = 6
	tkU16    = 7
	tkU32    = 8
	tkU64    = 9
	tkF32    = 10
	tkF64    = 11
	tkString = 12
	tkTable  = 13
	tkArray  = 14
	tkUnion  = 15
	// an ENUM-KEYED array body is its OWN kind (docs/SPEC-TABLES.md §3.2): the
	// positional array body and the keyed one are incompatible, so a reader
	// meeting the other must see a KIND MISMATCH and skip, never misdecode.
	tkKeyed = 16
)

func tableScalarKind(f *ir.Field) int {
	switch f.Type.Kind {
	case ir.TBool:
		return tkBool
	case ir.TInt:
		if f.Type.Signed {
			switch f.Type.Width {
			case 8:
				return tkI8
			case 16:
				return tkI16
			case 32:
				return tkI32
			default:
				return tkI64
			}
		}
		switch f.Type.Width {
		case 8:
			return tkU8
		case 16:
			return tkU16
		case 32:
			return tkU32
		default:
			return tkU64
		}
	case ir.TBits:
		switch {
		case f.Type.Width <= 8:
			return tkU8
		case f.Type.Width <= 16:
			return tkU16
		case f.Type.Width <= 32:
			return tkU32
		default:
			return tkU64
		}
	case ir.TFloat32:
		return tkF32
	case ir.TFloat64:
		return tkF64
	case ir.TString:
		return tkString
	case ir.TBytes:
		return tkArray
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			// an enum value rides as the u16 hash of its VARIANT NAME
			// (docs/SPEC-TABLES.md §5), whatever the declaration-side width
			return tkU16
		case *ir.Flags:
			return tkU64
		case *ir.Struct:
			return tkTable
		case *ir.Union:
			return tkUnion
		}
	}
	return 0
}

func tableKindWidth(kind int) int {
	switch kind {
	case tkBool, tkI8, tkU8:
		return 1
	case tkI16, tkU16:
		return 2
	case tkI32, tkU32, tkF32:
		return 4
	case tkI64, tkU64, tkF64:
		return 8
	}
	return 0
}

// runtimeHome is the basename of the module that carries a unit's shared
// JavaScript runtime — the table runtime here, the block runtime in block.go,
// the cook runtime in cook.go, each in its own <Package><Surface>.js.
//
// IT IS THE PACKAGE, and nothing else. An ES module is file-scoped, so a shared
// runtime is DEFINED once and imported everywhere else — and the module that
// defines it wants a name that does not move. Any rule that picks a FILE picks
// it off the file order, and relocates the whole runtime the day the unit gains
// a file that sorts earlier: correct output, and a diff nobody can read. The
// package names one home per unit and never moves. It is the rule the C#
// backend settled (schema#347), spelled here in this language's file names.
//
// A unit whose own <Package>.schema exists carries the runtime in that file's
// generated output — same name, one module, no collision. Every other unit gets
// the module emitted for it, which is what makes the home unconditional: it
// always exists, so no import of the runtime can dangle.
//
// A file basename that differs from the package ONLY BY CASE is the same file
// name on a case-insensitive filesystem and two on a case-sensitive one, so the
// match is case-insensitive and the FILE's own spelling wins.
func runtimeHome(u *ir.Unit) string {
	home := capitalize(u.Package)
	for _, f := range u.Files {
		if strings.EqualFold(f.Base, home) {
			return f.Base
		}
	}
	return home
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// runtimeHomeMarker rides the banner of each runtime home, so "which module
// carries this unit's shared runtime" is a grep rather than a line count.
const runtimeHomeMarker = "the unit's shared runtime lives here"

// generatedFrom is the DO NOT EDIT banner's first line. A runtime home the unit
// has no file for is generated FOR THE UNIT, and says so — there is no
// <Package>.schema to name.
func generatedFrom(f *ir.File, u *ir.Unit) string {
	if f == nil {
		return fmt.Sprintf("// Code generated by the schema compiler for package %s. DO NOT EDIT.\n", u.Package)
	}
	return fmt.Sprintf("// Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", f.Base)
}

// tableGen carries one generated <Base>Table.js.
type tableGen struct {
	unit     *ir.Unit
	file     *ir.File   // nil for a runtime home the unit has no file for
	base     string     // this module's own basename
	home     bool       // this module carries the unit's shared table runtime
	homeBase string     // the basename of the module that does
	anyKeyed bool       // the unit declares at least one enum-keyed array
	owner    *ir.Struct // the closure member whose codec is being emitted

	body    strings.Builder
	indent  string                     // extra per-line indent inside a branch guard
	imports map[string]map[string]bool // file base -> exported symbols
}

func (g *tableGen) pf(format string, args ...any) {
	s := fmt.Sprintf(format, args...)
	if g.indent != "" && s != "" {
		trailing := strings.HasSuffix(s, "\n")
		if trailing {
			s = s[:len(s)-1]
		}
		s = g.indent + strings.ReplaceAll(s, "\n", "\n"+g.indent)
		if trailing {
			s += "\n"
		}
	}
	g.body.WriteString(s)
}

// need imports a symbol from another module of the unit. ES modules are
// file-scoped, so unlike C#'s one assembly nothing is ambient here and every
// cross-file name is an explicit edge.
func (g *tableGen) need(base string, symbols ...string) {
	// the module this generator IS: <Base>Table.js, never <Base>.js — a
	// self-import would redeclare every name it defines
	if base == "" || base == g.base+"Table" {
		return
	}
	set := g.imports[base]
	if set == nil {
		set = map[string]bool{}
		g.imports[base] = set
	}
	for _, s := range symbols {
		set[s] = true
	}
}

// needDecl imports symbols from the PACKET module of the file that declares
// decl — the classes, enums and unions internal/codegen/js emits.
func (g *tableGen) needDecl(decl string, symbols ...string) {
	base, ok := g.unit.DeclFile[decl]
	if !ok {
		return
	}
	g.need(base, symbols...)
}

// needTable imports symbols from the TABLE module of the file that declares a
// table. A table is not in DeclFile — it lives beside the packet decl stream
// (ir.File.Tables) — so the lookup walks the unit's files.
func (g *tableGen) needTable(name string, symbols ...string) {
	for _, f := range g.unit.Files {
		for _, st := range f.Tables {
			if st.Name == name {
				g.need(f.Base+"Table", symbols...)
				return
			}
		}
	}
	// a closure `type`'s table codecs live in the TABLE module of the file
	// that declares the type
	if base, ok := g.unit.DeclFile[name]; ok {
		g.need(base+"Table", symbols...)
	}
}

// needRuntime imports one of the unit's shared runtime names from the home
// module.
func (g *tableGen) needRuntime(symbols ...string) { g.need(g.homeBase+"Table", symbols...) }

// Generate emits <Base>Table.js for every unit file when the unit declares
// tables, and nothing when it does not — a table-free unit's generated
// JavaScript is byte-identical with or without this package.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	if len(u.Tables) == 0 {
		return map[string][]byte{}, nil
	}
	if err := refuseFileCollisions(u); err != nil {
		return nil, err
	}
	// The two ACCELERATORS are emitted ON THE SIDE and neither needs a wire
	// codec: the BLOCK form (§19) points at bytes a producer wrote, and the
	// COOK (§7) points at a region the tooling wrote. Both are pure readers
	// over bytes at settled offsets, so both reach further than this
	// backend's wire half does — a unit whose variable class the wire cannot
	// spell still gets its cooks opened.
	blocks := ir.Blocks(u)
	out, err := generateBlockFiles(u, blocks)
	if err != nil {
		return nil, err
	}
	cooks, err := generateCookFiles(u, blocks)
	if err != nil {
		return nil, err
	}
	maps.Copy(out, cooks)
	// The VARIABLE-CLASS refusal (docs/SPEC-TABLES.md §2.2, §11) is a refusal of
	// the WIRE SURFACE, which is the half the variable class is missing: no
	// arena, no builder, no region and no node-table codec. It is named rather
	// than silent — every generated accelerator of the unit opens with the
	// banner below, naming each table and the follow-on — and no
	// <Base>Table.js is emitted, so a consumer that imports Save or Load gets
	// a missing export from its own module loader, beside a file that says why.
	if names := variableTableNames(u); len(names) > 0 {
		for name, data := range out {
			out[name] = append([]byte(variableClassBanner(names)), data...)
		}
		return out, nil
	}
	closure := ir.TableClosure(u)
	home := runtimeHome(u)
	anyKeyed := unitHasKeyedArray(u, closure)
	runtimeWritten := false
	// the identity pair of an enum is emitted ONCE per unit, by the module of
	// the file that declares it, and imported everywhere else: ES modules are
	// file-scoped, so a second definition would be a second symbol rather than
	// C++'s harmless re-inclusion behind a guard
	usedEnums := closureEnums(u, closure)
	for _, f := range u.Files {
		g := &tableGen{
			unit: u, file: f, base: f.Base, home: f.Base == home, homeBase: home,
			anyKeyed: anyKeyed, imports: map[string]map[string]bool{},
		}
		var members []*ir.Struct
		members = append(members, f.Tables...)
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok && closure[st.Name] {
				members = append(members, st)
			}
		}
		if g.home {
			runtimeWritten = true
			g.pf("%s", tableRuntime(anyKeyed))
			g.pf("%s", tableBitHelpers())
			// THE TEXT FORM's generic walk (docs/SPEC-TABLES.md §16), emitted
			// ONCE per unit beside the rest of the shared runtime and imported
			// by every other module of it. Its source never varies with the
			// unit, which is the generic-walk gate (`make tables-js-json-walk`).
			g.pf("%s", tableJsonWalkSource)
		}
		for _, st := range members {
			if st.IsTable {
				g.owner = st
				g.emitTableClass(st)
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
		}
		if len(members) > 0 {
			g.pf("// ---- reflection descriptors (tables only, docs/SPEC-TABLES.md §8) ----\n\n")
			for _, st := range members {
				g.owner = st
				g.emitTableDescriptor(st)
			}
			// and the TEXT FORM's per-member surface: three thin wrappers over
			// the generic walk, each naming a descriptor and nothing else
			// (docs/SPEC-TABLES.md §16.1)
			g.pf("// ---- the text form: JSON in and out of one table (docs/SPEC-TABLES.md §16) ----\n\n")
			for _, st := range members {
				g.owner = st
				g.emitJsonSurface(st)
			}
		}
		out[f.Base+"Table.js"] = g.assemble()
	}
	// No file of the unit is named for the package, so the home is emitted for
	// the unit rather than for a file. The runtime lands in <Package>Table.js
	// either way — that is the whole rule.
	if !runtimeWritten {
		g := &tableGen{
			unit: u, base: home, home: true, homeBase: home,
			anyKeyed: anyKeyed, imports: map[string]map[string]bool{},
		}
		g.pf("%s", tableRuntime(anyKeyed))
		g.pf("%s", tableBitHelpers())
		g.pf("%s", tableJsonWalkSource)
		out[home+"Table.js"] = g.assemble()
	}
	return out, nil
}

// refuseFileCollisions names the two module basenames this backend claims
// beside each schema file, so a unit that would have two emitters writing one
// path is refused here rather than losing a file quietly.
func refuseFileCollisions(u *ir.Unit) error {
	bases := map[string]bool{}
	for _, f := range u.Files {
		bases[f.Base] = true
	}
	// the runtime home is a basename too, whether a file of the unit carries it
	// or the compiler emits it for the unit
	bases[runtimeHome(u)] = true
	for base := range bases {
		for _, suffix := range []string{"Table", "Block", "Cook"} {
			if bases[base+suffix] {
				return fmt.Errorf("schema files %s and %s%s collide — the JS table emitter writes %s%s.js as %s's table surface; rename one file (docs/SPEC-TABLES.md §11)",
					base, base, suffix, base, suffix, base)
			}
		}
	}
	return nil
}

// variableTableNames is the unit's variable-length tables, sorted — the tables
// whose WIRE surface this backend does not emit.
func variableTableNames(u *ir.Unit) []string {
	variable := ir.VariableTables(u)
	if len(variable) == 0 {
		return nil
	}
	names := make([]string, 0, len(variable))
	for name := range variable {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// variableClassBanner is the VARIABLE-class refusal, written where a consumer
// meets it (docs/SPEC-TABLES.md §2.2, §11).
func variableClassBanner(names []string) string {
	return "// THE JAVASCRIPT WIRE SURFACE OF THIS UNIT IS REFUSED, BY NAME (docs/SPEC-TABLES.md §11).\n" +
		"//\n" +
		"// It declares variable-length tables (" + englishList(names) + "), and the JavaScript\n" +
		"// table backend's VARIABLE CLASS — the arena, the builder, the region and the\n" +
		"// node-table codec (kind 17) — is a named follow-on (§15). No <Base>Table.js is\n" +
		"// emitted for this unit, so a consumer importing Measure, Save or Load gets a\n" +
		"// missing export from its own module loader, beside this file, which says why.\n" +
		"//\n" +
		"// What IS emitted is the two ACCELERATORS, because neither needs a codec: a block\n" +
		"// (§19) and a cook (§7) are pointed at, not parsed. A build that loads this unit's\n" +
		"// cooked assets is served in full; one that wants the tolerant wire is not, and\n" +
		"// runs the tool or the C++ backend for it.\n\n"
}

func englishList(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// closureEnums is every enum whose values ride in the unit's table closure.
func closureEnums(u *ir.Unit, closure map[string]bool) map[string]*ir.Enum {
	used := map[string]*ir.Enum{}
	names := make([]string, 0, len(closure))
	for name := range closure {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			// an enum-keyed array's KEY rides as a variant hash too, so its
			// enum needs the identity pair even when no field has that type
			if f.KeyEnumRef != nil {
				used[f.KeyEnumRef.Name] = f.KeyEnumRef
			}
			if f.Type.Kind != ir.TNamed {
				continue
			}
			if e, isEnum := f.Type.Ref.(*ir.Enum); isEnum {
				used[e.Name] = e
			}
		}
	}
	return used
}

// fileEnums is the used enums this file DECLARES, in declaration order.
func fileEnums(f *ir.File, used map[string]*ir.Enum) []*ir.Enum {
	var out []*ir.Enum
	for _, d := range f.Decls {
		if e, ok := d.(*ir.Enum); ok {
			if u, live := used[e.Name]; live {
				out = append(out, u)
			}
		}
	}
	return out
}

func (g *tableGen) assemble() []byte {
	var h strings.Builder
	h.WriteString(generatedFrom(g.file, g.unit))
	h.WriteString("// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	h.WriteString("// your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	h.WriteString("// AGPL-3.0, its output is not.\n")
	fmt.Fprintf(&h, "// package %s — the TABLE wire (docs/SPEC-TABLES.md): evolution-tolerant, neutral\n", g.unit.Package)
	h.WriteString("// bytes, no serialize dependency. Tables version by field id, never by the\n")
	h.WriteString("// unit's protocol id.\n")
	if g.home {
		h.WriteString("//\n")
		h.WriteString("// " + runtimeHomeMarker + " — <Package>Table.js, one home per unit, named by\n")
		h.WriteString("// the package and independent of file order (docs/SPEC-TABLES.md §19.2).\n")
		h.WriteString("//\n")
		h.WriteString("// Measure/Save/Load are name-first module functions: <Name>Measure gives the\n")
		h.WriteString("// exact wire size, <Name>Save hands back a Uint8Array of exactly that many\n")
		h.WriteString("// bytes and <Name>SaveInto writes them into the caller's, <Name>Load hands\n")
		h.WriteString("// back the value — the caller's own, overlaid in place, when it passes one —\n")
		h.WriteString("// and ledgers every tolerance event in the report. A value the wire cannot\n")
		h.WriteString("// carry and a buffer too small are the CALLER's errors and throw a RangeError;\n")
		h.WriteString("// what the DATA does is never an exception, it is the report.\n")
		h.WriteString("//\n")
		h.WriteString("// Every multi-byte read and write names its byte order explicitly — the\n")
		h.WriteString("// `true` at every DataView call — so the wire does not depend on the host's.\n")
	}
	h.WriteString("\n")
	if len(g.imports) > 0 {
		bases := make([]string, 0, len(g.imports))
		for b := range g.imports {
			bases = append(bases, b)
		}
		sort.Strings(bases)
		for _, b := range bases {
			syms := make([]string, 0, len(g.imports[b]))
			for s := range g.imports[b] {
				syms = append(syms, s)
			}
			sort.Strings(syms)
			fmt.Fprintf(&h, "import { %s } from \"./%s.js\";\n", strings.Join(syms, ", "), b)
		}
		h.WriteString("\n")
	}
	if !g.home {
		// THE SHARED RUNTIME IS RE-EXPORTED from every module of the unit. It
		// is DEFINED once, in the unit's runtime home, because an ES module is
		// file-scoped and a second definition would be a second, unequal
		// class; but a caller of <Name>Load needs a TableReport, and making it
		// import a second module to construct one would be a papercut with no
		// property behind it. Every <Base>Table.js therefore offers the same
		// surface, and every one of them names the same objects.
		h.WriteString("// The unit's shared table runtime, defined once in the unit's runtime home and\n")
		h.WriteString("// re-exported here: one module, one whole surface.\n")
		fmt.Fprintf(&h, "export { %s } from \"./%sTable.js\";\n\n",
			strings.Join(sharedRuntimeNames(g.anyKeyed), ", "), g.homeBase)
	}
	h.WriteString(g.body.String())
	return []byte(h.String())
}

// sharedRuntimeNames is the unit-level surface the home module defines and
// every other module of the unit re-exports.
func sharedRuntimeNames(anyKeyed bool) []string {
	names := []string{
		"TableBitsToDouble", "TableBitsToFloat", "TableDoubleToBits", "TableFloatToBits",
		"TableJson", "TableReader", "TableReport", "TableWriter",
	}
	if anyKeyed {
		names = append(names, "TableKeyed")
	}
	sort.Strings(names)
	return names
}

// unitHasKeyedArray reports whether any closure member declares an enum-keyed
// array, which is what decides whether the unit carries the keyed storage type.
func unitHasKeyedArray(u *ir.Unit, closure map[string]bool) bool {
	for name := range closure {
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			if f.KeyEnum != "" {
				return true
			}
		}
	}
	return false
}

// tableKeyedStorage is the storage type behind `ships [ShipType]ShipConfig`
// (docs/SPEC-TABLES.md §2.4). Emitted only into a unit that declares a keyed
// array, so a unit without one is byte-identical to what it was.
//
// WHAT JAVASCRIPT SPELLS DIFFERENTLY, stated where a reader meets it. C++'s
// accessor takes the key's own enum type and C#'s takes the key's value as an
// int; a JavaScript enum IS its value — the generated enum is a frozen object
// of Numbers — so `hull.Turrets.get(Weapon.Missile)` is the whole call and no
// cast appears at any site. The SHIFT is the array's, exactly as in every
// other port: the accessor subtracts one, and no call site in any language
// spells it (§2.4). The refusal of a key that names no slot — None below the
// storage, anything past E.Max above it — is a throw and stands in every
// build, as C++'s abort does.
//
// ITERATION carries over with the same range: the iterator yields
// [key, element] pairs with the key 1..E.Max, the same currency the accessor
// takes, so the two halves of the surface agree and agree with every other
// port's.
//
// The EXTENT comes from the key enum's own Max member, passed at construction
// and named nowhere else — no generated constant a consumer could put out of
// step with the enum (§2.4).
const tableKeyedStorage = `// An ENUM-KEYED array's storage: E.Max slots, ONE PER NAMED VARIANT, with the
// key k at index k-1 — the storage SHIFTS LEFT and nothing is stored for None.
//
// NOTHING OUTSIDE THE ARRAY NAMES ITS SIZE: the extent is the key enum's own
// Max, handed in at construction, so there is no generated constant a consumer
// could put out of step with the enum.
//
// A KEY THAT NAMES NO SLOT IS AN ERROR AT BOTH ENDS (§2.4): None below the
// storage — the null key, which never rides on the wire and is malformed when
// stored — and anything past E.Max above it. The guard is symmetric because the
// storage is, and it is ONE unsigned compare: the storage index is key - 1,
// None's is -1, which wraps above the extent unsigned. The throw stands in
// every build exactly as the C++ abort does.
//
// ITERATION is the surface a consumer of the WHOLE array wants: for..of walks
// every stored slot and yields [key, element] with the key 1..E.Max, so no
// caller spells a bound, a lower limit or the shift.
//
// Slots is public and is what the generated codecs walk, by STORAGE INDEX; get
// and set are for callers and take the KEY, and they are the one place a key
// that names no slot can be caught.
export class TableKeyed {
  constructor(slotCount, make) {
    this.Slots = new Array(slotCount);
    for (let i = 0; i < slotCount; i++) {
      this.Slots[i] = make === null ? 0 : make();
    }
  }

  get(key) {
    this.refuse(key);
    return this.Slots[key - 1];
  }

  set(key, value) {
    this.refuse(key);
    this.Slots[key - 1] = value;
  }

  refuse(key) {
    if (!Number.isInteger(key) || ((key - 1) >>> 0) >= this.Slots.length) {
      throw new RangeError("an enum-keyed array has one slot per named variant, keys 1 to " +
        this.Slots.length + ": None keys no slot, and neither does " + String(key));
    }
  }

  *[Symbol.iterator]() {
    for (let i = 0; i < this.Slots.length; i++) {
      yield [i + 1, this.Slots[i]]; // the STORAGE index is i; the key it holds is i + 1
    }
  }
}

`

// tableBitHelpers is the float <-> IEEE-754 bit pattern pair. The generic walk
// and the descriptors' raw accessors carry a value as its bit pattern, exactly
// as the reference does, so the two ends need one conversion each way.
//
// The scratch DataView is shared, and is safe for the same reason the packet
// emitter's holders are: JavaScript is single threaded per realm and each
// conversion consumes the scratch in the same expression that fills it. It
// takes a name in the Table* family rather than a SCREAMING_SNAKE one because
// every module-scope name a generated module spells is a name a declaration
// could collide with, and the Table* family is the one §11 claims.
func tableBitHelpers() string {
	return `// the IEEE-754 bit patterns the wire carries for f32 and f64 (docs/SPEC-TABLES.md §3).
// A f32's pattern is a Number (32 bits fit exactly); a f64's is a BigInt,
// because 64 bits do not.
const TableBitsScratch = new DataView(new ArrayBuffer(8));

export function TableBitsToFloat(bits) {
  TableBitsScratch.setUint32(0, bits >>> 0, true);
  return TableBitsScratch.getFloat32(0, true);
}

export function TableFloatToBits(value) {
  TableBitsScratch.setFloat32(0, value, true);
  return TableBitsScratch.getUint32(0, true);
}

export function TableBitsToDouble(bits) {
  TableBitsScratch.setBigUint64(0, BigInt.asUintN(64, bits), true);
  return TableBitsScratch.getFloat64(0, true);
}

export function TableDoubleToBits(value) {
  TableBitsScratch.setFloat64(0, value, true);
  return TableBitsScratch.getBigUint64(0, true);
}

`
}

// tableRuntime is the unit's shared table runtime, emitted into ONE module per
// unit (<Package>Table.js — runtimeHome) and imported by the rest. C++ emits it into
// every header behind an include guard; an ES module is file-scoped, so a
// second copy would be a second, unequal symbol rather than a harmless
// re-inclusion.
func tableRuntime(anyKeyed bool) string {
	keyedStorage := ""
	if anyKeyed {
		keyedStorage = tableKeyedStorage
	}
	return keyedStorage + `// The table-wire read report — the permissive contract's ledger. Silence
// (all zero) means the data matched this reader's schema exactly.
export class TableReport {
  constructor() {
    this.Unknown = 0;      // unknown field ids skipped (newer data)
    this.KindMismatch = 0; // known id, changed type — skipped, never misdecoded
    this.Clamped = 0;      // out-of-range values clamped to declared bounds
    // duplicate is the TEXT FORM's counter and the WIRE NEVER RAISES IT
    // (docs/SPEC-TABLES.md §4, §16.2): a body carrying an id twice is legal input
    // whose last occurrence wins, silently. It rides on this object because a
    // caller has one report type, not two — so a wire read always leaves it
    // zero, and <Name>FromJson is what raises it.
    this.Duplicate = 0;
    this.Malformed = false; // framing damage; decode stopped, partial result kept
  }

  // A REPORT ACCUMULATES, exactly as the C++ one does: a Load adds to the
  // counters it is handed and clears nothing, so one report can ledger a whole
  // batch. A caller reusing one across reads it wants to tell apart clears it
  // here, between them.
  reset() {
    this.Unknown = 0;
    this.KindMismatch = 0;
    this.Clamped = 0;
    this.Duplicate = 0;
    this.Malformed = false;
    return this;
  }
}

// TableWriter writes the wire into the caller's Uint8Array. Nothing here
// allocates: the buffer is the caller's and the DataView is made once, with
// the writer.
//
// EVERY MULTI-BYTE STORE NAMES ITS BYTE ORDER — the ` + "`true`" + ` at each call — so a
// build on a big-endian host writes the same bytes as one on a little-endian
// host. The wire is little-endian because the wire says so, never because the
// machine is.
export class TableWriter {
  constructor(buffer) {
    if (!(buffer instanceof Uint8Array)) {
      throw new TypeError("a TableWriter writes into a Uint8Array, not " + (buffer === null ? "null" : typeof buffer));
    }
    this.Bytes = buffer;
    this.View = new DataView(buffer.buffer, buffer.byteOffset, buffer.byteLength);
    this.Offset = 0;
    this.Overflow = false;
  }

  // POINT AN EXISTING WRITER AT ANOTHER BUFFER. C++ puts its writer on the
  // stack and C# spells one as a ref struct, so neither pays for the thing
  // itself; JavaScript has no stack object, so a writer and its DataView are
  // two allocations per <Name>Save call. A caller on a per-frame path builds
  // ONE writer, points it at each buffer through this, and calls
  // <Name>SaveBody directly — which is the same code <Name>Save runs, with
  // the two objects hoisted out of the loop.
  reset(buffer) {
    if (this.Bytes.buffer !== buffer.buffer ||
        this.Bytes.byteOffset !== buffer.byteOffset ||
        this.Bytes.byteLength !== buffer.byteLength) {
      this.View = new DataView(buffer.buffer, buffer.byteOffset, buffer.byteLength);
    }
    this.Bytes = buffer;
    this.Offset = 0;
    this.Overflow = false;
    return this;
  }

  // A BYTE LOOP, and not ` + "`set(source.subarray(...))`" + `. A subarray is a new
  // TypedArray OBJECT — one heap allocation per string or bytes field — and
  // this form's rung says a load fills caller-owned storage and allocates
  // nothing. The type wire's flat tier copies a string the same way and for
  // the same reason; a byte loop over two Uint8Arrays is a plain copy with
  // nothing behind it.
  raw(source, from, length) {
    if (this.Offset + length > this.Bytes.length) { this.Overflow = true; return; }
    const bytes = this.Bytes;
    let at = this.Offset;
    for (let i = 0; i < length; i++) { bytes[at + i] = source[from + i]; }
    this.Offset = at + length;
  }

  put8(v) {
    if (this.Offset + 1 > this.Bytes.length) { this.Overflow = true; return; }
    this.View.setUint8(this.Offset, v & 0xff);
    this.Offset += 1;
  }

  put16(v) {
    if (this.Offset + 2 > this.Bytes.length) { this.Overflow = true; return; }
    this.View.setUint16(this.Offset, v & 0xffff, true);
    this.Offset += 2;
  }

  put32(v) {
    if (this.Offset + 4 > this.Bytes.length) { this.Overflow = true; return; }
    this.View.setUint32(this.Offset, v >>> 0, true);
    this.Offset += 4;
  }

  // THE BIGINT GOES STRAIGHT INTO THE VIEW. setBigUint64 wraps a negative
  // value modulo 2^64 by specification — a signed storage and an unsigned one
  // both land as their two's-complement bytes — without building a second
  // BigInt, where a BigInt.asUintN(64, v) in front of it would be one
  // allocation per 64-bit field written. The allocation gate measures this
  // line: a save allocates nothing.
  put64(v) {
    if (this.Offset + 8 > this.Bytes.length) { this.Overflow = true; return; }
    this.View.setBigUint64(this.Offset, v, true);
    this.Offset += 8;
  }

  // THE FIELD HEADER and THE LENGTH PREFIX, one call each, for the reason the
  // reader's mismatch() exists: a save body emits one block per field, and the
  // id-and-kind pair plus the open-and-patch pair are one call here rather
  // than two statements at every site. Both take Smis, so neither crosses a
  // boundary anything can box.
  field(id, kind) {
    this.put16(id);
    this.put8(kind);
  }

  // open a length prefix; close it against everything written since
  open32() {
    const at = this.Offset;
    this.put32(0);
    return at;
  }

  close32(at) {
    this.patch32(at, this.Offset - at - 4);
  }

  patch32(at, v) {
    if (at + 4 > this.Bytes.length) { this.Overflow = true; return; }
    this.View.setUint32(at, v >>> 0, true);
  }
}

// TableReader reads the wire out of the caller's Uint8Array.
//
// A NESTED BODY IS SCOPED, NOT SLICED. The reference takes a sub-span for
// every nested decode and C# a sliced ref struct; both live on the stack, and
// a JavaScript slice would be one object per nested body on a path this
// document promises allocates nothing. So the reader narrows its own ` + "`limit`" + `
// for the length of the child and the caller restores it afterwards, which
// gives the child exactly the reach a slice would: it cannot read one byte
// past its own framing.
export class TableReader {
  constructor(bytes, report) {
    if (!(bytes instanceof Uint8Array)) {
      throw new TypeError("a TableReader reads a Uint8Array, not " + (bytes === null ? "null" : typeof bytes));
    }
    this.Bytes = bytes;
    this.View = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    this.Offset = 0;
    this.Limit = bytes.length;
    this.Report = report;
  }

  // POINT AN EXISTING READER AT OTHER BYTES — the writer's twin, and for the
  // same reason: a reader and its DataView are two allocations per
  // <Name>Load call, and a caller on a per-frame path hoists both out of the
  // loop and calls <Name>LoadBody directly. The DataView is rebuilt only when
  // the buffer it covers actually moved.
  reset(bytes, report) {
    if (this.Bytes.buffer !== bytes.buffer ||
        this.Bytes.byteOffset !== bytes.byteOffset ||
        this.Bytes.byteLength !== bytes.byteLength) {
      this.View = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    }
    this.Bytes = bytes;
    this.Offset = 0;
    this.Limit = bytes.length;
    this.Report = report;
    return this;
  }

  has(count) { return this.Offset + count <= this.Limit; }

  get8() { const v = this.View.getUint8(this.Offset); this.Offset += 1; return v; }
  get16() { const v = this.View.getUint16(this.Offset, true); this.Offset += 2; return v; }
  get32() { const v = this.View.getUint32(this.Offset, true); this.Offset += 4; return v; }
  getI8() { const v = this.View.getInt8(this.Offset); this.Offset += 1; return v; }
  getI16() { const v = this.View.getInt16(this.Offset, true); this.Offset += 2; return v; }
  getI32() { const v = this.View.getInt32(this.Offset, true); this.Offset += 4; return v; }
  getU64() { const v = this.View.getBigUint64(this.Offset, true); this.Offset += 8; return v; }
  getI64() { const v = this.View.getBigInt64(this.Offset, true); this.Offset += 8; return v; }

  // THERE IS NO getF32 OR getF64 HERE, and no putF32/putF64 on the writer.
  // A double that crosses a JS call boundary is a HEAP NUMBER — sixteen bytes,
  // allocated per call — unless the callee is inlined, and whether V8 inlines
  // one depends on the size of the body around the call. So a float field is
  // read and written by the generated body itself, straight through this view
  // (docs/SPEC-TABLES.md §19.5). Every other width crosses as a Smi or a
  // BigInt, and neither is a per-call allocation.

  // A KIND MISMATCH and an UNKNOWN ID are one statement at the call site and a
  // method here, and the reason is the INLINING BUDGET (docs/SPEC-TABLES.md
  // §19.5's law, which this repo already carries for clang: "a generated codec
  // must not depend on the compiler's inlining budget"). A read body emits one
  // case per field, and five statements of counting and skipping in every case
  // would grow it. The bigger a body grows, the fewer of ITS OWN callees V8
  // will inline into it — and an un-inlined callee is where a float field's
  // sixteen bytes come from, above. Keeping the per-field bytecode small is
  // what keeps that headroom, so these two are hoisted out of every case.
  //
  // Both return false only for FRAMING DAMAGE, which is the caller's own
  // return false; a counted-and-skipped field returns true and the read goes on.
  mismatch(kind) {
    this.Report.KindMismatch++;
    if (!this.skip(kind)) { this.Report.Malformed = true; return false; }
    return true;
  }

  unknown(kind) {
    this.Report.Unknown++;
    if (!this.skip(kind)) { this.Report.Malformed = true; return false; }
    return true;
  }

  // the truncation guard, which every fixed-width read makes twice
  short(count) {
    if (this.Offset + count <= this.Limit) { return false; }
    this.Report.Malformed = true;
    return true;
  }

  // skip one payload by kind; false = framing damage
  skip(kind) {
    switch (kind) {
      case 1: case 2: case 6:
        if (!this.has(1)) { return false; }
        this.Offset += 1;
        return true;
      case 3: case 7:
        if (!this.has(2)) { return false; }
        this.Offset += 2;
        return true;
      // 17 is a NODE INDEX (docs/SPEC-TABLES.md §3.1): four bytes, so it costs one
      // arm here and a reader without the kind still skips a pointer field
      case 4: case 8: case 10: case 17:
        if (!this.has(4)) { return false; }
        this.Offset += 4;
        return true;
      case 5: case 9: case 11:
        if (!this.has(8)) { return false; }
        this.Offset += 8;
        return true;
      case 12: case 13: case 14: case 16: {
        if (!this.has(4)) { return false; }
        const n = this.get32();
        if (!this.has(n)) { return false; }
        this.Offset += n;
        return true;
      }
      case 15: { // union: u16 arm id, then the arm length-prefixed (id 0 = empty, no body)
        if (!this.has(2)) { return false; }
        if (this.get16() === 0) { return true; }
        if (!this.has(4)) { return false; }
        const n = this.get32();
        if (!this.has(n)) { return false; }
        this.Offset += n;
        return true;
      }
    }
    return false;
  }
}

`
}
