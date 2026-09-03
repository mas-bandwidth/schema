// Package darttable emits a unit's Dart table surface (docs/SPEC-TABLES.md): the
// TABLE-wire codecs, the reflection descriptors and the text form in
// <Base>Table.dart, one file per unit file, emitted only when the unit
// declares tables — plus the unit's ONE RUNTIME HOME, <Package>Table.dart,
// where everything shared lands.
//
// THE READING TIER. This backend is the FIXED class on the wire: storage
// classes for the `table` declarations, then measure/save/load codecs,
// reflection descriptors and the JSON text form for the whole TABLE CLOSURE.
// The VARIABLE class on the wire — the arena, the builder, the region and the
// node-table codec — is a named follow-on, refused BY NAME exactly as the C#
// backend refuses it (§11).
//
// The C++ backend (internal/codegen/cpptable) is the REFERENCE: this port
// mirrors its framing, its elision decisions, its clamps and its report
// events byte for byte, and invents no contract of its own. Where Dart forces
// a different spelling the reason is stated at the site, and there are exactly
// four:
//
//   - THE READER AND WRITER ARE OBJECTS. Dart has no value types, so the two
//     carry an `attach` and a caller may own one; a nested body narrows a
//     `limit` rather than taking a sub-view, because a Dart sub-view is an
//     allocation and a C++ one is not (runtime.go).
//   - A FLOAT32 FIELD IS A DOUBLE, so every elision comparison narrows first
//     (tableNarrowFloat) — which is what gives the decision C's own float
//     semantics rather than the double's, and what makes a Dart wire byte
//     equal the C++ one.
//   - THE DESCRIPTORS ARE `const`, and their memory columns are STATIC
//     METHODS of a per-type <Name>TableFields class rather than C++'s offset
//     and width: a Dart field has no address. A tear-off of a static method is
//     a compile-time constant, so the whole descriptor graph lands in the
//     binary's constant pool and a walk allocates nothing and initializes
//     nothing. C# reached the same place with per-field closures, which cannot
//     be const.
//   - THE ENUM VOCABULARIES are static const members of TableEnumVocab rather
//     than C#'s overload set, because Dart has neither overloading nor a
//     non-int enum on this wire — and as members they claim not one name a
//     schema could declare.
//
// Nothing on the read path allocates per FIELD: every buffer exists at
// construction, the caller owns the bytes and the report, and Load overlays a
// value in place after restoring its declared defaults.
package darttable

import (
	"fmt"
	"maps"
	"math/big"
	"sort"
	"strconv"
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

func tableScalarKind(f *ir.Field) int { return ir.TableScalarKind(f) }

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
func tableGet(width int) string { return fmt.Sprintf("get%d", width*8) }

// dartReserved is the Dart surface a generated spelling must not land on. It
// is the packet emitter's list plus the members every object inherits, which
// matter here because the per-enum vocabularies are static MEMBERS of
// TableEnumVocab and the per-type accessors are static members of
// <Name>TableFields.
var dartReserved = map[string]bool{
	"assert": true, "break": true, "case": true, "catch": true,
	"class": true, "const": true, "continue": true, "default": true,
	"do": true, "else": true, "enum": true, "extends": true, "false": true,
	"final": true, "finally": true, "for": true, "if": true, "in": true,
	"is": true, "new": true, "null": true, "rethrow": true, "return": true,
	"super": true, "switch": true, "this": true, "throw": true, "true": true,
	"try": true, "var": true, "void": true, "when": true, "while": true,
	"with": true,
	"bool": true, "double": true, "int": true, "num": true, "String": true,
	"List": true, "Object": true, "Endian": true, "ByteData": true,
	"dynamic": true,
	// Object's members: a static member sharing one of these names is a
	// declaration error in Dart, and these are reachable from an enum name
	// (an enum `HashCode` spells the member `hashCode`).
	"hashCode": true, "runtimeType": true, "toString": true,
	"noSuchMethod": true,
}

// dartName maps a schema member/variant name into Dart's lowerCamelCase: the
// first-letter-lowered form of ir.GoExportName. It is the packet emitter's
// mapping, spelled again here because the two packages share no unexported
// helper — and it must stay the same mapping, since the two emitters name the
// same fields of the same storage classes.
func dartName(name string) string {
	s := ir.GoExportName(name)
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// lowerFirst lowers an already-exported name: RootConfig -> rootConfig, which
// is how the name-first table surface spells itself in Dart.
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// runtimeHome is the basename of the file carrying the unit's shared Dart
// table runtime, and it is THE PACKAGE — the same rule the C# backend holds,
// for a related reason: any rule that picks a FILE picks it off the file
// order and relocates the runtime the day the unit gains a file that sorts
// earlier. Dart differs from C# in that a library is a file, so every other
// <Base>Table.dart IMPORTS this one rather than merely compiling beside it.
func runtimeHome(u *ir.Unit) string {
	home := capitalize(u.Package)
	for _, f := range u.Files {
		if strings.EqualFold(f.Base, home) {
			return f.Base
		}
	}
	return home
}

// runtimeHomeMarker rides the banner of the runtime home, so "which file
// carries this unit's shared runtime" is a grep rather than a line count.
const runtimeHomeMarker = "the unit's shared runtime lives here"

type tableGen struct {
	unit     *ir.Unit
	file     *ir.File // nil in the emitted-for-the-unit runtime file
	home     bool     // this file carries the unit's shared table runtime
	homeBase string   // the runtime home's basename, for the import
	anyKeyed bool
	owner    *ir.Struct
	body     strings.Builder
	// imports: file base -> symbol set, and the typed-data need
	imports  map[string]map[string]bool
	needData bool
	indent   string
}

func (g *tableGen) pf(format string, args ...any) {
	s := fmt.Sprintf(format, args...)
	if g.indent != "" && s != "" {
		trailing := strings.HasSuffix(s, "\n")
		if trailing {
			s = s[:len(s)-1]
		}
		// a line is indented where it BEGINS: the first piece of a line that
		// an earlier call left open continues that line and takes no indent
		atLineStart := g.body.Len() == 0 || strings.HasSuffix(g.body.String(), "\n")
		lines := strings.Split(s, "\n")
		for i, line := range lines {
			// a blank line stays blank: an indented one is trailing
			// whitespace, which `dart format` removes and the gate then reads
			// as drift
			if line != "" && (i > 0 || atLineStart) {
				lines[i] = g.indent + line
			}
		}
		s = strings.Join(lines, "\n")
		if trailing {
			s += "\n"
		}
	}
	g.body.WriteString(s)
}

// need records a symbol this library references from another generated
// library. `library` is the emitted file name, which is what a Dart import
// spells.
func (g *tableGen) need(library, symbol string) {
	if g.imports[library] == nil {
		g.imports[library] = map[string]bool{}
	}
	g.imports[library][symbol] = true
}

// needDecl records a reference to a PACKET-emitted declaration (an enum, a
// union's tag class, a closure `type`'s storage class), which lives in
// <Base>.dart — always a different library from this one, so the import is
// unconditional.
func (g *tableGen) needDecl(name string) {
	base := g.unit.DeclFile[name]
	if base == "" {
		return
	}
	g.need(base+".dart", name)
}

// needUnionTag records the reference to a union's generated tag class, which
// the packet emitter puts in the union's own file under <Name>Type.
func (g *tableGen) needUnionTag(un *ir.Union) {
	base := g.unit.DeclFile[un.Name]
	if base == "" {
		return
	}
	g.need(base+".dart", un.Name+"Type")
}

// needTable records a reference to a TABLE-emitted symbol, skipped when this
// file is the one that carries it.
func (g *tableGen) needTable(declName, symbol string) {
	base := g.unit.DeclFile[declName]
	if base == "" || (g.file != nil && base == g.file.Base) {
		return
	}
	g.need(base+"Table.dart", symbol)
}

// Generate emits <Base>Table.dart for every unit file when the unit declares
// tables — plus <Package>Table.dart, the unit's one runtime home, when no file
// is named for the package — and nothing when it declares none: a table-free
// unit's generated Dart is byte-identical with or without this package.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	if len(u.Tables) == 0 {
		return map[string][]byte{}, nil
	}
	if err := ir.RefuseWideTableKinds(u, "Dart"); err != nil {
		return nil, err
	}
	if err := checkNames(u); err != nil {
		return nil, err
	}
	// The BLOCK form is emitted ON THE SIDE and needs no wire codec: a block is
	// POINTED AT, not parsed. It reaches further than the wire half does — a
	// unit whose variable class the wire cannot spell still gets its blocks
	// opened.
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
	// THE VARIABLE-CLASS REFUSAL (docs/SPEC-TABLES.md §2.2, §11), named rather than
	// silent: a unit whose closure declares a pointer gets no Dart table
	// source at all, and the refusal names each table and the follow-on.
	if names := variableTableNames(u); len(names) > 0 {
		home := runtimeHome(u)
		out[home+"Table.dart"] = []byte(variableClassBanner(u, names))
		return out, nil
	}
	closure := ir.TableClosure(u)
	home := runtimeHome(u)
	anyKeyed := unitHasKeyedArray(u, closure)
	runtimeWritten := false
	for _, f := range u.Files {
		g := newGen(u, f, f.Base == home, home, anyKeyed)
		members := closureMembers(f, closure)
		if g.home {
			g.emitRuntime()
			runtimeWritten = true
		}
		for _, st := range members {
			g.owner = st
			g.emitMember(st)
		}
		if len(members) > 0 {
			g.pf("// ---- reflection descriptors (tables only, docs/SPEC-TABLES.md §8) ----\n\n")
			for _, st := range members {
				g.owner = st
				g.emitTableDescriptor(st)
			}
		}
		if g.body.Len() > 0 {
			out[f.Base+"Table.dart"] = g.assemble()
		}
	}
	// No file of the unit is named for the package, so the home is emitted for
	// the unit rather than for a file.
	if !runtimeWritten {
		g := newGen(u, nil, true, home, anyKeyed)
		g.emitRuntime()
		out[home+"Table.dart"] = g.assemble()
	}
	return out, nil
}

func newGen(u *ir.Unit, f *ir.File, home bool, homeBase string, anyKeyed bool) *tableGen {
	return &tableGen{
		unit: u, file: f, home: home, homeBase: homeBase,
		anyKeyed: anyKeyed, imports: map[string]map[string]bool{},
	}
}

// closureMembers is the file's tables plus the closure `type`s it declares,
// tables first — the same order the C# backend walks, so the two emitted
// surfaces read as translations of one another.
func closureMembers(f *ir.File, closure map[string]bool) []*ir.Struct {
	var members []*ir.Struct
	members = append(members, f.Tables...)
	for _, d := range f.Decls {
		if st, ok := d.(*ir.Struct); ok && closure[st.Name] {
			members = append(members, st)
		}
	}
	return members
}

func (g *tableGen) emitRuntime() {
	g.needData = true
	g.pf("%s", tableRuntimeSource(g.anyKeyed, g.enumVocabularies()))
}

// enumVocabularies is every vocabulary the unit's table closure names — an
// enum's values, a flags mask's BITS, a union's arms — as static const members
// of TableEnumVocab. They land in the runtime home rather than in the file that
// declares the type: Dart has no partial class, so one class means one file,
// and a vocabulary is pure data with no dependency on the declaring library (a
// value rides as a plain int).
//
// A FLAGS vocabulary carries NO IDS, and that is not an omission: a flags
// variant is a bit position and rides under no wire id (docs/SPEC-TABLES.md §4).
// The missing list is what tells a flags mask from an enum at runtime.
func (g *tableGen) enumVocabularies() string {
	vocabs := closureVocabularies(g.unit)
	if len(vocabs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n  // The closure's vocabularies (docs/SPEC-TABLES.md §5), one per named set.\n")
	b.WriteString("  // They are MEMBERS rather than library-scope constants so they claim no\n")
	b.WriteString("  // name a schema could declare (§11).\n")
	for _, v := range vocabs {
		ids := "<int>[" + strings.Join(v.ids, ", ") + "]"
		names := "<String>[" + strings.Join(v.names, ", ") + "]"
		head := fmt.Sprintf("  static const %s = TableEnumVocab(", dartName(v.name))
		// the formatter's own three shapes, in the order it tries them: one
		// line; the LAST argument block-formatted, which it will do only when
		// every other argument is simple (an empty list is, a filled one is
		// not); and otherwise one argument per line
		if len(head)+len(ids)+len(names)+3 <= 80 {
			fmt.Fprintf(&b, "%s%s, %s);\n", head, ids, names)
			continue
		}
		if len(v.ids) == 0 {
			fmt.Fprintf(&b, "%s%s, <String>[\n", head, ids)
			for _, n := range v.names {
				fmt.Fprintf(&b, "    %s,\n", n)
			}
			b.WriteString("  ]);\n")
			continue
		}
		fmt.Fprintf(&b, "%s\n", head)
		fmt.Fprintf(&b, "    %s,\n", ids)
		fmt.Fprintf(&b, "    %s,\n", names)
		b.WriteString("  );\n")
	}
	return b.String()
}

// vocabulary is one named set the descriptors index by value: its wire ids
// (empty for a flags mask) and its names.
type vocabulary struct {
	name  string
	ids   []string
	names []string
}

// closureVocabularies is every enum, flags and union the unit's table closure
// names, sorted — the order is the emitted file's, so regeneration is
// byte-stable.
func closureVocabularies(u *ir.Unit) []vocabulary {
	closure := ir.TableClosure(u)
	used := map[string]ir.Decl{}
	for _, name := range sortedKeys(closure) {
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			if f.KeyEnumRef != nil {
				used[f.KeyEnumRef.Name] = f.KeyEnumRef
			}
			if f.Type.Kind != ir.TNamed {
				continue
			}
			switch ref := f.Type.Ref.(type) {
			case *ir.Enum:
				used[ref.Name] = ref
			case *ir.Flags:
				used[ref.Name] = ref
			case *ir.Union:
				if f.Array == ir.ArrayNone {
					used[ref.Name] = ref
				}
			}
		}
	}
	var out []vocabulary
	for _, name := range sortedKeys(used) {
		switch d := used[name].(type) {
		case *ir.Enum:
			v := vocabulary{name: name, ids: []string{"0"}, names: []string{"'None'"}}
			for _, variant := range d.Variants {
				v.ids = append(v.ids, fmt.Sprintf("0x%04x", ir.VariantId(variant)))
				v.names = append(v.names, "'"+variant+"'")
			}
			out = append(out, v)
		case *ir.Flags:
			// a flags mask is the wire's one POSITIONAL vocabulary: its
			// variants are BIT POSITIONS, so there is no variant id
			v := vocabulary{name: name}
			for _, variant := range d.Variants {
				v.names = append(v.names, "'"+variant+"'")
			}
			out = append(out, v)
		case *ir.Union:
			v := vocabulary{name: name, ids: []string{"0"}, names: []string{"'None'"}}
			for _, variant := range d.Variants {
				v.ids = append(v.ids, fmt.Sprintf("0x%04x", ir.VariantId(variant.Name)))
				v.names = append(v.names, "'"+variant.Name+"'")
			}
			out = append(out, v)
		}
	}
	return out
}

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
// meets it. The Dart reading tier has no arena, no builder, no region and no
// node-table codec, so it emits no source for such a unit — one file remains,
// carrying the reason, so a consumer reaching for Save or Load meets an
// explanation rather than a missing name with none.
func variableClassBanner(u *ir.Unit, names []string) string {
	return "// Code generated by the schema compiler for package " + u.Package + ". DO NOT EDIT.\n" +
		"//\n" +
		"// THE DART TABLE SURFACE OF THIS UNIT IS REFUSED, BY NAME (docs/SPEC-TABLES.md §11).\n" +
		"//\n" +
		"// It declares variable-length tables (" + englishList(names) + "), and the Dart\n" +
		"// table backend is the READING TIER: the fixed class on the wire, the JSON text\n" +
		"// form and the reflection descriptors. The VARIABLE class — the arena, the\n" +
		"// builder, the region and the node-table codec — is a named follow-on (§15).\n" +
		"//\n" +
		"// No codec is emitted for this unit, so a consumer reaching for Measure, Save or\n" +
		"// Load gets a missing name from its own compiler, beside this file, which says\n" +
		"// why. A build that wants the tolerant wire for these tables runs the tool or\n" +
		"// the C++ backend for it.\n"
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

// checkNames refuses a declaration whose Dart table spelling would land on a
// reserved word or an inherited member — the same refusal the packet emitter
// gives, over the surface this backend adds.
func checkNames(u *ir.Unit) error {
	for _, name := range sortedKeys(u.Tables) {
		st := u.Tables[name]
		if dartReserved[st.Name] {
			return fmt.Errorf("table %s maps to the reserved Dart identifier %q; rename it", st.Name, st.Name)
		}
		for _, f := range st.Fields {
			if dartReserved[dartName(f.Name)] {
				return fmt.Errorf("field %s of table %s maps to the reserved Dart identifier %q; rename it",
					f.Name, st.Name, dartName(f.Name))
			}
		}
	}
	// a vocabulary is a static MEMBER of TableEnumVocab named by the type it
	// spells, so a type whose Dart spelling is an inherited member cannot be
	// named there
	for _, v := range closureVocabularies(u) {
		if dartReserved[dartName(v.name)] {
			return fmt.Errorf("%s maps to the reserved Dart identifier %q, which the table-wire vocabulary cannot be named; rename it",
				v.name, dartName(v.name))
		}
	}
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ---- storage ----

// emitTableClass writes a table's storage class. The conventions are the Dart
// PACKET emitter's exactly (internal/codegen/dart): a `final class` with
// public fields, string(N) and bytes(N) as a preallocated Uint8List beside an
// int used length, arrays as a preallocated typed list or List<T> beside an
// int used count, a nested table by value. That is not a free choice — a
// table's closure contains plain `type` declarations whose storage the packet
// emitter already wrote, and these codecs decode into those very classes.
func (g *tableGen) emitTableClass(st *ir.Struct) {
	g.pf("/// table %s — TABLE-wire storage: public fields, every buffer allocated at\n", st.Name)
	g.pf("/// construction, declared defaults in the field initializers\n")
	g.pf("/// (docs/SPEC-TABLES.md), and the table verbs as METHODS on the value.\n")
	g.pf("final class %s {\n", st.Name)
	if len(st.Fields) == 0 {
		g.pf("  // empty body — presence is the payload\n")
	}
	for _, f := range st.Fields {
		g.emitStorageField(f)
	}
	g.pf("\n")
}

// emitMember writes ONE closure member's whole surface: its storage class when
// this backend owns it, and in every case the TABLE VERBS AS MEMBERS OF THE
// VALUE — `config.measure()`, `config.save(out)`, `config.load(bytes, report)`.
//
// THE RULE, and it is the reason this backend claims almost nothing at library
// scope: **a Dart library is written in methods.** Free functions taking the
// value they operate on as a first argument are a C++ transliteration; the core
// library has none. A table's class is this backend's own, so its verbs are
// ordinary methods on it. A closure `type`'s class belongs to the PACKET
// emitter, and Dart's answer to "add methods to a type you do not own" is an
// EXTENSION — statically dispatched, allocating nothing, and applicable exactly
// where its name is in scope, which is why every cross-file `show` clause
// carries it.
//
// The cost is stated where it is paid (internal/check): the nine verb spellings
// are claimed against a FIELD name in a table closure, the way the block form's
// prologue words already are (§19.1, §11). A member surface has to be, and this
// is the whole of what Dart's shape asks for.
func (g *tableGen) emitMember(st *ir.Struct) {
	if st.IsTable {
		g.emitTableClass(st)
	} else {
		g.needDecl(st.Name)
		g.pf("/// %s's table verbs. The class is the PACKET emitter's, so they ride\n", st.Name)
		g.pf("/// on an EXTENSION — statically dispatched, allocating nothing, and\n")
		g.pf("/// applicable wherever this name is in scope (docs/SPEC-TABLES.md §11).\n")
		g.pf("extension %s on %s {\n", extensionName(st.Name), st.Name)
	}
	g.indent = "  "
	g.emitTableReset(st)
	g.emitTableMeasure(st)
	g.emitTableSave(st)
	g.emitTableLoad(st)
	g.emitJsonSurface(st)
	g.indent = ""
	g.trimBlank()
	g.pf("}\n\n")
}

func (g *tableGen) emitStorageField(f *ir.Field) {
	name := dartName(f.Name)
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		g.needData = true
		what := "string"
		if f.Type.Kind == ir.TBytes {
			what = "bytes"
		}
		g.pf("  // %s(%d): fixed buffer, used length beside it\n", what, f.Type.Size)
		g.pf("  final Uint8List %s = Uint8List(%d);\n", name, f.Type.Size)
		g.pf("  int %sLength = 0;\n", name)
	case f.KeyEnum != "":
		// ONE SLOT PER NAMED VARIANT, the key k at index k - 1: nothing is
		// stored for None, and the indexer is the only place the shift
		// appears. Every named slot exists, so there is no count companion
		// (docs/SPEC-TABLES.md §2.4).
		g.pf("  // [%s]%s: one slot per named variant, keyed by the variant's value\n",
			f.KeyEnum, tableFieldTypeName(f))
		if isClassRef(f.Type) {
			elem := g.elementDartType(f)
			g.assign("  ", fmt.Sprintf("final TableKeyed<%s> %s", elem, name),
				fmt.Sprintf("TableKeyed<%s>.generate", elem),
				fmt.Sprintf("%d", f.KeyEnumRef.Max), fmt.Sprintf("() => %s()", elem))
			return
		}
		elem := g.scalarDartType(f)
		g.assign("  ", fmt.Sprintf("final TableKeyed<%s> %s", elem, name),
			fmt.Sprintf("TableKeyed<%s>.filled", elem),
			fmt.Sprintf("%d", f.KeyEnumRef.Max), g.zeroElement(f))
	case f.Array != ir.ArrayNone:
		g.emitArrayStorage(f, name)
	default:
		typ, init := g.scalarStorage(f)
		g.pf("  %s %s = %s;\n", typ, name, init)
		if f.Type.Optional {
			g.pf("  bool %sPresent = false; // ?%s: absent until set\n", name, f.Type.Name)
		}
	}
}

func (g *tableGen) emitArrayStorage(f *ir.Field, name string) {
	bound := f.ArrayBound
	if f.Type.Kind == ir.TNamed {
		if _, isStruct := f.Type.Ref.(*ir.Struct); isStruct {
			elem := g.elementDartType(f)
			// the formatter breaks after the `=` when the head runs past 80
			// columns, and indents the argument list four further
			head := fmt.Sprintf("  final List<%s> %s = List<%s>.generate(", elem, name, elem)
			ind := "  "
			if len(head) > 80 {
				g.pf("  final List<%s> %s =\n", elem, name)
				g.pf("      List<%s>.generate(\n", elem)
				ind = "      "
			} else {
				g.pf("%s\n", head)
			}
			g.pf("%s  %d,\n", ind, bound)
			g.pf("%s  (_) => %s(),\n", ind, elem)
			g.pf("%s  growable: false,\n", ind)
			g.pf("%s);\n", ind)
			if f.Array == ir.ArrayCounted {
				g.pf("  int %sCount = 0;\n", name)
			}
			return
		}
	}
	g.needData = true
	list := typedListFor(f.Type)
	g.pf("  final %s %s = %s(%d);\n", list, name, list, bound)
	if f.Array == ir.ArrayCounted {
		g.pf("  int %sCount = 0;\n", name)
	}
}

// scalarDartType is the Dart type one SCALAR element is stored as: bool,
// double or the bit-transparent int everything else rides in.
func (g *tableGen) scalarDartType(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		return "bool"
	case ir.TFloat32, ir.TFloat64:
		return "double"
	}
	return "int"
}

// elementDartType is the Dart class an element of a table-typed field is
// stored as, recording the import it needs.
func (g *tableGen) elementDartType(f *ir.Field) string {
	name := f.Type.Name
	if g.unit.Tables[name] != nil {
		g.needTable(name, name)
	} else {
		g.needDecl(name)
	}
	return name
}

// typedListFor is the packet emitter's typed-list choice for an array of a
// scalar kind, so a table and a `type` spell the same storage.
func typedListFor(t ir.FieldType) string {
	switch t.Kind {
	case ir.TBool:
		return "Uint8List"
	case ir.TFloat32:
		return "Float32List"
	case ir.TFloat64:
		return "Float64List"
	case ir.TInt:
		if t.Signed {
			switch t.Width {
			case 8:
				return "Int8List"
			case 16:
				return "Int16List"
			case 32:
				return "Int32List"
			}
			return "Int64List"
		}
		switch t.Width {
		case 8:
			return "Uint8List"
		case 16:
			return "Uint16List"
		case 32:
			return "Uint32List"
		}
		return "Uint64List"
	case ir.TBits:
		switch {
		case t.Width <= 8:
			return "Uint8List"
		case t.Width <= 16:
			return "Uint16List"
		case t.Width <= 32:
			return "Uint32List"
		}
		return "Uint64List"
	case ir.TNamed:
		if e, ok := t.Ref.(*ir.Enum); ok {
			switch {
			case e.StorageBits <= 8:
				return "Uint8List"
			case e.StorageBits <= 16:
				return "Uint16List"
			case e.StorageBits <= 32:
				return "Uint32List"
			}
			return "Uint64List"
		}
		if fl, ok := t.Ref.(*ir.Flags); ok {
			switch {
			case fl.WireBits <= 8:
				return "Uint8List"
			case fl.WireBits <= 16:
				return "Uint16List"
			case fl.WireBits <= 32:
				return "Uint32List"
			}
			return "Uint64List"
		}
	}
	return "Uint64List"
}

// scalarStorage is a scalar field's Dart type and initializer — the specified
// default, else the zero form.
func (g *tableGen) scalarStorage(f *ir.Field) (typ, init string) {
	t := f.Type
	switch t.Kind {
	case ir.TBool:
		if f.HasDefault {
			return "bool", fmt.Sprintf("%v", f.DefBool)
		}
		return "bool", "false"
	case ir.TFloat32:
		if f.HasDefault {
			return "double", formatFloat32(f.DefFloat)
		}
		return "double", "0.0"
	case ir.TFloat64:
		if f.HasDefault {
			return "double", formatFloat(f.DefFloat)
		}
		return "double", "0.0"
	case ir.TNamed:
		switch t.Ref.(type) {
		case *ir.Enum:
			g.needDecl(t.Name)
			if f.DefVariant != "" {
				return "int", t.Name + "." + dartName(f.DefVariant)
			}
			return "int", t.Name + ".none"
		case *ir.Flags:
			return "int", "0"
		case *ir.Struct, *ir.Union:
			elem := g.elementDartType(f)
			return "final " + elem, elem + "()"
		}
	}
	if f.HasDefault {
		return "int", dartIntLit(f.DefInt)
	}
	return "int", "0"
}

var maxInt64 = big.NewInt(0).SetInt64(1<<63 - 1)

// dartIntLit renders an integer literal in the bit-transparent int a u64
// field's storage uses: a value above int64's top wraps to its two's
// complement, which is the same bit pattern.
func dartIntLit(v *big.Int) string {
	if v == nil {
		return "0"
	}
	if v.Cmp(maxInt64) > 0 {
		wrapped := new(big.Int).Sub(v, new(big.Int).Lsh(big.NewInt(1), 64))
		return wrapped.String()
	}
	return v.String()
}

func formatFloat(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// formatFloat32 renders a float32-precision literal the way the packet
// emitter does, so a closure `type`'s storage class and this backend's reset
// agree on the same spelling.
func formatFloat32(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 32)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// narrowedFloat32 renders the double a float32 value NARROWS to — the literal
// an elision comparison is against, since the comparison narrows its left
// side (tableNarrowFloat).
func narrowedFloat32(v float64) string {
	return formatFloat(float64(float32(v)))
}

// ---- assembly ----

func (g *tableGen) assemble() []byte {
	var h strings.Builder
	if g.file == nil {
		fmt.Fprintf(&h, "// Code generated by the schema compiler for package %s. DO NOT EDIT.\n", g.unit.Package)
	} else {
		fmt.Fprintf(&h, "// Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", g.file.Base)
	}
	h.WriteString("// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	h.WriteString("// your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	h.WriteString("// AGPL-3.0, its output is not.\n")
	fmt.Fprintf(&h, "// package %s — the TABLE wire (docs/SPEC-TABLES.md): evolution-tolerant, neutral\n", g.unit.Package)
	h.WriteString("// bytes, no serialize dependency. Tables version by field id, never by the\n")
	h.WriteString("// unit's protocol id.\n")
	if g.home {
		h.WriteString("//\n")
		h.WriteString("// " + runtimeHomeMarker + " — <Package>Table.dart, one home per unit, named\n")
		h.WriteString("// by the package and independent of file order. Every other <Base>Table.dart\n")
		h.WriteString("// of the unit imports it.\n")
		h.WriteString("//\n")
		h.WriteString("// The surface is NAME-FIRST: <name>Measure gives the exact wire size,\n")
		h.WriteString("// <name>Save writes exactly that many bytes into the caller's buffer,\n")
		h.WriteString("// <name>Load overlays a value in place and reports every tolerance event.\n")
		h.WriteString("// The caller owns the value, the bytes and the report.\n")
	}
	h.WriteString("\n")
	if g.needData {
		h.WriteString("import 'dart:typed_data';\n\n")
	}
	// the runtime home, unless this file IS it
	if !g.home {
		g.imports[g.homeBase+"Table.dart"] = nil // whole-library import
	}
	for _, base := range sortedKeys(g.imports) {
		syms := g.imports[base]
		if syms == nil {
			fmt.Fprintf(&h, "import '%s';\n", base)
			continue
		}
		names := sortedKeys(syms)
		one := fmt.Sprintf("import '%s' show %s;", base, strings.Join(names, ", "))
		if len(one) <= 80 {
			h.WriteString(one + "\n")
			continue
		}
		// the formatter's own long-show shape: the clause on its own line,
		// then one name per line at eight columns
		fmt.Fprintf(&h, "import '%s'\n    show\n", base)
		for i, n := range names {
			sep := ","
			if i == len(names)-1 {
				sep = ";"
			}
			fmt.Fprintf(&h, "        %s%s\n", n, sep)
		}
	}
	if len(g.imports) > 0 {
		h.WriteString("\n")
	}
	h.WriteString(strings.Trim(g.body.String(), "\n"))
	h.WriteString("\n")
	return []byte(h.String())
}

// sig writes a function signature, wrapping the parameter list the way `dart
// format` does when the one-line form runs past 80 columns. The emitted
// sources are format-canonical, so `dart format --set-exit-if-changed` is a
// gate rather than a chore.
func (g *tableGen) sig(ret, name string, params ...string) {
	one := fmt.Sprintf("%s %s(%s) {", ret, name, strings.Join(params, ", "))
	if len(g.indent)+len(one) <= 80 {
		g.pf("%s\n", one)
		return
	}
	g.pf("%s %s(\n", ret, name)
	for _, p := range params {
		g.pf("  %s,\n", p)
	}
	g.pf(") {\n")
}

// assign writes `<lhs> = <fn>(<args>);` at the given indent, wrapping the
// argument list the way `dart format` does when the one-line form runs past 80
// columns. Every emitted call site goes through this or through sig, which is
// what keeps the sources format-canonical without a reflow pass.
func (g *tableGen) assign(ind, lhs, fn string, args ...string) {
	one := fmt.Sprintf("%s%s = %s(%s);", ind, lhs, fn, strings.Join(args, ", "))
	if len(g.indent)+len(one) <= 80 {
		g.pf("%s\n", one)
		return
	}
	g.pf("%s%s = %s(\n", ind, lhs, fn)
	for _, a := range args {
		g.pf("%s  %s,\n", ind, a)
	}
	g.pf("%s);\n", ind)
}

// trimBlank drops a trailing blank line from the body — the emitter's
// per-member spacing meeting a closing brace, which `dart format` writes with
// no blank between.
func (g *tableGen) trimBlank() {
	text := g.body.String()
	trimmed := strings.TrimRight(text, "\n")
	g.body.Reset()
	g.body.WriteString(trimmed)
	g.body.WriteString("\n")
}

// needMember records the import a call to another closure member's TABLE VERBS
// needs. The verbs are MEMBERS of the value (docs/SPEC-TABLES.md §11), so a table's
// are on its own generated class and a `type`'s are on an EXTENSION over the
// class the packet emitter owns — and an extension is only applicable where its
// NAME is in scope, which is why the show clause carries it.
func (g *tableGen) needMember(declName string) {
	if g.unit.Tables[declName] != nil {
		// a table's verbs ride on the class this backend emits
		g.needTable(declName, declName)
		return
	}
	g.needDecl(declName)
	g.needTable(declName, extensionName(declName))
}

// extensionName is the extension that carries a closure `type`'s table verbs:
// <Name>Table, a spelling §11's suffix set claims.
func extensionName(name string) string { return name + "Table" }
