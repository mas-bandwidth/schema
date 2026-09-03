// Package c emits the C target: one header/source pair per schema file —
// Types.schema -> Types.h and Types.c — targeting the serialize.c runtime.
//
// # WHAT IS DIFFERENT ABOUT THIS TARGET
//
// C++ expresses reading and writing once, as a function templated over a write
// stream, a read stream or a measure stream. C has no templates, so this target
// emits SEPARATE write_X and read_X functions. That is the same trade the
// serialize.c runtime makes, and the reason a code generator earns its keep
// here more than anywhere else: the two halves cannot drift when one
// declaration produces both.
//
// CONVENTIONS (SPEC §6.1's C column)
//
//   - Constants are #define, by ruling. A #define carries no storage
//     and works in every context C has — array bounds, case labels, other
//     #defines — which a const int does not.
//
//   - Type names keep their schema spelling (Vec3), matching the C++ target.
//     C has no namespaces, so a unit's names live in the global namespace; the
//     package name is not prefixed because that would double the length of
//     every identifier for a collision that a caller can avoid by not mixing
//     two schema units in one translation unit.
//
//   - Functions are snake_case (write_vec3), matching the serialize.c runtime
//     they call into rather than the type names they carry.
//
//   - Enums are a fixed-width typedef plus #define constants, NOT a C enum. A C
//     enum's underlying type is implementation-defined, which would break the
//     storage-width contract in §6.1 — and headroom from | max = K makes
//     non-variant values wire-legal, which a C enum cannot represent honestly
//     anyway. Same reasoning as the Rust newtype.
package c

import (
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Generate returns filename -> contents for the unit: a header and a source
// file per schema file, plus the wire functions.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	bases := map[string]bool{}
	for _, f := range u.Files {
		bases[f.Base] = true
	}
	for _, f := range u.Files {
		if bases[f.Base+"Wire"] {
			return nil, fmt.Errorf("schema files %s and %sWire collide — the C emitter writes %sWire.h as %s's wire header; rename one file", f.Base, f.Base, f.Base, f.Base)
		}
	}

	out := map[string][]byte{}
	home := ir.ProtocolIdHome(u)
	deps := ir.FileDeps(u)

	var errs []error
	for _, f := range u.Files {
		g := &gen{unit: u, file: f, deps: sortedDeps(deps[f.Base])}
		g.emitDataHeader(f.Base == home)
		out[f.Base+".h"] = g.assembleHeader()
		errs = append(errs, g.errs...)

		w := &gen{unit: u, file: f, deps: sortedDeps(deps[f.Base]), wire: true}
		w.emitWireHeader()
		out[f.Base+"Wire.h"] = w.assembleWireHeader()
		errs = append(errs, w.errs...)
	}

	// Refuse to emit a partial target. Returning the files alongside an error
	// would invite a caller to use them.
	if len(errs) > 0 {
		var b strings.Builder
		for i, e := range errs {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(e.Error())
		}
		return nil, fmt.Errorf("%s", b.String())
	}

	return out, nil
}

type gen struct {
	unit     *ir.Unit
	errs     []error
	needs128 bool // a 128-bit or Q112.16 storage member needs serialize.h in the DATA header

	saidReadSlack  bool // the read-slack buffer contract is stated once per file, on its first MAX_BYTES
	saidFlagAppend bool // the flag_names_* append helpers are emitted once per file
	file           *ir.File
	deps           []string
	wire           bool
	body           strings.Builder

	// fixed [N]uint8 arrays of the struct being emitted whose element bytes
	// are statically byte-aligned (ir.AlignedFixedByteArrays) — the same map
	// the C++ backend consults, reloaded per function so it never outlives
	// its struct
	bulkBytes map[*ir.Field]bool

	// consts of this file #defined so far (symbolic-reference safety): the
	// preprocessor expands a reference at use, so a use may only render
	// symbolically once the definition stands above it
	emitted map[string]bool
}

func (g *gen) pf(format string, args ...any) {
	fmt.Fprintf(&g.body, format, args...)
}

// unsupported records a construct this backend cannot emit, and is the whole
// reason the C target now fails loudly.
//
// Three separate defects in this backend were an IR kind falling silently
// through a switch: const/reserved/align items, then every fixed-point field,
// then every object declaration. Each compiled clean, ran, and returned
// success while producing wrong bytes or none. A generator that cannot emit a
// construct must SAY SO -- a build error is recoverable, a silently truncated
// wire is not.
func (g *gen) unsupported(format string, args ...any) {
	g.errs = append(g.errs, fmt.Errorf("C backend: "+format, args...))
}

// guardName is the include guard: SCHEMA_<PACKAGE>_<BASE>_H.
func (g *gen) guardName(suffix string) string {
	return "SCHEMA_" + screaming(g.unit.Package) + "_" + screaming(g.file.Base) + suffix + "_H"
}

func (g *gen) assembleHeader() []byte {
	var h strings.Builder
	g.header(&h, g.file.Base)
	guard := g.guardName("")
	fmt.Fprintf(&h, "#ifndef %s\n#define %s\n\n", guard, guard)
	// includes follow what the body actually emitted, so a header never
	// carries an include nothing in it uses (a contains check can only ever
	// keep one too many, never drop one in use)
	body := g.body.String()
	h.WriteString("#include <stdint.h>\n")
	if strings.Contains(body, "memset") {
		h.WriteString("#include <string.h>   /* memset — the zero form (SPEC §4.2) */\n")
	}
	if strings.Contains(body, "floor") {
		h.WriteString("#include <math.h>     /* floor — the quantize pair */\n")
	}
	if g.needs128 {
		// serialize_int128_t / serialize_uint128_t are STORAGE here, so the
		// runtime header has to reach the data header and not only the wire one
		h.WriteString("#include \"serialize.h\"   /* serialize_int128_t: C has no 128-bit builtin */\n")
	}
	if strings.Contains(body, "SCHEMA_UNUSED") {
		h.WriteString(unusedMacro)
	}
	for _, d := range g.deps {
		fmt.Fprintf(&h, "#include \"%s.h\"\n", d)
	}
	h.WriteString("\n#ifdef __cplusplus\nextern \"C\" {\n#endif\n\n")
	h.WriteString(g.body.String())
	h.WriteString("#ifdef __cplusplus\n}\n#endif\n\n")
	fmt.Fprintf(&h, "#endif /* %s */\n", guard)
	return []byte(h.String())
}

func (g *gen) assembleWireHeader() []byte {
	var h strings.Builder
	g.header(&h, g.file.Base)
	guard := g.guardName("WIRE")
	fmt.Fprintf(&h, "#ifndef %s\n#define %s\n\n", guard, guard)
	fmt.Fprintf(&h, "#include \"%s.h\"\n", g.file.Base)
	body := g.body.String()
	if strings.Contains(body, "memset") || strings.Contains(body, "strlen") {
		h.WriteString("#include <string.h>   /* memset, strlen */\n")
	}
	h.WriteString("#include \"serialize.h\"\n")
	if strings.Contains(body, "SCHEMA_UNUSED") {
		h.WriteString(unusedMacro)
	}
	for _, d := range g.deps {
		fmt.Fprintf(&h, "#include \"%sWire.h\"\n", d)
	}
	h.WriteString("\n#ifdef __cplusplus\nextern \"C\" {\n#endif\n\n")
	h.WriteString(g.body.String())
	h.WriteString("#ifdef __cplusplus\n}\n#endif\n\n")
	fmt.Fprintf(&h, "#endif /* %s */\n", guard)
	return []byte(h.String())
}

// unusedMacro guards the header-static functions. A translation unit that
// includes a header but calls only some of its functions would otherwise warn
// on every one it did not use -- and this target is compiled with -Wall
// -Wextra -Werror in CI, so a warning is a build failure for the CONSUMER.
const unusedMacro = `
#ifndef SCHEMA_UNUSED
#if defined(__GNUC__) || defined(__clang__)
#define SCHEMA_UNUSED __attribute__((unused))
#else
#define SCHEMA_UNUSED
#endif
#endif
`

func (g *gen) header(h *strings.Builder, base string) {
	fmt.Fprintf(h, "/* Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", base)
	h.WriteString("   SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	h.WriteString("   your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	h.WriteString("   AGPL-3.0, its output is not.\n")
	fmt.Fprintf(h, "   package %s — protocol id 0x%016x */\n\n", g.unit.Package, g.unit.ProtocolId)
}

// ---- data header ----

func (g *gen) emitDataHeader(carriesProtocolId bool) {
	g.emitted = map[string]bool{}
	if carriesProtocolId {
		g.pf("/* The unit's protocol id — the hash of its wire shape (SPEC §3.1). Two\n")
		g.pf("   sides at the same id speak identical bits; there is no other versioning. */\n")
		g.pf("#define %s_PROTOCOL_ID 0x%016xULL\n\n", screaming(g.unit.Package), g.unit.ProtocolId)
	}

	// emission order, not declaration order: C needs every same-file named
	// type defined before its by-value users, same as C++ (found by
	// FuzzGeneratedCompiles — `type A { B B }  type B {}` emitted A first
	// and every consumer got `unknown type name 'B'`)
	for _, d := range ir.EmissionOrder(g.file) {
		switch decl := d.(type) {
		case *ir.Const:
			g.emitConst(decl)
		case *ir.Enum:
			g.emitEnum(decl)
		case *ir.Flags:
			g.emitFlags(decl)
		case *ir.Struct:
			g.emitStruct(decl)
		case *ir.Union:
			g.emitUnion(decl)
		default:
			g.unsupported("declaration kind %T in %s.schema has no C emission", decl, g.file.Base)
		}
	}
}

func (g *gen) emitConst(d *ir.Const) {
	// #define rather than a const: it carries no storage, and it works where a
	// const int does not — array bounds, case labels, other #defines.
	g.pf("#define %s %s%s\n", screaming(d.Name), g.constValue(d), g.foldComment(d.Expr))
	g.emitted[d.Name] = true
}

func (g *gen) constValue(d *ir.Const) string {
	if d.IsFloat {
		return formatFloat(d.Float)
	}
	if d.Int == nil {
		return "0"
	}
	// parenthesized so a #define used inside an expression cannot reassociate
	return "(" + g.renderInt(d.Expr, d.Int) + ")"
}

func formatFloat(v float64) string {
	// %v gives 8.388608e+06 for a large integral value; the plain decimal is
	// the same double and reads like the bound it is.
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return fmt.Sprintf("%.1f", v)
	}
	s := fmt.Sprintf("%v", v)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// formatFloat32 prints a float32 quantity as a C float literal: the shortest
// decimal that parses back to exactly this float32, f-suffixed because the
// value IS a float32 — the precomputed compressed-float constants (delta, min)
// are outputs of float32 arithmetic and the wire depends on their exact bits,
// so the literal must reproduce them, not the real-number declaration. Same
// discipline as the C++ backend's single-precision formatFloat.
func formatFloat32(v float32) string {
	s := strconv.FormatFloat(float64(v), 'g', -1, 32)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s + "f"
}

func (g *gen) emitEnum(d *ir.Enum) {
	g.pf("\n/* enum %s — None = 0 implicit, variants dense from 1, wire range [0, %d]\n", d.Name, d.Max)
	g.pf("   (SPEC §4.2). A fixed-width typedef rather than a C enum: an enum's underlying\n")
	g.pf("   type is implementation-defined, and | max = K headroom makes non-variant\n")
	g.pf("   values wire-legal, which a C enum cannot hold honestly. */\n")
	g.pf("typedef %s %s;\n", cUint(d.StorageBits), d.Name)
	g.pf("#define %s_NONE 0\n", screaming(d.Name))
	for i, v := range d.Variants {
		g.pf("#define %s_%s %d\n", screaming(d.Name), screaming(v), i+1)
	}
	g.pf("#define %s_MAX %d\n", screaming(d.Name), d.Max)

	// the debug/log name, the counterpart of C++'s and Go's EnumName
	g.pf("\n/* Debug/log name for any %s value, out-of-set included. */\n", d.Name)
	g.pf("static SCHEMA_UNUSED const char * enum_name_%s( %s value )\n{\n", snake(d.Name), d.Name)
	g.pf("    switch ( value )\n    {\n")
	g.pf("        case %s_NONE: return \"None\";\n", screaming(d.Name))
	for _, v := range d.Variants {
		g.pf("        case %s_%s: return %q;\n", screaming(d.Name), screaming(v), v)
	}
	g.pf("        default: return \"???\";\n    }\n}\n\n")
}

func (g *gen) emitFlags(d *ir.Flags) {
	g.pf("\n/* flags %s — one bit per variant, consumed as masks; storage uint64 in every\n", d.Name)
	g.pf("   target, wire %d bits (SPEC §4.2) */\n", d.WireBits)
	g.pf("typedef uint64_t %s;\n", d.Name)
	for i, v := range d.Variants {
		g.pf("#define %s_%s (1ULL << %d)\n", screaming(d.Name), screaming(v), i)
	}
	g.pf("#define %s_COUNT %d /* the declared variant count (SPEC §4.2) */\n", screaming(d.Name), len(d.Variants))
	g.pf("\n")

	g.pf("/* Debug/log name for bit i of %s — out-of-range bits name as \"???\". */\n", d.Name)
	g.pf("static SCHEMA_UNUSED const char * flag_name_%s( int bit )\n{\n", snake(d.Name))
	g.pf("    switch ( bit )\n    {\n")
	for i, v := range d.Variants {
		g.pf("        case %d: return %q;\n", i, v)
	}
	g.pf("        default: return \"???\";\n    }\n}\n\n")

	g.emitFlagAppendHelpers()
	g.pf("/* Renders the set bits of value into buffer as \"A|B\" — \"0\" for the empty\n")
	g.pf("   set, bits past the declared variants as hex — NUL-terminates and returns\n")
	g.pf("   buffer; %s_NAMES_MAX bytes always suffice. */\n", screaming(d.Name))
	g.pf("#define %s_NAMES_MAX %d\n", screaming(d.Name), flagNamesMax(d))
	g.pf("static SCHEMA_UNUSED const char * flag_names_%s( uint64_t value, char * buffer, int buffer_size )\n{\n", snake(d.Name))
	g.pf("    int position = 0;\n")
	for i := range d.Variants {
		g.pf("    if ( value & ( 1ULL << %d ) )\n    {\n        position = schema_flag_append_( buffer, buffer_size, position, flag_name_%s( %d ) );\n    }\n", i, snake(d.Name), i)
	}
	if len(d.Variants) < 64 { // a 64-variant set has no room for unknown bits
		g.pf("    if ( value >> %d )\n    {\n        position = schema_flag_append_hex_( buffer, buffer_size, position, ( value >> %d ) << %d );\n    }\n", len(d.Variants), len(d.Variants), len(d.Variants))
	}
	g.pf("    if ( position == 0 )\n    {\n        position = schema_flag_append_( buffer, buffer_size, position, \"0\" );\n    }\n")
	g.pf("    if ( position < buffer_size )\n    {\n        buffer[position] = '\\0';\n    }\n")
	g.pf("    return buffer;\n}\n\n")
}

// flagNamesMax is the buffer size that always holds a rendered flag set: every
// name plus its separator, the hex form of the residual bits, and the NUL.
func flagNamesMax(d *ir.Flags) int {
	n := 0
	for _, v := range d.Variants {
		n += len(v) + 1
	}
	return n + len("|0x") + 16 + 1
}

// emitFlagAppendHelpers emits the bounded append pair the flag_names_*
// renderers share, once per file — guarded like the SCHEMA_UNUSED macro
// because two data headers can land in one translation unit.
func (g *gen) emitFlagAppendHelpers() {
	if g.saidFlagAppend {
		return
	}
	g.saidFlagAppend = true
	g.pf("#ifndef SCHEMA_FLAG_APPEND_DEFINED\n#define SCHEMA_FLAG_APPEND_DEFINED\n")
	g.pf("static SCHEMA_UNUSED int schema_flag_append_( char * buffer, int buffer_size, int position, const char * name )\n{\n")
	g.pf("    if ( position > 0 && position < buffer_size - 1 )\n    {\n        buffer[position++] = '|';\n    }\n")
	g.pf("    while ( *name && position < buffer_size - 1 )\n    {\n        buffer[position++] = *name++;\n    }\n")
	g.pf("    return position;\n}\n\n")
	g.pf("static SCHEMA_UNUSED int schema_flag_append_hex_( char * buffer, int buffer_size, int position, uint64_t bits )\n{\n")
	g.pf("    int shift = 60;\n")
	g.pf("    position = schema_flag_append_( buffer, buffer_size, position, \"0x\" );\n")
	g.pf("    while ( shift > 0 && ( ( bits >> shift ) & 0xf ) == 0 )\n    {\n        shift -= 4;\n    }\n")
	g.pf("    for ( ; shift >= 0; shift -= 4 )\n    {\n")
	g.pf("        if ( position < buffer_size - 1 )\n        {\n            buffer[position++] = \"0123456789abcdef\"[( bits >> shift ) & 0xf];\n        }\n    }\n")
	g.pf("    return position;\n}\n#endif /* SCHEMA_FLAG_APPEND_DEFINED */\n\n")
}

func (g *gen) emitStruct(d *ir.Struct) {
	g.pf("\n/* type %s */\n", d.Name)
	if len(d.Fields) == 0 {
		// C forbids an empty struct; one padding byte keeps sizeof() legal and
		// the type usable. It carries no wire bits.
		g.pf("typedef struct %s {\n    char unused_; /* C has no empty struct; carries no wire bits */\n} %s;\n\n", d.Name, d.Name)
		return
	}
	g.pf("typedef struct %s {\n", d.Name)
	for _, f := range d.Fields {
		g.emitField(f)
	}
	g.pf("} %s;\n\n", d.Name)
	g.emitMaxBits(d)
	g.emitConstructor(d)
}

// emitUnion emits a first-class one-of (SPEC §4.8): the flat <NAME>_TYPE
// tag family (typedef + defines + debug-name function, the MessageType
// shape), then the tag-plus-named-union struct — the same shape the message
// dispatch uses. The selected arm is established ZEROED at selection
// (read_<name> memsets before decoding); bytes of unselected arms are
// indeterminate.
func (g *gen) emitUnion(d *ir.Union) {
	tag := d.Name + "Type"
	g.pf("\n/* union %s — first-class one-of (SPEC §4.8): the tag says which arm is\n", d.Name)
	g.pf("   live; None = 0 is the empty union, and the tag range is [0, %d]. */\n", d.Max)
	g.pf("typedef %s %s;\n", cUint(d.StorageBits), tag)
	g.pf("#define %s_NONE 0\n", screaming(tag))
	for i, v := range d.Variants {
		g.pf("#define %s_%s %d\n", screaming(tag), screaming(v.Name), i+1)
	}
	g.pf("#define %s_MAX %d\n", screaming(tag), d.Max)

	g.pf("\n/* Debug/log name for any %s value, out-of-set included. */\n", tag)
	g.pf("static SCHEMA_UNUSED const char * enum_name_%s( %s value )\n{\n", snake(tag), tag)
	g.pf("    switch ( value )\n    {\n")
	g.pf("        case %s_NONE: return \"None\";\n", screaming(tag))
	for i, v := range d.Variants {
		g.pf("        case %d: return %q;\n", i+1, ir.GoExportName(v.Name))
	}
	g.pf("        default: return \"???\";\n    }\n}\n\n")

	if len(d.Variants) == 0 {
		g.pf("/* An empty union holds only None; C forbids an empty union member, so the\n")
		g.pf("   struct is the tag alone. */\n")
		g.pf("typedef struct %s {\n    %s type;\n} %s;\n\n", d.Name, tag, d.Name)
	} else {
		g.pf("typedef struct %s {\n", d.Name)
		g.pf("    %s type;\n", tag)
		g.pf("    union {\n")
		for _, v := range d.Variants {
			g.pf("        %s %s;\n", v.Type, v.Name)
		}
		g.pf("    } as;\n")
		g.pf("} %s;\n\n", d.Name)
	}

	maxBits := ir.MaxBitsUnion(d)
	g.pf("#define %s_MAX_BITS %d   /* tag + the largest arm; None costs the tag only (SPEC §4.8) */\n", screaming(d.Name), maxBits)
	g.pf("#define %s_MAX_BYTES %d  %s\n\n", screaming(d.Name), ir.MaxBytes(maxBits), g.maxBytesTail())
}

// maxBytesTail is the comment after a MAX_BYTES define: the whole buffer
// contract on the file's first, the short form after.
func (g *gen) maxBytesTail() string {
	if g.saidReadSlack {
		return "/* 8-byte write granularity; read slack per the contract above */"
	}
	g.saidReadSlack = true
	return "/* rounded up to the 8-byte write-buffer granularity; a READ buffer's allocation must extend at least 8 bytes past the data — serialize.c loads 64-bit windows */"
}

func (g *gen) emitField(f *ir.Field) {
	name := f.Name
	switch {
	case f.Type.Kind == ir.TString:
		// N + 1: string(N) admits a length of N on the wire, and the reader
		// appends a terminator the wire does not carry. Sizing this at N is a
		// one-byte out-of-bounds WRITE driven by wire data -- the read path is
		// where hostile input arrives, so this is the array that must be right.
		g.pf("    char %s[%s + 1]; /* string(%s): N + 1 for the terminator the wire does not carry */\n",
			name, g.renderInt(f.Type.SizeExpr, big.NewInt(f.Type.Size)), ir.RenderExpr(f.Type.SizeExpr))
		g.pf("    int32_t %s_length;\n", name)
	case f.Type.Kind == ir.TBytes:
		g.pf("    uint8_t %s[%s];\n", name, g.renderInt(f.Type.SizeExpr, big.NewInt(f.Type.Size)))
		g.pf("    int32_t %s_length;\n", name)
	case f.Array == ir.ArrayFixed:
		g.pf("    %s %s[%s];\n", g.storageType(f), name, g.renderInt(f.ArrayExpr, big.NewInt(f.ArrayBound)))
	case f.Array == ir.ArrayCounted:
		g.pf("    %s %s[%s];\n", g.storageType(f), name, g.renderInt(f.ArrayExpr, big.NewInt(f.ArrayBound)))
		g.pf("    int32_t %s_count;\n", name)
	default:
		g.pf("    %s %s;\n", g.storageType(f), name)
	}
}

func (g *gen) storageType(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		// no <stdbool.h>: the floor is C89, where bool does not exist
		return "int"
	case ir.TInt:
		if f.Type.Width == 128 {
			// C has no int128_t. serialize.c models it as two 64-bit lanes,
			// which is also what makes the wire representation-independent.
			g.needs128 = true
			if f.Type.Signed {
				return "serialize_int128_t"
			}
			return "serialize_uint128_t"
		}
		return cInt(f.Type.Signed, f.Type.Width)
	case ir.TBits:
		if f.Type.Width <= 32 {
			return "uint32_t"
		}
		return "uint64_t"
	case ir.TFloat32:
		return "float"
	case ir.TFloat64:
		return "double"
	case ir.TFixed:
		// the raw scaled integer, in storage of exactly I + F bits with the
		// type's own signedness -- serialize_fixed's convention (STANDARD.md,
		// fixed)
		if f.Type.Width == 128 {
			g.needs128 = true
			if f.Type.Signed {
				return "serialize_int128_t"
			}
			return "serialize_uint128_t"
		}
		return cInt(f.Type.Signed, f.Type.Width)
	case ir.TNamed:
		return f.Type.Name
	}
	g.unsupported("field %s has type kind %v, which has no C storage mapping", f.Name, f.Type.Kind)
	return "int32_t"
}

func cInt(signed bool, width int) string {
	if signed {
		return fmt.Sprintf("int%d_t", width)
	}
	return fmt.Sprintf("uint%d_t", width)
}

func cUint(bits int) string {
	return fmt.Sprintf("uint%d_t", bits)
}

// ---- naming ----

// screaming converts a schema identifier to SCREAMING_SNAKE_CASE — the SAME
// word split Rust uses (ir.RustConstName), because the checker's claimed-name
// registry stores one flat spelling for both flat-snake targets. The local
// split this used to be disagreed on consecutive capitals (MaxHP became
// MAX_H_P here and MAX_HP in Rust) and only the Rust spelling was registered,
// so the C emission could collide unchecked.
func screaming(name string) string {
	return ir.RustConstName(name)
}

// snake converts PascalCase or snake_case to snake_case — ir.RustSnake, the
// registry's one flat spelling, for the same reason as screaming.
func snake(name string) string {
	return ir.RustSnake(name)
}

// sortedDeps makes the include list deterministic — generated output must be
// byte-identical wherever the compiler runs (SPEC §6.1).
func sortedDeps(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for d := range set {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// ---- wire header ----

func (g *gen) emitWireHeader() {
	// same-file constants are pre-marked emitted — they live in the data
	// header this wire header includes, so symbolic references stay
	// renderable rather than folding to literals (the C++ backend's
	// emitWireFile does the same)
	g.emitted = map[string]bool{}
	for _, d := range g.file.Decls {
		if c, ok := d.(*ir.Const); ok {
			g.emitted[c.Name] = true
		}
	}

	if g.fileEmitsWire() {
		// the calling convention, once per wire header — the per-function
		// comments below it stay one line each
		g.pf("/* Every write_x/read_x returns 1 on success, 0 on failure — the stream\n")
		g.pf("   latches the error, so a caller may check once at the end of a message.\n")
		g.pf("   Reads REFUSE out-of-range values, never clamp. A tag is validated BEFORE\n")
		g.pf("   it rides, and a read zero-establishes the selected arm before decoding\n")
		g.pf("   it (SPEC §4.8, §5). */\n\n")
		g.emitSpineInlineMacros()
	}
	if fileHasStrings(g.file) {
		g.emitUtf8Validator()
		g.emitInteriorNullScan()
	}
	// emission order for the same reason as the data header: write_a calls
	// write_b, and C99 has no implicit declarations
	for _, d := range ir.EmissionOrder(g.file) {
		switch decl := d.(type) {
		case *ir.Struct:
			g.emitWriteFunc(decl)
			g.emitReadFunc(decl)
		case *ir.Union:
			g.emitUnionWire(decl)
		}
	}
}

// emitUnionWire emits the union's write/read pair (SPEC §4.8): the write
// validates the tag BEFORE it rides (the message dispatch rule — an
// out-of-set tag writes nothing), the read rejects a tag above the count and
// memsets exactly the selected arm before decoding it (§5).
func (g *gen) emitUnionWire(d *ir.Union) {
	tag := d.Name + "Type"
	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(d.Max))

	g.pf("/* Writes %s. */\n", d.Name)
	g.pf("static SCHEMA_UNUSED SCHEMA_C_WRITE_INLINE int write_%s( serialize_write_stream_t * stream, const %s * value )\n{\n", snake(d.Name), d.Name)
	if d.Max == 0 {
		g.pf("    (void) stream; /* only None exists; the degenerate tag range [0, 0] costs zero bits */\n")
		g.pf("    return value->type == %s_NONE;\n}\n\n", screaming(tag))
	} else {
		g.pf("    if ( value->type > %s_MAX )\n    {\n        return 0; /* not a %s value; nothing was written */\n    }\n", screaming(tag), tag)
		g.call("    ", fmt.Sprintf("serialize_write_bits( stream, (serialize_uint32_t) value->type, %d )", bits))
		g.pf("    switch ( value->type )\n    {\n")
		for i, v := range d.Variants {
			g.pf("        case %d:\n            return write_%s( stream, &value->as.%s );\n", i+1, snake(v.Type), v.Name)
		}
		g.pf("        default:\n            return 1; /* None — the tag is the whole wire (SPEC §4.8) */\n")
		g.pf("    }\n}\n\n")
	}

	g.pf("/* Reads %s. */\n", d.Name)
	g.pf("static SCHEMA_UNUSED SCHEMA_C_READ_INLINE int read_%s( serialize_read_stream_t * stream, %s * value )\n{\n", snake(d.Name), d.Name)
	if d.Max == 0 {
		g.pf("    (void) stream; /* zero wire bits — only None exists */\n")
		g.pf("    value->type = %s_NONE;\n    return 1;\n}\n\n", screaming(tag))
		return
	}
	g.pf("    {\n        serialize_uint32_t tag_value = 0;\n")
	g.call("        ", fmt.Sprintf("serialize_read_bits( stream, &tag_value, %d )", bits))
	g.pf("        if ( tag_value > %s_MAX )\n        {\n            return 0; /* not a wire-legal tag */\n        }\n", screaming(tag))
	g.pf("        value->type = (%s) tag_value;\n    }\n", tag)
	g.pf("    switch ( value->type )\n    {\n")
	for i, v := range d.Variants {
		g.pf("        case %d:\n", i+1)
		g.pf("            memset( &value->as.%s, 0, sizeof( value->as.%s ) );\n", v.Name, v.Name)
		g.pf("            return read_%s( stream, &value->as.%s );\n", snake(v.Type), v.Name)
	}
	g.pf("        default:\n            return 1; /* None */\n")
	g.pf("    }\n}\n\n")
}

func (g *gen) emitWriteFunc(st *ir.Struct) {
	// fixed [N]uint8 arrays at statically byte-aligned positions take
	// serialize.c's bulk-bytes path instead of a per-byte loop — the bulk
	// call's internal align contributes zero bits at a boundary, so the wire
	// is byte-identical (the same switch, off the same analysis, as the C++
	// backend)
	g.bulkBytes = ir.AlignedFixedByteArrays(st)
	g.pf("/* Writes %s. */\n", st.Name)
	g.pf("static SCHEMA_UNUSED SCHEMA_C_WRITE_INLINE int write_%s( serialize_write_stream_t * stream, const %s * value )\n{\n", snake(st.Name), st.Name)
	// The early-out is keyed on ITEMS, not fields: a struct whose only items
	// are reserved/const/align has no storage but DOES have wire bits, and
	// keying on fields made C write nothing where C++ wrote the reserved
	// bits — a silent cross-language wire divergence (found by
	// FuzzGeneratedCompiles).
	if len(st.Items) == 0 {
		g.pf("    (void) stream;\n    (void) value;\n    return 1; /* no fields: no wire bits */\n}\n\n")
		return
	}
	if len(st.Fields) == 0 {
		g.pf("    (void) value; /* items only — reserved/const/align carry no storage */\n")
	}
	if ir.MaxBitsStruct(st) == 0 {
		// every range degenerate: the body refuses out-of-contract values but
		// never touches the stream (zero wire bits — found by
		// FuzzGeneratedCompiles)
		g.pf("    (void) stream; /* zero wire bits */\n")
	}
	g.emitWriteItems(st.Items, "    ")
	g.pf("    return 1;\n}\n\n")
}

func (g *gen) emitReadFunc(st *ir.Struct) {
	g.bulkBytes = ir.AlignedFixedByteArrays(st) // see emitWriteFunc
	g.pf("/* Reads %s. */\n", st.Name)
	g.pf("static SCHEMA_UNUSED SCHEMA_C_READ_INLINE int read_%s( serialize_read_stream_t * stream, %s * value )\n{\n", snake(st.Name), st.Name)
	if len(st.Items) == 0 {
		g.pf("    (void) stream;\n    (void) value;\n    return 1;\n}\n\n")
		return
	}
	if len(st.Fields) == 0 {
		g.pf("    (void) value; /* items only — reserved/const/align carry no storage */\n")
	}
	if ir.MaxBitsStruct(st) == 0 {
		g.pf("    (void) stream; /* zero wire bits — defaults prefill below */\n")
	}
	g.emitReadItems(st.Items, "    ")
	g.pf("    return 1;\n}\n\n")
}

func (g *gen) emitWriteItems(items []ir.Item, ind string) {
	for _, item := range items {
		switch n := item.(type) {
		case *ir.FieldItem:
			g.emitWriteField(n.F, ind)
		case *ir.ConstItem:
			g.call(ind, fmt.Sprintf("serialize_write_bits( stream, (serialize_uint32_t) %sULL, %d ) /* const(%s, %d) — SPEC §4.3 */", n.Value.String(), n.Bits, n.Value.String(), n.Bits))
		case *ir.ReservedItem:
			g.call(ind, fmt.Sprintf("serialize_write_bits( stream, 0, %d ) /* reserved(%d) — zeros on the wire */", n.Bits, n.Bits))
		case *ir.AlignItem:
			g.call(ind, "serialize_write_align( stream )")
		case *ir.Branch:
			cond := "value->" + n.Cond
			if n.Neg {
				cond = "!" + cond
			}
			g.pf("%sif ( %s )\n%s{\n", ind, cond, ind)
			g.emitWriteItems(n.Then, ind+"    ")
			g.pf("%s}\n", ind)
			if len(n.Else) > 0 {
				g.pf("%selse\n%s{\n", ind, ind)
				g.emitWriteItems(n.Else, ind+"    ")
				g.pf("%s}\n", ind)
			}
		}
	}
}

func (g *gen) emitReadItems(items []ir.Item, ind string) {
	for _, item := range items {
		switch n := item.(type) {
		case *ir.FieldItem:
			g.emitReadField(n.F, ind)
		case *ir.ConstItem:
			// the constant must be exactly what the writer sent; a mismatch is
			// a desynchronized stream, refused rather than ignored
			g.pf("%s{\n%s    serialize_uint32_t const_value = 0;\n", ind, ind)
			g.call(ind+"    ", fmt.Sprintf("serialize_read_bits( stream, &const_value, %d )", n.Bits))
			g.pf("%s    if ( const_value != (serialize_uint32_t) %sULL )\n%s    {\n%s        return 0;\n%s    }\n%s}\n",
				ind, n.Value.String(), ind, ind, ind, ind)
		case *ir.ReservedItem:
			g.pf("%s{\n%s    serialize_uint32_t reserved_value = 0;\n", ind, ind)
			g.call(ind+"    ", fmt.Sprintf("serialize_read_bits( stream, &reserved_value, %d )", n.Bits))
			g.pf("%s    if ( reserved_value != 0 )\n%s    {\n%s        return 0;\n%s    }\n%s}\n", ind, ind, ind, ind, ind)
		case *ir.AlignItem:
			g.call(ind, "serialize_read_align( stream )")
		case *ir.Branch:
			cond := "value->" + n.Cond
			if n.Neg {
				cond = "!" + cond
			}
			// SPEC §5: a read zeroes the UNTAKEN side, so storage never
			// carries a value the wire did not supply. Both sides need it --
			// zeroing only the then-fields in the else arm leaves the
			// else-fields holding whatever the caller's memory had when the
			// then arm is taken, which is exactly the stale-data bug the rule
			// exists to prevent.
			g.pf("%sif ( %s )\n%s{\n", ind, cond, ind)
			g.emitReadItems(n.Then, ind+"    ")
			g.emitZeroItems(n.Else, ind+"    ")
			g.pf("%s}\n", ind)
			g.pf("%selse\n%s{\n", ind, ind)
			if len(n.Else) > 0 {
				g.emitReadItems(n.Else, ind+"    ")
			}
			g.emitZeroItems(n.Then, ind+"    ")
			g.pf("%s}\n", ind)
		}
	}
}

func (g *gen) emitZeroItems(items []ir.Item, ind string) {
	for _, item := range items {
		switch n := item.(type) {
		case *ir.FieldItem:
			g.emitZeroField(n.F, ind)
		case *ir.Branch:
			g.emitZeroItems(n.Then, ind)
			g.emitZeroItems(n.Else, ind)
		}
	}
}

func (g *gen) emitZeroField(f *ir.Field, ind string) {
	switch {
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		g.pf("%smemset( value->%s, 0, sizeof( value->%s ) );\n", ind, f.Name, f.Name)
		g.pf("%svalue->%s_length = 0;\n", ind, f.Name)
	case f.Array != ir.ArrayNone:
		g.pf("%smemset( value->%s, 0, sizeof( value->%s ) );\n", ind, f.Name, f.Name)
		if f.Array == ir.ArrayCounted {
			g.pf("%svalue->%s_count = 0;\n", ind, f.Name)
		}
	case f.Type.Kind == ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Struct:
			g.pf("%smemset( &value->%s, 0, sizeof( value->%s ) );\n", ind, f.Name, f.Name)
			return
		case *ir.Union:
			/* zero IS None: the tag is sentinel-zero (SPEC §4.8) */
			g.pf("%smemset( &value->%s, 0, sizeof( value->%s ) );\n", ind, f.Name, f.Name)
			return
		}
		g.pf("%svalue->%s = 0;\n", ind, f.Name)
	case f.Type.Width == 128:
		// serialize_int128_t / serialize_uint128_t are STRUCTS in C, so the
		// zero form is a memset rather than an assignment
		g.pf("%smemset( &value->%s, 0, sizeof( value->%s ) );\n", ind, f.Name, f.Name)
	default:
		g.pf("%svalue->%s = 0;\n", ind, f.Name)
	}
}
