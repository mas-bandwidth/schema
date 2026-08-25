package c

// Field-level wire emission. Every rule here mirrors the C++ backend's folded
// form: the bit count is computed at GENERATION time and the offset written
// with serialize_write_bits, rather than calling a ranged helper with runtime
// bounds. That is what makes the C output bit-identical to the other four.

import (
	"fmt"
	"math/big"

	"github.com/mas-bandwidth/schema/ir"
)

// call wraps an expression that returns 1/0 in the early-out the whole target
// uses: a failed operation latches the stream error and unwinds immediately.
func (g *gen) call(ind, expr string) {
	g.pf("%sif ( !%s )\n%s{\n%s    return 0;\n%s}\n", ind, expr, ind, ind, ind)
}

func (g *gen) emitWriteField(f *ir.Field, ind string) {
	switch {
	case f.Type.Kind == ir.TString:
		// Composed from primitives, NOT serialize_write_string: schema frames
		// the length over [0, N] where the runtime's string call frames it over
		// [0, N-1]. One bit of difference, and every following field shifts.
		// Well-formed UTF-8 by contract, writer-trusted: debug-only assert,
		// no read-path validation (SPEC §4.7).
		g.pf("%sserialize_assert( schema_utf8_valid_( (const serialize_uint8_t *) value->%s, value->%s_length ) );\n", ind, f.Name, f.Name)
		g.call(ind, fmt.Sprintf("serialize_write_int( stream, value->%s_length, 0, %s )", f.Name, g.renderInt(f.Type.SizeExpr, big.NewInt(f.Type.Size))))
		g.call(ind, fmt.Sprintf("serialize_write_bytes( stream, (const serialize_uint8_t *) value->%s, (int) value->%s_length )", f.Name, f.Name))
	case f.Type.Kind == ir.TBytes:
		g.call(ind, fmt.Sprintf("serialize_write_int( stream, value->%s_length, 0, %s )", f.Name, g.renderInt(f.Type.SizeExpr, big.NewInt(f.Type.Size))))
		g.call(ind, fmt.Sprintf("serialize_write_bytes( stream, value->%s, (int) value->%s_length )", f.Name, f.Name))
	case f.Array == ir.ArrayCounted:
		g.call(ind, fmt.Sprintf("serialize_write_int( stream, value->%s_count, %d, %s )", f.Name, f.ArrayMin, g.renderInt(f.ArrayExpr, big.NewInt(f.ArrayBound))))
		g.pf("%s{\n%s    int32_t i;\n%s    for ( i = 0; i < value->%s_count; i++ )\n%s    {\n", ind, ind, ind, f.Name, ind)
		g.emitWriteScalar(f, fmt.Sprintf("value->%s[i]", f.Name), ind+"        ")
		g.pf("%s    }\n%s}\n", ind, ind)
	case f.Array == ir.ArrayFixed && g.bulkBytes[f]:
		// statically byte-aligned [N]uint8: the bulk path is byte-identical
		// to the per-byte loop (its internal align is zero bits here) and
		// memcpys instead of 8-bit packing
		g.call(ind, fmt.Sprintf("serialize_write_bytes( stream, value->%s, %s ) /* byte-aligned [N]uint8 — bulk copy, wire-identical to the per-byte loop */", f.Name, g.renderInt(f.ArrayExpr, big.NewInt(f.ArrayBound))))
	case f.Array == ir.ArrayFixed:
		g.pf("%s{\n%s    int32_t i;\n%s    for ( i = 0; i < %s; i++ )\n%s    {\n", ind, ind, ind, g.renderInt(f.ArrayExpr, big.NewInt(f.ArrayBound)), ind)
		g.emitWriteScalar(f, fmt.Sprintf("value->%s[i]", f.Name), ind+"        ")
		g.pf("%s    }\n%s}\n", ind, ind)
	default:
		g.emitWriteScalar(f, "value->"+f.Name, ind)
	}
}

func (g *gen) emitWriteScalar(f *ir.Field, expr, ind string) {
	switch f.Type.Kind {
	case ir.TBool:
		g.call(ind, fmt.Sprintf("serialize_write_bool( stream, %s )", expr))
	case ir.TFloat32:
		if f.HasFloatRange {
			// The runtime's precomputed entry point: the step
			// count, wire width and float32 range width depend only on the
			// declaration, so ir.CompressedFloatParams derives them ONCE at
			// generation time — the same derivation the Go fold
			// emits from — and the quantization arithmetic
			// keeps its one audited home in serialize.h.
			steps, wireBits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
			min32 := float32(f.FMin)
			delta := float32(f.FMax) - min32
			g.call(ind, fmt.Sprintf("serialize_write_compressed_float_precomputed( stream, %s, %du, %d, %s, %s )",
				expr, steps, wireBits, formatFloat32(delta), formatFloat32(min32)))
			return
		}
		g.call(ind, fmt.Sprintf("serialize_write_float( stream, %s )", expr))
	case ir.TFloat64:
		g.call(ind, fmt.Sprintf("serialize_write_double( stream, %s )", expr))
	case ir.TFixed:
		g.emitWriteFixed(f, expr, ind)
	case ir.TInt:
		if f.Type.Width == 128 {
			g.emitWrite128(f, expr, ind)
			return
		}
		g.emitWriteRangedInt(f, expr, ind)
	case ir.TBits:
		g.emitWriteBits(f, expr, ind)
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			bits := ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max))
			g.pf("%sif ( %s > %d )\n%s{\n%s    return 0; /* headroom above the wire range cannot ride */\n%s}\n", ind, expr, ref.Max, ind, ind, ind)
			if bits > 0 {
				g.call(ind, fmt.Sprintf("serialize_write_bits( stream, (serialize_uint32_t) %s, %d )", expr, bits))
			}
			// bits == 0: a degenerate range costs nothing — the value is
			// recovered from the range alone on read
		case *ir.Flags:
			g.call(ind, fmt.Sprintf("serialize_write_bits( stream, (serialize_uint32_t) %s, %d )", expr, ref.WireBits))
		case *ir.Struct:
			g.call(ind, fmt.Sprintf("write_%s( stream, &%s )", snake(f.Type.Name), expr))
		case *ir.Union:
			g.call(ind, fmt.Sprintf("write_%s( stream, &%s )", snake(f.Type.Name), expr))
		default:
			g.unsupported("field %s references %s, whose kind has no C write emission", f.Name, f.Type.Name)
		}
	default:
		g.unsupported("field %s has type kind %v, which has no C write emission", f.Name, f.Type.Kind)
	}
}

func (g *gen) emitWriteRangedInt(f *ir.Field, expr, ind string) {
	if !f.HasIntRange {
		// bare integer at its storage width
		if f.Type.Width == 64 {
			g.call(ind, fmt.Sprintf("serialize_write_uint64( stream, (serialize_uint64_t) %s )", expr))
			return
		}
		if f.Type.Signed && f.Type.Width < 32 {
			// through the same-width unsigned first: the direct cast
			// sign-extends a negative intN past its declared width, which
			// write_bits asserts against. The C++ backend narrows the same way.
			g.call(ind, fmt.Sprintf("serialize_write_bits( stream, (serialize_uint32_t) (serialize_uint%d_t) %s, %d )", f.Type.Width, expr, f.Type.Width))
			return
		}
		g.call(ind, fmt.Sprintf("serialize_write_bits( stream, (serialize_uint32_t) %s, %d )", expr, f.Type.Width))
		return
	}

	// The FOLDED form, matching the other four backends: the bit count is
	// computed here, at generation time, and the offset written with
	// serialize_write_bits. Calling the ranged helper with runtime bounds
	// would be equivalent on the wire but not expressible in C for an
	// unsigned range wider than int32 -- [0, 4294967295] does not fit the
	// int32 parameter. Folding sidesteps that and is bit-identical by
	// construction.
	bits := ir.BitsRequired(f.IntMin, f.IntMax)
	g.emitRangeAssertWrite(f, expr, ind)
	if bits == 0 {
		return // degenerate range: the value is the range
	}
	offset := expr
	if f.IntMin.Sign() != 0 {
		offset = fmt.Sprintf("(%s) - (%s)", expr, g.renderInt(f.IntMinExpr, f.IntMin))
	}
	if bits <= 32 {
		g.call(ind, fmt.Sprintf("serialize_write_bits( stream, (serialize_uint32_t) ( %s ), %d )", offset, bits))
		return
	}
	// low 32 first, then the remainder -- the same split serialize_bits uses
	g.pf("%s{\n%s    serialize_uint64_t offset_value = (serialize_uint64_t) ( %s );\n", ind, ind, offset)
	g.call(ind+"    ", "serialize_write_bits( stream, (serialize_uint32_t) ( offset_value & 0xFFFFFFFFu ), 32 )")
	g.call(ind+"    ", fmt.Sprintf("serialize_write_bits( stream, (serialize_uint32_t) ( offset_value >> 32 ), %d )", bits-32))
	g.pf("%s}\n", ind)
}

// emitRangeAssertWrite refuses an out-of-contract write rather than wrapping
// it -- the same guard the other backends fold in.
//
// Each half is emitted only when it CAN be false for the storage type. GCC's
// -Wtype-limits rejects a vacuous comparison (an unsigned value < 0, or a
// bound at the type's own maximum) and this family builds with -Werror, so a
// guard that is merely redundant on clang is a build failure on gcc. The C++
// backend elides the same way; it just gets there by casting to int64_t.
func (g *gen) emitRangeAssertWrite(f *ir.Field, expr, ind string) {
	loNeeded, hiNeeded := true, true
	if lo, hi, ok := storageRange(f); ok {
		if f.IntMin.Cmp(lo) <= 0 {
			loNeeded = false
		}
		if f.IntMax.Cmp(hi) >= 0 {
			hiNeeded = false
		}
	}
	if !loNeeded && !hiNeeded {
		return // the declared range is the storage range: nothing to check
	}

	cast := "serialize_int64_t"
	suffix := "LL"
	if !f.Type.Signed && f.Type.Width >= 64 {
		cast = "serialize_uint64_t"
		suffix = "ULL"
	}

	lo := g.renderIntSuffixed(f.IntMinExpr, f.IntMin, suffix)
	hi := g.renderIntSuffixed(f.IntMaxExpr, f.IntMax, suffix)
	var cond string
	switch {
	case loNeeded && hiNeeded:
		cond = fmt.Sprintf("(%s) %s < %s || (%s) %s > %s", cast, expr, lo, cast, expr, hi)
	case loNeeded:
		cond = fmt.Sprintf("(%s) %s < %s", cast, expr, lo)
	default:
		cond = fmt.Sprintf("(%s) %s > %s", cast, expr, hi)
	}
	g.pf("%sif ( %s )\n%s{\n%s    return 0;\n%s}\n",
		ind, cond, ind, ind, ind)
}

// storageRange is the inclusive range the field's STORAGE type can hold. A
// declared bound at or past it makes that half of the guard vacuous.
func storageRange(f *ir.Field) (*big.Int, *big.Int, bool) {
	if f.Type.Kind != ir.TInt {
		return nil, nil, false
	}
	one := big.NewInt(1)
	if f.Type.Signed {
		hi := new(big.Int).Sub(new(big.Int).Lsh(one, uint(f.Type.Width-1)), one)
		lo := new(big.Int).Neg(new(big.Int).Lsh(one, uint(f.Type.Width-1)))
		return lo, hi, true
	}
	hi := new(big.Int).Sub(new(big.Int).Lsh(one, uint(f.Type.Width)), one)
	return big.NewInt(0), hi, true
}

func (g *gen) emitWriteBits(f *ir.Field, expr, ind string) {
	if f.Type.Width > 32 {
		// The >32 split from STANDARD.md: the low 32 bits as one group, then
		// the remainder. NOT serialize_write_uint64 -- that always spends a
		// full 64 bits, which is right only when the width IS 64. A bits(33)
		// field would otherwise cost 31 bits too many and shift every field
		// after it.
		g.pf("%s{\n%s    serialize_uint64_t bits_value = (serialize_uint64_t) %s;\n", ind, ind, expr)
		g.call(ind+"    ", "serialize_write_bits( stream, (serialize_uint32_t) ( bits_value & 0xFFFFFFFFu ), 32 )")
		g.call(ind+"    ", fmt.Sprintf("serialize_write_bits( stream, (serialize_uint32_t) ( bits_value >> 32 ), %d )", f.Type.Width-32))
		g.pf("%s}\n", ind)
		return
	}
	g.call(ind, fmt.Sprintf("serialize_write_bits( stream, (serialize_uint32_t) %s, %d )", expr, f.Type.Width))
}

// ---- read ----

func (g *gen) emitReadField(f *ir.Field, ind string) {
	switch {
	case f.Type.Kind == ir.TString:
		g.call(ind, fmt.Sprintf("serialize_read_int( stream, &value->%s_length, 0, %s )", f.Name, g.renderInt(f.Type.SizeExpr, big.NewInt(f.Type.Size))))
		g.call(ind, fmt.Sprintf("serialize_read_bytes( stream, (serialize_uint8_t *) value->%s, (int) value->%s_length )", f.Name, f.Name))
		// the interior-null rule is generated-code validation (SPEC §4.7);
		// the word-wise scan lives in schema_interior_null_ (nullscan.go)
		g.pf("%sif ( schema_interior_null_( (const serialize_uint8_t *) value->%s, value->%s_length ) )\n%s{\n", ind, f.Name, f.Name, ind)
		g.pf("%s    return 0; /* an interior null is content the read refuses (SPEC §4.7) */\n%s}\n", ind, ind)
		// the terminator is not transmitted; the reader supplies it
		g.pf("%svalue->%s[value->%s_length] = 0;\n", ind, f.Name, f.Name)
	case f.Type.Kind == ir.TBytes:
		g.call(ind, fmt.Sprintf("serialize_read_int( stream, &value->%s_length, 0, %s )", f.Name, g.renderInt(f.Type.SizeExpr, big.NewInt(f.Type.Size))))
		g.call(ind, fmt.Sprintf("serialize_read_bytes( stream, value->%s, (int) value->%s_length )", f.Name, f.Name))
	case f.Array == ir.ArrayCounted:
		g.call(ind, fmt.Sprintf("serialize_read_int( stream, &value->%s_count, %d, %s )", f.Name, f.ArrayMin, g.renderInt(f.ArrayExpr, big.NewInt(f.ArrayBound))))
		g.pf("%s{\n%s    int32_t i;\n%s    for ( i = 0; i < value->%s_count; i++ )\n%s    {\n", ind, ind, ind, f.Name, ind)
		g.emitReadScalar(f, fmt.Sprintf("value->%s[i]", f.Name), ind+"        ")
		g.pf("%s    }\n%s}\n", ind, ind)
	case f.Array == ir.ArrayFixed && g.bulkBytes[f]:
		g.call(ind, fmt.Sprintf("serialize_read_bytes( stream, value->%s, %s ) /* byte-aligned [N]uint8 — bulk copy, wire-identical to the per-byte loop */", f.Name, g.renderInt(f.ArrayExpr, big.NewInt(f.ArrayBound))))
	case f.Array == ir.ArrayFixed:
		g.pf("%s{\n%s    int32_t i;\n%s    for ( i = 0; i < %s; i++ )\n%s    {\n", ind, ind, ind, g.renderInt(f.ArrayExpr, big.NewInt(f.ArrayBound)), ind)
		g.emitReadScalar(f, fmt.Sprintf("value->%s[i]", f.Name), ind+"        ")
		g.pf("%s    }\n%s}\n", ind, ind)
	default:
		g.emitReadScalar(f, "value->"+f.Name, ind)
	}
}

func (g *gen) emitReadScalar(f *ir.Field, expr, ind string) {
	switch f.Type.Kind {
	case ir.TBool:
		g.call(ind, fmt.Sprintf("serialize_read_bool( stream, &%s )", expr))
	case ir.TFloat32:
		if f.HasFloatRange {
			// read twin of the write-side fold: same generation-time
			// constants, and the headroom refusal stays inside the runtime
			// entry point where it always lived
			steps, wireBits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
			min32 := float32(f.FMin)
			delta := float32(f.FMax) - min32
			g.call(ind, fmt.Sprintf("serialize_read_compressed_float_precomputed( stream, &%s, %du, %d, %s, %s )",
				expr, steps, wireBits, formatFloat32(delta), formatFloat32(min32)))
			return
		}
		g.call(ind, fmt.Sprintf("serialize_read_float( stream, &%s )", expr))
	case ir.TFloat64:
		g.call(ind, fmt.Sprintf("serialize_read_double( stream, &%s )", expr))
	case ir.TFixed:
		g.emitReadFixed(f, expr, ind)
	case ir.TInt:
		if f.Type.Width == 128 {
			g.emitRead128(f, expr, ind)
			return
		}
		g.emitReadRangedInt(f, expr, ind)
	case ir.TBits:
		g.emitReadBits(f, expr, ind)
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			bits := ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max))
			if bits == 0 {
				// degenerate range: the value is the range
				g.pf("%s%s = 0;\n", ind, expr)
				return
			}
			g.pf("%s{\n%s    serialize_uint32_t enum_value = 0;\n", ind, ind)
			g.call(ind+"    ", fmt.Sprintf("serialize_read_bits( stream, &enum_value, %d )", bits))
			g.pf("%s    if ( enum_value > %d )\n%s    {\n%s        return 0; /* not a wire-legal value */\n%s    }\n", ind, ref.Max, ind, ind, ind)
			g.pf("%s    %s = (%s) enum_value;\n%s}\n", ind, expr, f.Type.Name, ind)
		case *ir.Flags:
			g.pf("%s{\n%s    serialize_uint32_t flags_value = 0;\n", ind, ind)
			g.call(ind+"    ", fmt.Sprintf("serialize_read_bits( stream, &flags_value, %d )", ref.WireBits))
			g.pf("%s    %s = (%s) flags_value;\n%s}\n", ind, expr, f.Type.Name, ind)
		case *ir.Struct:
			g.call(ind, fmt.Sprintf("read_%s( stream, &%s )", snake(f.Type.Name), expr))
		case *ir.Union:
			g.call(ind, fmt.Sprintf("read_%s( stream, &%s )", snake(f.Type.Name), expr))
		default:
			g.unsupported("field %s references %s, whose kind has no C read emission", f.Name, f.Type.Name)
		}
	default:
		g.unsupported("field %s has type kind %v, which has no C read emission", f.Name, f.Type.Kind)
	}
}

func (g *gen) emitReadRangedInt(f *ir.Field, expr, ind string) {
	if !f.HasIntRange {
		if f.Type.Width == 64 {
			g.pf("%s{\n%s    serialize_uint64_t raw = 0;\n", ind, ind)
			g.call(ind+"    ", "serialize_read_uint64( stream, &raw )")
			g.pf("%s    %s = (%s) raw;\n%s}\n", ind, expr, g.storageType(f), ind)
			return
		}
		g.pf("%s{\n%s    serialize_uint32_t raw = 0;\n", ind, ind)
		g.call(ind+"    ", fmt.Sprintf("serialize_read_bits( stream, &raw, %d )", f.Type.Width))
		g.pf("%s    %s = (%s) raw;\n%s}\n", ind, expr, g.storageType(f), ind)
		return
	}

	bits := ir.BitsRequired(f.IntMin, f.IntMax)
	if bits == 0 {
		g.pf("%s%s = (%s) (%s);\n", ind, expr, g.storageType(f), g.renderInt(f.IntMinExpr, f.IntMin))
		return
	}
	span := new(big.Int).Sub(f.IntMax, f.IntMin)
	g.pf("%s{\n%s    serialize_uint64_t offset_value = 0;\n", ind, ind)
	if bits <= 32 {
		g.pf("%s    serialize_uint32_t raw = 0;\n", ind)
		g.call(ind+"    ", fmt.Sprintf("serialize_read_bits( stream, &raw, %d )", bits))
		g.pf("%s    offset_value = raw;\n", ind)
	} else {
		g.pf("%s    serialize_uint32_t lo = 0;\n%s    serialize_uint32_t hi = 0;\n", ind, ind)
		g.call(ind+"    ", "serialize_read_bits( stream, &lo, 32 )")
		g.call(ind+"    ", fmt.Sprintf("serialize_read_bits( stream, &hi, %d )", bits-32))
		g.pf("%s    offset_value = (serialize_uint64_t) lo | ( ( (serialize_uint64_t) hi ) << 32 );\n", ind)
	}
	// Reject, never clamp -- a value smuggled into the range's bit headroom.
	// Elided when the span fills the full 64-bit domain: there is no headroom
	// to smuggle into, and the comparison would be vacuous (which -Wtype-limits
	// rejects, and this family builds with -Werror).
	maxU64 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(1))
	if span.Cmp(maxU64) < 0 {
		g.pf("%s    if ( offset_value > %sULL )\n%s    {\n%s        return 0;\n%s    }\n", ind, span.String(), ind, ind, ind)
	}
	if f.IntMin.Sign() == 0 {
		g.pf("%s    %s = (%s) offset_value;\n%s}\n", ind, expr, g.storageType(f), ind)
	} else {
		g.pf("%s    %s = (%s) ( offset_value + (%s) );\n%s}\n", ind, expr, g.storageType(f), g.renderInt(f.IntMinExpr, f.IntMin), ind)
	}
}

func (g *gen) emitReadBits(f *ir.Field, expr, ind string) {
	if f.Type.Width > 32 {
		g.pf("%s{\n%s    serialize_uint32_t lo = 0;\n%s    serialize_uint32_t hi = 0;\n", ind, ind, ind)
		g.call(ind+"    ", "serialize_read_bits( stream, &lo, 32 )")
		g.call(ind+"    ", fmt.Sprintf("serialize_read_bits( stream, &hi, %d )", f.Type.Width-32))
		g.pf("%s    %s = (%s) ( (serialize_uint64_t) lo | ( ( (serialize_uint64_t) hi ) << 32 ) );\n%s}\n",
			ind, expr, g.storageType(f), ind)
		return
	}
	g.pf("%s{\n%s    serialize_uint32_t raw = 0;\n", ind, ind)
	g.call(ind+"    ", fmt.Sprintf("serialize_read_bits( stream, &raw, %d )", f.Type.Width))
	g.pf("%s    %s = (%s) raw;\n%s}\n", ind, expr, g.storageType(f), ind)
}

// ---- fixed point ----

// fixedCall picks the runtime entry point for a Q format's storage width.
// serialize.c offers 32/64/128 only, and the language admits I+F in
// {8,16,32,64,128}. Widths 8 and 16 ride the 32-bit call: serialize.c ignores
// integer_bits entirely and derives the span from (min << fraction_bits), so
// the widened call produces IDENTICAL bytes. The narrowing is storage-side.
func fixedCall(width int) (string, string) {
	switch {
	case width <= 32:
		return "32", "serialize_int32_t"
	case width <= 64:
		return "64", "serialize_int64_t"
	default:
		return "128", "serialize_int128_t"
	}
}

func (g *gen) emitWriteFixed(f *ir.Field, expr, ind string) {
	suffix, temp := fixedCall(f.Type.Width)
	lo, hi := f.IntMin, f.IntMax
	if lo == nil || hi == nil {
		g.unsupported("fixed field %s has no resolved whole-unit bounds", f.Name)
		return
	}
	if lo.Cmp(hi) == 0 {
		// degenerate range: ZERO bits — the range refusal and no wire call
		// at all, so no runtime degenerate support is needed (SPEC §4.6,
		// decided deliberately). The one legal raw is min << F, compared in
		// the storage's own signedness (a wide ufixed raw can live above
		// INT64_MAX).
		rawMin := new(big.Int).Lsh(lo, uint(f.Type.FracBits))
		switch {
		case f.Type.Width == 128 && f.Type.Signed:
			g.pf("%sif ( !serialize_int128_equal( %s, %s ) )\n%s{\n%s    return 0;\n%s}\n",
				ind, expr, g.int128Literal(nil, rawMin), ind, ind, ind)
		case f.Type.Width == 128:
			g.pf("%sif ( !serialize_uint128_equal( %s, %s ) )\n%s{\n%s    return 0;\n%s}\n",
				ind, expr, uint128Literal(rawMin), ind, ind, ind)
		case f.Type.Signed:
			g.pf("%sif ( (serialize_int64_t) %s != %s )\n%s{\n%s    return 0;\n%s}\n",
				ind, expr, intLit(rawMin, "LL"), ind, ind, ind)
		default:
			g.pf("%sif ( (serialize_uint64_t) %s != %s )\n%s{\n%s    return 0;\n%s}\n",
				ind, expr, intLit(rawMin, "ULL"), ind, ind, ind)
		}
		return
	}
	if !f.Type.Signed {
		g.emitWriteUfixed(f, expr, ind)
		return
	}
	// through a temp so a narrower storage member widens to the call's type
	g.pf("%s{\n%s    %s fixed_value = %s;\n", ind, ind, temp, expr)
	g.call(ind+"    ", fmt.Sprintf("serialize_write_fixed%s( stream, fixed_value, %d, %d, %s, %s )",
		suffix, f.Type.IntBits, f.Type.FracBits, g.renderIntSuffixed(f.IntMinExpr, lo, "LL"), g.renderIntSuffixed(f.IntMaxExpr, hi, "LL")))
	g.pf("%s}\n", ind)
}

// emitWriteUfixed routes an unsigned fixed write. The C runtime's fixed
// entries take SIGNED values and sign-extend them into the shared unsigned
// 128-bit core, so an unsigned raw must arrive zero-extended: storage of 32
// bits or fewer zero-extends through the fixed64 entry's int64 value, 64-bit
// storage zero-extends into the fixed128 entry's low lane, and 128-bit
// storage bit-casts lane for lane. The wire sees only the raw span — the
// bounds and F — so the entry width never moves a byte (the core derives the
// bit count from the span alone, and the signed narrow path already leans on
// the same property).
func (g *gen) emitWriteUfixed(f *ir.Field, expr, ind string) {
	lo := g.renderIntSuffixed(f.IntMinExpr, f.IntMin, "LL")
	hi := g.renderIntSuffixed(f.IntMaxExpr, f.IntMax, "LL")
	switch {
	case f.Type.Width <= 32:
		g.pf("%s{\n%s    serialize_int64_t fixed_value = (serialize_int64_t) %s;\n", ind, ind, expr)
		g.call(ind+"    ", fmt.Sprintf("serialize_write_fixed64( stream, fixed_value, %d, %d, %s, %s )",
			f.Type.IntBits, f.Type.FracBits, lo, hi))
		g.pf("%s}\n", ind)
	case f.Type.Width == 64:
		g.pf("%s{\n%s    serialize_int128_t fixed_value = serialize_int128_make( 0, %s );\n", ind, ind, expr)
		g.call(ind+"    ", fmt.Sprintf("serialize_write_fixed128( stream, fixed_value, %d, %d, %s, %s )",
			f.Type.IntBits, f.Type.FracBits, lo, hi))
		g.pf("%s}\n", ind)
	default:
		g.pf("%s{\n%s    serialize_int128_t fixed_value = serialize_int128_make( %s.hi, %s.lo );\n", ind, ind, expr, expr)
		g.call(ind+"    ", fmt.Sprintf("serialize_write_fixed128( stream, fixed_value, %d, %d, %s, %s )",
			f.Type.IntBits, f.Type.FracBits, lo, hi))
		g.pf("%s}\n", ind)
	}
}

func (g *gen) emitReadFixed(f *ir.Field, expr, ind string) {
	suffix, temp := fixedCall(f.Type.Width)
	lo, hi := f.IntMin, f.IntMax
	if lo == nil || hi == nil {
		g.unsupported("fixed field %s has no resolved whole-unit bounds", f.Name)
		return
	}
	if lo.Cmp(hi) == 0 {
		// degenerate range: zero bits — the value is the range, raw
		// min << F, materialized with no wire call (SPEC §4.6), in the
		// storage's own signedness
		rawMin := new(big.Int).Lsh(lo, uint(f.Type.FracBits))
		switch {
		case f.Type.Width == 128 && f.Type.Signed:
			g.pf("%s%s = %s;\n", ind, expr, g.int128Literal(nil, rawMin))
		case f.Type.Width == 128:
			g.pf("%s%s = %s;\n", ind, expr, uint128Literal(rawMin))
		case f.Type.Signed:
			g.pf("%s%s = (%s) %s;\n", ind, expr, g.storageType(f), intLit(rawMin, "LL"))
		default:
			g.pf("%s%s = (%s) %s;\n", ind, expr, g.storageType(f), intLit(rawMin, "ULL"))
		}
		return
	}
	if !f.Type.Signed {
		g.emitReadUfixed(f, expr, ind)
		return
	}
	// The temp is REQUIRED on read even where the write could cast inline:
	// &value->small is int16_t* and the call wants serialize_int32_t*.
	g.pf("%s{\n%s    %s fixed_value;\n", ind, ind, temp)
	if f.Type.Width == 128 {
		g.pf("%s    fixed_value = serialize_int128_make( 0, 0 );\n", ind)
	} else {
		g.pf("%s    fixed_value = 0;\n", ind)
	}
	g.call(ind+"    ", fmt.Sprintf("serialize_read_fixed%s( stream, &fixed_value, %d, %d, %s, %s )",
		suffix, f.Type.IntBits, f.Type.FracBits, g.renderIntSuffixed(f.IntMinExpr, lo, "LL"), g.renderIntSuffixed(f.IntMaxExpr, hi, "LL")))
	if f.Type.Width == 128 {
		g.pf("%s    %s = fixed_value;\n%s}\n", ind, expr, ind)
		return
	}
	g.pf("%s    %s = (%s) fixed_value;\n%s}\n", ind, expr, g.storageType(f), ind)
}

// emitReadUfixed is emitWriteUfixed's read twin: the same per-width entry
// routing, with the raw recovered from the entry's signed carrier by the
// inverse bit-exact conversion. A decoded raw is inside the raw bounds or
// the read already failed, so every narrowing below is lossless.
func (g *gen) emitReadUfixed(f *ir.Field, expr, ind string) {
	lo := g.renderIntSuffixed(f.IntMinExpr, f.IntMin, "LL")
	hi := g.renderIntSuffixed(f.IntMaxExpr, f.IntMax, "LL")
	switch {
	case f.Type.Width <= 32:
		g.pf("%s{\n%s    serialize_int64_t fixed_value = 0;\n", ind, ind)
		g.call(ind+"    ", fmt.Sprintf("serialize_read_fixed64( stream, &fixed_value, %d, %d, %s, %s )",
			f.Type.IntBits, f.Type.FracBits, lo, hi))
		g.pf("%s    %s = (%s) fixed_value;\n%s}\n", ind, expr, g.storageType(f), ind)
	case f.Type.Width == 64:
		g.pf("%s{\n%s    serialize_int128_t fixed_value = serialize_int128_make( 0, 0 );\n", ind, ind)
		g.call(ind+"    ", fmt.Sprintf("serialize_read_fixed128( stream, &fixed_value, %d, %d, %s, %s )",
			f.Type.IntBits, f.Type.FracBits, lo, hi))
		g.pf("%s    %s = fixed_value.lo;\n%s}\n", ind, expr, ind)
	default:
		g.pf("%s{\n%s    serialize_int128_t fixed_value = serialize_int128_make( 0, 0 );\n", ind, ind)
		g.call(ind+"    ", fmt.Sprintf("serialize_read_fixed128( stream, &fixed_value, %d, %d, %s, %s )",
			f.Type.IntBits, f.Type.FracBits, lo, hi))
		g.pf("%s    %s = serialize_uint128_make( fixed_value.hi, fixed_value.lo );\n%s}\n", ind, expr, ind)
	}
}

// ---- 128-bit integers ----

func (g *gen) emitWrite128(f *ir.Field, expr, ind string) {
	if !f.Type.Signed {
		// uint128 is NOT ranged: always 128 bits, low half first
		g.call(ind, fmt.Sprintf("serialize_write_uint128( stream, %s )", expr))
		return
	}
	if f.IntMin == nil || f.IntMax == nil {
		g.unsupported("int128 field %s has no resolved range — int128 is always ranged (SPEC §4.3)", f.Name)
		return
	}
	if f.IntMin.Cmp(f.IntMax) == 0 {
		// degenerate range: ZERO bits — refusal only (SPEC §4.6)
		g.pf("%sif ( !serialize_int128_equal( %s, %s ) )\n%s{\n%s    return 0;\n%s}\n",
			ind, expr, g.int128Literal(f.IntMinExpr, f.IntMin), ind, ind, ind)
		return
	}
	g.call(ind, fmt.Sprintf("serialize_write_int128( stream, %s, %s, %s )",
		expr, g.int128Literal(f.IntMinExpr, f.IntMin), g.int128Literal(f.IntMaxExpr, f.IntMax)))
}

func (g *gen) emitRead128(f *ir.Field, expr, ind string) {
	if !f.Type.Signed {
		g.call(ind, fmt.Sprintf("serialize_read_uint128( stream, &%s )", expr))
		return
	}
	if f.IntMin == nil || f.IntMax == nil {
		g.unsupported("int128 field %s has no resolved range", f.Name)
		return
	}
	if f.IntMin.Cmp(f.IntMax) == 0 {
		// degenerate range: zero bits — materialize (SPEC §4.6)
		g.pf("%s%s = %s;\n", ind, expr, g.int128Literal(f.IntMinExpr, f.IntMin))
		return
	}
	g.call(ind, fmt.Sprintf("serialize_read_int128( stream, &%s, %s, %s )",
		expr, g.int128Literal(f.IntMinExpr, f.IntMin), g.int128Literal(f.IntMaxExpr, f.IntMax)))
}

// int128Literal renders a big.Int as a serialize_int128_t. C has no 128-bit
// literal, so a bound wider than 64 bits is built from its two lanes; a
// value that fits int64 rides from_int64, symbolically where safe (nil e:
// the value is derived, never symbolic).
func (g *gen) int128Literal(e ir.Expr, v *big.Int) string {
	if v.IsInt64() {
		return fmt.Sprintf("serialize_int128_from_int64( %s )", g.renderIntSuffixed(e, v, "LL"))
	}
	// two's complement lanes of the 128-bit value
	mod := new(big.Int).Lsh(big.NewInt(1), 128)
	u := new(big.Int).Mod(v, mod)
	lo := new(big.Int).And(u, new(big.Int).SetUint64(^uint64(0)))
	hi := new(big.Int).Rsh(u, 64)
	return fmt.Sprintf("serialize_int128_make( %sULL, %sULL )", hi.String(), lo.String())
}
