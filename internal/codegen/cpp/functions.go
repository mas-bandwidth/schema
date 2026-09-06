// Write/Read function emission for types (SPEC §6.1 items 2-4, §6.2):
// straight-line split functions against the classic serialize one-way macro
// families, plus MaxBits/MaxBytes constants and the union tag wire.
package cpp

import (
	"fmt"
	"math"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- worst-case wire size: shared in ir/wire.go ----

func bitsRequired(min, max *big.Int) int64 { return ir.BitsRequired(min, max) }

func (g *gen) maxBitsStruct(st *ir.Struct) int64 { return ir.MaxBitsStruct(st) }

// ---- function emission ----

// emitStructMaxBits emits the MaxBits/MaxBytes bounds beside the struct —
// data-side, because buffer sizing needs no serialize dependency. The
// MaxBytes comment carries the read half of the serialize buffer contract:
// the allocation backing a read buffer must extend at least 8 bytes past
// the data (serialize::BitReader loads unconditional 64-bit windows), so a
// caller sizing a receive allocation at exactly MaxBytes would be handing
// the reader undefined behavior. The full contract is stated once per file
// (maxBytesTail); every later MaxBytes carries the short form.
func (g *gen) emitStructMaxBits(st *ir.Struct) {
	maxBits := g.maxBitsStruct(st)
	g.pf("inline constexpr int64_t %sMaxBits = %d; // longest wire path; align pads at worst case (SPEC §6.1)\n", st.Name, maxBits)
	g.pf("inline constexpr int64_t %sMaxBytes = %d;%s\n\n", st.Name, ir.MaxBytes(maxBits), g.maxBytesTail())
}

// maxBytesTail is the comment after a MaxBytes constant: the whole buffer
// contract on the file's first, the short form after.
func (g *gen) maxBytesTail() string {
	if g.saidReadSlack {
		return " // 8-byte write granularity; read slack per the contract above"
	}
	g.saidReadSlack = true
	return " // rounded up to the 8-byte write-buffer granularity; a read buffer's allocation must extend at least 8 bytes past the data — the reader loads 64-bit windows"
}

// emitUnionMaxBits mirrors emitStructMaxBits: the tag plus the largest arm
// (SPEC §4.8 — None costs the tag bits only).
func (g *gen) emitUnionMaxBits(d *ir.Union) {
	maxBits := ir.MaxBitsUnion(d)
	g.pf("inline constexpr int64_t %sMaxBits = %d; // tag + the largest arm; None costs the tag only (SPEC §4.8)\n", d.Name, maxBits)
	g.pf("inline constexpr int64_t %sMaxBytes = %d;%s\n\n", d.Name, ir.MaxBytes(maxBits), g.maxBytesTail())
}

// emitUnionWire is the message dispatch pair scaled down to a field type
// (SPEC §4.8): the write validates the tag BEFORE it rides — an out-of-set
// tag writes nothing, it never desyncs the stream — and the read rejects a
// tag above the count inside read_int, then constructs the selected arm
// with its defaults before decoding it.
func (g *gen) emitUnionWire(d *ir.Union) {
	g.needsSerialize = true
	tag := d.Name + "Type"

	g.pf("SCHEMA_WRITE_INLINE bool Write%s( serialize::WriteStream & stream, const %s & value )\n{\n", d.Name, d.Name)
	if d.Max == 0 {
		g.pf("    (void) stream;\n")
		g.pf("    // an empty union holds only None and its degenerate tag range [0, 0]\n")
		g.pf("    // costs zero bits (SPEC §4.8)\n")
		g.pf("    return value.type == %s::None;\n}\n\n", tag)
	} else {
		bits := bitsRequired(big.NewInt(0), big.NewInt(d.Max))
		g.pf("    switch ( value.type )\n    {\n")
		g.pf("        case %s::None:\n", tag)
		g.pf("            write_bits( stream, 0u, %d );\n", bits)
		g.pf("            return true; // no payload — the tag is the whole wire (SPEC §4.8)\n")
		for i, v := range d.Variants {
			g.pf("        case %s::%s:\n", tag, ir.GoExportName(v.Name))
			g.pf("            write_bits( stream, %du, %d );\n", i+1, bits)
			g.pf("            return Write%s( stream, value.%s );\n", v.Type, v.Name)
		}
		g.pf("        default:\n")
		g.pf("            break;\n")
		g.pf("    }\n")
		g.pf("    return false; // not a %s value; nothing was written (SPEC §4.8)\n}\n\n", tag)
	}

	g.pf("SCHEMA_READ_INLINE bool Read%s( serialize::ReadStream & stream, %s & value )\n{\n", d.Name, d.Name)
	if d.Max == 0 {
		g.pf("    (void) stream; // zero wire bits — only None exists\n")
		g.pf("    value.type = %s::None;\n    return true;\n}\n\n", tag)
		return
	}
	g.pf("    int32_t tag_value = 0;\n")
	g.pf("    read_int( stream, tag_value, 0, %d ); // rejects a tag above the count (SPEC §4.8)\n", d.Max)
	g.pf("    value.type = %s( tag_value );\n", tag)
	g.pf("    switch ( value.type )\n    {\n")
	g.pf("        case %s::None:\n            return true;\n", tag)
	for _, v := range d.Variants {
		g.pf("        case %s::%s:\n", tag, ir.GoExportName(v.Name))
		g.needsNew = true
		g.pf("            ::new ( (void*) &value.%s ) %s{};\n", v.Name, v.Type)
		g.pf("            return Read%s( stream, value.%s );\n", v.Type, v.Name)
	}
	g.pf("    }\n    return false;\n}\n\n")
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
	// (found by FuzzGeneratedCompiles); they are harmless when a
	// nested call does use the parameters.
	zeroWire := len(st.Items) > 0 && ir.MaxBitsStruct(st) == 0
	// items but no fields: reserved/const/align carry wire bits with no
	// storage, so the body uses the stream and never the value (found by
	// FuzzGeneratedCompiles)
	noStorage := len(st.Items) > 0 && len(st.Fields) == 0

	g.pf("SCHEMA_WRITE_INLINE bool Write%s( serialize::WriteStream & stream, const %s & value )\n{\n", st.Name, st.Name)
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

	g.pf("SCHEMA_READ_INLINE bool Read%s( serialize::ReadStream & stream, %s & value )\n{\n", st.Name, st.Name)
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
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString || f.Type.Kind == ir.TBytes:
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
			case *ir.Union:
				// zero IS None: the tag is sentinel-zero, and §5's zero rule
				// wants the whole value cleared — the struct memset exactly
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

// ---- compile-time bound emission (the const-params lever) ----
//
// A ranged integer's min/max/bit count are schema constants, so the GENERATOR
// folds them: the write emits the offset from min in a bit count computed at
// generation time, through the always-inline write_bits macro — no runtime
// bits_required, no min/max parameter traffic, and the 32/64 dword split
// resolves here, not at run time. The wire bytes are identical to the runtime
// SerializeInteger/SerializeInteger64 forms (the wire-identity property,
// re-proven by the wire golden gate), and the range assert the runtime form
// carried survives for debug parity. Reads stay on the runtime macros: the
// branchless reader already folds — the measurements found nothing to gain there.
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

// emitWriteCount writes a counted array's count. A scalar's range is a §5
// writer contract held by an assert. A count outside [lo, hi] is REFUSED in
// every build instead, because the count guards the element loop and the pack
// subtracts the low bound, so a count below lo wraps and an unchecked write
// reports success on bytes no reader accepts (SPEC §4.6).
//
// bits is always at least 1 here. §4.6 refuses [Min..N]T with Min at or above
// N, so a count range is never degenerate and the zero-bit path a scalar range
// needs has no case to serve.
func (g *gen) emitWriteCount(expr, lo, hi string, bits int64, loZero bool, ind string) {
	g.pf("%sif ( int32_t( %s ) < int32_t( %s ) || int32_t( %s ) > int32_t( %s ) )\n%s{\n", ind, expr, lo, expr, hi, ind)
	g.pf("%s    return false; // a count outside its wire range is refused in every build (SPEC §4.6)\n%s}\n", ind, ind)
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
			g.emitWriteCount(name+"_count", fmt.Sprintf("%d", f.ArrayMin), bound,
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
			// (SPEC §4.6). The one legal raw is min << F.
			// The compare runs in the storage's own signedness: a wide ufixed
			// raw can live above int64, where the signed cast would mangle it.
			rawMin := new(big.Int).Lsh(f.IntMin, uint(f.Type.FracBits))
			switch {
			case f.Type.Width == 128:
				g.pf("%sserialize_assert( %s == %s );\n", ind, name, g.render128(nil, rawMin, f.Type.Signed))
			case f.Type.Signed:
				g.pf("%sserialize_assert( int64_t( %s ) == %s );\n", ind, name, cppInt64Lit(rawMin))
			default:
				g.pf("%sserialize_assert( uint64_t( %s ) == %sull );\n", ind, name, rawMin.String())
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
					// (SPEC §4.6)
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
					// whole write (SPEC §4.6)
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
			steps, wireBits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
			min32 := float32(f.FMin)
			delta := float32(f.FMax) - min32
			g.pf("%s{\n%s    float compressed_value = %s;\n", ind, ind, name)
			g.pf("%s    serialize_compressed_float_precomputed( stream, compressed_value, %du, %d, %s, %s );\n",
				ind, steps, wireBits, formatFloat(float64(delta), true), formatFloat(float64(min32), true))
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
		}
		g.emitWriteRangedFold32(name+"_length", "0", g.renderInt(f.Type.SizeExpr, big.NewInt(f.Type.Size)),
			bitsRequired(big.NewInt(0), big.NewInt(f.Type.Size)), true, ind)
		g.pf("%swrite_bytes( stream, %s, %s_length );\n", ind, name, name)
	case ir.TWString:
		// length in [0, N], then one 32-BIT GROUP per code unit and NO ALIGN
		// anywhere — the classic serialize_wstring framing over a buffer of
		// N + 1 (SPEC §4.12)
		//
		// Two things are checked on write, both writer misuse rather than
		// content (SPEC §4.12, §5): the used length guards the copy, and a
		// zero code unit among the used units is refused, §4.7's interior-null
		// rule in code-unit terms. Surrogate pairing is NOT checked here — it
		// is a writer obligation the READER enforces, and the reader refuses
		// an unpaired surrogate under §4.12's rules.
		g.pf("%sfor ( int32_t i = 0; i < %s_length; i++ )\n%s{\n", ind, name, ind)
		g.pf("%s    serialize_assert( %s[i] != 0 );\n%s}\n", ind, name, ind)
		g.emitWriteRangedFold32(name+"_length", "0", g.renderInt(f.Type.SizeExpr, big.NewInt(f.Type.Size)),
			bitsRequired(big.NewInt(0), big.NewInt(f.Type.Size)), true, ind)
		g.pf("%sfor ( int32_t i = 0; i < %s_length; i++ )\n%s{\n", ind, name, ind)
		g.pf("%s    write_bits( stream, uint32_t( %s[i] ), 32 );\n%s}\n", ind, name, ind)
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
		case *ir.Union:
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
			// min << F, materialized with no wire call (SPEC §4.6); the
			// literal rides the storage's own signedness
			rawMin := new(big.Int).Lsh(f.IntMin, uint(f.Type.FracBits))
			switch {
			case f.Type.Width == 128:
				g.pf("%s%s = %s;\n", ind, name, g.render128(nil, rawMin, f.Type.Signed))
			case f.Type.Signed:
				typ, _ := g.cppFieldType(f.Type)
				g.pf("%s%s = %s( %s );\n", ind, name, typ, cppInt64Lit(rawMin))
			default:
				typ, _ := g.cppFieldType(f.Type)
				g.pf("%s%s = %s( %sull );\n", ind, name, typ, rawMin.String())
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
			steps, wireBits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
			min32 := float32(f.FMin)
			delta := float32(f.FMax) - min32
			g.pf("%sserialize_compressed_float_precomputed( stream, %s, %du, %d, %s, %s );\n",
				ind, name, steps, wireBits, formatFloat(float64(delta), true), formatFloat(float64(min32), true))
			return
		}
		g.pf("%sread_float( stream, %s );\n", ind, name)
	case ir.TFloat64:
		g.pf("%sread_double( stream, %s );\n", ind, name)
	case ir.TString, ir.TBytes:
		g.pf("%sread_int( stream, %s_length, 0, %s );\n", ind, name, g.renderInt(f.Type.SizeExpr, big.NewInt(f.Type.Size)))
		g.pf("%sread_bytes( stream, %s, %s_length );\n", ind, name, name)
		if f.Type.Kind == ir.TString {
			// the interior-null rule is generated-code validation (SPEC §4.7);
			// the word-wise scan lives in schema_interior_null (nullscan.go)
			g.pf("%sif ( schema_interior_null( reinterpret_cast<const uint8_t *>( %s ), %s_length ) )\n%s{\n", ind, name, name, ind)
			g.pf("%s    return false; // an interior null is content the read refuses (SPEC §4.7)\n%s}\n", ind, ind)
			// a payload that is not well-formed UTF-8 fails the READ, in every
			// build mode (SPEC §4.7). The refusal is terminal: nothing after it
			// has a defined position.
			g.pf("%sif ( !schema_utf8_valid( reinterpret_cast<const uint8_t *>( %s ), %s_length ) )\n%s{\n", ind, name, name, ind)
			g.pf("%s    return false; // malformed UTF-8 is content the read refuses (SPEC §4.7)\n%s}\n", ind, ind)
			g.pf("%s%s[%s_length] = 0;\n", ind, name, name)
		}
	case ir.TWString:
		// length in [0, N], then one 32-bit group per code unit, no align
		// (SPEC §4.12). read_int refuses a length outside the range BEFORE the
		// loop, so the length never drives a copy it has not been bounded for.
		g.pf("%sread_int( stream, %s_length, 0, %s );\n", ind, name, g.renderInt(f.Type.SizeExpr, big.NewInt(f.Type.Size)))
		// The group loop carries every content refusal §4.12 states, in one
		// pass: a group above 0xFFFF is not a code unit, a zero group is the
		// interior null in code-unit terms, and expect_low carries the
		// surrogate pairing — it is set by a high surrogate and cleared by the
		// low one that must immediately follow, so a low without a high, a
		// high not followed by a low, and a high as the FINAL group are all one
		// comparison. Exhaustion at any group is read_bits's own refusal.
		g.pf("%s{\n%s    bool expect_low_surrogate = false;\n", ind, ind)
		g.pf("%s    for ( int32_t i = 0; i < %s_length; i++ )\n%s    {\n", ind, name, ind)
		g.pf("%s        uint32_t group = 0;\n", ind)
		g.pf("%s        read_bits( stream, group, 32 );\n", ind)
		g.pf("%s        if ( group == 0 || group > 0xFFFF )\n%s        {\n", ind, ind)
		g.pf("%s            return false; // a zero group and a group above 0xFFFF are content the read refuses (SPEC §4.12)\n%s        }\n", ind, ind)
		g.pf("%s        const bool high_surrogate = group >= 0xD800 && group <= 0xDBFF;\n", ind)
		g.pf("%s        const bool low_surrogate = group >= 0xDC00 && group <= 0xDFFF;\n", ind)
		g.pf("%s        if ( low_surrogate != expect_low_surrogate )\n%s        {\n", ind, ind)
		g.pf("%s            return false; // an unpaired surrogate is content the read refuses (SPEC §4.12)\n%s        }\n", ind, ind)
		g.pf("%s        expect_low_surrogate = high_surrogate;\n", ind)
		g.pf("%s        %s[i] = char16_t( group );\n%s    }\n", ind, name, ind)
		g.pf("%s    if ( expect_low_surrogate )\n%s    {\n", ind, ind)
		g.pf("%s        return false; // a high surrogate as the final group is unpaired (SPEC §4.12)\n%s    }\n%s}\n", ind, ind, ind)
		// the terminating zero UNIT, always — §5's one stated tail exception
		g.pf("%s%s[%s_length] = 0;\n", ind, name, name)
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
		case *ir.Union:
			g.pf("%sif ( !Read%s( stream, %s ) )\n%s{\n%s    return false;\n%s}\n", ind, f.Type.Name, name, ind, ind, ind)
		}
	}
}
