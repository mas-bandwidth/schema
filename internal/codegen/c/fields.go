package c

// Field-level wire emission. Every rule here mirrors the C++ backend's folded
// form: the bit count is computed at GENERATION time and the offset written
// with serialize_write_bits, rather than calling a ranged helper with runtime
// bounds. That is what makes the C output bit-identical to the other four.

import (
	"fmt"
	"math/big"

	"github.com/mas-bandwidth/schema/internal/ir"
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
		g.call(ind, fmt.Sprintf("serialize_write_int( stream, value->%s_length, 0, %d )", f.Name, f.Type.Size))
		g.call(ind, fmt.Sprintf("serialize_write_bytes( stream, (const serialize_uint8_t *) value->%s, (int) value->%s_length )", f.Name, f.Name))
	case f.Type.Kind == ir.TBytes:
		g.call(ind, fmt.Sprintf("serialize_write_int( stream, value->%s_length, 0, %d )", f.Name, f.Type.Size))
		g.call(ind, fmt.Sprintf("serialize_write_bytes( stream, value->%s, (int) value->%s_length )", f.Name, f.Name))
	case f.Array == ir.ArrayCounted:
		g.call(ind, fmt.Sprintf("serialize_write_int( stream, value->%s_count, %d, %d )", f.Name, f.ArrayMin, f.ArrayBound))
		g.pf("%s{\n%s    int32_t i;\n%s    for ( i = 0; i < value->%s_count; i++ )\n%s    {\n", ind, ind, ind, f.Name, ind)
		g.emitWriteScalar(f, fmt.Sprintf("value->%s[i]", f.Name), ind+"        ")
		g.pf("%s    }\n%s}\n", ind, ind)
	case f.Array == ir.ArrayFixed:
		g.pf("%s{\n%s    int32_t i;\n%s    for ( i = 0; i < %d; i++ )\n%s    {\n", ind, ind, ind, f.ArrayBound, ind)
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
			g.call(ind, fmt.Sprintf("serialize_write_compressed_float( stream, %s, %s, %s, %s )",
				expr, formatFloat(f.FMin), formatFloat(f.FMax), formatFloat(f.Resolution)))
			return
		}
		g.call(ind, fmt.Sprintf("serialize_write_float( stream, %s )", expr))
	case ir.TFloat64:
		g.call(ind, fmt.Sprintf("serialize_write_double( stream, %s )", expr))
	case ir.TInt:
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
		}
	}
}

func (g *gen) emitWriteRangedInt(f *ir.Field, expr, ind string) {
	if !f.HasIntRange {
		// bare integer at its storage width
		if f.Type.Width == 64 {
			g.call(ind, fmt.Sprintf("serialize_write_uint64( stream, (serialize_uint64_t) %s )", expr))
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
		offset = fmt.Sprintf("(%s) - (%s)", expr, f.IntMin.String())
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
func (g *gen) emitRangeAssertWrite(f *ir.Field, expr, ind string) {
	lo, hi := f.IntMin.String(), f.IntMax.String()
	cast := "serialize_int64_t"
	suffix := "LL"
	if !f.Type.Signed && f.Type.Width >= 64 {
		cast = "serialize_uint64_t"
		suffix = "ULL"
	}
	g.pf("%sif ( (%s) %s < %s%s || (%s) %s > %s%s )\n%s{\n%s    return 0; /* out-of-contract writes are refused, not wrapped */\n%s}\n",
		ind, cast, expr, lo, suffix, cast, expr, hi, suffix, ind, ind, ind)
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
		g.call(ind, fmt.Sprintf("serialize_read_int( stream, &value->%s_length, 0, %d )", f.Name, f.Type.Size))
		g.call(ind, fmt.Sprintf("serialize_read_bytes( stream, (serialize_uint8_t *) value->%s, (int) value->%s_length )", f.Name, f.Name))
		// the terminator is not transmitted; the reader supplies it
		g.pf("%svalue->%s[value->%s_length] = 0;\n", ind, f.Name, f.Name)
	case f.Type.Kind == ir.TBytes:
		g.call(ind, fmt.Sprintf("serialize_read_int( stream, &value->%s_length, 0, %d )", f.Name, f.Type.Size))
		g.call(ind, fmt.Sprintf("serialize_read_bytes( stream, value->%s, (int) value->%s_length )", f.Name, f.Name))
	case f.Array == ir.ArrayCounted:
		g.call(ind, fmt.Sprintf("serialize_read_int( stream, &value->%s_count, %d, %d )", f.Name, f.ArrayMin, f.ArrayBound))
		g.pf("%s{\n%s    int32_t i;\n%s    for ( i = 0; i < value->%s_count; i++ )\n%s    {\n", ind, ind, ind, f.Name, ind)
		g.emitReadScalar(f, fmt.Sprintf("value->%s[i]", f.Name), ind+"        ")
		g.pf("%s    }\n%s}\n", ind, ind)
	case f.Array == ir.ArrayFixed:
		g.pf("%s{\n%s    int32_t i;\n%s    for ( i = 0; i < %d; i++ )\n%s    {\n", ind, ind, ind, f.ArrayBound, ind)
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
			g.call(ind, fmt.Sprintf("serialize_read_compressed_float( stream, &%s, %s, %s, %s )",
				expr, formatFloat(f.FMin), formatFloat(f.FMax), formatFloat(f.Resolution)))
			return
		}
		g.call(ind, fmt.Sprintf("serialize_read_float( stream, &%s )", expr))
	case ir.TFloat64:
		g.call(ind, fmt.Sprintf("serialize_read_double( stream, &%s )", expr))
	case ir.TInt:
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
		}
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
		g.pf("%s%s = (%s) (%s);\n", ind, expr, g.storageType(f), f.IntMin.String())
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
	// reject, never clamp -- a value smuggled into the range's bit headroom
	g.pf("%s    if ( offset_value > %sULL )\n%s    {\n%s        return 0;\n%s    }\n", ind, span.String(), ind, ind, ind)
	if f.IntMin.Sign() == 0 {
		g.pf("%s    %s = (%s) offset_value;\n%s}\n", ind, expr, g.storageType(f), ind)
	} else {
		g.pf("%s    %s = (%s) ( offset_value + (%s) );\n%s}\n", ind, expr, g.storageType(f), f.IntMin.String(), ind)
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
