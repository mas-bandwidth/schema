// Write/Read function emission for types and messages (SPEC §6.1 items 2-4,
// §6.2): straight-line split functions against the serialize.rs &mut API —
// every serialize_* call returns Result and the generated code propagates
// with `?` (this runtime has no sticky errors, §6.3 Rust row), so counts and
// lengths are already validated before the loop or slice they guard. Write
// functions take &T, so every write goes through a mutable temp — the runtime
// takes &mut for every value, reads and writes alike. The wire is
// byte-identical to the C++ target's, construct by construct.
package rust

import (
	"fmt"
	"math"
	"math/big"

	"github.com/mas-bandwidth/schema/internal/ir"
)

// emitStructFunctions emits MAX_BITS/MAX_BYTES and the split write/read pair
// for a type or message.
func (g *gen) emitStructFunctions(st *ir.Struct) {
	g.needsStreams = true
	snake := ir.RustSnake(st.Name)
	maxBits := ir.MaxBitsStruct(st)
	g.pf("// %s is the longest wire path; align pads at worst case (SPEC §6.1).\n", ir.RustConstName(st.Name+"MaxBits"))
	g.pf("// %s is rounded up to the 8-byte write-buffer granularity.\n", ir.RustConstName(st.Name+"MaxBytes"))
	g.pf("pub const %s: u64 = %d;\n", ir.RustConstName(st.Name+"MaxBits"), maxBits)
	g.pf("pub const %s: usize = %d;\n\n", ir.RustConstName(st.Name+"MaxBytes"), ir.MaxBytes(maxBits))

	// #[inline] on every wire function: the generated crate is a separate
	// compilation unit from the caller, and without the hint a 6-10 byte
	// message pays a full call (and loses constant folding of its bit widths)
	// per serialize — measured 2-6x on tiny messages (bench 2026-08-06)
	g.pf("#[inline]\n")
	g.pf("pub fn write_%s(stream: &mut WriteStream<'_>, value: &%s) -> Result {\n", snake, st.Name)
	if len(st.Items) == 0 {
		g.pf("    let _ = (stream, value); // empty body — presence is the payload (SPEC §4.6)\n")
	} else {
		g.emitWriteItems(st.Items, "    ")
	}
	g.pf("    Ok(())\n}\n\n")

	g.pf("#[inline]\n")
	g.pf("pub fn read_%s(stream: &mut ReadStream<'_>, value: &mut %s) -> Result {\n", snake, st.Name)
	if len(st.Items) == 0 {
		g.pf("    let _ = (stream, value);\n")
	} else {
		g.emitReadItems(st.Items, "    ")
	}
	g.pf("    Ok(())\n}\n\n")
}

func (g *gen) emitWriteItems(items []ir.Item, ind string) {
	for _, item := range items {
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
}

func (g *gen) emitReadItems(items []ir.Item, ind string) {
	for _, item := range items {
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
		g.pf("%sif %s < %s || %s > %s { // out-of-contract writes are refused, not wrapped\n", ind, name, f.IntMin.String(), name, f.IntMax.String())
		g.pf("%s    return Err(Error::Stream(serialize::Error::ValueOutOfRange));\n%s}\n", ind, ind)
	case !loVacuous:
		g.pf("%sif %s < %s { // out-of-contract writes are refused, not wrapped\n", ind, name, f.IntMin.String())
		g.pf("%s    return Err(Error::Stream(serialize::Error::ValueOutOfRange));\n%s}\n", ind, ind)
	case !hiVacuous:
		g.pf("%sif %s > %s { // out-of-contract writes are refused, not wrapped\n", ind, name, f.IntMax.String())
		g.pf("%s    return Err(Error::Stream(serialize::Error::ValueOutOfRange));\n%s}\n", ind, ind)
	}
}

func (g *gen) emitWriteField(f *ir.Field, ind string) {
	g.needsStreamTrait = true
	name := "value." + f.Name
	if f.Array != ir.ArrayNone {
		if f.Array == ir.ArrayCounted {
			bound := g.renderArg(f.ArrayExpr, big.NewInt(f.ArrayBound), "i32")
			g.pf("%sif %s_count < %d || %s_count > %d { // refused, not wrapped: the runtime's write side only debug_asserts\n", ind, name, f.ArrayMin, name, f.ArrayBound)
			g.pf("%s    return Err(Error::Stream(serialize::Error::ValueOutOfRange));\n%s}\n", ind, ind)
			g.pf("%s{\n%s    let mut count_value = %s_count;\n", ind, ind, name)
			g.pf("%s    stream.serialize_int(&mut count_value, %d, %s)?; // the count guards the loop (§6.3)\n", ind, f.ArrayMin, bound)
			g.pf("%s}\n", ind)
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
	case ir.TInt:
		if f.HasIntRange {
			switch intRangePath(f.IntMin, f.IntMax) {
			case "int32":
				lo, hi := g.rangeArgs(f, "i32")
				g.emitWriteRangeGuard(name, f, ind)
				if f.Type.Signed && f.Type.Width == 32 {
					g.pf("%s{\n%s    let mut range_value = %s;\n", ind, ind, name)
				} else {
					g.pf("%s{\n%s    let mut range_value = %s as i32;\n", ind, ind, name)
				}
				g.pf("%s    stream.serialize_int(&mut range_value, %s, %s)?;\n%s}\n", ind, lo, hi, ind)
			case "int64":
				lo, hi := g.rangeArgs(f, "i64")
				g.emitWriteRangeGuard(name, f, ind)
				if f.Type.Signed && f.Type.Width == 64 {
					g.pf("%s{\n%s    let mut range_value = %s;\n", ind, ind, name)
				} else {
					g.pf("%s{\n%s    let mut range_value = %s as i64;\n", ind, ind, name)
				}
				g.pf("%s    stream.serialize_int64(&mut range_value, %s, %s)?;\n%s}\n", ind, lo, hi, ind)
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
			g.pf("%s    stream.serialize_compressed_float(&mut compressed_value, %s, %s, %s)?;\n%s}\n",
				ind, formatFloat32(f.FMin), formatFloat32(f.FMax), formatFloat32(f.Resolution), ind)
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
		g.pf("%sif %s_length < 0 || %s_length > %d { // refused, not wrapped or panicked: guards the slice too\n", ind, name, name, f.Type.Size)
		g.pf("%s    return Err(Error::Stream(serialize::Error::ValueOutOfRange));\n%s}\n", ind, ind)
		g.pf("%s{\n%s    let mut length_value = %s_length;\n", ind, ind, name)
		g.pf("%s    stream.serialize_int(&mut length_value, 0, %s)?; // the length guards the slice (§6.3)\n",
			ind, g.renderArg(f.Type.SizeExpr, big.NewInt(f.Type.Size), "i32"))
		g.pf("%s}\n", ind)
		// the write side borrows the used bytes in place: WriteStream::write_bytes
		// takes &[u8] (same wire as serialize_bytes — align, then the block copy),
		// where the unified &mut signature forced a whole-array copy into a mutable
		// local first — 256 B per chat, 2 KB per block, visible in perf (2026-08-06)
		g.pf("%sstream.write_bytes(&%s[..%s_length as usize])?; // borrowed in place: the write side never mutates\n", ind, name, name)
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			if ref.Max < (int64(1)<<uint(ref.StorageBits))-1 {
				// headroom storage can exceed the wire range: refused, not
				// wrapped (the runtime's write side only debug_asserts)
				g.pf("%sif %s.0 > %d {\n", ind, name, ref.Max)
				g.pf("%s    return Err(Error::Stream(serialize::Error::ValueOutOfRange));\n%s}\n", ind, ind)
			}
			g.pf("%s{\n%s    let mut enum_value = %s.0 as i32;\n", ind, ind, name)
			g.pf("%s    stream.serialize_int(&mut enum_value, 0, %d)?;\n%s}\n", ind, ref.Max, ind)
		case *ir.Flags:
			g.emitWriteFlagsValue(name, ref.WireBits, ind)
		case *ir.Struct:
			g.pf("%swrite_%s(stream, &%s)?;\n", ind, ir.RustSnake(f.Type.Name), name)
		}
	}
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
	case ir.TInt:
		if f.HasIntRange {
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
			g.pf("%sstream.serialize_compressed_float(&mut %s, %s, %s, %s)?;\n",
				ind, name, formatFloat32(f.FMin), formatFloat32(f.FMax), formatFloat32(f.Resolution))
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

// emitMessageTagFunctions emits the tag wire pair, the message-level bound,
// and the Rust dispatch surface: an enum over the message set — a Rust enum
// IS the tagged union (stack storage, size = largest message + tag; SPEC §6.1
// item 6's storage principle, natively). Message::None is the stream
// terminator, and no out-of-set value is representable, so WriteMessage's
// refusal arm does not exist here.
func (g *gen) emitMessageTagFunctions() {
	g.needsStreams = true
	g.needsStreamTrait = true
	count := int64(len(g.unit.Messages))
	g.pf("// The message tag wire: MessageType in [0, %d], minimal bits; None = 0 is a\n", count)
	g.pf("// valid wire value meaning *no message* — the stream terminator (SPEC §4.8).\n")
	g.pf("#[inline]\n")
	g.pf("pub fn write_message_type(stream: &mut WriteStream<'_>, value: MessageType) -> Result {\n")
	g.pf("    let mut tag_value = value.0 as i32;\n")
	g.pf("    stream.serialize_int(&mut tag_value, 0, %d)?;\n", count)
	g.pf("    Ok(())\n}\n\n")
	g.pf("#[inline]\n")
	g.pf("pub fn read_message_type(stream: &mut ReadStream<'_>, value: &mut MessageType) -> Result {\n")
	g.pf("    let mut tag_value: i32 = 0;\n")
	g.pf("    stream.serialize_int(&mut tag_value, 0, %d)?;\n", count)
	g.pf("    *value = MessageType(tag_value as %s);\n", rustUint(ir.StorageBitsFor(count)))
	g.pf("    Ok(())\n}\n\n")

	tagBits := ir.BitsRequired(big.NewInt(0), big.NewInt(count))
	largest := int64(0)
	for _, m := range g.unit.Messages {
		if b := ir.MaxBitsStruct(g.unit.Structs[m]); b > largest {
			largest = b
		}
	}
	g.pf("// The message-level bound: the tag plus the largest message (SPEC §6.1);\n")
	g.pf("// MESSAGE_MAX_BYTES is rounded up to the 8-byte write-buffer granularity.\n")
	g.pf("pub const MESSAGE_MAX_BITS: u64 = %d;\n", tagBits+largest)
	g.pf("pub const MESSAGE_MAX_BYTES: usize = %d;\n\n", ir.MaxBytes(tagBits+largest))

	msgs := g.unit.Messages
	g.pf("// Message is the dispatch surface: the tagged union over the message set —\n")
	g.pf("// a Rust enum IS that union (stack storage, size = the largest message plus\n")
	g.pf("// the tag; nothing heap-allocates per message, SPEC §6.1). Message::None is\n")
	g.pf("// the stream terminator (SPEC §4.8).\n")
	g.pf("#[derive(Clone, Copy, PartialEq, Debug)]\n")
	// size == the largest message is the DESIGN — the enum is the tagged
	// union (SPEC §6.1: fixed pre-allocated storage, no heap per message), so
	// clippy's boxing suggestion is refused where it fires
	g.pf("#[allow(clippy::large_enum_variant)]\n")
	g.pf("pub enum Message {\n")
	g.pf("    None,\n")
	for _, m := range msgs {
		g.pf("    %s(%s),\n", m, m)
	}
	g.pf("}\n\n")

	// the tag rides through the tag pair (one source), inside each arm BEFORE
	// its payload; the out-of-set refusal the other targets carry has no Rust
	// twin — the enum is closed, so no such value exists to refuse
	g.pf("#[inline]\n")
	g.pf("pub fn write_message(stream: &mut WriteStream<'_>, message: &Message) -> Result {\n")
	g.pf("    match message {\n")
	g.pf("        Message::None => write_message_type(stream, MessageType::NONE), // the stream terminator (SPEC §4.8)\n")
	for _, m := range msgs {
		g.pf("        Message::%s(value) => {\n", m)
		g.pf("            write_message_type(stream, MessageType::%s)?;\n", ir.RustConstName(m))
		g.pf("            write_%s(stream, value)\n", ir.RustSnake(m))
		g.pf("        }\n")
	}
	g.pf("    }\n}\n\n")

	// the read-into surface: decodes into caller-owned storage, so steady
	// loops re-read into one Message with no per-message copy-out of the
	// ~largest-message-sized union — the Go/C# MessageStorage discipline in
	// Rust shape. The tagged variant starts from the zero form before its
	// fields decode (§5), exactly as the C# ReadMessage zeroes its storage;
	// on error the message holds that zero form plus whatever fields decoded
	// before the failure — discard it, as with any failed read.
	g.pf("// read_message_into decodes the next message into caller-owned storage: the\n")
	g.pf("// variant is reset to its zero form (SPEC §5), then its fields decode in\n")
	g.pf("// place — no per-message copy of the union out of the call. On error the\n")
	g.pf("// storage holds the partially decoded zero form: discard it.\n")
	g.pf("#[inline]\n")
	g.pf("pub fn read_message_into(stream: &mut ReadStream<'_>, message: &mut Message) -> Result {\n")
	g.pf("    let mut tag_value = MessageType::NONE;\n")
	g.pf("    read_message_type(stream, &mut tag_value)?;\n")
	g.pf("    match tag_value {\n")
	g.pf("        MessageType::NONE => {\n")
	g.pf("            *message = Message::None; // the stream terminator (SPEC §4.8)\n")
	g.pf("            Ok(())\n")
	g.pf("        }\n")
	for _, m := range msgs {
		g.pf("        MessageType::%s => {\n", ir.RustConstName(m))
		g.pf("            *message = Message::%s(%s::default()); // reused storage starts from the zero form, as the union does\n", m, m)
		g.pf("            let Message::%s(value) = message else {\n", m)
		g.pf("                unreachable!()\n")
		g.pf("            };\n")
		g.pf("            read_%s(stream, value)\n", ir.RustSnake(m))
		g.pf("        }\n")
	}
	g.pf("        // read_message_type already rejected tags past the set; the match\n")
	g.pf("        // still needs the arm because tag constants are never exhaustive\n")
	g.pf("        _ => Err(Error::Validation),\n")
	g.pf("    }\n}\n\n")

	// read_message keeps its original by-value body rather than delegating to
	// read_message_into: routing the return through a &mut out-param defeated
	// LLVM's in-place construction of the returned union and cost the batch
	// read 23% (measured M2, 2026-08-06) — each arm constructing directly
	// into the return slot is what keeps the by-value surface cheap
	g.pf("#[inline]\n")
	g.pf("pub fn read_message(stream: &mut ReadStream<'_>) -> Result<Message> {\n")
	g.pf("    let mut tag_value = MessageType::NONE;\n")
	g.pf("    read_message_type(stream, &mut tag_value)?;\n")
	g.pf("    match tag_value {\n")
	g.pf("        MessageType::NONE => Ok(Message::None), // the stream terminator (SPEC §4.8)\n")
	for _, m := range msgs {
		g.pf("        MessageType::%s => {\n", ir.RustConstName(m))
		g.pf("            let mut value = %s::default();\n", m)
		g.pf("            read_%s(stream, &mut value)?;\n", ir.RustSnake(m))
		g.pf("            Ok(Message::%s(value))\n", m)
		g.pf("        }\n")
	}
	g.pf("        // read_message_type already rejected tags past the set; the match\n")
	g.pf("        // still needs the arm because tag constants are never exhaustive\n")
	g.pf("        _ => Err(Error::Validation),\n")
	g.pf("    }\n}\n\n")
}
