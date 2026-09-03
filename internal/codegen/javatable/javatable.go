// Package javatable emits a unit's Java table surface (docs/SPEC-TABLES.md): the
// TABLE-wire codecs in <Base>Table.java, and the two ACCELERATORS on the side
// in <Table>Block.java (§19) and <Table>Cook.java (§7) — plus the unit's
// shared runtime, one PUBLIC TYPE PER FILE, which is what Java's package scope
// is.
//
// The WIRE half is the FIXED class only: storage classes for the `table`
// declarations, then measure/save/load codecs and reflection descriptors for
// the whole TABLE CLOSURE (every table plus everything one references,
// transitively).
//
// The two ACCELERATORS reach further, because neither needs a codec: a block
// and a cook are POINTED AT, not parsed — in Java, read at explicit offsets out
// of a byte[] the consumer owns. They cover the VARIABLE class too, so a
// pointered unit's cooks open in full while its wire codecs do not exist.
//
// The C++ backend (internal/codegen/cpptable) is the REFERENCE and the C#
// backend (internal/codegen/cstable) is the worked managed-language port: this
// one mirrors their framing, their elision decisions, their clamps and their
// report events byte for byte, and invents no contract of its own. Where Java
// forces a different spelling the reason is stated at the site, and there are
// exactly five:
//
//   - THE GENERATED METHOD NAMES ARE lowerCamelCase, which is Java's one naming
//     rule and the rule this backend's own packet half already follows
//     (writeVec3, readVec3). §6.1's NAME-FIRST order is untouched — the method
//     is still the declaration's name and then the verb — so the spelling is
//     `patrolMeasure`, `patrolSave`, `patrolLoad`, `patrolReset`. Only the case
//     is the port's, and it is the language's rather than C++'s.
//   - THE UNIT'S NAMESPACE IS THE PACKAGE, and a public Java type lives in a
//     file of its own name. So the shared runtime is not "one home file" as it
//     is in C#: it is one file per runtime type (TableReport.java,
//     TableReader.java, ...), which is file-order independent by construction
//     rather than by a rule. Each of those spellings is a package-level name
//     and is claimed in internal/tablenames for every backend.
//   - THERE ARE NO UNSIGNED TYPES. A decode local is widened to the next
//     signed Java type that holds the wire kind's whole range (u8/u16 -> int,
//     u32 -> long) so a clamp compares the value the wire carried; u64 has no
//     wider type and compares through Long.compareUnsigned. Storage stays
//     bit-transparent in the same-width signed type, the packet emitter's own
//     convention.
//   - THERE ARE NO REF STRUCTS. TableReader and TableWriter are ordinary
//     objects and a nested body is bounded by MOVING THE READER'S LIMIT rather
//     than by slicing a sub-reader, so a Load of any depth allocates nothing
//     after the reader itself — which a caller may hoist and reset.
//   - THERE ARE NO POINTERS. The block and the cook are read out of a byte[]
//     at an offset the caller gives; the base's ALIGNMENT, which is a pointer
//     fact in C++ and C#, is that offset's residue. The arithmetic is
//     identical, so the refusals are.
//   - THE TWO ACCELERATORS ARE CAPPED AT A byte[], which is 2 GiB. §7 states the
//     scale it is built for in the owner's own words — "100mbs or many gigabytes
//     of data in Assets.bin" — and the scale fixtures reach a gigabyte, so this
//     is the one divergence that costs a stated requirement rather than a
//     spelling. C# meets the same int ceiling on its span overload and answers
//     it with the pointer form beside it; Java at --release 17 has no second
//     spelling to answer it with, because the foreign-memory API (MemorySegment)
//     is not stable before 22. Naming the FFM overload as the follow-on is the
//     whole of what can be done here, and it is named on the page rather than
//     left for a consumer to meet by hitting it.
//   - THERE ARE NO STRUCTS. A block row and a cooked record have no Java type
//     to lay out, so the generated accessors read each field at its offset and
//     the layout is stated as constants; §19.3's static_assert half has no Java
//     counterpart and TableBlockLayout asserts what Java can disagree about —
//     the accessors' own constants against the descriptors' (javablock.go).
//
// Storage follows the Java PACKET emitter's conventions exactly
// (internal/codegen/java): a final class with public fields, lowerCamel member
// names, string(N) and bytes(N) as a preallocated byte[N] beside an int used
// length, arrays as a preallocated T[N] beside an int used count, unions as a
// tag beside one preallocated arm per variant. That is not a free choice — a
// table's closure contains plain `type` declarations whose storage the packet
// emitter already wrote, and the table codecs decode into those very classes.
// One unit, one spelling.
//
// Nothing on the read path allocates: every buffer exists at construction, the
// caller owns the wire array and the report, and Load restores a value's
// declared defaults in place before overlaying.
//
// The VARIABLE class ON THE WIRE — the arena, the builder, the region and the
// node-table codec — is a named follow-on: a unit whose closure declares a
// pointer gets no <Base>Table.java, and the refusal is NAMED in every source
// the unit does emit rather than left as a missing symbol (§11).
package javatable

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

func tablePut(width int) string { return fmt.Sprintf("put%d", width*8) }

// javaDecodeType is the Java local one fixed-width wire kind decodes through:
// the smallest signed type that holds the kind's WHOLE range, so a clamp
// compares the value the wire carried rather than a wrapped one. u64 has no
// wider type and rides as a bit-transparent long, compared unsigned.
func javaDecodeType(kind int) string {
	switch kind {
	case tkI8, tkI16, tkI32, tkU8, tkU16:
		return "int"
	}
	return "long"
}

// javaDecodeUnsigned reports whether a decode local carries an unsigned value
// that its Java type cannot order — u64 alone.
func javaDecodeUnsigned(kind int) bool { return kind == tkU64 }

// generatedFrom is the DO NOT EDIT banner's first line. A runtime type the
// unit has no schema file for is generated FOR THE UNIT, and says so.
func generatedFrom(base string, u *ir.Unit) string {
	if base == "" {
		return fmt.Sprintf("// Code generated by the schema compiler for package %s. DO NOT EDIT.\n", u.Package)
	}
	return fmt.Sprintf("// Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", base)
}

// license is the SPDX exception every generated file carries.
const license = "// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n" +
	"// your choice. See the LICENSE exception in the schema compiler; the compiler is\n" +
	"// AGPL-3.0, its output is not.\n"

type tableGen struct {
	unit     *ir.Unit
	file     *ir.File
	anyKeyed bool       // the unit declares at least one enum-keyed array
	owner    *ir.Struct // the closure member whose codec is being emitted
	types    strings.Builder
	body     strings.Builder
	indent   string // extra per-line indent while emitting inside a branch guard
}

// tf prints into the nested-storage region.
func (g *tableGen) tf(format string, args ...any) {
	fmt.Fprintf(&g.types, format, args...)
}

// pf prints into the static-method region, honoring the guard indent.
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

// Generate emits the unit's Java table surface, and nothing when the unit
// declares no table: a table-free unit's generated Java is byte-identical with
// or without this package.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	if len(u.Tables) == 0 {
		return map[string][]byte{}, nil
	}
	if err := ir.RefuseWideTableKinds(u, "Java"); err != nil {
		return nil, err
	}
	if err := checkNames(u); err != nil {
		return nil, err
	}
	// The two ACCELERATORS are emitted ON THE SIDE and neither needs a wire
	// codec: the BLOCK form (§19) reads bytes a producer wrote and the COOK
	// (§7) reads a region the tooling wrote. Both are pure readers over a
	// byte[] the consumer owns, so both reach further than this backend's wire
	// half does — a unit whose variable class the wire cannot spell still gets
	// its cooks opened.
	blocks := ir.Blocks(u)
	ck := cookUnitOf(u)
	set := collectRecords(u, blocks, ck)
	out := map[string][]byte{
		"TableBytes.java":   tableBytesFile(u),
		"BuildVersion.java": buildVersionFile(u),
	}
	maps.Copy(out, emitRowFiles(u, set, blocks, ck))
	withBlock := anyBlockForm(u, blocks)
	if withBlock {
		blockFiles, err := generateBlockFiles(u, blocks)
		if err != nil {
			return nil, err
		}
		maps.Copy(out, blockFiles)
		out["TableBlockLayout.java"] = emitBlockLayoutFile(u, blocks, set)
	}
	cooks, err := generateCookFiles(u, ck)
	if err != nil {
		return nil, err
	}
	maps.Copy(out, cooks)
	out["TableCookLayout.java"] = emitCookLayoutFile(u, ck, set, withBlock)
	// The VARIABLE-CLASS refusal (docs/SPEC-TABLES.md §2.2, §11) is a refusal of
	// the WIRE SURFACE, which is the half the variable class is missing: no
	// arena, no builder, no region and no node-table codec. It is named rather
	// than silent — every generated Cook and Block file of the unit opens with
	// the banner below, naming each table and the follow-on — and no
	// <Base>Table.java is emitted, so a consumer that reaches for Save or Load
	// gets a missing name from its own compiler beside a file that says why.
	if names := variableTableNames(u); len(names) > 0 {
		banner := []byte(variableClassBanner(names))
		for name, data := range out {
			out[name] = append(append([]byte(nil), banner...), data...)
		}
		return out, nil
	}
	closure := ir.TableClosure(u)
	anyKeyed := unitHasKeyedArray(u, closure)
	maps.Copy(out, wireRuntimeFiles(u, anyKeyed))
	out["TableJson.java"] = []byte(jsonWalkFile(u))
	// the identity pair of every enum in the closure lands in ONE file each:
	// Java's unit scope is the package, so a second definition anywhere in it
	// is a duplicate class rather than C++'s harmless re-inclusion behind a
	// guard
	usedEnums := closureEnums(u, closure)
	out["TableEnumId.java"] = emitEnumIdentity(u, usedEnums, true)
	out["TableEnumValue.java"] = emitEnumIdentity(u, usedEnums, false)
	for _, f := range u.Files {
		g := &tableGen{unit: u, file: f, anyKeyed: anyKeyed}
		var members []*ir.Struct
		members = append(members, f.Tables...)
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok && closure[st.Name] {
				members = append(members, st)
			}
		}
		if len(members) == 0 {
			continue
		}
		for _, st := range members {
			if st.IsTable {
				g.owner = st
				g.emitTableClass(st)
			}
		}
		for _, st := range members {
			g.owner = st
			g.emitTableReset(st)
			g.emitTableMeasure(st)
			g.emitTableWrite(st)
			g.emitTableSave(st)
			g.emitTableRead(st)
		}
		g.pf("// ---- reflection descriptors (tables only, docs/SPEC-TABLES.md §8) ----\n\n")
		for _, st := range members {
			g.owner = st
			g.emitTableDescriptor(st)
		}
		// and the TEXT FORM's per-member surface: three thin wrappers over the
		// generic walk, each naming a descriptor and nothing else (§16.1)
		g.pf("// ---- the text form: JSON in and out of one table (docs/SPEC-TABLES.md §16) ----\n\n")
		for _, st := range members {
			g.owner = st
			g.emitJsonSurface(st)
		}
		out[f.Base+"Table.java"] = g.assemble()
	}
	return out, nil
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
	return "// THE JAVA WIRE SURFACE OF THIS UNIT IS REFUSED, BY NAME (docs/SPEC-TABLES.md §11).\n" +
		"//\n" +
		"// It declares variable-length tables (" + englishList(names) + "), and the Java table\n" +
		"// backend's VARIABLE CLASS — the arena, the builder, the region and the node-table\n" +
		"// codec — is a named follow-on (§15). No <Base>Table.java is emitted for this unit,\n" +
		"// so a consumer reaching for Measure, Save or Load gets a missing name from its own\n" +
		"// compiler, beside this file, which says why.\n" +
		"//\n" +
		"// What IS emitted is the two ACCELERATORS, because neither needs a codec: a block\n" +
		"// (§19) and a cook (§7) are read where they lie, not parsed. A build that loads this\n" +
		"// unit's cooked assets is served in full; one that wants the tolerant wire is not,\n" +
		"// and runs the tool or the C++ backend for it.\n\n"
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
func closureEnums(u *ir.Unit, closure map[string]bool) []*ir.Enum {
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
	ordered := make([]string, 0, len(used))
	for name := range used {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	out := make([]*ir.Enum, 0, len(ordered))
	for _, name := range ordered {
		out = append(out, used[name])
	}
	return out
}

func (g *tableGen) assemble() []byte {
	var h strings.Builder
	h.WriteString(generatedFrom(g.file.Base, g.unit))
	h.WriteString(license)
	fmt.Fprintf(&h, "// package %s — the TABLE wire (docs/SPEC-TABLES.md): evolution-tolerant, neutral\n", g.unit.Package)
	h.WriteString("// bytes, no serialize dependency. Tables version by field id, never by the\n")
	h.WriteString("// unit's protocol id.\n")
	h.WriteString("//\n")
	h.WriteString("// Measure/Save/Load are name-first static methods on this class (Java has no\n")
	h.WriteString("// top-level functions): <Name>Measure gives the exact wire size, <Name>Save\n")
	h.WriteString("// writes exactly that many bytes into the caller's array, <Name>Load overlays a\n")
	h.WriteString("// value in place and reports every tolerance event. Nothing here allocates: the\n")
	h.WriteString("// caller owns the value, the array and the report.\n")
	h.WriteString("\n")
	fmt.Fprintf(&h, "package %s;\n\n", g.unit.Package)
	fmt.Fprintf(&h, "// %sTable carries every table declaration of %s.schema and every table codec of\n", g.file.Base, g.file.Base)
	h.WriteString("// its closure — Java has no top-level functions, so the file's one public class\n")
	h.WriteString("// is their home; table storage nests inside it (SPEC §6.1 naming).\n")
	fmt.Fprintf(&h, "public final class %sTable {\n", g.file.Base)
	fmt.Fprintf(&h, "    private %sTable() {}\n\n", g.file.Base)
	h.WriteString(indent4(g.types.String()))
	h.WriteString(indent4(g.body.String()))
	h.WriteString("}\n")
	return []byte(h.String())
}

func indent4(s string) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	var b strings.Builder
	for line := range strings.SplitSeq(s, "\n") {
		if line == "" {
			b.WriteString("\n")
			continue
		}
		b.WriteString("    ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	return b.String()
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// javaName maps an exported member/constant/variant name into Java's
// lowerCamelCase — the packet emitter's own mapping, so one field is spelled
// one way across a unit.
func javaName(name string) string { return lowerFirst(ir.GoExportName(name)) }

// unitHasKeyedArray reports whether any closure member declares an enum-keyed
// array, which is what decides whether the unit carries the keyed slot rule.
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

// ---- qualification: a name's Java spelling from anywhere in the package ----
//
// Java's unit scope is the PACKAGE and a nested type is reached through its
// owner, so every reference is spelled with the owner even inside that owner.
// One rule, no per-file special case, and the emitted text does not change
// when a declaration moves file.

// declFile is the basename of the schema file that declares a name.
func (g *tableGen) declFile(name string) string { return g.unit.DeclFile[name] }

// ref is the Java spelling of a declaration's STORAGE type: a `table`'s class
// nests in <Base>Table, everything else in the packet emitter's <Base>.
func (g *tableGen) ref(name string) string {
	base := g.declFile(name)
	if g.unit.Tables[name] != nil {
		return base + "Table." + name
	}
	return base + "." + name
}

// fnRef is the Java spelling of a closure member's generated static methods —
// they live on the class of the file that declares the member.
func (g *tableGen) fnRef(name string) string { return g.declFile(name) + "Table." }

// method is one generated method's own spelling: NAME-FIRST as §6.1 requires,
// lowerCamelCase as Java requires. `Patrol` + `Measure` is `patrolMeasure`.
//
// The two halves of this backend's output agree on it: the packet emitter
// spells `writeVec3` and `zeroVec3`, and a table's codecs sit in the same
// package beside them. GoExportName and lowerFirst are a bijection, so the
// checker's PascalCase claim over `<Name>Measure` covers `<name>measure`'s
// spelling too and no second registry is needed (§11).
func method(name, verb string) string { return lowerFirst(name) + verb }

// call is `method` reached from anywhere in the package: the declaring file's
// table class, then the method.
func (g *tableGen) call(name, verb string) string { return g.fnRef(name) + method(name, verb) }

// packetRef is the Java spelling of an enum or flags namespace, which the
// PACKET emitter owns.
func (g *tableGen) packetRef(name string) string { return g.declFile(name) + "." + name }

// tagRef is the Java spelling of a union's TAG namespace, <Union>Type. The
// namespace is generated rather than declared, so it has no DeclFile entry of
// its own and is qualified by the union that owns it.
func (g *tableGen) tagRef(union string) string {
	return g.declFile(union) + "." + union + "Type"
}

// ---- name checks: what Java's one-public-type-per-file rule can collide ----

// runtimeFileNames is every public type this backend puts at package scope, so
// a schema FILE whose basename lands on one can be refused by name rather than
// clobbering it. (A DECLARATION named one of these is refused by the checker,
// from internal/tablenames; a FILE is this backend's own hazard, because a
// file base is what names the packet emitter's class.)
func runtimeFileNames() []string {
	return []string{
		"TableReport", "TableWriter", "TableReader", "TableKeyed",
		"TableTypeInfo", "TableFieldInfo", "TableUnionInfo", "TableUnionArmInfo",
		"TableJson", "TableEnumId", "TableEnumValue",
		"TableBlockRows", "TableBlockInfo", "TableBlockFieldInfo", "TableBlockLayout",
		"TableCookInfo", "TableCookFieldInfo", "TableCookStorage", "TableCookLayout",
		"BuildVersion",
	}
}

// checkNames refuses the collisions Java's file-per-public-type rule creates
// and the reserved words a table's own field names could land on. The packet
// emitter runs the same check over the packet declarations; tables are absent
// from that stream, so they are checked here.
func checkNames(u *ir.Unit) error {
	runtime := map[string]bool{}
	for _, name := range runtimeFileNames() {
		runtime[name] = true
	}
	for _, f := range u.Files {
		if runtime[f.Base] {
			return fmt.Errorf("schema file %s.schema collides with the %s.java runtime type the Java table backend writes for a unit with tables (one public class per file); rename the file (docs/SPEC-TABLES.md §11)", f.Base, f.Base)
		}
	}
	names := make([]string, 0, len(u.Tables))
	for name := range u.Tables {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, f := range u.Tables[name].Fields {
			if javaReserved[javaName(f.Name)] {
				return fmt.Errorf("field %s of table %s maps to the reserved Java identifier %q; rename it", f.Name, name, javaName(f.Name))
			}
		}
	}
	return nil
}

// javaReserved is Java's keyword set plus the literals, which are equally
// unusable as identifiers.
var javaReserved = map[string]bool{
	"abstract": true, "assert": true, "boolean": true, "break": true, "byte": true,
	"case": true, "catch": true, "char": true, "class": true, "const": true,
	"continue": true, "default": true, "do": true, "double": true, "else": true,
	"enum": true, "extends": true, "final": true, "finally": true, "float": true,
	"for": true, "goto": true, "if": true, "implements": true, "import": true,
	"instanceof": true, "int": true, "interface": true, "long": true, "native": true,
	"new": true, "package": true, "private": true, "protected": true, "public": true,
	"return": true, "short": true, "static": true, "strictfp": true, "super": true,
	"switch": true, "synchronized": true, "this": true, "throw": true, "throws": true,
	"transient": true, "try": true, "void": true, "volatile": true, "while": true,
	"true": true, "false": true, "null": true, "_": true,
}
