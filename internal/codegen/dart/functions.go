// Wire-function emission: write/read/measure/zero per type, the issue #155
// prescription — the family bitpacker inlined at every field with literal
// constant widths and masks, nested types inlined into the caller's body
// (one monomorphic function per top-level operation, zero dispatch), bounds
// checks fused per maximal static run on the read side, and the measure
// side's static runs folded to literals at generation time.
//
// The wire math transliterates the flat JS tier (internal/codegen/js/flat.go
// — the probe-proven kernels) into Dart's value domain: one 64-bit int
// carries every lane through width 64, the emitted Int128/UInt128 pair
// carries the 128-bit storage widths, and the 32-bit group structure (least
// significant first — serialize's own) is preserved exactly, so the wire is
// byte-identical to the other seven targets by construction and proven by
// the golden legs.
package dart

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// emitSignature writes a function signature, wrapped in the formatter's
// tall style when the one-line form passes 80 columns.
func (g *gen) emitSignature(ret, fname string, params []string) {
	one := fmt.Sprintf("%s %s(%s) {", ret, fname, strings.Join(params, ", "))
	if len(one) <= 80 {
		g.bpf("%s\n", one)
		return
	}
	g.bpf("%s %s(\n", ret, fname)
	for _, p := range params {
		g.bpf("  %s,\n", p)
	}
	g.bpf(") {\n")
}

func (g *gen) pf(format string, args ...any) {
	fmt.Fprintf(&g.fn, format, args...)
}

// maskHex renders the (1<<bits)-1 mask for bits in [1,64].
func maskHex(bits int64) string {
	if bits == 64 {
		return "-1" // all 64 bits — the bit-transparent spelling
	}
	return fmt.Sprintf("0x%x", (uint64(1)<<uint(bits))-1)
}

// mergeW merges v (already masked to bits) into the write scratch —
// serialize.dart BitWriter.writeBits inlined, constant width. bits in [1,32].
func (g *gen) mergeW(bits int64, ind string) {
	g.needV = true
	g.pf("%sscratch |= v << scratchBits;\n", ind)
	g.pf("%sscratchBits += %d;\n", ind, bits)
	g.pf("%sif (scratchBits >= 64) {\n", ind)
	g.pf("%s  view.setUint64(wordIndex * 8, scratch, Endian.little);\n", ind)
	g.pf("%s  wordIndex++;\n", ind)
	g.pf("%s  scratchBits -= 64;\n", ind)
	g.pf("%s  scratch = v >>> (%d - scratchBits);\n", ind, bits)
	g.pf("%s}\n", ind)
}

// readR reads bits (in [1,32]) from the 64-bit window at bitsRead into v,
// masked — serialize.dart BitReader.readBits inlined, with the tail window
// pricing the loads inside the buffer (no slack contract).
func (g *gen) readR(bits int64, ind string) {
	g.needV = true
	g.usesRead = true
	g.pf("%sif (bitsRead >>> 3 < tailBase) {\n", ind)
	g.pf("%s  window = view.getUint64(bitsRead >>> 3, Endian.little);\n", ind)
	g.pf("%s  shift = bitsRead & 7;\n", ind)
	g.pf("%s} else {\n", ind)
	g.pf("%s  window = tailWord;\n", ind)
	g.pf("%s  shift = bitsRead - tailBase * 8;\n", ind)
	g.pf("%s}\n", ind)
	g.pf("%sv = (window >>> shift) & %s;\n", ind, maskHex(bits))
	g.pf("%sbitsRead += %d;\n", ind, bits)
}

// ---- static wire-width analysis for run fusing ----

// staticBitsItem reports an item's exact wire bits when they are the same on
// every path (branches count only when both sides agree; strings, counted
// arrays, unions with any nonzero arm, and align are dynamic). Exactness
// matters: the fused read bound `bitsRead + runBits > numBits` must never
// overshoot a valid stream.
func (g *gen) staticBitsItem(item ir.Item) (int64, bool) {
	switch item := item.(type) {
	case *ir.FieldItem:
		return g.staticBitsField(item.F)
	case *ir.ConstItem:
		return item.Bits, true
	case *ir.ReservedItem:
		return item.Bits, true
	case *ir.AlignItem:
		return 0, false
	case *ir.Branch:
		then, ok := g.staticBitsItems(item.Then)
		if !ok {
			return 0, false
		}
		els := int64(0)
		if item.Else != nil {
			var ok2 bool
			els, ok2 = g.staticBitsItems(item.Else)
			if !ok2 {
				return 0, false
			}
		}
		if then == els {
			return then, true
		}
		return 0, false
	}
	return 0, false
}

func (g *gen) staticBitsItems(items []ir.Item) (int64, bool) {
	var total int64
	for _, item := range items {
		bits, ok := g.staticBitsItem(item)
		if !ok {
			return 0, false
		}
		total += bits
	}
	return total, true
}

func (g *gen) staticBitsField(f *ir.Field) (int64, bool) {
	elem, ok := g.staticBitsScalar(f)
	if !ok {
		return 0, false
	}
	switch f.Array {
	case ir.ArrayFixed:
		return f.ArrayBound * elem, true
	case ir.ArrayCounted:
		return 0, false
	default:
		return elem, true
	}
}

func (g *gen) staticBitsScalar(f *ir.Field) (int64, bool) {
	switch f.Type.Kind {
	case ir.TString, ir.TBytes:
		return 0, false
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			return g.staticBitsItems(ref.Items)
		case *ir.Union:
			// a union's wire is tag + SELECTED arm — static only in the
			// degenerate no-variant case (zero bits); counting MaxBits here
			// would let a fused read bound refuse valid wire carrying a
			// smaller arm
			if ref.Max == 0 {
				return 0, true
			}
			return 0, false
		}
	}
	// every remaining scalar kind is fixed-width and ir.MaxBitsField is exact
	// for it (ranged/bare ints, fixed, bits, bool, floats, enum, flags)
	return ir.MaxBitsField(&ir.Field{Type: f.Type, HasIntRange: f.HasIntRange,
		IntMin: f.IntMin, IntMax: f.IntMax, HasFloatRange: f.HasFloatRange,
		FMin: f.FMin, FMax: f.FMax, Resolution: f.Resolution}), true
}

// ---- per-struct emission ----

func (g *gen) emitStructFunctions(st *ir.Struct) {
	maxBits := ir.MaxBitsStruct(st)
	low := lowerFirst(st.Name)
	g.bpf("// %sMaxBits is the longest wire path; align pads at worst case (SPEC §6.1).\n", low)
	g.bpf("// %sMaxBytes is rounded up to the 8-byte write-buffer granularity.\n", low)
	g.bpf("const int %sMaxBits = %d;\n", low, maxBits)
	g.bpf("const int %sMaxBytes = %d;\n\n", low, ir.MaxBytes(maxBits))

	g.emitZeroFunction(st.Name, st.Fields)
	g.emitWriteFunction(st.Name, low, st.Items)
	g.emitReadFunction(st.Name, low, st.Items)
	g.emitMeasureFunction(st.Name, st.Items)
}

// emitUnionFunctions emits the union's bounds and the same function surface
// as a type: the tag rides in minimal bits, then the selected arm only
// (SPEC §4.8).
func (g *gen) emitUnionFunctions(d *ir.Union) {
	maxBits := ir.MaxBitsUnion(d)
	low := lowerFirst(d.Name)
	g.bpf("// %sMaxBits is the tag plus the largest arm; None costs the tag only (SPEC §4.8).\n", low)
	g.bpf("// %sMaxBytes is rounded up to the 8-byte write-buffer granularity.\n", low)
	g.bpf("const int %sMaxBits = %d;\n", low, maxBits)
	g.bpf("const int %sMaxBytes = %d;\n\n", low, ir.MaxBytes(maxBits))

	g.bpf("// zero%s resets value to the §5 zero form — the empty union. The tag alone\n", d.Name)
	g.bpf("// resets: unselected arms are unspecified by rule (SPEC §4.8), and every arm\n")
	g.bpf("// is unselected at None; an arm re-zeroes at its next selection.\n")
	g.bpf("void zero%s(%s value) {\n", d.Name, d.Name)
	g.bpf("  value.type = %sType.none;\n}\n\n", d.Name)

	item := unionItem(d)
	g.emitWriteFunction(d.Name, low, []ir.Item{item})
	g.emitReadFunction(d.Name, low, []ir.Item{item})
	g.emitMeasureFunction(d.Name, []ir.Item{item})
}

// unionItem wraps a union as a self-typed field item so the standalone
// union functions reuse the field emission with path "value" — the same
// bodies a union field inlines.
func unionItem(d *ir.Union) ir.Item {
	return &ir.FieldItem{F: &ir.Field{Name: "", Type: ir.FieldType{
		Kind: ir.TNamed, Name: d.Name, Ref: d,
	}}}
}

func (g *gen) resetFn() {
	g.fn.Reset()
	g.loopDepth = 0
	g.needV, g.needLo, g.needHi, g.usesRead = false, false, false, false
}

func (g *gen) emitWriteFunction(name, low string, items []ir.Item) {
	g.usesTypeData = true
	g.resetFn()
	g.emitWriteItems(items, "value", "  ")
	body := g.fn.String()

	g.bpf("// write%s packs value into view — the trusted writer (contracts asserted,\n", name)
	g.bpf("// compiled out without --enable-asserts). The buffer behind view must hold\n")
	g.bpf("// %sMaxBytes. Returns the bytes written.\n", low)
	g.emitSignature("int", "write"+name, []string{name + " value", "ByteData view"})
	g.bpf("  assert(view.lengthInBytes %% 8 == 0);\n")
	g.bpf("  assert(view.lengthInBytes >= %sMaxBytes);\n", low)
	g.bpf("  var scratch = 0;\n")
	g.bpf("  var scratchBits = 0;\n")
	g.bpf("  var wordIndex = 0;\n")
	if g.needV {
		g.bpf("  var v = 0;\n")
	}
	if body == "" {
		g.bpf("  // empty body — presence is the payload (SPEC §4.6)\n")
	}
	g.body.WriteString(body)
	g.bpf("  if (scratchBits != 0) {\n")
	g.bpf("    view.setUint64(wordIndex * 8, scratch, Endian.little);\n")
	g.bpf("  }\n")
	g.bpf("  return wordIndex * 8 + ((scratchBits + 7) >>> 3);\n")
	g.bpf("}\n\n")
}

func (g *gen) emitReadFunction(name, low string, items []ir.Item) {
	g.usesTypeData = true
	g.resetFn()
	g.emitReadItems(items, "value", "  ", false)
	body := g.fn.String()

	g.bpf("// read%s decodes value from the first numBits of view — the family read\n", name)
	g.bpf("// verdict: false rejects the wire (bounds, ranges, wire constants, padding);\n")
	g.bpf("// hostile bytes never throw. No slack past the payload is required.\n")
	g.emitSignature("bool", "read"+name, []string{name + " value", "ByteData view", "int numBits"})
	if g.usesRead {
		g.bpf("  if (numBits > view.lengthInBytes * 8) {\n")
		g.bpf("    return false; // the payload cannot exceed the buffer behind view\n")
		g.bpf("  }\n")
		g.bpf("  // the final 64-bit window, assembled once so every load stays inside\n")
		g.bpf("  // the buffer (serialize.dart's own no-slack reader stance)\n")
		g.bpf("  var tailBase = view.lengthInBytes - 8;\n")
		g.bpf("  var tailWord = 0;\n")
		g.bpf("  if (tailBase >= 0) {\n")
		g.bpf("    tailWord = view.getUint64(tailBase, Endian.little);\n")
		g.bpf("  } else {\n")
		g.bpf("    tailBase = 0;\n")
		g.bpf("    for (var i = view.lengthInBytes - 1; i >= 0; i--) {\n")
		g.bpf("      tailWord = (tailWord << 8) | view.getUint8(i);\n")
		g.bpf("    }\n")
		g.bpf("  }\n")
		g.bpf("  var bitsRead = 0;\n")
		g.bpf("  var window = 0;\n")
		g.bpf("  var shift = 0;\n")
	}
	if g.needV {
		g.bpf("  var v = 0;\n")
	}
	if g.needLo {
		g.bpf("  var lo = 0;\n")
	}
	if g.needHi {
		g.bpf("  var hi = 0;\n")
	}
	g.body.WriteString(body)
	g.bpf("  return true;\n")
	g.bpf("}\n\n")
}

// emitZeroFunction emits the §5 zero form for a class — all-zero storage,
// specified defaults NOT reapplied (those live in construction only; the
// wire contract stays a pure function of the encodings).
func (g *gen) emitZeroFunction(name string, fields []*ir.Field) {
	g.bpf("// The §5 zero form: all-zero storage; specified defaults live only in\n")
	g.bpf("// construction.\n")
	g.bpf("void zero%s(%s value) {\n", name, name)
	if len(fields) == 0 {
		g.bpf("  // empty body — nothing to reset (SPEC §4.6)\n")
	}
	g.resetFn()
	for _, f := range fields {
		g.emitZeroField(f, "value", "  ", true)
	}
	g.body.WriteString(g.fn.String())
	g.bpf("}\n\n")
}

// ---- write emission ----

func (g *gen) emitWriteItems(items []ir.Item, path, ind string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			g.emitWriteField(item.F, path, ind)
		case *ir.ConstItem:
			g.emitWriteRaw(item.Value, item.Bits, ind)
		case *ir.ReservedItem:
			g.emitWriteRaw(big.NewInt(0), item.Bits, ind)
		case *ir.AlignItem:
			g.emitWriteAlign(ind)
		case *ir.Branch:
			neg := ""
			if item.Neg {
				neg = "!"
			}
			g.pf("%sif (%s%s.%s) {\n", ind, neg, path, dartName(item.Cond))
			g.emitWriteItems(item.Then, path, ind+"  ")
			if item.Else != nil {
				g.pf("%s} else {\n", ind)
				g.emitWriteItems(item.Else, path, ind+"  ")
			}
			g.pf("%s}\n", ind)
		}
	}
}

// emitWriteRaw merges a compile-time constant (const/reserved items) of up
// to 64 bits, split low dword first past 32 (the serialize group rule).
func (g *gen) emitWriteRaw(value *big.Int, bits int64, ind string) {
	masked := new(big.Int).And(value, new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1)))
	if bits <= 32 {
		g.pf("%sv = %s;\n", ind, dartIntLit(masked))
		g.mergeW(bits, ind)
		return
	}
	lo := new(big.Int).And(masked, new(big.Int).SetUint64(0xffffffff))
	hi := new(big.Int).Rsh(masked, 32)
	g.pf("%sv = %s;\n", ind, dartIntLit(lo))
	g.mergeW(32, ind)
	g.pf("%sv = %s;\n", ind, dartIntLit(hi))
	g.mergeW(bits-32, ind)
}

// emitWriteAlign pads the write position to the next byte boundary with
// zeros (nothing merges, so only the counter moves — with the flush kept,
// exactly as the merge would keep it).
func (g *gen) emitWriteAlign(ind string) {
	g.pf("%s{\n", ind)
	g.pf("%s  final pad = scratchBits & 7;\n", ind)
	g.pf("%s  if (pad != 0) {\n", ind)
	g.pf("%s    scratchBits += 8 - pad;\n", ind)
	g.pf("%s    if (scratchBits >= 64) {\n", ind)
	g.pf("%s      view.setUint64(wordIndex * 8, scratch, Endian.little);\n", ind)
	g.pf("%s      wordIndex++;\n", ind)
	g.pf("%s      scratchBits -= 64;\n", ind)
	g.pf("%s      scratch = 0;\n", ind)
	g.pf("%s    }\n", ind)
	g.pf("%s  }\n", ind)
	g.pf("%s}\n", ind)
}

func (g *gen) emitWriteField(f *ir.Field, path, ind string) {
	name := path + "." + dartName(f.Name)
	if f.Name == "" {
		name = path // the standalone union functions' self item
	}
	switch f.Array {
	case ir.ArrayFixed:
		iv := fmt.Sprintf("i%d", g.loopDepth)
		g.loopDepth++
		g.pf("%sfor (var %s = 0; %s < %d; %s++) {\n", ind, iv, iv, f.ArrayBound, iv)
		g.emitWriteElem(f, name, iv, ind+"  ")
		g.pf("%s}\n", ind)
		g.loopDepth--
	case ir.ArrayCounted:
		count := name + "Count"
		g.assertRange(count, big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound), true, ind)
		g.emitWriteOffset(count, big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound), ind)
		iv := fmt.Sprintf("i%d", g.loopDepth)
		g.loopDepth++
		g.pf("%sfor (var %s = 0; %s < %s; %s++) {\n", ind, iv, iv, count, iv)
		g.emitWriteElem(f, name, iv, ind+"  ")
		g.pf("%s}\n", ind)
		g.loopDepth--
	default:
		g.emitWriteScalar(f, name, ind)
	}
}

// emitWriteElem writes one array element; class elements hoist a final ref.
func (g *gen) emitWriteElem(f *ir.Field, name, iv, ind string) {
	if f.Type.Kind == ir.TNamed {
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			ev := fmt.Sprintf("e%d", g.loopDepth-1)
			g.pf("%sfinal %s = %s[%s];\n", ind, ev, name, iv)
			g.emitWriteItems(ref.Items, ev, ind)
			return
		case *ir.Union:
			ev := fmt.Sprintf("e%d", g.loopDepth-1)
			g.pf("%sfinal %s = %s[%s];\n", ind, ev, name, iv)
			g.emitWriteUnion(ref, ev, ind)
			return
		}
	}
	g.emitWriteScalar(f, fmt.Sprintf("%s[%s]", name, iv), ind)
}

// assertRange emits the write-contract asserts for an integer path in
// [min, max], both inside the 64-bit domain. signedDomain selects the
// comparison domain; vacuous halves are elided against that domain's edges.
func (g *gen) assertRange(expr string, min, max *big.Int, signedDomain bool, ind string) {
	if signedDomain {
		if min.Cmp(minInt64) > 0 {
			g.pf("%sassert(%s >= %s);\n", ind, expr, dartIntLit(min))
		}
		if max.Cmp(maxInt64) < 0 {
			g.pf("%sassert(%s <= %s);\n", ind, expr, dartIntLit(max))
		}
		return
	}
	// unsigned 64-bit domain: bit-transparent compares through the helper
	if min.Sign() > 0 {
		g.needULess = true
		g.pf("%sassert(!_unsignedLessThan(%s, %s));\n", ind, expr, dartIntLit(min))
	}
	if max.Cmp(maxUint64Big) < 0 {
		g.needULess = true
		g.pf("%sassert(!_unsignedLessThan(%s, %s));\n", ind, dartIntLit(max), expr)
	}
}

// emitWriteOffset merges (expr - min) in the folded bit count for a range
// inside the 64-bit domain: single group through 32 bits, two groups (low
// dword first) past it — the serialize group structure exactly. Offsets wrap
// two's complement, which is the unsigned-domain subtraction every family
// port performs.
func (g *gen) emitWriteOffset(expr string, min, max *big.Int, ind string) {
	bits := ir.BitsRequired(min, max)
	if bits == 0 {
		return // degenerate range: ZERO bits — the assert is the whole write
	}
	off := expr
	if min.Sign() > 0 {
		off = fmt.Sprintf("%s - %s", expr, dartIntLit(min))
	} else if min.Sign() < 0 {
		// subtracting a negative min is adding its magnitude — the wrapping
		// arithmetic is identical and the spelling avoids `- -N`
		if neg := new(big.Int).Neg(min); neg.Cmp(maxInt64) <= 0 {
			off = fmt.Sprintf("%s + %s", expr, dartIntLit(neg))
		} else {
			off = fmt.Sprintf("%s - %s", expr, dartIntLit(min)) // int64 min, a hex literal
		}
	}
	if bits <= 32 {
		g.pf("%sv = (%s) & %s;\n", ind, off, maskHex(bits))
		g.mergeW(bits, ind)
		return
	}
	if min.Sign() != 0 {
		g.pf("%s{\n", ind)
		g.pf("%s  final off = %s;\n", ind, off)
		g.emitWriteWide64("off", bits, ind+"  ")
		g.pf("%s}\n", ind)
		return
	}
	g.emitWriteWide64(expr, bits, ind)
}

// emitWriteWide64 merges the low `bits` bits (33..64) of an int expression
// as two groups, low dword first.
func (g *gen) emitWriteWide64(expr string, bits int64, ind string) {
	g.pf("%sv = %s & 0xffffffff;\n", ind, expr)
	g.mergeW(32, ind)
	g.pf("%sv = (%s >>> 32) & %s;\n", ind, expr, maskHex(bits-32))
	g.mergeW(bits-32, ind)
}

// emitWriteWide128 merges the low `bits` bits (1..128) of a (hi, lo) pair
// expression, 32-bit groups least significant first.
func (g *gen) emitWriteWide128(loExpr, hiExpr string, bits int64, ind string) {
	switch {
	case bits <= 32:
		g.pf("%sv = %s & %s;\n", ind, loExpr, maskHex(bits))
		g.mergeW(bits, ind)
	case bits <= 64:
		g.emitWriteWide64(loExpr, bits, ind)
	case bits <= 96:
		g.emitWriteWide64(loExpr, 64, ind)
		g.pf("%sv = %s & %s;\n", ind, hiExpr, maskHex(bits-64))
		g.mergeW(bits-64, ind)
	default:
		g.emitWriteWide64(loExpr, 64, ind)
		g.emitWriteWide64(hiExpr, bits-64, ind)
	}
}

func (g *gen) emitWriteScalar(f *ir.Field, name, ind string) {
	switch f.Type.Kind {
	case ir.TFixed:
		g.emitWriteFixed(f, name, ind)
	case ir.TInt:
		g.emitWriteInt(f, name, ind)
	case ir.TBits:
		w := int64(f.Type.Width)
		if w <= 32 {
			g.pf("%sv = %s & %s;\n", ind, name, maskHex(w))
			g.mergeW(w, ind)
			return
		}
		g.emitWriteWide64(name, w, ind)
	case ir.TBool:
		g.pf("%sv = %s ? 1 : 0;\n", ind, name)
		g.mergeW(1, ind)
	case ir.TFloat32:
		if f.HasFloatRange {
			g.emitWriteCompressedFloat(f, name, ind)
			return
		}
		g.needScratch, g.needF32Conv = true, true
		g.pf("%sv = _float32BitsFromDouble(%s);\n", ind, name)
		g.mergeW(32, ind)
	case ir.TFloat64:
		g.needScratch, g.needF64Conv = true, true
		g.pf("%s{\n", ind)
		g.pf("%s  final b = _float64BitsFromDouble(%s);\n", ind, name)
		g.emitWriteWide64("b", 64, ind+"  ")
		g.pf("%s}\n", ind)
	case ir.TString, ir.TBytes:
		g.emitWriteBytesField(f, name, ind)
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			g.emitWriteEnum(ref, name, ind)
		case *ir.Flags:
			g.emitWriteFlags(ref, name, ind)
		case *ir.Struct:
			g.emitWriteItems(ref.Items, name, ind)
		case *ir.Union:
			g.emitWriteUnion(ref, name, ind)
		}
	}
}

// f32lit renders the float32 rounding of v as a Dart double literal (exact —
// f32 is a subset of f64).
func f32lit(v float64) string {
	return formatFloat32(float64(float32(v)))
}

// emitWriteCompressedFloat is the family's two-rounding float32 quantization
// with the declaration folded: round to float32, normalize, clamp (which
// also grounds NaN), quantize — every step through _fround (SPEC §4.3).
func (g *gen) emitWriteCompressedFloat(f *ir.Field, name, ind string) {
	g.needScratch, g.needFround = true, true
	maxInt, bits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
	minF := float32(f.FMin)
	deltaF := float32(f.FMax) - minF
	mivF := float32(maxInt)
	g.pf("%s{\n", ind)
	g.pf("%s  final x = _fround(%s);\n", ind, name)
	g.pf("%s  assert(x.isFinite);\n", ind)
	g.pf("%s  var n = _fround(_fround(x - %s) / %s);\n", ind, f32lit(float64(minF)), f32lit(float64(deltaF)))
	g.pf("%s  if (!(n >= 0.0)) {\n", ind)
	g.pf("%s    n = 0.0;\n", ind)
	g.pf("%s  } else if (!(n <= 1.0)) {\n", ind)
	g.pf("%s    n = 1.0;\n", ind)
	g.pf("%s  }\n", ind)
	g.pf("%s  v = _fround(_fround(n * %s) + 0.5).floor();\n", ind, f32lit(float64(mivF)))
	g.pf("%s}\n", ind)
	g.needV = true
	g.mergeW(bits, ind)
}

// fixedRaw derives a fixed field's raw wire parameters: the whole-unit
// bounds scaled by F, and the offset bit count.
func fixedRaw(f *ir.Field) (rawMin, rawMax *big.Int, bits int64) {
	fb := uint(f.Type.FracBits)
	rawMin = shiftedRaw(f.IntMin, fb)
	rawMax = shiftedRaw(f.IntMax, fb)
	bits = int64(new(big.Int).Sub(rawMax, rawMin).BitLen())
	return rawMin, rawMax, bits
}

func (g *gen) emitWriteFixed(f *ir.Field, name, ind string) {
	rawMin, rawMax, bits := fixedRaw(f)
	if f.Type.Width == 128 {
		g.emitWrite128Ranged(f.Type.Signed, name, rawMin, rawMax, bits, ind)
		return
	}
	if f.IntMin.Cmp(f.IntMax) == 0 {
		// degenerate: zero bits — the assert is the whole write (SPEC §4.6)
		g.pf("%sassert(%s == %s);\n", ind, name, dartIntLit(rawMin))
		return
	}
	g.assertRange(name, rawMin, rawMax, rawMax.Cmp(maxInt64) <= 0, ind)
	g.emitWriteOffset(name, rawMin, rawMax, ind)
}

func (g *gen) emitWriteInt(f *ir.Field, name, ind string) {
	w := int64(f.Type.Width)
	if w == 128 {
		if f.HasIntRange {
			g.emitWrite128Ranged(f.Type.Signed, name, f.IntMin, f.IntMax, ir.BitsRequired(f.IntMin, f.IntMax), ind)
			return
		}
		// uint128 raw: full 128 bits, 32-bit groups least significant first
		g.emitWriteWide128(name+".lo", name+".hi", 128, ind)
		return
	}
	if f.HasIntRange {
		if f.IntMin.Cmp(f.IntMax) == 0 {
			// degenerate range: ZERO bits — the assert is the whole write
			g.pf("%sassert(%s == %s);\n", ind, name, dartIntLit(f.IntMin))
			return
		}
		g.assertRange(name, f.IntMin, f.IntMax, f.IntMax.Cmp(maxInt64) <= 0, ind)
		g.emitWriteOffset(name, f.IntMin, f.IntMax, ind)
		return
	}
	// bare integer at storage width; signed values mask to the same-width
	// unsigned pattern (the sign smear a wider merge would spread corrupts
	// neighboring wire data, exactly as in C++)
	if w == 64 {
		g.emitWriteWide64(name, 64, ind)
		return
	}
	g.pf("%sv = %s & %s;\n", ind, name, maskHex(w))
	g.mergeW(w, ind)
}

// emitWrite128Ranged writes a ranged 128-bit-storage value (int128/uint128,
// fixed of width 128): assert the bounds, then the offset from min in the
// folded bit count, 32-bit groups least significant first. The pair bounds
// hoist into block-scoped consts so no emitted line outgrows the formatter.
func (g *gen) emitWrite128Ranged(signed bool, name string, min, max *big.Int, bits int64, ind string) {
	u := ""
	loDomain, hiDomain := big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1))
	if signed {
		u = ".toUnsigned()"
		loDomain = new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 127))
		hiDomain = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 127), big.NewInt(1))
	}
	if min.Cmp(max) == 0 {
		// degenerate range: ZERO bits — the assert is the whole write
		g.pf("%s{\n", ind)
		g.pf("%s  const locked = %s;\n", ind, g.pairCtor(signed, min))
		g.pf("%s  assert(%s == locked);\n", ind, name)
		g.pf("%s}\n", ind)
		return
	}
	guardLo := min.Cmp(loDomain) > 0
	guardHi := max.Cmp(hiDomain) < 0
	needMin := guardLo || min.Sign() != 0
	g.pf("%s{\n", ind)
	if needMin {
		g.pf("%s  const min = %s;\n", ind, g.pairCtor(signed, min))
	}
	if guardHi {
		g.pf("%s  const max = %s;\n", ind, g.pairCtor(signed, max))
	}
	if guardLo {
		g.pf("%s  assert(%s >= min);\n", ind, name)
	}
	if guardHi {
		g.pf("%s  assert(%s <= max);\n", ind, name)
	}
	switch {
	case min.Sign() == 0 && !signed:
		g.emitWriteWide128(name+".lo", name+".hi", bits, ind+"  ")
	case min.Sign() == 0 && signed:
		g.pf("%s  final off = %s.toUnsigned();\n", ind, name)
		g.emitWriteWide128("off.lo", "off.hi", bits, ind+"  ")
	default:
		g.pf("%s  final off = (%s - min)%s;\n", ind, name, u)
		g.emitWriteWide128("off.lo", "off.hi", bits, ind+"  ")
	}
	g.pf("%s}\n", ind)
}

func (g *gen) emitWriteEnum(ref *ir.Enum, name, ind string) {
	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max))
	// headroom above the wire range cannot ride
	g.assertRange(name, big.NewInt(0), big.NewInt(ref.Max), true, ind)
	if bits == 0 {
		return // degenerate [0, 0]: zero bits; the assert still rides
	}
	g.pf("%sv = %s & %s;\n", ind, name, maskHex(bits))
	g.mergeW(bits, ind)
}

func (g *gen) emitWriteFlags(ref *ir.Flags, name, ind string) {
	wb := int64(ref.WireBits)
	if wb < 64 {
		// a mask bit above the wire width cannot ride
		g.pf("%sassert(%s >>> %d == 0);\n", ind, name, wb)
	}
	if wb <= 32 {
		g.pf("%sv = %s & %s;\n", ind, name, maskHex(wb))
		g.mergeW(wb, ind)
		return
	}
	g.emitWriteWide64(name, wb, ind)
}

// emitWriteBytesField writes string(N)/bytes(N): folded ranged length,
// align (zero pad), then the used bytes through the packer — the classic
// serialize_string framing composed from primitives, byte-identical to
// every other target's.
func (g *gen) emitWriteBytesField(f *ir.Field, name, ind string) {
	length := name + "Length"
	g.assertRange(length, big.NewInt(0), big.NewInt(f.Type.Size), true, ind)
	g.emitWriteOffset(length, big.NewInt(0), big.NewInt(f.Type.Size), ind)
	g.emitWriteAlign(ind)
	iv := fmt.Sprintf("i%d", g.loopDepth)
	g.loopDepth++
	g.pf("%sfor (var %s = 0; %s < %s; %s++) {\n", ind, iv, iv, length, iv)
	g.pf("%s  v = %s[%s];\n", ind, name, iv)
	g.mergeW(8, ind+"  ")
	g.pf("%s}\n", ind)
	g.loopDepth--
}

// emitWriteUnion inlines a union (SPEC §4.8): the tag contract asserted
// BEFORE it rides, the tag in minimal bits, then a switch inlines each
// arm's items — the struct-inlining move, per arm.
func (g *gen) emitWriteUnion(u *ir.Union, expr, ind string) {
	// the tag validates BEFORE it rides (SPEC §4.8)
	g.assertRange(expr+".type", big.NewInt(0), big.NewInt(u.Max), true, ind)
	if u.Max == 0 {
		return // an empty union's degenerate tag range [0, 0] costs zero bits
	}
	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(u.Max))
	g.pf("%sv = %s.type & %s;\n", ind, expr, maskHex(bits))
	g.mergeW(bits, ind)
	g.pf("%sswitch (%s.type) {\n", ind, expr)
	for i, vr := range u.Variants {
		g.pf("%s  case %d:\n", ind, i+1)
		g.emitWriteItems(vr.Ref.Items, expr+"."+dartName(vr.Name), ind+"    ")
		if len(vr.Ref.Items) == 0 {
			g.pf("%s    break; // empty arm — presence is the payload (SPEC §4.6)\n", ind)
		}
	}
	g.pf("%s}\n", ind)
}

// ---- read emission ----

// emitReadItems walks a scope with run fusing: one bounds check covers each
// maximal run of statically-sized items (bounded=true suppresses checks —
// an enclosing scope already proved the bits).
func (g *gen) emitReadItems(items []ir.Item, path, ind string, bounded bool) {
	i := 0
	for i < len(items) {
		if _, ok := g.staticBitsItem(items[i]); ok {
			total := int64(0)
			j := i
			for j < len(items) {
				bits, ok2 := g.staticBitsItem(items[j])
				if !ok2 {
					break
				}
				total += bits
				j++
			}
			if !bounded && total > 0 {
				g.pf("%sif (bitsRead + %d > numBits) {\n%s  return false;\n%s}\n", ind, total, ind, ind)
			}
			for ; i < j; i++ {
				g.emitReadStaticItem(items[i], path, ind)
			}
			continue
		}
		g.emitReadDynamicItem(items[i], path, ind)
		i++
	}
}

// emitReadStaticItem reads one statically-sized item; bounds already proven.
func (g *gen) emitReadStaticItem(item ir.Item, path, ind string) {
	switch item := item.(type) {
	case *ir.FieldItem:
		g.emitReadStaticField(item.F, path, ind)
	case *ir.ConstItem:
		g.emitReadRaw(item.Value, item.Bits, true, ind)
	case *ir.ReservedItem:
		g.emitReadRaw(big.NewInt(0), item.Bits, false, ind)
	case *ir.Branch:
		// both sides statically equal (the run test admitted it)
		neg := ""
		if item.Neg {
			neg = "!"
		}
		g.pf("%sif (%s%s.%s) {\n", ind, neg, path, dartName(item.Cond))
		g.emitReadItems(item.Then, path, ind+"  ", true)
		g.emitZeroItems(item.Else, path, ind+"  ")
		g.pf("%s} else {\n", ind)
		if item.Else != nil {
			g.emitReadItems(item.Else, path, ind+"  ", true)
		}
		g.emitZeroItems(item.Then, path, ind+"  ")
		g.pf("%s}\n", ind)
	}
}

func (g *gen) emitReadDynamicItem(item ir.Item, path, ind string) {
	switch item := item.(type) {
	case *ir.FieldItem:
		g.emitReadDynamicField(item.F, path, ind)
	case *ir.AlignItem:
		g.emitReadAlign(ind)
	case *ir.Branch:
		neg := ""
		if item.Neg {
			neg = "!"
		}
		g.pf("%sif (%s%s.%s) {\n", ind, neg, path, dartName(item.Cond))
		g.emitReadItems(item.Then, path, ind+"  ", false)
		g.emitZeroItems(item.Else, path, ind+"  ")
		g.pf("%s} else {\n", ind)
		if item.Else != nil {
			g.emitReadItems(item.Else, path, ind+"  ", false)
		}
		g.emitZeroItems(item.Then, path, ind+"  ")
		g.pf("%s}\n", ind)
	}
}

// emitReadAlign verifies zero padding to the byte boundary and advances.
func (g *gen) emitReadAlign(ind string) {
	g.usesRead = true
	g.pf("%s{\n", ind)
	g.pf("%s  final pad = bitsRead & 7;\n", ind)
	g.pf("%s  if (pad != 0) {\n", ind)
	g.pf("%s    if (bitsRead + (8 - pad) > numBits) {\n", ind)
	g.pf("%s      return false;\n", ind)
	g.pf("%s    }\n", ind)
	g.pf("%s    if (view.getUint8(bitsRead >>> 3) >>> pad != 0) {\n", ind)
	g.pf("%s      return false; // nonzero padding is refused (SPEC §4.3)\n", ind)
	g.pf("%s    }\n", ind)
	g.pf("%s    bitsRead += 8 - pad;\n", ind)
	g.pf("%s  }\n", ind)
	g.pf("%s}\n", ind)
}

// emitReadRaw reads a const/reserved item and rejects any other value.
func (g *gen) emitReadRaw(value *big.Int, bits int64, isConst bool, ind string) {
	what := "reserved bits must read zero"
	if isConst {
		what = "a read rejects any other value"
	}
	masked := new(big.Int).And(value, new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1)))
	if bits <= 32 {
		g.readR(bits, ind)
		g.pf("%sif (v != %s) {\n%s  return false; // %s (SPEC §4.3)\n%s}\n", ind, dartIntLit(masked), ind, what, ind)
		return
	}
	g.emitReadWide64(bits, ind)
	g.pf("%sif (lo != %s) {\n%s  return false; // %s (SPEC §4.3)\n%s}\n", ind, dartIntLit(masked), ind, what, ind)
}

func (g *gen) emitReadStaticField(f *ir.Field, path, ind string) {
	name := path + "." + dartName(f.Name)
	if f.Name == "" {
		name = path
	}
	if f.Array == ir.ArrayFixed {
		iv := fmt.Sprintf("i%d", g.loopDepth)
		g.loopDepth++
		g.pf("%sfor (var %s = 0; %s < %d; %s++) {\n", ind, iv, iv, f.ArrayBound, iv)
		g.emitReadElem(f, name, iv, ind+"  ", true)
		g.pf("%s}\n", ind)
		g.loopDepth--
		return
	}
	g.emitReadScalar(f, name, ind, true)
}

func (g *gen) emitReadElem(f *ir.Field, name, iv, ind string, bounded bool) {
	if f.Type.Kind == ir.TNamed {
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			ev := fmt.Sprintf("e%d", g.loopDepth-1)
			g.pf("%sfinal %s = %s[%s];\n", ind, ev, name, iv)
			g.emitReadItems(ref.Items, ev, ind, bounded)
			return
		case *ir.Union:
			ev := fmt.Sprintf("e%d", g.loopDepth-1)
			g.pf("%sfinal %s = %s[%s];\n", ind, ev, name, iv)
			g.emitReadUnion(ref, ev, ind, bounded)
			return
		}
	}
	if is128(f.Type) {
		g.emitRead128Scalar(f, fmt.Sprintf("%s[%s] = ", name, iv), ind)
		return
	}
	g.emitReadScalar(f, fmt.Sprintf("%s[%s]", name, iv), ind, bounded)
}

func (g *gen) emitReadDynamicField(f *ir.Field, path, ind string) {
	name := path + "." + dartName(f.Name)
	if f.Name == "" {
		name = path
	}
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		g.emitReadBytesField(f, name, ind)
	case f.Array == ir.ArrayCounted:
		count := name + "Count"
		countBits := ir.BitsRequired(big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound))
		if countBits > 0 {
			g.pf("%sif (bitsRead + %d > numBits) {\n%s  return false;\n%s}\n", ind, countBits, ind, ind)
			g.readR(countBits, ind)
			diff := f.ArrayBound - f.ArrayMin
			if diff != (int64(1)<<countBits)-1 {
				g.pf("%sif (v > %d) {\n%s  return false; // the count guards the loop — reject, never clamp\n%s}\n", ind, diff, ind, ind)
			}
			if f.ArrayMin == 0 {
				g.pf("%s%s = v;\n", ind, count)
			} else {
				g.pf("%s%s = v + %d;\n", ind, count, f.ArrayMin)
			}
		} else {
			g.pf("%s%s = %d;\n", ind, count, f.ArrayMin)
		}
		iv := fmt.Sprintf("i%d", g.loopDepth)
		g.loopDepth++
		if elemBits, ok := g.staticBitsScalar(f); ok {
			if elemBits > 0 {
				g.pf("%sif (bitsRead + %s * %d > numBits) {\n%s  return false;\n%s}\n", ind, count, elemBits, ind, ind)
			}
			g.pf("%sfor (var %s = 0; %s < %s; %s++) {\n", ind, iv, iv, count, iv)
			g.emitReadElem(f, name, iv, ind+"  ", true)
			g.pf("%s}\n", ind)
		} else {
			g.pf("%sfor (var %s = 0; %s < %s; %s++) {\n", ind, iv, iv, count, iv)
			g.emitReadElem(f, name, iv, ind+"  ", false)
			g.pf("%s}\n", ind)
		}
		g.loopDepth--
	case f.Array == ir.ArrayFixed:
		// a fixed array of dynamically-sized elements (branches, unions,
		// strings)
		iv := fmt.Sprintf("i%d", g.loopDepth)
		g.loopDepth++
		g.pf("%sfor (var %s = 0; %s < %d; %s++) {\n", ind, iv, iv, f.ArrayBound, iv)
		g.emitReadElem(f, name, iv, ind+"  ", false)
		g.pf("%s}\n", ind)
		g.loopDepth--
	default:
		// a scalar whose size is dynamic: a nested struct with dynamic
		// items, or a union
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			g.emitReadItems(ref.Items, name, ind, false)
		case *ir.Union:
			g.emitReadUnion(ref, name, ind, false)
		}
	}
}

// emitReadBytesField reads string(N)/bytes(N): ranged length, align with
// zero padding verified, bounds, the bytes, and (strings) the interior-null
// refusal.
func (g *gen) emitReadBytesField(f *ir.Field, name, ind string) {
	length := name + "Length"
	lenBits := ir.BitsRequired(big.NewInt(0), big.NewInt(f.Type.Size))
	g.pf("%sif (bitsRead + %d > numBits) {\n%s  return false;\n%s}\n", ind, lenBits, ind, ind)
	g.readR(lenBits, ind)
	if f.Type.Size != (int64(1)<<lenBits)-1 {
		g.pf("%sif (v > %d) {\n%s  return false; // the length guards the slice — reject, never clamp\n%s}\n", ind, f.Type.Size, ind, ind)
	}
	g.pf("%s%s = v;\n", ind, length)
	g.emitReadAlign(ind)
	g.pf("%sif (bitsRead + %s * 8 > numBits) {\n%s  return false;\n%s}\n", ind, length, ind, ind)
	iv := fmt.Sprintf("i%d", g.loopDepth)
	g.loopDepth++
	g.pf("%s{\n", ind)
	g.pf("%s  final base = bitsRead >>> 3;\n", ind)
	g.pf("%s  for (var %s = 0; %s < %s; %s++) {\n", ind, iv, iv, length, iv)
	g.pf("%s    %s[%s] = view.getUint8(base + %s);\n", ind, name, iv, iv)
	g.pf("%s  }\n", ind)
	g.pf("%s}\n", ind)
	g.pf("%sbitsRead += %s * 8;\n", ind, length)
	if f.Type.Kind == ir.TString {
		g.pf("%sfor (var %s = 0; %s < %s; %s++) {\n", ind, iv, iv, length, iv)
		g.pf("%s  if (%s[%s] == 0) {\n", ind, name, iv)
		g.pf("%s    return false; // an interior null is content the read refuses (SPEC §4.7)\n", ind)
		g.pf("%s  }\n", ind)
		g.pf("%s}\n", ind)
	}
	g.loopDepth--
}

// emitReadWide64 assembles a 33..64-bit group pair into lo (low dword
// first — the serialize group order).
func (g *gen) emitReadWide64(bits int64, ind string) {
	g.needLo = true
	g.readR(32, ind)
	g.pf("%slo = v;\n", ind)
	g.readR(bits-32, ind)
	g.pf("%slo |= v << 32;\n", ind)
}

// emitReadWide128 assembles a 65..128-bit offset into (hi, lo), 32-bit
// groups least significant first.
func (g *gen) emitReadWide128(bits int64, ind string) {
	g.needLo, g.needHi = true, true
	g.emitReadWide64(64, ind)
	if bits <= 96 {
		g.readR(bits-64, ind)
		g.pf("%shi = v;\n", ind)
		return
	}
	g.readR(32, ind)
	g.pf("%shi = v;\n", ind)
	g.readR(bits-96, ind)
	g.pf("%shi |= v << 32;\n", ind)
}

func (g *gen) emitReadScalar(f *ir.Field, name, ind string, bounded bool) {
	if is128(f.Type) {
		g.emitRead128Scalar(f, name+" = ", ind)
		return
	}
	switch f.Type.Kind {
	case ir.TString, ir.TBytes:
		// only reachable as an array element (never inside a fused run —
		// staticBitsScalar calls it dynamic)
		g.emitReadBytesField(f, name, ind)
	case ir.TFixed:
		g.emitReadFixed(f, name, ind)
	case ir.TInt:
		g.emitReadInt(f, name, ind)
	case ir.TBits:
		w := int64(f.Type.Width)
		if w <= 32 {
			g.readR(w, ind)
			g.pf("%s%s = v;\n", ind, name)
			return
		}
		g.emitReadWide64(w, ind)
		g.pf("%s%s = lo;\n", ind, name)
	case ir.TBool:
		g.readR(1, ind)
		g.pf("%s%s = v != 0;\n", ind, name)
	case ir.TFloat32:
		if f.HasFloatRange {
			g.emitReadCompressedFloat(f, name, ind)
			return
		}
		g.needScratch, g.needF32Conv = true, true
		g.readR(32, ind)
		g.pf("%s%s = _doubleFromFloat32Bits(v);\n", ind, name)
	case ir.TFloat64:
		g.needScratch, g.needF64Conv = true, true
		g.emitReadWide64(64, ind)
		g.pf("%s%s = _doubleFromFloat64Bits(lo);\n", ind, name)
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			bits := ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max))
			if bits == 0 {
				g.pf("%s%s = 0;\n", ind, name)
				return
			}
			g.readR(bits, ind)
			if ref.Max != (int64(1)<<bits)-1 {
				g.pf("%sif (v > %d) {\n%s  return false; // headroom above the wire range is refused\n%s}\n", ind, ref.Max, ind, ind)
			}
			g.pf("%s%s = v;\n", ind, name)
		case *ir.Flags:
			wb := int64(ref.WireBits)
			if wb <= 32 {
				g.readR(wb, ind)
				g.pf("%s%s = v;\n", ind, name)
				return
			}
			g.emitReadWide64(wb, ind)
			g.pf("%s%s = lo;\n", ind, name)
		case *ir.Struct:
			g.emitReadItems(ref.Items, name, ind, bounded)
		case *ir.Union:
			g.emitReadUnion(ref, name, ind, bounded)
		}
	}
}

func (g *gen) emitReadCompressedFloat(f *ir.Field, name, ind string) {
	g.needScratch, g.needFround = true, true
	maxInt, bits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
	minF := float32(f.FMin)
	deltaF := float32(f.FMax) - minF
	mivF := float32(maxInt)
	g.readR(bits, ind)
	if maxInt != (uint64(1)<<bits)-1 {
		g.pf("%sif (v > %d) {\n%s  return false; // headroom above the quantum count is refused\n%s}\n", ind, maxInt, ind, ind)
	}
	g.pf("%s%s = _fround(\n", ind, name)
	g.pf("%s  _fround(_fround(_fround(v.toDouble()) / %s) * %s) + %s,\n",
		ind, f32lit(float64(mivF)), f32lit(float64(deltaF)), f32lit(float64(minF)))
	g.pf("%s);\n", ind)
}

func (g *gen) emitReadFixed(f *ir.Field, name, ind string) {
	rawMin, rawMax, bits := fixedRaw(f)
	if f.Type.Width == 128 {
		g.emitRead128Ranged(f.Type.Signed, name+" = ", rawMin, rawMax, bits, ind)
		return
	}
	if f.IntMin.Cmp(f.IntMax) == 0 {
		// degenerate: zero bits — the value is the range, raw min << F,
		// materialized with no wire read (SPEC §4.6)
		g.pf("%s%s = %s;\n", ind, name, dartIntLit(rawMin))
		return
	}
	g.emitReadOffset(name, rawMin, rawMax, bits, ind)
}

// emitReadOffset decodes a ranged value inside the 64-bit domain: the
// offset in `bits` bits, headroom rejected unless the range fills the
// width, min added back in the wrapping domain (exact — decoded values are
// in [min, max]).
func (g *gen) emitReadOffset(name string, min, max *big.Int, bits int64, ind string) {
	diff := new(big.Int).Sub(max, min)
	full := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))
	src := "v"
	if bits <= 32 {
		g.readR(bits, ind)
	} else {
		g.emitReadWide64(bits, ind)
		src = "lo"
	}
	if diff.Cmp(full) != 0 {
		if bits == 64 {
			g.needULess = true
			g.pf("%sif (_unsignedLessThan(%s, %s)) {\n%s  return false; // a smuggled offset is refused\n%s}\n",
				ind, dartIntLit(diff), src, ind, ind)
		} else {
			g.pf("%sif (%s > %s) {\n%s  return false; // a smuggled offset is refused\n%s}\n",
				ind, src, dartIntLit(diff), ind, ind)
		}
	}
	if min.Sign() == 0 {
		g.pf("%s%s = %s;\n", ind, name, src)
	} else {
		g.pf("%s%s = %s + %s;\n", ind, name, dartIntLit(min), src)
	}
}

func (g *gen) emitReadInt(f *ir.Field, name, ind string) {
	w := int64(f.Type.Width)
	if f.HasIntRange {
		if f.IntMin.Cmp(f.IntMax) == 0 {
			// degenerate range: zero bits — the value is the range,
			// materialized with no wire read (SPEC §4.6)
			g.pf("%s%s = %s;\n", ind, name, dartIntLit(f.IntMin))
			return
		}
		g.emitReadOffset(name, f.IntMin, f.IntMax, ir.BitsRequired(f.IntMin, f.IntMax), ind)
		return
	}
	// bare integer at storage width, sign recovered for signed narrows
	if w == 64 {
		g.emitReadWide64(64, ind)
		g.pf("%s%s = lo;\n", ind, name)
		return
	}
	g.readR(w, ind)
	if f.Type.Signed {
		shift := 64 - w
		g.pf("%s%s = (v << %d) >> %d;\n", ind, name, shift, shift)
	} else {
		g.pf("%s%s = v;\n", ind, name)
	}
}

// emitRead128Scalar decodes a 128-bit-storage scalar into the assignment
// target `assign` (a "path = " prefix).
func (g *gen) emitRead128Scalar(f *ir.Field, assign, ind string) {
	if f.Type.Kind == ir.TFixed {
		rawMin, rawMax, bits := fixedRaw(f)
		g.emitRead128Ranged(f.Type.Signed, assign, rawMin, rawMax, bits, ind)
		return
	}
	if f.HasIntRange {
		g.emitRead128Ranged(f.Type.Signed, assign, f.IntMin, f.IntMax, ir.BitsRequired(f.IntMin, f.IntMax), ind)
		return
	}
	// uint128 raw: full 128 bits
	g.addRef128("UInt128")
	g.emitReadWide128(128, ind)
	g.pf("%s%sUInt128(hi, lo);\n", ind, assign)
}

// emitRead128Ranged decodes a ranged 128-bit-storage value: the offset in
// the folded bit count, headroom rejected, min added back in 128-bit
// two's-complement arithmetic.
func (g *gen) emitRead128Ranged(signed bool, assign string, min, max *big.Int, bits int64, ind string) {
	typ := "UInt128"
	if signed {
		typ = "Int128"
	}
	g.addRef128(typ)
	if min.Cmp(max) == 0 {
		// degenerate: zero bits — the value is the range (SPEC §4.6)
		g.pf("%s{\n", ind)
		g.pf("%s  const locked = %s;\n", ind, g.pairCtor(signed, min))
		g.pf("%s  %slocked;\n", ind, assign)
		g.pf("%s}\n", ind)
		return
	}
	diff := new(big.Int).Sub(max, min)
	full := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))
	if bits <= 64 {
		src := "v"
		if bits <= 32 {
			g.readR(bits, ind)
		} else {
			g.emitReadWide64(bits, ind)
			src = "lo"
		}
		if diff.Cmp(full) != 0 {
			if bits == 64 {
				g.needULess = true
				g.pf("%sif (_unsignedLessThan(%s, %s)) {\n%s  return false; // a smuggled offset is refused\n%s}\n",
					ind, dartIntLit(new(big.Int).And(diff, maxUint64Big)), src, ind, ind)
			} else {
				g.pf("%sif (%s > %s) {\n%s  return false; // a smuggled offset is refused\n%s}\n",
					ind, src, dartIntLit(diff), ind, ind)
			}
		}
		g.emitAssign128(assign, signed, min, "0", src, ind)
		return
	}
	g.emitReadWide128(bits, ind)
	g.addRef128("UInt128")
	if diff.Cmp(full) != 0 {
		g.pf("%s{\n", ind)
		g.pf("%s  const diff = %s;\n", ind, g.pairCtor(false, diff))
		g.pf("%s  if (diff < UInt128(hi, lo)) {\n", ind)
		g.pf("%s    return false; // a smuggled offset is refused\n", ind)
		g.pf("%s  }\n", ind)
		g.pf("%s}\n", ind)
	}
	g.emitAssign128(assign, signed, min, "hi", "lo", ind)
}

// emitAssign128 assigns min + (hiExpr, loExpr) to the target, in the
// unsigned pair domain (two's complement — exact for both signednesses).
// The min const hoists into a block so no emitted line outgrows the
// formatter.
func (g *gen) emitAssign128(assign string, signed bool, min *big.Int, hiExpr, loExpr, ind string) {
	off := fmt.Sprintf("UInt128(%s, %s)", hiExpr, loExpr)
	g.addRef128("UInt128")
	if min.Sign() == 0 {
		if signed {
			g.pf("%s%s%s.toSigned();\n", ind, assign, off)
		} else {
			g.pf("%s%s%s;\n", ind, assign, off)
		}
		return
	}
	g.pf("%s{\n", ind)
	g.pf("%s  const min = %s;\n", ind, g.pairCtor(false, min))
	if signed {
		g.pf("%s  %s(min + %s).toSigned();\n", ind, assign, off)
	} else {
		g.pf("%s  %s(min + %s);\n", ind, assign, off)
	}
	g.pf("%s}\n", ind)
}

// emitReadUnion is the union read half: the tag reads in minimal bits and a
// value above the count is refused (SPEC §4.8); the selected arm
// zero-establishes, then its items inline.
func (g *gen) emitReadUnion(u *ir.Union, expr, ind string, bounded bool) {
	if u.Max == 0 {
		g.pf("%s%s.type = 0; // zero wire bits — only None exists (SPEC §4.8)\n", ind, expr)
		return
	}
	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(u.Max))
	if !bounded {
		g.pf("%sif (bitsRead + %d > numBits) {\n%s  return false;\n%s}\n", ind, bits, ind, ind)
	}
	g.readR(bits, ind)
	if u.Max != (int64(1)<<bits)-1 {
		g.pf("%sif (v > %d) {\n%s  return false; // not a wire-legal tag (SPEC §4.8)\n%s}\n", ind, u.Max, ind, ind)
	}
	g.pf("%s%s.type = v;\n", ind, expr)
	g.pf("%sswitch (%s.type) {\n", ind, expr)
	for i, vr := range u.Variants {
		arm := expr + "." + dartName(vr.Name)
		g.pf("%s  case %d:\n", ind, i+1)
		// the selected arm starts from the zero form (SPEC §5)
		empty := true
		for _, nf := range vr.Ref.Fields {
			g.emitZeroField(nf, arm, ind+"    ", false)
			empty = false
		}
		before := g.fn.Len()
		g.emitReadItems(vr.Ref.Items, arm, ind+"    ", false)
		if g.fn.Len() > before {
			empty = false
		}
		if empty {
			g.pf("%s    break; // empty arm — presence is the payload (SPEC §4.6)\n", ind)
		}
	}
	g.pf("%s}\n", ind)
}

// ---- zeroing (SPEC §5: untaken branch sides read as ZERO) ----

func (g *gen) emitZeroItems(items []ir.Item, path, ind string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			g.emitZeroField(item.F, path, ind, false)
		case *ir.Branch:
			g.emitZeroItems(item.Then, path, ind)
			g.emitZeroItems(item.Else, path, ind)
		}
	}
}

// emitZeroField zeroes one field's storage. viaCalls selects the zero*
// helper for named types (the standalone zero functions); the wire bodies
// inline recursively instead, keeping read paths call-free.
func (g *gen) emitZeroField(f *ir.Field, path, ind string, viaCalls bool) {
	name := path + "." + dartName(f.Name)
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		g.pf("%s%s.fillRange(0, %s.length, 0);\n", ind, name, name)
		g.pf("%s%sLength = 0;\n", ind, name)
	case f.Array != ir.ArrayNone:
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			iv := fmt.Sprintf("i%d", g.loopDepth)
			g.loopDepth++
			g.pf("%sfor (var %s = 0; %s < %d; %s++) {\n", ind, iv, iv, f.ArrayBound, iv)
			if viaCalls {
				g.addRef(f.Type.Name, "zero"+f.Type.Name)
				g.pf("%s  zero%s(%s[%s]);\n", ind, f.Type.Name, name, iv)
			} else {
				ev := fmt.Sprintf("e%d", g.loopDepth-1)
				g.pf("%s  final %s = %s[%s];\n", ind, ev, name, iv)
				for _, nf := range ref.Fields {
					g.emitZeroField(nf, ev, ind+"  ", false)
				}
			}
			g.pf("%s}\n", ind)
			g.loopDepth--
		case *ir.Union:
			// zero IS None per element: the tag resets; arms are unspecified
			// at None (SPEC §4.8)
			iv := fmt.Sprintf("i%d", g.loopDepth)
			g.loopDepth++
			g.pf("%sfor (var %s = 0; %s < %d; %s++) {\n", ind, iv, iv, f.ArrayBound, iv)
			g.pf("%s  %s[%s].type = 0;\n", ind, name, iv)
			g.pf("%s}\n", ind)
			g.loopDepth--
		default:
			g.pf("%s%s.fillRange(0, %s.length, %s);\n", ind, name, name, zeroElem(f.Type))
		}
		if f.Array == ir.ArrayCounted {
			g.pf("%s%sCount = 0;\n", ind, name)
		}
	default:
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			if f.Type.Kind == ir.TNamed {
				if viaCalls {
					g.addRef(f.Type.Name, "zero"+f.Type.Name)
					g.pf("%szero%s(%s);\n", ind, f.Type.Name, name)
				} else {
					for _, nf := range ref.Fields {
						g.emitZeroField(nf, name, ind, false)
					}
				}
				return
			}
		case *ir.Union:
			if f.Type.Kind == ir.TNamed {
				// zero IS None: the tag resets; arms are unspecified at None
				g.pf("%s%s.type = 0;\n", ind, name)
				return
			}
		}
		g.pf("%s%s = %s;\n", ind, name, zeroScalar(f.Type, g))
	}
}

// zeroElem is the fillRange zero of a typed-list element.
func zeroElem(t ir.FieldType) string {
	if t.Kind == ir.TFloat32 || t.Kind == ir.TFloat64 {
		return "0.0"
	}
	if is128(t) {
		if t.Signed {
			return "Int128.zero"
		}
		return "UInt128.zero"
	}
	return "0"
}

// zeroScalar is the §5 zero form of a scalar storage slot (enum references
// fold to 0 — wire bodies import nothing for zeroing).
func zeroScalar(t ir.FieldType, g *gen) string {
	switch {
	case t.Kind == ir.TBool:
		return "false"
	case t.Kind == ir.TFloat32 || t.Kind == ir.TFloat64:
		return "0.0"
	case is128(t):
		if t.Signed {
			g.addRef128("Int128")
			return "Int128.zero"
		}
		g.addRef128("UInt128")
		return "UInt128.zero"
	}
	return "0"
}

// ---- measure emission ----

// emitMeasureFunction emits measure<Name>: exact wire bits for a value,
// static runs folded to generation-time literals; a fully static type folds
// to a single return.
func (g *gen) emitMeasureFunction(name string, items []ir.Item) {
	g.resetFn()
	pending := int64(0)
	g.emitMeasureItems(items, "value", "  ", &pending)
	body := g.fn.String()

	g.bpf("// measure%s is the exact wire bits write%s would produce for value —\n", name, name)
	g.bpf("// trusted like the writer; static runs fold to literals at generation time.\n")
	if body == "" {
		// fully static: the whole wire folded to one constant
		g.bpf("int measure%s(%s value) => %d;\n\n", name, name, pending)
		return
	}
	g.bpf("int measure%s(%s value) {\n", name, name)
	g.bpf("  var bits = 0;\n")
	g.body.WriteString(body)
	if pending != 0 {
		g.bpf("  bits += %d;\n", pending)
	}
	g.bpf("  return bits;\n")
	g.bpf("}\n\n")
}

// flushMeasure adds the pending folded bits before dynamic code that needs
// the running position.
func (g *gen) flushMeasure(pending *int64, ind string) {
	if *pending != 0 {
		g.pf("%sbits += %d;\n", ind, *pending)
		*pending = 0
	}
}

func (g *gen) emitMeasureItems(items []ir.Item, path, ind string, pending *int64) {
	for _, item := range items {
		if bits, ok := g.staticBitsItem(item); ok {
			*pending += bits
			continue
		}
		switch item := item.(type) {
		case *ir.FieldItem:
			g.emitMeasureField(item.F, path, ind, pending)
		case *ir.AlignItem:
			g.flushMeasure(pending, ind)
			g.pf("%sbits += (8 - (bits & 7)) & 7;\n", ind)
		case *ir.Branch:
			g.flushMeasure(pending, ind)
			neg := ""
			if item.Neg {
				neg = "!"
			}
			g.pf("%sif (%s%s.%s) {\n", ind, neg, path, dartName(item.Cond))
			thenPending := int64(0)
			g.emitMeasureItems(item.Then, path, ind+"  ", &thenPending)
			if thenPending != 0 {
				g.pf("%s  bits += %d;\n", ind, thenPending)
			}
			if item.Else != nil {
				g.pf("%s} else {\n", ind)
				elsePending := int64(0)
				g.emitMeasureItems(item.Else, path, ind+"  ", &elsePending)
				if elsePending != 0 {
					g.pf("%s  bits += %d;\n", ind, elsePending)
				}
			}
			g.pf("%s}\n", ind)
		}
	}
}

func (g *gen) emitMeasureField(f *ir.Field, path, ind string, pending *int64) {
	name := path + "." + dartName(f.Name)
	if f.Name == "" {
		name = path
	}
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		lenBits := ir.BitsRequired(big.NewInt(0), big.NewInt(f.Type.Size))
		*pending += lenBits
		g.flushMeasure(pending, ind)
		g.pf("%sbits += (8 - (bits & 7)) & 7;\n", ind)
		g.pf("%sbits += %sLength * 8;\n", ind, name)
	case f.Array == ir.ArrayCounted:
		countBits := ir.BitsRequired(big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound))
		*pending += countBits
		if elemBits, ok := g.staticBitsScalar(f); ok {
			g.flushMeasure(pending, ind)
			g.pf("%sbits += %sCount * %d;\n", ind, name, elemBits)
			return
		}
		g.flushMeasure(pending, ind)
		iv := fmt.Sprintf("i%d", g.loopDepth)
		g.loopDepth++
		g.pf("%sfor (var %s = 0; %s < %sCount; %s++) {\n", ind, iv, iv, name, iv)
		g.emitMeasureElem(f, name, iv, ind+"  ", pending)
		g.pf("%s}\n", ind)
		g.loopDepth--
	case f.Array == ir.ArrayFixed:
		// a fixed array of dynamically-sized elements
		g.flushMeasure(pending, ind)
		iv := fmt.Sprintf("i%d", g.loopDepth)
		g.loopDepth++
		g.pf("%sfor (var %s = 0; %s < %d; %s++) {\n", ind, iv, iv, f.ArrayBound, iv)
		g.emitMeasureElem(f, name, iv, ind+"  ", pending)
		g.pf("%s}\n", ind)
		g.loopDepth--
	default:
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			g.emitMeasureItems(ref.Items, name, ind, pending)
		case *ir.Union:
			g.flushMeasure(pending, ind)
			g.emitMeasureUnion(ref, name, ind)
		}
	}
}

func (g *gen) emitMeasureElem(f *ir.Field, name, iv, ind string, pending *int64) {
	if f.Type.Kind == ir.TNamed {
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			ev := fmt.Sprintf("e%d", g.loopDepth-1)
			g.pf("%sfinal %s = %s[%s];\n", ind, ev, name, iv)
			inner := int64(0)
			g.emitMeasureItems(ref.Items, ev, ind, &inner)
			if inner != 0 {
				g.pf("%sbits += %d;\n", ind, inner)
			}
			return
		case *ir.Union:
			ev := fmt.Sprintf("e%d", g.loopDepth-1)
			g.pf("%sfinal %s = %s[%s];\n", ind, ev, name, iv)
			g.emitMeasureUnion(ref, ev, ind)
			return
		}
	}
	// unreachable: every dynamically-sized element is a named type — the
	// checker refuses arrays of string/bytes/bits (SPEC §4.6)
}

// emitMeasureUnion measures tag + selected arm through a switch.
func (g *gen) emitMeasureUnion(u *ir.Union, expr, ind string) {
	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(u.Max))
	if u.Max == 0 {
		return // zero wire bits — only None exists (SPEC §4.8)
	}
	g.pf("%sbits += %d;\n", ind, bits)
	g.pf("%sswitch (%s.type) {\n", ind, expr)
	for i, vr := range u.Variants {
		arm := expr + "." + dartName(vr.Name)
		g.pf("%s  case %d:\n", ind, i+1)
		inner := int64(0)
		before := g.fn.Len()
		g.emitMeasureItems(vr.Ref.Items, arm, ind+"    ", &inner)
		if inner != 0 {
			g.pf("%s    bits += %d;\n", ind, inner)
		} else if g.fn.Len() == before {
			g.pf("%s    break; // empty arm — presence is the payload (SPEC §4.6)\n", ind)
		}
	}
	g.pf("%s}\n", ind)
}
