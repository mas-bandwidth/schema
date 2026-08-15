// Write/Read function emission for types and messages (SPEC §6.1 items 2-4,
// §6.2): straight-line split functions against the classic serialize one-way
// macro families, plus MaxBits/MaxBytes constants and the message tag pair.
// Object view functions (Quantize/Unquantize, view read/write) are a later
// pass.
package cpp

import (
	"fmt"
	"math"
	"math/big"

	"github.com/mas-bandwidth/schema/internal/ir"
)

// ---- worst-case wire size: shared in ir/wire.go ----

func bitsRequired(min, max *big.Int) int64 { return ir.BitsRequired(min, max) }

func (g *gen) maxBitsStruct(st *ir.Struct) int64 { return ir.MaxBitsStruct(st) }

// ---- function emission ----

// emitStructMaxBits emits the MaxBits/MaxBytes bounds beside the struct —
// data-side, because buffer sizing needs no serialize dependency.
func (g *gen) emitStructMaxBits(st *ir.Struct) {
	maxBits := g.maxBitsStruct(st)
	g.pf("inline constexpr int64_t %sMaxBits = %d; // longest wire path; align pads at worst case (SPEC §6.1)\n", st.Name, maxBits)
	g.pf("inline constexpr int64_t %sMaxBytes = %d; // rounded up to the 8-byte write-buffer granularity\n\n", st.Name, ir.MaxBytes(maxBits))
}

// emitStructWire emits the split Write/Read pair for a type or message, in
// the topo order the struct itself was emitted in the data header.
func (g *gen) emitStructWire(st *ir.Struct) {
	g.needsSerialize = true
	// fixed [N]uint8 arrays at statically byte-aligned positions take the
	// runtime's bulk-bytes path instead of a per-byte loop — byte-identical
	// wire (the internal align is zero bits when already aligned), measured
	// ~2x on byte-array-heavy types
	g.bulkBytes = ir.AlignedFixedByteArrays(st)

	// A zero-wire-bit struct (every range degenerate — min == max costs zero
	// bits) emits no stream operation, and its write body's only value uses
	// are serialize_asserts that compile away under NDEBUG. The (void) casts
	// keep -Wall -Wextra -Werror consumers building in every configuration
	// (found by FuzzGeneratedCompiles, issue #22); they are harmless when a
	// nested call does use the parameters.
	zeroWire := len(st.Items) > 0 && ir.MaxBitsStruct(st) == 0
	// items but no fields: reserved/const/align carry wire bits with no
	// storage, so the body uses the stream and never the value (found by
	// FuzzGeneratedCompiles, issue #22)
	noStorage := len(st.Items) > 0 && len(st.Fields) == 0

	g.pf("inline bool Write%s( serialize::WriteStream & stream, const %s & value )\n{\n", st.Name, st.Name)
	if len(st.Items) == 0 {
		g.pf("    (void) stream;\n    (void) value; // empty body — presence is the payload (SPEC §4.6)\n")
	} else {
		if zeroWire {
			g.pf("    (void) stream;\n    (void) value; // zero wire bits — asserts compile away under NDEBUG\n")
		} else if noStorage {
			g.pf("    (void) value; // items only — reserved/const/align carry no storage\n")
		}
		g.emitWriteItems(st.Items, "    ")
	}
	g.pf("    return true;\n}\n\n")

	g.pf("inline bool Read%s( serialize::ReadStream & stream, %s & value )\n{\n", st.Name, st.Name)
	if len(st.Items) == 0 {
		g.pf("    (void) stream;\n    (void) value;\n")
	} else {
		if zeroWire {
			g.pf("    (void) stream; // zero wire bits — nothing to read, defaults prefill below\n")
		}
		if noStorage {
			g.pf("    (void) value; // items only — reserved/const/align carry no storage\n")
		}
		g.emitReadItems(st.Items, "    ")
	}
	g.pf("    return true;\n}\n\n")
}

func (g *gen) emitWriteItems(items []ir.Item, ind string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			g.emitWriteField(item.F, ind)
		case *ir.ConstItem:
			g.pf("%swrite_bits( stream, %sull, %d ); // const(%s, %d) — SPEC §4.3\n", ind, item.Value.String(), item.Bits, item.Value.String(), item.Bits)
		case *ir.ReservedItem:
			g.pf("%swrite_bits( stream, 0ull, %d ); // reserved(%d) — zeros on the wire\n", ind, item.Bits, item.Bits)
		case *ir.AlignItem:
			g.pf("%swrite_align( stream );\n", ind)
		case *ir.Branch:
			neg := ""
			if item.Neg {
				neg = "!"
			}
			g.pf("%sif ( %svalue.%s )\n%s{\n", ind, neg, item.Cond, ind)
			g.emitWriteItems(item.Then, ind+"    ")
			g.pf("%s}\n", ind)
			if item.Else != nil {
				g.pf("%selse\n%s{\n", ind, ind)
				g.emitWriteItems(item.Else, ind+"    ")
				g.pf("%s}\n", ind)
			}
		}
	}
}

func (g *gen) emitReadItems(items []ir.Item, ind string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			g.emitReadField(item.F, ind)
		case *ir.ConstItem:
			g.pf("%s{\n%s    uint64_t const_value = 0;\n", ind, ind)
			g.pf("%s    read_bits( stream, const_value, %d );\n", ind, item.Bits)
			g.pf("%s    if ( const_value != %sull ) // const(%s, %d): a read rejects any other value (SPEC §4.3)\n", ind, item.Value.String(), item.Value.String(), item.Bits)
			g.pf("%s    {\n%s        return false;\n%s    }\n%s}\n", ind, ind, ind, ind)
		case *ir.ReservedItem:
			g.pf("%s{\n%s    uint64_t reserved_value = 0;\n", ind, ind)
			g.pf("%s    read_bits( stream, reserved_value, %d );\n", ind, item.Bits)
			g.pf("%s    if ( reserved_value != 0 ) // reserved(%d): a read rejects nonzero (SPEC §4.3)\n", ind, item.Bits)
			g.pf("%s    {\n%s        return false;\n%s    }\n%s}\n", ind, ind, ind, ind)
		case *ir.AlignItem:
			g.pf("%sread_align( stream ); // rejects nonzero padding (SPEC §4.3)\n", ind)
		case *ir.Branch:
			neg := ""
			if item.Neg {
				neg = "!"
			}
			g.pf("%sif ( %svalue.%s )\n%s{\n", ind, neg, item.Cond, ind)
			g.emitReadItems(item.Then, ind+"    ")
			// the untaken side reads as zero values (SPEC §5)
			g.emitZeroItems(item.Else, ind+"    ")
			g.pf("%s}\n%selse\n%s{\n", ind, ind, ind)
			if item.Else != nil {
				g.emitReadItems(item.Else, ind+"    ")
			}
			g.emitZeroItems(item.Then, ind+"    ")
			g.pf("%s}\n", ind)
		}
	}
}

// emitZeroItems zero-initializes every field under an untaken branch side
// (SPEC §5: fields in untaken branches are set to their ZERO values — not
// their specified defaults; the wire contract stays a pure function of the
// encodings).
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
		g.needsCstring = true
		g.pf("%smemset( %s, 0, sizeof( %s ) );\n%s%s_length = 0;\n", ind, name, name, ind, name)
	case f.Array != ir.ArrayNone:
		// memset gives §5's ZERO values; T{} would restore specified defaults
		g.needsCstring = true
		g.pf("%smemset( (void*) %s, 0, sizeof( %s ) );\n", ind, name, name)
		if f.Array == ir.ArrayCounted {
			g.pf("%s%s_count = 0;\n", ind, name)
		}
	default:
		switch f.Type.Kind {
		case ir.TBool:
			g.pf("%s%s = false;\n", ind, name)
		case ir.TFloat32:
			g.pf("%s%s = 0.0f;\n", ind, name)
		case ir.TFloat64:
			g.pf("%s%s = 0.0;\n", ind, name)
		case ir.TNamed:
			switch f.Type.Ref.(type) {
			case *ir.Enum:
				g.pf("%s%s = %s::None;\n", ind, name, f.Type.Name)
			case *ir.Flags:
				g.pf("%s%s = 0;\n", ind, name)
			case *ir.Struct:
				// memset, not T{}: §5 wants ZERO values recursively, and
				// aggregate init would restore specified defaults instead.
				// The (void*) cast is load-bearing: a generated struct carries
				// default member initializers, which make its implicit default
				// constructor non-trivial, and GCC's -Wclass-memaccess then
				// refuses the memset outright under -Werror. Casting is the
				// documented way to say the raw clear is deliberate. A T{} or a
				// template helper would be the usual advice and both are wrong
				// here — T{} restores defaults instead of zeroing, and a
				// template costs compile time in a header every translation
				// unit includes.
				g.needsCstring = true
				g.pf("%smemset( (void*) &%s, 0, sizeof( %s ) );\n", ind, name, name)
			}
		default:
			g.pf("%s%s = 0;\n", ind, name)
		}
	}
}

// rangeArgs renders the min/max arguments symbolically where possible.
func (g *gen) rangeArgs(f *ir.Field) (string, string) {
	return g.renderInt(f.IntMinExpr, f.IntMin), g.renderInt(f.IntMaxExpr, f.IntMax)
}

// maxUint64 is 2^64 - 1, the top of unsigned-64 storage — the bound against
// which a range guard becomes vacuous.
var maxUint64 = new(big.Int).SetUint64(math.MaxUint64)

// intRangePath picks the runtime call family for a ranged integer.
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

// ---- compile-time bound emission (the const-params lever, serialize #25) ----
//
// A ranged integer's min/max/bit count are schema constants, so the GENERATOR
// folds them: the write emits the offset from min in a bit count computed at
// generation time, through the always-inline write_bits macro — no runtime
// bits_required, no min/max parameter traffic, and the 32/64 dword split
// resolves here, not at run time. The wire bytes are identical to the runtime
// SerializeInteger/SerializeInteger64 forms (#25's wire-identity property,
// re-proven by the wire golden gate), and the range assert the runtime form
// carried survives for debug parity. Reads stay on the runtime macros: the
// branchless reader already folds — #25 measured nothing to gain there.
//
// Deliberately NOT serialize's SerializeIntConst/SerializeBitsConst template
// forms, though they compute the same encoding: instantiations shared by
// several call sites (repeated bounds are the norm in real schemas) get
// OUTLINED — measured on this corpus (clang 17, M2): every shared
// instantiation went out of line and the by-reference value forced a stack
// round-trip per field, costing 10-33% on ranged-int-heavy writes, while the
// macro expansion is unconditionally inline and keeps the bit writer's state
// in registers across consecutive fields. The generator folding its own
// constants gets the entire benefit the templates exist to deliver with no
// new call boundary.

// emitWriteRangedFold32 writes an int in [lo,hi] as offset-from-lo in a
// generation-time bit count (the int32 family: SerializeInteger's encoding).
func (g *gen) emitWriteRangedFold32(expr, lo, hi string, bits int64, loZero bool, ind string) {
	g.pf("%sserialize_assert( int32_t( %s ) >= int32_t( %s ) && int32_t( %s ) <= int32_t( %s ) );\n", ind, expr, lo, expr, hi)
	if bits == 0 {
		// A degenerate range costs ZERO BITS -- the value is known from the
		// range alone. Emit the range assert and nothing else: write_bits( .., 0 )
		// would reach SerializeBits, whose bit count must be at least 1.
		return
	}
	if loZero {
		g.pf("%swrite_bits( stream, uint32_t( %s ), %d );\n", ind, expr, bits)
	} else {
		g.pf("%swrite_bits( stream, uint32_t( %s ) - uint32_t( %s ), %d );\n", ind, expr, lo, bits)
	}
}

// emitWriteRangedFold64 is the int64 family twin (SerializeInteger64's
// encoding: low dword first, then the high remainder — the write_bits macro's
// own >32 split, byte-identical).
func (g *gen) emitWriteRangedFold64(expr, lo, hi string, bits int64, loZero bool, ind string) {
	g.pf("%sserialize_assert( int64_t( %s ) >= int64_t( %s ) && int64_t( %s ) <= int64_t( %s ) );\n", ind, expr, lo, expr, hi)
	if bits == 0 {
		// A degenerate range costs ZERO BITS -- the value is known from the
		// range alone. Emit the range assert and nothing else: write_bits( .., 0 )
		// would reach SerializeBits, whose bit count must be at least 1.
		return
	}
	if loZero {
		g.pf("%swrite_bits( stream, uint64_t( %s ), %d );\n", ind, expr, bits)
	} else {
		g.pf("%swrite_bits( stream, uint64_t( %s ) - uint64_t( %s ), %d );\n", ind, expr, lo, bits)
	}
}

func (g *gen) emitWriteField(f *ir.Field, ind string) {
	name := "value." + f.Name
	if f.Array != ir.ArrayNone {
		bound := g.renderInt(f.ArrayExpr, big.NewInt(f.ArrayBound))
		if g.bulkBytes[f] {
			// statically byte-aligned [N]uint8: the bulk path is
			// byte-identical to the per-byte loop (its internal align is
			// zero bits here) and memcpys instead of 8-bit packing
			g.pf("%swrite_bytes( stream, %s, %s ); // byte-aligned [N]uint8 — bulk copy, wire-identical to the per-byte loop\n", ind, name, bound)
			return
		}
		if f.Array == ir.ArrayCounted {
			g.emitWriteRangedFold32(name+"_count", fmt.Sprintf("%d", f.ArrayMin), bound,
				bitsRequired(big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound)), f.ArrayMin == 0, ind)
			g.pf("%sfor ( int32_t i = 0; i < %s_count; i++ )\n%s{\n", ind, name, ind)
		} else {
			g.pf("%sfor ( int32_t i = 0; i < %s; i++ )\n%s{\n", ind, bound, ind)
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
			// degenerate range: ZERO bits — the §5 misuse assert and no wire
			// call at all, so no runtime degenerate support is needed
			// (SPEC §4.6, decided 2026-08-15). The one legal raw is min << F.
			rawMin := new(big.Int).Lsh(f.IntMin, uint(f.Type.FracBits))
			if f.Type.Width == 128 {
				g.pf("%sserialize_assert( %s == %s );\n", ind, name, g.render128(nil, rawMin, true))
			} else {
				g.pf("%sserialize_assert( int64_t( %s ) == %s );\n", ind, name, cppInt64Lit(rawMin))
			}
			return
		}
		// the Q format and the whole-unit bounds are compile-time constants of
		// the call site — part of the wire format, exactly like a ranged
		// integer's bounds (STANDARD.md, fixed); write misuse debug-asserts
		// inside the macro per §5. The macro's unified serializer takes a
		// mutable reference, so the write side goes through a local — the
		// compressed-float write's own shape
		lo, hi := g.rangeArgs(f)
		typ, _ := g.cppFieldType(f.Type)
		g.pf("%s{\n%s    %s fixed_value = %s;\n", ind, ind, typ, name)
		g.pf("%s    write_fixed( stream, fixed_value, %d, %d, %s, %s );\n", ind, f.Type.IntBits, f.Type.FracBits, lo, hi)
		g.pf("%s}\n", ind)
	case ir.TInt:
		if f.Type.Width == 128 {
			if f.HasIntRange {
				if f.IntMin.Cmp(f.IntMax) == 0 {
					// degenerate range: ZERO bits — assert and no wire call
					// (SPEC §4.6, decided 2026-08-15)
					g.pf("%sserialize_assert( %s == %s );\n", ind, name, g.render128(f.IntMinExpr, f.IntMin, true))
					return
				}
				// int128 is ALWAYS ranged (SPEC §4.3): offset from min in
				// bits_required128 bits — identical bytes to serialize_int64
				// wherever the range fits 64 bits or fewer
				g.pf("%swrite_int128( stream, %s, %s, %s );\n", ind, name,
					g.render128(f.IntMinExpr, f.IntMin, true), g.render128(f.IntMaxExpr, f.IntMax, true))
			} else {
				// uint128 is the raw field: 128 bits, low 64-bit half first
				g.pf("%swrite_uint128( stream, %s );\n", ind, name)
			}
			return
		}
		if f.HasIntRange {
			lo, hi := g.rangeArgs(f)
			switch intRangePath(f.IntMin, f.IntMax) {
			case "int32":
				g.emitWriteRangedFold32(name, lo, hi, bitsRequired(f.IntMin, f.IntMax), f.IntMin.Sign() == 0, ind)
			case "int64":
				g.emitWriteRangedFold64(name, lo, hi, bitsRequired(f.IntMin, f.IntMax), f.IntMin.Sign() == 0, ind)
			default:
				// full-range unsigned raw offset: this path bypasses the
				// runtime's ranged calls, so it supplies the write-side range
				// assert those calls carry (writer misuse must not wrap into
				// valid-looking wire); vacuous halves are elided — the uint64
				// storage cannot go below 0 or above 2^64-1
				loVacuous := f.IntMin.Sign() == 0
				hiVacuous := f.IntMax.Cmp(maxUint64) == 0
				switch {
				case !loVacuous && !hiVacuous:
					g.pf("%sserialize_assert( %s >= %s && %s <= %sull );\n", ind, name, lo, name, f.IntMax.String())
				case !loVacuous:
					g.pf("%sserialize_assert( %s >= %s );\n", ind, name, lo)
				case !hiVacuous:
					g.pf("%sserialize_assert( %s <= %sull );\n", ind, name, f.IntMax.String())
				}
				if bitsRequired(f.IntMin, f.IntMax) == 0 {
					// degenerate range: zero bits — the assert above is the
					// whole write (SPEC §4.6, decided 2026-08-15)
					return
				}
				if loVacuous {
					g.pf("%swrite_bits( stream, %s, %d );\n", ind, name, bitsRequired(f.IntMin, f.IntMax))
				} else {
					g.pf("%swrite_bits( stream, uint64_t( %s ) - %s, %d );\n", ind, name, lo, bitsRequired(f.IntMin, f.IntMax))
				}
			}
			return
		}
		if f.Type.Signed && f.Type.Width < 64 {
			// cast to the same-width unsigned first: write_bits sign-extends a
			// negative intN into a uint64 and the high bits corrupt the stream
			g.pf("%swrite_bits( stream, uint%d_t( %s ), %d );\n", ind, f.Type.Width, name, f.Type.Width)
			return
		}
		g.pf("%swrite_bits( stream, %s, %d );\n", ind, name, f.Type.Width)
	case ir.TBits:
		g.pf("%swrite_bits( stream, %s, %d );\n", ind, name, f.Type.Width)
	case ir.TBool:
		g.pf("%swrite_bool( stream, %s );\n", ind, name)
	case ir.TFloat32:
		if f.HasFloatRange {
			g.pf("%s{\n%s    float compressed_value = %s;\n", ind, ind, name)
			g.pf("%s    serialize_compressed_float( stream, compressed_value, %s, %s, %s );\n",
				ind, formatFloat(f.FMin, true), formatFloat(f.FMax, true), formatFloat(f.Resolution, true))
			g.pf("%s}\n", ind)
			return
		}
		g.pf("%swrite_float( stream, %s );\n", ind, name)
	case ir.TFloat64:
		g.pf("%swrite_double( stream, %s );\n", ind, name)
	case ir.TString, ir.TBytes:
		// length in [0, N], align, then the used bytes — the classic
		// serialize_string framing over a buffer of N + 1 (SPEC §4.7)
		if f.Type.Kind == ir.TString {
			// interior nulls are writer misuse: debug-assert per §5 (the read
			// side rejects them as validation — §4.7)
			g.pf("%sfor ( int32_t i = 0; i < %s_length; i++ )\n%s{\n", ind, name, ind)
			g.pf("%s    serialize_assert( %s[i] != 0 );\n%s}\n", ind, name, ind)
			// well-formed UTF-8 by contract, writer-trusted: debug-only
			// assert, no read-path validation (SPEC §4.7, decided 2026-08-15)
			g.pf("%sserialize_assert( schema_utf8_valid( reinterpret_cast<const uint8_t *>( %s ), %s_length ) );\n", ind, name, name)
		}
		g.emitWriteRangedFold32(name+"_length", "0", g.renderInt(f.Type.SizeExpr, big.NewInt(f.Type.Size)),
			bitsRequired(big.NewInt(0), big.NewInt(f.Type.Size)), true, ind)
		g.pf("%swrite_bytes( stream, %s, %s_length );\n", ind, name, name)
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			g.emitWriteRangedFold32(name, "0", fmt.Sprintf("%d", ref.Max),
				bitsRequired(big.NewInt(0), big.NewInt(ref.Max)), true, ind)
		case *ir.Flags:
			if ref.WireBits < 64 {
				// storage is wider than the wire: a mask bit above the wire
				// width is writer misuse, not silent truncation
				g.pf("%sserialize_assert( %s < ( 1ull << %d ) );\n", ind, name, ref.WireBits)
			}
			g.pf("%swrite_bits( stream, %s, %d );\n", ind, name, ref.WireBits)
		case *ir.Struct:
			g.pf("%sif ( !Write%s( stream, %s ) )\n%s{\n%s    return false;\n%s}\n", ind, f.Type.Name, name, ind, ind, ind)
		}
	}
}

func (g *gen) emitReadField(f *ir.Field, ind string) {
	name := "value." + f.Name
	if f.Array != ir.ArrayNone {
		bound := g.renderInt(f.ArrayExpr, big.NewInt(f.ArrayBound))
		if g.bulkBytes[f] {
			g.pf("%sread_bytes( stream, %s, %s ); // byte-aligned [N]uint8 — bulk copy, wire-identical to the per-byte loop\n", ind, name, bound)
			return
		}
		if f.Array == ir.ArrayCounted {
			g.pf("%sread_int( stream, %s_count, %d, %s );\n", ind, name, f.ArrayMin, bound)
			g.pf("%sfor ( int32_t i = 0; i < %s_count; i++ )\n%s{\n", ind, name, ind)
		} else {
			g.pf("%sfor ( int32_t i = 0; i < %s; i++ )\n%s{\n", ind, bound, ind)
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
			// min << F, materialized with no wire call (SPEC §4.6)
			rawMin := new(big.Int).Lsh(f.IntMin, uint(f.Type.FracBits))
			if f.Type.Width == 128 {
				g.pf("%s%s = %s;\n", ind, name, g.render128(nil, rawMin, true))
			} else {
				typ, _ := g.cppFieldType(f.Type)
				g.pf("%s%s = %s( %s );\n", ind, name, typ, cppInt64Lit(rawMin))
			}
			return
		}
		// the macro validates the raw offset against the raw bounds and
		// rejects — never clamps — returning false on a hostile stream
		lo, hi := g.rangeArgs(f)
		g.pf("%sread_fixed( stream, %s, %d, %d, %s, %s );\n", ind, name, f.Type.IntBits, f.Type.FracBits, lo, hi)
	case ir.TInt:
		if f.Type.Width == 128 {
			if f.HasIntRange {
				if f.IntMin.Cmp(f.IntMax) == 0 {
					// degenerate range: zero bits — materialize (SPEC §4.6)
					g.pf("%s%s = %s;\n", ind, name, g.render128(f.IntMinExpr, f.IntMin, true))
					return
				}
				// rejects a decoded offset beyond max - min (reject, never clamp)
				g.pf("%sread_int128( stream, %s, %s, %s );\n", ind, name,
					g.render128(f.IntMinExpr, f.IntMin, true), g.render128(f.IntMaxExpr, f.IntMax, true))
			} else {
				g.pf("%sread_uint128( stream, %s );\n", ind, name)
			}
			return
		}
		if f.HasIntRange {
			if f.IntMin.Cmp(f.IntMax) == 0 {
				// degenerate range: zero bits — the value is the range,
				// materialized with no wire call (SPEC §4.6)
				lit := g.renderInt(f.IntMinExpr, f.IntMin)
				if !f.Type.Signed && !f.IntMin.IsInt64() {
					lit = f.IntMin.String() + "ull" // above the signed-literal domain
				}
				g.pf("%s%s = %s( %s );\n", ind, name, cppInt2(f.Type.Signed, f.Type.Width), lit)
				return
			}
			lo, hi := g.rangeArgs(f)
			switch intRangePath(f.IntMin, f.IntMax) {
			case "int32":
				if f.Type.Width < 32 {
					// a wide temp, cast on the member assignment — keeps the
					// narrowing explicit for stricter compilers
					g.pf("%s{\n%s    int32_t range_value = 0;\n", ind, ind)
					g.pf("%s    read_int( stream, range_value, %s, %s );\n", ind, lo, hi)
					g.pf("%s    %s = %s( range_value );\n%s}\n", ind, name, cppInt2(f.Type.Signed, f.Type.Width), ind)
					return
				}
				g.pf("%sread_int( stream, %s, %s, %s );\n", ind, name, lo, hi)
			case "int64":
				g.pf("%sread_int64( stream, %s, %s, %s );\n", ind, name, lo, hi)
			default:
				diff := new(big.Int).Sub(f.IntMax, f.IntMin)
				g.pf("%s{\n%s    uint64_t offset_value = 0;\n", ind, ind)
				g.pf("%s    read_bits( stream, offset_value, %d );\n", ind, bitsRequired(f.IntMin, f.IntMax))
				if diff.Cmp(maxUint64) != 0 {
					// a full-width diff cannot overflow its own read — elided
					g.pf("%s    if ( offset_value > %sull )\n%s    {\n%s        return false;\n%s    }\n", ind, diff.String(), ind, ind, ind)
				}
				if f.IntMin.Sign() == 0 {
					g.pf("%s    %s = offset_value;\n%s}\n", ind, name, ind)
				} else {
					g.pf("%s    %s = offset_value + %s;\n%s}\n", ind, name, lo, ind)
				}
			}
			return
		}
		if f.Type.Signed || f.Type.Width < 32 {
			tempT := "uint32_t"
			if f.Type.Width == 64 {
				tempT = "uint64_t"
			}
			g.pf("%s{\n%s    %s raw_value = 0;\n", ind, ind, tempT)
			g.pf("%s    read_bits( stream, raw_value, %d );\n", ind, f.Type.Width)
			g.pf("%s    %s = %s( raw_value );\n%s}\n", ind, name, cppInt2(f.Type.Signed, f.Type.Width), ind)
			return
		}
		g.pf("%sread_bits( stream, %s, %d );\n", ind, name, f.Type.Width)
	case ir.TBits:
		g.pf("%sread_bits( stream, %s, %d );\n", ind, name, f.Type.Width)
	case ir.TBool:
		g.pf("%sread_bool( stream, %s );\n", ind, name)
	case ir.TFloat32:
		if f.HasFloatRange {
			g.pf("%sserialize_compressed_float( stream, %s, %s, %s, %s );\n",
				ind, name, formatFloat(f.FMin, true), formatFloat(f.FMax, true), formatFloat(f.Resolution, true))
			return
		}
		g.pf("%sread_float( stream, %s );\n", ind, name)
	case ir.TFloat64:
		g.pf("%sread_double( stream, %s );\n", ind, name)
	case ir.TString, ir.TBytes:
		g.pf("%sread_int( stream, %s_length, 0, %s );\n", ind, name, g.renderInt(f.Type.SizeExpr, big.NewInt(f.Type.Size)))
		g.pf("%sread_bytes( stream, %s, %s_length );\n", ind, name, name)
		if f.Type.Kind == ir.TString {
			// the interior-null rule is generated-code validation (SPEC §4.7)
			g.pf("%sfor ( int32_t i = 0; i < %s_length; i++ )\n%s{\n", ind, name, ind)
			g.pf("%s    if ( %s[i] == 0 )\n%s    {\n%s        return false;\n%s    }\n%s}\n", ind, name, ind, ind, ind, ind)
			g.pf("%s%s[%s_length] = 0;\n", ind, name, name)
		}
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			g.pf("%s{\n%s    int32_t enum_value = 0;\n", ind, ind)
			g.pf("%s    read_int( stream, enum_value, 0, %d );\n", ind, ref.Max)
			g.pf("%s    %s = %s( enum_value );\n%s}\n", ind, name, f.Type.Name, ind)
		case *ir.Flags:
			g.pf("%sread_bits( stream, %s, %d );\n", ind, name, ref.WireBits)
		case *ir.Struct:
			g.pf("%sif ( !Read%s( stream, %s ) )\n%s{\n%s    return false;\n%s}\n", ind, f.Type.Name, name, ind, ind, ind)
		}
	}
}

// emitMessageData emits the message-level bound and the Message storage
// surface (union or variant) — data-side: holding and routing messages needs
// no serialize dependency (SPEC §4.8, §6.1 item 6).
func (g *gen) emitMessageData() {
	// the dispatch value holds every message by value — the owner file
	// includes each message's home (safe: the owner is topologically last)
	for _, m := range g.unit.Messages {
		g.noteRef(m)
	}
	count := int64(len(g.unit.Messages))
	tagBits := bitsRequired(big.NewInt(0), big.NewInt(count))
	largest := int64(0)
	for _, m := range g.unit.Messages {
		if b := g.maxBitsStruct(g.unit.Structs[m]); b > largest {
			largest = b
		}
	}
	g.pf("// The message-level bound: the tag plus the largest message (SPEC §6.1)\n")
	g.pf("inline constexpr int64_t MessageMaxBits = %d;\n", tagBits+largest)
	g.pf("inline constexpr int64_t MessageMaxBytes = %d; // rounded up to the 8-byte write-buffer granularity\n\n", ir.MaxBytes(tagBits+largest))

	if g.opts.MessageRepr == "variant" {
		g.emitMessageStorageVariant()
	} else {
		g.emitMessageStorageUnion()
	}
}

// emitMessageWire emits the tag wire pair and the WriteMessage/ReadMessage
// dispatch. The per-language dispatch surface (union, variant, factory) is
// deliberately not chosen here — representation is per-language and not part
// of the contract.
func (g *gen) emitMessageWire() {
	g.needsSerialize = true
	// dispatch calls every message's Write/Read — the deps ride the wire headers
	for _, m := range g.unit.Messages {
		g.noteRef(m)
	}
	count := int64(len(g.unit.Messages))
	g.pf("// The message tag wire: MessageType in [0, %d], minimal bits; None = 0 is a\n", count)
	g.pf("// valid wire value meaning *no message* — the stream terminator (SPEC §4.8).\n")
	g.pf("inline bool WriteMessageType( serialize::WriteStream & stream, MessageType value )\n{\n")
	g.emitWriteRangedFold32("value", "0", fmt.Sprintf("%d", count),
		bitsRequired(big.NewInt(0), big.NewInt(count)), true, "    ")
	g.pf("    return true;\n}\n\n")
	g.pf("inline bool ReadMessageType( serialize::ReadStream & stream, MessageType & value )\n{\n")
	g.pf("    int32_t tag_value = 0;\n")
	g.pf("    read_int( stream, tag_value, 0, %d );\n", count)
	g.pf("    value = MessageType( tag_value );\n    return true;\n}\n\n")

	if g.opts.MessageRepr == "variant" {
		g.emitMessageWireVariant()
	} else {
		g.emitMessageWireUnion()
	}
}

// emitMessageStorageUnion (+ emitMessageWireUnion) is the DEFAULT C++
// dispatch surface: a tagged
// struct over an anonymous union — the classic game idiom, zero template
// machinery, zero extra includes. Construction initializes the tag only
// (None); an arm's storage is zero-established when the arm is SELECTED —
// by ReadMessage before it decodes (SPEC §5's read rule is relative to a
// zero-initialized output object, and the arm re-init below provides that
// baseline for exactly the arm the wire tag selects), or by the writer
// assigning the arm (message.chat = Chat{} — the generated structs'
// default member initializers make that a zero value). The constructor
// used to memset the whole Block-sized union; the 2026-08-06 bench pass
// measured that memset at 60.6% of batch-read self-cycles (Zen 4, ~2 KB
// zeroed per ~25 B message), and the zeroing moved to arm selection —
// the exact shape the variant surface always had: default construction
// is monostate, emplace<T>() value-initializes only the selected arm.
func (g *gen) emitMessageStorageUnion() {
	msgs := g.unit.Messages

	g.pf("// The message value: a tagged union — the payload member matching `type` is\n")
	g.pf("// the active one. Construction is the None message: the tag alone is\n")
	g.pf("// initialized (sentinel-zero — a zero tag is None); an arm's storage is\n")
	g.pf("// established ZEROED when the arm is selected — by ReadMessage before it\n")
	g.pf("// decodes (SPEC §5), or by assigning it: message.chat = Chat{}. Bytes of\n")
	g.pf("// unselected arms are indeterminate. No heap, no templates.\n")
	g.pf("// (--cpp-message variant generates a std::variant surface instead.)\n")
	g.pf("struct Message\n{\n")
	g.pf("    MessageType type;\n\n    union\n    {\n")
	for _, m := range msgs {
		g.pf("        %s %s;\n", m, camelToSnake(m))
	}
	g.pf("    };\n\n")
	g.pf("    Message() : type( MessageType::None ) {} // the tag only — arms are zero-established at selection\n")
	g.pf("};\n\n")

	g.pf("inline MessageType GetMessageType( const Message & message )\n{\n")
	g.pf("    return message.type;\n}\n\n")
}

func (g *gen) emitMessageWireUnion() {
	msgs := g.unit.Messages

	// dispatch validates BEFORE the tag rides the wire: an out-of-set type
	// value writes nothing (a tag with no payload would desynchronize the
	// stream), and the tag framing is the tag pair's — one source
	g.pf("inline bool WriteMessage( serialize::WriteStream & stream, const Message & message )\n{\n")
	g.pf("    switch ( message.type )\n    {\n")
	g.pf("        case MessageType::None:\n            return WriteMessageType( stream, MessageType::None ); // the stream terminator (SPEC §4.8)\n")
	for _, m := range msgs {
		g.pf("        case MessageType::%s:\n", m)
		g.pf("            if ( !WriteMessageType( stream, MessageType::%s ) )\n            {\n                return false;\n            }\n", m)
		g.pf("            return Write%s( stream, message.%s );\n", m, camelToSnake(m))
	}
	g.pf("    }\n    return false; // not a message type; nothing was written\n}\n\n")

	g.pf("inline bool ReadMessage( serialize::ReadStream & stream, Message & message )\n{\n")
	g.pf("    MessageType tag_value = MessageType::None;\n")
	g.pf("    if ( !ReadMessageType( stream, tag_value ) )\n    {\n        return false;\n    }\n")
	g.pf("    message.type = tag_value;\n")
	g.pf("    switch ( message.type )\n    {\n")
	g.pf("        case MessageType::None:\n            return true;\n")
	for _, m := range msgs {
		g.pf("        case MessageType::%s:\n            message.%s = %s{};\n            return Read%s( stream, message.%s );\n",
			m, camelToSnake(m), m, m, camelToSnake(m))
	}
	g.pf("    }\n    return false;\n}\n\n")
}

// emitMessageStorageVariant (+ emitMessageWireVariant) is the OPT-IN modern
// surface (--cpp-message
// variant): a std::variant whose INDEX equals the wire tag — monostate is
// None = 0, then each message in tag order. std::variant never heap-allocates
// (storage is inline, the size of the largest message — the same footprint as
// a tagged union), and every generated message is trivially copyable, so the
// variant is too. Generated dispatch is a plain switch on the index; std::visit
// stays available to callers who want compile-time exhaustiveness and costs
// nothing here. Measured cost of <variant>: ~50ms per TU (arm64 clang), which
// is why it is not the default.
func (g *gen) emitMessageStorageVariant() {
	g.needsVariant = true
	msgs := g.unit.Messages
	count := int64(len(msgs))

	g.pf("// The message value: index == wire tag (std::monostate is None = 0, then each\n")
	g.pf("// message in tag order). Inline storage, no heap, trivially copyable.\n")
	g.pf("using Message = std::variant<std::monostate")
	for _, m := range msgs {
		g.pf(", %s", m)
	}
	g.pf(">;\n\n")
	g.pf("static_assert( std::variant_size_v<Message> == %d, \"one alternative per message plus None\" );\n\n", count+1)

	g.pf("inline MessageType GetMessageType( const Message & message )\n{\n")
	g.pf("    return MessageType( message.index() );\n}\n\n")
}

func (g *gen) emitMessageWireVariant() {
	msgs := g.unit.Messages

	// the variant's index is always in-set, so no pre-validation is needed;
	// the tag framing is the tag pair's — one source
	g.pf("inline bool WriteMessage( serialize::WriteStream & stream, const Message & message )\n{\n")
	g.pf("    if ( !WriteMessageType( stream, MessageType( message.index() ) ) )\n    {\n        return false;\n    }\n")
	g.pf("    switch ( message.index() )\n    {\n")
	g.pf("        case 0:\n            return true; // None — the stream terminator (SPEC §4.8)\n")
	for i, m := range msgs {
		g.pf("        case %d:\n            return Write%s( stream, *std::get_if<%s>( &message ) );\n", i+1, m, m)
	}
	g.pf("    }\n    return false;\n}\n\n")

	g.pf("inline bool ReadMessage( serialize::ReadStream & stream, Message & message )\n{\n")
	g.pf("    MessageType tag_value = MessageType::None;\n")
	g.pf("    if ( !ReadMessageType( stream, tag_value ) )\n    {\n        return false;\n    }\n")
	g.pf("    switch ( int32_t( tag_value ) )\n    {\n")
	g.pf("        case 0:\n            message.emplace<std::monostate>();\n            return true;\n")
	for i, m := range msgs {
		g.pf("        case %d:\n            return Read%s( stream, message.emplace<%s>() );\n", i+1, m, m)
	}
	g.pf("    }\n    return false;\n}\n\n")
}
