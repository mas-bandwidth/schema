// Write/Read function emission for types and messages (SPEC §6.1 items 2-4,
// §6.2): straight-line split functions against the classic serialize one-way
// macro families, plus MaxBits/MaxBytes constants and the message tag pair.
// Object view functions (Quantize/Unquantize, view read/write) are a later
// pass.
package cpp

import (
	"math"
	"math/big"
	"math/bits"

	"github.com/mas-bandwidth/schema/internal/ir"
)

// ---- worst-case wire size (SPEC §6.1 item 4) ----

// bitsRequired mirrors the runtime's bits_required(min, max): the bit length
// of (max - min). Streams require min < max, so the zero case cannot arise.
func bitsRequired(min, max *big.Int) int64 {
	diff := new(big.Int).Sub(max, min)
	return int64(diff.BitLen())
}

// compressedFloatBits replicates serialize_compressed_float's width
// derivation exactly (float32 arithmetic, the clamp, the ceil — the extracted
// contract's formulas).
func compressedFloatBits(fmin, fmax, res float64) int64 {
	delta := float32(fmax) - float32(fmin)
	values := delta / float32(res)
	if !(values >= 1.0) { // the runtime's own form — it also catches NaN
		values = 1.0
	}
	if values > 4294967040.0 {
		values = 4294967040.0
	}
	maxIntegerValue := uint64(math.Ceil(float64(values)))
	return int64(bits.Len64(maxIntegerValue))
}

// maxBitsField is one field's worst-case wire bits, alignment points counted
// at the worst 7 (SPEC §6.1 item 4).
func (g *gen) maxBitsField(f *ir.Field) int64 {
	elem := g.maxBitsScalar(f)
	switch f.Array {
	case ir.ArrayFixed:
		return f.ArrayBound * elem
	case ir.ArrayCounted:
		return bitsRequired(big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound)) + f.ArrayBound*elem
	default:
		return elem
	}
}

func (g *gen) maxBitsScalar(f *ir.Field) int64 {
	switch f.Type.Kind {
	case ir.TInt:
		if f.HasIntRange {
			return bitsRequired(f.IntMin, f.IntMax)
		}
		return int64(f.Type.Width)
	case ir.TBits:
		return int64(f.Type.Width)
	case ir.TBool:
		return 1
	case ir.TFloat32:
		if f.HasFloatRange {
			return compressedFloatBits(f.FMin, f.FMax, f.Resolution)
		}
		return 32
	case ir.TFloat64:
		return 64
	case ir.TString, ir.TBytes:
		// length prefix + worst-case align pad + the bytes
		return bitsRequired(big.NewInt(0), big.NewInt(f.Type.Size)) + 7 + f.Type.Size*8
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			return bitsRequired(big.NewInt(0), big.NewInt(ref.Max))
		case *ir.Flags:
			return int64(ref.WireBits)
		case *ir.Struct:
			return g.maxBitsStruct(ref)
		}
	}
	return 0
}

// maxBitsStruct is the longest wire path through a struct: branches take the
// larger side (composition cycles are compile errors, so recursion ends).
func (g *gen) maxBitsStruct(st *ir.Struct) int64 {
	var walk func(items []ir.Item) int64
	walk = func(items []ir.Item) int64 {
		var total int64
		for _, item := range items {
			switch item := item.(type) {
			case *ir.FieldItem:
				total += g.maxBitsField(item.F)
			case *ir.Branch:
				then, els := walk(item.Then), walk(item.Else)
				if then > els {
					total += then
				} else {
					total += els
				}
			case *ir.ConstItem:
				total += item.Bits
			case *ir.ReservedItem:
				total += item.Bits
			case *ir.AlignItem:
				total += 7 // worst-case pad (SPEC §6.1 item 4)
			}
		}
		return total
	}
	return walk(st.Items)
}

// ---- function emission ----

// emitStructFunctions emits MaxBits/MaxBytes and the split Write/Read pair
// for a type or message, in the topo order the struct itself was emitted.
func (g *gen) emitStructFunctions(st *ir.Struct) {
	g.needsSerialize = true
	maxBits := g.maxBitsStruct(st)
	g.pf("inline constexpr int64_t %sMaxBits = %d; // longest wire path; align pads at worst case (SPEC §6.1)\n", st.Name, maxBits)
	g.pf("inline constexpr int64_t %sMaxBytes = %d;\n\n", st.Name, (maxBits+7)/8)

	g.pf("inline bool Write%s( serialize::WriteStream & stream, const %s & value )\n{\n", st.Name, st.Name)
	if len(st.Items) == 0 {
		g.pf("    (void) stream;\n    (void) value; // empty body — presence is the payload (SPEC §4.6)\n")
	} else {
		g.emitWriteItems(st.Items, "    ")
	}
	g.pf("    return true;\n}\n\n")

	g.pf("inline bool Read%s( serialize::ReadStream & stream, %s & value )\n{\n", st.Name, st.Name)
	if len(st.Items) == 0 {
		g.pf("    (void) stream;\n    (void) value;\n")
	} else {
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
		g.pf("%smemset( %s, 0, sizeof( %s ) );\n", ind, name, name)
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
				// aggregate init would restore specified defaults instead
				g.needsCstring = true
				g.pf("%smemset( &%s, 0, sizeof( %s ) );\n", ind, name, name)
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

func (g *gen) emitWriteField(f *ir.Field, ind string) {
	name := "value." + f.Name
	if f.Array != ir.ArrayNone {
		bound := g.renderInt(f.ArrayExpr, big.NewInt(f.ArrayBound))
		if f.Array == ir.ArrayCounted {
			g.pf("%swrite_int( stream, %s_count, %d, %s );\n", ind, name, f.ArrayMin, bound)
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
	case ir.TInt:
		if f.HasIntRange {
			lo, hi := g.rangeArgs(f)
			switch intRangePath(f.IntMin, f.IntMax) {
			case "int32":
				g.pf("%swrite_int( stream, %s, %s, %s );\n", ind, name, lo, hi)
			case "int64":
				g.pf("%swrite_int64( stream, %s, %s, %s );\n", ind, name, lo, hi)
			default:
				g.pf("%swrite_bits( stream, uint64_t( %s ) - %s, %d );\n", ind, name, lo, bitsRequired(f.IntMin, f.IntMax))
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
		}
		g.pf("%swrite_int( stream, %s_length, 0, %s );\n", ind, name, g.renderInt(f.Type.SizeExpr, big.NewInt(f.Type.Size)))
		g.pf("%swrite_bytes( stream, %s, %s_length );\n", ind, name, name)
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			g.pf("%swrite_int( stream, int32_t( %s ), 0, %d );\n", ind, name, ref.Max)
		case *ir.Flags:
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
	case ir.TInt:
		if f.HasIntRange {
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
				g.pf("%s    if ( offset_value > %sull )\n%s    {\n%s        return false;\n%s    }\n", ind, diff.String(), ind, ind, ind)
				g.pf("%s    %s = offset_value + %s;\n%s}\n", ind, name, lo, ind)
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

// emitMessageTagFunctions emits the tag wire pair and the message-level bound
// (SPEC §4.8, §6.1 item 6). The per-language dispatch surface (union, variant,
// factory) is deliberately not chosen here — representation is per-language
// and not part of the contract.
func (g *gen) emitMessageTagFunctions() {
	g.needsSerialize = true
	count := int64(len(g.unit.Messages))
	g.pf("// The message tag wire: MessageType in [0, %d], minimal bits; None = 0 is a\n", count)
	g.pf("// valid wire value meaning *no message* — the stream terminator (SPEC §4.8).\n")
	g.pf("inline bool WriteMessageType( serialize::WriteStream & stream, MessageType value )\n{\n")
	g.pf("    write_int( stream, int32_t( value ), 0, %d );\n    return true;\n}\n\n", count)
	g.pf("inline bool ReadMessageType( serialize::ReadStream & stream, MessageType & value )\n{\n")
	g.pf("    int32_t tag_value = 0;\n")
	g.pf("    read_int( stream, tag_value, 0, %d );\n", count)
	g.pf("    value = MessageType( tag_value );\n    return true;\n}\n\n")

	tagBits := bitsRequired(big.NewInt(0), big.NewInt(count))
	largest := int64(0)
	for _, m := range g.unit.Messages {
		if b := g.maxBitsStruct(g.unit.Structs[m]); b > largest {
			largest = b
		}
	}
	g.pf("// The message-level bound: the tag plus the largest message (SPEC §6.1)\n")
	g.pf("inline constexpr int64_t MessageMaxBits = %d;\n", tagBits+largest)
	g.pf("inline constexpr int64_t MessageMaxBytes = %d;\n\n", (tagBits+largest+7)/8)

	g.emitMessageDispatch()
}

// emitMessageDispatch is the C++ dispatch surface (SPEC §4.8: representation
// per language): a std::variant whose INDEX equals the wire tag — monostate is
// None = 0, then each message in tag order. std::variant never heap-allocates
// (storage is inline, the size of the largest message — the same footprint as
// a tagged union), and every generated message is trivially copyable, so the
// variant is too. Generated dispatch is a plain switch on the index; std::visit
// stays available to callers who want compile-time exhaustiveness and costs
// nothing here.
func (g *gen) emitMessageDispatch() {
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

	g.pf("inline bool WriteMessage( serialize::WriteStream & stream, const Message & message )\n{\n")
	g.pf("    write_int( stream, int32_t( message.index() ), 0, %d );\n", count)
	g.pf("    switch ( message.index() )\n    {\n")
	g.pf("        case 0:\n            return true; // None — the stream terminator (SPEC §4.8)\n")
	for i, m := range msgs {
		g.pf("        case %d:\n            return Write%s( stream, *std::get_if<%s>( &message ) );\n", i+1, m, m)
	}
	g.pf("    }\n    return false;\n}\n\n")

	g.pf("inline bool ReadMessage( serialize::ReadStream & stream, Message & message )\n{\n")
	g.pf("    int32_t tag_value = 0;\n")
	g.pf("    read_int( stream, tag_value, 0, %d );\n", count)
	g.pf("    switch ( tag_value )\n    {\n")
	g.pf("        case 0:\n            message.emplace<std::monostate>();\n            return true;\n")
	for i, m := range msgs {
		g.pf("        case %d:\n            return Read%s( stream, message.emplace<%s>() );\n", i+1, m, m)
	}
	g.pf("    }\n    return false;\n}\n\n")
}
