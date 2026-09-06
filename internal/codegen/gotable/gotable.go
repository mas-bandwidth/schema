// Package gotable emits a unit's Go table surface (docs/SPEC-TABLES.md): the
// TABLE-wire codecs and the reflection descriptors in <Base>Table.go, the
// TEXT form in <Base>TableJson.go, and the two ACCELERATORS on the side in
// <Base>Block.go (§19) and <Base>Cook.go (§7). One file per unit file,
// emitted only when the unit declares tables.
//
// The C++ backend (internal/codegen/cpptable) is the REFERENCE and the C#
// backend (internal/codegen/cstable) is the second implementation: this port
// mirrors their framing, their elision decisions, their clamps and their
// report events byte for byte, and invents no contract of its own. Where Go
// forces a different spelling the reason is stated at the site.
//
// Storage follows the Go PACKET emitter's conventions exactly
// (internal/codegen/golang): structs with exported fields, string(N) and
// bytes(N) as [N]byte beside an int32 used length, arrays as [N]T beside an
// int32 used count, enum-keyed arrays as [E.Max]T, unions as a tag beside one
// arm per variant. That is not a free choice — a table's closure contains
// plain `type` declarations whose storage the packet emitter already wrote,
// and the table codecs decode into those very structs. One unit, one spelling.
//
// The codecs are FREE FUNCTIONS taking a *T, name-first (<Name>Measure,
// <Name>Save, <Name>Load), which is both §11's claimed surface and the Go
// packet emitter's own shape. NOTHING ON THE READ PATH ALLOCATES: every buffer
// is inside the value the caller owns, sub-readers are stack values, and Load
// overlays in place after restoring the declared defaults.
//
// The VARIABLE class ON THE WIRE — the arena, the builder, the region and the
// node-table codec — is a named follow-on, exactly as it is under C# (§11): a
// unit whose closure declares a pointer gets no <Base>Table.go, and the
// refusal is NAMED in every source the unit does emit rather than left as a
// missing symbol. The two ACCELERATORS reach further, because neither needs a
// codec: a block and a cook are POINTED AT, not parsed.
package gotable

import (
	"fmt"
	"go/format"
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

func tablePut(width int) string { return fmt.Sprintf("Put%d", width*8) }
func tableGet(width int) string { return fmt.Sprintf("Get%d", width*8) }

// goKindStorage is the Go type one wire kind decodes through.
func goKindStorage(kind int) string {
	switch kind {
	case tkI8:
		return "int8"
	case tkI16:
		return "int16"
	case tkI32:
		return "int32"
	case tkI64:
		return "int64"
	case tkU8:
		return "uint8"
	case tkU16:
		return "uint16"
	case tkU32:
		return "uint32"
	case tkU64:
		return "uint64"
	}
	return "uint64"
}

type tableGen struct {
	unit     *ir.Unit
	file     *ir.File
	home     bool // this file carries the unit's shared table runtime
	anyKeyed bool
	owner    *ir.Struct // the closure member whose codec is being emitted

	types  strings.Builder // the table storage structs
	body   strings.Builder // everything else
	indent string          // extra per-line indent while emitting inside a branch guard

	needsMath  bool
	unsafeUsed bool

	// the assignments that fill this file's slots of the unit's arms table,
	// emitted in an init() below the descriptors so a cyclic descriptor graph
	// stays expressible
	unionArms []string
	// every union FIELD of the unit, by (owner, field), to the slot it takes
	// in the one arms table — computed for the whole unit before any file is
	// emitted, so the slice is one name and not one per declaration
	unionArmSlot map[armKey]int
}

// armKey names one union FIELD: the closure member that carries it and the
// field's own name. A union's arm offsets are the union's, but a descriptor
// column belongs to the field that carries it.
type armKey struct {
	owner string
	field string
}

// unionArmSlots numbers every union field in the unit, in the order the files
// and their members declare them, so the emitted text is deterministic.
func unionArmSlots(u *ir.Unit, closure map[string]bool) map[armKey]int {
	slots := map[armKey]int{}
	for _, f := range u.Files {
		for _, st := range fileMembers(f, closure) {
			for _, field := range st.Fields {
				if field.Type.Kind != ir.TNamed || field.Array != ir.ArrayNone {
					continue
				}
				if _, isUnion := field.Type.Ref.(*ir.Union); !isUnion {
					continue
				}
				slots[armKey{owner: st.Name, field: field.Name}] = len(slots)
			}
		}
	}
	return slots
}

// needsUnsafe marks this file as reaching for the layout model — every
// descriptor does, through unsafe.Sizeof and unsafe.Offsetof, which are
// CONSTANT EXPRESSIONS of the compiler's own model rather than a guess about
// it (docs/SPEC-TABLES.md §8).
func (g *tableGen) needsUnsafe() { g.unsafeUsed = true }

func (g *tableGen) tf(format string, args ...any) {
	fmt.Fprintf(&g.types, format, args...)
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

// Generate returns <Base>Table.go (plus the accelerators) for every file of a
// unit that declares tables, and nothing when it declares none — a table-free
// unit's generated tree is byte-identical with or without this package.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	if len(u.Tables) == 0 {
		return map[string][]byte{}, nil
	}
	if err := ir.RefuseWideTableKinds(u, "Go"); err != nil {
		return nil, err
	}
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
	// than silent — every generated source of the unit opens with the banner
	// below — and no <Base>Table.go is emitted, so a consumer that reaches for
	// Save or Load gets a missing name from its own compiler beside a file
	// that says why.
	if names := variableTableNames(u); len(names) > 0 {
		banner := variableClassBanner(names)
		for name, data := range out {
			out[name] = withBanner(banner, data)
		}
		return out, nil
	}

	closure := ir.TableClosure(u)
	home := ir.ProtocolIdHome(u)
	anyKeyed := unitHasKeyedArray(u, closure)
	// the identity pair of an enum is emitted ONCE per unit, by the file that
	// declares it: a Go package is one namespace across its files, so a second
	// definition is a compile error rather than C++'s harmless re-inclusion
	// behind a guard
	usedEnums := closureEnums(u, closure)
	armSlots := unionArmSlots(u, closure)
	for _, f := range u.Files {
		g := &tableGen{unit: u, file: f, home: f.Base == home, anyKeyed: anyKeyed, unionArmSlot: armSlots}
		members := fileMembers(f, closure)
		if g.home {
			g.needsUnsafe() // the descriptor surface's reset column takes an unsafe.Pointer
			g.pf("%s", tableRuntime())
			if anyKeyed {
				g.pf("%s", tableKeyedAccessor)
			}
			if len(armSlots) > 0 {
				g.pf("// tableUnionArms is the unit's ONE arms table: one slot per union\n")
				g.pf("// FIELD, filled by the init() beside the descriptors that point into\n")
				g.pf("// it. One fixed name for the whole unit rather than one derived from\n")
				g.pf("// each declaration's own spelling, because a derived name is a name a\n")
				g.pf("// declaration can collide with (docs/SPEC-TABLES.md §11).\n")
				g.pf("var tableUnionArms = make([]TableUnionInfo, %d)\n\n", len(armSlots))
			}
		}
		for _, st := range members {
			if st.IsTable {
				g.owner = st
				g.emitTableStruct(st)
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
			g.emitUnionArms()
		}
		src, err := g.assemble()
		if err != nil {
			return nil, err
		}
		out[f.Base+"Table.go"] = src
	}

	// the TEXT form (docs/SPEC-TABLES.md §16): one generic walk over the
	// descriptors, in its own file so a project that never reads or writes a
	// text does not compile it
	texts, err := generateJsonFiles(u, closure, home)
	if err != nil {
		return nil, err
	}
	maps.Copy(out, texts)
	return out, nil
}

// fileMembers is a file's closure members: its tables, then the plain `type`
// declarations the closure reaches.
func fileMembers(f *ir.File, closure map[string]bool) []*ir.Struct {
	var members []*ir.Struct
	members = append(members, f.Tables...)
	for _, d := range f.Decls {
		if st, ok := d.(*ir.Struct); ok && closure[st.Name] {
			members = append(members, st)
		}
	}
	return members
}

func (g *tableGen) assemble() ([]byte, error) {
	var h strings.Builder
	fmt.Fprintf(&h, "// Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", g.file.Base)
	h.WriteString("// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	h.WriteString("// your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	h.WriteString("// AGPL-3.0, its output is not.\n")
	fmt.Fprintf(&h, "// package %s — the TABLE wire (docs/SPEC-TABLES.md): evolution-tolerant, neutral\n", g.unit.Package)
	h.WriteString("// bytes, no serialize dependency. Tables version by field id, never by the\n")
	h.WriteString("// unit's protocol id.\n")
	if g.home {
		h.WriteString("//\n")
		h.WriteString("// Measure/Save/Load are NAME-FIRST free functions over a *T: <Name>Measure\n")
		h.WriteString("// gives the exact wire size, <Name>Save writes exactly that many bytes into\n")
		h.WriteString("// the caller's slice, <Name>Load overlays a value in place and reports every\n")
		h.WriteString("// tolerance event. Nothing here allocates: the caller owns the value, the\n")
		h.WriteString("// slice and the report.\n")
	}
	h.WriteString("\n")
	fmt.Fprintf(&h, "package %s\n\n", g.unit.Package)
	var std []string
	if g.needsMath {
		std = append(std, `"math"`)
	}
	if g.unsafeUsed {
		std = append(std, `"unsafe"`)
	}
	h.WriteString(goImports(std))
	h.WriteString(g.types.String())
	h.WriteString(g.body.String())
	src, err := format.Source([]byte(h.String()))
	if err != nil {
		return nil, fmt.Errorf("generated Go for %sTable.go does not parse — a compiler bug, not a schema error: %w", g.file.Base, err)
	}
	return src, nil
}

// goImports renders an import block the way gofmt would have written it: a
// lone import takes the one-line form, which is what a hand-written file of
// this shape looks like. go/format does not rewrite the grouping, so the
// emitter picks it.
func goImports(paths []string) string {
	switch len(paths) {
	case 0:
		return ""
	case 1:
		return "import " + paths[0] + "\n\n"
	}
	var b strings.Builder
	b.WriteString("import (\n")
	for _, p := range paths {
		fmt.Fprintf(&b, "\t%s\n", p)
	}
	b.WriteString(")\n\n")
	return b.String()
}

// withBanner puts the variable-class refusal above a generated file's own
// header comment. The banner must not land before the `// Code generated by`
// line, which tooling reads, so it rides after the package clause's comment
// block — practically, prepended to the file and re-formatted.
func withBanner(banner string, data []byte) []byte {
	return append([]byte(banner), data...)
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
// meets it (docs/SPEC-TABLES.md §2.2, §11). The refusal is of the WIRE half and of
// nothing else: the accelerators below are pure readers over blittable records
// and need no codec, so they are emitted. What is absent is Measure, Save,
// Load, the arena and the builder — named here rather than left as a missing
// symbol with no explanation.
func variableClassBanner(names []string) string {
	return "// THE GO WIRE SURFACE OF THIS UNIT IS REFUSED, BY NAME (docs/SPEC-TABLES.md §11).\n" +
		"//\n" +
		"// It declares variable-length tables (" + englishList(names) + "), and the Go table\n" +
		"// backend's VARIABLE CLASS — the arena, the builder, the region and the node-table\n" +
		"// codec — is a named follow-on (§15). No <Base>Table.go is emitted for this unit,\n" +
		"// so a consumer reaching for Measure, Save or Load gets a missing name from its own\n" +
		"// compiler, beside this file, which says why.\n" +
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
			if live, ok := used[e.Name]; ok {
				out = append(out, live)
			}
		}
	}
	return out
}

// unitHasKeyedArray reports whether any closure member declares an enum-keyed
// array, which is what decides whether the unit carries the keyed accessor.
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

// tableRuntime is the unit's shared table runtime, emitted into ONE file per
// unit (the protocol-id home). C++ emits it into every header behind an
// include guard; a Go package is one namespace across its files, so a second
// copy would be a redeclaration error instead.
func tableRuntime() string {
	return `// TableReport is the table-wire read report — the permissive contract's
// ledger. Silence (all zero) means the data matched this reader's schema
// exactly.
type TableReport struct {
	Unknown      int32 // unknown field ids skipped (newer data)
	KindMismatch int32 // known id, changed type — skipped, never misdecoded
	Clamped      int32 // out-of-range values clamped to declared bounds
	// Duplicate is the TEXT FORM's counter and the WIRE NEVER RAISES IT
	// (docs/SPEC-TABLES.md §4, §16.2): a body carrying an id twice is legal input
	// whose last occurrence wins, silently. A wire read always leaves it zero.
	Duplicate int32
	Malformed bool // framing damage; decode stopped, partial result kept
}

// ---- reflection (tables only, docs/SPEC-TABLES.md §8) ----
//
// Static field descriptors for every type in the table closure: name, the
// TEXT form's key, wire id and kind, storage offset, bounds, ranges, the
// enum/union vocabulary and its wire ids, and branch guards — enough to walk,
// print, diff, edit or bind any table value at runtime with no schema files on
// hand. <Name>TableType() returns the descriptor.
//
// The descriptors carry MEMORY facts — Offset, ElemSize, CountOffset,
// PresentOffset and TableTypeInfo.Size — exactly as the C++ reference does,
// because Go can state them: unsafe.Offsetof and unsafe.Sizeof are constant
// expressions of the compiler's own layout model, so the generated numbers are
// this build's own rather than a guess about it. They are what lets one
// generic walk drive the text form for every table.

// THE SHARED EMPTY DOC (docs/SPEC-TABLES.md §8.1): a declaration with no ///
// block carries a doc column naming this one definition, so absence costs a
// unit no string data and a printer concatenates doc columns with no nil test.
// Go gives a string no address identity, so what the rule is READ OFF here is
// the emitted text: the generated file defines the empty doc once and every
// unannotated row names that definition, rather than each row carrying an
// inline "" of its own.
const TableDocNone = ""

// TableFieldInfo is one field's descriptor.
type TableFieldInfo struct {
	Name     string // schema field name, e.g. "health"
	Json     string // the TEXT form's key: the json = "key" attribute, else Name (§16.3)
	TypeName string // schema type name, e.g. "float32", "Grade"
	Id       uint16 // table-wire field id (name hash; the was alias's hash after a rename)
	Kind     uint8  // table-wire kind; for arrays/strings/bytes, the ELEMENT kind
	IsArray  bool   // fixed or counted array (bytes included)
	Counted  bool   // a <Name>Count/<Name>Length int32 companion exists
	Optional bool   // a ?T field: a <Name>Present bool decides whether it rides

	ArrayBound    int32  // array capacity / string max length; 0 for plain scalars
	Offset        uint32 // unsafe.Offsetof the storage member
	ElemSize      uint32 // unsafe.Sizeof the member (element size for arrays)
	CountOffset   uint32 // unsafe.Offsetof the count/length companion, or 0xffffffff
	PresentOffset uint32 // unsafe.Offsetof the Present companion, or 0xffffffff

	HasRange bool    // a declared [min, max] (int or float)
	RangeMin float64 // NOTE: int64 ranges beyond 2^53 lose precision here
	RangeMax float64

	// EnumMax bounds the vocabulary: enums, the highest valid value (None = 0
	// is always valid); unions, the arm count (tag range [0, EnumMax]); a
	// FLAGS field, the highest declared BIT INDEX; else -1.
	EnumMax int64
	// EnumName is the vocabulary's names, indexed the way EnumMax bounds: an
	// enum's value -> name, a union's tag -> arm name, a FLAGS field's bit
	// index -> variant name. nil for every other kind.
	EnumName func(value uint64) string
	// VariantId is the TABLE-WIRE id of one variant (docs/SPEC-TABLES.md §5): for
	// an enum, the hash of the variant's name; for a union, the hash of the
	// arm's name. 0 is the reserved id — an enum's None, a union's empty. nil
	// for every other kind — a FLAGS field's variants have no per-variant wire
	// id (§4), so a nil here beside a non-nil EnumName is what says "flags".
	VariantId func(value uint64) uint16

	// an ENUM-KEYED array (docs/SPEC-TABLES.md §2.4, §8): the array has one slot
	// per variant of KeyTypeName, indexed by the variant's value, and its
	// slots ride under variant ids rather than positions. KeyName and KeyId
	// are the key's vocabulary — walk [0, ArrayBound) to print slots by name.
	// All three are nil/"" on every other field.
	KeyTypeName string
	KeyName     func(value uint64) string
	KeyId       func(value uint64) uint16

	// Arms is a union field's shape: the tag and the arms indexed by it, behind
	// a function so a descriptor graph needs no initialization order. nil for
	// every other kind.
	Arms func() *TableUnionInfo

	Guard string // branch guard, e.g. "at_rest" or "!at_rest"; "" if unguarded

	// Table is the nested table's descriptor, or nil. Held as a factory rather
	// than a value so a descriptor graph needs no initialization order: Go
	// refuses an initialization cycle across package-level variables, and a
	// table may name one declared later.
	Table func() *TableTypeInfo

	// what a PERSON wrote about the field (docs/SPEC-TABLES.md §8.1): the ///
	// block above it, verbatim (SPEC §4.1). It is TableDocNone when there is
	// none, never nil. Its tags (SPEC §4.2) follow in declared order, and an
	// untagged field is 0 beside a nil list. Static package data, allocating
	// nothing.
	Doc     string
	NumTags int32
	Tags    []string
}

// TableUnionArmInfo is one arm of a union field: where its payload sits inside
// the union's storage and what its payload looks like. The arm's NAME and its
// table-wire id come from the field's EnumName/VariantId functions at the same
// tag, so nothing is spelled twice (docs/SPEC-TABLES.md §8).
type TableUnionArmInfo struct {
	Offset uint32               // unsafe.Offsetof the arm's payload within the union storage
	Table  func() *TableTypeInfo // the arm payload's descriptor
}

// TableUnionInfo is a union field's shape: the tag, and the arms indexed by
// it. Arms run [0, EnumMax]; index 0 is the EMPTY arm and carries no payload.
type TableUnionInfo struct {
	TagOffset uint32 // unsafe.Offsetof the tag within the union storage
	TagSize   uint32 // unsafe.Sizeof the tag
	Arms      []TableUnionArmInfo
}

// TableTypeInfo is one type's descriptor.
type TableTypeInfo struct {
	Name      string // schema type name
	Size      uint32 // unsafe.Sizeof the storage struct
	NumFields int32
	Fields    []TableFieldInfo
	// Reset puts one instance back at its declared defaults, in place. A
	// generic walker that fills a value has to be able to establish the
	// defaults an absent field takes, and it holds no type to spell — this is
	// the one thing the descriptors could not express without it.
	Reset func(storage unsafe.Pointer)

	// the declaration's own doc and tags, on the same terms as a field's
	// (docs/SPEC-TABLES.md §8.1, SPEC §4.1, §4.2)
	Doc     string
	NumTags int32
	Tags    []string
}

// TableWriter writes the wire into the caller's slice. Nothing here allocates.
type TableWriter struct {
	Buffer   []byte
	Offset   int64
	Overflow bool
}

// Raw copies bytes in place.
func (w *TableWriter) Raw(data []byte) {
	if w.Offset+int64(len(data)) > int64(len(w.Buffer)) {
		w.Overflow = true
		return
	}
	copy(w.Buffer[w.Offset:], data)
	w.Offset += int64(len(data))
}

// Put8 writes one byte.
func (w *TableWriter) Put8(v uint8) {
	if w.Offset+1 > int64(len(w.Buffer)) {
		w.Overflow = true
		return
	}
	w.Buffer[w.Offset] = v
	w.Offset++
}

// Put16 writes a little-endian u16.
func (w *TableWriter) Put16(v uint16) {
	if w.Offset+2 > int64(len(w.Buffer)) {
		w.Overflow = true
		return
	}
	w.Buffer[w.Offset] = uint8(v)
	w.Buffer[w.Offset+1] = uint8(v >> 8)
	w.Offset += 2
}

// Put32 writes a little-endian u32.
func (w *TableWriter) Put32(v uint32) {
	if w.Offset+4 > int64(len(w.Buffer)) {
		w.Overflow = true
		return
	}
	w.Buffer[w.Offset] = uint8(v)
	w.Buffer[w.Offset+1] = uint8(v >> 8)
	w.Buffer[w.Offset+2] = uint8(v >> 16)
	w.Buffer[w.Offset+3] = uint8(v >> 24)
	w.Offset += 4
}

// Put64 writes a little-endian u64.
func (w *TableWriter) Put64(v uint64) {
	w.Put32(uint32(v))
	w.Put32(uint32(v >> 32))
}

// Patch32 rewrites a length prefix already reserved.
func (w *TableWriter) Patch32(at int64, v uint32) {
	if at+4 > int64(len(w.Buffer)) {
		w.Overflow = true
		return
	}
	w.Buffer[at] = uint8(v)
	w.Buffer[at+1] = uint8(v >> 8)
	w.Buffer[at+2] = uint8(v >> 16)
	w.Buffer[at+3] = uint8(v >> 24)
}

// TableReader reads the wire out of the caller's slice. A nested body is read
// through a sub-reader sliced out of this one, so an inner decode can never
// reach past its own framing.
type TableReader struct {
	Buffer []byte
	Offset int64
	Report *TableReport
}

// Has reports whether the reader still holds that many bytes.
func (r *TableReader) Has(bytes int64) bool { return r.Offset+bytes <= int64(len(r.Buffer)) }

// Get8 reads one byte.
func (r *TableReader) Get8() uint8 {
	v := r.Buffer[r.Offset]
	r.Offset++
	return v
}

// Get16 reads a little-endian u16.
func (r *TableReader) Get16() uint16 {
	v := uint16(r.Buffer[r.Offset]) | uint16(r.Buffer[r.Offset+1])<<8
	r.Offset += 2
	return v
}

// Get32 reads a little-endian u32.
func (r *TableReader) Get32() uint32 {
	v := uint32(r.Buffer[r.Offset]) | uint32(r.Buffer[r.Offset+1])<<8 |
		uint32(r.Buffer[r.Offset+2])<<16 | uint32(r.Buffer[r.Offset+3])<<24
	r.Offset += 4
	return v
}

// Get64 reads a little-endian u64.
func (r *TableReader) Get64() uint64 {
	lo := uint64(r.Get32())
	hi := uint64(r.Get32())
	return lo | hi<<32
}

// Sub slices a nested body out of this reader, sharing the report.
func (r *TableReader) Sub(bytes int64) TableReader {
	return TableReader{Buffer: r.Buffer[r.Offset : r.Offset+bytes], Report: r.Report}
}

// Skip steps over one payload by kind; false = framing damage.
func (r *TableReader) Skip(kind uint8) bool {
	switch kind {
	case 1, 2, 6:
		if !r.Has(1) {
			return false
		}
		r.Offset++
		return true
	case 3, 7:
		if !r.Has(2) {
			return false
		}
		r.Offset += 2
		return true
	// 17 is a NODE INDEX (docs/SPEC-TABLES.md §3.1): four bytes, so it costs one
	// row here and a reader without the kind still skips a pointer field
	case 4, 8, 10, 17:
		if !r.Has(4) {
			return false
		}
		r.Offset += 4
		return true
	case 5, 9, 11:
		if !r.Has(8) {
			return false
		}
		r.Offset += 8
		return true
	case 12, 13, 14, 16:
		if !r.Has(4) {
			return false
		}
		n := int64(r.Get32())
		if !r.Has(n) {
			return false
		}
		r.Offset += n
		return true
	case 15: // union: u16 arm id, then the arm length-prefixed (id 0 = empty, no body)
		if !r.Has(2) {
			return false
		}
		if r.Get16() == 0 {
			return true
		}
		if !r.Has(4) {
			return false
		}
		n := int64(r.Get32())
		if !r.Has(n) {
			return false
		}
		r.Offset += n
		return true
	}
	return false
}

`
}

// tableKeyedAccessor is the enum-keyed array's one indexing helper
// (docs/SPEC-TABLES.md §2.4). Emitted only into a unit that declares a keyed array,
// so a unit without one is byte-identical to what it was.
//
// WHAT GO SPELLS DIFFERENTLY, stated where a reader meets it. C++ has an
// operator[] on a storage template and C# an indexer on a generic class; Go
// has neither operator overloading nor generic array extents, so an enum-keyed
// array's STORAGE IS the plain array the schema means — [E.Max]T, the key k at
// index k-1, which is also exactly what the packet emitter writes for the same
// declaration on the type wire. The extent is still derived and named nowhere
// else: it is the generated E.Max constant and no other number.
//
// The SHIFT and the None refusal live here, in one function, so no call site
// in any language spells either (§2.4). A None key is a program error and this
// panics in EVERY build — the C++ abort and the C# throw, in Go's spelling —
// because the storage holds no slot for None and indexing one byte before an
// array is not a thing a config read may do.
const tableKeyedAccessor = `// TableKeyed indexes an enum-keyed array's slots BY KEY: the storage shifts
// left, so the key k lives at index k-1 and nothing is stored for None
// (docs/SPEC-TABLES.md §2.4). It hands back a pointer, so the same call reads and
// fills a slot.
//
// NONE IS THE NULL KEY: it names no slot, it never rides on the wire, a stored
// key of 0 is malformed, and INDEXING BY IT IS A PROGRAM ERROR IN EVERY BUILD.
// The panic stands in release too: the alternative is reading the element
// before the array.
//
// The generated codecs walk the array by STORAGE INDEX and never call this;
// it is the surface a CALLER writes, so no call site spells the shift:
//
//	*TableKeyed(hull.Turrets[:], int(WeaponMissile)) = turret
//
// THE GUARD IS SYMMETRIC, and §2.4's argument for the None compare is what
// makes it so: the storage holds one slot per NAMED variant and nothing else,
// so a key of 0 indexes the element BEFORE the array and a key past E.Max
// indexes past its end. Both are the same program error, both are refused in
// EVERY BUILD, and neither compare is removable — a build that dropped them
// would read memory that is not a slot.
func TableKeyed[T any](slots []T, key int) *T {
	if key <= 0 || key > len(slots) {
		panic("an enum-keyed array is indexed by a NAMED variant of its key enum: " +
			"None keys no slot, and neither does a value past E.Max")
	}
	return &slots[key-1]
}

`
