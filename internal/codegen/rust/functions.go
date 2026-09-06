// Write/Read function emission for types (SPEC §6.1 items 2-4,
// §6.2): straight-line split functions against the serialize.rs &mut API —
// every serialize_* call returns Result and the generated code propagates
// with `?` (this runtime has no sticky errors, §6.3 Rust row), so counts and
// lengths are already validated before the loop or slice they guard. Write
// functions take &T, so every write goes through a mutable temp — the runtime
// takes &mut for every value, reads and writes alike. The wire is
// byte-identical to the C++ target's, construct by construct.
//
// Two forms share the emission. Maximal runs of statically-sized pieces take
// the flat word codec (flat.go): the emitter folds the bit placement itself
// and the stream sees whole 32-bit chunks. Everything else — align,
// string/bytes, arrays, branches, nested struct and union calls, and the
// fixed-point, compressed-float and 128-bit families, whose value arithmetic
// lives in the runtime — keeps the per-field form below, one item per
// runtime call family. Both spell the same wire.
package rust

import (
	"fmt"
	"math"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/internal/ast"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// emitUnionFunctions emits the union's bounds and wire pair (SPEC §4.8). The
// tag rides in minimal bits for [0, count]; the read rejects a tag above the
// count (serialize_int's own refusal) and reconstructs the selected arm with its
// declared initial values before decoding it, even when the tag is unchanged.
func (g *gen) emitUnionFunctions(d *ir.Union) {
	g.needsStreams = true
	snake := ir.RustSnake(d.Name)
	maxBits := ir.MaxBitsUnion(d)
	g.pf("// %s is the tag plus the largest arm; None costs the tag only (SPEC §4.8).\n", ir.RustConstName(d.Name+"MaxBits"))
	g.pf("// %s is rounded up to the 8-byte write-buffer granularity.\n", ir.RustConstName(d.Name+"MaxBytes"))
	g.pf("pub const %s: u64 = %d;\n", ir.RustConstName(d.Name+"MaxBits"), maxBits)
	g.pf("pub const %s: usize = %d;\n\n", ir.RustConstName(d.Name+"MaxBytes"), ir.MaxBytes(maxBits))

	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(d.Max))
	g.pf("pub fn write_%s(stream: &mut WriteStream<'_>, value: &%s) -> Result {\n", snake, d.Name)
	if d.Max == 0 {
		g.pf("    let _ = (stream, value); // only None exists; the degenerate tag range [0, 0] costs zero bits (SPEC §4.8)\n")
		g.pf("    Ok(())\n}\n\n")
	} else {
		g.pf("    match value {\n")
		g.pf("        %s::None => {\n", d.Name)
		g.pf("            let mut offset_value: u32 = 0;\n")
		g.pf("            stream.serialize_bits(&mut offset_value, %d)?;\n", bits)
		g.pf("            Ok(()) // no payload — the tag is the whole wire (SPEC §4.8)\n        }\n")
		for i, v := range d.Variants {
			if v.Void() {
				g.pf("        %s::%s => {\n", d.Name, ir.GoExportName(v.Name))
				g.pf("            let mut offset_value: u32 = %d;\n", i+1)
				g.pf("            stream.serialize_bits(&mut offset_value, %d)?;\n", bits)
				g.pf("            Ok(()) // a payload-free arm: the tag is the whole wire (SPEC §4.8)\n        }\n")
				continue
			}
			g.pf("        %s::%s(arm) => {\n", d.Name, ir.GoExportName(v.Name))
			g.pf("            let mut offset_value: u32 = %d;\n", i+1)
			g.pf("            stream.serialize_bits(&mut offset_value, %d)?;\n", bits)
			g.pf("            write_%s(stream, arm)\n        }\n", ir.RustSnake(v.Type))
		}
		g.pf("    }\n}\n\n")
	}

	g.pf("pub fn read_%s(stream: &mut ReadStream<'_>, value: &mut %s) -> Result {\n", snake, d.Name)
	if d.Max == 0 {
		g.pf("    let _ = stream; // zero wire bits — only None exists (SPEC §4.8)\n")
		g.pf("    *value = %s::None;\n    Ok(())\n}\n\n", d.Name)
		return
	}
	g.pf("    let mut tag_value: i32 = 0;\n")
	g.pf("    stream.serialize_int(&mut tag_value, 0, %d)?; // rejects a tag above the count (SPEC §4.8)\n", d.Max)
	g.pf("    match tag_value {\n")
	for i, v := range d.Variants {
		if v.Void() {
			g.pf("        %d => *value = %s::%s, // a payload-free arm: the tag is the whole wire (SPEC §4.8)\n", i+1, d.Name, ir.GoExportName(v.Name))
			continue
		}
		g.pf("        %d => {\n", i+1)
		if v.Ref != nil && g.hasDefaults(v.Ref) {
			g.pf("            let mut arm = %s::new();\n", v.Type)
		} else {
			g.pf("            let mut arm = %s::default();\n", v.Type)
		}
		g.pf("            read_%s(stream, &mut arm)?;\n", ir.RustSnake(v.Type))
		g.pf("            *value = %s::%s(arm);\n        }\n", d.Name, ir.GoExportName(v.Name))
	}
	g.pf("        _ => *value = %s::None,\n", d.Name)
	g.pf("    }\n    Ok(())\n}\n\n")
}

// emitStructFunctions emits MAX_BITS/MAX_BYTES and the split write/read pair
// for a type.
func (g *gen) emitStructFunctions(st *ir.Struct) {
	g.needsStreams = true
	// fixed [N]u8 arrays at statically byte-aligned positions take the
	// runtime's bulk-bytes path instead of a per-byte loop — byte-identical
	// wire (the internal align is zero bits when already aligned); the same
	// ir.AlignedFixedByteArrays proof the C++ backend adopted
	g.bulkBytes = ir.AlignedFixedByteArrays(st)
	snake := ir.RustSnake(st.Name)
	maxBits := ir.MaxBitsStruct(st)
	g.pf("// %s is the longest wire path; align pads at worst case (SPEC §6.1).\n", ir.RustConstName(st.Name+"MaxBits"))
	g.pf("// %s is rounded up to the 8-byte write-buffer granularity.\n", ir.RustConstName(st.Name+"MaxBytes"))
	g.pf("pub const %s: u64 = %d;\n", ir.RustConstName(st.Name+"MaxBits"), maxBits)
	g.pf("pub const %s: usize = %d;\n\n", ir.RustConstName(st.Name+"MaxBytes"), ir.MaxBytes(maxBits))

	// Both spines carry an attribute because the generated crate is a separate
	// compilation unit from the caller, and without one a 6-10 byte packet
	// pays a full call (and loses constant folding of its bit widths) per
	// serialize — measured 2-6x on tiny types.
	//
	// The WRITE spine demands (see inline.go): the plain hint only raises
	// LLVM's threshold, and these spines price far over it and were refused
	// anyway. The READ spine keeps the hint: the C++ backend's
	// blanket read demand was ported here, measured, and refused — it
	// collapsed probearray read to 0.53x and shipcreate read to 0.71x.
	g.pf(writeSpineInline)
	g.pf("pub fn write_%s(stream: &mut WriteStream<'_>, value: &%s) -> Result {\n", snake, st.Name)
	if len(st.Items) == 0 {
		g.pf("    let _ = (stream, value); // empty body — presence is the payload (SPEC §4.6)\n")
	} else {
		g.emitWriteItems(st.Items, "    ")
	}
	g.pf("    Ok(())\n}\n\n")

	g.pf(readSpineInline)
	g.pf("pub fn read_%s(stream: &mut ReadStream<'_>, value: &mut %s) -> Result {\n", snake, st.Name)
	if len(st.Items) == 0 {
		g.pf("    let _ = (stream, value);\n")
	} else {
		g.emitReadItems(st.Items, "    ")
	}
	g.pf("    Ok(())\n}\n\n")
}

// emitWriteItems walks the item list, gathering maximal runs of
// statically-sized pieces into the flat word codec (flat.go) and emitting
// everything else — align, arrays, strings, branches, nested calls, the
// runtime-arithmetic families — one item at a time, per-field.
func (g *gen) emitWriteItems(items []ir.Item, ind string) {
	var run flatRun
	flush := func() {
		if len(run.pieces) == 0 {
			return
		}
		if run.worthFlattening() {
			g.emitFlatWriteRun(&run, ind)
		} else {
			for _, p := range run.pieces {
				g.emitWriteItem(p.item, ind)
			}
		}
		run = flatRun{}
	}
	for _, item := range items {
		p, ok := g.flatPieceOf(item)
		if !ok {
			flush()
			g.emitWriteItem(item, ind)
			continue
		}
		if run.bits+p.bits > maxRunBits {
			flush()
		}
		run.pieces = append(run.pieces, p)
		run.bits += p.bits
	}
	flush()
}

// emitWriteItem is the per-field form: one item, one runtime call family.
func (g *gen) emitWriteItem(item ir.Item, ind string) {
	switch item := item.(type) {
	case *ir.FieldItem:
		g.emitWriteField(item.F, ind)
	case *ir.ConstItem:
		g.emitConstItem(item, ind, true)
	case *ir.ReservedItem:
		g.emitReservedItem(item, ind, true)
	case *ir.AlignItem:
		g.needsStreamTrait = true
		g.pf("%sstream.serialize_align()?;\n", ind)
	case *ir.Branch:
		neg := ""
		if item.Neg {
			neg = "!"
		}
		g.pf("%sif %svalue.%s {\n", ind, neg, item.Cond)
		g.emitWriteItems(item.Then, ind+"    ")
		if item.Else != nil {
			g.pf("%s} else {\n", ind)
			g.emitWriteItems(item.Else, ind+"    ")
		}
		g.pf("%s}\n", ind)
	}
}

// emitReadItems is emitWriteItems' twin: the same maximal runs take the flat
// word codec, with one bounds check per 32-bit chunk instead of one per field.
func (g *gen) emitReadItems(items []ir.Item, ind string) {
	var run flatRun
	flush := func() {
		if len(run.pieces) == 0 {
			return
		}
		if run.worthFlattening() {
			g.emitFlatReadRun(&run, ind)
		} else {
			for _, p := range run.pieces {
				g.emitReadItem(p.item, ind)
			}
		}
		run = flatRun{}
	}
	for _, item := range items {
		p, ok := g.flatPieceOf(item)
		if !ok {
			flush()
			g.emitReadItem(item, ind)
			continue
		}
		if run.bits+p.bits > maxRunBits {
			flush()
		}
		run.pieces = append(run.pieces, p)
		run.bits += p.bits
	}
	flush()
}

// emitReadItem is the per-field form: one item, one runtime call family.
func (g *gen) emitReadItem(item ir.Item, ind string) {
	switch item := item.(type) {
	case *ir.FieldItem:
		g.emitReadField(item.F, ind)
	case *ir.ConstItem:
		g.emitConstItem(item, ind, false)
	case *ir.ReservedItem:
		g.emitReservedItem(item, ind, false)
	case *ir.AlignItem:
		g.needsStreamTrait = true
		g.pf("%sstream.serialize_align()?; // rejects nonzero padding (SPEC §4.3)\n", ind)
	case *ir.Branch:
		neg := ""
		if item.Neg {
			neg = "!"
		}
		g.pf("%sif %svalue.%s {\n", ind, neg, item.Cond)
		g.emitReadItems(item.Then, ind+"    ")
		// the untaken side reads as zero values (SPEC §5)
		g.emitZeroItems(item.Else, ind+"    ")
		g.pf("%s} else {\n", ind)
		if item.Else != nil {
			g.emitReadItems(item.Else, ind+"    ")
		}
		g.emitZeroItems(item.Then, ind+"    ")
		g.pf("%s}\n", ind)
	}
}

// emitConstItem writes const(value, bits) on the wire; a read rejects any
// other value (SPEC §4.3).
func (g *gen) emitConstItem(item *ir.ConstItem, ind string, writing bool) {
	g.needsStreamTrait = true
	if writing {
		if item.Bits <= 32 {
			g.pf("%s{\n%s    let mut const_value: u32 = %s;\n", ind, ind, item.Value.String())
			g.pf("%s    stream.serialize_bits(&mut const_value, %d)?; // const(%s, %d) — SPEC §4.3\n", ind, item.Bits, item.Value.String(), item.Bits)
		} else {
			g.pf("%s{\n%s    let mut const_value: u64 = %s;\n", ind, ind, item.Value.String())
			g.pf("%s    stream.serialize_bits64(&mut const_value, %d)?; // const(%s, %d) — SPEC §4.3\n", ind, item.Bits, item.Value.String(), item.Bits)
		}
		g.pf("%s}\n", ind)
		return
	}
	if item.Bits <= 32 {
		g.pf("%s{\n%s    let mut const_value: u32 = 0;\n", ind, ind)
		g.pf("%s    stream.serialize_bits(&mut const_value, %d)?;\n", ind, item.Bits)
	} else {
		g.pf("%s{\n%s    let mut const_value: u64 = 0;\n", ind, ind)
		g.pf("%s    stream.serialize_bits64(&mut const_value, %d)?;\n", ind, item.Bits)
	}
	g.pf("%s    if const_value != %s {\n", ind, item.Value.String())
	g.pf("%s        // const(%s, %d): a read rejects any other value (SPEC §4.3)\n", ind, item.Value.String(), item.Bits)
	g.pf("%s        return Err(Error::Validation);\n%s    }\n%s}\n", ind, ind, ind)
}

// emitReservedItem writes reserved(bits) as zeros; a read rejects nonzero.
func (g *gen) emitReservedItem(item *ir.ReservedItem, ind string, writing bool) {
	g.needsStreamTrait = true
	if writing {
		if item.Bits <= 32 {
			g.pf("%s{\n%s    let mut reserved_value: u32 = 0;\n", ind, ind)
			g.pf("%s    stream.serialize_bits(&mut reserved_value, %d)?; // reserved(%d) — zeros on the wire\n", ind, item.Bits, item.Bits)
		} else {
			g.pf("%s{\n%s    let mut reserved_value: u64 = 0;\n", ind, ind)
			g.pf("%s    stream.serialize_bits64(&mut reserved_value, %d)?; // reserved(%d) — zeros on the wire\n", ind, item.Bits, item.Bits)
		}
		g.pf("%s}\n", ind)
		return
	}
	if item.Bits <= 32 {
		g.pf("%s{\n%s    let mut reserved_value: u32 = 0;\n", ind, ind)
		g.pf("%s    stream.serialize_bits(&mut reserved_value, %d)?;\n", ind, item.Bits)
	} else {
		g.pf("%s{\n%s    let mut reserved_value: u64 = 0;\n", ind, ind)
		g.pf("%s    stream.serialize_bits64(&mut reserved_value, %d)?;\n", ind, item.Bits)
	}
	g.pf("%s    if reserved_value != 0 {\n", ind)
	g.pf("%s        // reserved(%d): a read rejects nonzero (SPEC §4.3)\n", ind, item.Bits)
	g.pf("%s        return Err(Error::Validation);\n%s    }\n%s}\n", ind, ind, ind)
}

// emitZeroItems zero-initializes every field under an untaken branch side
// (SPEC §5: ZERO values, not specified defaults — Default IS that zero form
// here, so plain zero assignments are the memset twin).
func (g *gen) emitZeroItems(items []ir.Item, ind string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			g.emitZeroField(item.F, ind)
		case *ir.Branch:
			g.emitZeroItems(item.Then, ind)
			g.emitZeroItems(item.Else, ind)
		}
	}
}

func (g *gen) emitZeroField(f *ir.Field, ind string) {
	name := "value." + f.Name
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		g.pf("%s%s = [0; %s];\n%s%s_length = 0;\n", ind, name, g.renderArg(f.Type.SizeExpr, big.NewInt(f.Type.Size), "usize"), ind, name)
	case f.Array != ir.ArrayNone:
		g.pf("%s%s = [%s; %s];\n", ind, name, g.zeroScalar(f), g.renderArg(f.ArrayExpr, big.NewInt(f.ArrayBound), "usize"))
		if f.Array == ir.ArrayCounted {
			g.pf("%s%s_count = 0;\n", ind, name)
		}
	default:
		g.pf("%s%s = %s;\n", ind, name, g.zeroScalar(f))
	}
}

// rangeArgs renders the min/max arguments symbolically where possible, cast
// to the runtime call family's argument type.
func (g *gen) rangeArgs(f *ir.Field, typ string) (string, string) {
	return g.renderArg(f.IntMinExpr, f.IntMin, typ), g.renderArg(f.IntMaxExpr, f.IntMax, typ)
}

// maxUint64 is 2^64 - 1, the top of unsigned-64 storage — the bound against
// which a range guard becomes vacuous.
var maxUint64 = new(big.Int).SetUint64(math.MaxUint64)

// intRangePath picks the runtime call family for a ranged integer — the same
// switch as the C++ and Go targets, so all emit identical wire.
func intRangePath(min, max *big.Int) string {
	i32 := big.NewInt(math.MaxInt32)
	i32lo := big.NewInt(math.MinInt32)
	if min.Cmp(i32lo) >= 0 && max.Cmp(i32) <= 0 {
		return "int32"
	}
	i64 := big.NewInt(math.MaxInt64)
	i64lo := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 63))
	if min.Cmp(i64lo) >= 0 && max.Cmp(i64) <= 0 {
		return "int64"
	}
	return "bits64" // full-range unsigned: width-computed raw bits over value - min
}

// ---- generation-time bound folding (the C++ fold pass.s mechanism, Rust shape) ----
//
// A ranged integer's min/max/bit count are schema constants, so the GENERATOR
// folds them: a ranged write emits the offset from min in a bit count computed
// at generation time, straight into serialize_bits/serialize_bits64 with a
// literal width — no runtime bits_required, no min/max parameter traffic, and
// the 32/64 dword split resolves here, not at run time. The wire bytes are
// identical to the runtime serialize_int/serialize_int64 forms, which compute
// exactly this encoding ((value as uN).wrapping_sub(min as uN) in
// bits_required(0, max - min) bits, low dword first past 32) — re-proven by
// the wire golden gates.
//
// Deliberately NOT const-generic call forms (a serialize_int_const::<MIN,
// MAX>): the C++ fold pass built the template twin of that design and measurement
// disqualified it — instantiations shared by repeated bounds get outlined and
// the call boundary cost 10-33% on ranged-int-heavy writes. The Rust hazard
// is the same shape (each generic instantiation is a fresh function the
// inliner may keep out of line), so the fold emits literals, never generics —
// the entire benefit with no new call boundary.
//
// Reads stay on the runtime methods: the serialize const-params work measured the branchless
// reader has nothing to gain from constant bounds, unchallenged since.

// foldOffset32 renders the u32 offset expression for a fold site —
// serialize_int's `(*value as u32).wrapping_sub(min as u32)` with the bounds
// in hand. exprIsU32 skips the cast when the storage is already u32.
func foldOffset32(expr string, exprIsU32 bool, lo string, loZero bool) string {
	cast := expr + " as u32"
	if exprIsU32 {
		cast = expr
	}
	if loZero {
		return cast
	}
	return fmt.Sprintf("(%s).wrapping_sub((%s) as u32)", cast, lo)
}

// emitWriteRangedFold32 emits the folded write for the int32 family: the
// offset from lo in a generation-time bit count, byte-identical to
// stream.serialize_int(&mut value, lo, hi). Release-mode range refusal (or
// debug parity) is the CALLER's job — every fold site either follows a
// generated guard or emits its own debug_assert first.
func (g *gen) emitWriteRangedFold32(expr string, exprIsU32 bool, lo string, loZero bool, bits int64, comment, ind string) {
	// A degenerate range costs ZERO BITS -- the value is known from the range
	// alone, so nothing rides. The bit packer requires at least one bit, so
	// this must not fall through to it.
	if bits == 0 {
		return
	}
	g.pf("%s{\n%s    let mut offset_value = %s;\n", ind, ind, foldOffset32(expr, exprIsU32, lo, loZero))
	g.pf("%s    stream.serialize_bits(&mut offset_value, %d)?;%s\n%s}\n", ind, bits, comment, ind)
}

// emitWriteRangedFold64 is the int64 family twin (serialize_int64's encoding).
// The dword split resolves here: an offset fitting one dword takes
// serialize_bits directly — exactly the low-dword path serialize_int64
// branches to at run time (truncation commutes with the wrapping subtract, so
// computing the offset at u32 width is bit-identical). exprIsU32 feeds that
// delegation: a 64-family RANGE can sit over u32 storage (uint32 full-range).
func (g *gen) emitWriteRangedFold64(expr string, exprIsU64, exprIsU32 bool, lo string, loZero bool, bits int64, comment, ind string) {
	if bits <= 32 {
		g.emitWriteRangedFold32(expr, exprIsU32, lo, loZero, bits, comment, ind)
		return
	}
	cast := expr + " as u64"
	if exprIsU64 {
		cast = expr
	}
	offset := cast
	if !loZero {
		offset = fmt.Sprintf("(%s).wrapping_sub((%s) as u64)", cast, lo)
	}
	g.pf("%s{\n%s    let mut offset_value = %s;\n", ind, ind, offset)
	g.pf("%s    stream.serialize_bits64(&mut offset_value, %d)?;%s\n%s}\n", ind, bits, comment, ind)
}

// foldArg32 and foldArg64 render a bound for a cast context: a bare literal
// gains an explicit type suffix — `expr as uN` propagates the UNSIGNED target
// into an unconstrained literal, so `(-100) as u32` refuses to compile — while
// a symbolic render keeps its own typing through the referenced consts. The
// overflow gate rides here too: a fold FORCED by an i64-escaping
// intermediate must take the suffixed spelling, not renderArg's bare one.
func (g *gen) foldArg32(e ast.Expr, folded *big.Int) string {
	if e == nil || ir.ExprHasEnumMax(e) || !g.renderable(e) || !containsIdent(e) || !g.overflowSafe(e) {
		return folded.String() + "_i32"
	}
	return g.renderArg(e, folded, "i32")
}

func (g *gen) foldArg64(e ast.Expr, folded *big.Int) string {
	if e == nil || ir.ExprHasEnumMax(e) || !g.renderable(e) || !containsIdent(e) || !g.overflowSafe(e) {
		return folded.String() + "_i64"
	}
	return g.renderArg(e, folded, "i64")
}

// storageMin and storageMax bound an integer field's STORAGE domain — the
// range its Rust type can physically hold.
func storageMin(t ir.FieldType) *big.Int {
	if !t.Signed {
		return big.NewInt(0)
	}
	return new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), uint(t.Width-1)))
}

func storageMax(t ir.FieldType) *big.Int {
	if t.Signed {
		return new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(t.Width-1)), big.NewInt(1))
	}
	return new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(t.Width)), big.NewInt(1))
}

// emitWriteRangeGuard refuses an out-of-contract caller value BEFORE it
// reaches the runtime. serialize.rs's write side only debug_asserts — in a
// release build an out-of-range value wraps and ORs stray bits over
// NEIGHBORING fields, and the reader can accept the corrupt packet — so the
// generated code supplies the refusal the Go runtime provides natively.
// Halves vacuous against the storage domain are elided.
func (g *gen) emitWriteRangeGuard(name string, f *ir.Field, ind string) {
	loVacuous := f.IntMin.Cmp(storageMin(f.Type)) <= 0
	hiVacuous := f.IntMax.Cmp(storageMax(f.Type)) >= 0
	switch {
	case !loVacuous && !hiVacuous:
		g.pf("%sif %s < %s || %s > %s {\n", ind, name, f.IntMin.String(), name, f.IntMax.String())
		g.pf("%s    return Err(Error::Stream(serialize::Error::ValueOutOfRange));\n%s}\n", ind, ind)
	case !loVacuous:
		g.pf("%sif %s < %s {\n", ind, name, f.IntMin.String())
		g.pf("%s    return Err(Error::Stream(serialize::Error::ValueOutOfRange));\n%s}\n", ind, ind)
	case !hiVacuous:
		g.pf("%sif %s > %s {\n", ind, name, f.IntMax.String())
		g.pf("%s    return Err(Error::Stream(serialize::Error::ValueOutOfRange));\n%s}\n", ind, ind)
	}
}

// emitWriteFixedRawGuard is emitWriteRangeGuard for a fixed(I, F) field: the
// storage holds the RAW (scaled) value, so the refusal compares against the
// whole-unit bounds shifted into the raw domain — the exact range
// serialize_fixed debug_asserts on write and rejects on read. Halves vacuous
// against the I+F-bit signed storage domain are elided.
func (g *gen) emitWriteFixedRawGuard(name string, f *ir.Field, ind string) {
	rawMin := new(big.Int).Lsh(f.IntMin, uint(f.Type.FracBits))
	rawMax := new(big.Int).Lsh(f.IntMax, uint(f.Type.FracBits))
	var smin, smax *big.Int
	if f.Type.Signed {
		smin = new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width-1)))
		smax = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width-1)), big.NewInt(1))
	} else {
		// unsigned storage: the raw domain is [0, 2^W), so a zero lower
		// bound is vacuous (uN cannot be negative) and the compare literals
		// stay plain decimals the storage type infers
		smin = big.NewInt(0)
		smax = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
	}
	loVacuous := rawMin.Cmp(smin) <= 0
	hiVacuous := rawMax.Cmp(smax) >= 0
	switch {
	case !loVacuous && !hiVacuous:
		g.pf("%sif %s < %s || %s > %s {\n", ind, name, rawMin.String(), name, rawMax.String())
		g.pf("%s    return Err(Error::Stream(serialize::Error::ValueOutOfRange));\n%s}\n", ind, ind)
	case !loVacuous:
		g.pf("%sif %s < %s {\n", ind, name, rawMin.String())
		g.pf("%s    return Err(Error::Stream(serialize::Error::ValueOutOfRange));\n%s}\n", ind, ind)
	case !hiVacuous:
		g.pf("%sif %s > %s {\n", ind, name, rawMax.String())
		g.pf("%s    return Err(Error::Stream(serialize::Error::ValueOutOfRange));\n%s}\n", ind, ind)
	}
}

func (g *gen) emitWriteField(f *ir.Field, ind string) {
	g.needsStreamTrait = true
	name := "value." + f.Name
	if f.Array != ir.ArrayNone {
		if g.bulkBytes[f] {
			// statically byte-aligned [N]u8: the bulk path is byte-identical
			// to the per-byte loop (its internal align is zero bits here) and
			// block-copies instead of 8-bit packing; borrowed in place via
			// WriteStream::write_bytes, as the length-prefixed fields already are
			g.pf("%sstream.write_bytes(&%s); // byte-aligned [N]u8 — bulk copy, wire-identical to the per-byte loop (infallible: returns () in serialize.rs 2.0.0)\n", ind, name)
			return
		}
		if f.Array == ir.ArrayCounted {
			g.pf("%sif %s_count < %d || %s_count > %d {\n", ind, name, f.ArrayMin, name, f.ArrayBound)
			g.pf("%s    return Err(Error::Stream(serialize::Error::ValueOutOfRange));\n%s}\n", ind, ind)
			g.emitWriteRangedFold32(name+"_count", false, fmt.Sprintf("%d_i32", f.ArrayMin), f.ArrayMin == 0,
				ir.BitsRequired(big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound)), " // the count guards the loop (§6.3)", ind)
			g.pf("%sfor i in 0..%s_count as usize {\n", ind, name)
		} else {
			g.pf("%sfor i in 0..%s {\n", ind, g.renderArg(f.ArrayExpr, big.NewInt(f.ArrayBound), "usize"))
		}
		g.emitWriteScalar(f, name+"[i]", ind+"    ")
		g.pf("%s}\n", ind)
		return
	}
	g.emitWriteScalar(f, name, ind)
}

func (g *gen) emitWriteScalar(f *ir.Field, name, ind string) {
	switch f.Type.Kind {
	case ir.TFixed:
		if f.IntMin.Cmp(f.IntMax) == 0 {
			// degenerate range: ZERO bits — the generated raw-domain refusal
			// and no wire call at all, so no runtime degenerate support is
			// needed (SPEC §4.6)
			g.emitWriteFixedRawGuard(name, f, ind)
			return
		}
		// the Q format and the whole-unit bounds are compile-time constants
		// of the call site — part of the wire format, exactly like a ranged
		// integer's bounds (STANDARD.md, fixed). serialize.rs's write side
		// only debug_asserts, so the raw-domain refusal is generated, like
		// every bounded write path in this target; the call goes through a
		// temp — the write functions borrow value immutably
		g.emitWriteFixedRawGuard(name, f, ind)
		lo, hi := g.rangeArgs(f, "i64")
		g.pf("%s{\n%s    let mut fixed_value = %s;\n", ind, ind, name)
		g.pf("%s    stream.serialize_fixed(&mut fixed_value, %d, %d, %s, %s)?;\n%s}\n",
			ind, f.Type.IntBits, f.Type.FracBits, lo, hi, ind)
	case ir.TInt:
		if f.Type.Width == 128 {
			if f.HasIntRange {
				if f.IntMin.Cmp(f.IntMax) == 0 {
					// degenerate range: ZERO bits — refusal only (SPEC §4.6)
					g.emitWriteRangeGuard(name, f, ind)
					return
				}
				// int128 is ALWAYS ranged (SPEC §4.3): offset from min —
				// identical bytes to serialize_int64 wherever the range fits.
				// The runtime's write side only debug_asserts, so the range
				// refusal is generated (the target's family rule)
				g.emitWriteRangeGuard(name, f, ind)
				lo, hi := g.rangeArgs(f, "i128")
				g.pf("%s{\n%s    let mut range_value = %s;\n", ind, ind, name)
				g.pf("%s    stream.serialize_int128(&mut range_value, %s, %s)?;\n%s}\n", ind, lo, hi, ind)
			} else {
				// uint128 is the raw field: 128 bits, low 64-bit half first
				g.pf("%s{\n%s    let mut raw_value = %s;\n", ind, ind, name)
				g.pf("%s    stream.serialize_u128(&mut raw_value)?;\n%s}\n", ind, ind)
			}
			return
		}
		if f.HasIntRange {
			switch intRangePath(f.IntMin, f.IntMax) {
			case "int32":
				lo := g.foldArg32(f.IntMinExpr, f.IntMin)
				g.emitWriteRangeGuard(name, f, ind)
				g.emitWriteRangedFold32(name, !f.Type.Signed && f.Type.Width == 32, lo, f.IntMin.Sign() == 0,
					ir.BitsRequired(f.IntMin, f.IntMax), "", ind)
			case "int64":
				lo := g.foldArg64(f.IntMinExpr, f.IntMin)
				g.emitWriteRangeGuard(name, f, ind)
				g.emitWriteRangedFold64(name, !f.Type.Signed && f.Type.Width == 64, !f.Type.Signed && f.Type.Width == 32,
					lo, f.IntMin.Sign() == 0, ir.BitsRequired(f.IntMin, f.IntMax), "", ind)
			default:
				// full-range unsigned: raw offset bits (u64 storage only — no
				// narrower storage can hold a range past i64). Like every
				// bounded write path in this target, the range refusal is
				// generated: serialize.rs's write side only debug_asserts,
				// so a misuse value must be refused here or it wraps into
				// valid-looking wire; vacuous halves are elided
				lo, _ := g.rangeArgs(f, "u64")
				loVacuous := f.IntMin.Sign() == 0
				hiVacuous := f.IntMax.Cmp(maxUint64) == 0
				switch {
				case !loVacuous && !hiVacuous:
					g.pf("%sif %s < %s || %s > %s {\n", ind, name, lo, name, f.IntMax.String())
					g.pf("%s    return Err(Error::Stream(serialize::Error::ValueOutOfRange));\n%s}\n", ind, ind)
				case !loVacuous:
					g.pf("%sif %s < %s {\n", ind, name, lo)
					g.pf("%s    return Err(Error::Stream(serialize::Error::ValueOutOfRange));\n%s}\n", ind, ind)
				case !hiVacuous:
					g.pf("%sif %s > %s {\n", ind, name, f.IntMax.String())
					g.pf("%s    return Err(Error::Stream(serialize::Error::ValueOutOfRange));\n%s}\n", ind, ind)
				}
				if loVacuous {
					g.pf("%s{\n%s    let mut offset_value = %s;\n", ind, ind, name)
				} else {
					g.pf("%s{\n%s    let mut offset_value = %s - %s;\n", ind, ind, name, lo)
				}
				g.pf("%s    stream.serialize_bits64(&mut offset_value, %d)?;\n%s}\n", ind, ir.BitsRequired(f.IntMin, f.IntMax), ind)
			}
			return
		}
		g.emitWriteBareInt(f, name, ind)
	case ir.TBits:
		if f.Type.Width != 32 && f.Type.Width != 64 {
			// storage is the wider unsigned type: bits above the wire width
			// are refused, not wrapped (the runtime's write side only
			// debug_asserts)
			g.pf("%sif %s >= 1 << %d {\n", ind, name, f.Type.Width)
			g.pf("%s    return Err(Error::Stream(serialize::Error::ValueOutOfRange));\n%s}\n", ind, ind)
		}
		g.pf("%s{\n%s    let mut raw_value = %s;\n", ind, ind, name)
		if f.Type.Width <= 32 {
			g.pf("%s    stream.serialize_bits(&mut raw_value, %d)?;\n%s}\n", ind, f.Type.Width, ind)
		} else {
			g.pf("%s    stream.serialize_bits64(&mut raw_value, %d)?;\n%s}\n", ind, f.Type.Width, ind)
		}
	case ir.TBool:
		g.pf("%s{\n%s    let mut bool_value = %s;\n", ind, ind, name)
		g.pf("%s    stream.serialize_bool(&mut bool_value)?;\n%s}\n", ind, ind)
	case ir.TFloat32:
		if f.HasFloatRange {
			// a temp so the wire quantization cannot write back into the input
			g.pf("%s{\n%s    let mut compressed_value = %s;\n", ind, ind, name)
			g.pf("%s    stream.serialize_compressed_float_precomputed(&mut compressed_value, %s)?;%s\n%s}\n",
				ind, compressedFloatArgs(f), compressedFloatNote(f), ind)
			return
		}
		g.pf("%s{\n%s    let mut float_value = %s;\n", ind, ind, name)
		g.pf("%s    stream.serialize_f32(&mut float_value)?;\n%s}\n", ind, ind)
	case ir.TFloat64:
		g.pf("%s{\n%s    let mut float_value = %s;\n", ind, ind, name)
		g.pf("%s    stream.serialize_f64(&mut float_value)?;\n%s}\n", ind, ind)
	case ir.TString, ir.TBytes:
		// length in [0, N], align, then the used bytes — the classic
		// serialize_string framing over a buffer of N + 1 (SPEC §4.7).
		// Interior nulls are writer misuse; the read side rejects them (§4.7).
		g.pf("%sif %s_length < 0 || %s_length > %d {\n", ind, name, name, f.Type.Size)
		g.pf("%s    return Err(Error::Stream(serialize::Error::ValueOutOfRange));\n%s}\n", ind, ind)
		if f.Type.Kind == ir.TString {
			// well-formed UTF-8 by contract, writer-trusted: debug-only
			// assert, no read-path validation (SPEC §4.7)
			g.pf("%sdebug_assert!(\n%s    std::str::from_utf8(&%s[..%s_length as usize]).is_ok(),\n", ind, ind, name, name)
			g.pf("%s    \"string(N) payloads are well-formed UTF-8 by contract (SPEC 4.7)\"\n%s);\n", ind, ind)
		}
		g.emitWriteRangedFold32(name+"_length", false, "0", true,
			ir.BitsRequired(big.NewInt(0), big.NewInt(f.Type.Size)), " // the length guards the slice (§6.3)", ind)
		// the write side borrows the used bytes in place: WriteStream::write_bytes
		// takes &[u8] (same wire as serialize_bytes — align, then the block copy),
		// where the unified &mut signature forced a whole-array copy into a mutable
		// local first — 256 B per chat, 2 KB per block, visible in perf
		g.pf("%sstream.write_bytes(&%s[..%s_length as usize]); // borrowed in place: the write side never mutates (infallible: returns () in serialize.rs 2.0.0)\n", ind, name, name)
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			if ref.Max < (int64(1)<<uint(ref.StorageBits))-1 {
				// headroom storage can exceed the wire range: refused, not
				// wrapped (the runtime's write side only debug_asserts)
				g.pf("%sif %s.0 > %d {\n", ind, name, ref.Max)
				g.pf("%s    return Err(Error::Stream(serialize::Error::ValueOutOfRange));\n%s}\n", ind, ind)
			}
			g.emitWriteRangedFold32(name+".0", ref.StorageBits == 32, "0", true,
				ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max)), "", ind)
		case *ir.Flags:
			g.emitWriteFlagsValue(name, ref.WireBits, ind)
		case *ir.Struct:
			g.pf("%swrite_%s(stream, &%s)?;\n", ind, ir.RustSnake(f.Type.Name), name)
		case *ir.Union:
			g.pf("%swrite_%s(stream, &%s)?;\n", ind, ir.RustSnake(f.Type.Name), name)
		}
	}
}

// compressedFloatArgs renders the constant arguments of the runtime's
// precomputed compressed-float entry point: the family contract's
// four scalars in the family order — max_integer_value, bits, delta, min, with
// min last. The values are ir.CompressedFloatParams' generation-time fold, the
// same derivation the JS flat backend, the Go fold and
// the cpp backend already consume, and the same arithmetic
// serialize.rs's serialize_compressed_float_params performs per call — so the
// entry points are wire-identical by construction, and the runtime's debug
// misuse checks (bits == bits_required(0, max_integer_value), delta finite
// and positive) hold every generated site to it in the debug test legs. delta
// and min are float32 quantities the wire depends on bit-for-bit;
// formatFloat32 prints literals that convert back to exactly those values.
func compressedFloatArgs(f *ir.Field) string {
	steps, bits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
	min32 := float32(f.FMin)
	delta := float32(f.FMax) - min32
	return fmt.Sprintf("%d, %d, %s, %s", steps, bits,
		formatFloat32(float64(delta)), formatFloat32(float64(min32)))
}

// compressedFloatNote echoes the declaration beside the folded constants:
// four bare scalars do not identify (min, max, resolution) — resolution is
// consumed by the step count — and a reader auditing a call site should not
// have to run the derivation backwards.
func compressedFloatNote(f *ir.Field) string {
	return fmt.Sprintf(" // compressed float [%s, %s] @ %s, constants folded at generation",
		formatFloat(f.FMin), formatFloat(f.FMax), formatFloat(f.Resolution))
}

// emitWriteBareInt writes a bare integer at its storage width. Signed values
// cast through the same-width unsigned first — an `as u32` from a narrower
// signed type sign-extends and would corrupt neighboring wire data, exactly
// as in C++ and Go.
func (g *gen) emitWriteBareInt(f *ir.Field, name, ind string) {
	if f.Type.Width == 64 {
		if f.Type.Signed {
			g.pf("%s{\n%s    let mut raw_value = %s as u64;\n", ind, ind, name)
		} else {
			g.pf("%s{\n%s    let mut raw_value = %s;\n", ind, ind, name)
		}
		g.pf("%s    stream.serialize_bits64(&mut raw_value, 64)?;\n%s}\n", ind, ind)
		return
	}
	cast := name + " as u32"
	if !f.Type.Signed && f.Type.Width == 32 {
		cast = name
	} else if f.Type.Signed && f.Type.Width < 32 {
		cast = fmt32Cast(f.Type.Width, name)
	}
	g.pf("%s{\n%s    let mut raw_value = %s;\n", ind, ind, cast)
	g.pf("%s    stream.serialize_bits(&mut raw_value, %d)?;\n%s}\n", ind, f.Type.Width, ind)
}

// fmt32Cast renders the value-to-u32 conversion for a sub-32 signed bare
// integer: through the same-width unsigned so the sign bit cannot extend.
func fmt32Cast(width int, name string) string {
	return fmt.Sprintf("(%s as u%d) as u32", name, width)
}

func (g *gen) emitReadField(f *ir.Field, ind string) {
	g.needsStreamTrait = true
	name := "value." + f.Name
	if f.Array != ir.ArrayNone {
		if g.bulkBytes[f] {
			g.pf("%sstream.serialize_bytes(&mut %s)?; // byte-aligned [N]u8 — bulk copy, wire-identical to the per-byte loop\n", ind, name)
			return
		}
		if f.Array == ir.ArrayCounted {
			bound := g.renderArg(f.ArrayExpr, big.NewInt(f.ArrayBound), "i32")
			g.pf("%sstream.serialize_int(&mut %s_count, %d, %s)?; // the count guards the loop (§6.3)\n", ind, name, f.ArrayMin, bound)
			g.pf("%sfor i in 0..%s_count as usize {\n", ind, name)
		} else {
			g.pf("%sfor i in 0..%s {\n", ind, g.renderArg(f.ArrayExpr, big.NewInt(f.ArrayBound), "usize"))
		}
		g.emitReadScalar(f, name+"[i]", ind+"    ")
		g.pf("%s}\n", ind)
		return
	}
	g.emitReadScalar(f, name, ind)
}

func (g *gen) emitReadScalar(f *ir.Field, name, ind string) {
	switch f.Type.Kind {
	case ir.TFixed:
		if f.IntMin.Cmp(f.IntMax) == 0 {
			// degenerate range: zero bits — the value is the range, raw
			// min << F, materialized with no wire call (SPEC §4.6), in the
			// storage's own signedness
			rawMin := new(big.Int).Lsh(f.IntMin, uint(f.Type.FracBits))
			g.pf("%s%s = %s;\n", ind, name, rustIntLitStorage(rawMin, f.Type.Signed, f.Type.Width))
			return
		}
		// the runtime validates the raw offset against the raw bounds and
		// rejects — never clamps — Error::ValueOutOfRange on a hostile stream
		lo, hi := g.rangeArgs(f, "i64")
		g.pf("%sstream.serialize_fixed(&mut %s, %d, %d, %s, %s)?;\n",
			ind, name, f.Type.IntBits, f.Type.FracBits, lo, hi)
	case ir.TInt:
		if f.Type.Width == 128 {
			if f.HasIntRange {
				if f.IntMin.Cmp(f.IntMax) == 0 {
					// degenerate range: zero bits — materialize (SPEC §4.6)
					g.pf("%s%s = %s;\n", ind, name, rustIntLit(f.IntMin, 128))
					return
				}
				// rejects a decoded offset beyond max - min (reject, never clamp)
				lo, hi := g.rangeArgs(f, "i128")
				g.pf("%sstream.serialize_int128(&mut %s, %s, %s)?;\n", ind, name, lo, hi)
			} else {
				g.pf("%sstream.serialize_u128(&mut %s)?;\n", ind, name)
			}
			return
		}
		if f.HasIntRange {
			if f.IntMin.Cmp(f.IntMax) == 0 {
				// degenerate range: zero bits — the value is the range,
				// materialized with no wire call (SPEC §4.6)
				g.pf("%s%s = %s;\n", ind, name, rustIntLitStorage(f.IntMin, f.Type.Signed, f.Type.Width))
				return
			}
			switch intRangePath(f.IntMin, f.IntMax) {
			case "int32":
				lo, hi := g.rangeArgs(f, "i32")
				if f.Type.Signed && f.Type.Width == 32 {
					g.pf("%sstream.serialize_int(&mut %s, %s, %s)?;\n", ind, name, lo, hi)
					return
				}
				g.pf("%s{\n%s    let mut range_value: i32 = 0;\n", ind, ind)
				g.pf("%s    stream.serialize_int(&mut range_value, %s, %s)?;\n", ind, lo, hi)
				g.pf("%s    %s = range_value as %s;\n%s}\n", ind, name, g.rustFieldType(f.Type), ind)
			case "int64":
				lo, hi := g.rangeArgs(f, "i64")
				if f.Type.Signed && f.Type.Width == 64 {
					g.pf("%sstream.serialize_int64(&mut %s, %s, %s)?;\n", ind, name, lo, hi)
					return
				}
				g.pf("%s{\n%s    let mut range_value: i64 = 0;\n", ind, ind)
				g.pf("%s    stream.serialize_int64(&mut range_value, %s, %s)?;\n", ind, lo, hi)
				g.pf("%s    %s = range_value as %s;\n%s}\n", ind, name, g.rustFieldType(f.Type), ind)
			default:
				lo, _ := g.rangeArgs(f, "u64")
				diff := new(big.Int).Sub(f.IntMax, f.IntMin)
				g.pf("%s{\n%s    let mut offset_value: u64 = 0;\n", ind, ind)
				g.pf("%s    stream.serialize_bits64(&mut offset_value, %d)?;\n", ind, ir.BitsRequired(f.IntMin, f.IntMax))
				if diff.Cmp(maxUint64) != 0 {
					// a full-width diff cannot overflow its own read — elided
					g.pf("%s    if offset_value > %s {\n", ind, diff.String())
					g.pf("%s        // a read rejects out-of-range (SPEC §5)\n", ind)
					g.pf("%s        return Err(Error::Validation);\n%s    }\n", ind, ind)
				}
				if f.IntMin.Sign() == 0 {
					g.pf("%s    %s = offset_value;\n%s}\n", ind, name, ind)
				} else {
					g.pf("%s    %s = offset_value + %s;\n%s}\n", ind, name, lo, ind)
				}
			}
			return
		}
		g.emitReadBareInt(f, name, ind)
	case ir.TBits:
		if f.Type.Width <= 32 {
			g.pf("%sstream.serialize_bits(&mut %s, %d)?;\n", ind, name, f.Type.Width)
		} else {
			g.pf("%sstream.serialize_bits64(&mut %s, %d)?;\n", ind, name, f.Type.Width)
		}
	case ir.TBool:
		g.pf("%sstream.serialize_bool(&mut %s)?;\n", ind, name)
	case ir.TFloat32:
		if f.HasFloatRange {
			g.pf("%sstream.serialize_compressed_float_precomputed(&mut %s, %s)?;%s\n",
				ind, name, compressedFloatArgs(f), compressedFloatNote(f))
			return
		}
		g.pf("%sstream.serialize_f32(&mut %s)?;\n", ind, name)
	case ir.TFloat64:
		g.pf("%sstream.serialize_f64(&mut %s)?;\n", ind, name)
	case ir.TString, ir.TBytes:
		g.pf("%sstream.serialize_int(&mut %s_length, 0, %s)?; // the length guards the slice (§6.3)\n",
			ind, name, g.renderArg(f.Type.SizeExpr, big.NewInt(f.Type.Size), "i32"))
		g.pf("%sstream.serialize_bytes(&mut %s[..%s_length as usize])?;\n", ind, name, name)
		if f.Type.Kind == ir.TString {
			// the interior-null rule is generated-code validation (SPEC §4.7);
			// `?` above already surfaced a truncated stream as the stream's own
			// error, so this verdict only ever judges bytes that arrived
			g.pf("%sfor i in 0..%s_length as usize {\n", ind, name)
			g.pf("%s    if %s[i] == 0 {\n%s        return Err(Error::Validation);\n%s    }\n%s}\n", ind, name, ind, ind, ind)
		}
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			g.pf("%s{\n%s    let mut enum_value: i32 = 0;\n", ind, ind)
			g.pf("%s    stream.serialize_int(&mut enum_value, 0, %d)?;\n", ind, ref.Max)
			g.pf("%s    %s = %s(enum_value as %s);\n%s}\n", ind, name, f.Type.Name, rustUint(ref.StorageBits), ind)
		case *ir.Flags:
			g.emitReadFlags(name, f.Type.Name, ref.WireBits, ind)
		case *ir.Struct:
			g.pf("%sread_%s(stream, &mut %s)?;\n", ind, ir.RustSnake(f.Type.Name), name)
		case *ir.Union:
			g.pf("%sread_%s(stream, &mut %s)?;\n", ind, ir.RustSnake(f.Type.Name), name)
		}
	}
}

func (g *gen) emitReadBareInt(f *ir.Field, name, ind string) {
	if f.Type.Width == 64 {
		if f.Type.Signed {
			g.pf("%s{\n%s    let mut raw_value: u64 = 0;\n", ind, ind)
			g.pf("%s    stream.serialize_bits64(&mut raw_value, 64)?;\n", ind)
			g.pf("%s    %s = raw_value as i64;\n%s}\n", ind, name, ind)
			return
		}
		g.pf("%sstream.serialize_bits64(&mut %s, 64)?;\n", ind, name)
		return
	}
	if !f.Type.Signed && f.Type.Width == 32 {
		g.pf("%sstream.serialize_bits(&mut %s, 32)?;\n", ind, name)
		return
	}
	g.pf("%s{\n%s    let mut raw_value: u32 = 0;\n", ind, ind)
	g.pf("%s    stream.serialize_bits(&mut raw_value, %d)?;\n", ind, f.Type.Width)
	if f.Type.Signed && f.Type.Width < 32 {
		// back through the same-width unsigned so the sign bit lands right
		g.pf("%s    %s = raw_value as u%d as i%d;\n%s}\n", ind, name, f.Type.Width, f.Type.Width, ind)
		return
	}
	g.pf("%s    %s = raw_value as %s;\n%s}\n", ind, name, g.rustFieldType(f.Type), ind)
}

// emitReadFlags reads a flags value through an unsigned temp; past 32 wire
// bits the u64 storage takes the read directly.
func (g *gen) emitReadFlags(name, typeName string, wireBits int, ind string) {
	if wireBits <= 32 {
		g.pf("%s{\n%s    let mut flags_value: u32 = 0;\n", ind, ind)
		g.pf("%s    stream.serialize_bits(&mut flags_value, %d)?;\n", ind, wireBits)
		g.pf("%s    %s = flags_value as %s;\n%s}\n", ind, name, typeName, ind)
		return
	}
	g.pf("%sstream.serialize_bits64(&mut %s, %d)?;\n", ind, name, wireBits)
}

// emitWriteFlagsValue is the write half used by emitWriteScalar. Storage is
// wider than the wire wherever WireBits < 64, so a mask bit above the wire
// width is refused rather than silently truncated.
func (g *gen) emitWriteFlagsValue(name string, wireBits int, ind string) {
	if wireBits < 64 {
		g.pf("%sif %s >= 1 << %d {\n", ind, name, wireBits)
		g.pf("%s    // a mask bit above the wire width cannot ride\n", ind)
		g.pf("%s    return Err(Error::Stream(serialize::Error::ValueOutOfRange));\n%s}\n", ind, ind)
	}
	if wireBits <= 32 {
		g.pf("%s{\n%s    let mut flags_value = %s as u32;\n", ind, ind, name)
		g.pf("%s    stream.serialize_bits(&mut flags_value, %d)?;\n%s}\n", ind, wireBits, ind)
		return
	}
	g.pf("%s{\n%s    let mut flags_value = %s;\n", ind, ind, name)
	g.pf("%s    stream.serialize_bits64(&mut flags_value, %d)?;\n%s}\n", ind, wireBits, ind)
}

// rustIntLitStorage renders a degenerate-range materialization literal in the
// field's own storage type — signed through rustIntLit (which handles the
// two's-complement minimum), unsigned with the u-width suffix.
func rustIntLitStorage(v *big.Int, signed bool, width int) string {
	if signed {
		return rustIntLit(v, width)
	}
	return fmt.Sprintf("%s_u%d", v, width)
}
