// Package java emits the Java target: one .java file per schema file —
// Constants.schema -> Constants.java — deterministic to the byte (SPEC §6.1).
// Java allows one public class per file, so each schema file becomes one
// public final class of the same name (the protobuf outer-class shape):
// nested static classes for the file's types and unions, static constants,
// and the static monomorphic wire functions. Every file of a unit shares
// `package <unitpackage>;`, so cross-file references are plain qualified
// names — Types.Vec3, Constants.MAX_CHAT_LENGTH — and generated files never
// import each other.
//
// The target is Java 17 on the JVM. Generated code is SELF-CONTAINED: it
// imports only java.lang.invoke (VarHandle word access), java.nio.ByteOrder
// and java.util.Arrays, never a runtime package. The serialize.java port
// measured the JVM's failure modes against the C++ reference and each is a
// directive here (issue #156): separate monomorphic write/read functions
// per type that never dispatch through a stream interface (the unified
// pattern measured ~2x), the family bitpacker at every field with literal
// constant bit widths and masks (a literal-width probe ran +40% write /
// +85% read over array-driven widths) through small static helpers the JIT
// inlines, and writer contracts in ONE `assert checkWriteX(value, data,
// bitOffset)` predicate call per function — dormant assert bodies count
// against C2's inline thresholds even with -ea absent, so the hot bodies
// carry a single small call and the contract walk lives in a private
// predicate (the port's assert-predicate shape). Zero allocation on the hot
// paths; the 128-bit storage widths ride the emitted immutable
// Int128/UInt128 pair, which allocates per value exactly as the port's
// Int128Value/UInt128Value do.
//
// Naming is Java's own: classes PascalCase, members and methods lowerCamel —
// the first-letter-lowered form of the same ir.GoExportName mapping the
// Go/C#/JS/Dart targets share, a bijection on it, so the checker's existing
// collision detection covers Java without a second registry — and every
// static final constant UPPER_SNAKE (schema consts, enum variants as
// Weapon.LASER, flag masks, <NAME>_MAX_BITS, PROTOCOL_ID), the Rust target's
// constant mapping.
//
// Storage is the §6.1 storage principle in Java's value domain: classes
// with public fields. Java has no unsigned types: signed integers store the
// same-width signed type; unsigned ones store the next type that holds every
// value (uint8/uint16 in int, uint32 and bits(N <= 32) in long), so they
// read as their values; uint64, bits(N > 32) and flags store long, two's
// complement, with Long.compareUnsigned wherever unsigned ordering matters.
// Enums and union tags keep their narrow storage types (byte for most), as
// integer constants. A [N]uint8 array is a byte[]. float32 stores float
// (Java's native single), float64 double, and the 128-bit widths the
// emulated (hi, lo) pair — Int128.java/UInt128.java emitted beside the unit
// exactly when a 128-bit field exists.
//
// Functions per type, static on the declaring file's class:
// write<Name>Bits(value, data, bitOffset) -> the bit position after value
// (trusted writer, contracts asserted through the predicate);
// read<Name>Bits(value, data, bitOffset, numBits) -> the bit position after
// value or -1 (the family read verdict — reads validate EVERYTHING and never
// throw on hostile bytes); write<Name>(value, data) -> bytes written and
// read<Name>(value, data, numBits) -> boolean, the offset-0 wrappers;
// zero<Name>(value) -> the §5 zero form. There is no measure function
// (SPEC §6.1): the write returns the size and <NAME>_MAX_BYTES sizes
// buffers. Buffers are caller-owned byte arrays: write buffers are a
// multiple of 8 bytes (the family write-buffer granularity) holding the
// value's MAX_BITS past its offset; read buffers need NO slack past the
// payload — the reader prices its 64-bit windows inside the buffer via the
// assembled tail window, the same no-slack stance the Dart target ships.
package java

import (
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mas-bandwidth/schema/v2/internal/ast"
	schemacheck "github.com/mas-bandwidth/schema/v2/internal/check"
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

// javaReserved is Java's reserved words, read from the one table (check.ReservedIn,
// SPEC §4.6) and never copied here.
var javaReserved = schemacheck.ReservedIn("Java")

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
	// constants, enum variants and flag masks are UPPER_SNAKE (constName),
	// and no Java reserved word is upper case: only types and members check
	for _, e := range u.Enums {
		if err := checkType("enum", e.Name); err != nil {
			return err
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
	fn          strings.Builder
	loopDepth   int
	needV       bool // the group value temp
	needLo      bool // the wide-lane accumulator (33..64-bit reads, 128 lanes)
	needHi      bool // the high 64-bit lane (128-bit-storage reads)
	usesRead    bool // read side: the body checks bounds against numBits
	usesPeek    bool // read side: the body loads windows (tailBase, tailWord)
	inElem      bool // emitting an array element (uint8 elements are bytes)
	stored      bool // write side: data holds the partial word, scratch is stale
	usesScratch bool // write side: the body loads the scratch word

	usesWire   bool            // the file emitted wire functions: the VarHandle rides
	usesArrays bool            // the file calls Arrays.fill
	helpers    map[string]bool // the bitpacker helpers the file's bodies call
}

// use records that a body calls the named bitpacker helper, so the file
// carries it (javaHelpers, in their fixed order).
func (g *gen) use(helper string) {
	if g.helpers == nil {
		g.helpers = map[string]bool{}
	}
	g.helpers[helper] = true
}

// javaHelpers is the bitpacker, one small private static method a step, in
// the order a file carries them. Each sits far under C2's inline threshold
// (FreqInlineSize), so the bodies that call them compile to the inlined
// word arithmetic; the wire is the family bitpacker's exactly.
var javaHelpers = []struct{ name, src string }{
	{"putBits", `    // putBits merges v (already masked to bits, at most 32) into the scratch word at bit
    // pos, storing the word to data once it fills, and returns the new scratch word. The
    // caller advances pos by bits.
    private static long putBits(byte[] data, int pos, long scratch, long v, int bits) {
        final int used = pos & 63;
        scratch |= v << used;
        if (used + bits >= 64) {
            LONG_LE.set(data, (pos >>> 6) * 8, scratch);
            return v >>> (64 - used);
        }
        return scratch;
    }
`},
	{"alignBits", `    // alignBits zero-pads the write to the next byte boundary and returns the new scratch
    // word; the caller rounds pos up to a multiple of 8.
    private static long alignBits(byte[] data, int pos, long scratch) {
        if ((pos & 63) > 56) {
            LONG_LE.set(data, (pos >>> 6) * 8, scratch);
            return 0;
        }
        return scratch;
    }
`},
	{"flushBits", `    // flushBits stores the partial scratch word at bit pos — before a bulk copy, a nested
    // write, or the end of the value.
    private static void flushBits(byte[] data, int pos, long scratch) {
        if ((pos & 63) != 0) {
            LONG_LE.set(data, (pos >>> 6) * 8, scratch);
        }
    }
`},
	{"loadBits", `    // loadBits is the scratch word for writing on from bit pos: the bits data already
    // holds below pos, zero above it.
    private static long loadBits(byte[] data, int pos) {
        final int used = pos & 63;
        if (used == 0) {
            return 0;
        }
        return (long) LONG_LE.get(data, (pos >>> 6) * 8) & ((1L << used) - 1);
    }
`},
	{"loadTail", `    // loadTail assembles the buffer's last 64-bit window, once per read (the whole buffer,
    // zero-extended, when it is shorter than eight bytes).
    private static long loadTail(byte[] data) {
        if (data.length >= 8) {
            return (long) LONG_LE.get(data, data.length - 8);
        }
        long word = 0;
        for (int i = data.length - 1; i >= 0; i--) {
            word = (word << 8) | (data[i] & 0xffL);
        }
        return word;
    }
`},
	{"peekBits", `    // peekBits is the stream from bit pos on, low bit first, in a 64-bit window that never
    // loads past the buffer: positions in its last eight bytes read the tail word.
    private static long peekBits(byte[] data, int pos, int tailBase, long tailWord) {
        final int at = pos >>> 3;
        if (at < tailBase) {
            return (long) LONG_LE.get(data, at) >>> (pos & 7);
        }
        return tailWord >>> (pos - tailBase * 8);
    }
`},
	{"alignRead", `    // alignRead skips the padding to the next byte boundary and returns the new position,
    // or -1 for padding that is nonzero or runs past numBits (SPEC §4.3).
    private static int alignRead(byte[] data, int pos, int numBits) {
        final int pad = pos & 7;
        if (pad == 0) {
            return pos;
        }
        final int next = pos + 8 - pad;
        if (next > numBits || (data[pos >>> 3] & 0xff) >>> pad != 0) {
            return -1;
        }
        return next;
    }
`},
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
		h.WriteString("// at every field, literal constant widths and masks, monomorphic\n")
		h.WriteString("// write/read functions per type, zero runtime dependencies.\n")
		h.WriteString("//\n")
		h.WriteString("// write<Name>Bits(value, data, bitOffset) -> the bit position after value.\n")
		h.WriteString("// The writer TRUSTS the caller (the C++ stance): contracts live in one\n")
		h.WriteString("// assert-predicate call — active under -ea, dormant bytecode otherwise —\n")
		h.WriteString("// and width masks stay, as wire arithmetic. write<Name>(value, data) is\n")
		h.WriteString("// the offset-0 form and returns bytes; its buffer holds <NAME>_MAX_BYTES.\n")
		h.WriteString("//\n")
		h.WriteString("// read<Name>Bits(value, data, bitOffset, numBits) -> the bit position\n")
		h.WriteString("// after value, or -1: the family read verdict. The wire is a trust\n")
		h.WriteString("// boundary: reads validate everything — bounds fused per static run,\n")
		h.WriteString("// ranges, wire constants, reserved and align padding, union tags — and\n")
		h.WriteString("// NEVER throw on hostile bytes. No slack past the payload is required.\n")
		h.WriteString("// read<Name>(value, data, numBits) is the offset-0 form, a boolean.\n")
		h.WriteString("//\n")
		h.WriteString("// The Bits entries compose: nested types call them, and hand-written\n")
		h.WriteString("// bit code shares the buffer between them. zero<Name>(value) -> the §5\n")
		h.WriteString("// zero form.\n")
	}
	h.WriteString("\n")
	fmt.Fprintf(&h, "package %s;\n\n", g.unit.Package)
	var imports []string
	if g.usesWire {
		imports = append(imports, "java.lang.invoke.MethodHandles", "java.lang.invoke.VarHandle", "java.nio.ByteOrder")
	}
	if g.usesArrays {
		imports = append(imports, "java.util.Arrays")
	}
	for _, imp := range imports {
		fmt.Fprintf(&h, "import %s;\n", imp)
	}
	if len(imports) > 0 {
		h.WriteString("\n")
	}
	fmt.Fprintf(&h, "// %s carries every generated declaration of %s.schema — Java has no\n", g.file.Base, g.file.Base)
	h.WriteString("// top-level functions or constants, so the file's one public class is their\n")
	h.WriteString("// home; types nest inside it (SPEC §6.1 naming).\n")
	fmt.Fprintf(&h, "public final class %s {\n", g.file.Base)
	fmt.Fprintf(&h, "    private %s() {}\n\n", g.file.Base)
	if g.usesWire {
		h.WriteString("    // 64-bit little-endian word access into byte[] — the proven-fast JVM\n")
		h.WriteString("    // form (VarHandle byteArrayViewVarHandle, serialize.java's own).\n")
		h.WriteString("    private static final VarHandle LONG_LE =\n")
		h.WriteString("            MethodHandles.byteArrayViewVarHandle(long[].class, ByteOrder.LITTLE_ENDIAN);\n\n")
		for _, helper := range javaHelpers {
			if g.helpers[helper.name] {
				h.WriteString(helper.src)
				h.WriteString("\n")
			}
		}
	}
	h.WriteString(strings.TrimRight(g.body.String(), "\n"))
	h.WriteString("\n}\n")
	return []byte(h.String())
}

func (g *gen) emitFile() {
	if g.home {
		g.bpf("    // The unit's protocol id — the hash of its wire shape (SPEC §3.1). Two\n")
		g.bpf("    // sides at the same id speak identical bits; there is no other versioning.\n")
		g.bpf("    public static final long PROTOCOL_ID = 0x%016xL;\n\n", g.unit.ProtocolId)
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
	name := constName(d.Name)
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
	g.bpf("        public static final %s NONE = 0;\n", typ)
	for i, m := range members {
		g.bpf("%s", ir.DocComment(docs[i], "        ", "//"))
		g.bpf("        public static final %s %s = %s;\n", typ, constName(m), narrowLit(typ, big.NewInt(int64(i+1))))
	}
	g.bpf("        // the exported extent (SPEC §4.2)\n")
	g.bpf("        public static final %s MAX = %s;\n", typ, narrowLit(typ, big.NewInt(max)))
	g.bpf("    }\n\n")

	// the tag enum's name surface is a declared enum's, member for member: a
	// reader logging which message arrived writes enumName<Tag>(value)
	// whichever enum it is. Nothing on the read or write path calls it.
	g.bpf("    // enumName%s: debug/log/tooling name for any %s storage value —\n", name, name)
	g.bpf("    // out-of-set values (wire-legal up to the declared max) name as \"???\"\n")
	g.bpf("    public static String enumName%s(long value) {\n", name)
	g.bpf("        if (value == %s.NONE) {\n            return \"None\";\n        }\n", name)
	for _, m := range members {
		g.bpf("        if (value == %s.%s) {\n            return \"%s\";\n        }\n", name, constName(m), m)
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
	g.bpf("        public static final %s NONE = 0;\n", typ)
	for i, v := range d.Variants {
		g.bpf("%s", ir.DocComment(d.VariantDocs[i], "        ", "//"))
		g.bpf("        public static final %s %s = %s;\n", typ, constName(v), narrowLit(typ, big.NewInt(int64(i+1))))
	}
	g.bpf("        // the exported extent (SPEC §4.2)\n")
	g.bpf("        public static final %s MAX = %s;\n", typ, narrowLit(typ, big.NewInt(d.Max)))
	g.bpf("    }\n\n")

	g.bpf("    // enumName%s: debug/log/tooling name for any %s storage value —\n", d.Name, d.Name)
	g.bpf("    // out-of-set values (wire-legal up to the declared max) name as \"???\"\n")
	g.bpf("    public static String enumName%s(long value) {\n", d.Name)
	g.bpf("        if (value == %s.NONE) {\n            return \"None\";\n        }\n", d.Name)
	for _, v := range d.Variants {
		g.bpf("        if (value == %s.%s) {\n            return \"%s\";\n        }\n", d.Name, constName(v), v)
	}
	g.bpf("        return \"???\";\n    }\n\n")
}

func (g *gen) emitFlags(d *ir.Flags) {
	g.bpf("%s", ir.DocComment(d.Doc, "    ", "//"))
	g.bpf("    // %s — one bit per variant, consumed as masks; flags-typed fields store\n", d.Name)
	g.bpf("    // uint64 in every target — a bit-transparent long here — wire %d bits\n", d.WireBits)
	g.bpf("    // (SPEC §4.2). Mask names are the family's flat spelling, UPPER_SNAKE.\n")
	for i, v := range d.Variants {
		g.bpf("%s", ir.DocComment(d.VariantDocs[i], "    ", "//"))
		g.bpf("    public static final long %s = 1L << %d;\n", constName(d.Name+v), i)
	}
	g.bpf("    // the declared variant count (SPEC §4.2)\n")
	g.bpf("    public static final int %s = %d;\n", constName(d.Name+"Count"), len(d.Variants))
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
				fmt.Sprintf("            Arrays.fill(%s, %s.zero);\n", name, typ))
			g.usesArrays = true
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
		typ = intStorage(t)
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
				return typ, g.qualifyType(t.Name) + "." + constName(f.DefVariant)
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
		// bits(N) stores uint32 (N <= 32) or uint64: a long either way, so a
		// 32-bit pattern reads as its value
		if f.HasDefault {
			return "long", narrowLit("long", f.DefInt)
		}
		return "long", ""
	}
	// signed integers in the same-width signed type; unsigned ones one size
	// up, so they read as their values (intStorage)
	typ = intStorage(t)
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

// intStorage is the Java storage type of an integer or fixed-point value.
// Signed widths take the same-width signed type. Java has no unsigned types,
// so unsigned widths take the next signed type that holds every value —
// uint8 and uint16 in int, uint32 in long — and read as their values with no
// masking. uint64 stays long, two's complement: values past
// Long.MAX_VALUE read negative, and Long.compareUnsigned, Long.divideUnsigned
// and Long.toUnsignedString are its unsigned operations.
func intStorage(t ir.FieldType) string {
	switch {
	case t.Signed:
		return intJavaType(t.Width)
	case t.Width <= 16:
		return "int"
	}
	return "long"
}

// intType is intStorage where the emitter stands: a uint8 ARRAY element is
// the one exception, a byte — a [N]uint8 is a byte[], the byte-buffer idiom
// the bulk copy and System.arraycopy take.
func (g *gen) intType(t ir.FieldType) string {
	if g.inElem {
		return elemIntStorage(t)
	}
	return intStorage(t)
}

// elemIntStorage is intStorage for an array element: uint8 elements are bytes.
func elemIntStorage(t ir.FieldType) string {
	if !t.Signed && t.Width <= 8 {
		return "byte"
	}
	return intStorage(t)
}

// intJavaType is the same-width signed Java type for an integer storage
// width.
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
		return elemIntStorage(t)
	case ir.TBits:
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
	g.bpf("        public %s type = %sType.NONE;\n", typ, d.Name)
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

// renderExpr renders an expression in Java form: constants in their
// UPPER_SNAKE spelling, qualified where they live in another
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
		return g.qualify(e.Name, constName(e.Name))
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
//     and a blank line around each member that carries a Javadoc;
//   - a signature too wide breaks before its throws clause first, and a
//     conditional or an argument list a template broke by hand is rejoined
//     so the formatter's breaks are laid down instead (joinThrows,
//     joinTernaries, handBroken);
//   - a chain keeps the formatter's receiver on its first line (`new T(..)`,
//     a qualified type with its constant, a short name with its first call),
//     and a declaration whose `Type name =` is too wide breaks at the name.
//
// It NEVER moves the indentation of a line already in the formatter's shape:
// a template that indents such a line wrongly is a template the gate must see
// (tools/sabotage/table_java_format_drift.go plants exactly that), so the fix
// for one is in the template. Only a compact line -- statements run together,
// unspaced -- and the blocks it opens are laid out afresh (relayout), and a
// method body EVERY line of which sits the same whole number of indents too
// deep (a field reader written for a switch arm, reused as a method body) is
// moved back to its level whole (deepBodies); one mis-indented line among
// lines at the right depth is never moved.
func Canonical(src []byte) []byte {
	lines := strings.Split(javaElseIf.ReplaceAllString(string(src), "} else if ("), "\n")
	lines = canonJavadocs(lines)
	lines = joinThrows(lines)
	lines = joinTernaries(lines)
	lines = joinOpenParens(lines)
	lines = joinLoneBraces(lines)
	lines = relayout(lines)
	lines = deepBodies(lines)
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
	if w := wrapBracelessFor(l); w != nil {
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
			switch c {
			case '\\':
				i++
			case quote:
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
		if javaWidth(ind+strings.TrimSuffix(lhs, " ")) > javaColumnLimit {
			return nil // the declaration itself breaks first (wrapLong)
		}
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
		if javaWidth(argInd+a+end) > javaColumnLimit && (!strings.HasPrefix(a, "\"") || !strings.HasSuffix(a, "\"")) {
			w := breakArg(argInd, a, end)
			if w == nil {
				return nil
			}
			out = append(out, w...)
			continue
		}
		out = append(out, argInd+a+end)
	}
	return out
}

// breakPrefixed is breakArg for an operand behind a prefix (`? `, `: `, an
// operator): the prefix leads its first line.
func breakPrefixed(ind, prefix, e, tail string) []string {
	w := breakArg(ind, e, tail)
	if w == nil {
		return nil
	}
	w[0] = ind + prefix + strings.TrimLeft(w[0], " ")
	return fitAll(w)
}

// breakArg lays out one argument that does not fit on its line: a call (or a
// `new`) opens its own argument list eight columns deeper, all on one line
// when they fit and one a line otherwise, recursively; a string literal is
// left whole. nil when the argument is neither.
func breakArg(ind, a, end string) []string {
	if javaWidth(ind+a+end) <= javaColumnLimit || (strings.HasPrefix(a, "\"") && strings.HasSuffix(a, "\"")) {
		return []string{ind + a + end}
	}
	if c, x, y, ok := splitTernary(a); ok {
		// a conditional: its arms eight columns under the condition, each
		// broken as an argument of its own when it does not fit
		yes, no := breakPrefixed(ind+"        ", "? ", x, ""), breakPrefixed(ind+"        ", ": ", y, end)
		if yes == nil || no == nil || javaWidth(ind+c) > javaColumnLimit {
			return nil
		}
		return append(append([]string{ind + c}, yes...), no...)
	}
	if parts, ok := splitBinary(a); ok && len(parts) > 1 {
		// a binary chain: one operand a line, each later one eight columns
		// in behind its operator, broken in turn when it does not fit
		tail := ""
		if len(parts) == 1 {
			tail = end
		}
		out := breakArg(ind, parts[0], tail)
		if out == nil {
			return nil
		}
		for i, p := range parts[1:] {
			op, operand, _ := strings.Cut(p, " ")
			t := ""
			if i == len(parts)-2 {
				t = end
			}
			w := breakPrefixed(ind+"        ", op+" ", operand, t)
			if w == nil {
				return nil
			}
			out = append(out, w...)
		}
		return out
	}
	if m := javaArrayArg.FindStringSubmatch(a); m != nil {
		// `new T[] {a, b}`: one element a line, four columns in
		elems, ok := splitArgs(m[2])
		if !ok {
			return nil
		}
		out := []string{ind + m[1] + " {"}
		for i, e := range elems {
			if i < len(elems)-1 {
				e += ","
			}
			out = append(out, ind+"    "+e)
		}
		return fitAll(append(out, ind+"}"+end))
	}
	if strings.HasPrefix(a, "(") {
		// a cast: the operand eight columns under it
		if c := matchParen(a, 0); c > 0 && c+2 < len(a) && a[c+1] == ' ' && javaCallee.MatchString(a[1:c]) {
			rest := breakArg(ind+"        ", a[c+2:], end)
			if rest == nil {
				return nil
			}
			return append([]string{ind + a[:c+1]}, rest...)
		}
	}
	open := strings.IndexByte(a, '(')
	if open < 0 || !strings.HasSuffix(a, ")") || matchParen(a, open) != len(a)-1 {
		// a chain of calls: broken before each, eight columns in
		return layoutChain(ind, a, end)
	}
	if !javaCallee.MatchString(strings.TrimPrefix(a[:open], "new ")) {
		return nil
	}
	args, ok := splitArgs(a[open+1 : len(a)-1])
	if !ok || len(args) == 0 {
		return nil
	}
	out := []string{ind + a[:open+1]}
	inner := ind + "        "
	if one := inner + strings.Join(args, ", ") + ")" + end; javaWidth(one) <= javaColumnLimit {
		return append(out, one)
	}
	for i, x := range args {
		e := ","
		if i == len(args)-1 {
			e = ")" + end
		}
		w := breakArg(inner, x, e)
		if w == nil {
			return nil
		}
		out = append(out, w...)
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
			switch c {
			case '\\':
				i++
			case quote:
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
	pin := ind + "        "
	// a throws clause is the outer break: it goes onto its own line, eight
	// columns in, before the parameters break, and once they break it is on
	// a line of its own as well
	throws, tail := strings.TrimSpace(m[3]), ")"+m[3]+" {"
	if throws != "" {
		if sig := ind + m[1] + "(" + strings.Join(params, ", ") + ")"; javaWidth(sig) <= javaColumnLimit {
			return []string{sig, pin + throws + " {"}
		}
		tail = ")"
	}
	out := []string{ind + m[1] + "("}
	if one := pin + strings.Join(params, ", ") + tail; javaWidth(one) <= javaColumnLimit {
		out = append(out, one)
	} else {
		for i, p := range params {
			end := ","
			if i == len(params)-1 {
				end = tail
			}
			out = append(out, pin+p+end)
		}
	}
	if throws != "" {
		out = append(out, pin+throws+" {")
	}
	return out
}

// joinThrows rejoins a signature a template broke before its throws clause,
// so wrapDecl lays the break down where the formatter puts it.
func joinThrows(lines []string) []string {
	var out []string
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		t := strings.TrimSpace(l)
		if i+1 < len(lines) && strings.HasSuffix(t, ")") && !strings.Contains(t, "//") && parenDepth(l) == 0 {
			n := strings.TrimSpace(lines[i+1])
			if strings.HasPrefix(n, "throws ") && strings.HasSuffix(n, " {") && len(indentOf(lines[i+1])) == len(indentOf(l))+8 &&
				javaDecl.MatchString(t+" "+n) {
				out = append(out, l+" "+n)
				i++
				continue
			}
		}
		out = append(out, l)
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
			switch c {
			case '\\':
				i++
			case quote:
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

// deepBodies moves a method body a template wrote whole levels too deep (a
// field reader written for a switch arm, reused as a method's body) back to
// its level. It moves a body only when EVERY line of it is deeper than the
// level by the same whole number of indents, so one mis-indented line among
// lines at the right depth, or any line off the four-column unit -- the drift
// the format gate exists to see, and what its control plants -- is never
// absorbed. A body of ONE line drifted by whole indents is moved (the map
// readers' pointer field is one); a sabotage meant to be seen must therefore
// drift off the unit or beside a line at the right depth.
func deepBodies(lines []string) []string {
	out := append([]string(nil), lines...)
	for i := 0; i < len(out); i++ {
		t := strings.TrimSpace(out[i])
		if !javaDecl.MatchString(t) {
			continue
		}
		ind := indentOf(out[i])
		j := i + 1
		for j < len(out) && out[j] != ind+"}" {
			if t := strings.TrimSpace(out[j]); t != "" && len(indentOf(out[j])) <= len(ind) {
				j = -1
				break
			}
			j++
		}
		if j < 0 || j >= len(out) || j == i+1 {
			continue
		}
		least := -1
		for _, l := range out[i+1 : j] {
			if strings.TrimSpace(l) == "" {
				continue
			}
			if n := len(indentOf(l)); least < 0 || n < least {
				least = n
			}
		}
		d := least - (len(ind) + 4)
		if d <= 0 || d%4 != 0 {
			continue
		}
		for k := i + 1; k < j; k++ {
			if strings.TrimSpace(out[k]) != "" {
				out[k] = out[k][d:]
			}
		}
		i = j
	}
	return out
}

// handBroken: a line a template broke inside an argument list, after a comma
// with arguments already behind the open parenthesis. The formatter never
// writes that shape -- it breaks right after the parenthesis or not at all --
// so the continuation below it is the template's and is rejoined.
func handBroken(l string) bool {
	t := strings.TrimSpace(l)
	if !strings.HasSuffix(t, ",") || strings.Contains(t, "//") {
		return false
	}
	var opens []int
	var quote byte
	for i := 0; i < len(t); i++ {
		c := t[i]
		if quote != 0 {
			switch c {
			case '\\':
				i++
			case quote:
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case '(':
			opens = append(opens, i)
		case ')':
			if len(opens) > 0 {
				opens = opens[:len(opens)-1]
			}
		}
	}
	return len(opens) == 1 && strings.TrimSpace(t[opens[0]+1:len(t)-1]) != ""
}

// joinTernaries rejoins a conditional a template broke before its `:`, so the
// formatter's breaks (before `?` and `:`, eight columns in) are laid down in
// its place.
func joinTernaries(lines []string) []string {
	var out []string
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		t := strings.TrimSpace(l)
		if i+1 < len(lines) && strings.Contains(t, " ? ") && !strings.HasPrefix(t, "? ") && !strings.HasPrefix(t, ": ") &&
			!strings.Contains(t, "//") && parenDepth(l) == 0 {
			n := strings.TrimSpace(lines[i+1])
			if strings.HasPrefix(n, ": ") && strings.HasSuffix(n, ";") && len(indentOf(lines[i+1])) > len(indentOf(l)) {
				e := strings.TrimSuffix(t+" "+n, ";")
				if strings.HasPrefix(e, "return ") {
					e = e[len("return "):]
				} else if k := topAssign(e); k >= 0 {
					e = e[k+3:]
				}
				if _, _, _, ok := splitTernary(e); ok {
					out = append(out, l+" "+n)
					i++
					continue
				}
			}
		}
		out = append(out, l)
	}
	return out
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
			if step := len(indentOf(lines[j+1])) - len(indentOf(l)); step > 0 && step%8 == 0 && !handBroken(l) {
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
	labels := strings.FieldsSeq(strings.ReplaceAll(m[2], "case ", "case\x00"))
	for lb := range labels {
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
	found := make([][]cut, len(javaBinaryOps))
	depth := 0
	var quote byte
	for i := 0; i < len(expr); i++ {
		c := expr[i]
		if quote != 0 {
			switch c {
			case '\\':
				i++
			case quote:
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
	case strings.HasPrefix(t, "} else if (") && strings.HasSuffix(t, ") {") && matchParen(t, 10) == len(t)-3:
		return breakCond(ind, "} else if (", t[len("} else if ("):len(t)-3], ") {")
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
		if len(stack) == 0 && javaTypeHeader.MatchString(t) && len(out) > 0 && strings.HasPrefix(strings.TrimSpace(out[len(out)-1]), "import ") {
			out = append(out, "") // the imports and the type are apart
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
		switch {
		case strings.HasSuffix(code, "{") && !strings.HasPrefix(t, "*") && !strings.HasPrefix(t, "//"):
			stack = append(stack, frame{class: javaTypeHeader.MatchString(code), member: len(indentOf(l)) + 4})
			lastMemberEnd = ""
		case atMember(l) && strings.HasSuffix(code, ";") && !strings.HasPrefix(t, "*"):
			lastMemberEnd = "field"
		case atMember(l) && strings.HasSuffix(code, "{}") && !strings.HasPrefix(t, "*"):
			lastMemberEnd = "block" // a member whose body is empty, on one line
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
		if (!strings.HasSuffix(t, "= {") && !strings.HasSuffix(t, "[] {")) || strings.HasPrefix(t, "//") {
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
			switch c {
			case '\\':
				i++
			case quote:
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
		return append([]string{head}, canonLine(body)...)
	}
	if w := breakCond(ind, "if (", cond, ")"); w != nil {
		return append(w, canonLine(body)...)
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
			switch c {
			case '\\':
				i++
			case quote:
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
	if strings.HasPrefix(t, "return ") {
		// `return c ? a : b;`: the condition stays, each arm eight columns in
		if c, a, b, ok := splitTernary(strings.TrimSuffix(t[len("return "):], ";")); ok {
			return fitAll([]string{ind + "return " + c, ind + "        ? " + a, ind + "        : " + b + ";"})
		}
	}
	if m := javaCompound.FindStringSubmatch(t); m != nil {
		// `x op= rhs;`: the right side eight columns in, broken as an argument
		if w := breakArg(ind+"        ", m[3], ";"); w != nil {
			return append([]string{ind + m[1] + " " + m[2] + "="}, w...)
		}
	}
	k := topAssign(t)
	if k < 0 {
		return nil
	}
	lhs, rhs := t[:k], t[k+3:]
	inner := ind + "        "
	if javaWidth(ind+lhs+" =") > javaColumnLimit {
		// `Type name = init;` with the type alone near the limit: the break
		// is between the type and the name
		if sp := strings.LastIndexByte(lhs, ' '); sp > 0 && javaSimpleName.MatchString(lhs[sp+1:]) {
			typ, rest := lhs[:sp], inner+lhs[sp+1:]+" = "+rhs
			if javaWidth(ind+typ) <= javaColumnLimit {
				return fitAll([]string{ind + typ, rest})
			}
			// a generic type too wide for its line: one type argument a
			// line, sixteen columns in
			if lt := strings.IndexByte(typ, '<'); lt > 0 && strings.HasSuffix(typ, ">") {
				targs, ok := splitArgs(typ[lt+1 : len(typ)-1])
				if !ok || len(targs) < 2 {
					return nil
				}
				for _, a := range targs {
					if strings.Count(a, "<") != strings.Count(a, ">") {
						return nil // a nested generic: splitArgs does not see its brackets
					}
				}
				out := []string{ind + typ[:lt+1]}
				for i, a := range targs {
					e := ","
					if i == len(targs)-1 {
						e = ">"
					}
					out = append(out, inner+"        "+a+e)
				}
				return fitAll(append(out, rest))
			}
		}
		return nil
	}
	if javaWidth(inner+rhs) <= javaColumnLimit {
		return []string{ind + lhs + " =", inner + rhs}
	}
	if _, _, _, ok := splitTernary(strings.TrimSuffix(rhs, ";")); ok {
		if w := breakArg(inner, strings.TrimSuffix(rhs, ";"), ";"); w != nil {
			return append([]string{ind + lhs + " ="}, w...)
		}
	}
	if parts, ok := splitBinary(strings.TrimSuffix(rhs, ";")); ok && len(parts) > 1 {
		if w := breakArg(inner, strings.TrimSuffix(rhs, ";"), ";"); w != nil {
			return append([]string{ind + lhs + " ="}, w...)
		}
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

// splitTernary cuts `c ? a : b` at its top-level operators; ok is false when
// the expression is not one conditional.
func splitTernary(e string) (c, a, b string, ok bool) {
	depth, q, colon, nest := 0, -1, -1, 0
	var quote byte
	for i := 0; i < len(e); i++ {
		ch := e[i]
		if quote != 0 {
			switch ch {
			case '\\':
				i++
			case quote:
				quote = 0
			}
			continue
		}
		switch ch {
		case '"', '\'':
			quote = ch
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '?':
			if depth == 0 {
				if q < 0 {
					q = i
				} else {
					nest++
				}
			}
		case ':':
			if depth == 0 && q >= 0 {
				if nest > 0 {
					nest--
				} else if colon < 0 {
					colon = i
				}
			}
		}
	}
	if q < 1 || colon < 0 || e[q-1] != ' ' || colon+2 > len(e) || e[colon-1] != ' ' {
		return "", "", "", false
	}
	return strings.TrimSpace(e[:q]), strings.TrimSpace(e[q+1 : colon]), strings.TrimSpace(e[colon+1:]), true
}

// fitAll is lines when every one is within the limit, else nil.
func fitAll(lines []string) []string {
	for _, l := range lines {
		if javaWidth(l) > javaColumnLimit {
			return nil
		}
	}
	return lines
}

// wrapBracelessFor: a loop whose one-statement body overflows puts the body on
// the next line, four columns in, and lays that line out on its own.
func wrapBracelessFor(l string) []string {
	if javaWidth(l) <= javaColumnLimit || strings.Contains(l, "//") {
		return nil
	}
	ind := indentOf(l)
	t := strings.TrimSpace(l)
	if !strings.HasPrefix(t, "for (") || !strings.HasSuffix(t, ";") {
		return nil
	}
	end := matchParen(t, 4)
	if end < 0 || end+2 >= len(t) || t[end+1] != ' ' || strings.HasPrefix(t[end+2:], "{") {
		return nil
	}
	head := ind + t[:end+1]
	if javaWidth(head) > javaColumnLimit {
		return nil
	}
	return append([]string{head}, canonLine(ind+"    "+t[end+2:])...)
}

var javaCompound = regexp.MustCompile(`^([\w.\[\]]+) ([-+*/%&|^]|<<|>>|>>>)= (.*);$`)

var javaArrayArg = regexp.MustCompile(`^(new [\w.]+\[\]) \{(.*)\}$`)

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
		} else if _, nested := splitBinary(rest); !nested && k == 0 && len(parts) > 1 {
			// a call leading a broken binary: its arguments sixteen columns
			// in, the binary's continuation and the call's own
			sub = breakCallAt(lineInd, pre+rest, lineInd+"                ", end)
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
//
// The receiver is what the formatter keeps on the chain's first line: a
// `new T(...)` whole; a qualified type name with the constants behind it
// (`java.math.BigInteger.ONE`); a name with its first field (`value.reach`);
// and a name no wider than the continuation indent with its first call
// (`value.multiply(...)`), whose arguments, when they break, go sixteen
// columns under it. A call too wide for its line breaks its own arguments
// eight columns under it.
func wrapChain(ind, lhs, rhs string) []string {
	w := layoutChain(ind+"        ", strings.TrimSuffix(rhs, ";"), ";")
	if w == nil {
		return nil
	}
	return append([]string{ind + lhs + " ="}, w...)
}

// layoutChain lays a chain of two or more calls out from inner: the receiver
// on the first line, every later dereference eight columns in; end closes the
// last line. nil when e is not such a chain or a line will not fit.
func layoutChain(inner, e, end string) []string {
	segs := chainSegments(e)
	if len(segs) < 2 || segs[0] == "" || (segs[0][0] >= '0' && segs[0][0] <= '9') {
		return nil // a number's point is not a dereference
	}
	recv, k, prefixCall := chainReceiver(segs)
	calls := 0
	if prefixCall {
		calls++
	}
	for _, sg := range segs[k:] {
		if strings.Contains(sg, "(") {
			calls++
		}
	}
	if calls < 2 || k >= len(segs) {
		return nil
	}
	var out []string
	last := len(segs) - 1
	if one := inner + recv; javaWidth(one) <= javaColumnLimit {
		out = append(out, one)
	} else if prefixCall {
		w := breakCallAt(inner, recv, inner+"                ", "")
		if w == nil {
			return nil
		}
		out = append(out, w...)
	} else {
		return nil
	}
	for i, sg := range segs[k:] {
		e := ""
		if k+i == last {
			e = end
		}
		line := inner + "        ." + sg + e
		if javaWidth(line) <= javaColumnLimit {
			out = append(out, line)
			continue
		}
		w := breakCallAt(inner+"        ", "."+sg, inner+"                ", e)
		if w == nil {
			return nil
		}
		out = append(out, w...)
	}
	return fitAll(out)
}

// chainSegments cuts an expression at its top-level dots.
func chainSegments(rhs string) []string {
	var segs []string
	depth, start := 0, 0
	var quote byte
	for i := 0; i < len(rhs); i++ {
		c := rhs[i]
		if quote != 0 {
			switch c {
			case '\\':
				i++
			case quote:
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
	return append(segs, rhs[start:])
}

var javaSimpleName = regexp.MustCompile(`^[a-z_]\w*$`)

// javaTypeName is an UpperCamel name; an ALL_CAPS constant is not one.
var javaTypeName = regexp.MustCompile(`^[A-Z]\w*[a-z]\w*$`)

// chainReceiver is the receiver the formatter keeps on a chain's first line,
// the index of the first segment behind it, and whether it ends in a call.
func chainReceiver(segs []string) (string, int, bool) {
	if strings.HasPrefix(segs[0], "new ") && !strings.Contains(segs[0], "(") {
		for j := 1; j < len(segs); j++ {
			if strings.Contains(segs[j], "(") {
				return strings.Join(segs[:j+1], "."), j + 1, false
			}
		}
		return "", len(segs), false
	}
	if strings.HasPrefix(segs[0], "new ") || strings.Contains(segs[0], "(") {
		return segs[0], 1, false
	}
	// a qualified type name: every dotted name ahead of the first call, when a
	// type (an upper-case name) is among them
	j := 0
	for j < len(segs) && !strings.Contains(segs[j], "(") {
		j++
	}
	if javaSimpleName.MatchString(segs[0]) && j > 1 {
		for _, sg := range segs[1:j] {
			if sg != "" && sg[0] >= 'A' && sg[0] <= 'Z' {
				return strings.Join(segs[:j], "."), j, false
			}
		}
	}
	if len(segs) > 2 && !strings.Contains(segs[1], "(") {
		return segs[0] + "." + segs[1], 2, false
	}
	// a type with its static call (`T.of(..).arm(1)`): the formatter's type
	// prefix runs through the first member after an UpperCamel type name
	if javaTypeName.MatchString(segs[0]) && strings.Contains(segs[1], "(") {
		return segs[0] + "." + segs[1], 2, true
	}
	if javaSimpleName.MatchString(segs[0]) && javaWidth(segs[0]) <= 8 && strings.Contains(segs[1], "(") {
		return segs[0] + "." + segs[1], 2, true
	}
	return segs[0], 1, false
}

// breakCallAt lays `head(args)` out with its arguments at argInd: on one line
// when they fit, one a line (each broken as an argument) otherwise.
func breakCallAt(ind, call, argInd, end string) []string {
	open := strings.IndexByte(call, '(')
	if open < 0 || !strings.HasSuffix(call, ")") || matchParen(call, open) != len(call)-1 {
		return nil
	}
	args, ok := splitArgs(call[open+1 : len(call)-1])
	if !ok || len(args) == 0 {
		return nil
	}
	out := []string{ind + call[:open+1]}
	if one := argInd + strings.Join(args, ", ") + ")" + end; javaWidth(one) <= javaColumnLimit {
		return append(out, one)
	}
	for i, x := range args {
		e := ","
		if i == len(args)-1 {
			e = ")" + end
		}
		w := breakArg(argInd, x, e)
		if w == nil {
			return nil
		}
		out = append(out, w...)
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
			switch c {
			case '\\':
				i++
			case quote:
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
			for l := range strings.SplitSeq(body, "\n") {
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

// COMPACT TEMPLATES ARE LAID OUT ONCE, HERE (#547). Some emitters write a
// statement group on one line, unspaced (`if(kind==15){v.x=...;}else{...}`),
// or a whole method on a few lines whose indentation is not the block's. The
// formatter's answer for such text is mechanical: one statement per line,
// one block level per four columns, the standard spacing between tokens.
// relayout gives that answer for those lines only, so each emitter need not
// carry it.
//
// A line is re-laid when its own token spacing is not the standard spacing,
// or it holds more than one statement or a block's opening and its contents.
// The lines a re-laid line opens a block over, up to its closing brace, are
// re-laid with it (their indentation is the block's, whatever was written).
// A line already in the formatter's shape is never touched, so a template
// that indents one correct line wrongly is still red at the gate
// (tools/sabotage/table_java_format_drift.go plants exactly that).

type jtok struct {
	s    string
	line int    // source line index
	kind byte   // 'i' ident/keyword, 'n' number, 's' string/char, 'o' operator/punct, 'c' line comment, 'b' block comment
	raw  string // for s == "r": a whole comment or annotation line, kept as written
}

var javaOps = []string{">>>=", "<<=", ">>=", ">>>", "...", "->", "::", "++", "--", "&&", "||", "==", "!=", "<=", ">=", "+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=", "<<", ">>"}

// lexJava splits one line's code into tokens; ok is false for text the lexer
// does not understand (an unterminated literal, a text block), which is then
// left exactly as written.
func lexJava(s string, line int) (toks []jtok, ok bool) {
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t':
			i++
		case c == '/' && i+1 < len(s) && s[i+1] == '/':
			toks = append(toks, jtok{s: strings.TrimRight(s[i:], " "), line: line, kind: 'c'})
			i = len(s)
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				return nil, false
			}
			toks = append(toks, jtok{s: s[i : i+2+end+2], line: line, kind: 'b'})
			i += 2 + end + 2
		case c == '"' || c == '\'':
			if c == '"' && strings.HasPrefix(s[i:], `"""`) {
				return nil, false
			}
			j := i + 1
			for j < len(s) && s[j] != c {
				if s[j] == '\\' {
					j++
				}
				j++
			}
			if j >= len(s) {
				return nil, false
			}
			toks = append(toks, jtok{s: s[i : j+1], line: line, kind: 's'})
			i = j + 1
		case c == '_' || c == '$' || unicode.IsLetter(rune(c)) || c >= 0x80:
			j := i
			for j < len(s) && (s[j] == '_' || s[j] == '$' || s[j] >= 0x80 || unicode.IsLetter(rune(s[j])) || unicode.IsDigit(rune(s[j]))) {
				j++
			}
			toks = append(toks, jtok{s: s[i:j], line: line, kind: 'i'})
			i = j
		case unicode.IsDigit(rune(c)) || (c == '.' && i+1 < len(s) && unicode.IsDigit(rune(s[i+1]))):
			j := i
			for j < len(s) {
				d := s[j]
				if unicode.IsDigit(rune(d)) || unicode.IsLetter(rune(d)) || d == '.' || d == '_' {
					j++
					continue
				}
				if (d == '-' || d == '+') && (s[j-1] == 'e' || s[j-1] == 'E') && !strings.HasPrefix(strings.ToLower(s[i:]), "0x") {
					j++
					continue
				}
				break
			}
			toks = append(toks, jtok{s: s[i:j], line: line, kind: 'n'})
			i = j
		default:
			op := string(c)
			for _, o := range javaOps {
				if strings.HasPrefix(s[i:], o) {
					op = o
					break
				}
			}
			toks = append(toks, jtok{s: op, line: line, kind: 'o'})
			i += len(op)
		}
	}
	return toks, true
}

var javaBinary = map[string]bool{
	"=": true, "==": true, "!=": true, "<": true, ">": true, "<=": true, ">=": true, "&&": true, "||": true,
	"+": true, "-": true, "*": true, "/": true, "%": true, "&": true, "|": true, "^": true, "<<": true, ">>": true, ">>>": true,
	"+=": true, "-=": true, "*=": true, "/=": true, "%=": true, "&=": true, "|=": true, "^=": true, "<<=": true, ">>=": true, ">>>=": true,
	"?": true, ":": true, "->": true,
}

var javaPrimitive = map[string]bool{"byte": true, "short": true, "int": true, "long": true, "float": true, "double": true, "boolean": true, "char": true}

var javaSpaceParen = map[string]bool{"if": true, "for": true, "while": true, "switch": true, "catch": true, "synchronized": true, "return": true, "throw": true, "case": true, "else": true, "try": true}

func isTypeName(s string) bool {
	return javaPrimitive[s] || (s != "" && unicode.IsUpper(rune(s[0])))
}

// genericSpans marks each `<` and `>` that is a type argument bracket.
func genericSpans(t []jtok) map[int]bool {
	g := map[int]bool{}
	for i := 0; i < len(t); i++ {
		if t[i].s != "<" || i == 0 || t[i-1].kind != 'i' || !isTypeName(t[i-1].s) {
			continue
		}
		depth, j := 0, i
		okSpan := true
		var marks []int
		for ; j < len(t); j++ {
			s := t[j].s
			switch {
			case s == "<":
				depth++
				marks = append(marks, j)
			case s == ">":
				depth--
				marks = append(marks, j)
			case s == ">>":
				okSpan = false
			case t[j].kind == 'i' || s == "," || s == "." || s == "?" || s == "[" || s == "]":
			default:
				okSpan = false
			}
			if !okSpan || depth == 0 {
				break
			}
		}
		if okSpan && depth == 0 {
			for _, m := range marks {
				g[m] = true
			}
			i = j
		}
	}
	return g
}

// joinTokens prints one line's tokens with the formatter's spacing.
func joinTokens(t []jtok) string {
	gen := genericSpans(t)
	casts := map[int]bool{} // index of a cast's closing paren
	for i := 0; i+2 < len(t); i++ {
		if t[i].s != "(" || (i > 0 && t[i-1].kind == 'i' && !javaSpaceParen[t[i-1].s]) {
			continue
		}
		j := i + 1
		if !isTypeName(t[j].s) {
			continue
		}
		for j+1 < len(t) && (t[j+1].s == "." || t[j+1].s == "[" || t[j+1].s == "]" || (t[j].s == "." && t[j+1].kind == 'i')) {
			j++
		}
		if j+2 < len(t) && t[j+1].s == ")" {
			n := t[j+2]
			if n.kind == 'i' || n.kind == 'n' || n.kind == 's' || n.s == "(" || n.s == "!" || n.s == "~" {
				if n.kind != 'i' || !javaBinaryWord(n.s) {
					casts[j+1] = true
				}
			}
		}
	}
	var b strings.Builder
	question := 0
	for i, k := range t {
		if i == 0 {
			b.WriteString(k.s)
			if k.s == "?" {
				question++
			}
			continue
		}
		p := t[i-1]
		unary := func(j int) bool { // is t[j] a unary + or -?
			if t[j].s != "-" && t[j].s != "+" {
				return false
			}
			if j == 0 {
				return true
			}
			q := t[j-1]
			if q.kind == 'o' && q.s != ")" && q.s != "]" && q.s != "++" && q.s != "--" && (q.s != ">" || !gen[j-1]) {
				return true
			}
			return q.kind == 'i' && (q.s == "return" || q.s == "case" || q.s == "throw")
		}
		space := true
		switch {
		case k.kind == 'c' || k.kind == 'b':
			space = true
		case k.s == "," || k.s == ";":
			space = false
		case k.s == ")" || k.s == "]":
			space = false
		case k.s == ".":
			space = false
		case p.s == ".":
			space = false
		case k.s == "::" || p.s == "::":
			space = false
		case p.s == "(" || p.s == "[":
			space = false
		case p.s == "@":
			space = false
		case k.s == "++" || k.s == "--":
			space = p.kind == 'o' && javaBinary[p.s] && (p.s != ">" || !gen[i-1])
			if p.s == "(" || p.s == "[" {
				space = false
			}
		case p.s == "++" || p.s == "--":
			space = (k.kind != 'i' && k.kind != 'n') || (i >= 2 && (t[i-2].kind == 'i' || t[i-2].s == ")" || t[i-2].s == "]"))
		case k.s == "(":
			space = p.kind == 'i' && javaSpaceParen[p.s] || (p.kind == 'o' && javaBinary[p.s] && (p.s != ">" || !gen[i-1])) || p.s == "," || p.kind == 'c'
			if p.s == "!" || p.s == "~" || unary(i-1) || p.s == "(" || p.s == "[" {
				space = false
			}
			if p.s == ")" && casts[i-1] {
				space = true
			}
		case k.s == "[":
			space = false
		case k.s == "<" && gen[i]:
			space = false
		case k.s == ">" && gen[i]:
			space = false
		case p.s == "<" && gen[i-1]:
			space = false
		case p.s == ">" && gen[i-1]:
			space = k.s != "(" && k.s != "." && k.s != "[" && k.s != ")" && k.s != ","
		case p.s == "!" || p.s == "~":
			space = false
		case unary(i - 1):
			space = false
		case k.s == ":" && i >= 1 && question == 0 && (t[0].s == "case" || t[0].s == "default"):
			space = false
		case p.s == ")" && !casts[i-1] && (k.kind == 'i' || k.kind == 'n' || k.kind == 's'):
			space = true
		case p.s == "]" && k.s == "{":
			space = true
		case p.s == "{" || k.s == "}":
			// an initializer's braces hold their elements tight
			space = false
		}
		if k.s == "?" {
			question++
		}
		if space {
			b.WriteByte(' ')
		}
		b.WriteString(k.s)
	}
	return b.String()
}

func javaBinaryWord(s string) bool { return s == "instanceof" }

// relayLine is the standard text of one line that holds whole statements, or
// "" when the line is not something relayout prints.
func blockOpens(t []jtok, i int) bool {
	if t[i].s != "{" {
		return false
	}
	if i == 0 {
		return true
	}
	p := t[i-1].s
	return p != "]" && p != "=" && p != "," && p != "{" && p != "return"
}

type relayFrame struct {
	isSwitch bool
	inCase   bool
	isDo     bool
	extra    int // a braceless loop's body: the block sits one level in
}

type relayPrinter struct {
	base     string
	stack    []relayFrame
	cur      []jtok
	out      []string
	under    int  // closing braces of blocks opened before the region
	rootCase bool // the region opened inside a switch, under a case label
}

func (p *relayPrinter) level(label bool) int {
	n := 0
	if p.rootCase && (!label || len(p.stack) != 0) {
		n++
	}
	for i, f := range p.stack {
		n += 1 + f.extra
		if f.isSwitch && f.inCase && (!label || i != len(p.stack)-1) {
			n++
		}
	}
	return n - p.under
}

func (p *relayPrinter) emit(s string, label bool) { p.emitAt(s, label, 0) }

func (p *relayPrinter) emitAt(s string, label bool, extra int) {
	lv := p.level(label) + extra
	ind := max(len(p.base)+4*lv, 0)
	p.out = append(p.out, strings.Repeat(" ", ind)+s)
}

func (p *relayPrinter) flush(label bool) {
	if len(p.cur) == 0 {
		return
	}
	p.emit(joinTokens(p.cur), label)
	p.cur = nil
}

// relayout re-lays each compact line (and the block it opens) in the
// formatter's shape; everything else passes through unchanged.
func relayout(lines []string) []string {
	var out []string
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "//") || strings.HasPrefix(t, "*") || strings.HasPrefix(t, "/*") || strings.HasPrefix(t, "@") || strings.HasPrefix(t, "package ") || strings.HasPrefix(t, "import ") {
			out = append(out, l)
			continue
		}
		toks, ok := lexJava(t, i)
		if !ok || len(toks) == 0 || !relayNeeded(l, toks) {
			out = append(out, l)
			continue
		}
		ind := indentOf(l)
		if ind == "" {
			ind = contextIndent(out)
		}
		// gather the region: this line and every line inside the blocks it opens
		depth, parens := 0, 0
		var region []jtok
		j := i
		blank := map[int]bool{}
		for {
			lj := strings.TrimSpace(lines[j])
			if strings.HasPrefix(lj, "//") || strings.HasPrefix(lj, "/*") || strings.HasPrefix(lj, "*") || strings.HasPrefix(lj, "@") {
				raw := lj
				if strings.HasPrefix(lj, "*") {
					raw = " " + lj
				}
				region = append(region, jtok{s: raw, line: j, kind: 'r', raw: raw})
				if j+1 >= len(lines) {
					break
				}
				j++
				continue
			}
			lt, ok2 := lexJava(lj, j)
			if !ok2 {
				depth = 1 // a line the lexer cannot read ends the attempt
				break
			}
			if strings.TrimSpace(lines[j]) == "" {
				blank[j] = true
			}
			for _, k := range lt {
				switch k.s {
				case "{", "(", "[":
					if k.s == "{" {
						depth++
					} else {
						parens++
					}
				case "}", ")", "]":
					if k.s == "}" {
						depth--
					} else {
						parens--
					}
				}
			}
			region = append(region, lt...)
			if (depth <= 0 && parens <= 0) || j+1 >= len(lines) {
				break
			}
			j++
		}
		if depth > 0 || parens > 0 {
			out = append(out, l)
			continue
		}
		if len(region) > 0 && region[0].s == "}" {
			ind += "    " // the closing brace is at the line's own indentation
		}
		out = append(out, printRegion(ind, region, blank)...)
		i = j
	}
	return out
}

// relayNeeded: the line's spacing is not the standard spacing, or it holds a
// statement after a statement, or a block's opening with its contents.
func relayNeeded(l string, toks []jtok) bool {
	t := strings.TrimSpace(l)
	// a line of a statement broken over several (its parentheses do not
	// balance on it) is the formatter's own continuation: left alone
	parens := 0
	for _, k := range toks {
		switch k.s {
		case "(", "[":
			parens++
		case ")", "]":
			parens--
		}
		if parens < 0 {
			return false
		}
	}
	if parens != 0 {
		return false
	}
	for i, k := range toks {
		if k.kind == 'c' || k.kind == 'b' {
			return false // a commented line is left as written
		}
		if k.s == "{" && blockOpens(toks, i) && i+1 < len(toks) && toks[i+1].s != "}" {
			return true
		}
		if k.s == "}" && i > 0 && i+1 < len(toks) && toks[i+1].s != "else" && toks[i+1].s != "catch" && toks[i+1].s != "finally" && toks[i+1].s != "while" && toks[i+1].s != ";" && toks[i+1].s != ")" && toks[i+1].s != "," {
			if toks[i-1].s != "{" {
				return true
			}
		}
		if k.s == ";" && i+1 < len(toks) && toks[0].s != "for" {
			return true
		}
		if k.s == ":" && i+1 < len(toks) && (toks[0].s == "case" || toks[0].s == "default") && !strings.Contains(t, "?") && !strings.Contains(t, "->") {
			return true
		}
	}
	if !javaCompactSmell.MatchString(javaStrings.ReplaceAllString(t, `""`)) {
		return false
	}
	return joinTokens(toks) != t
}

var (
	javaStrings      = regexp.MustCompile(`"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'`)
	javaCompactSmell = regexp.MustCompile(`\b(if|for|while|switch|catch)\(|\)\{|\}else|else\{|[\w\])]=[^=]|[^=!<>+\-*/%&|^ ]=[\w("'!-]|,[^ ]|;[^ ]|[\w)\]](==|!=|&&|\|\||<=|>=)|(==|!=|&&|\|\||<=|>=)[\w(!]`)
)

// contextIndent is the indentation a line written at column 0 takes from the
// code line above it.
func contextIndent(out []string) string {
	for _, o := range slices.Backward(out) {
		t := strings.TrimSpace(o)
		if t == "" || strings.HasPrefix(t, "//") || strings.HasPrefix(t, "*") || strings.HasPrefix(t, "/*") {
			continue
		}
		ind := indentOf(o)
		if strings.HasSuffix(t, "{") || ((strings.HasPrefix(t, "case ") || strings.HasPrefix(t, "default")) && strings.HasSuffix(t, ":")) {
			return ind + "    "
		}
		return ind
	}
	return ""
}

func printRegion(base string, toks []jtok, blank map[int]bool) []string {
	p := &relayPrinter{base: base}
	parens := 0
	lastLine := -1
	for i := 0; i < len(toks); i++ {
		k := toks[i]
		if lastLine >= 0 && k.line > lastLine+1 {
			for b := lastLine + 1; b < k.line; b++ {
				if blank[b] && len(p.cur) == 0 {
					p.out = append(p.out, "")
				}
			}
		}
		lastLine = k.line
		next := func() string {
			if i+1 < len(toks) {
				return toks[i+1].s
			}
			return ""
		}
		switch {
		case k.kind == 'c':
			switch {
			case len(p.cur) > 0:
				p.cur = append(p.cur, k)
				p.flush(false)
			case len(p.out) > 0 && i > 0 && toks[i-1].line == k.line:
				p.out[len(p.out)-1] += " " + k.s
			default:
				p.emit(k.s, false)
			}
		case k.s == "(" || k.s == "[":
			parens++
			p.cur = append(p.cur, k)
		case k.s == ")" || k.s == "]":
			parens--
			p.cur = append(p.cur, k)
		case k.s == ";" && parens == 0:
			p.cur = append(p.cur, k)
			p.flush(false)
		case k.s == ":" && parens == 0 && len(p.cur) > 0 && (p.cur[0].s == "case" || p.cur[0].s == "default") && !hasTok(p.cur, "?"):
			p.cur = append(p.cur, k)
			if n := len(p.stack); n > 0 && p.stack[n-1].isSwitch {
				p.stack[n-1].inCase = true
			} else if n == 0 {
				p.rootCase = true
			}
			p.flush(true)
		case k.kind == 'r':
			p.flush(false)
			p.emit(k.raw, false)
		case k.s == "{" && parens == 0 && (len(p.cur) == 0 || !isInitializer(p.cur)):
			isSwitch := len(p.cur) > 0 && p.cur[0].s == "switch"
			isDo := len(p.cur) > 0 && p.cur[len(p.cur)-1].s == "do"
			extra := 0
			if len(p.cur) == 0 {
				p.emit("{", false)
			} else if sp := bracelessHead(p.cur); sp > 0 {
				// `for (...) if (...) {`: the loop's body goes one level in
				p.emit(joinTokens(p.cur[:sp]), false)
				body := append(append([]jtok{}, p.cur[sp:]...), k)
				p.cur = nil
				p.emitAt(joinTokens(body), false, 1)
				isSwitch = body[0].s == "switch"
				extra = 1
			} else {
				p.cur = append(p.cur, k)
				p.flush(false)
			}
			p.stack = append(p.stack, relayFrame{isSwitch: isSwitch, isDo: isDo, extra: extra})
			if next() == "}" && (i+2 >= len(toks) || (toks[i+2].s != "else" && toks[i+2].s != "catch" && toks[i+2].s != "finally")) {
				// an empty block closes on its opening line
				p.stack = p.stack[:len(p.stack)-1]
				p.out[len(p.out)-1] += "}"
				i++
				if n := next(); n == "else" || n == "catch" || n == "finally" {
					last := strings.TrimLeft(p.out[len(p.out)-1], " ")
					p.out = p.out[:len(p.out)-1]
					p.cur = []jtok{{s: last, line: k.line, kind: 'i'}}
				}
			}
		case k.s == "{" && parens == 0:
			// an initializer: copy it inline up to its closing brace
			depth := 0
			for ; i < len(toks); i++ {
				p.cur = append(p.cur, toks[i])
				if toks[i].s == "{" {
					depth++
				} else if toks[i].s == "}" {
					depth--
					if depth == 0 {
						break
					}
				}
			}
		case k.s == "}" && parens == 0:
			p.flush(false)
			var f relayFrame
			closeExtra := 0
			if len(p.stack) == 0 {
				p.under++
			} else {
				f = p.stack[len(p.stack)-1]
				p.stack = p.stack[:len(p.stack)-1]
				closeExtra = f.extra
			}
			n := next()
			switch {
			case n == "else" || n == "catch" || n == "finally" || (n == "while" && f.isDo):
				p.cur = []jtok{k}
			case n == ";" || n == ")" || n == ",":
				p.cur = []jtok{k}
			default:
				p.emitAt("}", false, closeExtra)
			}
		default:
			p.cur = append(p.cur, k)
		}
	}
	p.flush(false)
	return p.out
}

// bracelessHead is where a braceless loop or if header ends when a statement
// follows it on the same line, or 0.
func bracelessHead(t []jtok) int {
	if len(t) < 4 {
		return 0
	}
	switch t[0].s {
	case "for", "while", "if":
		if t[1].s != "(" {
			return 0
		}
		depth := 0
		for i := 1; i < len(t); i++ {
			switch t[i].s {
			case "(":
				depth++
			case ")":
				depth--
				if depth == 0 {
					if i+1 < len(t) {
						return i + 1
					}
					return 0
				}
			}
		}
	}
	return 0
}

func hasTok(t []jtok, s string) bool {
	for _, k := range t {
		if k.s == s {
			return true
		}
	}
	return false
}

// isInitializer: a brace after these opens an array initializer, not a block.
func isInitializer(cur []jtok) bool {
	last := cur[len(cur)-1].s
	return last == "]" || last == "=" || last == "," || last == "{" || last == "return"
}
