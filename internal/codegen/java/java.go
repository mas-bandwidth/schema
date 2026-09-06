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
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/ast"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Generate returns basename.java -> file contents for every file of the
// unit, plus Int128.java and UInt128.java when the unit has 128-bit storage.
func Generate(u *ir.Unit) (map[string][]byte, error) {
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
		out["Int128.java"] = emitInt128Support(u)
		out["UInt128.java"] = emitUInt128Support(u)
	}
	for _, f := range u.Files {
		g := &gen{unit: u, file: f, home: f.Base == home, bulkBytes: bulk}
		g.emitFile()
		out[f.Base+".java"] = g.assemble()
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
func unitNeeds128(u *ir.Unit) bool {
	for _, st := range u.Structs {
		for _, f := range st.Fields {
			if is128(f.Type) {
				return true
			}
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
func (g *gen) emitTagEnum(name, typ string, members []string, max int64, comment string) {
	g.bpf("    // %s: %s\n", name, comment)
	g.bpf("    public static final class %s {\n", name)
	g.bpf("        private %s() {}\n\n", name)
	g.bpf("        public static final %s none = 0;\n", typ)
	for i, m := range members {
		g.bpf("        public static final %s %s = %s;\n", typ, javaName(m), narrowLit(typ, big.NewInt(int64(i+1))))
	}
	g.bpf("        // the exported extent (SPEC §4.2)\n")
	g.bpf("        public static final %s max = %s;\n", typ, narrowLit(typ, big.NewInt(max)))
	g.bpf("    }\n\n")
}

func (g *gen) emitEnum(d *ir.Enum) {
	typ := enumJavaType(d.StorageBits)
	g.bpf("    // %s — None = 0 implicit, variants dense from 1, wire range [0, %d] (SPEC §4.2);\n", d.Name, d.Max)
	g.bpf("    // an int-constant namespace — the Java translation of the family's integer-\n")
	g.bpf("    // backed enums: storage must hold every wire-legal value, and | max = ...\n")
	g.bpf("    // headroom values have no Java enum member to be\n")
	g.bpf("    public static final class %s {\n", d.Name)
	g.bpf("        private %s() {}\n\n", d.Name)
	g.bpf("        public static final %s none = 0;\n", typ)
	for i, v := range d.Variants {
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
	g.bpf("    // %s — one bit per variant, consumed as masks; flags-typed fields store\n", d.Name)
	g.bpf("    // uint64 in every target — a bit-transparent long here — wire %d bits\n", d.WireBits)
	g.bpf("    // (SPEC §4.2). Mask names are the family's flat spelling, lowerCamel.\n")
	for i, v := range d.Variants {
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
	if len(d.Tags) > 0 {
		g.bpf("    // type %s [%s] — tags are user-chosen and inert in v1 (SPEC §4.2, Type tags)\n", d.Name, strings.Join(d.Tags, ", "))
	} else {
		g.bpf("    // type %s\n", d.Name)
	}
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
	case f.Type.Kind == ir.TString:
		g.bpf("        // string(%s): max length, used length beside it (SPEC §4.7)\n", ir.RenderExpr(f.Type.SizeExpr))
		g.bpf("        public final byte[] %s = new byte[%s];\n", name, g.renderArraySize(f.Type.SizeExpr, big.NewInt(f.Type.Size)))
		g.bpf("        public int %sLength;\n", name)
	case f.Type.Kind == ir.TBytes:
		g.bpf("        // bytes(%s): fixed buffer, used length beside it (SPEC §4.7)\n", ir.RenderExpr(f.Type.SizeExpr))
		g.bpf("        public final byte[] %s = new byte[%s];\n", name, g.renderArraySize(f.Type.SizeExpr, big.NewInt(f.Type.Size)))
		g.bpf("        public int %sLength;\n", name)
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
			// value can carry (SPEC §4.6); a [..N] count takes the field's zero
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
	g.emitTagEnum(d.Name+"Type", typ, variantNames(d), d.Max,
		fmt.Sprintf("union %s's tag — None = 0, then each variant in declared order (SPEC §4.8)", d.Name))

	g.bpf("    // %s — at most one of the arms; type says which. Construction is the empty\n", d.Name)
	g.bpf("    // union (None). A read zero-establishes exactly the selected arm before\n")
	g.bpf("    // decoding it (SPEC §5); unselected arms keep what they last held — the\n")
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
