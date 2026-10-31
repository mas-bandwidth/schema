package rusttable

// THE VARIABLE FORM'S CODEC (docs/SPEC-TABLES.md §3, §3.1): the Rust save and
// load for the variable class this leg carries (variablerefusal.go names what
// it does not) — table roots, tables nested by value and as array elements,
// pointers *T over the flat node table, *string and *bytes blobs, every scalar
// width with its default and range, string(N) and bytes(N), enums, and
// unbounded and bounded counted arrays of scalar, enum, table and pointer
// elements — the wire the carried framing (variable.go) pins the field headers
// for. Every byte decision mirrors the oracle (internal/tablewire/encode.go and
// decode.go), which the tests hold this codec to byte for byte.

import (
	"fmt"
	"maps"
	"math"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// variableCodecModule emits one source file's variable-table storage structs,
// their numbering walks and their save/load codecs, for the tables the variable
// class carries. Nil for a file that declares no such table.
func (g *gen) variableCodecModule() []byte {
	names := cardVariableTableNames(g)
	if len(names) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString(header(g.file.Base, g.unit.Package, "the variable wire's storage and codecs (docs/SPEC-TABLES.md §3, §3.1, §16)"))
	b.WriteString("\nuse super::variable_runtime::*;\n")
	b.WriteString("#[allow(unused_imports)]\nuse std::any::Any;\n")
	b.WriteString("#[allow(unused_imports)]\nuse std::collections::HashMap;\n")
	b.WriteString("#[allow(unused_imports)]\nuse std::rc::Rc;\n\n")
	b.WriteString("/// The reader's id-table distinctness bound and open-addressing probe capacity (docs/SPEC-TABLES.md §3).\n")
	b.WriteString("#[allow(dead_code)]\nconst ID_TABLE_PROBE_BOUND: usize = 4096;\n")
	b.WriteString("#[allow(dead_code)]\nconst ID_TABLE_PROBE_SLOTS: usize = 8192;\n")
	for _, other := range g.unit.Files {
		if other.Base == g.file.Base {
			continue
		}
		otherNames := fileVariableTableNames(g.unit, other, g.vc)
		if len(otherNames) == 0 {
			continue
		}
		var syms []string
		for _, n := range otherNames {
			syms = append(syms, n, fn(n, "number"), fn(n, "save_body"), fn(n, "load_body"), fn(n, "json_count"), fn(n, "json_write"), fn(n, "json_read"), fn(n, "json_fields"), fn(n, "json_ptr"))
		}
		fmt.Fprintf(&b, "#[allow(unused_imports)]\nuse super::%s_table::{%s};\n", strings.ToLower(other.Base), strings.Join(syms, ", "))
		if cooked := cookWriteImports(g.unit, g.vc, otherNames); len(cooked) > 0 {
			fmt.Fprintf(&b, "#[cfg(feature = \"cook\")]\n#[allow(unused_imports)]\nuse super::%s_table::{%s};\n", strings.ToLower(other.Base), strings.Join(cooked, ", "))
		}
	}
	// the table enums live once per crate (table_enums.rs), not per file; the
	// import is placed once the body shows it names one (a key enum is only
	// counted, never named)
	b.WriteString(enumsImportMark)
	b.WriteString("\n")
	var plain, holding []string
	for _, n := range names {
		if structHoldsUnion(vStruct(g.unit, n)) {
			holding = append(holding, n)
		} else {
			plain = append(plain, n)
		}
	}
	for _, n := range plain {
		g.emitVariableStruct(&b, vStruct(g.unit, n))
	}
	g.emitUnionTypes(&b, names)
	for _, n := range holding {
		g.emitVariableStruct(&b, vStruct(g.unit, n))
	}
	for _, n := range names {
		g.emitVariableNumber(&b, vStruct(g.unit, n))
	}
	for _, n := range names {
		g.emitVariableSaveBody(&b, vStruct(g.unit, n))
	}
	for _, n := range names {
		g.emitVariableLoadBody(&b, vStruct(g.unit, n))
	}
	g.emitVariableDispatch(&b, names)
	for _, n := range names {
		if g.isVariableRoot(vStruct(g.unit, n)) {
			g.emitVariableRoot(&b, vStruct(g.unit, n))
		}
	}
	g.emitVariableText(&b)
	g.emitCookWriteFromStorage(&b, names)
	body := b.String()
	importLine := ""
	for _, e := range g.collectVariableEnums(names) {
		if regexp.MustCompile(`\b(` + e.Name + `|` + ir.RustSnake(e.Name) + `_(from_)?json_name)\b`).MatchString(strings.Replace(body, enumsImportMark, "", 1)) {
			importLine = fmt.Sprintf("use super::%s::*;\n", TableEnumsModule)
			break
		}
	}
	return []byte(strings.Replace(body, enumsImportMark, importLine, 1))
}

// enumsImportMark holds the table-enums import's place until the module body
// is known.
const enumsImportMark = "\x00table-enums-import\x00"

// rustVariant turns a schema variant's snake_case into the PascalCase Rust
// enum-variant spelling: flash -> Flash, _reserved -> Reserved.
func rustVariant(name string) string {
	out := make([]byte, 0, len(name))
	up := true
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c == '_' {
			up = true
			continue
		}
		if up && c >= 'a' && c <= 'z' {
			out = append(out, c-'a'+'A')
		} else {
			out = append(out, c)
		}
		up = false
	}
	return string(out)
}

// vClass is what one field (or one element of an array field) IS on the
// variable wire, which decides its storage, its framing and its walk.
type vClass int

const (
	vScalar  vClass = iota // bool, an integer, bits(N), a float
	vEnum                  // kind 30: the variant name's id reference
	vString                // string(N): kind 12, L, the bytes
	vBytes                 // bytes(N): kind 14 over element kind 6
	vBlob                  // *string / *bytes: kind 17, an index to a blob node (§2.5)
	vPointer               // *T: kind 17, an index to a table node (§3.1)
	vTable                 // T by value: kind 13, L, a body
	vMap                   // map[K]V: kind 14 over element kind 13, entries in key order (§2.8)
	vUnion                 // kind 15: an arm header (§2.6)
)

func classOf(f *ir.Field) vClass {
	switch {
	case f.IsMap():
		return vMap
	case f.Type.Blob():
		return vBlob
	case f.Type.Pointer:
		return vPointer
	case f.Type.Kind == ir.TString:
		return vString
	case f.Type.Kind == ir.TBytes:
		return vBytes
	case f.Type.Kind == ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			return vEnum
		case *ir.Union:
			return vUnion
		case *ir.Flags:
			// a FLAGS MASK rides as a u64 scalar (docs/SPEC-TABLES.md §3:
			// kind 9), its default the mask the brace list spells (SPEC §4.2)
			return vScalar
		}
		return vTable
	}
	return vScalar
}

// isFlags reports a flags-mask field, whose text form is the array of its set
// bits' names (docs/SPEC-TABLES.md §16.2).
func isFlags(f *ir.Field) *ir.Flags {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	fl, _ := f.Type.Ref.(*ir.Flags)
	return fl
}

// fieldWireKind is the kind byte a FIELD rides under (the oracle's wireKind):
// 17 for a pointer or a blob, 14 for any array and for bytes(N), and the
// element's own kind otherwise.
func fieldWireKind(f *ir.Field) int {
	if f.Type.Pointer && f.Array == ir.ArrayNone {
		return ir.TableKindPointer
	}
	if f.Type.Kind == ir.TBytes {
		return ir.TableKindArray
	}
	return ir.TableWireFieldKind(f)
}

// elemWireKind is the element kind an array body opens with (§3, §2.9): 17
// for an array of pointers, 30 for enums, 13 for tables, the scalar's kind.
func elemWireKind(f *ir.Field) int {
	return ir.TableWireElemKind(f)
}

// scalarWireKind is the kind ONE scalar value of the field rides under, for a
// field and for an element alike.
func scalarWireKind(f *ir.Field) int {
	return ir.TableWireScalarKind(f)
}

// scalarRust is a scalar's Rust storage: bits(N) in the narrowest unsigned
// integer that holds N bits, and fixed(I, F) as its raw storage integer.
func scalarRust(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		return "bool"
	case ir.TFloat32:
		return "f32"
	case ir.TFloat64:
		return "f64"
	case ir.TBits:
		return rustUint(f.Type.Width)
	case ir.TInt, ir.TFixed:
		// a FIXED value is the raw integer its storage holds, units × 2^F
		// (docs/SPEC-TABLES.md §3, kinds 20–29), as the oracle's storage is
		if f.Type.Signed {
			return rustInt(f.Type.Width)
		}
		return rustUint(f.Type.Width)
	case ir.TNamed:
		if isFlags(f) != nil {
			return "u64" // a flags mask's storage is u64 in every target (SPEC §4.2)
		}
	}
	return "()"
}

// elemRust is the Rust storage of ONE value of the field.
func elemRust(f *ir.Field) string {
	switch classOf(f) {
	case vBlob:
		// a blob is a NODE (§2.5): its Rc is its identity, so a slot that
		// shares another's Rc shares one record, and equal bytes in two Rcs
		// are two records, as the oracle numbers them
		if f.Type.Kind == ir.TString {
			return "Option<Rc<str>>"
		}
		return "Option<Rc<[u8]>>"
	case vPointer:
		return "Option<Rc<" + f.Type.Name + ">>"
	case vString:
		return "String"
	case vBytes:
		return "Vec<u8>"
	case vEnum, vTable:
		return f.Type.Name
	case vMap:
		return f.MapEntry.Name
	case vUnion:
		return f.Type.Ref.(*ir.Union).Name
	}
	return scalarRust(f)
}

// variableFieldType is a field's Rust storage type: an array of any length is
// a Vec of its element (a bounded [..N]T holds at most N, §2.9).
func variableFieldType(f *ir.Field) string {
	if f.IsMap() {
		return "Vec<" + f.MapEntry.Name + ">"
	}
	if f.KeyEnum != "" || f.Array != ir.ArrayNone {
		t := "Vec<" + elemRust(f) + ">"
		if f.Type.Optional {
			return "Option<" + t + ">"
		}
		return t
	}
	if f.Type.Optional && !f.Type.Pointer {
		return "Option<" + elemRust(f) + ">"
	}
	return elemRust(f)
}

// rustStr spells bytes as a Rust string literal.
func rustStr(b []byte) string {
	var s strings.Builder
	s.WriteByte('"')
	for _, r := range string(b) {
		switch {
		case r == '"':
			s.WriteString(`\"`)
		case r == '\\':
			s.WriteString(`\\`)
		case r >= 0x20 && r < 0x7f:
			s.WriteRune(r)
		default:
			fmt.Fprintf(&s, `\u{%x}`, r)
		}
	}
	s.WriteByte('"')
	return s.String()
}

// rustByteArray spells bytes as a Rust u8 array literal.
func rustByteArray(b []byte) string {
	parts := make([]string, len(b))
	for i, c := range b {
		parts[i] = strconv.Itoa(int(c)) + "u8"
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// scalarDefault is the declared default of one scalar (SPEC §5), in its Rust
// storage type: zero when none is declared.
func scalarDefault(f *ir.Field) string {
	t := scalarRust(f)
	switch f.Type.Kind {
	case ir.TBool:
		if f.HasDefault && f.DefBool {
			return "true"
		}
		return "false"
	case ir.TFloat32:
		if f.HasDefault {
			return fmt.Sprintf("f32::from_bits(0x%08x)", math.Float32bits(float32(f.DefFloat)))
		}
		return "0.0f32"
	case ir.TFloat64:
		if f.HasDefault {
			return fmt.Sprintf("f64::from_bits(0x%016x)", math.Float64bits(f.DefFloat))
		}
		return "0.0f64"
	}
	if f.HasDefault && f.DefInt != nil {
		return f.DefInt.String() + t
	}
	return "0" + t
}

// enumDefault is an enum field's declared default variant, None otherwise.
func enumDefault(f *ir.Field) string {
	if f.HasDefault && f.DefVariant != "" && f.DefVariant != "None" {
		return f.Type.Name + "::" + rustVariant(f.DefVariant)
	}
	return f.Type.Name + "::None"
}

// fieldDefault is the value a fresh field holds and an absent field reads
// (SPEC §5, docs/SPEC-TABLES.md §4).
func fieldDefault(f *ir.Field) string {
	if f.Type.Optional {
		return "None"
	}
	if f.KeyEnum != "" && f.KeyEnumRef != nil {
		return fmt.Sprintf("(0..%d).map(|_| %s).collect::<Vec<_>>()", len(f.KeyEnumRef.Variants), fieldDefault(scalarOnly(f)))
	}
	if f.Array == ir.ArrayFixed {
		return fmt.Sprintf("(0..%d).map(|_| %s).collect::<Vec<_>>()", f.ArrayBound, fieldDefault(scalarOnly(f)))
	}
	if f.Array != ir.ArrayNone {
		return "Vec::new()"
	}
	switch classOf(f) {
	case vBlob, vPointer:
		return "None"
	case vString:
		if len(f.DefBytes) == 0 {
			return "String::new()"
		}
		return "String::from(" + rustStr(f.DefBytes) + ")"
	case vBytes:
		if len(f.DefBytes) == 0 {
			return "Vec::new()"
		}
		return rustByteArray(f.DefBytes) + ".to_vec()"
	case vEnum:
		return enumDefault(f)
	case vTable:
		return f.Type.Name + "::default()"
	case vMap:
		return "Vec::new()"
	case vUnion:
		return f.Type.Ref.(*ir.Union).Name + "::None"
	}
	return scalarDefault(f)
}

// zeroLiteral matches a numeric zero in any of fieldDefault's spellings
// (`0`, `0i32`, `0u64`, `0.0f32`, `0.0`).
var zeroLiteral = regexp.MustCompile(`^0(\.0)?_?([iu](8|16|32|64|128|size)|f32|f64)?$`)

// defaultIsDerived reports whether a field's declared default (fieldDefault)
// is the value its type's own Default produces, so a derive can stand in for
// the hand-written impl. Table enums and unions derive Default as None
// (emitVariableEnums, the union emitters), and a nested table's Default is
// its own.
func defaultIsDerived(f *ir.Field) bool {
	d := fieldDefault(f)
	switch d {
	case "None", "String::new()", "Vec::new()", "false":
		return true
	}
	if zeroLiteral.MatchString(d) {
		return true
	}
	if f.Type.Optional || f.KeyEnum != "" || f.Array != ir.ArrayNone {
		return false
	}
	switch classOf(f) {
	case vTable:
		return d == f.Type.Name+"::default()"
	case vEnum:
		return d == f.Type.Name+"::None"
	case vUnion:
		return d == f.Type.Ref.(*ir.Union).Name+"::None"
	}
	return false
}

func (g *gen) collectVariableEnums(names []string) []*ir.Enum {
	seen := map[string]bool{}
	var out []*ir.Enum
	seenUnion := map[*ir.Union]bool{}
	var walkField func(f *ir.Field)
	walkField = func(f *ir.Field) {
		if f == nil {
			return
		}
		if f.KeyEnumRef != nil && !seen[f.KeyEnumRef.Name] {
			seen[f.KeyEnumRef.Name] = true
			out = append(out, f.KeyEnumRef)
		}
		if e, ok := f.Type.Ref.(*ir.Enum); ok && !seen[e.Name] {
			seen[e.Name] = true
			out = append(out, e)
		}
		if f.IsMap() && f.MapEntry != nil {
			for _, ef := range f.MapEntry.Fields {
				walkField(ef)
			}
		}
		if un := unionOf(f); un != nil && !seenUnion[un] {
			seenUnion[un] = true
			for _, v := range un.Variants {
				walkField(v.F)
			}
		}
	}
	for _, n := range names {
		for _, f := range vStruct(g.unit, n).Fields {
			walkField(f)
		}
	}
	return out
}

// emitVariableEnums emits the given table enums.
func emitVariableEnums(b *strings.Builder, enums []*ir.Enum) {
	for _, e := range enums {
		fmt.Fprintf(b, "/// %s — an enum on the variable wire (docs/SPEC-TABLES.md §3, §5).\n", e.Name)
		for _, v := range e.Variants {
			if r := rustVariant(v); len(r) > 1 && strings.ToUpper(r) == r {
				fmt.Fprintf(b, "#[allow(clippy::upper_case_acronyms)] // variant names are fixed across targets (SPEC §4.2)\n")
				break
			}
		}
		fmt.Fprintf(b, "#[derive(Clone, Copy, PartialEq, Eq, Default, Debug)]\n")
		fmt.Fprintf(b, "pub enum %s {\n", e.Name)
		fmt.Fprintf(b, "    #[default]\n    None,\n")
		for _, v := range e.Variants {
			fmt.Fprintf(b, "    %s,\n", rustVariant(v))
		}
		b.WriteString("}\n\n")
	}
}

// emitVariableStruct emits one table's storage struct and its Default, which
// is the DECLARED defaults (SPEC §5) — the value a fresh table holds, an absent
// field reads, and the writer elides against (docs/SPEC-TABLES.md §3).
//
// When every declared default is its type's own Default, the impl is derived
// rather than hand-written: the two are the same value, and clippy's
// derivable_impls says so.
func (g *gen) emitVariableStruct(b *strings.Builder, st *ir.Struct) {
	derived := true
	for _, f := range st.Fields {
		derived = derived && defaultIsDerived(f)
	}
	fmt.Fprintf(b, "/// %s — a variable-table storage struct (docs/SPEC-TABLES.md §3).\n", st.Name)
	if derived {
		fmt.Fprintf(b, "#[derive(Clone, Debug, Default)]\n")
	} else {
		fmt.Fprintf(b, "#[derive(Clone, Debug)]\n")
	}
	fmt.Fprintf(b, "pub struct %s {\n", st.Name)
	for _, f := range st.Fields {
		fmt.Fprintf(b, "    pub %s: %s,\n", ir.RustSnake(f.Name), variableFieldType(f))
	}
	b.WriteString("}\n\n")
	if derived {
		return
	}
	fmt.Fprintf(b, "impl Default for %s {\n", st.Name)
	fmt.Fprintf(b, "    fn default() -> Self {\n")
	fmt.Fprintf(b, "        %s {\n", st.Name)
	for _, f := range st.Fields {
		fmt.Fprintf(b, "            %s: %s,\n", ir.RustSnake(f.Name), fieldDefault(f))
	}
	fmt.Fprintf(b, "        }\n    }\n}\n\n")
}

// emitVariableNumber emits <name>_number: the save's numbering walk over one
// body (docs/SPEC-TABLES.md §3.1) — fields in declaration order, elements in
// index order, a node numbered at its first visit and descended at once, and
// every by-value edge descended to reach the pointer slots inside it.
func (g *gen) emitVariableNumber(b *strings.Builder, st *ir.Struct) {
	fmt.Fprintf(b, "/// %s — the numbering walk over one %s body (docs/SPEC-TABLES.md §3.1).\n", fn(st.Name, "number"), st.Name)
	fmt.Fprintf(b, "#[allow(unused_variables)]\n")
	fmt.Fprintf(b, "pub fn %s(v: &%s, g: &mut TableNodeGraph) {\n", fn(st.Name, "number"), st.Name)
	conds := guardConds(st, "v")
	for _, f := range st.Fields {
		// a field under a false guard is not written, so it is not an edge
		// (§3.1): the numbering does not visit what it holds
		guardedField(b, conds[f.Name], func(b *strings.Builder) { g.emitFieldNumber(b, f) })
	}
	fmt.Fprintf(b, "}\n\n")
}

// emitFieldNumber emits one field's step of the numbering walk.
func (g *gen) emitFieldNumber(b *strings.Builder, f *ir.Field) {
	x := "v." + ir.RustSnake(f.Name)
	if f.Type.Optional {
		switch classOf(f) {
		case vPointer:
			if f.Array != ir.ArrayNone {
				fmt.Fprintf(b, "    if let Some(arr) = &%s { for n in arr.iter().flatten() { if g.visit_table(0x%016x, n.clone()) { %s(n, g); } } }\n", x, ir.TableWireId(f.Type.Ref.(*ir.Struct).WireName()), fn(f.Type.Name, "number"))
			}
		case vTable:
			if f.Array != ir.ArrayNone {
				fmt.Fprintf(b, "    if let Some(arr) = &%s { for e in arr.iter() { %s(e, g); } }\n", x, fn(f.Type.Name, "number"))
			} else {
				fmt.Fprintf(b, "    if let Some(inner) = &%s { %s(inner, g); }\n", x, fn(f.Type.Name, "number"))
			}
		case vUnion:
			if f.Array != ir.ArrayNone {
				fmt.Fprintf(b, "    if let Some(arr) = &%s { for e in arr.iter() {\n", x)
				g.emitUnionNumber(b, f, "(*e)")
				fmt.Fprintf(b, "    } }\n")
			} else {
				fmt.Fprintf(b, "    if let Some(inner) = &%s {\n", x)
				g.emitUnionNumber(b, f, "(*inner)")
				fmt.Fprintf(b, "    }\n")
			}
		}
		return
	}
	switch classOf(f) {
	case vBlob:
		bytes := "b.as_bytes()"
		if f.Type.Kind == ir.TBytes {
			bytes = "&b[..]"
		}
		fmt.Fprintf(b, "    if let Some(b) = &%s { g.visit_blob(%s, Rc::as_ptr(b) as *const u8 as usize, %s); }\n", x, blobTypeConst(f), bytes)
	case vPointer:
		id, number := ir.TableWireId(f.Type.Ref.(*ir.Struct).WireName()), fn(f.Type.Name, "number")
		if f.KeyEnum != "" || f.Array != ir.ArrayNone {
			fmt.Fprintf(b, "    for n in %s.iter().flatten() { if g.visit_table(0x%016x, n.clone()) { %s(n, g); } }\n", x, id, number)
		} else {
			fmt.Fprintf(b, "    if let Some(n) = &%s && g.visit_table(0x%016x, n.clone()) { %s(n, g); }\n", x, id, number)
		}
	case vTable:
		if f.KeyEnum != "" || f.Array != ir.ArrayNone {
			fmt.Fprintf(b, "    for e in %s.iter() { %s(e, g); }\n", x, fn(f.Type.Name, "number"))
		} else {
			fmt.Fprintf(b, "    %s(&%s, g);\n", fn(f.Type.Name, "number"), x)
		}
	case vMap:
		g.emitMapNumber(b, f, x)
	case vUnion:
		if f.KeyEnum != "" || f.Array != ir.ArrayNone {
			fmt.Fprintf(b, "    for e in %s.iter() {\n", x)
			g.emitUnionNumber(b, f, "(*e)")
			fmt.Fprintf(b, "    }\n")
		} else {
			g.emitUnionNumber(b, f, x)
		}
	}
}

func blobTypeConst(f *ir.Field) string {
	if f.Type.Kind == ir.TString {
		return "STRING_TYPE_ID"
	}
	return "BYTES_TYPE_ID"
}

// emitVariableSaveBody emits <name>_save_body: the field walk that appends one
// body's fields to `out` (the caller writes the zero reference), threading the
// shared id table and the save's numbering.
func (g *gen) emitVariableSaveBody(b *strings.Builder, st *ir.Struct) {
	fnName := fn(st.Name, "save_body")
	fmt.Fprintf(b, "/// %s — one %s body's fields (docs/SPEC-TABLES.md §3), in declaration\n", fnName, st.Name)
	fmt.Fprintf(b, "/// order; the caller writes the zero reference. `retain` and `report` are\n")
	fmt.Fprintf(b, "/// Some on the retaining path, where each nested body emits its retained tail\n")
	fmt.Fprintf(b, "/// before its zero reference (docs/SPEC-TABLES.md §6.6); None on the plain path.\n")
	fmt.Fprintf(b, "#[allow(unused_variables, unused_mut, clippy::too_many_arguments)]\n")
	fmt.Fprintf(b, "pub fn %s(v: &%s, out: &mut Vec<u8>, ids: &mut IdTable, g: &TableNodeGraph, mut retain: Option<&mut TableRetain>, mut report: Option<&mut TableReport>, path: TableRetainPath) {\n", fnName, st.Name)
	conds := guardConds(st, "v")
	for i, f := range st.Fields {
		// a field under a false guard is not written (§4), nor anything it
		// holds, on the plain and the retaining path alike
		guardedField(b, conds[f.Name], func(b *strings.Builder) {
			g.emitVariableSaveField(b, f, i, "v."+ir.RustSnake(f.Name), bodyIndent)
		})
	}
	fmt.Fprintf(b, "}\n\n")
}

// emitTableBodyInto emits the build of one nested table body into `dst`: its
// fields, its retained tail on the retaining path, then the zero reference.
func emitTableBodyInto(b *strings.Builder, f *ir.Field, src, dst, pathExpr, ind string) {
	fmt.Fprintf(b, "%slet bp = %s;\n", ind, pathExpr)
	fmt.Fprintf(b, "%s%s(%s, &mut %s, ids, g, retain.as_deref_mut(), report.as_deref_mut(), bp);\n", ind, fn(f.Type.Name, "save_body"), src, dst)
	fmt.Fprintf(b, "%sif let (Some(ret), Some(rep)) = (retain.as_mut(), report.as_mut()) {\n", ind)
	fmt.Fprintf(b, "%s    retain_emit_tail(&mut %s, ids, ret, KNOWN_IDS, bp, rep);\n", ind, dst)
	fmt.Fprintf(b, "%s}\n", ind)
	fmt.Fprintf(b, "%s%s.push(0); // the body's zero reference\n", ind, dst)
}

// scalarWrite is the write of one scalar at its wire kind's width, little
// endian (§3): a float as its IEEE-754 bit pattern with no canonicalisation,
// and a NaN or an infinity the save's refusal by name (§3.3, the finite rule).
func (g *gen) scalarWrite(f *ir.Field, x, dst string) string {
	kind := scalarWireKind(f)
	switch f.Type.Kind {
	case ir.TBool:
		return fmt.Sprintf("%s.push(if %s { 1 } else { 0 });", dst, x)
	case ir.TFloat32, ir.TFloat64:
		return fmt.Sprintf("if !%s.is_finite() { table_float_refuse(ids, %q); } %s.extend_from_slice(&%s.to_bits().to_le_bytes());", x, g.floatOwner(f), dst, x)
	}
	w := ir.TableKindWidth(kind) * 8
	t := fmt.Sprintf("u%d", w)
	if ir.TableKindSigned(kind) {
		t = fmt.Sprintf("i%d", w)
	}
	return fmt.Sprintf("%s.extend_from_slice(&%s.to_le_bytes());", dst, rustCast(x, scalarRust(f), t))
}

// rustCast renders `(x as to)`, or `(x)` when x's storage type `from` is
// already `to` (clippy's unnecessary_cast).
func rustCast(x, from, to string) string {
	if from == to {
		if strings.HasPrefix(x, "(") && strings.HasSuffix(x, ")") || plainPath.MatchString(x) {
			return x
		}
		return "(" + x + ")"
	}
	return fmt.Sprintf("(%s as %s)", x, to)
}

// plainPath is a variable or field path, which needs no parentheses to take a
// method call.
var plainPath = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z0-9_]+)*$`)

// rustCastStmt is rustCast for an assignment's right-hand side.
func rustCastStmt(x, from, to string) string {
	if from == to {
		return x
	}
	return x + " as " + to
}

// enumRef is an enum value's reference expression: the zero reference for
// None, the variant name's id interned otherwise (§3).
func enumRef(f *ir.Field, x string) string {
	e := f.Type.Ref.(*ir.Enum)
	var s strings.Builder
	fmt.Fprintf(&s, "match %s { %s::None => 0", x, e.Name)
	for i, v := range e.Variants {
		fmt.Fprintf(&s, ", %s::%s => ids.id_ref(0x%016x)", e.Name, rustVariant(v), ir.TableWireId(e.VariantWireName(i)))
	}
	s.WriteString(" }")
	return s.String()
}

// emitVariableSaveField emits the elision guard and the write for one field,
// in the oracle's order: the field id is interned before anything its payload
// interns, because the header rides first and first-use order follows the
// write order (docs/SPEC-TABLES.md §3).
func (g *gen) emitVariableSaveField(b *strings.Builder, f *ir.Field, ordinal int, x, ind string) {
	id := ir.TableFieldWireId(f)
	kind := fieldWireKind(f)
	inner := ind + "    "
	if f.Type.Optional {
		g.emitOptionalSaveField(b, f, ordinal, x, ind)
		return
	}
	if f.KeyEnum != "" {
		g.emitKeyedSave(b, f, ordinal, x, ind)
		return
	}
	if f.Array == ir.ArrayFixed {
		g.emitFixedArraySave(b, f, ordinal, x, ind)
		return
	}
	if f.Array != ir.ArrayNone {
		// AN UNBOUNDED ARRAY IS THE COUNTED ARRAY'S OWN BYTES (§2.9): kind 14,
		// L, the element kind, the live count, the elements; empty elides.
		fmt.Fprintf(b, "%sif !%s.is_empty() {\n", ind, x)
		count := x + ".len()"
		if f.Array == ir.ArrayCounted {
			count = fmt.Sprintf("%s.len().min(%d)", x, f.ArrayBound)
		}
		fmt.Fprintf(b, "%slet r = ids.id_ref(0x%016x);\n", inner, id)
		fmt.Fprintf(b, "%slet count = %s;\n", inner, count)
		fmt.Fprintf(b, "%slet mut body = Vec::new();\n", inner)
		fmt.Fprintf(b, "%sbody.push(%d); // the element kind\n", inner, elemWireKind(f))
		fmt.Fprintf(b, "%sleb_write(&mut body, count as u64);\n", inner)
		fmt.Fprintf(b, "%sfor (j, e) in %s.iter().take(count).enumerate() {\n", inner, x)
		ein := inner + "    "
		switch classOf(f) {
		case vTable:
			fmt.Fprintf(b, "%slet mut eb = Vec::new();\n", ein)
			emitTableBodyInto(b, f, "e", "eb", fmt.Sprintf("path.step(%d, j as u32)", ordinal), ein)
			fmt.Fprintf(b, "%sleb_write(&mut body, eb.len() as u64);\n", ein)
			fmt.Fprintf(b, "%sbody.extend_from_slice(&eb);\n", ein)
		case vPointer:
			fmt.Fprintf(b, "%slet _ = j;\n", ein)
			fmt.Fprintf(b, "%sleb_write(&mut body, match e { Some(n) => g.node_index(n), None => 0 });\n", ein)
		case vEnum:
			fmt.Fprintf(b, "%slet _ = j;\n", ein)
			fmt.Fprintf(b, "%slet vref = %s;\n", ein, enumRef(f, "*e"))
			fmt.Fprintf(b, "%sleb_write(&mut body, vref);\n", ein)
		case vUnion:
			g.withArmIndex("j as u32", func() { g.emitUnionElement(b, f, "e", "body", ordinal, ein) })
		default:
			fmt.Fprintf(b, "%slet _ = j;\n", ein)
			fmt.Fprintf(b, "%s%s\n", ein, g.scalarWrite(f, "(*e)", "body"))
		}
		fmt.Fprintf(b, "%s}\n", inner)
		fmt.Fprintf(b, "%sleb_write(out, r);\n", inner)
		fmt.Fprintf(b, "%sout.push(%d);\n", inner, kind)
		fmt.Fprintf(b, "%sleb_write(out, body.len() as u64);\n", inner)
		fmt.Fprintf(b, "%sout.extend_from_slice(&body);\n", inner)
		fmt.Fprintf(b, "%s}\n", ind)
		return
	}
	switch classOf(f) {
	case vBlob:
		// a NON-NULL blob ALWAYS rides, a zero-length one included (§2.5)
		fmt.Fprintf(b, "%sif let Some(n) = &%s {\n", ind, x)
		fmt.Fprintf(b, "%sleb_write(out, ids.id_ref(0x%016x));\n", inner, id)
		fmt.Fprintf(b, "%sout.push(%d);\n", inner, kind)
		fmt.Fprintf(b, "%sleb_write(out, g.blob_index(Rc::as_ptr(n) as *const u8 as usize));\n", inner)
		fmt.Fprintf(b, "%s}\n", ind)
	case vPointer:
		// NULL IS INDEX 0, AND NULL IS ELIDED; a non-null pointer always rides (§3.1)
		fmt.Fprintf(b, "%sif let Some(n) = &%s {\n", ind, x)
		fmt.Fprintf(b, "%sleb_write(out, ids.id_ref(0x%016x));\n", inner, id)
		fmt.Fprintf(b, "%sout.push(%d);\n", inner, kind)
		fmt.Fprintf(b, "%sleb_write(out, g.node_index(n));\n", inner)
		fmt.Fprintf(b, "%s}\n", ind)
	case vTable:
		// AN ELIDED FIELD COSTS NOTHING IN THE ID TABLE EITHER (§3): the field
		// id is interned, the body built, and both undone when the body is its
		// terminator alone — an all-default nested table elides
		fmt.Fprintf(b, "%s{\n", ind)
		fmt.Fprintf(b, "%slet mark = ids.mark();\n", inner)
		fmt.Fprintf(b, "%slet r = ids.id_ref(0x%016x);\n", inner, id)
		fmt.Fprintf(b, "%slet mut body = Vec::new();\n", inner)
		emitTableBodyInto(b, f, "&"+x, "body", fmt.Sprintf("path.step(%d, 0)", ordinal), inner)
		fmt.Fprintf(b, "%sif body.len() <= 1 {\n", inner)
		fmt.Fprintf(b, "%s    ids.rollback(mark);\n", inner)
		fmt.Fprintf(b, "%s} else {\n", inner)
		fmt.Fprintf(b, "%s    leb_write(out, r);\n", inner)
		fmt.Fprintf(b, "%s    out.push(%d);\n", inner, kind)
		fmt.Fprintf(b, "%s    leb_write(out, body.len() as u64);\n", inner)
		fmt.Fprintf(b, "%s    out.extend_from_slice(&body);\n", inner)
		fmt.Fprintf(b, "%s}\n", inner)
		fmt.Fprintf(b, "%s}\n", ind)
	case vString:
		// at its default, empty when none is declared (§3)
		guard := "!" + x + ".is_empty()"
		if len(f.DefBytes) > 0 {
			guard = x + ".as_bytes() != " + rustByteArray(f.DefBytes)
		}
		fmt.Fprintf(b, "%sif %s {\n", ind, guard)
		fmt.Fprintf(b, "%sleb_write(out, ids.id_ref(0x%016x));\n", inner, id)
		fmt.Fprintf(b, "%sout.push(%d);\n", inner, kind)
		fmt.Fprintf(b, "%sleb_write(out, %s.len() as u64);\n", inner, x)
		fmt.Fprintf(b, "%sout.extend_from_slice(%s.as_bytes());\n", inner, x)
		fmt.Fprintf(b, "%s}\n", ind)
	case vBytes:
		// bytes(N) rides as an array of u8 (§2.5): kind 14, L = 2 + n, element
		// kind 6, the count, the bytes; at its default it elides
		guard := "!" + x + ".is_empty()"
		if len(f.DefBytes) > 0 {
			guard = x + ".as_slice() != " + rustByteArray(f.DefBytes)
		}
		fmt.Fprintf(b, "%sif %s {\n", ind, guard)
		fmt.Fprintf(b, "%slet r = ids.id_ref(0x%016x);\n", inner, id)
		fmt.Fprintf(b, "%slet mut body = Vec::new();\n", inner)
		fmt.Fprintf(b, "%sbody.push(%d); // the element kind: u8\n", inner, ir.TableKindU8)
		fmt.Fprintf(b, "%sleb_write(&mut body, %s.len() as u64);\n", inner, x)
		fmt.Fprintf(b, "%sbody.extend_from_slice(&%s);\n", inner, x)
		fmt.Fprintf(b, "%sleb_write(out, r);\n", inner)
		fmt.Fprintf(b, "%sout.push(%d);\n", inner, kind)
		fmt.Fprintf(b, "%sleb_write(out, body.len() as u64);\n", inner)
		fmt.Fprintf(b, "%sout.extend_from_slice(&body);\n", inner)
		fmt.Fprintf(b, "%s}\n", ind)
	case vEnum:
		fmt.Fprintf(b, "%sif %s != %s {\n", ind, x, enumDefault(f))
		fmt.Fprintf(b, "%sleb_write(out, ids.id_ref(0x%016x));\n", inner, id)
		fmt.Fprintf(b, "%sout.push(%d);\n", inner, kind)
		fmt.Fprintf(b, "%slet vref = %s;\n", inner, enumRef(f, x))
		fmt.Fprintf(b, "%sleb_write(out, vref);\n", inner)
		fmt.Fprintf(b, "%s}\n", ind)
	case vMap:
		g.emitMapSave(b, f, ordinal, x, ind)
	case vUnion:
		g.emitUnionSave(b, f, ordinal, x, ind)
	default:
		// a field holding its declared default is not written at all (§3)
		guard := scalarDiffers(f, x, scalarDefault(f))
		if f.Type.Kind == ir.TBool {
			guard = x
			if f.HasDefault && f.DefBool {
				guard = "!" + x
			}
		}
		fmt.Fprintf(b, "%sif %s {\n", ind, guard)
		fmt.Fprintf(b, "%sleb_write(out, ids.id_ref(0x%016x));\n", inner, id)
		fmt.Fprintf(b, "%sout.push(%d);\n", inner, kind)
		fmt.Fprintf(b, "%s%s\n", inner, g.scalarWrite(f, x, "out"))
		fmt.Fprintf(b, "%s}\n", ind)
	}
}

// intRange is the range one integer value clamps to on read (§4): the declared
// min/max when there is one, and otherwise none on the wire (bits(N) clamps to
// its width separately, as the oracle's scalarAt does).
func intRange(f *ir.Field) (lo, hi string, ok bool) {
	if !f.HasIntRange || f.IntMin == nil || f.IntMax == nil {
		return "", "", false
	}
	return f.IntMin.String(), f.IntMax.String(), true
}

// clampStmt is §4's declared clamp (docs/SPEC-TABLES.md §4) on a decode local
// of `bits` width and the given signedness, spelled with `suffix` and counted
// on `counter`: each end that sits ON the local's own limit is elided, because
// a comparison that cannot fire is a deny-by-default clippy error
// (absurd_extreme_comparisons) and describes a value the local cannot hold —
// the same "this check cannot fire" rule tables/examples/Ranges.schema pins
// for the C and C++ legs. "" when there is no declared range or neither end
// can fire.
func clampStmt(f *ir.Field, bits int, signed bool, suffix, counter, ind string) string {
	// a fixed field clamps on the RAW scale: its whole-unit bounds shifted by
	// F (docs/SPEC-TABLES.md §4), the integers' bounds as they are
	rlo, rhi, ok := ir.TableRawRange(f)
	if !ok {
		return ""
	}
	localMin, localMax := new(big.Int), new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))
	if signed {
		localMin = new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), uint(bits-1)))
		localMax = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits-1)), big.NewInt(1))
	}
	lo, hi := rlo.String(), rhi.String()
	low := fmt.Sprintf("if x < %s%s { x = %s%s; %s += 1; }", lo, suffix, lo, suffix, counter)
	high := fmt.Sprintf("if x > %s%s { x = %s%s; %s += 1; }", hi, suffix, hi, suffix, counter)
	switch canLow, canHigh := rlo.Cmp(localMin) > 0, rhi.Cmp(localMax) < 0; {
	case canLow && canHigh:
		return ind + low + " else " + high + "\n"
	case canLow:
		return ind + low + "\n"
	case canHigh:
		return ind + high + "\n"
	}
	return ""
}

func scalarReadFn(f *ir.Field) string {
	if f.Type.Width == 128 {
		return "scalar_read_128"
	}
	return "table_scalar_read"
}

// emitScalarConvert emits the conversion of `raw` — read at the wire kind held
// in `kvar` — into the destination expression `dst`, with §4's declared clamp
// and bits(N)'s implied one, exactly as the oracle's scalarAt orders them.
// owner is a float's <Owner>.<field>, the name its non_finite_float refusal
// carries (§3.3).
func emitScalarConvert(b *strings.Builder, f *ir.Field, owner, dst, kvar, ind string) {
	switch f.Type.Kind {
	case ir.TBool:
		// a bool byte that is neither 0 nor 1 keeps true and is MALFORMED on
		// the tolerant read (docs/SPEC-TABLES.md §6.5), as the oracle's scalarAt
		fmt.Fprintf(b, "%s%s = raw != 0;\n", ind, dst)
		fmt.Fprintf(b, "%sif raw > 1 { report.malformed = true; }\n", ind)
		return
	case ir.TFloat32:
		// A NaN OR AN INFINITY IS REFUSED ON ITS RAW BITS, before the bits
		// become a value (docs/SPEC-TABLES.md §3.3): the read names the field
		// and the load refuses as non_finite_float, the tolerant one included,
		// so the 0.0 that stands in is never handed to a caller.
		fmt.Fprintf(b, "%s%s = if table_finite_f32(raw as u32) { f32::from_bits(raw as u32) } else { report.non_finite(%q); 0.0 };\n", ind, dst, owner)
		return
	case ir.TFloat64:
		// the float rung (kind 10 into a float64) is the ordinary conversion:
		// every finite float32 widens exactly (§3.3, §4)
		fmt.Fprintf(b, "%s%s = if %s == %d { if table_finite_f32(raw as u32) { f64::from(f32::from_bits(raw as u32)) } else { report.non_finite(%q); 0.0 } } else if table_finite_f64(raw) { f64::from_bits(raw) } else { report.non_finite(%q); 0.0 };\n", ind, dst, kvar, ir.TableKindF32, owner, owner)
		return
	}
	signed := (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) && f.Type.Signed
	// the local is mutable only when a clamp below can write it
	mut := func(clamps string) string {
		if clamps == "" {
			return ""
		}
		return "mut "
	}
	if f.Type.Width == 128 {
		suffix, from := "u128", "raw"
		if signed {
			suffix, from = "i128", "raw as i128"
		}
		clamp := clampStmt(f, 128, signed, suffix, "report.clamped", ind)
		fmt.Fprintf(b, "%slet %sx = %s;\n", ind, mut(clamp), from)
		b.WriteString(clamp)
		fmt.Fprintf(b, "%s%s = %s;\n", ind, dst, rustCastStmt("x", suffix, scalarRust(f)))
		return
	}
	suffix, from := "u64", "raw"
	if signed {
		suffix, from = "i64", "raw as i64"
	}
	clamp := clampStmt(f, 64, signed, suffix, "report.clamped", ind)
	if f.Type.Kind == ir.TBits && f.Type.Width < 64 {
		max := uint64(1)<<uint(f.Type.Width) - 1
		clamp += fmt.Sprintf("%sif x > %du64 { x = %du64; report.clamped += 1; }\n", ind, max, max)
	}
	fmt.Fprintf(b, "%slet %sx = %s;\n", ind, mut(clamp), from)
	b.WriteString(clamp)
	fmt.Fprintf(b, "%s%s = %s;\n", ind, dst, rustCastStmt("x", suffix, scalarRust(f)))
}

// emitVariableLoadBody emits <name>_load_body: the tolerant scan of one body
// (docs/SPEC-TABLES.md §4, §5) into the storage struct. A pointer field's
// payload is a NUMBER, bounds-checked and resolved against the numbering the
// root gathered first (§3.1); `nested` says the body is not the root's, where
// the reserved node-table id is malformed.
func (g *gen) emitVariableLoadBody(b *strings.Builder, st *ir.Struct) {
	fnName := fn(st.Name, "load_body")
	fmt.Fprintf(b, "/// %s — the tolerant read of one %s body (docs/SPEC-TABLES.md §4, §5).\n", fnName, st.Name)
	fmt.Fprintf(b, "/// `retain` is Some on the retaining path, and every unknown field it names\n")
	fmt.Fprintf(b, "/// is captured into the store under `path` (docs/SPEC-TABLES.md §6.6); None on\n")
	fmt.Fprintf(b, "/// the plain path.\n")
	fmt.Fprintf(b, "#[allow(unused_variables, unused_mut, clippy::too_many_arguments)]\n")
	fmt.Fprintf(b, "pub fn %s(wire: &[u8], off: &mut usize, end: usize, ids: &[u64], v: &mut %s, nodes: &mut TableNodeReader, report: &mut TableReport, path: TableRetainPath, mut retain: Option<&mut TableRetain>, nested: bool) -> bool {\n", fnName, st.Name)
	fmt.Fprintf(b, "    if end > wire.len() { report.malformed = true; return false; }\n")
	fmt.Fprintf(b, "    // LEBs in this body cannot borrow a byte from the enclosing body.\n")
	fmt.Fprintf(b, "    let leb_read = |bytes: &[u8], at: &mut usize| leb_read(&bytes[..end], at);\n")
	fmt.Fprintf(b, "    while *off < end {\n")
	fmt.Fprintf(b, "        let Some(r) = leb_read(wire, off) else { report.malformed = true; return false; };\n")
	fmt.Fprintf(b, "        if r == 0 { return true; }\n")
	fmt.Fprintf(b, "        let Some(id) = ref_id(ids, r) else { report.malformed = true; return false; };\n")
	fmt.Fprintf(b, "        if *off >= end { report.malformed = true; return false; }\n")
	fmt.Fprintf(b, "        let kind = wire[*off]; *off += 1;\n")
	fmt.Fprintf(b, "        if id == BUILD_VERSION_ID || id == VOCABULARY_ID { report.malformed = true; return false; } // the reserved announcement ids ride in the announcement and nowhere else (§3.3)\n")
	fmt.Fprintf(b, "        if id == NODE_TABLE_ID {\n")
	fmt.Fprintf(b, "            // the reserved id inside a NESTED body is malformed (§3.1); a\n")
	fmt.Fprintf(b, "            // variable-class root steps over its own node table (§3.1).\n")
	fmt.Fprintf(b, "            // A fixed-class root has no numbering and sees it as unknown (§4).\n")
	fmt.Fprintf(b, "            if nested { report.malformed = true; return false; }\n")
	if !st.FixedDeclared {
		fmt.Fprintf(b, "            if !leb_skip(wire, off, end, kind) { report.malformed = true; return false; }\n")
		fmt.Fprintf(b, "            continue;\n")
	}
	fmt.Fprintf(b, "        }\n")
	fmt.Fprintf(b, "        match id {\n")
	for i, f := range st.Fields {
		g.emitVariableLoadField(b, st, f, i, "            ")
	}
	// THE UNKNOWN ARM (docs/SPEC-TABLES.md §4, §6.6): the plain read skips the
	// field and counts one unknown; the retaining read also captures it, or
	// counts one retain_lost when the walk or the capacity cannot keep it whole.
	fmt.Fprintf(b, "            _ => {\n")
	fmt.Fprintf(b, "                report.unknown += 1;\n")
	fmt.Fprintf(b, "                let start = *off;\n")
	fmt.Fprintf(b, "                if !leb_skip(wire, off, end, kind) { report.malformed = true; return false; }\n")
	fmt.Fprintf(b, "                if let Some(ret) = retain.as_deref_mut() {\n")
	// A RETAINED UNKNOWN FLOAT IS TESTED LIKE A KNOWN ONE (docs/SPEC-TABLES.md
	// §3.3, §6.6, ruling 3 of 2026-10-27): a NaN or an infinity is not kept and
	// refuses the read as non_finite_float, named <owner>.0x<id>.
	fmt.Fprintf(b, "                    if !table_retained_float_finite(kind, &wire[start..*off]) { report.non_finite_unknown(%q, id); continue; }\n", st.DeclaredName())
	fmt.Fprintf(b, "                    match retain_capture(ids, kind, &wire[start..*off]) {\n")
	fmt.Fprintf(b, "                        Some(resolved) => {\n")
	fmt.Fprintf(b, "                            let need = 17 + 8 * path.depth as usize + resolved.len();\n")
	fmt.Fprintf(b, "                            if ret.bytes.len() + need <= ret.bytes.capacity() {\n")
	fmt.Fprintf(b, "                                ret.bytes.extend_from_slice(&(need as u32).to_le_bytes());\n")
	fmt.Fprintf(b, "                                ret.bytes.extend_from_slice(&path.depth.to_le_bytes());\n")
	fmt.Fprintf(b, "                                ret.bytes.extend_from_slice(&id.to_le_bytes());\n")
	fmt.Fprintf(b, "                                ret.bytes.push(kind);\n")
	fmt.Fprintf(b, "                                for i in 0..path.depth as usize {\n")
	fmt.Fprintf(b, "                                    let (o, x) = path.steps[i];\n")
	fmt.Fprintf(b, "                                    ret.bytes.extend_from_slice(&o.to_le_bytes());\n")
	fmt.Fprintf(b, "                                    ret.bytes.extend_from_slice(&x.to_le_bytes());\n")
	fmt.Fprintf(b, "                                }\n")
	fmt.Fprintf(b, "                                ret.bytes.extend_from_slice(&resolved);\n")
	fmt.Fprintf(b, "                                report.retained += 1;\n")
	fmt.Fprintf(b, "                            } else { report.retain_lost += 1; }\n")
	fmt.Fprintf(b, "                        }\n")
	fmt.Fprintf(b, "                        None => { report.retain_lost += 1; }\n")
	fmt.Fprintf(b, "                    }\n")
	fmt.Fprintf(b, "                }\n")
	fmt.Fprintf(b, "            }\n")
	fmt.Fprintf(b, "        }\n")
	fmt.Fprintf(b, "    }\n")
	fmt.Fprintf(b, "    report.malformed = true; false\n")
	fmt.Fprintf(b, "}\n\n")
}

const kindSkip = "report.kind_mismatch += 1; if !leb_skip(wire, off, end, kind) { report.malformed = true; return false; }"

// emitEnumDecode emits the read of one enum reference into `dst`: 0 is None, a
// reference past the table is framing damage, and an id this reader's enum
// cannot name is §4's ordinary unknown, the value reading None (§3).
func emitEnumDecode(b *strings.Builder, f *ir.Field, dst, bound, onBad, ind string) {
	e := f.Type.Ref.(*ir.Enum)
	fmt.Fprintf(b, "%slet Some(vref) = leb_read(wire, off) else { report.malformed = true; %s };\n", ind, onBad)
	fmt.Fprintf(b, "%sif *off > %s { report.malformed = true; %s }\n", ind, bound, onBad)
	// one assignment, so dst may be a fresh `let`; onBad diverges, so a
	// refused reference leaves dst as it was
	fmt.Fprintf(b, "%s%s = if vref == 0 { %s::None }\n", ind, dst, e.Name)
	fmt.Fprintf(b, "%selse if vref as usize > ids.len() { report.malformed = true; %s }\n", ind, onBad)
	fmt.Fprintf(b, "%selse {\n", ind)
	fmt.Fprintf(b, "%s    match ids[vref as usize - 1] {\n", ind)
	for i, v := range e.Variants {
		fmt.Fprintf(b, "%s        0x%016x => %s::%s,\n", ind, ir.TableWireId(e.VariantWireName(i)), e.Name, rustVariant(v))
	}
	fmt.Fprintf(b, "%s        _ => { report.unknown += 1; if retain.is_some() { report.retain_lost += 1; } %s::None },\n", ind, e.Name)
	fmt.Fprintf(b, "%s    }\n", ind)
	fmt.Fprintf(b, "%s};\n", ind)
}

// emitPointerResolve emits the resolution of one node index into `dst` against
// the numbering (§3.1): the node is decoded once, at the first slot that names
// it, and every later slot shares it.
func emitPointerResolve(b *strings.Builder, f *ir.Field, dst, ind string) {
	if f.Type.Blob() {
		if f.Type.Kind == ir.TString {
			fmt.Fprintf(b, "%s%s = nodes.text_slot(index, report);\n", ind, dst)
		} else {
			fmt.Fprintf(b, "%s%s = nodes.bytes_slot(index, report);\n", ind, dst)
		}
		return
	}
	tid := ir.TableWireId(f.Type.Ref.(*ir.Struct).WireName())
	fmt.Fprintf(b, "%s%s = match nodes.node_slot(index, 0x%016x, report) {\n", ind, dst, tid)
	fmt.Fprintf(b, "%s    Some(k) => variable_node_build(wire, ids, nodes, k, report).and_then(|n| n.downcast::<%s>().ok()),\n", ind, f.Type.Name)
	fmt.Fprintf(b, "%s    None => None,\n", ind)
	fmt.Fprintf(b, "%s};\n", ind)
}

// emitVariableLoadField emits one field's decode arm inside the load match.
// ordinal is the field's position in its body's declaration order, the step
// the retention path descends through (docs/SPEC-TABLES.md §6.6).
func (g *gen) emitVariableLoadField(b *strings.Builder, st *ir.Struct, f *ir.Field, ordinal int, ind string) {
	id := ir.TableFieldWireId(f)
	field := "v." + ir.RustSnake(f.Name)
	kind := fieldWireKind(f)
	fmt.Fprintf(b, "%s0x%016x => {\n", ind, id)
	in := ind + "    "
	if f.Type.Optional {
		g.emitOptionalLoadField(b, f, ordinal, field, in)
		fmt.Fprintf(b, "%s}\n", ind)
		return
	}
	if f.KeyEnum != "" {
		g.emitKeyedLoad(b, f, ordinal, field, in)
		fmt.Fprintf(b, "%s}\n", ind)
		return
	}
	if f.Array != ir.ArrayNone {
		g.emitArrayLoad(b, f, ordinal, field, in)
		fmt.Fprintf(b, "%s}\n", ind)
		return
	}
	switch classOf(f) {
	case vBlob, vPointer:
		// A pointer field's payload is a NUMBER: bounds-checked and resolved,
		// never followed (§3.1). Any other kind is a kind mismatch, skipped by
		// its own kind's rule, the pointer null.
		fmt.Fprintf(b, "%sif kind != %d { %s }\n", in, kind, kindSkip)
		fmt.Fprintf(b, "%selse {\n", in)
		fmt.Fprintf(b, "%s    let Some(index) = leb_read(wire, off) else { report.malformed = true; return false; };\n", in)
		fmt.Fprintf(b, "%s    if *off > end { report.malformed = true; return false; }\n", in)
		emitPointerResolve(b, f, field, in+"    ")
		fmt.Fprintf(b, "%s}\n", in)
	case vTable:
		// THE BODY IS RESET, not merged, on every occurrence; a body whose
		// terminator is not the last byte of its L is framing damage, the field
		// reads its declared defaults and the parent reads on past L (§3, §4)
		fmt.Fprintf(b, "%sif kind != %d { %s }\n", in, kind, kindSkip)
		fmt.Fprintf(b, "%selse {\n", in)
		fmt.Fprintf(b, "%s    let Some(l) = leb_read(wire, off) else { report.malformed = true; return false; };\n", in)
		fmt.Fprintf(b, "%s    if *off > end || l > (end - *off) as u64 { report.malformed = true; return false; }\n", in)
		fmt.Fprintf(b, "%s    let sub = *off + l as usize;\n", in)
		fmt.Fprintf(b, "%s    if let Some(ret) = retain.as_deref_mut() { retain_discard(ret, path, %d); }\n", in, ordinal)
		fmt.Fprintf(b, "%s    %s = %s::default();\n", in, field, f.Type.Name)
		fmt.Fprintf(b, "%s    let mut o = *off;\n", in)
		fmt.Fprintf(b, "%s    let _ = %s(wire, &mut o, sub, ids, &mut %s, nodes, report, path.step(%d, 0), retain.as_deref_mut(), true);\n", in, fn(f.Type.Name, "load_body"), field, ordinal)
		fmt.Fprintf(b, "%s    if o != sub { report.malformed = true; %s = %s::default(); }\n", in, field, f.Type.Name)
		fmt.Fprintf(b, "%s    *off = sub;\n", in)
		fmt.Fprintf(b, "%s}\n", in)
	case vString:
		fmt.Fprintf(b, "%sif kind != %d { %s }\n", in, kind, kindSkip)
		fmt.Fprintf(b, "%selse {\n", in)
		fmt.Fprintf(b, "%s    let Some(l) = leb_read(wire, off) else { report.malformed = true; return false; };\n", in)
		fmt.Fprintf(b, "%s    if *off > end || l > (end - *off) as u64 { report.malformed = true; return false; }\n", in)
		fmt.Fprintf(b, "%s    let s = *off + l as usize;\n", in)
		fmt.Fprintf(b, "%s    let b = &wire[*off..s];\n", in)
		fmt.Fprintf(b, "%s    // ILL-FORMED TEXT IS DAMAGE (§3, §4): the field reads its declared default\n", in)
		fmt.Fprintf(b, "%s    if !text_valid(b) { report.malformed = true; %s = %s; }\n", in, field, fieldDefault(f))
		fmt.Fprintf(b, "%s    else if b.len() > %d {\n", in, f.Type.Size)
		fmt.Fprintf(b, "%s        // A CLAMP CUTS AT A CODE POINT BOUNDARY (§3)\n", in)
		fmt.Fprintf(b, "%s        let mut cut = %d;\n", in, f.Type.Size)
		fmt.Fprintf(b, "%s        while cut > 0 && b[cut] & 0xC0 == 0x80 { cut -= 1; }\n", in)
		fmt.Fprintf(b, "%s        %s = std::str::from_utf8(&b[..cut]).unwrap().to_string();\n", in, field)
		fmt.Fprintf(b, "%s        report.clamped += 1;\n", in)
		fmt.Fprintf(b, "%s    }\n", in)
		fmt.Fprintf(b, "%s    else { %s = std::str::from_utf8(b).unwrap().to_string(); }\n", in, field)
		fmt.Fprintf(b, "%s    *off = s;\n", in)
		fmt.Fprintf(b, "%s}\n", in)
	case vBytes:
		g.emitArrayLoad(b, f, ordinal, field, in)
	case vEnum:
		fmt.Fprintf(b, "%sif kind != %d { %s }\n", in, kind, kindSkip)
		fmt.Fprintf(b, "%selse {\n", in)
		emitEnumDecode(b, f, field, "end", "return false;", in+"    ")
		fmt.Fprintf(b, "%s}\n", in)
	case vMap:
		g.emitMapLoad(b, f, ordinal, field, in)
	case vUnion:
		g.emitUnionLoad(b, f, ordinal, field, in)
	default:
		// the declared kind, or a narrower one that WIDENS into it (§4):
		// decoded exactly at its own width and counted widened once
		fmt.Fprintf(b, "%sif kind == %d || table_kind_widens(kind, %d) {\n", in, kind, kind)
		fmt.Fprintf(b, "%s    let Some(raw) = %s(wire, off, end, kind) else { report.malformed = true; return false; };\n", in, scalarReadFn(f))
		if !st.IsMapEntry() || f.Name != ir.MapKeyFieldName {
			fmt.Fprintf(b, "%s    if kind != %d { report.widened += 1; }\n", in, kind)
		}
		emitScalarConvert(b, f, g.floatOwner(f), field, "kind", in+"    ")
		fmt.Fprintf(b, "%s} else { %s }\n", in, kindSkip)
	}
	fmt.Fprintf(b, "%s}\n", ind)
}

// emitArrayLoad emits the decode of an array body — a []T, a [..N]T, or
// bytes(N), which rides as an array of u8 (§2.5, §2.9, §3) — in the oracle's
// arrayBody order: a body under two bytes is INERT; a damaged count is framing
// damage inside the body's own length; an element kind that neither matches
// nor widens is a kind mismatch and the field keeps its value; otherwise the
// field is REPLACED, a bounded count past N keeps N and counts clamped, and an
// element the body cannot cover keeps the decoded prefix and counts malformed.
func (g *gen) emitArrayLoad(b *strings.Builder, f *ir.Field, ordinal int, field, in string) {
	isBytes := f.Array == ir.ArrayNone && f.Type.Kind == ir.TBytes
	ek := elemWireKind(f)
	if isBytes {
		ek = ir.TableKindU8
	}
	fmt.Fprintf(b, "%sif kind != %d { %s }\n", in, ir.TableKindArray, kindSkip)
	fmt.Fprintf(b, "%selse {\n", in)
	i1 := in + "    "
	fmt.Fprintf(b, "%slet Some(l) = leb_read(wire, off) else { report.malformed = true; return false; };\n", i1)
	fmt.Fprintf(b, "%sif *off > end || l > (end - *off) as u64 { report.malformed = true; return false; }\n", i1)
	fmt.Fprintf(b, "%slet sub = *off + l as usize;\n", i1)
	fmt.Fprintf(b, "%sif l >= 2 {\n", i1)
	i2 := i1 + "    "
	fmt.Fprintf(b, "%slet ek = wire[*off]; *off += 1;\n", i2)
	fmt.Fprintf(b, "%smatch leb_read(wire, off) {\n", i2)
	fmt.Fprintf(b, "%s    None => { report.malformed = true; }\n", i2)
	// A canonical count may be wider than this array's L and finish in the
	// enclosing body. The oracle still accepts the count, then sees an empty
	// element span, applying kind/clamp rules before marking damage (§3, §4).
	fmt.Fprintf(b, "%s    Some(count) => {\n", i2)
	i3 := i2 + "        "
	cls := classOf(f)
	widens := cls == vScalar
	if widens {
		fmt.Fprintf(b, "%sif ek != %d && !table_kind_widens(ek, %d) { report.kind_mismatch += 1; }\n", i3, ek, ek)
	} else {
		fmt.Fprintf(b, "%sif ek != %d { report.kind_mismatch += 1; }\n", i3, ek)
	}
	fmt.Fprintf(b, "%selse {\n", i3)
	i4 := i3 + "    "
	if widens {
		fmt.Fprintf(b, "%sif ek != %d { report.widened += 1; }\n", i4, ek)
	}
	if cls == vTable || cls == vUnion {
		fmt.Fprintf(b, "%sif let Some(ret) = retain.as_deref_mut() { retain_discard(ret, path, %d); }\n", i4, ordinal)
	}
	if f.Array == ir.ArrayFixed {
		fmt.Fprintf(b, "%slet prior = %s.clone();\n", i4, field)
	}
	fmt.Fprintf(b, "%s%s.clear();\n", i4, field)
	switch f.Array {
	case ir.ArrayList:
		fmt.Fprintf(b, "%slet keep = if count > 0x7fffffff { report.malformed = true; 0 } else { count };\n", i4)
	case ir.ArrayCounted, ir.ArrayFixed:
		fmt.Fprintf(b, "%slet keep = if count > %d { report.clamped += 1; %d } else { count };\n", i4, f.ArrayBound, f.ArrayBound)
	default: // bytes(N)
		fmt.Fprintf(b, "%slet keep = if count > %d { report.clamped += 1; %d } else { count };\n", i4, f.Type.Size, f.Type.Size)
	}
	fmt.Fprintf(b, "%sfor j in 0..keep {\n", i4)
	i5 := i4 + "    "
	fmt.Fprintf(b, "%slet _ = j;\n", i5)
	fmt.Fprintf(b, "%sif *off >= sub { report.malformed = true; break; }\n", i5)
	switch {
	case isBytes:
		fmt.Fprintf(b, "%sif *off >= sub { report.malformed = true; break; }\n", i5)
		fmt.Fprintf(b, "%s%s.push(wire[*off]); *off += 1;\n", i5, field)
	case cls == vTable:
		fmt.Fprintf(b, "%sif *off >= sub { report.malformed = true; break; }\n", i5)
		fmt.Fprintf(b, "%slet Some(el) = leb_read(wire, off) else { report.malformed = true; break; };\n", i5)
		fmt.Fprintf(b, "%sif *off > sub || el > (sub - *off) as u64 { report.malformed = true; break; }\n", i5)
		fmt.Fprintf(b, "%slet eend = *off + el as usize;\n", i5)
		fmt.Fprintf(b, "%slet mut e = %s::default();\n", i5, f.Type.Name)
		fmt.Fprintf(b, "%slet mut o = *off;\n", i5)
		fmt.Fprintf(b, "%slet _ = %s(wire, &mut o, eend, ids, &mut e, nodes, report, path.step(%d, j as u32), retain.as_deref_mut(), true);\n", i5, fn(f.Type.Name, "load_body"), ordinal)
		fmt.Fprintf(b, "%sif o != eend { report.malformed = true; e = %s::default(); }\n", i5, f.Type.Name)
		fmt.Fprintf(b, "%s*off = eend;\n", i5)
		fmt.Fprintf(b, "%s%s.push(e);\n", i5, field)
	case cls == vPointer:
		fmt.Fprintf(b, "%sif *off >= sub { report.malformed = true; break; }\n", i5)
		fmt.Fprintf(b, "%slet Some(index) = leb_read(wire, off) else { report.malformed = true; break; };\n", i5)
		fmt.Fprintf(b, "%sif *off > sub { report.malformed = true; break; }\n", i5)
		emitPointerResolve(b, f, "let e", i5)
		fmt.Fprintf(b, "%s%s.push(e);\n", i5, field)
	case cls == vEnum:
		fmt.Fprintf(b, "%sif *off >= sub { report.malformed = true; break; }\n", i5)
		emitEnumDecode(b, f, "let e", "sub", "break;", i5)
		fmt.Fprintf(b, "%s%s.push(e);\n", i5, field)
	case cls == vUnion:
		fmt.Fprintf(b, "%slet mut e = %s::None;\n", i5, f.Type.Name)
		fmt.Fprintf(b, "%slet item_ok = (|| -> bool {\n", i5)
		// each element is its own body occurrence (§6.6)
		g.withArmIndex("j as u32", func() { g.emitUnionDecode(b, f, "e", "sub", ordinal, i5+"    ", true) })
		fmt.Fprintf(b, "%s    true\n%s})();\n", i5, i5)
		if f.Array == ir.ArrayFixed {
			// The oracle resets a fixed union slot as soon as its arm id is
			// entered. A damaged arm therefore replaces that slot with None,
			// while the untouched tail keeps the preceding occurrence (§3, §4).
			fmt.Fprintf(b, "%sif !item_ok { %s.push(e); break; }\n", i5, field)
		} else {
			fmt.Fprintf(b, "%sif !item_ok { break; }\n", i5)
		}
		fmt.Fprintf(b, "%s%s.push(e);\n", i5, field)
	default:
		fmt.Fprintf(b, "%slet Some(raw) = %s(wire, off, sub, ek) else { report.malformed = true; break; };\n", i5, scalarReadFn(f))
		emitScalarConvert(b, f, g.floatOwner(f), "let e", "ek", i5)
		fmt.Fprintf(b, "%s%s.push(e);\n", i5, field)
	}
	fmt.Fprintf(b, "%s}\n", i4)
	if f.Array == ir.ArrayFixed {
		fmt.Fprintf(b, "%swhile %s.len() < %d { %s.push(prior.get(%s.len()).cloned()%s); }\n", i4, field, f.ArrayBound, field, field, orElse(fieldDefault(scalarOnly(f))))
	}
	fmt.Fprintf(b, "%s}\n", i3)
	fmt.Fprintf(b, "%s    }\n", i2)
	fmt.Fprintf(b, "%s}\n", i2)
	fmt.Fprintf(b, "%s}\n", i1)
	fmt.Fprintf(b, "%s*off = sub; // excess elements and slack skip via the length\n", i1)
	fmt.Fprintf(b, "%s}\n", in)
}

// pointerTargets is every table of the unit a pointer slot can name: the
// tables the numbering's records are bodies of (§3.1).
func (g *gen) pointerTargets(names []string) []*ir.Struct {
	seen := map[string]bool{}
	visited := map[string]bool{}
	var out []*ir.Struct
	var consider func(f *ir.Field)
	considerStruct := func(st *ir.Struct) {
		if st == nil || visited[st.Name] {
			return
		}
		visited[st.Name] = true
		for _, f := range st.Fields {
			consider(f)
		}
	}
	consider = func(f *ir.Field) {
		if f == nil {
			return
		}
		if f.IsMap() && f.MapEntry != nil {
			considerStruct(f.MapEntry)
			return
		}
		if un := unionOf(f); un != nil {
			for _, v := range un.Variants {
				consider(v.F)
			}
		}
		if f.Type.Pointer && !f.Type.Blob() {
			if ref, ok := f.Type.Ref.(*ir.Struct); ok && (g.vc == nil || g.vc.emitted[ref.Name]) {
				if !seen[ref.Name] {
					seen[ref.Name] = true
					out = append(out, ref)
				}
				considerStruct(ref)
			}
		} else if ref, ok := f.Type.Ref.(*ir.Struct); ok && !f.Type.Blob() && (g.vc == nil || g.vc.emitted[ref.Name]) {
			considerStruct(ref)
		}
	}
	for _, n := range names {
		considerStruct(vStruct(g.unit, n))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// emitVariableDispatch emits the file's two node dispatchers, by a record's
// type id (§3.1): the save writes a table record's body, and the load decodes
// one record into its table, once, the first time a slot resolves it.
func (g *gen) emitVariableDispatch(b *strings.Builder, names []string) {
	targets := g.pointerTargets(names)
	fmt.Fprintf(b, "/// The body of one table record of the numbering, by its type id (docs/SPEC-TABLES.md §3.1).\n")
	fmt.Fprintf(b, "// one arm per node type the unit can place: zero or one is a real shape\n")
	fmt.Fprintf(b, "#[allow(dead_code, unused_variables, unused_mut, clippy::too_many_arguments, clippy::match_single_binding, clippy::single_match)]\n")
	fmt.Fprintf(b, "fn variable_record_save(type_id: u64, node: &Rc<dyn Any>, out: &mut Vec<u8>, ids: &mut IdTable, g: &TableNodeGraph, mut retain: Option<&mut TableRetain>, mut report: Option<&mut TableReport>, path: TableRetainPath) {\n")
	fmt.Fprintf(b, "    match type_id {\n")
	for _, st := range targets {
		fmt.Fprintf(b, "        0x%016x => {\n", ir.TableWireId(st.WireName()))
		fmt.Fprintf(b, "            if let Some(v) = node.downcast_ref::<%s>() {\n", st.Name)
		fmt.Fprintf(b, "                %s(v, out, ids, g, retain.as_deref_mut(), report.as_deref_mut(), path);\n", fn(st.Name, "save_body"))
		fmt.Fprintf(b, "                if let (Some(ret), Some(rep)) = (retain.as_mut(), report.as_mut()) { retain_emit_tail(out, ids, ret, KNOWN_IDS, path, rep); }\n")
		fmt.Fprintf(b, "            }\n")
		fmt.Fprintf(b, "        }\n")
	}
	fmt.Fprintf(b, "        _ => {}\n")
	fmt.Fprintf(b, "    }\n")
	fmt.Fprintf(b, "    out.push(0); // the record body's zero reference\n")
	fmt.Fprintf(b, "}\n\n")

	fmt.Fprintf(b, "/// Decode record k of the numbering into its table, ONCE (docs/SPEC-TABLES.md\n")
	fmt.Fprintf(b, "/// §3.1): every later slot naming it shares the one node. A record that names\n")
	fmt.Fprintf(b, "/// itself while its own body decodes is a cycle no writer produces and this\n")
	fmt.Fprintf(b, "/// storage cannot hold: that slot reads null and one malformed counts. On a\n")
	fmt.Fprintf(b, "/// retaining load an unknown field inside a node body counts retain_lost: the\n")
	fmt.Fprintf(b, "/// store carries no path into a node (§6.6).\n")
	fmt.Fprintf(b, "#[allow(dead_code, unused_variables, unused_mut, clippy::match_single_binding)]\n")
	fmt.Fprintf(b, "fn variable_node_build(wire: &[u8], ids: &[u64], nodes: &mut TableNodeReader, k: usize, report: &mut TableReport) -> Option<Rc<dyn Any>> {\n")
	fmt.Fprintf(b, "    match &nodes.slots[k] {\n")
	fmt.Fprintf(b, "        TableNodeSlot::Built(_, n) => return Some(n.clone()),\n")
	fmt.Fprintf(b, "        TableNodeSlot::Table(_) => {}\n")
	fmt.Fprintf(b, "        TableNodeSlot::Building => { report.cycle_on_save = true; return None; }\n")
	fmt.Fprintf(b, "        _ => return None,\n")
	fmt.Fprintf(b, "    }\n")
	fmt.Fprintf(b, "    let (type_id, start, end) = nodes.recs[k];\n")
	fmt.Fprintf(b, "    nodes.slots[k] = TableNodeSlot::Building;\n")
	fmt.Fprintf(b, "    let mut sink = TableRetain { bytes: Vec::with_capacity(nodes.retain_capacity), ids: Vec::new() };\n")
	fmt.Fprintf(b, "    let retaining = nodes.retaining;\n")
	fmt.Fprintf(b, "    let node: Option<Rc<dyn Any>> = match type_id {\n")
	for _, st := range targets {
		fmt.Fprintf(b, "        0x%016x => {\n", ir.TableWireId(st.WireName()))
		fmt.Fprintf(b, "            let mut v = %s::default();\n", st.Name)
		fmt.Fprintf(b, "            let mut off = start;\n")
		fmt.Fprintf(b, "            let _ = %s(wire, &mut off, end, ids, &mut v, nodes, report, TableRetainPath::default().step(u32::MAX, k as u32), if retaining { Some(&mut sink) } else { None }, true);\n", fn(st.Name, "load_body"))
		fmt.Fprintf(b, "            Some(Rc::new(v))\n")
		fmt.Fprintf(b, "        }\n")
	}
	fmt.Fprintf(b, "        _ => None,\n")
	fmt.Fprintf(b, "    };\n")
	fmt.Fprintf(b, "    nodes.slots[k] = match &node { Some(n) => TableNodeSlot::Built(type_id, n.clone()), None => TableNodeSlot::Null };\n")
	fmt.Fprintf(b, "    if let Some(n) = &node { retain_bind_node(&mut sink, n); }\n")
	fmt.Fprintf(b, "    nodes.retained.extend_from_slice(&sink.bytes);\n")
	fmt.Fprintf(b, "    node\n")
	fmt.Fprintf(b, "}\n\n")
}

// emitNodeTableWrite emits the root's node-table field (§3.1): LAST in the
// root body, after its own fields (and the retained tail), under the reserved
// id, kind 12, the count and the records in index order — each record's type
// id interned before its body, as the oracle writes it.
func emitNodeTableWrite(b *strings.Builder, retaining bool) {
	fmt.Fprintf(b, "    if !g.records.is_empty() {\n")
	fmt.Fprintf(b, "        leb_write(&mut out, ids.id_ref(NODE_TABLE_ID));\n")
	fmt.Fprintf(b, "        out.push(12); // kind 12: the opaque byte payload (§3.1)\n")
	fmt.Fprintf(b, "        let mut payload = Vec::new();\n")
	fmt.Fprintf(b, "        leb_write(&mut payload, g.records.len() as u64);\n")
	fmt.Fprintf(b, "        for rec in &g.records {\n")
	fmt.Fprintf(b, "            match rec {\n")
	fmt.Fprintf(b, "                TableNodeRecord::Blob(type_id, blob) => {\n")
	fmt.Fprintf(b, "                    leb_write(&mut payload, ids.id_ref(*type_id));\n")
	fmt.Fprintf(b, "                    leb_write(&mut payload, blob.len() as u64);\n")
	fmt.Fprintf(b, "                    payload.extend_from_slice(blob);\n")
	fmt.Fprintf(b, "                }\n")
	fmt.Fprintf(b, "                TableNodeRecord::Table(type_id, node) => {\n")
	fmt.Fprintf(b, "                    leb_write(&mut payload, ids.id_ref(*type_id));\n")
	fmt.Fprintf(b, "                    let mut body = Vec::new();\n")
	if retaining {
		fmt.Fprintf(b, "                    variable_record_save(*type_id, node, &mut body, &mut ids, &g, Some(&mut *retain), Some(&mut *report), retain_node_path(node));\n")
	} else {
		fmt.Fprintf(b, "                    variable_record_save(*type_id, node, &mut body, &mut ids, &g, None, None, TableRetainPath::default());\n")
	}
	fmt.Fprintf(b, "                    leb_write(&mut payload, body.len() as u64);\n")
	fmt.Fprintf(b, "                    payload.extend_from_slice(&body);\n")
	fmt.Fprintf(b, "                }\n")
	fmt.Fprintf(b, "            }\n")
	fmt.Fprintf(b, "        }\n")
	fmt.Fprintf(b, "        leb_write(&mut out, payload.len() as u64);\n")
	fmt.Fprintf(b, "        out.extend_from_slice(&payload);\n")
	fmt.Fprintf(b, "    }\n")
}

// emitVariableRoot emits <name>_save and <name>_load: the whole file — form
// byte, the root body, the node table (§3.1) and the id-table trailer — and
// the tolerant read of the same.
func (g *gen) emitVariableRoot(b *strings.Builder, st *ir.Struct) {
	fnName := ir.RustSnake(st.Name)
	// SAVE
	fmt.Fprintf(b, "/// %s_save — a whole form-1 FILE (docs/SPEC-TABLES.md §3, §3.1).\n", fnName)
	fmt.Fprintf(b, "/// An over-bound value panics with its field name; use %s_save_checked\n", fnName)
	fmt.Fprintf(b, "/// for a named Err instead of a panic (docs/SPEC-TABLES.md §3).\n")
	fmt.Fprintf(b, "pub fn %s_save(v: &%s) -> Vec<u8> {\n", fnName, st.Name)
	fmt.Fprintf(b, "    let mut out = Vec::new();\n")
	fmt.Fprintf(b, "    out.push(TABLE_WIRE_FORM);\n")
	fmt.Fprintf(b, "    let mut ids = IdTable::new();\n")
	fmt.Fprintf(b, "    let mut g = TableNodeGraph::new();\n")
	fmt.Fprintf(b, "    %s(v, &mut g);\n", fn(st.Name, "number"))
	fmt.Fprintf(b, "    if g.blob_shared { ids.refused = Some(String::from(TableReason::BlobShared.name())); } // §2.5: a blob two slots name\n")
	fmt.Fprintf(b, "    %s(v, &mut out, &mut ids, &g, None, None, TableRetainPath::default());\n", fn(st.Name, "save_body"))
	emitNodeTableWrite(b, false)
	fmt.Fprintf(b, "    // A Vec cannot carry the writer's named refusal (§3): never return a truncated file.\n")
	fmt.Fprintf(b, "    if let Some(why) = ids.refused.take() { panic!(\"{}\", why); }\n")
	fmt.Fprintf(b, "    out.push(0); // the root body's zero reference\n")
	fmt.Fprintf(b, "    for e in &ids.entries { out.extend_from_slice(&e.to_le_bytes()); }\n")
	fmt.Fprintf(b, "    out.extend_from_slice(&(ids.entries.len() as u64).to_le_bytes());\n")
	fmt.Fprintf(b, "    out\n")
	fmt.Fprintf(b, "}\n\n")

	// SAVE-CHECKED: the same file, or the writer's refusal BY NAME when a
	// value is over its declared bound (docs/SPEC-TABLES.md §3), as the Go
	// leg's save refuses
	// (internal/codegen/gotable/write.go).
	fmt.Fprintf(b, "/// %s_save_checked — %s_save, or the refusal by name of a value over its bound\n", fnName, fnName)
	fmt.Fprintf(b, "/// (docs/SPEC-TABLES.md §3): nothing is returned for a refused value.\n")
	fmt.Fprintf(b, "pub fn %s_save_checked(v: &%s) -> Result<Vec<u8>, String> {\n", fnName, st.Name)
	fmt.Fprintf(b, "    let mut out = Vec::new();\n")
	fmt.Fprintf(b, "    out.push(TABLE_WIRE_FORM);\n")
	fmt.Fprintf(b, "    let mut ids = IdTable::new();\n")
	fmt.Fprintf(b, "    let mut g = TableNodeGraph::new();\n")
	fmt.Fprintf(b, "    %s(v, &mut g);\n", fn(st.Name, "number"))
	fmt.Fprintf(b, "    if g.blob_shared { ids.refused = Some(String::from(TableReason::BlobShared.name())); } // §2.5: a blob two slots name\n")
	fmt.Fprintf(b, "    %s(v, &mut out, &mut ids, &g, None, None, TableRetainPath::default());\n", fn(st.Name, "save_body"))
	fmt.Fprintf(b, "    // every node record's body rides too, and is checked by the same walk\n")
	emitNodeTableWrite(b, false)
	fmt.Fprintf(b, "    if let Some(why) = ids.refused.take() { return Err(why); }\n")
	fmt.Fprintf(b, "    out.push(0); // the root body's zero reference\n")
	fmt.Fprintf(b, "    for e in &ids.entries { out.extend_from_slice(&e.to_le_bytes()); }\n")
	fmt.Fprintf(b, "    out.extend_from_slice(&(ids.entries.len() as u64).to_le_bytes());\n")
	fmt.Fprintf(b, "    Ok(out)\n")
	fmt.Fprintf(b, "}\n\n")

	// SAVE-RETAIN (docs/SPEC-TABLES.md §6.6): the same file, with the retained
	// tail pinned BEFORE the node-table field and the retained ids numbered into
	// the one trailer in merged first-use order.
	fmt.Fprintf(b, "/// %s_save_retain — the retaining save (docs/SPEC-TABLES.md §6.6): the\n", fnName)
	fmt.Fprintf(b, "/// retained tail rides before the node-table field, and every retained id\n")
	fmt.Fprintf(b, "/// enters the one id table in first-use order.\n")
	fmt.Fprintf(b, "/// An over-bound value panics with its field name; it cannot return a\n")
	fmt.Fprintf(b, "/// truncated Vec (docs/SPEC-TABLES.md §3).\n")
	fmt.Fprintf(b, "pub fn %s_save_retain(v: &%s, retain: &mut TableRetain, report: &mut TableReport) -> Vec<u8> {\n", fnName, st.Name)
	fmt.Fprintf(b, "    retain.ids.clear();\n")
	fmt.Fprintf(b, "    retain_clear_placed(retain);\n")
	fmt.Fprintf(b, "    let mut out = Vec::new();\n")
	fmt.Fprintf(b, "    out.push(TABLE_WIRE_FORM);\n")
	fmt.Fprintf(b, "    let mut ids = IdTable::new();\n")
	fmt.Fprintf(b, "    let mut g = TableNodeGraph::new();\n")
	fmt.Fprintf(b, "    %s(v, &mut g);\n", fn(st.Name, "number"))
	fmt.Fprintf(b, "    if g.blob_shared { ids.refused = Some(String::from(TableReason::BlobShared.name())); } // §2.5: a blob two slots name\n")
	fmt.Fprintf(b, "    %s(v, &mut out, &mut ids, &g, Some(&mut *retain), Some(&mut *report), TableRetainPath::default());\n", fn(st.Name, "save_body"))
	fmt.Fprintf(b, "    retain_emit_tail(&mut out, &mut ids, retain, KNOWN_IDS, TableRetainPath::default(), report);\n")
	emitNodeTableWrite(b, true)
	fmt.Fprintf(b, "    retain_count_unplaced(retain, report);\n")
	fmt.Fprintf(b, "    // A Vec cannot carry the writer's named refusal (§3): never return a truncated file.\n")
	fmt.Fprintf(b, "    if let Some(why) = ids.refused.take() { panic!(\"{}\", why); }\n")
	fmt.Fprintf(b, "    out.push(0); // the root body's zero reference\n")
	fmt.Fprintf(b, "    for e in &ids.entries { out.extend_from_slice(&e.to_le_bytes()); }\n")
	fmt.Fprintf(b, "    out.extend_from_slice(&(ids.entries.len() as u64).to_le_bytes());\n")
	fmt.Fprintf(b, "    out\n")
	fmt.Fprintf(b, "}\n\n")

	// LOAD
	fmt.Fprintf(b, "/// %s_load — the tolerant read of a whole file (docs/SPEC-TABLES.md §4, §5).\n", fnName)
	fmt.Fprintf(b, "pub fn %s_load(wire: &[u8], report: &mut TableReport) -> Option<%s> {\n", fnName, st.Name)
	fmt.Fprintf(b, "    %s_load_inner(wire, None, report)\n", fnName)
	fmt.Fprintf(b, "}\n\n")
	fmt.Fprintf(b, "/// %s_load_retain — the retaining read of the same file (docs/SPEC-TABLES.md\n", fnName)
	fmt.Fprintf(b, "/// §6.6): the next load into the store resets both of its stores, and every\n")
	fmt.Fprintf(b, "/// unknown field it can keep is captured whole.\n")
	fmt.Fprintf(b, "pub fn %s_load_retain(wire: &[u8], retain: &mut TableRetain, report: &mut TableReport) -> Option<%s> {\n", fnName, st.Name)
	fmt.Fprintf(b, "    retain.bytes.clear();\n")
	fmt.Fprintf(b, "    retain.ids.clear();\n")
	fmt.Fprintf(b, "    %s_load_inner(wire, Some(retain), report)\n", fnName)
	fmt.Fprintf(b, "}\n\n")

	// the root's PLACEABLE set (§3.1): a record whose type id no pointer below
	// this root can name is a node this reader cannot place
	var placeable []string
	for _, t := range ir.PointerReachable(st) {
		placeable = append(placeable, fmt.Sprintf("0x%016x", ir.TableWireId(t.WireName())))
	}
	bytesEdge, stringEdge := ir.PointerReachableBlobs(st)

	fmt.Fprintf(b, "/// %s_load_inner — decodes one variable-form wire, enforcing the 4096-entry\n", fnName)
	fmt.Fprintf(b, "/// id-table reader bound and 8192-slot open-addressing distinctness probe with\n")
	fmt.Fprintf(b, "/// no heap allocation for the id table (docs/SPEC-TABLES.md §3).\n")
	fmt.Fprintf(b, "pub fn %s_load_inner(wire: &[u8], retain: Option<&mut TableRetain>, report: &mut TableReport) -> Option<%s> {\n", fnName, st.Name)
	fmt.Fprintf(b, "    // this read's own non-finite float (docs/SPEC-TABLES.md §3.3)\n")
	fmt.Fprintf(b, "    report.non_finite_float = \"\";\n")
	fmt.Fprintf(b, "    report.non_finite_float_id = None;\n")
	fmt.Fprintf(b, "    if wire.is_empty() { report.malformed = true; return None; }\n")
	fmt.Fprintf(b, "    if wire[0] != TABLE_WIRE_FORM { report.refused = true; report.reason = TableRefuseReason::UnknownForm; return None; }\n")
	fmt.Fprintf(b, "    if wire.len() < 9 { report.malformed = true; return None; }\n")
	fmt.Fprintf(b, "    let count = u64::from_le_bytes(wire[wire.len() - 8..].try_into().ok()?);\n")
	fmt.Fprintf(b, "    if count > (wire.len() / 8) as u64 { report.malformed = true; return None; }\n")
	fmt.Fprintf(b, "    if count > ID_TABLE_PROBE_BOUND as u64 { report.malformed = true; return None; } // §3: at most 4096 entries in the id table\n")
	fmt.Fprintf(b, "    let count = count as usize;\n")
	fmt.Fprintf(b, "    let span = 8 + 8 * count;\n")
	fmt.Fprintf(b, "    if span + 1 > wire.len() { report.malformed = true; return None; }\n")
	fmt.Fprintf(b, "    let estart = wire.len() - span;\n")
	fmt.Fprintf(b, "    let end = estart;\n")
	fmt.Fprintf(b, "    let mut ids_buf = [0u64; ID_TABLE_PROBE_SLOTS];\n")
	fmt.Fprintf(b, "    let mut probe_slots = [0u64; ID_TABLE_PROBE_SLOTS];\n")
	fmt.Fprintf(b, "    let mut probe_used = [0u64; ID_TABLE_PROBE_SLOTS / 64];\n")
	fmt.Fprintf(b, "    for (i, slot) in ids_buf.iter_mut().enumerate().take(count) {\n")
	fmt.Fprintf(b, "        let s = estart + 8 * i;\n")
	fmt.Fprintf(b, "        let id = u64::from_le_bytes(wire[s..s + 8].try_into().ok()?);\n")
	fmt.Fprintf(b, "        let mut h = ((id.wrapping_mul(0x9e3779b97f4a7c15) >> 51) as usize) & (ID_TABLE_PROBE_SLOTS - 1);\n")
	fmt.Fprintf(b, "        loop {\n")
	fmt.Fprintf(b, "            let bit = 1u64 << (h & 63);\n")
	fmt.Fprintf(b, "            if (probe_used[h >> 6] & bit) == 0 {\n")
	fmt.Fprintf(b, "                probe_used[h >> 6] |= bit;\n")
	fmt.Fprintf(b, "                probe_slots[h] = id;\n")
	fmt.Fprintf(b, "                break;\n")
	fmt.Fprintf(b, "            }\n")
	fmt.Fprintf(b, "            if probe_slots[h] == id { report.malformed = true; return None; }\n")
	fmt.Fprintf(b, "            h = (h + 1) & (ID_TABLE_PROBE_SLOTS - 1);\n")
	fmt.Fprintf(b, "        }\n")
	fmt.Fprintf(b, "        *slot = id;\n")
	fmt.Fprintf(b, "    }\n")
	fmt.Fprintf(b, "    let ids: &[u64] = &ids_buf[..count];\n")
	if !st.FixedDeclared {
		fmt.Fprintf(b, "    if %s_load_measure(wire) < 0 {\n", fnName)
		fmt.Fprintf(b, "        // A FRAMING THE CONTAINER CANNOT OPEN loads nothing (§3, §6.5). A\n")
		fmt.Fprintf(b, "        // NON-FINITE FLOAT the walk meets before the damage still outranks it\n")
		fmt.Fprintf(b, "        // (docs/SPEC-TABLES.md §3.3), as in the oracle, whose walk runs ahead\n")
		fmt.Fprintf(b, "        // of any region measure: the walk runs over a scratch report and store\n")
		fmt.Fprintf(b, "        // to ask that one question, and keeps nothing else.\n")
		fmt.Fprintf(b, "        let mut scratch = *report;\n")
		fmt.Fprintf(b, "        let mut store = TableRetain { bytes: Vec::new(), ids: Vec::new() };\n")
		fmt.Fprintf(b, "        let _ = %s_load_walk(wire, end, ids, if retain.is_some() { Some(&mut store) } else { None }, &mut scratch, true);\n", fnName)
		fmt.Fprintf(b, "        if !scratch.non_finite_float.is_empty() {\n")
		fmt.Fprintf(b, "            report.refuse(TableReason::NonFiniteFloat);\n")
		fmt.Fprintf(b, "            report.non_finite_float = scratch.non_finite_float;\n")
		fmt.Fprintf(b, "            report.non_finite_float_id = scratch.non_finite_float_id;\n")
		fmt.Fprintf(b, "        }\n")
		fmt.Fprintf(b, "        return None;\n")
		fmt.Fprintf(b, "    }\n")
	}
	fmt.Fprintf(b, "    %s_load_walk(wire, end, ids, retain, report, false)\n", fnName)
	fmt.Fprintf(b, "}\n\n")
	fmt.Fprintf(b, "/// %s_load_walk — the read past the id table: the body-extent pre-scan,\n", fnName)
	fmt.Fprintf(b, "/// the node table, the root body and every placeable record, and the\n")
	fmt.Fprintf(b, "/// non-finite refusal (docs/SPEC-TABLES.md §3, §3.1, §3.3). `probe` is the\n")
	fmt.Fprintf(b, "/// scratch walk of a framing the container cannot open, which reads the root\n")
	fmt.Fprintf(b, "/// body with null nodes where the node table cannot be scanned, as the\n")
	fmt.Fprintf(b, "/// oracle's walk does, to find a non-finite float ahead of the damage.\n")
	fmt.Fprintf(b, "#[allow(unused_variables)]\n")
	fmt.Fprintf(b, "fn %s_load_walk(wire: &[u8], end: usize, ids: &[u64], mut retain: Option<&mut TableRetain>, report: &mut TableReport, probe: bool) -> Option<%s> {\n", fnName, st.Name)
	// body-extent pre-scan (docs/SPEC-TABLES.md §3): a body whose zero reference
	// lands before the id table is malformed, and nothing is decoded.
	fmt.Fprintf(b, "    {\n")
	fmt.Fprintf(b, "        let mut p = 1usize;\n")
	fmt.Fprintf(b, "        let mut term: Option<usize> = None;\n")
	fmt.Fprintf(b, "        loop {\n")
	fmt.Fprintf(b, "            if p >= end { break; }\n")
	fmt.Fprintf(b, "            let Some(r) = leb_read(wire, &mut p) else { break; };\n")
	fmt.Fprintf(b, "            if r == 0 { term = Some(p); break; }\n")
	fmt.Fprintf(b, "            if r as usize > ids.len() { break; }\n")
	fmt.Fprintf(b, "            if p >= end { break; }\n")
	fmt.Fprintf(b, "            let kind = wire[p]; p += 1;\n")
	fmt.Fprintf(b, "            if !leb_skip(wire, &mut p, end, kind) { break; }\n")
	fmt.Fprintf(b, "        }\n")
	fmt.Fprintf(b, "        if let Some(t) = term && t != end { report.malformed = true; return Some(%s::default()); }\n", st.Name)
	fmt.Fprintf(b, "    }\n")
	// The node table (docs/SPEC-TABLES.md §3.1) is written LAST, so a scan that
	// resolves pointers as it meets them cannot know the numbering yet: it is
	// gathered whole first, then the body read decodes and resolves.
	if st.FixedDeclared {
		fmt.Fprintf(b, "    // A fixed-class root does not own the node table: §4 reads it as an unknown field.\n")
		fmt.Fprintf(b, "    let mut nodes = TableNodeReader { good: true, root_type: 0x%016x, retaining: retain.is_some(), retain_capacity: retain.as_ref().map_or(0, |r| r.bytes.capacity()), retained: Vec::new(), recs: Vec::new(), slots: Vec::new(), blob_named: Vec::new() };\n", ir.TableWireId(st.WireName()))
	} else {
		fmt.Fprintf(b, "    let placeable: &[u64] = &[%s];\n", strings.Join(placeable, ", "))
		fmt.Fprintf(b, "    let mut nodes = match TableNodeReader::scan(wire, end, ids, 0x%016x, placeable, %v, %v, retain.is_some(), retain.as_ref().map_or(0, |r| r.bytes.capacity()), report) {\n", ir.TableWireId(st.WireName()), bytesEdge, stringEdge)
		fmt.Fprintf(b, "        Some(nodes) => nodes,\n")
		fmt.Fprintf(b, "        None if probe => TableNodeReader { good: false, root_type: 0x%016x, retaining: retain.is_some(), retain_capacity: 0, retained: Vec::new(), recs: Vec::new(), slots: Vec::new(), blob_named: Vec::new() },\n", ir.TableWireId(st.WireName()))
		fmt.Fprintf(b, "        None => return None,\n")
		fmt.Fprintf(b, "    };\n")
	}
	fmt.Fprintf(b, "    let mut v = %s::default();\n", st.Name)
	fmt.Fprintf(b, "    let mut off = 1usize;\n")
	fmt.Fprintf(b, "    let _ = %s(wire, &mut off, end, ids, &mut v, &mut nodes, report, TableRetainPath::default(), retain.as_deref_mut(), false);\n", fn(st.Name, "load_body"))
	fmt.Fprintf(b, "    let reachable_cycle = report.cycle_on_save;\n")
	fmt.Fprintf(b, "    // every placeable record is decoded whether or not a slot names it, so\n")
	fmt.Fprintf(b, "    // what its body reports is counted as the oracle counts it (§3.1, §4)\n")
	fmt.Fprintf(b, "    for k in 0..nodes.slots.len() {\n")
	fmt.Fprintf(b, "        if matches!(nodes.slots[k], TableNodeSlot::Table(_)) { let _ = variable_node_build(wire, ids, &mut nodes, k, report); }\n")
	fmt.Fprintf(b, "    }\n")
	fmt.Fprintf(b, "    report.cycle_on_save = reachable_cycle;\n")
	fmt.Fprintf(b, "    // A NON-FINITE FLOAT REFUSES THE READ, the tolerant one too, and outranks\n")
	fmt.Fprintf(b, "    // any damage the walk met after it (docs/SPEC-TABLES.md §3.3): nothing\n")
	fmt.Fprintf(b, "    // lands and nothing retained is kept.\n")
	fmt.Fprintf(b, "    if !report.non_finite_float.is_empty() { report.refuse(TableReason::NonFiniteFloat); return None; }\n")
	fmt.Fprintf(b, "    if let Some(ret) = retain {\n")
	fmt.Fprintf(b, "        let mut at = 0usize;\n")
	fmt.Fprintf(b, "        while at + 4 <= nodes.retained.len() {\n")
	fmt.Fprintf(b, "            let len = retain_record_len(&nodes.retained[at..]);\n")
	fmt.Fprintf(b, "            if len < 17 || len > nodes.retained.len() - at { break; }\n")
	fmt.Fprintf(b, "            if len <= ret.bytes.capacity().saturating_sub(ret.bytes.len()) { ret.bytes.extend_from_slice(&nodes.retained[at..at+len]); }\n")
	fmt.Fprintf(b, "            else { report.retained -= 1; report.retain_lost += 1; }\n")
	fmt.Fprintf(b, "            at += len;\n")
	fmt.Fprintf(b, "        }\n")
	fmt.Fprintf(b, "    }\n")
	fmt.Fprintf(b, "    Some(v)\n")
	fmt.Fprintf(b, "}\n\n")

	// MEASURE
	g.emitVariableMeasure(b, st)
}

// emitVariableMeasure emits <name>_load_measure: the region storage the framing
// commands (docs/SPEC-TABLES.md §3.1, §6.5), the slice of the oracle's
// FileRegionMeasure (internal/tablewire/extent.go) this leg carries — the root
// record, every []T list's elements, the blob nodes and the attribution. Its
// framing refusals gate the load; the region a cook lays out is the cook
// card's.
func (g *gen) emitVariableMeasure(b *strings.Builder, st *ir.Struct) {
	fnName := ir.RustSnake(st.Name)
	rootSize := ir.RecordLayout(g.unit, st).Size
	align := ir.TableRegionAlign(g.unit)
	var lists []*ir.Field
	for _, f := range st.Fields {
		if f.IsList() || f.IsMap() {
			lists = append(lists, f)
		}
	}
	fmt.Fprintf(b, "/// %s_load_measure — the region bytes the framing commands\n", fnName)
	fmt.Fprintf(b, "/// (docs/SPEC-TABLES.md §3, §3.1, §6.5): -1 when the file cannot be opened.\n")
	fmt.Fprintf(b, "#[allow(clippy::single_match)] // one arm per placeable type or list field: one is a real shape\n")
	fmt.Fprintf(b, "pub fn %s_load_measure(wire: &[u8]) -> i64 {\n", fnName)
	fmt.Fprintf(b, "    if wire.is_empty() || wire[0] != TABLE_WIRE_FORM { return -1; }\n")
	fmt.Fprintf(b, "    if wire.len() < 9 { return -1; }\n")
	fmt.Fprintf(b, "    let count = u64::from_le_bytes(wire[wire.len() - 8..].try_into().unwrap());\n")
	fmt.Fprintf(b, "    if count > (wire.len() / 8) as u64 { return -1; }\n")
	fmt.Fprintf(b, "    if count > ID_TABLE_PROBE_BOUND as u64 { return -1; } // §3: at most 4096 entries in the id table\n")
	fmt.Fprintf(b, "    let count = count as usize;\n")
	fmt.Fprintf(b, "    let span = 8 + 8 * count;\n")
	fmt.Fprintf(b, "    if span + 1 > wire.len() { return -1; }\n")
	fmt.Fprintf(b, "    let estart = wire.len() - span;\n")
	fmt.Fprintf(b, "    let end = estart;\n")
	fmt.Fprintf(b, "    let mut ids_buf = [0u64; ID_TABLE_PROBE_SLOTS];\n")
	fmt.Fprintf(b, "    let mut probe_slots = [0u64; ID_TABLE_PROBE_SLOTS];\n")
	fmt.Fprintf(b, "    let mut probe_used = [0u64; ID_TABLE_PROBE_SLOTS / 64];\n")
	fmt.Fprintf(b, "    for (i, slot) in ids_buf.iter_mut().enumerate().take(count) {\n")
	fmt.Fprintf(b, "        let s = estart + 8 * i;\n")
	fmt.Fprintf(b, "        let id = u64::from_le_bytes(wire[s..s + 8].try_into().unwrap());\n")
	fmt.Fprintf(b, "        let mut h = ((id.wrapping_mul(0x9e3779b97f4a7c15) >> 51) as usize) & (ID_TABLE_PROBE_SLOTS - 1);\n")
	fmt.Fprintf(b, "        loop {\n")
	fmt.Fprintf(b, "            let bit = 1u64 << (h & 63);\n")
	fmt.Fprintf(b, "            if (probe_used[h >> 6] & bit) == 0 {\n")
	fmt.Fprintf(b, "                probe_used[h >> 6] |= bit;\n")
	fmt.Fprintf(b, "                probe_slots[h] = id;\n")
	fmt.Fprintf(b, "                break;\n")
	fmt.Fprintf(b, "            }\n")
	fmt.Fprintf(b, "            if probe_slots[h] == id { return -1; }\n")
	fmt.Fprintf(b, "            h = (h + 1) & (ID_TABLE_PROBE_SLOTS - 1);\n")
	fmt.Fprintf(b, "        }\n")
	fmt.Fprintf(b, "        *slot = id;\n")
	fmt.Fprintf(b, "    }\n")
	fmt.Fprintf(b, "    let ids: &[u64] = &ids_buf[..count];\n")
	fmt.Fprintf(b, "    let round = |n: i64| (n + %d - 1) & -%d;\n", align, align)
	g.emitNestedMeasureHelper(b, st)
	fmt.Fprintf(b, "    let mut blob_total: i64 = 0;\n")
	fmt.Fprintf(b, "    let mut records: i64 = 0;\n")
	fmt.Fprintf(b, "    let mut off = 1usize;\n")
	fmt.Fprintf(b, "    while off < end {\n")
	fmt.Fprintf(b, "        let Some(r) = leb_read(wire, &mut off) else { return -1; };\n")
	fmt.Fprintf(b, "        if r == 0 { break; }\n")
	fmt.Fprintf(b, "        if off >= end { return -1; }\n")
	fmt.Fprintf(b, "        let kind = wire[off]; off += 1;\n")
	fmt.Fprintf(b, "        let Some(id) = ref_id(ids, r) else { return -1; };\n")
	fmt.Fprintf(b, "        if id == NODE_TABLE_ID {\n")
	fmt.Fprintf(b, "            if kind != 12 { return -1; }\n")
	fmt.Fprintf(b, "            let Some(l) = leb_read(wire, &mut off) else { return -1; };\n")
	fmt.Fprintf(b, "            if l > (end - off) as u64 { return -1; }\n")
	fmt.Fprintf(b, "            let sub = off + l as usize;\n")
	fmt.Fprintf(b, "            // The last reserved field supplies the node table (§3.1); an\n")
	fmt.Fprintf(b, "            // earlier occurrence is superseded like the oracle's scan.\n")
	fmt.Fprintf(b, "            let mut later = sub; let mut superseded = false;\n")
	fmt.Fprintf(b, "            while later < end { let Some(r2) = leb_read(&wire[..end], &mut later) else { break; }; if r2 == 0 || later >= end { break; } let Some(id2) = ref_id(ids, r2) else { break; }; let k2 = wire[later]; later += 1;\n")
	fmt.Fprintf(b, "                if id2 == NODE_TABLE_ID { superseded = true; break; }\n")
	fmt.Fprintf(b, "                if !leb_skip(wire, &mut later, end, k2) { break; }\n")
	fmt.Fprintf(b, "            }\n")
	fmt.Fprintf(b, "            if superseded { off = sub; continue; }\n")
	fmt.Fprintf(b, "            let Some(declared) = leb_read(wire, &mut off) else { return -1; };\n")
	fmt.Fprintf(b, "            let mut recs = 0i64;\n")
	fmt.Fprintf(b, "            let mut cur_blob = 0i64;\n")
	fmt.Fprintf(b, "            while off < sub {\n")
	fmt.Fprintf(b, "                let Some(tref) = leb_read(wire, &mut off) else { return -1; };\n")
	fmt.Fprintf(b, "                if tref == 0 || tref as usize > ids.len() { return -1; }\n")
	fmt.Fprintf(b, "                let Some(bl) = leb_read(wire, &mut off) else { return -1; };\n")
	fmt.Fprintf(b, "                if off > sub || bl as usize > sub - off { return -1; }\n")
	fmt.Fprintf(b, "                if ids[tref as usize - 1] == STRING_TYPE_ID || ids[tref as usize - 1] == BYTES_TYPE_ID {\n")
	fmt.Fprintf(b, "                    if bl > 0xffffffff { return -1; }\n")
	fmt.Fprintf(b, "                    // Only text needs a terminator in region storage (§6.5).\n")
	fmt.Fprintf(b, "                    let nul = if ids[tref as usize - 1] == STRING_TYPE_ID { 1 } else { 0 };\n")
	fmt.Fprintf(b, "                    cur_blob += round(8 + bl as i64 + nul);\n")
	fmt.Fprintf(b, "                }\n")
	// THE STORAGE A NODE RECORD COMMANDS is its type's, and only for a type
	// THIS ROOT can place (docs/SPEC-TABLES.md §3.1, §6.5): a table no pointer
	// below the root targets is a node the reader cannot name and commands none,
	// exactly as tablewire.FileRegionMeasure sizes it.
	if reached := ir.PointerReachable(st); len(reached) > 0 {
		fmt.Fprintf(b, "                match ids[tref as usize - 1] {\n")
		seen := map[uint64]bool{}
		for _, n := range reached {
			id := ir.TableWireId(n.WireName())
			if seen[id] {
				continue
			}
			seen[id] = true
			fmt.Fprintf(b, "                    0x%016x => { let extra = nested_measure(0x%016x, &wire[off..off + bl as usize], ids, 0); if extra < 0 { return -1; } cur_blob += round(round(%d) + extra); }\n", id, id, ir.RecordLayout(g.unit, n).Size)
		}
		fmt.Fprintf(b, "                    _ => {}\n")
		fmt.Fprintf(b, "                }\n")
	}
	fmt.Fprintf(b, "                off += bl as usize;\n")
	fmt.Fprintf(b, "                recs += 1;\n")
	fmt.Fprintf(b, "            }\n")
	fmt.Fprintf(b, "            if off != sub || declared != recs as u64 { return -1; }\n")
	fmt.Fprintf(b, "            records = recs;\n")
	fmt.Fprintf(b, "            blob_total = cur_blob;\n")
	fmt.Fprintf(b, "            off = sub;\n")
	fmt.Fprintf(b, "            continue;\n")
	fmt.Fprintf(b, "        }\n")
	if len(lists) > 0 {
		fmt.Fprintf(b, "        if ")
		for i, f := range lists {
			if i > 0 {
				fmt.Fprintf(b, " || ")
			}
			fmt.Fprintf(b, "id == 0x%016x", ir.TableFieldWireId(f))
		}
		fmt.Fprintf(b, " {\n")
		fmt.Fprintf(b, "            if kind != %d { if !leb_skip(wire, &mut off, end, kind) { return -1; } }\n", ir.TableKindArray)
		fmt.Fprintf(b, "            else {\n")
		fmt.Fprintf(b, "                let Some(l) = leb_read(wire, &mut off) else { return -1; };\n")
		fmt.Fprintf(b, "                if l > (end - off) as u64 { return -1; }\n")
		fmt.Fprintf(b, "                let sub = off + l as usize;\n")
		fmt.Fprintf(b, "                if sub - off >= 2 {\n")
		fmt.Fprintf(b, "                    let ek = wire[off]; off += 1;\n")
		fmt.Fprintf(b, "                    let Some(c) = leb_read(wire, &mut off) else { return -1; };\n")
		fmt.Fprintf(b, "                    let carries = match id {\n")
		for _, f := range lists {
			want := ir.TableWireElemKind(f)
			if f.IsMap() {
				want = ir.TableKindTable
			}
			fmt.Fprintf(b, "                        0x%016x => ek == %d || table_kind_widens(ek, %d),\n", ir.TableFieldWireId(f), want, want)
		}
		fmt.Fprintf(b, "                        _ => false,\n")
		fmt.Fprintf(b, "                    };\n")
		fmt.Fprintf(b, "                    if carries {\n")
		fmt.Fprintf(b, "                        if c > 0x7fffffff { return -1; }\n")
		fmt.Fprintf(b, "                        let floor = if ek == %d { 2 } else { table_kind_width(ek).max(1) };\n", ir.TableKindTable)
		fmt.Fprintf(b, "                        if off > sub || c as usize > (sub - off) / floor { return -1; }\n")
		// the root's own extent is root_at's (nested_measure walks the root
		// body, lists included); this pass only refuses the framing, and
		// descends into each element that is itself a body
		var walks []string
		for _, f := range lists {
			child, _ := f.Type.Ref.(*ir.Struct)
			if f.IsMap() {
				child = f.MapEntry
			}
			if child != nil && !f.Type.Pointer {
				walks = append(walks, fmt.Sprintf(`                            0x%016x => {
                                let mut p = off;
                                for _ in 0..c {
                                    if p >= sub { break; }
                                    let Some(n) = leb_read(&wire[..sub], &mut p) else { break; };
                                    if n > (sub - p) as u64 { break; }
                                    if nested_measure(0x%016x, &wire[p..p + n as usize], ids, 0) < 0 { return -1; }
                                    p += n as usize;
                                }
                            }
`, ir.TableFieldWireId(f), ir.TableWireId(child.WireName())))
			}
		}
		if len(walks) > 0 {
			fmt.Fprintf(b, "                        match id {\n")
			for _, w := range walks {
				b.WriteString(w)
			}
			fmt.Fprintf(b, "                            _ => {}\n")
			fmt.Fprintf(b, "                        }\n")
		}
		fmt.Fprintf(b, "                    }\n")
		fmt.Fprintf(b, "                }\n")
		fmt.Fprintf(b, "                off = sub;\n")
		fmt.Fprintf(b, "            }\n")
		fmt.Fprintf(b, "            continue;\n")
		fmt.Fprintf(b, "        }\n")
	}
	fmt.Fprintf(b, "        if !leb_skip(wire, &mut off, end, kind) { return -1; }\n")
	fmt.Fprintf(b, "    }\n")
	fmt.Fprintf(b, "    let root_at = nested_measure(0x%016x, &wire[1..end], ids, 0);\n", ir.TableWireId(st.WireName()))
	fmt.Fprintf(b, "    if root_at < 0 { return -1; }\n")
	fmt.Fprintf(b, "    round(round(%d) + root_at) + blob_total + (records + 1) * 16\n", rootSize)
	fmt.Fprintf(b, "}\n\n")
}

// floatOwner names a float field <Owner>.<field> for the non_finite_float
// refusal (docs/SPEC-TABLES.md §3.3), the owner being the table, type or union
// that declares it, the name the Go reference's reads and writers record. A
// field the emitter holds as a copy (an optional's or an element's inner
// line) is found by its name, in a stable order, and named by itself last.
func (g *gen) floatOwner(f *ir.Field) string { return floatOwnerIn(g.unit, f) }

// floatOwnerIn is floatOwner over a unit, for the emitters that hold no gen.
func floatOwnerIn(u *ir.Unit, f *ir.Field) string {
	structs := func(yield func(*ir.Struct) bool) {
		for _, m := range []map[string]*ir.Struct{u.Tables, u.Structs} {
			for _, k := range slices.Sorted(maps.Keys(m)) {
				if !yield(m[k]) {
					return
				}
			}
		}
	}
	unions := func(yield func(*ir.Union) bool) {
		for _, m := range []map[string]*ir.Union{u.TableUnions, u.Unions} {
			for _, k := range slices.Sorted(maps.Keys(m)) {
				if !yield(m[k]) {
					return
				}
			}
		}
	}
	for _, byName := range []bool{false, true} {
		same := func(x *ir.Field) bool { return x == f || byName && x != nil && x.Name == f.Name }
		for st := range structs {
			if slices.ContainsFunc(st.Fields, same) {
				return st.DeclaredName() + "." + f.Name
			}
		}
		for un := range unions {
			for _, v := range un.Variants {
				if same(v.F) {
					return un.Name + "." + f.Name
				}
			}
		}
	}
	return f.Name
}

// scalarDiffers is the default-elision test of one scalar: the field rides
// when it differs from its default. A PLAIN float compares BIT PATTERNS, not
// values (docs/SPEC-TABLES.md §3.3, "A PLAIN FLOAT'S SPECIAL VALUES"): under
// !=, -0 equals +0 and would be elided and read back as +0.
func scalarDiffers(f *ir.Field, x, def string) string {
	if !f.HasFloatRange && (f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64) {
		return x + ".to_bits() != " + def + ".to_bits()"
	}
	return x + " != " + def
}
