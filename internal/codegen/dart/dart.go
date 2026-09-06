// Package dart emits the Dart target: one .dart library per schema file —
// Constants.schema -> Constants.dart — deterministic to the byte (SPEC §6.1).
// There is no external formatter in the build path: the emitter writes clean
// 2-space-indented Dart directly, held to `dart format`'s own style (goldens
// pin it, and the test chain runs the analyzer and formatter as refusers).
//
// The target is Dart 3 on the VM and AOT (Flutter release ships AOT — the
// deployment that matters, issue #155). Generated code is SELF-CONTAINED:
// it imports dart:typed_data and its sibling generated files, never a
// runtime package. The serialize.dart port measured library-shaped code
// 5-6x off the C++ reference and located the gap in dispatch and per-call
// width arithmetic, so this backend emits what that port's findings
// prescribe (issue #155): the family bitpacker INLINED at every field with
// literal constant widths and masks, separate monomorphic write/read/measure
// functions per type, bounds checks fused per static run, and asserts (not
// branches) for writer contracts — active under --enable-asserts, compiled
// out in release, exactly the C++ trusted-writer stance.
//
// Storage is the §6.1 storage principle in Dart's value domain: classes with
// public fields, every field initialized at declaration, member names in
// lowerCamelCase — the first-letter-lowered form of the same
// ir.GoExportName mapping the Go/C#/JS targets share, a bijection on it, so
// the checker's existing collision detection covers Dart without a second
// registry. Dart's int is a signed 64-bit integer: every integer width
// through 64 stores in it bit-transparently (the serialize.dart value
// domain), fixed point stores the raw scaled integer, flags store uint64
// patterns, and the 128-bit widths store the emulated (hi, lo) pair — the
// Int128/UInt128 classes emitted into Int128.dart beside the unit exactly
// when a 128-bit field exists. Typed-data lists back every scalar array.
//
// Functions per type: write<Name>(value, view) -> bytes written (trusted
// writer, contracts asserted); read<Name>(value, view, numBits) -> bool (the
// family read verdict — reads validate EVERYTHING and never throw on hostile
// bytes); measure<Name>(value) -> exact wire bits (static runs folded to one
// literal at generation time); zero<Name>(value) -> the §5 zero form.
// Buffers are caller-owned ByteData views: write buffers hold <name>MaxBytes
// (a multiple of 8 — the family write-buffer granularity); read buffers need
// NO slack past the payload — the reader prices its 64-bit windows inside
// the buffer via the assembled tail window, serialize.dart's own stance.
package dart

import (
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/ast"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Generate returns basename.dart -> file contents for every file of the
// unit, plus Int128.dart when the unit has 128-bit storage.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	if err := checkNames(u); err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	home := ir.ProtocolIdHome(u)
	need128 := unitNeeds128(u)
	if need128 {
		if _, taken := u.DeclFile["Int128"]; taken {
			return nil, fmt.Errorf("declaration Int128 collides with the Int128.dart support library the Dart emitter writes beside a 128-bit unit; rename the declaration")
		}
		if _, taken := u.DeclFile["UInt128"]; taken {
			return nil, fmt.Errorf("declaration UInt128 collides with the Int128.dart support library the Dart emitter writes beside a 128-bit unit; rename the declaration")
		}
		for _, f := range u.Files {
			if f.Base == "Int128" {
				return nil, fmt.Errorf("schema file Int128.schema collides with the Int128.dart support library the Dart emitter writes beside a 128-bit unit; rename the file")
			}
		}
		out["Int128.dart"] = emitInt128Support(u)
	}
	for _, f := range u.Files {
		g := &gen{unit: u, file: f, home: f.Base == home, imports: map[string]map[string]bool{}}
		g.emitFile()
		out[f.Base+".dart"] = g.assemble()
	}
	return out, nil
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

// dartReserved is every spelling a generated Dart identifier must not take:
// the reserved words of Dart 3, plus the dart:core type names generated
// files use unqualified — shadowing `int` with a top-level constant would
// corrupt every declaration below it.
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
}

// dartName maps an exported member/constant/variant name into Dart's
// lowerCamelCase: the first-letter-lowered form of ir.GoExportName. The two
// spellings are a bijection, so the checker's PascalCase collision registry
// covers Dart members without a second registry.
func dartName(name string) string {
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

// checkNames refuses any declaration whose Dart spelling lands on a reserved
// word or a shadow-hazard core name — mangling silently would make the Dart
// surface diverge from every sibling target's naming.
func checkNames(u *ir.Unit) error {
	check := func(kind, owner, name, mapped string) error {
		if dartReserved[mapped] {
			if owner != "" {
				return fmt.Errorf("%s %s of %s maps to the reserved Dart identifier %q; rename it", kind, name, owner, mapped)
			}
			return fmt.Errorf("%s %s maps to the reserved Dart identifier %q; rename it", kind, name, mapped)
		}
		return nil
	}
	for _, c := range u.Consts {
		if err := check("const", "", c.Name, dartName(c.Name)); err != nil {
			return err
		}
	}
	// declaration names emit verbatim as class/extension names, so they are
	// exactly the surface the shadow-hazard entries (String, List, Object,
	// Endian, ByteData) exist for — a `type List` would shadow dart:core
	for _, e := range u.Enums {
		if err := check("enum", "", e.Name, e.Name); err != nil {
			return err
		}
	}
	for _, fl := range u.Flags {
		if err := check("flags", "", fl.Name, fl.Name); err != nil {
			return err
		}
	}
	for _, st := range u.Structs {
		if err := check("type", "", st.Name, st.Name); err != nil {
			return err
		}
	}
	for _, un := range u.Unions {
		if err := check("union", "", un.Name, un.Name); err != nil {
			return err
		}
	}
	for _, e := range u.Enums {
		for _, v := range e.Variants {
			if err := check("variant", e.Name, v, dartName(v)); err != nil {
				return err
			}
		}
	}
	for _, fl := range u.Flags {
		for _, v := range fl.Variants {
			if err := check("variant", fl.Name, v, dartName(fl.Name+v)); err != nil {
				return err
			}
		}
	}
	for _, st := range u.Structs {
		for _, f := range st.Fields {
			if err := check("field", st.Name, f.Name, dartName(f.Name)); err != nil {
				return err
			}
		}
	}
	for _, un := range u.Unions {
		for _, v := range un.Variants {
			if err := check("variant", un.Name, v.Name, dartName(v.Name)); err != nil {
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

	// imports collects cross-file references as file base -> shown symbol
	// set; a Dart library sees nothing ambient, so every cross-file name is
	// imported with a show list (the same edges ir.FileDeps tracks).
	imports map[string]map[string]bool

	// per-function emission state (functions.go)
	fn        strings.Builder
	loopDepth int
	needV     bool // the group value temp
	needLo    bool // the wide-lane accumulator (58..64-bit reads, 128 lanes)
	needHi    bool // the high 64-bit lane (129-bit-storage reads)
	usesRead  bool // read side: the window machinery is used

	// the write-side chunk accumulator: adjacent constant-width fields whose
	// value expressions are pure (field loads, literals — never a mutable
	// temp) pack into ONE staged word with literal relative shifts, so a
	// chunk costs one merge where its fields used to cost one each. Dart's
	// native 64-bit int carries the whole lane, so the cap is 64 where the
	// js flat tier's number domain held it to 32. Relative offsets inside a
	// chunk are static even where the absolute cursor is dynamic (loop
	// bodies), which is what makes this reach the hot arrays.
	chunk     []chunkPiece
	chunkBits int64

	// the read-side window ledger: bits still extractable from the loaded
	// 64-bit window (windowAvail) and the literal shift of the next field
	// inside it (windowRel). A load guarantees 57 valid bits (64 minus the
	// worst-case byte-interior shift), so consecutive constant-width reads
	// share one load instead of paying a load and a tail branch each.
	windowAvail int64
	windowRel   int64

	// per-file helper needs
	needFround   bool // _fround
	needF32Conv  bool // _float32BitsFromDouble / _doubleFromFloat32Bits
	needF64Conv  bool // _float64BitsFromDouble / _doubleFromFloat64Bits
	needULess    bool // _unsignedLessThan
	needHex      bool // _hex64 (flagNames high-bit rendering)
	needScratch  bool // the overlaid conversion scratch views
	usesTypeData bool // dart:typed_data is imported
}

func (g *gen) bpf(format string, args ...any) {
	fmt.Fprintf(&g.body, format, args...)
}

// addRef records that the current file references declaration decl (by the
// symbols listed), importing it when it lives in another file.
func (g *gen) addRef(decl string, symbols ...string) {
	base, ok := g.unit.DeclFile[decl]
	if !ok {
		return
	}
	if base == g.file.Base {
		return
	}
	set := g.imports[base]
	if set == nil {
		set = map[string]bool{}
		g.imports[base] = set
	}
	for _, s := range symbols {
		set[s] = true
	}
}

// addRef128 imports a symbol from the Int128.dart support library.
func (g *gen) addRef128(symbols ...string) {
	set := g.imports["Int128"]
	if set == nil {
		set = map[string]bool{}
		g.imports["Int128"] = set
	}
	for _, s := range symbols {
		set[s] = true
	}
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
		h.WriteString("// The shipped Dart wire path (issue #155): the serialize.dart bitpacker\n")
		h.WriteString("// inlined at every field, literal constant widths and masks, monomorphic\n")
		h.WriteString("// write/read/measure functions per type, zero runtime dependencies.\n")
		h.WriteString("//\n")
		h.WriteString("// write<Name>(value, view) -> bytes written. The writer TRUSTS the caller\n")
		h.WriteString("// (the C++ stance): contracts are asserted — active under --enable-asserts,\n")
		h.WriteString("// compiled out in release — and width masks stay, as wire arithmetic. The\n")
		h.WriteString("// buffer behind view must hold <name>MaxBytes (a multiple of 8).\n")
		h.WriteString("//\n")
		h.WriteString("// read<Name>(value, view, numBits) -> bool, the family read verdict. The\n")
		h.WriteString("// wire is a trust boundary: reads validate everything — bounds fused per\n")
		h.WriteString("// static run, ranges, wire constants, reserved and align padding, union\n")
		h.WriteString("// tags — and NEVER throw on hostile bytes. No slack past the payload is\n")
		h.WriteString("// required: the reader prices its 64-bit windows inside the buffer.\n")
		h.WriteString("//\n")
		h.WriteString("// measure<Name>(value) -> exact wire bits for that value (trusted, like\n")
		h.WriteString("// the writer). zero<Name>(value) -> the §5 zero form.\n")
	}
	h.WriteString("\n")
	if g.usesTypeData {
		h.WriteString("import 'dart:typed_data';\n\n")
	}
	if len(g.imports) > 0 {
		bases := make([]string, 0, len(g.imports))
		for b := range g.imports {
			bases = append(bases, b)
		}
		sort.Strings(bases)
		for _, b := range bases {
			syms := make([]string, 0, len(g.imports[b]))
			for s := range g.imports[b] {
				syms = append(syms, s)
			}
			sort.Strings(syms)
			g.writeImport(&h, b, syms)
		}
		h.WriteString("\n")
	}
	g.emitHelpers(&h)
	h.WriteString(strings.TrimRight(g.body.String(), "\n"))
	h.WriteString("\n")
	return []byte(h.String())
}

// writeImport renders one show-list import, wrapped the way dart format
// wraps it when the one-line form passes 80 columns.
func (g *gen) writeImport(h *strings.Builder, base string, syms []string) {
	one := fmt.Sprintf("import '%s.dart' show %s;", base, strings.Join(syms, ", "))
	if len(one) <= 80 {
		h.WriteString(one)
		h.WriteString("\n")
		return
	}
	fmt.Fprintf(h, "import '%s.dart'\n    show\n", base)
	for i, s := range syms {
		sep := ","
		if i == len(syms)-1 {
			sep = ";"
		}
		fmt.Fprintf(h, "        %s%s\n", s, sep)
	}
}

// emitHelpers writes the per-file private helpers the emitted bodies used.
func (g *gen) emitHelpers(h *strings.Builder) {
	if g.needScratch {
		// One view per conversion actually emitted: `dart analyze` refuses an
		// unreferenced private declaration, so a file that converts only
		// float64 must not carry the float32 view (and the corpus had no such
		// file until Degenerate.schema). _f32 owns the buffer, so it is
		// referenced whenever any view is.
		h.WriteString("// One 8-byte conversion scratch under overlaid typed-data views — single\n")
		h.WriteString("// threaded per isolate, always consumed in the same operation that fills it.\n")
		h.WriteString("final Float32List _f32 = Float32List(2);\n")
		if g.needF32Conv {
			h.WriteString("final Uint32List _u32 = _f32.buffer.asUint32List();\n")
		}
		if g.needF32Conv || g.needF64Conv {
			h.WriteString("final Float64List _f64 = _f32.buffer.asFloat64List();\n")
			h.WriteString("final Uint64List _u64 = _f32.buffer.asUint64List();\n")
		}
		h.WriteString("\n")
	}
	if g.needFround {
		h.WriteString("// value rounded to the nearest float32, as a double: the float32 rounding\n")
		h.WriteString("// boundary the compressed-float arithmetic is pinned to (SPEC §4.3).\n")
		h.WriteString("@pragma('vm:prefer-inline')\n")
		h.WriteString("double _fround(double value) {\n")
		h.WriteString("  _f32[0] = value;\n")
		h.WriteString("  return _f32[0];\n")
		h.WriteString("}\n\n")
	}
	if g.needF32Conv {
		h.WriteString("// The 32 bits of the IEEE-754 single representation of value. Non-NaN goes\n")
		h.WriteString("// through the hardware conversion; a NaN narrows in software — sign kept,\n")
		h.WriteString("// top 23 mantissa bits kept, the quiet bit forced only for the all-low-\n")
		h.WriteString("// payload case — so every pattern the read half produces round trips.\n")
		h.WriteString("@pragma('vm:prefer-inline')\n")
		h.WriteString("int _float32BitsFromDouble(double value) {\n")
		h.WriteString("  if (!value.isNaN) {\n")
		h.WriteString("    _f32[0] = value;\n")
		h.WriteString("    return _u32[0];\n")
		h.WriteString("  }\n")
		h.WriteString("  _f64[0] = value;\n")
		h.WriteString("  final bits64 = _u64[0];\n")
		h.WriteString("  final sign = (bits64 >>> 63) << 31;\n")
		h.WriteString("  var mantissa = (bits64 >>> 29) & 0x7fffff;\n")
		h.WriteString("  if (mantissa == 0) {\n")
		h.WriteString("    mantissa = 0x400000;\n")
		h.WriteString("  }\n")
		h.WriteString("  return sign | 0x7f800000 | mantissa;\n")
		h.WriteString("}\n\n")
		h.WriteString("// The double whose IEEE-754 single representation is bits. NaN patterns\n")
		h.WriteString("// widen in software (the hardware conversion quiets a signaling NaN); the\n")
		h.WriteString("// reader reproduces the transmitted pattern exactly.\n")
		h.WriteString("@pragma('vm:prefer-inline')\n")
		h.WriteString("double _doubleFromFloat32Bits(int bits) {\n")
		h.WriteString("  if ((bits & 0x7f800000) == 0x7f800000 && (bits & 0x7fffff) != 0) {\n")
		h.WriteString("    _u64[0] =\n")
		h.WriteString("        ((bits >>> 31) << 63) | 0x7ff0000000000000 | ((bits & 0x7fffff) << 29);\n")
		h.WriteString("    return _f64[0];\n")
		h.WriteString("  }\n")
		h.WriteString("  _u32[0] = bits;\n")
		h.WriteString("  return _f32[0];\n")
		h.WriteString("}\n\n")
	}
	if g.needF64Conv {
		h.WriteString("// The 64 bits of the IEEE-754 double representation of value — exactly the\n")
		h.WriteString("// bits of the Dart double — and the inverse.\n")
		h.WriteString("@pragma('vm:prefer-inline')\n")
		h.WriteString("int _float64BitsFromDouble(double value) {\n")
		h.WriteString("  _f64[0] = value;\n")
		h.WriteString("  return _u64[0];\n")
		h.WriteString("}\n\n")
		h.WriteString("@pragma('vm:prefer-inline')\n")
		h.WriteString("double _doubleFromFloat64Bits(int bits) {\n")
		h.WriteString("  _u64[0] = bits;\n")
		h.WriteString("  return _f64[0];\n")
		h.WriteString("}\n\n")
	}
	if g.needULess {
		h.WriteString("// Unsigned 64-bit less-than: flip the sign bit so the signed comparison\n")
		h.WriteString("// orders the unsigned domain (uint64 values ride bit-transparently in int).\n")
		h.WriteString("@pragma('vm:prefer-inline')\n")
		h.WriteString("bool _unsignedLessThan(int a, int b) =>\n")
		h.WriteString("    (a ^ 0x8000000000000000) < (b ^ 0x8000000000000000);\n\n")
	}
	if g.needHex {
		h.WriteString("// Hex of a uint64 bit pattern held in a signed int (toRadixString would\n")
		h.WriteString("// render the sign, not the pattern).\n")
		h.WriteString("String _hex64(int value) => value < 0\n")
		h.WriteString("    ? (value >>> 32).toRadixString(16) +\n")
		h.WriteString("          (value & 0xffffffff).toRadixString(16).padLeft(8, '0')\n")
		h.WriteString("    : value.toRadixString(16);\n\n")
	}
}

func (g *gen) emitFile() {
	if g.home {
		g.bpf("// The unit's protocol id — the hash of its wire shape (SPEC §3.1). Two\n")
		g.bpf("// sides at the same id speak identical bits; there is no other versioning.\n")
		g.bpf("const int protocolId = 0x%016x;\n\n", g.unit.ProtocolId)
	}

	// EmissionOrder, not declaration order: a top-level const initializer
	// referencing a later const is a compile error in Dart, exactly as in JS
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

// emitConst emits a schema const: int and double top-level constants in the
// lowerCamel spelling. Every integer storage through uint64 rides in int —
// values past int64 render as bit-transparent hex (Dart's unsigned spelling).
func (g *gen) emitConst(d *ir.Const) {
	name := dartName(d.Name)
	if d.IsFloat {
		if d.Storage == "float32" {
			g.bpf("const double %s = %s;%s\n\n", name, formatFloat32(d.Float), g.foldComment(d.Expr))
			return
		}
		g.bpf("const double %s = %s;%s\n\n", name, formatFloat(d.Float), g.foldComment(d.Expr))
		return
	}
	g.bpf("const int %s = %s;%s\n\n", name, g.renderInt(d.Expr, d.Int), g.foldComment(d.Expr))
}

// foldComment returns a trailing comment carrying the schema expression when
// the rendered Dart had to fold it (an E.Max reference has no Dart twin).
func (g *gen) foldComment(e ast.Expr) string {
	if e != nil && ir.ExprHasEnumMax(e) {
		return fmt.Sprintf(" // = %s", ir.RenderExpr(e))
	}
	return ""
}

// emitTagEnum emits an integer-constant namespace — the Dart translation of
// the family's integer-backed enums: an abstract final class of static
// consts, because storage must hold every wire-legal value and | max = ...
// headroom values have no Dart enum member to be.
func (g *gen) emitTagEnum(name string, members []string, max int64, comment string) {
	g.bpf("// %s: %s\n", name, comment)
	g.bpf("abstract final class %s {\n", name)
	g.bpf("  static const int none = 0;\n")
	for i, m := range members {
		g.bpf("  static const int %s = %d;\n", dartName(m), i+1)
	}
	g.bpf("  static const int count = %d; // the declared variant count (SPEC §4.2)\n", len(members))
	g.bpf("  static const int max = %d; // the exported extent (SPEC §4.2)\n", max)
	g.bpf("}\n\n")

	// the tag enum's name surface is a declared enum's, member for member: a
	// reader logging which message arrived writes enumName<Tag>(value)
	// whichever enum it is. Nothing on the read or write path calls it.
	g.bpf("// enumName%s: debug/log/tooling name for any %s wire value —\n", name, name)
	g.bpf("// out-of-set values (wire-legal up to the declared max) name as '???'\n")
	g.bpf("String enumName%s(int value) {\n", name)
	g.bpf("  switch (value) {\n")
	g.bpf("    case %s.none:\n      return 'None';\n", name)
	for _, m := range members {
		g.bpf("    case %s.%s:\n      return '%s';\n", name, dartName(m), m)
	}
	g.bpf("    default:\n      return '???';\n  }\n}\n\n")
}

func (g *gen) emitEnum(d *ir.Enum) {
	g.bpf("// %s — None = 0 implicit, variants dense from 1, wire range [0, %d] (SPEC §4.2);\n", d.Name, d.Max)
	g.bpf("// an int-constant namespace — the Dart translation of the family's integer-\n")
	g.bpf("// backed enums: storage must hold every wire-legal value, and | max = ...\n")
	g.bpf("// headroom values have no Dart enum member to be\n")
	g.bpf("abstract final class %s {\n", d.Name)
	g.bpf("  static const int none = 0;\n")
	for i, v := range d.Variants {
		g.bpf("  static const int %s = %d;\n", dartName(v), i+1)
	}
	g.bpf("  static const int count = %d; // the declared variant count (SPEC §4.2)\n", len(d.Variants))
	g.bpf("  static const int max = %d; // the exported extent (SPEC §4.2)\n", d.Max)
	g.bpf("}\n\n")

	g.bpf("// enumName%s: debug/log/tooling name for any %s wire value —\n", d.Name, d.Name)
	g.bpf("// out-of-set values (wire-legal up to the declared max) name as '???'\n")
	g.bpf("String enumName%s(int value) {\n", d.Name)
	g.bpf("  switch (value) {\n")
	g.bpf("    case %s.none:\n      return 'None';\n", d.Name)
	for _, v := range d.Variants {
		g.bpf("    case %s.%s:\n      return '%s';\n", d.Name, dartName(v), v)
	}
	g.bpf("    default:\n      return '???';\n  }\n}\n\n")
}

func (g *gen) emitFlags(d *ir.Flags) {
	g.bpf("// %s — one bit per variant, consumed as masks; flags-typed fields store\n", d.Name)
	g.bpf("// uint64 in every target — a bit-transparent int here — wire %d bits\n", d.WireBits)
	g.bpf("// (SPEC §4.2). Mask names are the family's flat spelling, lowerCamel.\n")
	for i, v := range d.Variants {
		g.bpf("const int %s = 1 << %d;\n", dartName(d.Name+v), i)
	}
	g.bpf("// the declared variant count (SPEC §4.2)\n")
	g.bpf("const int %sCount = %d;\n", lowerFirst(d.Name), len(d.Variants))
	g.bpf("\n")

	g.bpf("// flagName%s: debug/log/tooling name for bit i of %s —\n", d.Name, d.Name)
	g.bpf("// out-of-range bits name as '???'\n")
	g.bpf("String flagName%s(int bit) {\n", d.Name)
	g.bpf("  switch (bit) {\n")
	for i, v := range d.Variants {
		g.bpf("    case %d:\n      return '%s';\n", i, v)
	}
	g.bpf("    default:\n      return '???';\n  }\n}\n\n")

	g.bpf("// flagNames%s renders the set bits of value as 'A|B' — '0' for the\n", d.Name)
	g.bpf("// empty set, bits past the declared variants as hex\n")
	g.bpf("String flagNames%s(int value) {\n", d.Name)
	g.bpf("  final names = <String>[];\n")
	for i, v := range d.Variants {
		g.bpf("  if (value & (1 << %d) != 0) {\n    names.add('%s');\n  }\n", i, v)
	}
	if len(d.Variants) < 64 { // a 64-variant set has no room for unknown bits
		g.needHex = true
		g.bpf("  if (value >>> %d != 0) {\n", len(d.Variants))
		g.bpf("    names.add('0x${_hex64((value >>> %d) << %d)}');\n", len(d.Variants), len(d.Variants))
		g.bpf("  }\n")
	}
	g.bpf("  return names.isEmpty ? '0' : names.join('|');\n}\n\n")
}

func (g *gen) emitClass(d *ir.Struct) {
	if len(d.Tags) > 0 {
		g.bpf("// type %s [%s] — tags are user-chosen and inert in v1 (SPEC §4.2, Type tags)\n", d.Name, strings.Join(d.Tags, ", "))
	} else {
		g.bpf("// type %s\n", d.Name)
	}
	g.bpf("final class %s {\n", d.Name)
	if len(d.Fields) == 0 {
		g.bpf("  // empty body — presence is the payload (SPEC §4.6)\n")
	}
	prevGuard := ""
	for _, f := range d.Fields {
		if f.Guard != prevGuard {
			if f.Guard != "" {
				g.bpf("\n  // %s — wire branch; storage holds both sides, a read zeroes the\n", f.Guard)
				g.bpf("  // untaken side (SPEC §5)\n")
			} else {
				g.bpf("\n") // leaving a branch group — separate so membership stays visible
			}
			prevGuard = f.Guard
		}
		g.emitStorageField(f)
	}
	g.bpf("}\n\n")
}

func (g *gen) emitStorageField(f *ir.Field) {
	name := dartName(f.Name)
	switch {
	case f.Type.Kind == ir.TString:
		g.usesTypeData = true
		g.bpf("  // string(%s): max length, used length beside it (SPEC §4.7)\n", ir.RenderExpr(f.Type.SizeExpr))
		g.bpf("  final Uint8List %s = Uint8List(%s);\n", name, g.renderInt(f.Type.SizeExpr, big.NewInt(f.Type.Size)))
		g.bpf("  int %sLength = 0;\n", name)
	case f.Type.Kind == ir.TBytes:
		g.usesTypeData = true
		g.bpf("  // bytes(%s): fixed buffer, used length beside it (SPEC §4.7)\n", ir.RenderExpr(f.Type.SizeExpr))
		g.bpf("  final Uint8List %s = Uint8List(%s);\n", name, g.renderInt(f.Type.SizeExpr, big.NewInt(f.Type.Size)))
		g.bpf("  int %sLength = 0;\n", name)
	case f.Array != ir.ArrayNone:
		g.emitFieldComment(f)
		bound := g.renderInt(f.ArrayExpr, big.NewInt(f.ArrayBound))
		switch {
		case f.Type.Kind == ir.TNamed && isClassRef(f.Type.Ref):
			// pre-allocated element instances — the storage principle
			// (SPEC §6.1): every buffer exists at construction; unions are
			// classes here exactly like structs
			g.addRef(f.Type.Name, f.Type.Name)
			one := fmt.Sprintf("  final List<%s> %s = List.generate(%s, (_) => %s());",
				f.Type.Name, name, bound, f.Type.Name)
			if len(one) <= 80 {
				g.bpf("%s\n", one)
			} else {
				g.bpf("  final List<%s> %s = List.generate(\n", f.Type.Name, name)
				g.bpf("    %s,\n", bound)
				g.bpf("    (_) => %s(),\n", f.Type.Name)
				g.bpf("  );\n")
			}
		case is128(f.Type):
			zero := "UInt128.zero"
			typ := "UInt128"
			if f.Type.Signed {
				zero, typ = "Int128.zero", "Int128"
			}
			g.addRef128(typ)
			g.bpf("  final List<%s> %s = List.filled(%s, %s);\n", typ, name, bound, zero)
		default:
			g.usesTypeData = true
			g.bpf("  final %s %s = %s(%s);\n", typedListFor(f.Type), name, typedListFor(f.Type), bound)
		}
		if f.Array == ir.ArrayCounted {
			// a [A..B] count is born at A, the one wire-legal count a fresh
			// value can carry (SPEC §4.6); a [..N] count is born empty
			g.bpf("  int %sCount = %d;\n", name, f.BornCount())
		}
	default:
		g.emitFieldComment(f)
		typ, init := g.scalarStorage(f)
		g.bpf("  %s %s = %s;\n", typ, name, init)
	}
}

// emitFieldComment writes a field's annotation comment on its own line —
// trailing comments would push lines past the formatter's 80 columns.
func (g *gen) emitFieldComment(f *ir.Field) {
	if c := g.fieldComment(f); c != "" {
		g.bpf("  // %s\n", c)
	}
}

// scalarStorage is a scalar field's Dart type and initializer (the specified
// default, else the §5 zero form).
func (g *gen) scalarStorage(f *ir.Field) (typ, init string) {
	t := f.Type
	switch {
	case t.Kind == ir.TBool:
		if f.HasDefault {
			return "bool", fmt.Sprintf("%v", f.DefBool)
		}
		return "bool", "false"
	case t.Kind == ir.TFloat32 || t.Kind == ir.TFloat64:
		if f.HasDefault {
			if t.Kind == ir.TFloat32 {
				return "double", formatFloat32(f.DefFloat)
			}
			return "double", formatFloat(f.DefFloat)
		}
		return "double", "0.0"
	case is128(t):
		if t.Kind == ir.TFixed {
			// ir.Field.DefInt is ALREADY the raw scaled integer for fixed
			// fields (the C++ golden pins it) — emit it verbatim
			if f.HasDefault {
				return g.pair128Type(t), g.render128(t.Signed, f.DefInt)
			}
			return g.pair128Type(t), g.pair128Type(t) + ".zero"
		}
		if f.HasDefault {
			return g.pair128Type(t), g.render128(t.Signed, f.DefInt)
		}
		return g.pair128Type(t), g.pair128Type(t) + ".zero"
	case t.Kind == ir.TFixed:
		if f.HasDefault {
			return "int", dartIntLit(f.DefInt)
		}
		return "int", "0"
	case t.Kind == ir.TNamed:
		switch t.Ref.(type) {
		case *ir.Enum:
			g.addRef(t.Name, t.Name)
			if f.DefVariant != "" {
				return "int", t.Name + "." + dartName(f.DefVariant)
			}
			return "int", t.Name + ".none"
		case *ir.Flags:
			return "int", "0"
		case *ir.Struct, *ir.Union:
			g.addRef(t.Name, t.Name)
			return "final " + t.Name, t.Name + "()"
		}
	}
	// integer / bits storage in a bit-transparent int
	if f.HasDefault {
		return "int", g.renderInt(f.DefExpr, f.DefInt)
	}
	return "int", "0"
}

func (g *gen) pair128Type(t ir.FieldType) string {
	if t.Signed {
		g.addRef128("Int128")
		return "Int128"
	}
	g.addRef128("UInt128")
	return "UInt128"
}

// shiftedRaw is a fixed-point whole-unit value scaled to raw storage:
// v << F, with negative values negated around the shift so the arithmetic is
// exact (the same derivation the wire bounds use). For BOUNDS only:
// ir.Field.IntMin/IntMax are whole units, but ir.Field.DefInt is ALREADY the
// raw scaled integer — a default through this helper double-scales (#168).
func shiftedRaw(v *big.Int, fracBits uint) *big.Int {
	if v.Sign() < 0 {
		return new(big.Int).Neg(new(big.Int).Lsh(new(big.Int).Neg(v), fracBits))
	}
	return new(big.Int).Lsh(v, fracBits)
}

// typedListFor is the typed-data list backing a scalar array — dense storage
// with the width's own store semantics, the Dart twin of C#'s T[N].
func typedListFor(t ir.FieldType) string {
	switch t.Kind {
	case ir.TInt, ir.TFixed:
		if t.Signed {
			switch {
			case t.Width <= 8:
				return "Int8List"
			case t.Width <= 16:
				return "Int16List"
			case t.Width <= 32:
				return "Int32List"
			}
			return "Int64List"
		}
		switch {
		case t.Width <= 8:
			return "Uint8List"
		case t.Width <= 16:
			return "Uint16List"
		case t.Width <= 32:
			return "Uint32List"
		}
		return "Uint64List"
	case ir.TBits:
		if t.Width <= 32 {
			return "Uint32List"
		}
		return "Uint64List"
	case ir.TFloat32:
		return "Float32List"
	case ir.TFloat64:
		return "Float64List"
	case ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Enum:
			switch {
			case ref.StorageBits <= 8:
				return "Uint8List"
			case ref.StorageBits <= 16:
				return "Uint16List"
			case ref.StorageBits <= 32:
				return "Uint32List"
			}
			return "Uint64List"
		case *ir.Flags:
			return "Uint64List" // §6.1: flags-typed fields store uint64
		}
	}
	return "List<bool>" // unreachable: bool arrays have no schema spelling
}

// isClassRef reports a named reference whose Dart storage is a pre-allocated
// class instance: a generated struct class or a union (SPEC §4.8).
func isClassRef(ref ir.Decl) bool {
	switch ref.(type) {
	case *ir.Struct, *ir.Union:
		return true
	}
	return false
}

// emitUnion emits a first-class one-of (SPEC §4.8): the <Name>Type tag
// namespace, then the class — the tag beside one pre-allocated arm per
// variant; nothing allocates per value after construction.
func (g *gen) emitUnion(d *ir.Union) {
	g.emitTagEnum(d.Name+"Type", variantNames(d), d.Max,
		fmt.Sprintf("union %s's tag — None = 0, then each variant in declared order (SPEC §4.8)", d.Name))

	g.bpf("// %s — at most one of the arms; type says which. Construction is the empty\n", d.Name)
	g.bpf("// union (None). A read zero-establishes exactly the selected arm before\n")
	g.bpf("// decoding it (SPEC §5); unselected arms keep what they last held — the\n")
	g.bpf("// reused-storage discipline. Consumers read the selected arm only.\n")
	g.bpf("final class %s {\n", d.Name)
	g.bpf("  int type = %sType.none;\n", d.Name)
	for _, v := range d.Variants {
		if v.Void() {
			continue // a PAYLOAD-FREE arm has no storage (SPEC §4.8)
		}
		g.addRef(v.Type, v.Type)
		g.bpf("  final %s %s = %s();\n", v.Type, dartName(v.Name), v.Type)
	}
	g.bpf("}\n\n")
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

// ---- integer literal / expression rendering ----

var maxInt64 = big.NewInt(0).SetInt64(1<<63 - 1)
var minInt64 = big.NewInt(0).Lsh(big.NewInt(-1), 63)
var maxUint64Big = new(big.Int).SetUint64(1<<64 - 1)

// dartIntLit renders an integer as a Dart int literal: decimal inside int64;
// past it (through uint64) the bit-transparent hex spelling — Dart's only
// unsigned-64 literal form. int64 min itself has no decimal spelling (the
// positive half of `-9223372036854775808` overflows before the minus
// applies — the issue #95 class), so it renders as its hex bit pattern.
func dartIntLit(v *big.Int) string {
	if v.Cmp(minInt64) == 0 {
		return "0x8000000000000000"
	}
	if v.Cmp(minInt64) > 0 && v.Cmp(maxInt64) <= 0 {
		return v.String()
	}
	u := new(big.Int).Set(v)
	if u.Sign() < 0 {
		u.Add(u, new(big.Int).Lsh(big.NewInt(1), 64)) // the 64-bit pattern
	}
	return fmt.Sprintf("0x%x", u)
}

// renderInt renders an integer expression for an int-typed Dart context:
// symbolically where every referenced constant is a bare (untyped) schema
// const and every subtree folds inside int64 (Dart const arithmetic is
// 64-bit); the computed literal otherwise. Folding is always correct.
func (g *gen) renderInt(e ast.Expr, folded *big.Int) string {
	if e == nil || ir.ExprHasEnumMax(e) || !g.renderable(e) || !g.exprExact(e) {
		return dartIntLit(folded)
	}
	return g.renderExpr(e)
}

// exprExact reports whether every subtree of e evaluates exactly in Dart
// int arithmetic: values inside int64, and % only where Dart's euclidean
// remainder agrees with schema's truncated one (both operands non-negative).
func (g *gen) exprExact(e ast.Expr) bool {
	_, ok := g.intEval(e)
	return ok
}

func (g *gen) intEval(e ast.Expr) (*big.Int, bool) {
	fits := func(v *big.Int) bool {
		return v.Cmp(minInt64) >= 0 && v.Cmp(maxInt64) <= 0
	}
	switch e := e.(type) {
	case *ast.IntLit:
		return e.Value, fits(e.Value)
	case *ast.IdentExpr:
		c, ok := g.unit.Consts[e.Name]
		if !ok || c.IsFloat || c.Int == nil {
			return nil, false
		}
		return c.Int, fits(c.Int)
	case *ast.ParenExpr:
		return g.intEval(e.X)
	case *ast.UnaryExpr:
		v, ok := g.intEval(e.X)
		if !ok {
			return nil, false
		}
		nv := new(big.Int).Neg(v)
		return nv, fits(nv)
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
			v.Quo(x, y) // Dart ~/ truncates toward zero, as schema folds
		case "%":
			if y.Sign() == 0 || x.Sign() < 0 || y.Sign() < 0 {
				// Dart % is euclidean; it agrees with schema's truncated
				// remainder only in the non-negative domain
				return nil, false
			}
			v.Rem(x, y)
		default:
			return nil, false
		}
		return v, fits(v)
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

// renderExpr renders an expression in Dart form: constants keep the schema
// spelling in the lowerCamel mapping, imported where they live in another
// file, and schema's truncating / becomes Dart's ~/.
func (g *gen) renderExpr(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.BinaryExpr:
		op := e.Op
		if op == "/" {
			op = "~/"
		}
		return g.renderExpr(e.X) + " " + op + " " + g.renderExpr(e.Y)
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
		g.addRef(e.Name, dartName(e.Name))
		return dartName(e.Name)
	}
	return ir.RenderExpr(e) // unreachable for renderable expressions
}

// pairCtor renders a 128-bit value as a pair-constructor expression, each
// lane a bit-transparent int literal (for const contexts — a local const
// declaration or behind render128's explicit const).
func (g *gen) pairCtor(signed bool, v *big.Int) string {
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
	g.addRef128(typ)
	return fmt.Sprintf("%s(%s, %s)", typ, dartIntLit(hi), dartIntLit(lo))
}

// render128 is pairCtor as a standalone const expression.
func (g *gen) render128(signed bool, v *big.Int) string {
	return "const " + g.pairCtor(signed, v)
}

func formatFloat(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// formatFloat32 renders a float32-precision literal: the shortest form that
// parses to exactly float32(v) — as a Dart double it holds that float32
// value exactly (f32 ⊂ f64), so the wire widths agree with
// ir.CompressedFloatBits in every target.
func formatFloat32(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 32)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}
