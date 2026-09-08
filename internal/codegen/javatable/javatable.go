// Package javatable emits a unit's Java table surface (docs/SPEC-TABLES.md):
// the BLOCK form's read half in <Table>Block.java (§19) and the COOKED form's
// read half in <Table>Cook.java (§7), the record accessors the two share
// (<Name>Row.java), their reflection descriptors and the unit's build version
// — plus the shared runtime those need, one PUBLIC TYPE PER FILE, which is
// what Java's package scope is. It is emitted only when the unit declares
// tables, and nothing when it declares none: a table-free unit's generated
// Java is byte-identical with or without this package.
//
// THE ACCELERATOR TIER. A block is POINTED AT and a cook is OPENED; neither
// parses a wire, so both reach every table of the unit whatever its closure
// declares — a pointered unit's cooks open in full. In Java both are read at
// explicit offsets out of a byte[] the consumer owns. The TABLE WIRE itself —
// measure, save, load, the reflection descriptors and the JSON text form of
// docs/SPEC-TABLES.md §3, §4, §8 and §16 in their id-table form — is not
// emitted for Java. The port that once stood here wrote the form that preceded
// the id-table wire, which the current specification does not describe and the
// C++ reference does not open; it was removed rather than carried (schema#517
// is the row that brings the wire to Java). ROADMAP.md marks the cells.
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
	"maps"
	"sort"
	"strings"

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
	if len(u.Tables) == 0 {
		return map[string][]byte{}, nil
	}
	if err := ir.RefuseWideTableKinds(u, "Java"); err != nil {
		return nil, err
	}
	if err := checkNames(u); err != nil {
		return nil, err
	}
	// The two ACCELERATORS need no wire codec: the BLOCK form (§19) reads
	// bytes a producer wrote and the COOK (§7) reads a region the tooling
	// wrote. Both are pure readers over a byte[] the consumer owns, so both
	// reach every table of the unit — a pointered unit's cooks open in full.
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
