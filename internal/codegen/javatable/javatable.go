// Package javatable emits a unit's Java table surface (docs/SPEC-TABLES.md):
// the BLOCK form's builder and reader in <Table>Block.java (§19) and the COOKED
// form's reader and writer in <Table>Cook.java (§7), the record accessors the two share
// (<Name>Row.java), their reflection descriptors and the unit's build version
// — plus the shared runtime those need, one PUBLIC TYPE PER FILE, which is
// what Java's package scope is. It is emitted only when the unit declares
// tables, and nothing when it declares none: a table-free unit's generated
// Java is byte-identical with or without this package.
//
// THE ACCELERATOR TIER. A block is BUILT or POINTED AT and a cook is WRITTEN or
// OPENED; neither parses a wire on the read path, so both reach every table of
// the unit whatever its closure declares — a pointered unit's cooks open and
// write in full. In Java both operate at explicit offsets in a byte[] the
// caller owns. The TABLE WIRE — measure,
// save, load, the reflection descriptors and the JSON text form of
// docs/SPEC-TABLES.md §3, §4, §8 and §16 in their id-table form — rides the
// FIXED form (§3.4) for every table, and the VARIABLE form (§3, form byte 1)
// and MESSAGE form (§3.3) beside it (schema#517). ROADMAP.md marks the cells.
//
// The C++ backend (internal/codegen/cpptable) is the REFERENCE and the C#
// backend (internal/codegen/cstable) is the worked managed-language port for
// the two forms that remain: this one mirrors their prologue and header checks,
// their offsets and their descriptor rows, and invents no contract of its own.
// Where Java forces a different spelling the reason is stated at the site:
//
//   - THE UNIT'S NAMESPACE IS THE PACKAGE, and a public Java type lives in a
//     file of its own name. So the shared runtime is not "one home file" as it
//     is in C#: it is one file per runtime type (TableBytes.java,
//     TableBlockInfo.java, ...), which is file-order independent by
//     construction rather than by a rule. Each of those spellings is a
//     package-level name and is claimed in internal/tablenames for every
//     backend.
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
//     the accessors' own constants against the descriptors' (block.go).
//
// Nothing on the read path allocates: the caller owns the array, and a block
// or a cook is read where it lies.
package javatable

import (
	"fmt"
	javapkt "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"maps"
	"sort"
	"strings"

	schemacheck "github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// table-wire kinds (docs/SPEC-TABLES.md §3), the ones a descriptor row of the
// block form carries as its field's kind — the numbers are the wire's, not a
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

// javaFile wraps one generated Java source in the banner every file carries.
// Every file this backend writes is generated FOR THE UNIT: a runtime type has
// no schema file of its own, and a Block, Cook or Row is named for its table
// rather than for the file that declares it.
func javaFile(u *ir.Unit, summary, body string) []byte {
	var b strings.Builder
	b.WriteString(generatedFrom("", u))
	b.WriteString(license)
	fmt.Fprintf(&b, "// package %s — %s\n\n", u.Package, summary)
	fmt.Fprintf(&b, "package %s;\n\n", u.Package)
	b.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		b.WriteString("\n")
	}
	return []byte(b.String())
}

// Generate emits the unit's Java table surface — the record accessors, the
// block and cook read halves with their runtime types, and the build version —
// and nothing when the unit declares no table: a table-free unit's generated
// Java is byte-identical with or without this package.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	return GenerateLineage(u, nil)
}

// GenerateLineage is [Generate] with the LINEAGE the build holds for each fixed
// table: the locked layouts, OLDEST FIRST, the current one last
// (docs/FIXED-FORM-ALGORITHM.md §5.2, and §5.9 #1 for why it is a second entry
// point rather than a changed signature). A nil map is a build with NO LOCK, and
// every table then carries the one entry it can always compute: its own.
//
// THE BACKEND OPENS NO FILE. `lockfile.Open`, `lockfile.Lineage` and
// `lockfile.Floor` are the caller's three calls, so the disk is read in one place
// and a test can play the lock in one line.
func GenerateLineage(u *ir.Unit, lineage map[string][]FixedLineageEntry) (map[string][]byte, error) {
	return generateLineage(u, lineage, javapkt.Canonical)
}

// generateLineage is GenerateLineage with the last pass named: canon puts each
// file in the formatter's shape (java.Canonical), and a test passes the
// identity to hold that the pass moves layout and never a token.
func generateLineage(u *ir.Unit, lineage map[string][]FixedLineageEntry, canon func([]byte) []byte) (map[string][]byte, error) {
	if len(u.Tables) == 0 {
		return map[string][]byte{}, nil
	}
	// The variable wire carries fixed-point and 128-bit values (§15), and so
	// does the COOKED FORM: a fixed is its raw integer at its storage width,
	// and a 128-bit slot is sixteen bytes at sixteen (§7.2). The BLOCK FORM
	// keeps the scope's Accelerators limitation: its accessors and builder
	// spell a slot as one Java primitive, which a 128-bit slot is not.
	scope := ir.WideTableKinds(u, "Java", len(fixedRoots(u)) > 0 || len(cardVariableTables(u)) > 0)
	if scope.Unit {
		return nil, scope.Refusal
	}
	if err := checkNames(u); err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	{
		// The two ACCELERATORS operate directly on a byte[] the caller owns:
		// the BLOCK form (§19) builds and opens in-place block images and the
		// COOK (§7) measures, writes, and opens cooked regions, reaching every
		// cookable table of the unit — a pointered unit's cooks open and write in full.
		blocks := ir.Blocks(u)
		if scope.Accelerators {
			blocks = nil
		}
		ck := cookUnitOf(u)
		set := collectRecords(u, blocks, ck)
		// TableBytes and BuildVersion BELONG TO THE TWO ACCELERATORS and to
		// nothing else: block.go and cook.go are their only callers, the fixed
		// form reads its bytes through TableFixed's own accessors and carries
		// its own layout hash rather than the build version. So they ride in
		// this branch, where the accelerators are, and a wide-kind unit that
		// gets only a fixed form is not handed two classes nobody names.
		out["TableBytes.java"] = tableBytesFile(u)
		out["BuildVersion.java"] = buildVersionFile(u)
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
	}
	// THE FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3: this backend's
	// FIRST table wire. It rides unconditionally — a wire a consumer can be
	// handed is not something they opt into — and nothing about form 1 moves.
	fixed, err := generateFixedFiles(u, lineage)
	if err != nil {
		return nil, err
	}
	maps.Copy(out, fixed)
	// THE MESSAGE FORM (docs/SPEC-TABLES.md §3.3), form byte 2: the announcement
	// and batch surface, emitted for a unit with a `fixed table` root.
	message, err := generateMessageFiles(u)
	if err != nil {
		return nil, err
	}
	maps.Copy(out, message)
	// THE VARIABLE FORM (docs/SPEC-TABLES.md §3), form byte 1: the card's wire.
	variable, err := generateVariableFiles(u)
	if err != nil {
		return nil, err
	}
	maps.Copy(out, variable)
	// THE REFLECTION DESCRIPTORS (docs/SPEC-TABLES.md §8): a table type
	// descriptor per card table and the unit registry, emitted beside the
	// variable form whose fields they describe.
	view, err := generateViewFiles(u)
	if err != nil {
		return nil, err
	}
	maps.Copy(out, view)
	// THE RETAIN-UNKNOWN SURFACE (docs/SPEC-TABLES.md §6.6): the retained load,
	// measure and save for the card's constructs.
	retain, err := generateRetainFiles(u)
	if err != nil {
		return nil, err
	}
	maps.Copy(out, retain)
	// every file in the formatter's shape (java.Canonical, docs/JAVA-DIALECT.md).
	for name, src := range out {
		out[name] = canon(src)
	}
	return out, nil
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

// member is a field's Java storage member name — the lowerCamel mapping the
// packet emitter uses, so one field is spelled one way across a unit.
func member(f *ir.Field) string { return javaName(f.Name) }

func intJavaType(width int) string {
	switch {
	case width <= 8:
		return "byte"
	case width <= 16:
		return "short"
	case width <= 32:
		return "int"
	}
	return "long"
}

func enumJavaType(storageBits int) string { return intJavaType(storageBits) }

// isClassRef reports a named reference whose Java storage is a class instance:
// a generated struct/table class or a union.
func isClassRef(t ir.FieldType) bool {
	if t.Kind != ir.TNamed {
		return false
	}
	switch t.Ref.(type) {
	case *ir.Struct, *ir.Union:
		return true
	}
	return false
}

// ---- name checks: what Java's one-public-type-per-file rule can collide ----

// runtimeFileNames is every public type this backend puts at package scope, so
// a schema FILE whose basename lands on one can be refused by name rather than
// clobbering it. (A DECLARATION named one of these is refused by the checker,
// from internal/tablenames; a FILE is this backend's own hazard, because a
// file base is what names the packet emitter's class.)
func runtimeFileNames() []string {
	return []string{
		"TableBytes",
		"TableBlockRows", "TableBlockInfo", "TableBlockFieldInfo", "TableBlockLayout",
		"TableCookInfo", "TableCookFieldInfo", "TableCookStorage", "TableCookLayout", "TableCookWrite",
		"TableFieldInfo", "TableTypeInfo", "TableRegistry",
		"TableVocabulary", "TableMessage",
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
	// and every class the FIXED FORM and MESSAGE FORM claim for this unit
	// (docs/SPEC-TABLES.md §3.3, §3.4): TableFixed, `<Type>Fixed`, TableMessage,
	// and `<Type>Message` per type of a fixed root's closure.
	// Java allows one public class per file, so a schema file that spells one
	// of them is a collision and is refused by name rather than overwritten.
	for _, name := range fixedNames(u) {
		runtime[name] = true
	}
	for _, name := range messageNames(u) {
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
		st := u.Tables[name]
		for _, f := range st.Fields {
			// A reserved spelling is refused ONLY for a fixed table, whose raw
			// member() the fixed/block/cook forms emit unchanged. A variable
			// table's codec escapes it (varMember, the card's `_new` -> `new_`),
			// so it is not refused here.
			if st.FixedDeclared && javaReserved[javaName(f.Name)] {
				return fmt.Errorf("field %s of table %s maps to the reserved Java identifier %q; rename it", f.Name, name, javaName(f.Name))
			}
		}
	}
	return nil
}

// refuseVariableForm refuses, by name, every variable-class construct the card
// record does not use (docs/SPEC-TABLES.md §2, §11). The VARIABLE FORM (§3,
// form byte 1) is taught for plain `table` roots and tables as list elements,
// bool/int32/int64/enum fields, string(N), the *string and *bytes byte buffers
// (§2.5) and unbounded lists of tables ([]T, §2.9). Every other
// variable-class construct — a map (§2.8), a union arm (§2.6), a pointer to a
// table outside a list element (§2.1), an optional ?T (§2.3), an enum-keyed
// array (§2.4) — is refused here, naming the table, the field and the
// construct, so it is never mis-encoded and never silently dropped.
//
// The refusal is scoped to the VARIABLE tables. A DECLARED `fixed table`
// ([ir.Struct.FixedDeclared]) keeps the fixed form's own answers for optional
// and the other constructs it carries (docs/SPEC-TABLES.md §3.4), so it is not
// walked here. A map-entry table is the map's own generated `{ key, value }`
// struct (§2.8); its owner names it, so it is skipped.
func refuseVariableForm(u *ir.Unit) error {
	seen := map[string]bool{}
	for _, st := range u.Tables {
		if st == nil || st.IsMapEntry() {
			continue
		}
		if err := refuseVariableFormClosure(st, seen); err != nil {
			return err
		}
	}
	return nil
}

// refuseVariableFormClosure walks one table and the declarations its closure
// reaches, returning the first construct the variable form does not carry.
func refuseVariableFormClosure(st *ir.Struct, seen map[string]bool) error {
	if seen[st.Name] {
		return nil
	}
	seen[st.Name] = true
	for _, f := range st.Fields {
		if why := variableFormRefusal(f); why != "" {
			return fmt.Errorf("table %s field %s: %s is not carried by the Java variable form (docs/SPEC-TABLES.md §11)", st.Name, f.Name, why)
		}
		if f.IsMap() {
			if err := refuseVariableFormClosure(f.MapEntry, seen); err != nil {
				return err
			}
		}
		if un := variableUnion(f); un != nil {
			if err := refuseVariableFormClosure(variableUnionStruct(un), seen); err != nil {
				return err
			}
		}
		if f.Type.Kind == ir.TNamed {
			if target, ok := f.Type.Ref.(*ir.Struct); ok {
				if err := refuseVariableFormClosure(target, seen); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func cardVariableTables(u *ir.Unit) []*ir.Struct {
	if len(u.Tables) == 0 {
		return nil
	}
	var out []*ir.Struct
	seen := map[string]bool{}
	for _, st := range u.Tables {
		if st == nil || seen[st.Name] {
			continue
		}
		if refuseVariableFormClosure(st, map[string]bool{}) == nil {
			seen[st.Name] = true
			out = append(out, st)
		}
	}
	for _, st := range u.Structs {
		if st == nil || seen[st.Name] {
			continue
		}
		if refuseVariableFormClosure(st, map[string]bool{}) == nil {
			seen[st.Name] = true
			out = append(out, st)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// variableFormRefusal names the variable-class construct a field spells that
// the Java variable form does not carry, or "" when it carries it
// (docs/SPEC-TABLES.md §2, §11). The *string and *bytes byte buffers (§2.5) are
// carried; a pointer to a table by value is not.
func variableFormRefusal(f *ir.Field) string {
	if f.Type.Pointer {
		switch {
		case f.Type.Kind == ir.TString && f.Type.Blob():
			return "" // the *string byte buffer, the card's own construct (§2.5)
		case f.Type.Kind == ir.TBytes && f.Type.Blob():
			return "" // the *bytes byte buffer (§2.5)
		case f.Type.Kind == ir.TNamed:
			if _, ok := f.Type.Ref.(*ir.Struct); ok {
				return "" // pointer to a table (§2.1, §3.1)
			}
			return "a pointer to a non-table type (docs/SPEC-TABLES.md §2.1)"
		default:
			return "a pointer to a non-table type (docs/SPEC-TABLES.md §2.1)"
		}
	}

	return ""
}

// javaReserved is Java's reserved words, read from the one table (check.ReservedIn,
// SPEC §4.6) and never copied here.
var javaReserved = schemacheck.ReservedIn("Java")
