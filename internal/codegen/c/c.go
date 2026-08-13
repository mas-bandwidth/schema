// Package c emits the C target: one header/source pair per schema file —
// Types.schema -> Types.h and Types.c — targeting the serialize.c runtime.
//
// WHAT IS DIFFERENT ABOUT THIS TARGET
//
// C++ expresses reading and writing once, as a function templated over a write
// stream, a read stream or a measure stream. C has no templates, so this target
// emits SEPARATE write_X and read_X functions. That is the same trade the
// serialize.c runtime makes, and the reason a code generator earns its keep
// here more than anywhere else: the two halves cannot drift when one
// declaration produces both.
//
// CONVENTIONS, decided here because SPEC had no C column (2026-08-13)
//
//   - Constants are #define, on Glenn's direction. A #define carries no storage
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
//     storage-width contract in §6.1 — and headroom from [max = K] makes
//     non-variant values wire-legal, which a C enum cannot represent honestly
//     anyway. Same reasoning as the Rust newtype.
package c

import (
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/internal/ir"
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
	home := protocolIdHome(u)
	msgOwner := ir.MessageOwner(u)
	objOwner := ir.ObjectOwner(u)
	deps := ir.FileDeps(u)

	var errs []error
	for _, f := range u.Files {
		g := &gen{unit: u, file: f, msgOwner: msgOwner, objOwner: objOwner, deps: sortedDeps(deps[f.Base])}
		g.emitDataHeader(f.Base == home)
		out[f.Base+".h"] = g.assembleHeader()
		errs = append(errs, g.errs...)

		w := &gen{unit: u, file: f, msgOwner: msgOwner, objOwner: objOwner, deps: sortedDeps(deps[f.Base]), wire: true}
		w.emitWireHeader()
		out[f.Base+"Wire.h"] = w.assembleWireHeader()
		errs = append(errs, w.errs...)
	}

	tables, terr := GenerateTable(u)
	if terr != nil {
		errs = append(errs, terr)
	}
	for name, data := range tables {
		out[name] = data
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

func protocolIdHome(u *ir.Unit) string {
	for _, f := range u.Files {
		if f.Base == "Constants" {
			return f.Base
		}
	}
	if len(u.Files) > 0 {
		return u.Files[0].Base
	}
	return ""
}

type gen struct {
	unit     *ir.Unit
	errs     []error
	needs128 bool // a 128-bit or Q112.16 storage member needs serialize.h in the DATA header

	emitMessagesPending bool // this file owns the message dispatch surface
	file     *ir.File
	msgOwner string
	objOwner string
	deps     []string
	wire     bool
	body     strings.Builder
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
	h.WriteString("#include <stdint.h>\n#include <string.h>   /* memset — the zero form (SPEC §4.2) */\n#include <math.h>     /* floor — the quantize pair */\n")
	if g.needs128 {
		// serialize_int128_t / serialize_uint128_t are STORAGE here, so the
		// runtime header has to reach the data header and not only the wire one
		h.WriteString("#include \"serialize.h\"   /* serialize_int128_t: C has no 128-bit builtin */\n")
	}
	h.WriteString(unusedMacro)
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
	h.WriteString("#include <string.h>   /* memset, strlen */\n#include \"serialize.h\"\n")
	h.WriteString(unusedMacro)
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
	fmt.Fprintf(h, "/* Generated by the schema compiler from %s.schema. DO NOT EDIT.\n", base)
	h.WriteString("   SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	h.WriteString("   your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	h.WriteString("   AGPL-3.0, its output is not.\n")
	fmt.Fprintf(h, "   package %s — protocol id 0x%016x */\n\n", g.unit.Package, g.unit.ProtocolId)
}

// ---- data header ----

func (g *gen) emitDataHeader(carriesProtocolId bool) {
	if carriesProtocolId {
		g.pf("/* The unit's protocol id — the hash of its schema files (SPEC §3.1). Two\n")
		g.pf("   sides at the same id speak identical bits; there is no other versioning. */\n")
		g.pf("#define %s_PROTOCOL_ID 0x%016xULL\n\n", screaming(g.unit.Package), g.unit.ProtocolId)
	}

	if g.file.Base == g.msgOwner && len(g.unit.Messages) > 0 {
		g.emitMessagesPending = true
	}

	for _, d := range g.file.Decls {
		switch decl := d.(type) {
		case *ir.Const:
			g.emitConst(decl)
		case *ir.Enum:
			g.emitEnum(decl)
		case *ir.Flags:
			g.emitFlags(decl)
		case *ir.Struct:
			g.emitStruct(decl)
		case *ir.Object:
			g.emitObject(decl)
		case *ir.ContextsMarker:
			g.pf("/* contexts declared for this unit: %s (SPEC §4.2).\n", strings.Join(decl.Names, ", "))
			g.pf("   Contexts generate no standalone artifacts — where an object carries\n")
			g.pf("   context-scoped [local] fields, its State struct is generated once per\n")
			g.pf("   context (ClientShipState, ServerShipState, ...), each holding the `all`\n")
			g.pf("   fields plus its own context's. No preprocessor in any target. */\n\n")
		default:
			g.unsupported("declaration kind %T in %s.schema has no C emission", decl, g.file.Base)
		}
	}

	// the message tag and union come after the arms they name
	if g.emitMessagesPending {
		g.emitMessageTypes()
	}
}

func (g *gen) emitConst(d *ir.Const) {
	// #define rather than a const: it carries no storage, and it works where a
	// const int does not — array bounds, case labels, other #defines.
	g.pf("#define %s %s\n", screaming(d.Name), constValue(d))
}

func constValue(d *ir.Const) string {
	if d.IsFloat {
		return formatFloat(d.Float)
	}
	if d.Int == nil {
		return "0"
	}
	// parenthesised so a #define used inside an expression cannot reassociate
	return "(" + d.Int.String() + ")"
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

func (g *gen) emitEnum(d *ir.Enum) {
	g.pf("\n/* enum %s — None = 0 implicit, variants dense from 1, wire range [0, %d]\n", d.Name, d.Max)
	g.pf("   (SPEC §4.2). A fixed-width typedef rather than a C enum: an enum's underlying\n")
	g.pf("   type is implementation-defined, and [max = K] headroom makes non-variant\n")
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
	for i, v := range d.Variants {
		g.pf("        case %d: return %q;\n", i+1, v)
	}
	g.pf("        default: return \"???\";\n    }\n}\n\n")

	// The wire-value form the table reflection descriptors hold. They store a
	// uint64_t-taking function pointer, so the typed one above cannot be used
	// directly -- a function pointer conversion between differing parameter
	// types is undefined behaviour, not merely a warning.
	g.pf("/* As enum_name_%s, over a raw wire value — the form the table reflection\n", snake(d.Name))
	g.pf("   descriptors hold. */\n")
	g.pf("static SCHEMA_UNUSED const char * enum_name_%s_dyn( uint64_t value )\n{\n", snake(d.Name))
	g.pf("    switch ( value )\n    {\n")
	g.pf("        case 0: return \"None\";\n")
	for i, v := range d.Variants {
		g.pf("        case %d: return %q;\n", i+1, v)
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
	g.pf("\n")
}

func (g *gen) emitStruct(d *ir.Struct) {
	kind := "type"
	if d.IsMessage {
		kind = "message"
	}
	g.pf("\n/* %s %s */\n", kind, d.Name)
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

func (g *gen) emitField(f *ir.Field) {
	name := f.Name
	switch {
	case f.Type.Kind == ir.TString:
		// N + 1: string(N) admits a length of N on the wire, and the reader
		// appends a terminator the wire does not carry. Sizing this at N is a
		// one-byte out-of-bounds WRITE driven by wire data -- the read path is
		// where hostile input arrives, so this is the array that must be right.
		g.pf("    char %s[%d]; /* string(%d): N + 1 for the terminator the wire does not carry */\n", name, f.Type.Size+1, f.Type.Size)
		g.pf("    int32_t %s_length;\n", name)
	case f.Type.Kind == ir.TBytes:
		g.pf("    uint8_t %s[%d];\n", name, f.Type.Size)
		g.pf("    int32_t %s_length;\n", name)
	case f.Array == ir.ArrayFixed:
		g.pf("    %s %s[%d];\n", g.storageType(f), name, f.ArrayBound)
	case f.Array == ir.ArrayCounted:
		g.pf("    %s %s[%d];\n", g.storageType(f), name, f.ArrayBound)
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
		// the raw scaled integer, in SIGNED storage of exactly I + F bits --
		// serialize_fixed's own convention (STANDARD.md, fixed)
		if f.Type.Width == 128 {
			g.needs128 = true
			return "serialize_int128_t"
		}
		return cInt(true, f.Type.Width)
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

// screaming converts a schema identifier to SCREAMING_SNAKE_CASE.
func screaming(name string) string {
	return strings.ToUpper(snake(name))
}

// snake converts PascalCase or snake_case to snake_case.
func snake(name string) string {
	var b strings.Builder
	for i, r := range name {
		if r >= 'A' && r <= 'Z' {
			if i > 0 && name[i-1] != '_' {
				b.WriteByte('_')
			}
			b.WriteRune(r - 'A' + 'a')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
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

var _ = big.NewInt

// ---- wire header ----

func (g *gen) emitWireHeader() {
	for _, d := range g.file.Decls {
		switch decl := d.(type) {
		case *ir.Struct:
			g.emitWriteFunc(decl)
			g.emitReadFunc(decl)
		case *ir.Object:
			g.emitObjectFunctions(decl)
		}
	}

	if g.file.Base == g.msgOwner && len(g.unit.Messages) > 0 {
		g.emitMessageDispatch()
	}
}

func (g *gen) emitWriteFunc(st *ir.Struct) {
	g.pf("/* Writes %s. Returns 1 on success, 0 on failure — the stream latches the\n", st.Name)
	g.pf("   error, so a caller may check once at the end of a message. */\n")
	g.pf("static SCHEMA_UNUSED int write_%s( serialize_write_stream_t * stream, const %s * value )\n{\n", snake(st.Name), st.Name)
	if len(st.Fields) == 0 {
		g.pf("    (void) stream;\n    (void) value;\n    return 1; /* no fields: no wire bits */\n}\n\n")
		return
	}
	g.emitWriteItems(st.Items, "    ")
	g.pf("    return 1;\n}\n\n")
}

func (g *gen) emitReadFunc(st *ir.Struct) {
	g.pf("/* Reads %s. Returns 1 on success, 0 on failure. Out-of-range values are\n", st.Name)
	g.pf("   REFUSED, never clamped. */\n")
	g.pf("static SCHEMA_UNUSED int read_%s( serialize_read_stream_t * stream, %s * value )\n{\n", snake(st.Name), st.Name)
	if len(st.Fields) == 0 {
		g.pf("    (void) stream;\n    (void) value;\n    return 1;\n}\n\n")
		return
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
		if _, isStruct := f.Type.Ref.(*ir.Struct); isStruct {
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
