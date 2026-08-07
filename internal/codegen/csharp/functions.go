// Write/Read function emission for types and messages (SPEC §6.1 items 2-4,
// §6.2): straight-line split static functions against the serialize.cs ref
// API — every call's bool is checked and early-outs, the C++ twin's shape
// (§6.3 C# row), so counts and lengths are validated before the loop or slice
// they guard. The runtime's error latch (stream.Error) rides beneath the bool
// for callers; generated validation refusals return false without latching.
// The wire is byte-identical to the C++ target's, construct by construct.
//
// Write-side guards: serialize.cs's WriteStream refuses out-of-range values
// on the ranged calls (SerializeInt/SerializeInt64 latch ValueOutOfRange and
// return false — Serialize.cs:866-884, 887-917), exactly like serialize.go,
// and its raw bit writer masks silently (WriteBitsUnchecked, Serialize.cs:439)
// exactly like serialize.go's — so this emitter carries the Go target's
// leaner guard set: generated refusals exist only where the generated code
// bypasses the ranged calls (the flags wire-width guard and the full-range
// unsigned raw-offset path).
package csharp

import (
	"fmt"
	"math"
	"math/big"

	"github.com/mas-bandwidth/schema/internal/ir"
)

// emitStructFunctions emits MaxBits/MaxBytes, the Zero helper, and the split
// Write/Read pair for a type or message.
func (g *gen) emitStructFunctions(st *ir.Struct) {
	g.needsSerialize = true
	g.owner = st.Name // member references escape exactly as the class emitted them
	g.bulkBytes = ir.AlignedFixedByteArrays(st)
	maxBits := ir.MaxBitsStruct(st)
	g.sf("// %sMaxBits is the longest wire path; align pads at worst case (SPEC §6.1).\n", st.Name)
	g.sf("// %sMaxBytes is rounded up to the 8-byte write-buffer granularity.\n", st.Name)
	g.sf("public const long %sMaxBits = %d;\n", st.Name, maxBits)
	g.sf("public const long %sMaxBytes = %d;\n\n", st.Name, ir.MaxBytes(maxBits))

	g.emitZeroFunction(st)

	g.sf("public static bool Write%s(WriteStream stream, %s value)\n{\n", st.Name, st.Name)
	if len(st.Items) == 0 {
		g.sf("    // empty body — presence is the payload (SPEC §4.6)\n")
	} else {
		g.emitWriteItems(st.Items, "    ")
	}
	g.sf("    return true;\n}\n\n")

	g.sf("public static bool Read%s(ReadStream stream, %s value)\n{\n", st.Name, st.Name)
	if len(st.Items) > 0 {
		g.emitReadItems(st.Items, "    ")
	}
	g.sf("    return true;\n}\n\n")
}

// emitZeroFunction emits the §5 ZERO form for a class — all-zero storage,
// specified defaults NOT reapplied (those live in construction only; the wire
// contract stays a pure function of the encodings). It is the C# twin of the
// C++ target's memset: branch zeroing and ReadMessage's storage reset both go
// through here.
func (g *gen) emitZeroFunction(st *ir.Struct) {
	g.sf("// Zero%s resets value to the §5 ZERO form — all-zero storage; specified\n", st.Name)
	g.sf("// defaults live in construction only and are NOT reapplied here.\n")
	g.sf("public static void Zero%s(%s value)\n{\n", st.Name, st.Name)
	if len(st.Fields) == 0 {
		g.sf("    _ = value; // empty body — nothing to reset (SPEC §4.6)\n")
	}
	for _, f := range st.Fields {
		g.emitZeroField(f, "    ")
	}
	g.sf("}\n\n")
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
			g.call(ind, g.rv()+".SerializeAlign()", "")
		case *ir.Branch:
			neg := ""
			if item.Neg {
				neg = "!"
			}
			g.sf("%sif (%svalue.%s)\n%s{\n", ind, neg, g.m(ir.GoExportName(item.Cond)), ind)
			g.emitWriteItems(item.Then, ind+"    ")
			g.sf("%s}\n", ind)
			if item.Else != nil {
				g.sf("%selse\n%s{\n", ind, ind)
				g.emitWriteItems(item.Else, ind+"    ")
				g.sf("%s}\n", ind)
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
			g.emitConstItem(item, ind, false)
		case *ir.ReservedItem:
			g.emitReservedItem(item, ind, false)
		case *ir.AlignItem:
			g.call(ind, g.rv()+".SerializeAlign()", " // rejects nonzero padding (SPEC §4.3)")
		case *ir.Branch:
			neg := ""
			if item.Neg {
				neg = "!"
			}
			g.sf("%sif (%svalue.%s)\n%s{\n", ind, neg, g.m(ir.GoExportName(item.Cond)), ind)
			g.emitReadItems(item.Then, ind+"    ")
			// the untaken side reads as zero values (SPEC §5)
			g.emitZeroItems(item.Else, ind+"    ")
			g.sf("%s}\n%selse\n%s{\n", ind, ind, ind)
			if item.Else != nil {
				g.emitReadItems(item.Else, ind+"    ")
			}
			g.emitZeroItems(item.Then, ind+"    ")
			g.sf("%s}\n", ind)
		}
	}
}

// call emits the C++-style bool early-out around one serialize call; comment
// (leading " // ...") rides the if line.
func (g *gen) call(ind, expr, comment string) {
	g.sf("%sif (!%s)%s\n%s{\n%s    return false;\n%s}\n", ind, expr, comment, ind, ind, ind)
}

// emitConstItem writes const(value, bits) on the wire; a read rejects any
// other value (SPEC §4.3).
func (g *gen) emitConstItem(item *ir.ConstItem, ind string, writing bool) {
	typ, fn := "uint", "SerializeBits"
	if item.Bits > 32 {
		typ, fn = "ulong", "SerializeBits64"
	}
	if writing {
		g.sf("%s{\n%s    %s constValue = %s;\n", ind, ind, typ, item.Value.String())
		g.call(ind+"    ", fmt.Sprintf("%s.%s(ref constValue, %d)", g.rv(), fn, item.Bits),
			fmt.Sprintf(" // const(%s, %d) — SPEC §4.3", item.Value.String(), item.Bits))
		g.sf("%s}\n", ind)
		return
	}
	g.sf("%s{\n%s    %s constValue = 0;\n", ind, ind, typ)
	g.call(ind+"    ", fmt.Sprintf("%s.%s(ref constValue, %d)", g.rv(), fn, item.Bits), "")
	g.sf("%s    if (constValue != %s) // const(%s, %d): a read rejects any other value (SPEC §4.3)\n",
		ind, item.Value.String(), item.Value.String(), item.Bits)
	g.sf("%s    {\n%s        return false;\n%s    }\n%s}\n", ind, ind, ind, ind)
}

// emitReservedItem writes reserved(bits) as zeros; a read rejects nonzero.
func (g *gen) emitReservedItem(item *ir.ReservedItem, ind string, writing bool) {
	typ, fn := "uint", "SerializeBits"
	if item.Bits > 32 {
		typ, fn = "ulong", "SerializeBits64"
	}
	if writing {
		g.sf("%s{\n%s    %s reservedValue = 0;\n", ind, ind, typ)
		g.call(ind+"    ", fmt.Sprintf("%s.%s(ref reservedValue, %d)", g.rv(), fn, item.Bits),
			fmt.Sprintf(" // reserved(%d) — zeros on the wire", item.Bits))
		g.sf("%s}\n", ind)
		return
	}
	g.sf("%s{\n%s    %s reservedValue = 0;\n", ind, ind, typ)
	g.call(ind+"    ", fmt.Sprintf("%s.%s(ref reservedValue, %d)", g.rv(), fn, item.Bits), "")
	g.sf("%s    if (reservedValue != 0) // reserved(%d): a read rejects nonzero (SPEC §4.3)\n", ind, item.Bits)
	g.sf("%s    {\n%s        return false;\n%s    }\n%s}\n", ind, ind, ind, ind)
}

// emitZeroItems zero-initializes every field under an untaken branch side
// (SPEC §5: ZERO values, not specified defaults — the Zero* helpers are that
// zero form, so this is the memset twin exactly).
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
	base := g.fieldBase(f)
	name := "value." + base
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		g.needsSystem = true
		g.sf("%sArray.Clear(%s, 0, %s);\n%svalue.%s = 0;\n", ind, name, g.renderArg(f.Type.SizeExpr, big.NewInt(f.Type.Size), "int", false), ind, g.m(base+"Length"))
	case f.Array != ir.ArrayNone:
		if _, isStruct := f.Type.Ref.(*ir.Struct); isStruct && f.Type.Kind == ir.TNamed {
			// clearing a class array would null the pre-allocated elements —
			// zero through them instead (the SCHEMA bound, like the wire loops)
			g.sf("%sfor (int i = 0; i < %s; i++)\n%s{\n", ind, g.renderArg(f.ArrayExpr, big.NewInt(f.ArrayBound), "int", false), ind)
			g.sf("%s    Zero%s(%s[i]);\n%s}\n", ind, f.Type.Name, name, ind)
		} else {
			g.needsSystem = true
			g.sf("%sArray.Clear(%s, 0, %s);\n", ind, name, g.renderArg(f.ArrayExpr, big.NewInt(f.ArrayBound), "int", false))
		}
		if f.Array == ir.ArrayCounted {
			g.sf("%svalue.%s = 0;\n", ind, g.m(base+"Count"))
		}
	default:
		switch f.Type.Kind {
		case ir.TBool:
			g.sf("%s%s = false;\n", ind, name)
		case ir.TFloat32:
			g.sf("%s%s = 0.0f;\n", ind, name)
		case ir.TFloat64:
			g.sf("%s%s = 0.0;\n", ind, name)
		case ir.TNamed:
			switch f.Type.Ref.(type) {
			case *ir.Enum:
				g.sf("%s%s = %s.None;\n", ind, name, f.Type.Name)
			case *ir.Flags:
				g.sf("%s%s = 0;\n", ind, name)
			case *ir.Struct:
				// through the nested Zero — §5 wants ZERO values recursively,
				// and re-newing would restore specified defaults instead
				g.sf("%sZero%s(%s);\n", ind, f.Type.Name, name)
			}
		default:
			g.sf("%s%s = 0;\n", ind, name)
		}
	}
}

// rangeArgs renders the min/max arguments symbolically where possible, cast
// to the runtime call family's argument type.
func (g *gen) rangeArgs(f *ir.Field, typ string) (string, string) {
	return g.renderArg(f.IntMinExpr, f.IntMin, typ, false), g.renderArg(f.IntMaxExpr, f.IntMax, typ, false)
}

// ---- generation-time bound folding on the write path (C++ PR #8's
// mechanism, carried to C#) ----
//
// A ranged integer's min/max/bit count are schema constants, so the generator
// folds them: the write emits the offset from min in a bit count computed at
// generation time through the raw SerializeBits/SerializeBits64 calls — no
// runtime BitsRequired, no min/max parameter traffic, and the 32/64 dword
// split resolves here, not at run time. The wire bytes are identical to the
// runtime SerializeInt/SerializeInt64 forms (offset-from-min in
// BitsRequired(min, max) bits — the wire-identity property C++ #25/#8 proved,
// re-proven here by the wire golden gate). The runtime ranged write's
// out-of-range refusal moves into the generated code with the fold: the guard
// returns false WITHOUT latching — the family's generated-guard form (the
// flags wire-width guard and the full-range unsigned raw path already refuse
// this way); vacuous halves (bounds the storage type cannot escape) are
// elided. Reads stay on the runtime ranged calls: #25/#8 measured nothing to
// gain on the read path, and the read side's latched ValueOutOfRange
// semantics (pinned by test/cs) stay untouched.

// storageBounds is the representable range of an integer storage type.
func storageBounds(t ir.FieldType) (*big.Int, *big.Int) {
	w := uint(t.Width)
	if t.Signed {
		half := new(big.Int).Lsh(big.NewInt(1), w-1)
		return new(big.Int).Neg(half), new(big.Int).Sub(half, big.NewInt(1))
	}
	return big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), w), big.NewInt(1))
}

// emitWriteFoldedRange writes the integer expression expr, in [min, max], as
// offset-from-min in a generation-time bit count. lo/hi are the rendered
// bound expressions (typed so they compare against expr legally); wide picks
// the 64-bit call family; guardLo/guardHi say whether each half of the range
// refusal is non-vacuous for expr's storage type. Byte-identical to the
// runtime SerializeInt/SerializeInt64 forms.
func (g *gen) emitWriteFoldedRange(expr, lo, hi string, min, max *big.Int, wide, guardLo, guardHi bool, comment, ind string) {
	switch {
	case guardLo && guardHi:
		g.sf("%sif (%s < %s || %s > %s)%s\n%s{\n%s    return false;\n%s}\n", ind, expr, lo, expr, hi, comment, ind, ind, ind)
	case guardLo:
		g.sf("%sif (%s < %s)%s\n%s{\n%s    return false;\n%s}\n", ind, expr, lo, comment, ind, ind, ind)
	case guardHi:
		g.sf("%sif (%s > %s)%s\n%s{\n%s    return false;\n%s}\n", ind, expr, hi, comment, ind, ind, ind)
	}
	bits := ir.BitsRequired(min, max)
	typ, fn := "uint", "SerializeBits"
	if wide {
		typ, fn = "ulong", "SerializeBits64"
	}
	// the offset subtraction lives in the unsigned domain, exactly as the
	// runtime forms compute it; a negative constant bound needs unchecked()
	// because C# refuses checked constant narrowing at compile time
	loCast := fmt.Sprintf("(%s)(%s)", typ, lo)
	if min.Sign() < 0 {
		loCast = fmt.Sprintf("unchecked((%s)(%s))", typ, lo)
	}
	if min.Sign() == 0 {
		g.sf("%s{\n%s    %s offsetValue = (%s)(%s);\n", ind, ind, typ, typ, expr)
	} else {
		g.sf("%s{\n%s    %s offsetValue = (%s)(%s) - %s;\n", ind, ind, typ, typ, expr, loCast)
	}
	g.call(ind+"    ", fmt.Sprintf("%s.%s(ref offsetValue, %d)", g.rv(), fn, bits), "")
	g.sf("%s}\n", ind)
}

// emitWriteFoldedInt is emitWriteFoldedRange for a ranged TInt field: it
// derives the guard vacuousness from the field's storage type and renders the
// bounds in that storage's comparison domain.
func (g *gen) emitWriteFoldedInt(f *ir.Field, name, ind string) {
	sMin, sMax := storageBounds(f.Type)
	guardLo := f.IntMin.Cmp(sMin) > 0
	guardHi := f.IntMax.Cmp(sMax) < 0
	wide := intRangePath(f.IntMin, f.IntMax) != "int32"
	typ := "int"
	if wide {
		typ = "long"
		if !f.Type.Signed && f.Type.Width == 64 {
			// ulong storage compares against ulong-typed bounds (a long-cast
			// symbolic bound would be an illegal ulong/long comparison)
			typ = "ulong"
		}
	}
	lo, hi := g.rangeArgs(f, typ)
	g.emitWriteFoldedRange(name, lo, hi, f.IntMin, f.IntMax, wide, guardLo, guardHi,
		" // out-of-contract writes are refused, not wrapped", ind)
}

// maxUint64 is 2^64 - 1, the top of unsigned-64 storage — the bound against
// which a range guard becomes vacuous.
var maxUint64 = new(big.Int).SetUint64(math.MaxUint64)

// intRangePath picks the runtime call family for a ranged integer — the same
// switch as the other targets, so all four emit identical wire.
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
	base := g.fieldBase(f)
	name := "value." + base
	if f.Array != ir.ArrayNone {
		bound := g.renderArg(f.ArrayExpr, big.NewInt(f.ArrayBound), "int", false)
		if g.bulkBytes[f] {
			// statically byte-aligned [N]uint8 (ir.AlignedFixedByteArrays):
			// the bulk path aligns zero bits here and memcpys — byte-identical
			// to the per-byte loop. The SCHEMA bound slices the span, so
			// reassigned-short storage faults loudly, as the loop did.
			g.needsSystem = true
			g.call(ind, fmt.Sprintf("%s.SerializeBytes(%s.AsSpan(0, %s))", g.rv(), name, bound),
				" // byte-aligned [N]uint8 — bulk copy, wire-identical to the per-byte loop")
			return
		}
		if f.Array == ir.ArrayCounted {
			count := "value." + g.m(base+"Count")
			g.emitWriteFoldedRange(count, fmt.Sprintf("%d", f.ArrayMin), bound,
				big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound), false, true, true,
				" // the count guards the loop (§6.3); out-of-contract writes are refused", ind)
			g.sf("%sfor (int i = 0; i < %s; i++)\n%s{\n", ind, count, ind)
		} else {
			// the SCHEMA bound, never the storage's Length: reassigned-short
			// storage must fault loudly, not silently write fewer elements
			g.sf("%sfor (int i = 0; i < %s; i++)\n%s{\n", ind, bound, ind)
		}
		g.emitWriteScalar(f, name+"[i]", ind+"    ")
		g.sf("%s}\n", ind)
		return
	}
	g.emitWriteScalar(f, name, ind)
}

func (g *gen) emitWriteScalar(f *ir.Field, name, ind string) {
	switch f.Type.Kind {
	case ir.TInt:
		if f.HasIntRange {
			switch intRangePath(f.IntMin, f.IntMax) {
			case "int32", "int64":
				g.emitWriteFoldedInt(f, name, ind)
			default:
				// full-range unsigned: raw offset bits (ulong storage only —
				// no narrower storage can hold a range past long). This path
				// bypasses the runtime's ranged calls, so it supplies their
				// write-side range refusal (a misuse value must not wrap into
				// valid-looking wire); vacuous halves are elided. Nothing
				// latches — bool is the whole verdict here.
				lo, _ := g.rangeArgs(f, "ulong")
				loVacuous := f.IntMin.Sign() == 0
				hiVacuous := f.IntMax.Cmp(maxUint64) == 0
				switch {
				case !loVacuous && !hiVacuous:
					g.sf("%sif (%s < %s || %s > %s) // out-of-contract writes are refused, not wrapped\n", ind, name, lo, name, f.IntMax.String())
					g.sf("%s{\n%s    return false;\n%s}\n", ind, ind, ind)
				case !loVacuous:
					g.sf("%sif (%s < %s) // out-of-contract writes are refused, not wrapped\n", ind, name, lo)
					g.sf("%s{\n%s    return false;\n%s}\n", ind, ind, ind)
				case !hiVacuous:
					g.sf("%sif (%s > %s) // out-of-contract writes are refused, not wrapped\n", ind, name, f.IntMax.String())
					g.sf("%s{\n%s    return false;\n%s}\n", ind, ind, ind)
				}
				if loVacuous {
					g.sf("%s{\n%s    ulong offsetValue = %s;\n", ind, ind, name)
				} else {
					g.sf("%s{\n%s    ulong offsetValue = %s - %s;\n", ind, ind, name, lo)
				}
				g.call(ind+"    ", fmt.Sprintf("%s.SerializeBits64(ref offsetValue, %d)", g.rv(), ir.BitsRequired(f.IntMin, f.IntMax)), "")
				g.sf("%s}\n", ind)
			}
			return
		}
		g.emitWriteBareInt(f, name, ind)
	case ir.TBits:
		if f.Type.Width <= 32 {
			g.call(ind, fmt.Sprintf("%s.SerializeBits(ref %s, %d)", g.rv(), name, f.Type.Width), "")
		} else {
			g.call(ind, fmt.Sprintf("%s.SerializeBits64(ref %s, %d)", g.rv(), name, f.Type.Width), "")
		}
	case ir.TBool:
		g.call(ind, fmt.Sprintf("%s.SerializeBool(ref %s)", g.rv(), name), "")
	case ir.TFloat32:
		if f.HasFloatRange {
			// a temp so the wire quantization cannot write back into the input
			g.sf("%s{\n%s    float compressedValue = %s;\n", ind, ind, name)
			g.call(ind+"    ", fmt.Sprintf("%s.SerializeCompressedFloat(ref compressedValue, %s, %s, %s)",
				g.rv(), formatFloat32(f.FMin), formatFloat32(f.FMax), formatFloat32(f.Resolution)), "")
			g.sf("%s}\n", ind)
			return
		}
		g.call(ind, fmt.Sprintf("%s.SerializeFloat(ref %s)", g.rv(), name), "")
	case ir.TFloat64:
		g.call(ind, fmt.Sprintf("%s.SerializeDouble(ref %s)", g.rv(), name), "")
	case ir.TString, ir.TBytes:
		// length in [0, N], align, then the used bytes — the classic
		// serialize_string framing over a buffer of N + 1 (SPEC §4.7).
		// The length guard runs BEFORE the slice, so a stale length never
		// reaches AsSpan (§6.3). Interior nulls are writer misuse; the read
		// side rejects them (§4.7).
		g.needsSystem = true
		length := "value." + g.m(g.fieldBase(f)+"Length")
		g.emitWriteFoldedRange(length, "0", g.renderArg(f.Type.SizeExpr, big.NewInt(f.Type.Size), "int", false),
			big.NewInt(0), big.NewInt(f.Type.Size), false, true, true,
			" // the length guards the slice (§6.3); out-of-contract writes are refused", ind)
		g.call(ind, fmt.Sprintf("%s.SerializeBytes(%s.AsSpan(0, %s))", g.rv(), name, length), "")
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			// headroom storage can exceed the wire range [0, Max]: the
			// generated guard refuses it — bool alone, nothing latches
			g.emitWriteFoldedEnum(ref, name, ind)
		case *ir.Flags:
			g.emitWriteFlagsValue(name, ref.WireBits, ind)
		case *ir.Struct:
			if g.inBatch {
				g.call(ind, fmt.Sprintf("Write%sBatch(ref batch, %s)", f.Type.Name, name), "")
			} else {
				g.call(ind, fmt.Sprintf("Write%s(stream, %s)", f.Type.Name, name), "")
			}
		}
	}
}

// emitWriteFoldedEnum writes an enum in [0, Max] with a generation-time bit
// count — byte-identical to the runtime SerializeInt form it replaces. The
// unsigned temp doubles as the headroom guard's comparison domain.
func (g *gen) emitWriteFoldedEnum(ref *ir.Enum, name, ind string) {
	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max))
	typ, fn := "uint", "SerializeBits"
	if ref.StorageBits > 32 {
		typ, fn = "ulong", "SerializeBits64"
	}
	g.sf("%s{\n%s    %s enumValue = (%s)%s;\n", ind, ind, typ, typ, name)
	// the guard is vacuous only when the wire range fills the backing type
	if big.NewInt(ref.Max).Cmp(new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(ref.StorageBits)), big.NewInt(1))) < 0 {
		g.sf("%s    if (enumValue > %d) // headroom above the wire range cannot ride\n", ind, ref.Max)
		g.sf("%s    {\n%s        return false;\n%s    }\n", ind, ind, ind)
	}
	g.call(ind+"    ", fmt.Sprintf("%s.%s(ref enumValue, %d)", g.rv(), fn, bits), "")
	g.sf("%s}\n", ind)
}

// emitWriteBareInt writes a bare integer at its storage width. Signed values
// cast through the same-width unsigned first — the sign extension into a
// wider temp would corrupt neighboring wire data, exactly as in C++.
func (g *gen) emitWriteBareInt(f *ir.Field, name, ind string) {
	if f.Type.Width == 64 {
		if f.Type.Signed {
			g.sf("%s{\n%s    ulong rawValue = (ulong)%s;\n", ind, ind, name)
			g.call(ind+"    ", g.rv()+".SerializeBits64(ref rawValue, 64)", "")
			g.sf("%s}\n", ind)
			return
		}
		g.call(ind, fmt.Sprintf("%s.SerializeBits64(ref %s, 64)", g.rv(), name), "")
		return
	}
	if !f.Type.Signed && f.Type.Width == 32 {
		g.call(ind, fmt.Sprintf("%s.SerializeBits(ref %s, 32)", g.rv(), name), "")
		return
	}
	g.sf("%s{\n%s    uint rawValue = %s;\n", ind, ind, fmt32Cast(f, name))
	g.call(ind+"    ", fmt.Sprintf("%s.SerializeBits(ref rawValue, %d)", g.rv(), f.Type.Width), "")
	g.sf("%s}\n", ind)
}

// fmt32Cast renders the value-to-uint conversion for a sub-32 (or signed
// 32-bit) bare integer: signed narrows go through the same-width unsigned.
func fmt32Cast(f *ir.Field, name string) string {
	if f.Type.Signed && f.Type.Width < 32 {
		return "(" + csUint(f.Type.Width) + ")" + name
	}
	if f.Type.Signed {
		return "(uint)" + name
	}
	return name // byte/ushort widen implicitly
}

func (g *gen) emitReadField(f *ir.Field, ind string) {
	base := g.fieldBase(f)
	name := "value." + base
	if f.Array != ir.ArrayNone {
		bound := g.renderArg(f.ArrayExpr, big.NewInt(f.ArrayBound), "int", false)
		if g.bulkBytes[f] {
			// the write twin's bulk path exactly: zero align bits consumed
			// here, then a bulk copy of the N bytes (see emitWriteField)
			g.needsSystem = true
			g.call(ind, fmt.Sprintf("%s.SerializeBytes(%s.AsSpan(0, %s))", g.rv(), name, bound),
				" // byte-aligned [N]uint8 — bulk copy, wire-identical to the per-byte loop")
			return
		}
		if f.Array == ir.ArrayCounted {
			count := "value." + g.m(base+"Count")
			g.call(ind, fmt.Sprintf("%s.SerializeInt(ref %s, %d, %s)", g.rv(), count, f.ArrayMin, bound),
				" // the count guards the loop (§6.3)")
			g.sf("%sfor (int i = 0; i < %s; i++)\n%s{\n", ind, count, ind)
		} else {
			// the SCHEMA bound, never the storage's Length (see the write twin)
			g.sf("%sfor (int i = 0; i < %s; i++)\n%s{\n", ind, bound, ind)
		}
		g.emitReadScalar(f, name+"[i]", ind+"    ")
		g.sf("%s}\n", ind)
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
				lo, hi := g.rangeArgs(f, "int")
				if f.Type.Signed && f.Type.Width == 32 {
					g.call(ind, fmt.Sprintf("%s.SerializeInt(ref %s, %s, %s)", g.rv(), name, lo, hi), "")
					return
				}
				g.sf("%s{\n%s    int rangeValue = 0;\n", ind, ind)
				g.call(ind+"    ", fmt.Sprintf("%s.SerializeInt(ref rangeValue, %s, %s)", g.rv(), lo, hi), "")
				g.sf("%s    %s = (%s)rangeValue;\n%s}\n", ind, name, g.csFieldType(f.Type), ind)
			case "int64":
				lo, hi := g.rangeArgs(f, "long")
				if f.Type.Signed && f.Type.Width == 64 {
					g.call(ind, fmt.Sprintf("%s.SerializeInt64(ref %s, %s, %s)", g.rv(), name, lo, hi), "")
					return
				}
				g.sf("%s{\n%s    long rangeValue = 0;\n", ind, ind)
				g.call(ind+"    ", fmt.Sprintf("%s.SerializeInt64(ref rangeValue, %s, %s)", g.rv(), lo, hi), "")
				g.sf("%s    %s = (%s)rangeValue;\n%s}\n", ind, name, g.csFieldType(f.Type), ind)
			default:
				lo, _ := g.rangeArgs(f, "ulong")
				diff := new(big.Int).Sub(f.IntMax, f.IntMin)
				g.sf("%s{\n%s    ulong offsetValue = 0;\n", ind, ind)
				g.call(ind+"    ", fmt.Sprintf("%s.SerializeBits64(ref offsetValue, %d)", g.rv(), ir.BitsRequired(f.IntMin, f.IntMax)), "")
				if diff.Cmp(maxUint64) != 0 {
					// a full-width diff cannot overflow its own read — elided
					g.sf("%s    if (offsetValue > %s) // a read rejects out-of-range (SPEC §5) — not latched\n", ind, diff.String())
					g.sf("%s    {\n%s        return false;\n%s    }\n", ind, ind, ind)
				}
				if f.IntMin.Sign() == 0 {
					g.sf("%s    %s = offsetValue;\n%s}\n", ind, name, ind)
				} else {
					g.sf("%s    %s = offsetValue + %s;\n%s}\n", ind, name, lo, ind)
				}
			}
			return
		}
		g.emitReadBareInt(f, name, ind)
	case ir.TBits:
		if f.Type.Width <= 32 {
			g.call(ind, fmt.Sprintf("%s.SerializeBits(ref %s, %d)", g.rv(), name, f.Type.Width), "")
		} else {
			g.call(ind, fmt.Sprintf("%s.SerializeBits64(ref %s, %d)", g.rv(), name, f.Type.Width), "")
		}
	case ir.TBool:
		g.call(ind, fmt.Sprintf("%s.SerializeBool(ref %s)", g.rv(), name), "")
	case ir.TFloat32:
		if f.HasFloatRange {
			g.call(ind, fmt.Sprintf("%s.SerializeCompressedFloat(ref %s, %s, %s, %s)",
				g.rv(), name, formatFloat32(f.FMin), formatFloat32(f.FMax), formatFloat32(f.Resolution)), "")
			return
		}
		g.call(ind, fmt.Sprintf("%s.SerializeFloat(ref %s)", g.rv(), name), "")
	case ir.TFloat64:
		g.call(ind, fmt.Sprintf("%s.SerializeDouble(ref %s)", g.rv(), name), "")
	case ir.TString, ir.TBytes:
		// the bool is checked BEFORE the slice: a hostile length never
		// reaches AsSpan (a successful ranged read guarantees [0, N])
		g.needsSystem = true
		length := "value." + g.m(g.fieldBase(f)+"Length")
		g.call(ind, fmt.Sprintf("%s.SerializeInt(ref %s, 0, %s)",
			g.rv(), length, g.renderArg(f.Type.SizeExpr, big.NewInt(f.Type.Size), "int", false)),
			" // the length guards the slice (§6.3)")
		g.call(ind, fmt.Sprintf("%s.SerializeBytes(%s.AsSpan(0, %s))", g.rv(), name, length), "")
		if f.Type.Kind == ir.TString {
			// the interior-null rule is generated-code validation (SPEC §4.7);
			// the SerializeBytes bool above already surfaced a truncated
			// stream as the stream's own latched error, so this verdict only
			// ever judges bytes that arrived — false, nothing latched
			g.sf("%sfor (int i = 0; i < %s; i++)\n%s{\n", ind, length, ind)
			g.sf("%s    if (%s[i] == 0) // an interior null is content the read refuses (SPEC §4.7)\n", ind, name)
			g.sf("%s    {\n%s        return false;\n%s    }\n%s}\n", ind, ind, ind, ind)
		}
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			g.sf("%s{\n%s    int enumValue = 0;\n", ind, ind)
			g.call(ind+"    ", fmt.Sprintf("%s.SerializeInt(ref enumValue, 0, %d)", g.rv(), ref.Max), "")
			g.sf("%s    %s = (%s)enumValue;\n%s}\n", ind, name, f.Type.Name, ind)
		case *ir.Flags:
			g.emitReadFlags(name, ref.WireBits, ind)
		case *ir.Struct:
			if g.inBatch {
				g.call(ind, fmt.Sprintf("Read%sBatch(ref batch, %s)", f.Type.Name, name), "")
			} else {
				g.call(ind, fmt.Sprintf("Read%s(stream, %s)", f.Type.Name, name), "")
			}
		}
	}
}

func (g *gen) emitReadBareInt(f *ir.Field, name, ind string) {
	if f.Type.Width == 64 {
		if f.Type.Signed {
			g.sf("%s{\n%s    ulong rawValue = 0;\n", ind, ind)
			g.call(ind+"    ", g.rv()+".SerializeBits64(ref rawValue, 64)", "")
			g.sf("%s    %s = (long)rawValue;\n%s}\n", ind, name, ind)
			return
		}
		g.call(ind, fmt.Sprintf("%s.SerializeBits64(ref %s, 64)", g.rv(), name), "")
		return
	}
	if !f.Type.Signed && f.Type.Width == 32 {
		g.call(ind, fmt.Sprintf("%s.SerializeBits(ref %s, 32)", g.rv(), name), "")
		return
	}
	g.sf("%s{\n%s    uint rawValue = 0;\n", ind, ind)
	g.call(ind+"    ", fmt.Sprintf("%s.SerializeBits(ref rawValue, %d)", g.rv(), f.Type.Width), "")
	if f.Type.Signed && f.Type.Width < 32 {
		// back through the same-width unsigned so the sign bit lands right
		g.sf("%s    %s = (%s)(%s)rawValue;\n%s}\n", ind, name, csInt(f.Type.Width), csUint(f.Type.Width), ind)
		return
	}
	g.sf("%s    %s = (%s)rawValue;\n%s}\n", ind, name, g.csFieldType(f.Type), ind)
}

// emitReadFlags reads a flags value through an unsigned temp; past 32 wire
// bits the ulong storage takes the read directly.
func (g *gen) emitReadFlags(name string, wireBits int, ind string) {
	if wireBits <= 32 {
		g.sf("%s{\n%s    uint flagsValue = 0;\n", ind, ind)
		g.call(ind+"    ", fmt.Sprintf("%s.SerializeBits(ref flagsValue, %d)", g.rv(), wireBits), "")
		g.sf("%s    %s = flagsValue;\n%s}\n", ind, name, ind)
		return
	}
	g.call(ind, fmt.Sprintf("%s.SerializeBits64(ref %s, %d)", g.rv(), name, wireBits), "")
}

// emitWriteFlagsValue is the write half used by emitWriteScalar. Storage is
// wider than the wire wherever WireBits < 64, so a mask bit above the wire
// width is refused rather than silently truncated — the raw bit calls mask,
// so this refusal is generated (nothing latches; bool is the verdict).
func (g *gen) emitWriteFlagsValue(name string, wireBits int, ind string) {
	if wireBits < 64 {
		g.sf("%sif (%s >= 1ul << %d) // a mask bit above the wire width cannot ride\n", ind, name, wireBits)
		g.sf("%s{\n%s    return false;\n%s}\n", ind, ind, ind)
	}
	if wireBits <= 32 {
		g.sf("%s{\n%s    uint flagsValue = (uint)%s;\n", ind, ind, name)
		g.call(ind+"    ", fmt.Sprintf("%s.SerializeBits(ref flagsValue, %d)", g.rv(), wireBits), "")
		g.sf("%s}\n", ind)
		return
	}
	g.sf("%s{\n%s    ulong flagsValue = %s;\n", ind, ind, name)
	g.call(ind+"    ", fmt.Sprintf("%s.SerializeBits64(ref flagsValue, %d)", g.rv(), wireBits), "")
	g.sf("%s}\n", ind)
}

// emitMessageStorage emits the pre-allocated home reads land in.
func (g *gen) emitMessageStorage() {
	g.tf("// MessageStorage is the pre-allocated home a read message lands in — the C#\n")
	g.tf("// stand-in for the C++ tagged union (SPEC §6.1: storage never heap-allocates\n")
	g.tf("// per message; fixed buffers on sender and receiver). Reuse it across reads:\n")
	g.tf("// the Message a read returns points into it and stays valid until the next\n")
	g.tf("// read against the same storage — the union's own discipline, exactly.\n")
	g.tf("public sealed class MessageStorage\n{\n")
	for _, m := range g.unit.Messages {
		g.tf("    public %s %s = new %s();\n", m, m, m)
	}
	g.tf("}\n\n")
}

// emitMessageTagFunctions emits the tag wire pair, the message-level bound,
// and the C# dispatch surface: a type-pattern switch over the abstract base —
// the language's own idiom (SPEC §6.1 item 6). null is None, the stream
// terminator.
func (g *gen) emitMessageTagFunctions() {
	g.needsSerialize = true
	count := int64(len(g.unit.Messages))
	g.sf("// The message tag wire: MessageType in [0, %d], minimal bits; None = 0 is a\n", count)
	g.sf("// valid wire value meaning *no message* — the stream terminator (SPEC §4.8).\n")
	g.sf("// The write folds the tag's bit count at generation time (byte-identical to\n")
	g.sf("// the ranged form); a tag outside the set is refused — bool alone.\n")
	g.sf("public static bool WriteMessageType(WriteStream stream, MessageType value)\n{\n")
	g.sf("    uint tagValue = (uint)value;\n")
	g.sf("    if (tagValue > %d)\n    {\n        return false;\n    }\n", count)
	g.sf("    return stream.SerializeBits(ref tagValue, %d);\n}\n\n", ir.BitsRequired(big.NewInt(0), big.NewInt(count)))
	g.sf("public static bool ReadMessageType(ReadStream stream, ref MessageType value)\n{\n")
	g.sf("    int tagValue = 0;\n")
	g.sf("    if (!stream.SerializeInt(ref tagValue, 0, %d))\n    {\n        return false;\n    }\n", count)
	g.sf("    value = (MessageType)tagValue;\n    return true;\n}\n\n")

	tagBits := ir.BitsRequired(big.NewInt(0), big.NewInt(count))
	largest := int64(0)
	for _, m := range g.unit.Messages {
		if b := ir.MaxBitsStruct(g.unit.Structs[m]); b > largest {
			largest = b
		}
	}
	g.sf("// The message-level bound: the tag plus the largest message (SPEC §6.1);\n")
	g.sf("// MessageMaxBytes is rounded up to the 8-byte write-buffer granularity.\n")
	g.sf("public const long MessageMaxBits = %d;\n", tagBits+largest)
	g.sf("public const long MessageMaxBytes = %d;\n\n", ir.MaxBytes(tagBits+largest))

	msgs := g.unit.Messages

	// dispatch validates BEFORE the tag rides the wire: a Message subclass
	// from outside the generated set writes nothing (a tag with no payload
	// would desynchronize the stream), and the tag framing is the tag
	// pair's — one source
	g.sf("public static bool WriteMessage(WriteStream stream, Message message)\n{\n")
	g.sf("    switch (message)\n    {\n")
	g.sf("        case null:\n            return WriteMessageType(stream, MessageType.None); // the stream terminator (SPEC §4.8)\n")
	for _, m := range msgs {
		g.sf("        case %s value:\n", m)
		g.sf("            if (!WriteMessageType(stream, MessageType.%s))\n            {\n                return false;\n            }\n", m)
		g.sf("            return Write%s(stream, value);\n", m)
	}
	g.sf("        default:\n            return false; // not a generated message type; nothing was written\n")
	g.sf("    }\n}\n\n")

	g.sf("public static bool ReadMessage(ReadStream stream, MessageStorage storage, out Message message)\n{\n")
	g.sf("    message = null;\n")
	g.sf("    MessageType tagValue = MessageType.None;\n")
	g.sf("    if (!ReadMessageType(stream, ref tagValue))\n    {\n        return false;\n    }\n")
	g.sf("    switch (tagValue)\n    {\n")
	g.sf("        case MessageType.None:\n            return true; // the stream terminator (SPEC §4.8) — message stays null\n")
	for _, m := range msgs {
		g.sf("        case MessageType.%s:\n", m)
		g.sf("            Zero%s(storage.%s); // reused storage starts from the zero form, as the union does\n", m, m)
		g.sf("            if (!Read%s(stream, storage.%s))\n            {\n                return false;\n            }\n", m, m)
		g.sf("            message = storage.%s;\n            return true;\n", m)
	}
	g.sf("        default:\n")
	g.sf("            // ReadMessageType already rejected tags past the set; the switch\n")
	g.sf("            // still needs the arm because enums are open storage\n")
	g.sf("            return false;\n")
	g.sf("    }\n}\n\n")
}
