// Package java emits the Java target: one .java file per schema file —
// Constants.schema -> Constants.java — deterministic to the byte (SPEC §6.1).
// Java allows one public class per file, so each schema file becomes one
// public final class of the same name (the protobuf outer-class shape):
// nested static classes for the file's types and unions, static constants,
// and the static monomorphic wire functions. Every file of a unit shares
// `package <unitpackage>;`, so cross-file references are plain qualified
// names — Types.Vec3, Constants.maxChatLength — and generated files never
// import each other.
//
// The target is Java 17 on the JVM. Generated code is SELF-CONTAINED: it
// references only java.lang, java.lang.invoke.VarHandle word access and
// java.util.Arrays, never a runtime package. The serialize.java port
// measured the JVM's failure modes against the C++ reference and each is a
// directive here (issue #156): separate monomorphic write/read functions
// per type that never dispatch through a stream interface (the unified
// pattern measured ~2x), the family bitpacker INLINED at every field with
// literal constant bit widths and masks (a literal-width probe ran +40%
// write / +85% read over array-driven widths), and writer contracts in ONE
// `assert checkWriteX(value, data)` predicate call per function — dormant
// assert bodies count against C2's inline thresholds even with -ea absent,
// so the hot bodies carry a single small call and the contract walk lives
// in a private predicate (the port's assert-predicate shape). Zero
// allocation on the hot paths; the 128-bit storage widths ride the emitted
// immutable Int128/UInt128 pair, which allocates per value exactly as the
// port's Int128Value/UInt128Value do.
//
// Storage is the §6.1 storage principle in Java's value domain: classes
// with public fields, member names in lowerCamelCase — the first-letter-
// lowered form of the same ir.GoExportName mapping the Go/C#/JS/Dart
// targets share, a bijection on it, so the checker's existing collision
// detection covers Java without a second registry. Java has no unsigned
// types: integer storage is the same-width signed type, bit-transparent
// (the protobuf convention), with masks applied after widening wherever an
// unsigned value enters 64-bit wire arithmetic and Long.compareUnsigned
// wherever unsigned ordering matters. float32 stores float (Java's native
// single), float64 double, flags long (uint64 patterns), and the 128-bit
// widths the emulated (hi, lo) pair — Int128.java/UInt128.java emitted
// beside the unit exactly when a 128-bit field exists.
//
// Functions per type, static on the declaring file's class:
// write<Name>(value, data) -> bytes written (trusted writer, contracts
// asserted through the predicate); read<Name>(value, data, numBits) ->
// boolean (the family read verdict — reads validate EVERYTHING and never
// throw on hostile bytes); measure<Name>(value) -> exact wire bits (static
// runs folded to one literal at generation time); zero<Name>(value) -> the
// §5 zero form. Buffers are caller-owned byte arrays: write buffers hold
// <name>MaxBytes (a multiple of 8 — the family write-buffer granularity);
// read buffers need NO slack past the payload — the reader prices its
// 64-bit windows inside the buffer via the assembled tail window, the same
// no-slack stance the Dart target ships.
package java

import (
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mas-bandwidth/schema/v2/internal/ast"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Generate returns basename.java -> file contents for every file of the
// unit, plus Int128.java and UInt128.java when the unit has 128-bit storage.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	return generate(u, Canonical)
}

// generate is Generate with its last pass named (see Canonical); a test passes
// the identity to hold that the pass moves layout and never a token.
func generate(u *ir.Unit, canon func([]byte) []byte) (map[string][]byte, error) {
	if err := checkNames(u); err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	home := ir.ProtocolIdHome(u)
	bulk := unitBulkBytes(u)
	if unitNeeds128(u) {
		for _, f := range u.Files {
			if f.Base == "Int128" || f.Base == "UInt128" {
				return nil, fmt.Errorf("schema file %s.schema collides with the %s.java support library the Java emitter writes beside a 128-bit unit; rename the file", f.Base, f.Base)
			}
		}
		out["Int128.java"] = canon(emitInt128Support(u))
		out["UInt128.java"] = canon(emitUInt128Support(u))
	}
	for _, f := range u.Files {
		g := &gen{unit: u, file: f, home: f.Base == home, bulkBytes: bulk}
		g.emitFile()
		out[f.Base+".java"] = canon(g.assemble())
	}
	return out, nil
}

// unitBulkBytes is the union of ir.AlignedFixedByteArrays over every struct
// of the unit: the [N]uint8 fields whose wire position is statically
// byte-aligned, safe to bulk-copy at ANY embedding because the analysis
// treats each struct's entry position as unknown. The union is valid in the
// inlined bodies (a field belongs to exactly one struct).
func unitBulkBytes(u *ir.Unit) map[*ir.Field]bool {
	out := map[*ir.Field]bool{}
	for _, st := range u.Structs {
		for f := range ir.AlignedFixedByteArrays(st) {
			out[f] = true
		}
	}
	return out
}

// unitNeeds128 reports whether any field of the unit stores as the emulated
// 128-bit pair (int128/uint128, or fixed point of storage width 128).
//
// TABLES AND TABLE UNIONS ARE ASKED TOO, and they are the reason this is not
// a walk of Structs alone: a `table` is DELIBERATELY absent from Structs (see
// ir.Unit) because it rides the table wire and never the packet wire, and the
// same goes for a union with a table arm. But the FIXED FORM (docs/SPEC-TABLES.md
// §3.4) carries an int128/uint128 as sixteen bytes and spells the value it
// scatters `new UInt128(hi, lo)` — so a unit whose only 128-bit field is a
// table's own field still names Int128/UInt128 in its generated Java, and
// skipping them there emits a package that does not compile. Such a unit is
// exactly the wide-kind unit the two ACCELERATORS refuse (§15), which is why
// nothing but the fixed form ever reaches this case.
func unitNeeds128(u *ir.Unit) bool {
	for _, st := range u.Structs {
		if structNeeds128(st) {
			return true
		}
	}
	for _, st := range u.Tables {
		if structNeeds128(st) {
			return true
		}
	}
	for _, un := range u.TableUnions {
		for _, v := range un.Variants {
			if v.F != nil && is128(v.F.Type) {
				return true
			}
		}
	}
	return false
}

func structNeeds128(st *ir.Struct) bool {
	for _, f := range st.Fields {
		if is128(f.Type) {
			return true
		}
	}
	return false
}

func is128(t ir.FieldType) bool {
	return (t.Kind == ir.TInt || t.Kind == ir.TFixed) && t.Width == 128
}

// javaReserved is every spelling a generated lowerCamel Java identifier must
// not take: the Java keywords and literals, plus the contextual keywords a
// simple identifier cannot safely be.
var javaReserved = map[string]bool{
	"abstract": true, "assert": true, "boolean": true, "break": true,
	"byte": true, "case": true, "catch": true, "char": true, "class": true,
	"const": true, "continue": true, "default": true, "do": true,
	"double": true, "else": true, "enum": true, "extends": true,
	"final": true, "finally": true, "float": true, "for": true,
	"goto": true, "if": true, "implements": true, "import": true,
	"instanceof": true, "int": true, "interface": true, "long": true,
	"native": true, "new": true, "package": true, "private": true,
	"protected": true, "public": true, "return": true, "short": true,
	"static": true, "strictfp": true, "super": true, "switch": true,
	"synchronized": true, "this": true, "throw": true, "throws": true,
	"transient": true, "try": true, "void": true, "volatile": true,
	"while": true, "true": true, "false": true, "null": true,
	"var": true, "yield": true, "record": true,
}

// javaTypeHazards is every PascalCase spelling a generated nested class must
// not take: the java.lang / core names generated bodies use unqualified —
// a nested class named Long would shadow java.lang.Long inside its file —
// plus the emitted 128-bit support pair.
var javaTypeHazards = map[string]bool{
	"String": true, "StringBuilder": true, "Long": true, "Float": true,
	"Double": true, "Math": true, "System": true,
	"Int128": true, "UInt128": true,
}

// javaName maps an exported member/constant/variant name into Java's
// lowerCamelCase: the first-letter-lowered form of ir.GoExportName. The two
// spellings are a bijection, so the checker's PascalCase collision registry
// covers Java members without a second registry.
func javaName(name string) string {
	s := ir.GoExportName(name)
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// lowerFirst lowers the first letter of an already-exported name (type names
// feeding function/constant spellings: ShipCreate -> shipCreate...).
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// checkNames refuses any declaration whose Java spelling lands on a reserved
// word or a shadow-hazard name — mangling silently would make the Java
// surface diverge from every sibling target's naming.
func checkNames(u *ir.Unit) error {
	check := func(kind, owner, name, mapped string) error {
		if javaReserved[mapped] {
			if owner != "" {
				return fmt.Errorf("%s %s of %s maps to the reserved Java identifier %q; rename it", kind, name, owner, mapped)
			}
			return fmt.Errorf("%s %s maps to the reserved Java identifier %q; rename it", kind, name, mapped)
		}
		return nil
	}
	fileBases := map[string]bool{}
	for _, f := range u.Files {
		fileBases[f.Base] = true
	}
	checkType := func(kind, name string) error {
		if fileBases[name] {
			return fmt.Errorf("%s %s collides with the outer class the Java emitter writes for %s.schema (one public class per file); rename one of them", kind, name, name)
		}
		if javaTypeHazards[name] {
			return fmt.Errorf("%s %s would shadow the core Java name %q inside generated files; rename it", kind, name, name)
		}
		return nil
	}
	for _, c := range u.Consts {
		if err := check("const", "", c.Name, javaName(c.Name)); err != nil {
			return err
		}
	}
	for _, e := range u.Enums {
		if err := checkType("enum", e.Name); err != nil {
			return err
		}
		for _, v := range e.Variants {
			if err := check("variant", e.Name, v, javaName(v)); err != nil {
				return err
			}
		}
	}
	for _, fl := range u.Flags {
		for _, v := range fl.Variants {
			if err := check("variant", fl.Name, v, javaName(fl.Name+v)); err != nil {
				return err
			}
		}
	}
	for _, st := range u.Structs {
		if err := checkType("type", st.Name); err != nil {
			return err
		}
		for _, f := range st.Fields {
			if err := check("field", st.Name, f.Name, javaName(f.Name)); err != nil {
				return err
			}
		}
	}
	for _, un := range u.Unions {
		if err := checkType("union", un.Name); err != nil {
			return err
		}
		if err := checkType("union tag namespace", un.Name+"Type"); err != nil {
			return err
		}
		for _, v := range un.Variants {
			if err := check("variant", un.Name, v.Name, javaName(v.Name)); err != nil {
				return err
			}
		}
	}
	return nil
}

type gen struct {
	unit *ir.Unit
	file *ir.File
	home bool // this file carries protocolId and the unit-level target notes

	body strings.Builder

	// bulkBytes marks the [N]uint8 fields at statically byte-aligned wire
	// positions (ir.AlignedFixedByteArrays, unioned across the unit): these
	// serialize through the fused bulk copy instead of the per-byte merge.
	bulkBytes map[*ir.Field]bool

	// per-function emission state (functions.go)
	fn        strings.Builder
	loopDepth int
	needV     bool // the group value temp
	needLo    bool // the wide-lane accumulator (33..64-bit reads, 128 lanes)
	needHi    bool // the high 64-bit lane (128-bit-storage reads)
	usesRead  bool // read side: the window machinery is used

	usesWire bool // the file emitted wire functions: the VarHandle rides
}

func (g *gen) bpf(format string, args ...any) {
	fmt.Fprintf(&g.body, format, args...)
}

// qualify renders declaration decl's Java spelling from the current file:
// plain when declared here, OuterClass.name from any other file of the unit.
func (g *gen) qualify(decl, spelled string) string {
	base, ok := g.unit.DeclFile[decl]
	if !ok || base == g.file.Base {
		return spelled
	}
	return base + "." + spelled
}

// qualifyType is qualify for a type/enum/union/tag-namespace name.
func (g *gen) qualifyType(name string) string {
	return g.qualify(name, name)
}

func (g *gen) assemble() []byte {
	var h strings.Builder
	// the basename, not the invocation-relative path: output is deterministic
	// to the byte wherever the compiler runs (SPEC §6.1)
	fmt.Fprintf(&h, "// Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", g.file.Base)
	h.WriteString("// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	h.WriteString("// your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	h.WriteString("// AGPL-3.0, its output is not.\n")
	fmt.Fprintf(&h, "// package %s — protocol id 0x%016x\n", g.unit.Package, g.unit.ProtocolId)
	if g.home {
		// the unit-level target notes ride the home file only — said once
		// per unit, not once per file
		h.WriteString("//\n")
		h.WriteString("// The shipped Java wire path (issue #156): the serialize.java bitpacker\n")
		h.WriteString("// inlined at every field, literal constant widths and masks, monomorphic\n")
		h.WriteString("// write/read/measure functions per type, zero runtime dependencies.\n")
		h.WriteString("//\n")
		h.WriteString("// write<Name>(value, data) -> bytes written. The writer TRUSTS the caller\n")
		h.WriteString("// (the C++ stance): contracts live in one assert-predicate call — active\n")
		h.WriteString("// under -ea, dormant bytecode otherwise — and width masks stay, as wire\n")
		h.WriteString("// arithmetic. The buffer must hold <name>MaxBytes (a multiple of 8).\n")
		h.WriteString("//\n")
		h.WriteString("// read<Name>(value, data, numBits) -> boolean, the family read verdict.\n")
		h.WriteString("// The wire is a trust boundary: reads validate everything — bounds fused\n")
		h.WriteString("// per static run, ranges, wire constants, reserved and align padding,\n")
		h.WriteString("// union tags — and NEVER throw on hostile bytes. No slack past the\n")
		h.WriteString("// payload is required: the reader prices its 64-bit windows inside the\n")
		h.WriteString("// buffer.\n")
		h.WriteString("//\n")
		h.WriteString("// measure<Name>(value) -> exact wire bits for that value (trusted, like\n")
		h.WriteString("// the writer). zero<Name>(value) -> the §5 zero form.\n")
	}
	h.WriteString("\n")
	fmt.Fprintf(&h, "package %s;\n\n", g.unit.Package)
	fmt.Fprintf(&h, "// %s carries every generated declaration of %s.schema — Java has no\n", g.file.Base, g.file.Base)
	h.WriteString("// top-level functions or constants, so the file's one public class is their\n")
	h.WriteString("// home; types nest inside it (SPEC §6.1 naming).\n")
	fmt.Fprintf(&h, "public final class %s {\n", g.file.Base)
	fmt.Fprintf(&h, "    private %s() {}\n\n", g.file.Base)
	if g.usesWire {
		h.WriteString("    // 64-bit little-endian word access into byte[] — the proven-fast JVM\n")
		h.WriteString("    // form (VarHandle byteArrayViewVarHandle, serialize.java's own).\n")
		h.WriteString("    private static final java.lang.invoke.VarHandle LONG_LE =\n")
		h.WriteString("            java.lang.invoke.MethodHandles.byteArrayViewVarHandle(\n")
		h.WriteString("                    long[].class, java.nio.ByteOrder.LITTLE_ENDIAN);\n\n")
	}
	h.WriteString(strings.TrimRight(g.body.String(), "\n"))
	h.WriteString("\n}\n")
	return []byte(h.String())
}

func (g *gen) emitFile() {
	if g.home {
		g.bpf("    // The unit's protocol id — the hash of its wire shape (SPEC §3.1). Two\n")
		g.bpf("    // sides at the same id speak identical bits; there is no other versioning.\n")
		g.bpf("    public static final long protocolId = 0x%016xL;\n\n", g.unit.ProtocolId)
	}

	// EmissionOrder, not declaration order: a static-final initializer
	// referencing a later simple name is an illegal forward reference in
	// Java, exactly as in JS and Dart
	for _, d := range ir.EmissionOrder(g.file) {
		switch d := d.(type) {
		case *ir.Const:
			g.emitConst(d)
		case *ir.Enum:
			g.emitEnum(d)
		case *ir.Flags:
			g.emitFlags(d)
		case *ir.Struct:
			g.emitClass(d)
			g.emitStructFunctions(d)
		case *ir.Union:
			g.emitUnion(d)
			g.emitUnionFunctions(d)
		}
	}
}

// emitConst emits a schema const: integer constants as int where the value
// fits (so they serve as array dimensions directly), long past that; float
// constants as float/double per storage.
func (g *gen) emitConst(d *ir.Const) {
	name := javaName(d.Name)
	g.bpf("%s", ir.DocComment(d.Doc, "    ", "//"))
	if d.IsFloat {
		if d.Storage == "float32" {
			g.bpf("    public static final float %s = %sf;%s\n\n", name, formatFloat32(d.Float), g.foldComment(d.Expr))
			return
		}
		g.bpf("    public static final double %s = %s;%s\n\n", name, formatFloat(d.Float), g.foldComment(d.Expr))
		return
	}
	typ := "int"
	if !fitsInt32(d.Int) {
		typ = "long"
	}
	g.bpf("    public static final %s %s = %s;%s\n\n", typ, name, g.renderInt(d.Expr, d.Int), g.foldComment(d.Expr))
}

// foldComment returns a trailing comment carrying the schema expression when
// the rendered Java had to fold it (an E.Max reference has no Java twin).
func (g *gen) foldComment(e ast.Expr) string {
	if e != nil && ir.ExprHasEnumMax(e) {
		return fmt.Sprintf(" // = %s", ir.RenderExpr(e))
	}
	return ""
}

// enumJavaType is the Java storage type of an integer-constant namespace
// (enums and union tags): the same-width signed type for the declared
// storage bits, bit-transparent like every unsigned storage here.
func enumJavaType(storageBits int) string {
	switch {
	case storageBits <= 8:
		return "byte"
	case storageBits <= 16:
		return "short"
	case storageBits <= 32:
		return "int"
	}
	return "long"
}

// emitTagEnum emits an integer-constant namespace — the Java translation of
// the family's integer-backed enums: a final class of static constants,
// because storage must hold every wire-legal value and | max = ... headroom
// values have no Java enum member to be.
// docs is each arm's doc comment (SPEC §4.1), parallel to members: it reaches
// the tag's constant as a line comment, the one place the arm is a
// declaration of its own.
func (g *gen) emitTagEnum(name, typ string, members, docs []string, max int64, comment string) {
	g.bpf("    // %s: %s\n", name, comment)
	g.bpf("    public static final class %s {\n", name)
	g.bpf("        private %s() {}\n\n", name)
	g.bpf("        public static final %s none = 0;\n", typ)
	for i, m := range members {
		g.bpf("%s", ir.DocComment(docs[i], "        ", "//"))
		g.bpf("        public static final %s %s = %s;\n", typ, javaName(m), narrowLit(typ, big.NewInt(int64(i+1))))
	}
	g.bpf("        // the declared variant count (SPEC §4.2)\n")
	g.bpf("        public static final %s count = %s;\n", typ, narrowLit(typ, big.NewInt(int64(len(members)))))
	g.bpf("        // the exported extent (SPEC §4.2)\n")
	g.bpf("        public static final %s max = %s;\n", typ, narrowLit(typ, big.NewInt(max)))
	g.bpf("    }\n\n")

	// the tag enum's name surface is a declared enum's, member for member: a
	// reader logging which message arrived writes enumName<Tag>(value)
	// whichever enum it is. Nothing on the read or write path calls it.
	g.bpf("    // enumName%s: debug/log/tooling name for any %s storage value —\n", name, name)
	g.bpf("    // out-of-set values (wire-legal up to the declared max) name as \"???\"\n")
	g.bpf("    public static String enumName%s(long value) {\n", name)
	g.bpf("        if (value == %s.none) {\n            return \"None\";\n        }\n", name)
	for _, m := range members {
		g.bpf("        if (value == %s.%s) {\n            return \"%s\";\n        }\n", name, javaName(m), m)
	}
	g.bpf("        return \"???\";\n    }\n\n")
}

func (g *gen) emitEnum(d *ir.Enum) {
	typ := enumJavaType(d.StorageBits)
	g.bpf("%s", ir.DocComment(d.Doc, "    ", "//"))
	g.bpf("    // %s — None = 0 implicit, variants dense from 1, wire range [0, %d] (SPEC §4.2);\n", d.Name, d.Max)
	g.bpf("    // an int-constant namespace — the Java translation of the family's integer-\n")
	g.bpf("    // backed enums: storage must hold every wire-legal value, and | max = ...\n")
	g.bpf("    // headroom values have no Java enum member to be\n")
	g.bpf("    public static final class %s {\n", d.Name)
	g.bpf("        private %s() {}\n\n", d.Name)
	g.bpf("        public static final %s none = 0;\n", typ)
	for i, v := range d.Variants {
		g.bpf("%s", ir.DocComment(d.VariantDocs[i], "        ", "//"))
		g.bpf("        public static final %s %s = %s;\n", typ, javaName(v), narrowLit(typ, big.NewInt(int64(i+1))))
	}
	g.bpf("        // the declared variant count (SPEC §4.2)\n")
	g.bpf("        public static final %s count = %s;\n", typ, narrowLit(typ, big.NewInt(int64(len(d.Variants)))))
	g.bpf("        // the exported extent (SPEC §4.2)\n")
	g.bpf("        public static final %s max = %s;\n", typ, narrowLit(typ, big.NewInt(d.Max)))
	g.bpf("    }\n\n")

	g.bpf("    // enumName%s: debug/log/tooling name for any %s storage value —\n", d.Name, d.Name)
	g.bpf("    // out-of-set values (wire-legal up to the declared max) name as \"???\"\n")
	g.bpf("    public static String enumName%s(long value) {\n", d.Name)
	g.bpf("        if (value == %s.none) {\n            return \"None\";\n        }\n", d.Name)
	for _, v := range d.Variants {
		g.bpf("        if (value == %s.%s) {\n            return \"%s\";\n        }\n", d.Name, javaName(v), v)
	}
	g.bpf("        return \"???\";\n    }\n\n")
}

func (g *gen) emitFlags(d *ir.Flags) {
	g.bpf("%s", ir.DocComment(d.Doc, "    ", "//"))
	g.bpf("    // %s — one bit per variant, consumed as masks; flags-typed fields store\n", d.Name)
	g.bpf("    // uint64 in every target — a bit-transparent long here — wire %d bits\n", d.WireBits)
	g.bpf("    // (SPEC §4.2). Mask names are the family's flat spelling, lowerCamel.\n")
	for i, v := range d.Variants {
		g.bpf("%s", ir.DocComment(d.VariantDocs[i], "    ", "//"))
		g.bpf("    public static final long %s = 1L << %d;\n", javaName(d.Name+v), i)
	}
	g.bpf("    // the declared variant count (SPEC §4.2)\n")
	g.bpf("    public static final int %sCount = %d;\n", lowerFirst(d.Name), len(d.Variants))
	g.bpf("\n")

	g.bpf("    // flagName%s: debug/log/tooling name for bit i of %s —\n", d.Name, d.Name)
	g.bpf("    // out-of-range bits name as \"???\"\n")
	g.bpf("    public static String flagName%s(int bit) {\n", d.Name)
	g.bpf("        switch (bit) {\n")
	for i, v := range d.Variants {
		g.bpf("            case %d:\n                return \"%s\";\n", i, v)
	}
	g.bpf("            default:\n                return \"???\";\n        }\n    }\n\n")

	g.bpf("    // flagNames%s renders the set bits of value as \"A|B\" — \"0\" for the\n", d.Name)
	g.bpf("    // empty set, bits past the declared variants as hex\n")
	g.bpf("    public static String flagNames%s(long value) {\n", d.Name)
	g.bpf("        StringBuilder names = new StringBuilder();\n")
	for i, v := range d.Variants {
		g.bpf("        if ((value & (1L << %d)) != 0) {\n", i)
		g.bpf("            if (names.length() > 0) {\n                names.append('|');\n            }\n")
		g.bpf("            names.append(\"%s\");\n        }\n", v)
	}
	if len(d.Variants) < 64 { // a 64-variant set has no room for unknown bits
		g.bpf("        if (value >>> %d != 0) {\n", len(d.Variants))
		g.bpf("            if (names.length() > 0) {\n                names.append('|');\n            }\n")
		g.bpf("            // Long.toHexString renders the bit pattern, not a sign\n")
		g.bpf("            names.append(\"0x\").append(Long.toHexString((value >>> %d) << %d));\n", len(d.Variants), len(d.Variants))
		g.bpf("        }\n")
	}
	g.bpf("        return names.length() == 0 ? \"0\" : names.toString();\n    }\n\n")
}

func (g *gen) emitClass(d *ir.Struct) {
	g.bpf("%s", ir.DocComment(d.Doc, "    ", "//"))
	g.bpf("    // type %s\n", d.Name)
	g.bpf("    public static final class %s {\n", d.Name)
	if len(d.Fields) == 0 {
		g.bpf("        // empty body — presence is the payload (SPEC §4.6)\n")
	}
	prevGuard := ""
	var ctor []string
	for _, f := range d.Fields {
		if f.Guard != prevGuard {
			if f.Guard != "" {
				g.bpf("\n        // %s — wire branch; storage holds both sides, a read zeroes the\n", f.Guard)
				g.bpf("        // untaken side (SPEC §5)\n")
			} else {
				g.bpf("\n") // leaving a branch group — separate so membership stays visible
			}
			prevGuard = f.Guard
		}
		g.bpf("%s", ir.DocComment(f.Doc, "        ", "//"))
		ctor = append(ctor, g.emitStorageField(f)...)
	}
	if len(ctor) > 0 {
		g.bpf("\n        // pre-allocated element instances — the storage principle (SPEC §6.1):\n")
		g.bpf("        // every buffer exists at construction\n")
		g.bpf("        public %s() {\n", d.Name)
		for _, line := range ctor {
			g.bpf("%s", line)
		}
		g.bpf("        }\n")
	}
	g.bpf("    }\n\n")
}

// emitStorageField writes one field's storage declaration and returns any
// constructor lines it needs (object-array element construction — Java
// object arrays start null).
func (g *gen) emitStorageField(f *ir.Field) []string {
	name := javaName(f.Name)
	switch {
	case f.Type.Kind == ir.TWString:
		g.bpf("        public final char[] %s = new char[%s];\n        public int %sLength;\n", name, g.renderArraySize(f.Type.SizeExpr, big.NewInt(f.Type.Size)), name)
		return nil
	case f.Type.Kind == ir.TString:
		g.bpf("        // string(%s): max length, used length beside it (SPEC §4.7)\n", ir.RenderExpr(f.Type.SizeExpr))
		g.bpf("        public final byte[] %s = new byte[%s];\n", name, g.renderArraySize(f.Type.SizeExpr, big.NewInt(f.Type.Size)))
		g.bpf("        public int %sLength;\n", name)
		return byteDefaultLines(f, name, "            ")
	case f.Type.Kind == ir.TBytes:
		g.bpf("        // bytes(%s): fixed buffer, used length beside it (SPEC §4.7)\n", ir.RenderExpr(f.Type.SizeExpr))
		g.bpf("        public final byte[] %s = new byte[%s];\n", name, g.renderArraySize(f.Type.SizeExpr, big.NewInt(f.Type.Size)))
		g.bpf("        public int %sLength;\n", name)
		return byteDefaultLines(f, name, "            ")
	case f.Array != ir.ArrayNone:
		g.emitFieldComment(f)
		bound := g.renderArraySize(f.ArrayExpr, big.NewInt(f.ArrayBound))
		var ctor []string
		switch {
		case f.Type.Kind == ir.TNamed && isClassRef(f.Type.Ref):
			typ := g.qualifyType(f.Type.Name)
			g.bpf("        public final %s[] %s = new %s[%s];\n", typ, name, typ, bound)
			ctor = append(ctor,
				fmt.Sprintf("            for (int i = 0; i < %s.length; i++) {\n", name),
				fmt.Sprintf("                %s[i] = new %s();\n", name, typ),
				"            }\n")
		case is128(f.Type):
			typ := "UInt128"
			if f.Type.Signed {
				typ = "Int128"
			}
			g.bpf("        public final %s[] %s = new %s[%s];\n", typ, name, typ, bound)
			ctor = append(ctor,
				fmt.Sprintf("            java.util.Arrays.fill(%s, %s.zero);\n", name, typ))
		default:
			g.bpf("        public final %s[] %s = new %s[%s];\n", scalarJavaType(f.Type), name, scalarJavaType(f.Type), bound)
		}
		if f.Array == ir.ArrayCounted {
			// a [A..B] count is born at A, the one wire-legal count a fresh
			// value can carry (SPEC §4.6). A [..N] count takes the field's zero
			if n := f.BornCount(); n > 0 {
				g.bpf("        public int %sCount = %d;\n", name, n)
			} else {
				g.bpf("        public int %sCount;\n", name)
			}
		}
		return ctor
	default:
		g.emitFieldComment(f)
		typ, init := g.scalarStorage(f)
		if init == "" {
			g.bpf("        public %s %s;\n", typ, name)
		} else {
			g.bpf("        public %s %s = %s;\n", typ, name, init)
		}
	}
	return nil
}

// Construction and Init write the same literal bytes into existing zeroed
// storage. Casts keep bytes above 127 bit-exact in Java's signed byte type.
func byteDefaultLines(f *ir.Field, name, ind string) []string {
	if !f.HasDefault {
		return nil
	}
	var lines []string
	for i, b := range f.DefBytes {
		lines = append(lines, fmt.Sprintf("%s%s[%d] = (byte) 0x%02x;\n", ind, name, i, b))
	}
	return append(lines, fmt.Sprintf("%s%sLength = %d;\n", ind, name, len(f.DefBytes)))
}

// emitFieldComment writes a field's annotation comment on its own line.
func (g *gen) emitFieldComment(f *ir.Field) {
	if c := g.fieldComment(f); c != "" {
		g.bpf("        // %s\n", c)
	}
}

// scalarStorage is a scalar field's Java type and initializer (the specified
// default, else empty — Java zero-initializes fields, the §5 zero form).
func (g *gen) scalarStorage(f *ir.Field) (typ, init string) {
	t := f.Type
	switch {
	case t.Kind == ir.TBool:
		if f.HasDefault && f.DefBool {
			return "boolean", "true"
		}
		return "boolean", ""
	case t.Kind == ir.TFloat32:
		if f.HasDefault {
			return "float", formatFloat32(f.DefFloat) + "f"
		}
		return "float", ""
	case t.Kind == ir.TFloat64:
		if f.HasDefault {
			return "double", formatFloat(f.DefFloat)
		}
		return "double", ""
	case is128(t):
		typ = "UInt128"
		if t.Signed {
			typ = "Int128"
		}
		if f.HasDefault {
			// a fixed default's DefInt is ALREADY the raw scaled integer —
			// the C++ target pins this semantics
			return typ, pairCtor(t.Signed, f.DefInt)
		}
		return typ, typ + ".zero"
	case t.Kind == ir.TFixed:
		typ = intJavaType(t.Width)
		if f.HasDefault {
			// DefInt is ALREADY the raw scaled integer (the C++ target pins
			// this); fixed storage is that raw value (STANDARD.md, fixed)
			return typ, narrowLit(typ, f.DefInt)
		}
		return typ, ""
	case t.Kind == ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Enum:
			typ = enumJavaType(ref.StorageBits)
			if f.DefVariant != "" {
				return typ, g.qualifyType(t.Name) + "." + javaName(f.DefVariant)
			}
			return typ, ""
		case *ir.Flags:
			if f.HasDefault {
				return "long", narrowLit("long", f.DefInt)
			}
			return "long", ""
		case *ir.Struct, *ir.Union:
			q := g.qualifyType(t.Name)
			return "final " + q, "new " + q + "()"
		}
	case t.Kind == ir.TBits:
		if t.Width <= 32 {
			typ = "int"
		} else {
			typ = "long"
		}
		if f.HasDefault {
			return typ, narrowLit(typ, f.DefInt)
		}
		return typ, ""
	}
	// integer storage in the same-width signed type, bit-transparent
	typ = intJavaType(t.Width)
	if f.HasDefault {
		if s := g.renderNarrowInt(typ, f.DefExpr, f.DefInt); s != "" {
			return typ, s
		}
		return typ, narrowLit(typ, f.DefInt)
	}
	return typ, ""
}

// renderNarrowInt renders a default expression symbolically only when the
// storage type is wide enough that no cast is needed (int or long) — narrow
// storage folds to the cast literal instead.
func (g *gen) renderNarrowInt(typ string, e ast.Expr, folded *big.Int) string {
	if typ != "int" && typ != "long" {
		return ""
	}
	if typ == "int" && !fitsInt32(folded) {
		return ""
	}
	return g.renderInt(e, folded)
}

// intJavaType is the same-width signed Java type for an integer storage
// width (Java has no unsigned types; unsigned values ride bit-transparent,
// the protobuf convention).
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

// scalarJavaType is the element type of a scalar array — dense storage with
// the width's own store semantics, the Java twin of C#'s T[N].
func scalarJavaType(t ir.FieldType) string {
	switch t.Kind {
	case ir.TInt, ir.TFixed:
		return intJavaType(t.Width)
	case ir.TBits:
		if t.Width <= 32 {
			return "int"
		}
		return "long"
	case ir.TFloat32:
		return "float"
	case ir.TFloat64:
		return "double"
	case ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Enum:
			return enumJavaType(ref.StorageBits)
		case *ir.Flags:
			return "long" // §6.1: flags-typed fields store uint64
		}
	}
	return "boolean" // unreachable: bool arrays have no schema spelling
}

// isClassRef reports a named reference whose Java storage is a pre-allocated
// class instance: a generated struct class or a union (SPEC §4.8).
func isClassRef(ref ir.Decl) bool {
	switch ref.(type) {
	case *ir.Struct, *ir.Union:
		return true
	}
	return false
}

// tagJavaType is the storage type of a union's tag: wide enough for the
// declared max in the same signed-storage discipline as enums.
func tagJavaType(max int64) string {
	switch {
	case max <= 0x7f:
		return "byte"
	case max <= 0x7fff:
		return "short"
	case max <= 0x7fffffff:
		return "int"
	}
	return "long"
}

// emitUnion emits a first-class one-of (SPEC §4.8): the <Name>Type tag
// namespace, then the class — the tag beside one pre-allocated arm per
// variant; nothing allocates per value after construction.
func (g *gen) emitUnion(d *ir.Union) {
	typ := tagJavaType(d.Max)
	g.emitTagEnum(d.Name+"Type", typ, variantNames(d), variantDocs(d), d.Max,
		fmt.Sprintf("union %s's tag — None = 0, then each variant in declared order (SPEC §4.8)", d.Name))

	g.bpf("%s", ir.DocComment(d.Doc, "    ", "//"))
	g.bpf("    // %s — at most one of the arms; type says which. Construction is the empty\n", d.Name)
	g.bpf("    // union (None). Every read selection initializes the chosen payload from\n")
	g.bpf("    // construction defaults; unselected arms keep what they last held — the\n")
	g.bpf("    // reused-storage discipline. Consumers read the selected arm only.\n")
	g.bpf("    public static final class %s {\n", d.Name)
	g.bpf("        public %s type = %sType.none;\n", typ, d.Name)
	for _, v := range d.Variants {
		if v.Void() {
			continue // a PAYLOAD-FREE arm has no storage (SPEC §4.8)
		}
		q := g.qualifyType(v.Type)
		g.bpf("        public final %s %s = new %s();\n", q, javaName(v.Name), q)
	}
	g.bpf("    }\n\n")
}

// variantDocs is each arm's `///` block (SPEC §4.1), parallel to
// [variantNames].
func variantDocs(d *ir.Union) []string {
	docs := make([]string, len(d.Variants))
	for i, v := range d.Variants {
		docs[i] = v.Doc
	}
	return docs
}

func variantNames(d *ir.Union) []string {
	names := make([]string, len(d.Variants))
	for i, v := range d.Variants {
		names[i] = ir.GoExportName(v.Name)
	}
	return names
}

func (g *gen) fieldComment(f *ir.Field) string {
	var parts []string
	if f.Type.Kind == ir.TNamed {
		if _, isFlags := f.Type.Ref.(*ir.Flags); isFlags {
			parts = append(parts, fmt.Sprintf("%s — consumed as masks, uint64 storage (SPEC §4.2)", f.Type.Name))
		}
	}
	if f.HasDefault {
		parts = append(parts, "specified default at construction; zero* gives the §5 zero form")
	}
	if f.HasIntRange {
		parts = append(parts, fmt.Sprintf("wire [%s, %s]", f.IntMin, f.IntMax))
	}
	if f.HasFloatRange {
		parts = append(parts, fmt.Sprintf("compressed float [%s, %s] @ %s",
			formatFloat(f.FMin), formatFloat(f.FMax), formatFloat(f.Resolution)))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "; ")
}

// shiftedRaw is a fixed-point whole-unit value scaled to raw storage:
// v << F, with negative values negated around the shift so the arithmetic is
// exact (the same derivation the wire bounds use).
func shiftedRaw(v *big.Int, fracBits uint) *big.Int {
	if v.Sign() < 0 {
		return new(big.Int).Neg(new(big.Int).Lsh(new(big.Int).Neg(v), fracBits))
	}
	return new(big.Int).Lsh(v, fracBits)
}

// ---- integer literal / expression rendering ----

var maxInt32 = big.NewInt(1<<31 - 1)
var minInt32 = big.NewInt(-(1 << 31))
var maxInt64 = big.NewInt(0).SetInt64(1<<63 - 1)
var minInt64 = big.NewInt(0).Lsh(big.NewInt(-1), 63)
var maxUint64Big = new(big.Int).SetUint64(1<<64 - 1)

func fitsInt32(v *big.Int) bool {
	return v.Cmp(minInt32) >= 0 && v.Cmp(maxInt32) <= 0
}

// javaIntLit renders an integer as a Java literal: plain decimal inside
// int32, decimal with the L suffix through int64, and past int64 (through
// uint64) the bit-transparent hex long spelling — Java's only unsigned-64
// literal form. int64 min itself has no decimal spelling (the positive half
// overflows before the minus applies), so it renders as its hex bit pattern.
func javaIntLit(v *big.Int) string {
	if fitsInt32(v) {
		return v.String()
	}
	if v.Cmp(minInt64) == 0 {
		return "0x8000000000000000L"
	}
	if v.Cmp(minInt64) > 0 && v.Cmp(maxInt64) <= 0 {
		return v.String() + "L"
	}
	u := new(big.Int).Set(v)
	if u.Sign() < 0 {
		u.Add(u, new(big.Int).Lsh(big.NewInt(1), 64)) // the 64-bit pattern
	}
	return fmt.Sprintf("0x%xL", u)
}

// narrowLit renders an integer for a narrow-typed context (byte/short/int
// storage): the plain decimal when the value is representable (constant
// narrowing covers assignment), else the bit pattern behind an explicit
// cast — (byte) 200 is -56, the storage pattern of unsigned 200.
func narrowLit(typ string, v *big.Int) string {
	switch typ {
	case "byte":
		if v.IsInt64() && v.Int64() >= -128 && v.Int64() <= 127 {
			return v.String()
		}
		return fmt.Sprintf("(byte) %s", new(big.Int).And(v, big.NewInt(0xff)).String())
	case "short":
		if v.IsInt64() && v.Int64() >= -32768 && v.Int64() <= 32767 {
			return v.String()
		}
		return fmt.Sprintf("(short) %s", new(big.Int).And(v, big.NewInt(0xffff)).String())
	case "int":
		if fitsInt32(v) {
			return v.String()
		}
		return fmt.Sprintf("(int) %s", javaIntLit(new(big.Int).And(v, big.NewInt(0xffffffff))))
	}
	return javaIntLit(v)
}

// renderInt renders an integer expression for a numeric Java context:
// symbolically where every referenced constant is a bare (untyped) schema
// const and every subtree folds inside int32 (so mixed int/long constant
// arithmetic can never overflow Java's int domain); the computed literal
// otherwise. Folding is always correct.
func (g *gen) renderInt(e ast.Expr, folded *big.Int) string {
	if e == nil || ir.ExprHasEnumMax(e) || !g.renderable(e) || !g.exprExact(e) {
		return javaIntLit(folded)
	}
	return g.renderExpr(e)
}

// renderArraySize renders an array dimension: the symbolic form when it
// renders (schema consts that fit int32 — and array dimensions always do,
// or javac refuses loudly), the folded int literal otherwise.
func (g *gen) renderArraySize(e ast.Expr, folded *big.Int) string {
	if e == nil || ir.ExprHasEnumMax(e) || !g.renderable(e) || !g.exprExact(e) {
		return folded.String()
	}
	return g.renderExpr(e)
}

// exprExact reports whether every subtree of e evaluates exactly in Java
// int arithmetic: values inside int32 (int consts and plain literals keep
// the whole expression in the int domain), with / and % on a nonzero
// divisor — Java's truncating division and remainder agree with schema's.
func (g *gen) exprExact(e ast.Expr) bool {
	_, ok := g.intEval(e)
	return ok
}

func (g *gen) intEval(e ast.Expr) (*big.Int, bool) {
	switch e := e.(type) {
	case *ast.IntLit:
		return e.Value, fitsInt32(e.Value)
	case *ast.IdentExpr:
		c, ok := g.unit.Consts[e.Name]
		if !ok || c.IsFloat || c.Int == nil {
			return nil, false
		}
		return c.Int, fitsInt32(c.Int)
	case *ast.ParenExpr:
		return g.intEval(e.X)
	case *ast.UnaryExpr:
		v, ok := g.intEval(e.X)
		if !ok {
			return nil, false
		}
		nv := new(big.Int).Neg(v)
		return nv, fitsInt32(nv)
	case *ast.BinaryExpr:
		x, ok := g.intEval(e.X)
		if !ok {
			return nil, false
		}
		y, ok := g.intEval(e.Y)
		if !ok {
			return nil, false
		}
		v := new(big.Int)
		switch e.Op {
		case "+":
			v.Add(x, y)
		case "-":
			v.Sub(x, y)
		case "*":
			v.Mul(x, y)
		case "/":
			if y.Sign() == 0 {
				return nil, false
			}
			v.Quo(x, y) // Java / truncates toward zero, as schema folds
		case "%":
			if y.Sign() == 0 {
				return nil, false
			}
			v.Rem(x, y) // Java % is the truncated remainder, schema's own
		default:
			return nil, false
		}
		return v, fitsInt32(v)
	}
	return nil, false
}

// renderable: every referenced constant must be a bare (untyped) integer
// schema const — the same renderability rule as the sibling targets, so all
// of them fold identically.
func (g *gen) renderable(e ast.Expr) bool {
	for _, name := range ir.ExprConsts(e) {
		c, ok := g.unit.Consts[name]
		if !ok || c.IsFloat || c.Explicit {
			return false
		}
	}
	return true
}

// renderExpr renders an expression in Java form: constants keep the schema
// spelling in the lowerCamel mapping, qualified where they live in another
// file of the unit.
func (g *gen) renderExpr(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.BinaryExpr:
		return g.renderExpr(e.X) + " " + e.Op + " " + g.renderExpr(e.Y)
	case *ast.UnaryExpr:
		if _, nested := e.X.(*ast.UnaryExpr); nested {
			return "-(" + g.renderExpr(e.X) + ")"
		}
		return "-" + g.renderExpr(e.X)
	case *ast.ParenExpr:
		return "(" + g.renderExpr(e.X) + ")"
	case *ast.IntLit:
		return e.Value.String()
	case *ast.IdentExpr:
		return g.qualify(e.Name, javaName(e.Name))
	}
	return ir.RenderExpr(e) // unreachable for renderable expressions
}

// pairCtor renders a 128-bit value as a pair-constructor expression, each
// lane a bit-transparent long literal.
func pairCtor(signed bool, v *big.Int) string {
	u := new(big.Int).Set(v)
	if u.Sign() < 0 {
		u.Add(u, new(big.Int).Lsh(big.NewInt(1), 128))
	}
	hi := new(big.Int).Rsh(u, 64)
	lo := new(big.Int).And(u, maxUint64Big)
	typ := "UInt128"
	if signed {
		typ = "Int128"
	}
	return fmt.Sprintf("new %s(%s, %s)", typ, javaIntLit(hi), javaIntLit(lo))
}

func formatFloat(v float64) string {
	return formatFloatCore(v, 64)
}

// formatFloat32 renders a float32-precision literal: the shortest form that
// parses to exactly float32(v), so the wire widths agree with
// ir.CompressedFloatBits in every target. Callers append the f suffix.
func formatFloat32(v float64) string {
	return formatFloatCore(v, 32)
}

func formatFloatCore(v float64, bits int) string {
	s := strconv.FormatFloat(v, 'g', -1, bits)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// JAVA SOURCE IS WRITTEN IN THE FORMATTER'S SHAPE (issue #547,
// docs/JAVA-DIALECT.md). make tables-java-clean holds every generated unit to
// google-java-format --aosp at the release make/java.mk pins. Most of the
// emitters' templates write fixed text; the drift they had in common was in
// the parts whose width depends on a name -- the comments, filled to the
// column -- and in a handful of whitespace rules every template broke the
// same way. Canonical applies those rules once, at the end of each emitter,
// rather than in two hundred templates:
//
//   - a Javadoc of plain prose is refilled to the formatter's column limit
//     (one line when it fits, else /** ... */ on their own lines);
//   - a line comment longer than the limit is refilled at its words;
//   - a trailing comment is one space after the code it follows;
//   - a method whose body is one statement written on its signature's line
//     is opened onto three lines;
//   - two statements on one line are two lines;
//   - an array initializer has no space inside its braces;
//   - no blank line before a closing brace, never two blank lines in a row,
//     and a blank line around each member that carries a Javadoc.
//
// It NEVER moves a code line's indentation: a template that indents wrongly
// is a template the gate must see (tools/sabotage/table_java_format_drift.go
// plants exactly that), so the fix for one is in the template.
func Canonical(src []byte) []byte {
	lines := strings.Split(javaElseIf.ReplaceAllString(string(src), "} else if ("), "\n")
	lines = canonJavadocs(lines)
	lines = joinOpenParens(lines)
	lines = joinLoneBraces(lines)
	var out []string
	for _, l := range lines {
		out = append(out, canonLine(l)...)
	}
	out = joinElse(out)
	out = refillInitializers(out)
	out = emptyBlocks(out)
	out = dropUnusedImports(out)
	out = canonBlankLines(out)
	out = memberBlankLines(out)
	return []byte(strings.Join(out, "\n"))
}

const javaColumnLimit = 100

func javaWidth(s string) int { return utf8.RuneCountInString(s) }

func indentOf(s string) string { return s[:len(s)-len(strings.TrimLeft(s, " "))] }

// fill lays words out greedily behind prefix, each line within the limit; a
// word longer than the line goes on a line of its own.
func fill(prefix string, words []string) []string {
	var out []string
	cur := ""
	for _, w := range words {
		if cur == "" {
			cur = prefix + w
			continue
		}
		if javaWidth(cur)+1+javaWidth(w) <= javaColumnLimit {
			cur += " " + w
			continue
		}
		out = append(out, cur)
		cur = prefix + w
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// canonJavadocs refills every Javadoc that is plain prose: no HTML, no block
// or inline tag, no blank line inside. Anything richer is left as written.
func canonJavadocs(lines []string) []string {
	var out []string
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		t := strings.TrimSpace(l)
		if !strings.HasPrefix(t, "/**") {
			out = append(out, l)
			continue
		}
		end := -1
		for j := i; j < len(lines); j++ {
			if strings.Contains(lines[j], "*/") {
				end = j
				break
			}
		}
		if end < 0 || !strings.HasSuffix(strings.TrimSpace(lines[end]), "*/") {
			out = append(out, l)
			continue
		}
		var words []string
		plain := true
		for j := i; j <= end; j++ {
			s := strings.TrimSpace(lines[j])
			if j == end {
				s = strings.TrimSpace(strings.TrimSuffix(s, "*/"))
			}
			if j == i {
				s = strings.TrimPrefix(s, "/**")
			} else {
				s = strings.TrimPrefix(s, "*")
			}
			s = strings.TrimSpace(s)
			if s == "" {
				if j != i && j != end && len(words) > 0 && words[len(words)-1] != "\x00" {
					words = append(words, "\x00")
				}
				continue
			}
			s = strings.TrimPrefix(s, "<p>")
			if strings.ContainsAny(s, "<>@{}") || strings.Contains(s, "/*") {
				plain = false
			}
			words = append(words, strings.Fields(s)...)
		}
		if !plain || len(words) == 0 {
			out = append(out, lines[i:end+1]...)
			i = end
			continue
		}
		ind := indentOf(l)
		var paras [][]string
		cur := []string{}
		for _, w := range words {
			if w == "\x00" {
				paras = append(paras, cur)
				cur = []string{}
				continue
			}
			cur = append(cur, w)
		}
		if len(cur) > 0 {
			paras = append(paras, cur)
		}
		one := ind + "/** " + strings.Join(words, " ") + " */"
		if len(paras) == 1 && javaWidth(one) <= javaColumnLimit {
			out = append(out, one)
		} else {
			out = append(out, ind+"/**")
			for k, p := range paras {
				if k > 0 {
					out = append(out, ind+" *")
					p = append([]string{"<p>" + p[0]}, p[1:]...)
				}
				out = append(out, fill(ind+" * ", p)...)
			}
			out = append(out, ind+" */")
		}
		i = end
	}
	return out
}

var (
	javaArrayInit = regexp.MustCompile(`(= |\] )\{ ([^{};]*) \}`)
	javaTrailing  = regexp.MustCompile(`^( *\S.*?\S)  +(//.*)$`)
)

// canonLine applies the one-line rules; a line may come back as several.
func canonLine(l string) []string {
	t := strings.TrimSpace(l)
	if t == "" {
		return []string{""}
	}
	ind := indentOf(l)
	if strings.HasPrefix(t, "//") {
		if javaWidth(l) <= javaColumnLimit {
			return []string{l}
		}
		return fill(ind+"// ", strings.Fields(strings.TrimPrefix(t, "//")))
	}
	if strings.HasPrefix(t, "*") || strings.HasPrefix(t, "/*") {
		return []string{l}
	}
	l = javaElseIf.ReplaceAllString(l, "} else if (")
	if !strings.ContainsAny(l, "\"'") {
		if m := javaTrailing.FindStringSubmatch(l); m != nil && !strings.Contains(m[1], "//") {
			l = m[1] + " " + m[2]
		}
		l = javaArrayInit.ReplaceAllString(l, "$1{$2}")
		l = strings.ReplaceAll(l, "[]{", "[] {")
		if strings.HasPrefix(t, "for (") {
			l = strings.Replace(l, ";)", "; )", 1)
		}
	}
	if w := wrapCall(l); w != nil {
		return w
	}
	if w := wrapDecl(l); w != nil {
		return w
	}
	if w := wrapBinary(l); w != nil {
		return w
	}
	if w := wrapBracelessIf(l); w != nil {
		return w
	}
	if w := wrapFor(l); w != nil {
		return w
	}
	if w := splitCases(l); w != nil {
		return w
	}
	if w := wrapArray(l); w != nil {
		return w
	}
	if w := wrapShortArray(l); w != nil {
		return w
	}
	if w := expandBraces(l); w != nil {
		return w
	}
	if w := wrapLong(l); w != nil {
		return w
	}
	if strings.HasPrefix(t, "for ") || strings.HasPrefix(t, "for(") || strings.HasSuffix(t, "{") {
		return []string{l}
	}
	if code, _, _ := strings.Cut(t, "//"); !strings.ContainsAny(code, "{}") {
		stmts := splitStatements(t)
		if len(stmts) > 1 {
			var out []string
			for _, s := range stmts {
				out = append(out, ind+s)
			}
			return out
		}
	}
	return []string{l}
}

// splitStatements cuts s at each `; ` outside a literal, a paren, a bracket
// and a brace; a trailing comment stays with the statement it follows.
func splitStatements(s string) []string {
	var out []string
	depth := 0
	var quote byte
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '/':
			if i+1 < len(s) && (s[i+1] == '/' || s[i+1] == '*') {
				i = len(s)
			}
		case ';':
			if depth == 0 && i+1 < len(s) && s[i+1] == ' ' && i+2 < len(s) && s[i+2] != '/' {
				out = append(out, strings.TrimSpace(s[start:i+1]))
				start = i + 1
			}
		}
	}
	return append(out, strings.TrimSpace(s[start:]))
}

// canonBlankLines: no run of blank lines, none before a closing brace, and a
// blank line before and after each member that carries a Javadoc (the
// formatter's rule for class bodies; a member opening its class keeps
// whatever was written).
func canonBlankLines(lines []string) []string {
	var out []string
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		t := strings.TrimSpace(l)
		if t == "" {
			j := i
			for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
				j++
			}
			if j < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[j]), "}") {
				i = j - 1
				continue
			}
			if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
				continue
			}
			out = append(out, l)
			continue
		}
		if strings.HasPrefix(t, "/**") && len(out) > 0 {
			prev := strings.TrimSpace(out[len(out)-1])
			if prev != "" && !strings.HasSuffix(prev, "{") && !strings.HasPrefix(prev, "@") && !strings.HasPrefix(prev, "//") && !strings.HasPrefix(prev, "*") && !strings.HasSuffix(prev, "*/") {
				out = append(out, "")
			}
		}
		out = append(out, l)
		// the end of a Javadoc'd one-line field: a blank line after it.
		if strings.HasSuffix(t, ";") && len(out) >= 2 && strings.HasSuffix(strings.TrimSpace(out[len(out)-2]), "*/") && strings.HasPrefix(strings.TrimSpace(javadocStart(out, len(out)-2)), "/**") {
			if i+1 < len(lines) {
				next := strings.TrimSpace(lines[i+1])
				if next != "" && !strings.HasPrefix(next, "}") && !strings.HasPrefix(next, "/**") {
					out = append(out, "")
				}
			}
		}
	}
	return out
}

// javadocStart is the first line of the comment that ends at out[end].
func javadocStart(out []string, end int) string {
	for k := end; k >= 0; k-- {
		t := strings.TrimSpace(out[k])
		if strings.HasPrefix(t, "/*") {
			return t
		}
	}
	return ""
}

var javaCallee = regexp.MustCompile(`^(?:new )?[A-Za-z_][\w.<>]*$`)

// wrapCall breaks one statement wider than the column limit the way the
// formatter does when the statement is a single call: after the `=` of an
// assignment first (the continuation is eight columns deeper), then inside
// the call's parentheses -- every argument on one line if they fit, else one
// argument a line. A statement of any other shape is left as written.
func wrapCall(l string) []string {
	ind := indentOf(l)
	t := strings.TrimSpace(l)
	if javaWidth(l) <= javaColumnLimit || !strings.HasSuffix(t, ");") || strings.Contains(t, "//") || strings.Contains(t, "/*") {
		return nil
	}
	open := strings.IndexByte(t, '(')
	if open < 0 {
		return nil
	}
	head := t[:open]
	lhs, callee := "", head
	switch {
	case strings.HasPrefix(head, "return "):
		lhs, callee = "return ", strings.TrimPrefix(head, "return ")
	case strings.Contains(head, " = "):
		k := strings.LastIndex(head, " = ")
		lhs, callee = head[:k+3], head[k+3:]
	}
	if !javaCallee.MatchString(callee) {
		return nil
	}
	args, ok := splitArgs(t[open+1 : len(t)-2])
	if !ok {
		return nil
	}
	call := callee + "(" + strings.Join(args, ", ") + ");"
	inner := ind
	var out []string
	if strings.HasSuffix(lhs, " = ") {
		inner = ind + "        "
		if javaWidth(inner+call) <= javaColumnLimit {
			return []string{ind + strings.TrimSuffix(lhs, " "), inner + call}
		}
		out = append(out, ind+strings.TrimSuffix(lhs, " "), inner+callee+"(")
	} else {
		out = append(out, ind+lhs+callee+"(")
	}
	argInd := inner + "        "
	if one := argInd + strings.Join(args, ", ") + ");"; javaWidth(one) <= javaColumnLimit {
		return append(out, one)
	}
	if allShort(args) {
		toks := make([]string, len(args))
		for i, a := range args {
			toks[i] = a + ","
		}
		toks[len(toks)-1] = args[len(args)-1] + ");"
		return append(out, fill(argInd, toks)...)
	}
	for i, a := range args {
		end := ","
		if i == len(args)-1 {
			end = ");"
		}
		if javaWidth(argInd+a+end) > javaColumnLimit && !(strings.HasPrefix(a, "\"") && strings.HasSuffix(a, "\"")) {
			return nil
		}
		out = append(out, argInd+a+end)
	}
	return out
}

// splitArgs cuts an argument list at its top-level commas; ok is false when
// the parentheses do not balance inside it (the call was not the whole
// statement).
func splitArgs(s string) ([]string, bool) {
	var out []string
	depth := 0
	var quote byte
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth < 0 {
				return nil, false
			}
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	if depth != 0 || quote != 0 {
		return nil, false
	}
	if rest := strings.TrimSpace(s[start:]); rest != "" || len(out) > 0 {
		out = append(out, rest)
	}
	return out, true
}

var (
	javaDecl = regexp.MustCompile(`^((?:(?:public|private|protected|static|final|synchronized|abstract|default) )*[A-Za-z_][\w.<>\[\], ?]* [a-zA-Z_]\w*)\((.*)\)((?: throws [\w., ]+)?) \{$`)
)

// wrapDecl breaks a method or constructor signature wider than the limit:
// the parameters on one line eight columns deeper if they fit, else one a
// line.
func wrapDecl(l string) []string {
	if javaWidth(l) <= javaColumnLimit {
		return nil
	}
	ind := indentOf(l)
	m := javaDecl.FindStringSubmatch(strings.TrimSpace(l))
	if m == nil {
		return nil
	}
	params, ok := splitArgs(m[2])
	if !ok || len(params) == 0 {
		return nil
	}
	out := []string{ind + m[1] + "("}
	pin := ind + "        "
	if one := pin + strings.Join(params, ", ") + ")" + m[3] + " {"; javaWidth(one) <= javaColumnLimit {
		return append(out, one)
	}
	for i, p := range params {
		end := ","
		if i == len(params)-1 {
			end = ")" + m[3] + " {"
		}
		out = append(out, pin+p+end)
	}
	return out
}

// parenDepth is the count of open parentheses a code line leaves open.
func parenDepth(s string) int {
	depth := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case '(':
			depth++
		case ')':
			depth--
		case '/':
			if i+1 < len(s) && (s[i+1] == '/' || s[i+1] == '*') {
				return depth
			}
		}
	}
	return depth
}

// joinOpenParens rejoins a parenthesised list a template broke by hand, so
// the formatter's own breaks can be laid down in its place.
func joinOpenParens(lines []string) []string {
	var out []string
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "//") || strings.HasPrefix(t, "*") || strings.HasPrefix(t, "/*") || strings.Contains(t, "//") {
			out = append(out, l)
			continue
		}
		d := parenDepth(l)
		j := i
		joined := l
		for d > 0 && j+1 < len(lines) {
			n := strings.TrimSpace(lines[j+1])
			if n == "" || strings.Contains(n, "//") || strings.HasPrefix(n, "*") || strings.HasPrefix(n, "/*") {
				break
			}
			if step := len(indentOf(lines[j+1])) - len(indentOf(l)); step > 0 && step%8 == 0 {
				break
			}
			j++
			if strings.HasSuffix(joined, "(") {
				joined += n
			} else {
				joined += " " + n
			}
			d = parenDepth(joined)
		}
		if d == 0 && j > i {
			out = append(out, joined)
			i = j
			continue
		}
		out = append(out, l)
	}
	return out
}

var javaCaseLabels = regexp.MustCompile(`^( *)((?:case [^:"']+:|default:)(?: (?:case [^:"']+:|default:))*) (.+)$`)

// splitCases puts each case label of a switch on its own line and the
// statement under them four columns deeper, as the formatter does.
func splitCases(l string) []string {
	m := javaCaseLabels.FindStringSubmatch(l)
	if m == nil || strings.HasPrefix(m[3], "case ") || strings.HasPrefix(m[3], "default:") {
		return nil
	}
	var out []string
	labels := strings.Fields(strings.ReplaceAll(strings.ReplaceAll(m[2], "case ", "case\x00"), "default:", "default:"))
	for _, lb := range labels {
		out = append(out, m[1]+strings.ReplaceAll(lb, "\x00", " "))
	}
	return append(out, canonLine(m[1]+"    "+m[3])...)
}

// expandBraces opens every block a template wrote on one line -- an if and
// its else chain, a loop, a method, a one-member interface -- onto lines of
// their own, four columns a level, with a trailing comment kept after the
// brace it followed. An array initializer and a lambda are left alone.
func expandBraces(l string) []string {
	t := strings.TrimSpace(l)
	if !strings.Contains(t, "{ ") || !strings.Contains(t, " }") || strings.Contains(t, "->") || strings.HasPrefix(t, "@") {
		return nil
	}
	ind := indentOf(l)
	var out []string
	depth, paren := 0, 0
	var quote byte
	cur := ""
	if strings.HasPrefix(t, "} ") {
		cur, t = "} ", t[2:]
	}
	pad := func() string { return ind + strings.Repeat("    ", depth) }
	flush := func() {
		if s := strings.TrimSpace(cur); s != "" {
			out = append(out, pad()+s)
		}
		cur = ""
	}
	for i := 0; i < len(t); i++ {
		c := t[i]
		if quote != 0 {
			cur += string(c)
			if c == '\\' && i+1 < len(t) {
				i++
				cur += string(t[i])
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch {
		case c == '"' || c == '\'':
			quote = c
			cur += string(c)
		case c == '(' || c == '[':
			paren++
			cur += string(c)
		case c == ')' || c == ']':
			paren--
			cur += string(c)
		case c == '/' && i+1 < len(t) && t[i+1] == '/':
			// a trailing comment: it stays on the line it closes.
			comment := strings.TrimSpace(t[i:])
			if s := strings.TrimSpace(cur); s != "" {
				out = append(out, pad()+s+" "+comment)
			} else if len(out) > 0 {
				out[len(out)-1] += " " + comment
			} else {
				return nil
			}
			cur = ""
			i = len(t)
		case paren == 0 && c == '{':
			prev := strings.TrimSpace(cur)
			if strings.HasSuffix(prev, "=") || strings.HasSuffix(prev, "]") || strings.HasSuffix(prev, ",") || prev == "" {
				return nil
			}
			if i+1 < len(t) && t[i+1] == '}' {
				cur += "{}"
				i++
				continue
			}
			out = append(out, pad()+prev+" {")
			cur = ""
			depth++
		case paren == 0 && c == ';':
			cur += ";"
			flush()
		case paren == 0 && c == '}':
			flush()
			depth--
			if depth < 0 {
				return nil
			}
			rest := strings.TrimSpace(t[i+1:])
			if strings.HasPrefix(rest, "else") || strings.HasPrefix(rest, "catch") || strings.HasPrefix(rest, "finally") || strings.HasPrefix(rest, "while") {
				cur = "} "
				i = strings.Index(t[i+1:], rest[:1]) + i
				continue
			}
			out = append(out, pad()+"}")
		default:
			cur += string(c)
		}
	}
	flush()
	if depth == 1 && strings.HasSuffix(t, "{") {
		depth = 0
	}
	if depth != 0 || quote != 0 || paren != 0 || len(out) < 2 {
		return nil
	}
	var res []string
	for _, o := range out {
		res = append(res, canonLine(o)...)
	}
	return res
}

var javaLongArray = regexp.MustCompile(`^( *)(.+) = (new [\w.]+\[\] )\{(.+)\};$`)

// wrapArray breaks an array initializer wider than the limit: after the `=`,
// the initializer eight columns deeper, one element a line four deeper still.
func wrapArray(l string) []string {
	if javaWidth(l) <= javaColumnLimit || strings.Contains(l, "//") {
		return nil
	}
	m := javaLongArray.FindStringSubmatch(l)
	if m == nil {
		return nil
	}
	elems, ok := splitArgs(m[4])
	if !ok || len(elems) == 0 {
		return nil
	}
	ind := m[1]
	out := []string{ind + m[2] + " =", ind + "        " + m[3] + "{"}
	out = append(out, fillElements(ind+"            ", elems, false)...)
	return append(out, ind+"        };")
}

// dropUnusedImports removes a single-type import whose simple name the file
// never uses, which the formatter removes too.
func dropUnusedImports(lines []string) []string {
	body := strings.Builder{}
	for _, l := range lines {
		if !strings.HasPrefix(l, "import ") {
			body.WriteString(l)
			body.WriteByte('\n')
		}
	}
	text := body.String()
	var out []string
	for _, l := range lines {
		if strings.HasPrefix(l, "import ") && !strings.HasPrefix(l, "import static ") && strings.HasSuffix(l, ";") && !strings.HasSuffix(l, ".*;") {
			path := strings.TrimSuffix(strings.TrimPrefix(l, "import "), ";")
			name := path[strings.LastIndexByte(path, '.')+1:]
			if !regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`).MatchString(text) {
				continue
			}
		}
		out = append(out, l)
	}
	return out
}

// joinElse puts an else, catch or finally on the line of the brace it
// follows.
func joinElse(lines []string) []string {
	var out []string
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if (strings.HasPrefix(t, "else") || strings.HasPrefix(t, "catch") || strings.HasPrefix(t, "finally")) && len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "}" {
			t = javaElseSpaces.ReplaceAllString(t, "else if (")
			out[len(out)-1] += " " + t
			continue
		}
		out = append(out, l)
	}
	return out
}

// javaBinaryOps, loosest first: a statement wider than the limit breaks
// before every operator of the loosest kind its expression uses at the top.
var javaBinaryOps = [][]string{{"||"}, {"&&"}, {"|"}, {"^"}, {"&"}, {"==", "!="}, {"<=", ">=", "<", ">"}, {"<<", ">>>", ">>"}, {"+", "-"}, {"*", "/", "%"}}

// splitBinary cuts expr before each top-level operator of the loosest kind it
// uses; ok is false for a ternary or when no operator is at the top.
func splitBinary(expr string) ([]string, bool) {
	type cut struct {
		at int
		op string
	}
	var found [][]cut
	found = make([][]cut, len(javaBinaryOps))
	depth := 0
	var quote byte
	for i := 0; i < len(expr); i++ {
		c := expr[i]
		if quote != 0 {
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
			continue
		case '(', '[', '{':
			depth++
			continue
		case ')', ']', '}':
			depth--
			continue
		case '?':
			if depth == 0 {
				return nil, false
			}
		}
		if depth != 0 || c != ' ' || i == 0 {
			continue
		}
		for lvl, ops := range javaBinaryOps {
			for _, op := range ops {
				if strings.HasPrefix(expr[i+1:], op+" ") {
					found[lvl] = append(found[lvl], cut{i, op})
					goto next
				}
			}
		}
	next:
	}
	for _, cuts := range found {
		if len(cuts) == 0 {
			continue
		}
		var parts []string
		start := 0
		for _, c := range cuts {
			parts = append(parts, strings.TrimSpace(expr[start:c.at]))
			start = c.at + 1
		}
		return append(parts, strings.TrimSpace(expr[start:])), true
	}
	return nil, false
}

// wrapBinary breaks a return or an if condition wider than the limit before
// each operator of its loosest kind, eight columns in.
func wrapBinary(l string) []string {
	if javaWidth(l) <= javaColumnLimit || strings.Contains(l, "//") {
		return nil
	}
	ind := indentOf(l)
	t := strings.TrimSpace(l)
	switch {
	case strings.HasPrefix(t, "return ") && strings.HasSuffix(t, ";"):
		if e := t[len("return ") : len(t)-1]; parenDepth(e) == 0 {
			return breakCond(ind, "return ", e, ";")
		}
	case strings.HasPrefix(t, "if (") && strings.HasSuffix(t, ") {") && matchParen(t, 3) == len(t)-3:
		return breakCond(ind, "if (", t[len("if ("):len(t)-3], ") {")
	}
	return nil
}

// memberBlankLines puts a blank line between two members of a class body
// whenever either of them is not a field -- a method, a constructor, a
// nested type -- which is the formatter's rule; fields side by side keep what
// was written.
func memberBlankLines(lines []string) []string {
	type frame struct {
		class  bool
		member int // the indent of the members a class body holds
	}
	var stack []frame
	inClass := func() bool { return len(stack) > 0 && stack[len(stack)-1].class }
	atMember := func(l string) bool { return inClass() && len(indentOf(l)) == stack[len(stack)-1].member }
	isHeader := func(t string) bool {
		return strings.HasSuffix(t, "{") && (strings.Contains(t, "(") || javaTypeHeader.MatchString(t)) ||
			strings.HasSuffix(t, "(") && !strings.Contains(t, "=")
	}
	var out []string
	lastMemberEnd := "" // "field", "block" or ""
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if atMember(l) && t != "" && !strings.HasPrefix(t, "}") && len(out) > 0 {
			prev := strings.TrimSpace(out[len(out)-1])
			if prev != "" && !strings.HasSuffix(prev, "{") {
				// the first code line of the member this line starts
				code := t
				for k := i; k < len(lines) && k < i+40; k++ {
					c := strings.TrimSpace(lines[k])
					if c == "" || strings.HasPrefix(c, "//") || strings.HasPrefix(c, "/*") || strings.HasPrefix(c, "*") || strings.HasPrefix(c, "@") {
						continue
					}
					code = c
					break
				}
				startsMember := !strings.HasPrefix(prev, "//") && !strings.HasPrefix(prev, "*") && !strings.HasSuffix(prev, "*/") && !strings.HasPrefix(prev, "@") &&
					(lastMemberEnd == "block" || lastMemberEnd == "field" && isHeader(code))
				if startsMember {
					out = append(out, "")
				}
			}
		}
		out = append(out, l)
		// track the braces this line opens and closes
		code, _, _ := strings.Cut(t, "//")
		code = strings.TrimSpace(code)
		if strings.HasPrefix(code, "}") && len(stack) > 0 {
			stack = stack[:len(stack)-1]
			if atMember(l) {
				lastMemberEnd = "block"
			}
		}
		if strings.HasSuffix(code, "{") && !strings.HasPrefix(t, "*") && !strings.HasPrefix(t, "//") {
			stack = append(stack, frame{class: javaTypeHeader.MatchString(code), member: len(indentOf(l)) + 4})
			lastMemberEnd = ""
		} else if atMember(l) && strings.HasSuffix(code, ";") && !strings.HasPrefix(t, "*") {
			lastMemberEnd = "field"
		}
	}
	return out
}

var javaTypeHeader = regexp.MustCompile(`\b(class|interface|record) [A-Z]\w*`)

var (
	javaShortArray = regexp.MustCompile(`^( *)(.+) = \{(.+)\};$`)
	javaElseIf     = regexp.MustCompile(`\} else +if \(`)
	javaElseSpaces = regexp.MustCompile(`^else +if \(`)
)

// fillElements lays array elements out the formatter's way: numeric
// literals filled to the column limit, anything else on one line when the
// whole list fits there and one element a line when it does not.
func fillElements(ind string, elems []string, trailingComma bool) []string {
	numeric := allShort(elems)
	toks := make([]string, len(elems))
	for i, e := range elems {
		toks[i] = e
		if i < len(elems)-1 || trailingComma {
			toks[i] += ","
		}
	}
	if !numeric {
		if one := ind + strings.Join(toks, " "); javaWidth(one) <= javaColumnLimit {
			return []string{one}
		}
		out := make([]string, len(toks))
		for i, t := range toks {
			out[i] = ind + t
		}
		return out
	}
	return fill(ind, toks)
}

// wrapShortArray breaks `x = {a, b, c};` wider than the limit: the braces
// stay on the declaration and its closing line, the elements four columns in.
func wrapShortArray(l string) []string {
	if javaWidth(l) <= javaColumnLimit || strings.Contains(l, "//") {
		return nil
	}
	m := javaShortArray.FindStringSubmatch(l)
	if m == nil || strings.Contains(m[2], "(") {
		return nil
	}
	elems, ok := splitArgs(m[3])
	if !ok || len(elems) == 0 {
		return nil
	}
	out := []string{m[1] + m[2] + " = {"}
	out = append(out, fillElements(m[1]+"    ", elems, false)...)
	return append(out, m[1]+"};")
}

// refillInitializers re-lays the element lines of a multi-line array
// initializer (`= {` or `new T[] {` closing on its own `};`) whose elements
// are one or more a line, in the formatter's shape.
func refillInitializers(lines []string) []string {
	var out []string
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		t := strings.TrimSpace(l)
		if !(strings.HasSuffix(t, "= {") || strings.HasSuffix(t, "[] {")) || strings.HasPrefix(t, "//") {
			out = append(out, l)
			continue
		}
		ind := indentOf(l)
		j := i + 1
		var elems []string
		ok := true
		trailing := false
		elemInd := ""
		for ; j < len(lines); j++ {
			e := strings.TrimSpace(lines[j])
			if e == "};" || e == "}" || e == "});" {
				break
			}
			if e == "" || strings.Contains(e, "//") || strings.Contains(e, "/*") || strings.HasSuffix(e, "{") {
				ok = false
				break
			}
			if elemInd == "" {
				elemInd = indentOf(lines[j])
			}
			parts, pok := splitArgs(strings.TrimSuffix(e, ","))
			if !pok {
				ok = false
				break
			}
			trailing = strings.HasSuffix(e, ",")
			elems = append(elems, parts...)
		}
		if !ok || j >= len(lines) || len(elems) == 0 || len(indentOf(lines[j])) != len(ind) || elemInd != ind+"    " {
			out = append(out, l)
			continue
		}
		out = append(out, l)
		out = append(out, fillElements(ind+"    ", elems, trailing)...)
		out = append(out, lines[j])
		i = j
	}
	return out
}

// emptyBlocks closes a block with nothing in it on the line that opened it.
func emptyBlocks(lines []string) []string {
	var out []string
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if i+1 < len(lines) && strings.HasSuffix(l, "{") && strings.TrimSpace(lines[i+1]) == "}" && len(indentOf(lines[i+1])) <= len(indentOf(l)) && !strings.HasSuffix(strings.TrimSpace(l), "= {") && !strings.HasSuffix(l, "[] {") {
			out = append(out, l+"}")
			i++
			continue
		}
		out = append(out, l)
	}
	return out
}

// matchParen is the index of the parenthesis closing the one at s[open].
func matchParen(s string, open int) int {
	depth := 0
	var quote byte
	for i := open; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// wrapBracelessIf breaks `if (cond) statement;` wider than the limit: the
// statement four columns in on a line of its own, and the condition broken
// at its loosest operator when it is still too wide.
func wrapBracelessIf(l string) []string {
	if javaWidth(l) <= javaColumnLimit || strings.Contains(l, "//") {
		return nil
	}
	ind := indentOf(l)
	t := strings.TrimSpace(l)
	if !strings.HasPrefix(t, "if (") || !strings.HasSuffix(t, ";") {
		return nil
	}
	end := matchParen(t, 3)
	if end < 0 || end+2 >= len(t) {
		return nil
	}
	cond, stmt := t[4:end], strings.TrimSpace(t[end+1:])
	if strings.HasPrefix(stmt, "{") {
		return nil
	}
	body := ind + "    " + stmt
	if head := ind + "if (" + cond + ")"; javaWidth(head) <= javaColumnLimit {
		return []string{head, body}
	}
	if w := breakCond(ind, "if (", cond, ")"); w != nil {
		return append(w, body)
	}
	// `if (!call(args)) stmt;`: the arguments on one line eight columns in
	c := strings.TrimPrefix(cond, "!")
	if open := strings.IndexByte(c, '('); open > 0 && matchParen(c, open) == len(c)-1 && javaCallee.MatchString(c[:open]) {
		if args, ok := splitArgs(c[open+1 : len(c)-1]); ok {
			first := ind + "if (" + cond[:len(cond)-len(c)] + c[:open+1]
			one := ind + "        " + strings.Join(args, ", ") + "))"
			if javaWidth(one) <= javaColumnLimit {
				return []string{first, one, body}
			}
			out := []string{first}
			for i, a := range args {
				if i < len(args)-1 {
					out = append(out, ind+"        "+a+",")
				} else {
					out = append(out, ind+"        "+a+")) "+strings.TrimSpace(body))
				}
			}
			for _, o := range out {
				if javaWidth(o) > javaColumnLimit {
					return nil
				}
			}
			return out
		}
	}
	return nil
}

// joinLoneBraces puts a `{` a template wrote on a line of its own back on the
// line it opens.
func joinLoneBraces(lines []string) []string {
	var out []string
	for _, l := range lines {
		if strings.TrimSpace(l) == "{" && len(out) > 0 {
			prev := strings.TrimSpace(out[len(out)-1])
			if prev != "" && len(indentOf(l)) == len(indentOf(out[len(out)-1])) && !strings.HasSuffix(prev, ";") && !strings.HasSuffix(prev, "{") && !strings.HasSuffix(prev, "}") && !strings.HasSuffix(prev, ",") && !strings.HasSuffix(prev, ":") && !strings.Contains(prev, "//") && !strings.HasPrefix(prev, "*") && !strings.HasSuffix(prev, "*/") {
				out[len(out)-1] += " {"
				continue
			}
		}
		out = append(out, l)
	}
	return out
}

// topAssign is the index of a statement's top-level ` = `, or -1.
func topAssign(s string) int {
	depth := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '=':
			if depth == 0 && i > 0 && s[i-1] == ' ' && i+1 < len(s) && s[i+1] == ' ' {
				return i - 1
			}
		}
	}
	return -1
}

// wrapLong is the last resort for a statement wider than the limit: a
// trailing comment that pushed it over moves onto its own line above, and an
// assignment whose right side fits on one line eight columns in breaks after
// its `=`; a cast of a call breaks the formatter's way beneath that.
func wrapLong(l string) []string {
	if javaWidth(l) <= javaColumnLimit {
		return nil
	}
	ind := indentOf(l)
	t := strings.TrimSpace(l)
	if k := strings.Index(t, " // "); k > 0 && !strings.ContainsAny(t[:k], "\"'") {
		code, comment := t[:k], t[k+1:]
		if javaWidth(ind+code) <= javaColumnLimit && javaWidth(ind+comment) <= javaColumnLimit {
			return []string{ind + comment, ind + code}
		}
		return nil
	}
	if strings.Contains(t, "//") || !strings.HasSuffix(t, ";") {
		return nil
	}
	k := topAssign(t)
	if k < 0 {
		return nil
	}
	lhs, rhs := t[:k], t[k+3:]
	inner := ind + "        "
	if javaWidth(inner+rhs) <= javaColumnLimit {
		return []string{ind + lhs + " =", inner + rhs}
	}
	if w := wrapChain(ind, lhs, rhs); w != nil {
		return w
	}
	if m := javaCastCall.FindStringSubmatch(rhs); m != nil {
		args, ok := splitArgs(m[3])
		if !ok {
			return nil
		}
		out := []string{ind + lhs + " =", inner + m[1], inner + "        " + m[2] + "("}
		argInd := inner + "                "
		if one := argInd + strings.Join(args, ", ") + ");"; javaWidth(one) <= javaColumnLimit {
			return append(out, one)
		}
		for i, a := range args {
			end := ","
			if i == len(args)-1 {
				end = ");"
			}
			if javaWidth(argInd+a+end) > javaColumnLimit {
				return nil
			}
			out = append(out, argInd+a+end)
		}
		return out
	}
	return nil
}

var javaCastCall = regexp.MustCompile(`^(\([\w.<>\[\]]+\)) ([A-Za-z_][\w.]*)\((.*)\);$`)

// allShort: the formatter fills a list (arguments or array elements) only
// when every item is shorter than ten columns; otherwise a broken list is one
// item a line.
func allShort(items []string) bool {
	for _, a := range items {
		if javaWidth(a) >= 10 {
			return false
		}
	}
	return len(items) > 0
}

// breakCond lays a condition out from `first` (the line holding its opening)
// with its operands below at ind+8, recursing into a parenthesised operand
// still too wide; tail closes the last line.
func breakCond(ind, head, expr, tail string) []string {
	if one := ind + head + expr + tail; javaWidth(one) <= javaColumnLimit {
		return []string{one}
	}
	parts, ok := splitBinary(expr)
	if !ok {
		return nil
	}
	var out []string
	for k, p := range parts {
		lead, lineInd := head, ind
		if k > 0 {
			lead, lineInd = "", ind+"        "
		}
		end := ""
		if k == len(parts)-1 {
			end = tail
		}
		if line := lineInd + lead + p + end; javaWidth(line) <= javaColumnLimit {
			out = append(out, line)
			continue
		}
		// `&& (a || b)`: break inside the parentheses, eight columns deeper.
		op, rest, _ := strings.Cut(p, " ")
		if k == 0 {
			op, rest = "", p
		}
		pre := lead
		if op != "" {
			pre += op + " "
		}
		var sub []string
		if strings.HasPrefix(rest, "(") && matchParen(rest, 0) == len(rest)-1 {
			sub = breakCond(lineInd, pre+"(", rest[1:len(rest)-1], ")"+end)
		} else {
			// an operand of a tighter kind: broken at its own operators
			sub = breakCond(lineInd, pre, rest, end)
		}
		if sub == nil {
			return nil
		}
		out = append(out, sub...)
	}
	return out
}

// wrapFor breaks a for header wider than the limit at its semicolons.
func wrapFor(l string) []string {
	if javaWidth(l) <= javaColumnLimit || strings.Contains(l, "//") {
		return nil
	}
	ind := indentOf(l)
	t := strings.TrimSpace(l)
	if !strings.HasPrefix(t, "for (") || !strings.HasSuffix(t, ") {") {
		return nil
	}
	parts := strings.Split(t[5:len(t)-3], "; ")
	if len(parts) != 3 {
		return nil
	}
	out := []string{ind + "for (" + parts[0] + ";", ind + "        " + parts[1] + ";", ind + "        " + parts[2] + ") {"}
	if parts[2] == "" || parts[2] == " " {
		out = []string{ind + "for (" + parts[0] + ";", ind + "        " + parts[1] + "; ) {"}
	}
	for _, o := range out {
		if javaWidth(o) > javaColumnLimit {
			return nil
		}
	}
	return out
}

// wrapChain breaks the right side of an assignment that is a chain of two or
// more method calls before each call, sixteen columns in.
func wrapChain(ind, lhs, rhs string) []string {
	var segs []string
	depth, start := 0, 0
	var quote byte
	for i := 0; i < len(rhs); i++ {
		c := rhs[i]
		if quote != 0 {
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case '.':
			if depth == 0 {
				segs = append(segs, rhs[start:i])
				start = i + 1
			}
		}
	}
	segs = append(segs, rhs[start:])
	// the receiver keeps its first dereference (`value.reach`); every later
	// one, a field or a call, starts a line of its own.
	recv, k := segs[0], 1
	if !strings.HasPrefix(recv, "new ") && !strings.Contains(recv, "(") && len(segs) > 2 && !strings.Contains(segs[1], "(") {
		recv, k = segs[0]+"."+segs[1], 2
	}
	calls := 0
	for _, sg := range segs[k:] {
		if strings.Contains(sg, "(") {
			calls++
		}
	}
	if calls < 2 {
		return nil
	}
	out := []string{ind + lhs + " =", ind + "        " + recv}
	for _, s := range segs[k:] {
		out = append(out, ind+"                ."+s)
	}
	out[len(out)-1] = strings.TrimSuffix(out[len(out)-1], ";") + ";"
	for _, o := range out {
		if javaWidth(o) > javaColumnLimit {
			return nil
		}
	}
	return out
}

// CodeTokens is src with its comments and every run of whitespace removed:
// what Canonical must leave exactly as it found it (an unused import aside).
func CodeTokens(src string) string {
	var b strings.Builder
	var quote byte
	for i := 0; i < len(src); i++ {
		c := src[i]
		if quote != 0 {
			b.WriteByte(c)
			if c == '\\' && i+1 < len(src) {
				i++
				b.WriteByte(src[i])
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch {
		case c == '"' || c == '\'':
			quote = c
			b.WriteByte(c)
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return b.String()
			}
			i += end + 3
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// CommentText is the words of src's comments in order, with the comment
// markers, the Javadoc paragraph tags and every run of whitespace removed:
// the other thing Canonical must leave as it found it.
func CommentText(src string) string {
	var b strings.Builder
	var quote byte
	for i := 0; i < len(src); i++ {
		c := src[i]
		if quote != 0 {
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch {
		case c == '"' || c == '\'':
			quote = c
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			end := strings.IndexByte(src[i:], '\n')
			if end < 0 {
				end = len(src) - i
			}
			b.WriteString(src[i+2 : i+end])
			i += end
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return b.String()
			}
			body := src[i+2 : i+2+end]
			for _, l := range strings.Split(body, "\n") {
				l = strings.TrimSpace(l)
				l = strings.TrimPrefix(l, "*")
				l = strings.TrimPrefix(strings.TrimSpace(l), "<p>")
				b.WriteString(l)
			}
			i += end + 3
		}
	}
	return strings.Join(strings.Fields(b.String()), "")
}
