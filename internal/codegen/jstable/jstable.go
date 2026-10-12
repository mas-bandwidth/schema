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
//     behaviour is identical: a nested decode cannot read past its own
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
//     does not get that licence: a block array's `offset_of` and a cook
//     reference's delta are both composed from TWO 32-BIT READS instead. Those
//     are the two hottest lines the accelerators have — once per row addressed,
//     once per edge followed — and a `getBigUint64` there would allocate per
//     row and per edge. Open has already bounded both, in BigInt, once, where a
//     forgery near 2^63 must refuse; by the time a row is addressed the value
//     fits a Number exactly.
//   - A ROW'S 64-BIT FIELD reads as a BigInt through its accessor, one
//     allocation per read, on the same licence as the wire's. A `flags` field
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
	if err := ir.RefuseWideTableKinds(u, "JavaScript"); err != nil {
		return nil, err
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

// Generate emits <Base>Block.js and <Base>Cook.js for every unit file when the
// unit declares tables, and nothing when it does not — a table-free unit's
// generated JavaScript is byte-identical with or without this package.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	return GenerateLineage(u, nil)
}

// GenerateLineage is [Generate] with the LINEAGE the build holds for each fixed
// table: the locked layouts, OLDEST FIRST, the current one last
// (docs/FIXED-FORM-ALGORITHM.md §5.2, §5.9 #1). A nil map is a build with no
// lock — a unit that was never locked promises nothing — and every table then
// carries the one entry it can always compute: its own.
//
// THE BACKEND OPENS NO FILE: `lockfile.Open`, `lockfile.Lineage` and
// `lockfile.Floor` are the CALLER's three calls, so the disk is read in one
// place and a test can play the lock in one line.
func GenerateLineage(u *ir.Unit, lineage map[string][]FixedLineageEntry) (map[string][]byte, error) {
	if len(u.Tables) == 0 {
		return map[string][]byte{}, nil
	}
	// §15's WIDE-KIND REFUSAL IS THE FORM-1 ACCELERATORS' AND NOT THE WIRE'S
	// (ir.WideTableKinds). The rule lives in ir because a rule five ports each
	// spell for themselves is five rules — and the day this backend gained
	// §3.4's fixed form is the day the block form and the cooked form stood down
	// alone, with the unit NOT refused whole because there is a wire left to
	// emit. The refusal still names every wide field, and it is stated by name in
	// every module this unit gets.
	//
	// THE ANSWER IS THIS LEG'S OWN ROOTS, the way java and elixir answer it
	// (len(jsFixedUnitRoots) > 0), and not a hard-coded `true`: a unit this leg lays no
	// fixed table out for has no wire left to put in the accelerators' place, and
	// `true` would have told ir otherwise and let the unit through unrefused.
	scope := ir.WideTableKinds(u, "JavaScript", len(jsFixedUnitRoots(u)) > 0)
	if scope.Unit {
		return nil, scope.Refusal
	}
	sort.Strings(names)
	return names
}

// tableNeedsVariableWire reports whether a table's by-value closure reaches a
// construct whose body is variable size on the id-table wire: a pointer, a
// `*bytes`/`*string` byte buffer, a map, or an unbounded array (docs/SPEC-TABLES.md
// §2.5, §2.8, §2.9). A table without one has a fixed-size body and rides the
// ordinary codec.
func tableNeedsVariableWire(st *ir.Struct, seen map[string]bool) bool {
	if st == nil || seen[st.Name] {
		return false
	}
	// A HANDED ENTRY THAT IS A LOCK BUG FAILS THE BUILD, before a line is
	// emitted (§5.9 #8, #26): a layout that does not parse, or a record size
	// that is not what the entry's own layout accounts for, is never a wire
	// event and never a refusal at the first file that matches its hash.
	if err := refuseFixedLineage(u, lineage); err != nil {
		return nil, err
	}
	// THE WIDE KINDS (docs/SPEC-TABLES.md §15) ARE A REFUSAL OF THE
	// ACCELERATORS, NOT OF THE FIXED FORM. A block row and a cooked node are
	// laid out in the ID-TABLE's kind vocabulary, which has no fixed-point and
	// no 128-bit kind in this backend yet (schema#366) — but §3.4's fixed form
	// carries both by its own constant-size table, a `fixed(I, F)` riding as
	// the raw scaled integer at its storage width and an `int128`/`uint128` as
	// sixteen bytes, the low half then the high. So the refusal is SCOPED to
	// the two accelerators and stated by name in every module this unit gets,
	// rather than taken out on a form that carries the kinds fine.
	wide := ir.TableWideFields(u)
	out := map[string][]byte{}
	if len(wide) == 0 {
		blocks := ir.Blocks(u)
		var err error
		out, err = generateBlockFiles(u, blocks)
		if err != nil {
			return nil, err
		}
		cooks, err := generateCookFiles(u, blocks)
		if err != nil {
			return nil, err
		}
		maps.Copy(out, cooks)
	}
	// THE VARIABLE FORM (docs/SPEC-TABLES.md §3.1), form byte 1: tolerant
	// wire reading and canonical byte layout for card constructs.
	variable, err := generateVariableFiles(u, wide)
	if err != nil {
		return nil, err
	}
	maps.Copy(out, variable)
	// THE FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3: this backend's
	// second table wire.
	fixed, err := generateFixedFiles(u, wide, lineage)
	if err != nil {
		return nil, err
	}
	maps.Copy(out, fixed)
	// A unit whose wide kinds cost it the accelerators AND that has no fixed
	// form to put in their place has nothing to emit, so it is refused whole
	// and by name, exactly as it was before the form arrived.
	if len(wide) > 0 && len(fixed) == 0 {
		return nil, scope.Refusal
	}
	return out, nil
}
