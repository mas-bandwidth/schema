// Write/Read function emission for types (SPEC §6.1 items 2-4,
// §6.2): straight-line split functions against the serialize.js holder API —
// every call's bool is checked and early-outs, the C++ twin's shape (§6.3),
// so counts and lengths are validated before the loop or slice they guard.
// The runtime's error latch (stream.error) rides beneath the bool for
// callers; generated validation refusals return false without latching. The
// wire is byte-identical to the C++ target's, construct by construct.
//
// Write-side guards: the generated code folds ranged bounds at generation
// time (the C++ fold mechanism, carried through Go and C#), bypassing the
// runtime's ranged calls on the write path — so it supplies their
// write-side range refusal itself, returning false without latching, in
// BOTH runtime modes. JS storage is untyped where the siblings' is typed,
// so Number-domain guards also refuse non-integers (the runtime's own
// checked-write shape); BigInt-domain paths reinterpret through
// BigInt.asIntN/asUintN exactly where C# storage typing would wrap. Reads
// stay on the runtime ranged calls — the family rule (nothing to gain, and
// the read side's latched ValueOutOfRange semantics stay untouched).
package js

import (
	"fmt"
	"math"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// emitUnionFunctions emits the union's bounds, Zero helper and wire pair
// (SPEC §4.8): the write validates the tag BEFORE it rides (the union tag
// rule), the read rejects a tag above the count and zero-establishes exactly
// the selected arm.
func (g *gen) emitUnionFunctions(d *ir.Union) {
	maxBits := ir.MaxBitsUnion(d)
	g.pf("// %sMaxBits is the tag plus the largest arm; None costs the tag only (SPEC §4.8).\n", d.Name)
	g.pf("// %sMaxBytes is rounded up to the 8-byte write-buffer granularity.\n", d.Name)
	g.pf("export const %sMaxBits = %d;\n", d.Name, maxBits)
	g.pf("export const %sMaxBytes = %d;\n\n", d.Name, ir.MaxBytes(maxBits))

	g.pf("// Zero%s resets value to the §5 zero form — the empty union. The tag alone\n", d.Name)
	g.pf("// resets: unselected arms are unspecified by rule (SPEC §4.8), and every arm\n")
	g.pf("// is unselected at None; an arm re-zeroes at its next selection.\n")
	g.pf("export function Zero%s(value) {\n", d.Name)
	g.pf("  value.Type = %sType.None;\n}\n\n", d.Name)

	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(d.Max))
	g.pf("export function Write%s(stream, value) {\n", d.Name)
	if d.Max == 0 {
		g.pf("  // an empty union holds only None; its degenerate tag range [0, 0] costs\n")
		g.pf("  // zero bits (SPEC §4.8)\n")
		g.pf("  return value.Type === %sType.None;\n}\n\n", d.Name)
	} else {
		g.pf("  if (!Number.isInteger(value.Type) || value.Type < 0 || value.Type > %d) {\n", d.Max)
		g.pf("    return false; // the tag validates BEFORE it rides (SPEC §4.8)\n  }\n")
		scratch := g.numScratch()
		g.pf("  %s.value = value.Type;\n", scratch)
		g.call("  ", fmt.Sprintf("stream.serializeBits(%s, %d)", scratch, bits), "")
		g.pf("  switch (value.Type) {\n")
		for _, v := range d.Variants {
			if v.Void() {
				g.pf("    case %sType.%s:\n      return true; // a payload-free arm: the tag is the whole wire (SPEC §4.8)\n",
					d.Name, ir.GoExportName(v.Name))
				continue
			}
			g.addRef(v.Type, "Write"+v.Type)
			g.pf("    case %sType.%s:\n      return Write%s(stream, value.%s);\n",
				d.Name, ir.GoExportName(v.Name), v.Type, ir.GoExportName(v.Name))
		}
		g.pf("  }\n  return true; // None — the tag is the whole wire (SPEC §4.8)\n}\n\n")
	}

	g.pf("export function Read%s(stream, value) {\n", d.Name)
	if d.Max == 0 {
		g.pf("  value.Type = %sType.None; // zero wire bits — only None exists (SPEC §4.8)\n", d.Name)
		g.pf("  return true;\n}\n\n")
		return
	}
	scratch := g.numScratch()
	g.call("  ", fmt.Sprintf("stream.serializeInt(%s, 0, %d)", scratch, d.Max), " // rejects a tag above the count (SPEC §4.8)")
	g.pf("  value.Type = %s.value;\n", scratch)
	g.pf("  switch (value.Type) {\n")
	for _, v := range d.Variants {
		g.pf("    case %sType.%s:\n", d.Name, ir.GoExportName(v.Name))
		if v.Void() {
			g.pf("      return true; // a payload-free arm: the tag is the whole wire (SPEC §4.8)\n")
			continue
		}
		g.addRef(v.Type, "Zero"+v.Type, "Read"+v.Type)
		g.pf("      Zero%s(value.%s); // the selected arm starts from the zero form (SPEC §5)\n", v.Type, ir.GoExportName(v.Name))
		g.pf("      return Read%s(stream, value.%s);\n", v.Type, ir.GoExportName(v.Name))
	}
	g.pf("  }\n  return true; // None\n}\n\n")
}

// emitStructFunctions emits MaxBits/MaxBytes, the Zero helper, and the split
// Write/Read pair for a type.
func (g *gen) emitStructFunctions(st *ir.Struct) {
	// the statically byte-aligned [N]uint8 fields of THIS struct take the
	// serializeBytes bulk path (a per-struct alignment proof)
	g.bulkBytes = ir.AlignedFixedByteArrays(st)
	maxBits := ir.MaxBitsStruct(st)
	g.pf("// %sMaxBits is the longest wire path; align pads at worst case (SPEC §6.1).\n", st.Name)
	g.pf("// %sMaxBytes is rounded up to the 8-byte write-buffer granularity.\n", st.Name)
	g.pf("export const %sMaxBits = %d;\n", st.Name, maxBits)
	g.pf("export const %sMaxBytes = %d;\n\n", st.Name, ir.MaxBytes(maxBits))

	g.emitZeroFunction(st)

	g.pf("export function Write%s(stream, value) {\n", st.Name)
	if len(st.Items) == 0 {
		g.pf("  // empty body — presence is the payload (SPEC §4.6)\n")
	} else {
		g.emitWriteItems(st.Items, "  ")
	}
	g.pf("  return true;\n}\n\n")

	g.pf("export function Read%s(stream, value) {\n", st.Name)
	if len(st.Items) > 0 {
		g.emitReadItems(st.Items, "  ")
	}
	g.pf("  return true;\n}\n\n")
}

// emitZeroFunction emits the §5 ZERO form for a class — all-zero storage,
// specified defaults NOT reapplied (those live in construction only; the
// wire contract stays a pure function of the encodings). JS classes are
// reference types, so this is the C# in-place-zero idiom: branch zeroing
// and the union arm reset both go through here.
func (g *gen) emitZeroFunction(st *ir.Struct) {
	g.pf("// The §5 zero form: all-zero storage; specified defaults live only in construction.\n")
	g.pf("export function Zero%s(value) {\n", st.Name)
	if len(st.Fields) == 0 {
		g.pf("  // empty body — nothing to reset (SPEC §4.6)\n")
	}
	for _, f := range st.Fields {
		g.emitZeroField(f, "  ")
	}
	g.pf("}\n\n")
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
			g.call(ind, "stream.serializeAlign()", "")
		case *ir.Branch:
			neg := ""
			if item.Neg {
				neg = "!"
			}
			g.pf("%sif (%svalue.%s) {\n", ind, neg, ir.GoExportName(item.Cond))
			g.emitWriteItems(item.Then, ind+"  ")
			if item.Else != nil {
				g.pf("%s} else {\n", ind)
				g.emitWriteItems(item.Else, ind+"  ")
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
			g.call(ind, "stream.serializeAlign()", " // rejects nonzero padding (SPEC §4.3)")
		case *ir.Branch:
			neg := ""
			if item.Neg {
				neg = "!"
			}
			g.pf("%sif (%svalue.%s) {\n", ind, neg, ir.GoExportName(item.Cond))
			g.emitReadItems(item.Then, ind+"  ")
			// the untaken side reads as zero values (SPEC §5)
			g.emitZeroItems(item.Else, ind+"  ")
			g.pf("%s} else {\n", ind)
			if item.Else != nil {
				g.emitReadItems(item.Else, ind+"  ")
			}
			g.emitZeroItems(item.Then, ind+"  ")
			g.pf("%s}\n", ind)
		}
	}
}

// call emits the C++-style bool early-out around one serialize call; comment
// (leading " // ...") rides the if line.
func (g *gen) call(ind, expr, comment string) {
	g.pf("%sif (!%s) {%s\n%s  return false;\n%s}\n", ind, expr, comment, ind, ind)
}

// emitConstItem writes const(value, bits) on the wire; a read rejects any
// other value (SPEC §4.3).
func (g *gen) emitConstItem(item *ir.ConstItem, ind string, writing bool) {
	scratch, fn, lit := g.numScratch(), "serializeBits", item.Value.String()
	if item.Bits > 32 {
		scratch, fn, lit = g.bigScratch(), "serializeBits64", bigLit(item.Value)
	}
	if writing {
		g.pf("%s%s.value = %s;\n", ind, scratch, lit)
		g.call(ind, fmt.Sprintf("stream.%s(%s, %d)", fn, scratch, item.Bits),
			fmt.Sprintf(" // const(%s, %d) — SPEC §4.3", item.Value.String(), item.Bits))
		return
	}
	g.call(ind, fmt.Sprintf("stream.%s(%s, %d)", fn, scratch, item.Bits), "")
	g.pf("%sif (%s.value !== %s) { // const(%s, %d): a read rejects any other value (SPEC §4.3)\n",
		ind, scratch, lit, item.Value.String(), item.Bits)
	g.pf("%s  return false;\n%s}\n", ind, ind)
}

// emitReservedItem writes reserved(bits) as zeros; a read rejects nonzero.
func (g *gen) emitReservedItem(item *ir.ReservedItem, ind string, writing bool) {
	scratch, fn, zero := g.numScratch(), "serializeBits", "0"
	if item.Bits > 32 {
		scratch, fn, zero = g.bigScratch(), "serializeBits64", "0n"
	}
	if writing {
		g.pf("%s%s.value = %s;\n", ind, scratch, zero)
		g.call(ind, fmt.Sprintf("stream.%s(%s, %d)", fn, scratch, item.Bits),
			fmt.Sprintf(" // reserved(%d) — zeros on the wire", item.Bits))
		return
	}
	g.call(ind, fmt.Sprintf("stream.%s(%s, %d)", fn, scratch, item.Bits), "")
	g.pf("%sif (%s.value !== %s) { // reserved(%d): a read rejects nonzero (SPEC §4.3)\n", ind, scratch, zero, item.Bits)
	g.pf("%s  return false;\n%s}\n", ind, ind)
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
	name := "value." + ir.GoExportName(f.Name)
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		g.pf("%s%s.fill(0);\n%s%sLength = 0;\n", ind, name, ind, name)
	case f.Array != ir.ArrayNone:
		if f.Type.Kind == ir.TNamed && isClassRef(f.Type.Ref) {
			// clearing the array would drop the pre-allocated elements — zero
			// through them instead (the SCHEMA bound, like the wire loops)
			g.addRef(f.Type.Name, "Zero"+f.Type.Name)
			g.pf("%sfor (let i = 0; i < %s; i++) {\n", ind, g.renderNum(f.ArrayExpr, big.NewInt(f.ArrayBound)))
			g.pf("%s  Zero%s(%s[i]);\n%s}\n", ind, f.Type.Name, name, ind)
		} else {
			g.pf("%s%s.fill(%s);\n", ind, name, g.zeroValue(f.Type))
		}
		if f.Array == ir.ArrayCounted {
			g.pf("%s%sCount = 0;\n", ind, name)
		}
	default:
		if f.Type.Kind == ir.TNamed && isClassRef(f.Type.Ref) {
			// through the nested Zero — §5 wants ZERO values recursively (a
			// union's Zero is the tag reset), and re-constructing would
			// restore specified defaults instead
			g.addRef(f.Type.Name, "Zero"+f.Type.Name)
			g.pf("%sZero%s(%s);\n", ind, f.Type.Name, name)
			return
		}
		g.pf("%s%s = %s;\n", ind, name, g.zeroValue(f.Type))
	}
}

// scratch accessors mark the module-scoped holders used.
func (g *gen) numScratch() string {
	g.needNumScratch = true
	return "NUMBER_SCRATCH"
}

func (g *gen) bigScratch() string {
	g.needBigScratch = true
	return "BIGINT_SCRATCH"
}

func (g *gen) boolScratch() string {
	g.needBoolScratch = true
	return "BOOL_SCRATCH"
}

// rangeArgsNum renders the min/max arguments for a Number-domain call site.
func (g *gen) rangeArgsNum(f *ir.Field) (string, string) {
	return g.renderNum(f.IntMinExpr, f.IntMin), g.renderNum(f.IntMaxExpr, f.IntMax)
}

// rangeArgsBig renders the min/max arguments for a BigInt-domain call site.
// BigInt bounds always fold to literals: bare schema consts store as Numbers
// and JS cannot mix the domains in one expression — a BigInt literal is
// exact at any width, so folding is always correct.
func (g *gen) rangeArgsBig(f *ir.Field) (string, string) {
	return bigLit(f.IntMin), bigLit(f.IntMax)
}

// ---- generation-time bound folding on the write path (the C++ fold pass.s
// mechanism, carried through Go and C# to JS) ----
//
// A ranged integer's min/max/bit count are schema constants, so the
// generator folds them: the write emits the offset from min in a bit count
// computed at generation time through the raw serializeBits/serializeBits64
// calls — no runtime bitsRequired, no min/max parameter traffic, and the
// 32/64 split resolves here, not at run time. The wire bytes are identical
// to the runtime serializeInt/serializeInt64 forms (offset-from-min in
// bitsRequired(min, max) bits — the wire-identity property the C++ backend
// proved, re-proven here by the wire golden gate). The runtime ranged
// write's out-of-range refusal moves into the generated code with the fold:
// the guard returns false WITHOUT latching — the family's generated-guard
// form. Reads stay on the runtime ranged calls.

// maxUint64 is 2^64 - 1, the top of unsigned-64 storage — the bound against
// which a range guard becomes vacuous.
var maxUint64 = new(big.Int).SetUint64(math.MaxUint64)

// intRangePath picks the runtime call family for a ranged integer — the same
// switch as the other targets, so all six emit identical wire.
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

// emitWriteFoldedNum writes the Number-domain integer expression expr, in
// | min, max, as offset-from-min in a generation-time bit count. JS Number
// storage is untyped, so the guard is never vacuous and also refuses
// non-integers (checkInt) — the runtime checked write's own shape, folded,
// riding in both modes. The offset subtraction is exact: the int64-family
// Number case only arises for uint32 storage, whose offsets stay below
// 2^32 — inside the double integer domain — and bits <= 32 always holds
// there, so every Number-domain fold lands on serializeBits.
func (g *gen) emitWriteFoldedNum(expr, lo, hi string, min, max *big.Int, checkInt bool, comment, ind string) {
	guard := fmt.Sprintf("%s < %s || %s > %s", expr, lo, expr, hi)
	if checkInt {
		guard = fmt.Sprintf("!Number.isInteger(%s) || ", expr) + guard
	}
	g.pf("%sif (%s) {%s\n%s  return false;\n%s}\n", ind, guard, comment, ind, ind)
	bits := ir.BitsRequired(min, max)
	if bits == 0 {
		// A degenerate range costs ZERO BITS — the value is known from the
		// range alone; the refusal above is the whole write (SPEC §4.6,
		// decided deliberately)
		return
	}
	scratch := g.numScratch()
	if min.Sign() == 0 {
		g.pf("%s%s.value = %s;\n", ind, scratch, expr)
	} else {
		// the offset subtraction lives in the unsigned domain, exactly as the
		// runtime forms compute it (streams.js serializeInt); >>> 0 keeps
		// ranges wider than 2^31 exact — for bounds past the int32 domain
		// (uint32 storage) the plain subtraction is already exact in doubles
		if min.Cmp(big.NewInt(math.MinInt32)) >= 0 && max.Cmp(big.NewInt(math.MaxInt32)) <= 0 {
			g.pf("%s%s.value = ((%s >>> 0) - (%s >>> 0)) >>> 0;\n", ind, scratch, expr, lo)
		} else {
			g.pf("%s%s.value = %s - %s;\n", ind, scratch, expr, lo)
		}
	}
	g.call(ind, fmt.Sprintf("stream.serializeBits(%s, %d)", scratch, bits), "")
}

// emitWriteFoldedBig is the BigInt-domain fold: guard (vacuous halves
// elided per the declared storage bounds, the C# discipline), then the
// offset via BigInt.asUintN(64, v - min) — BigInt does not wrap, so the
// two's-complement reduction is explicit where Go relies on defined signed
// wrap. Where the bit count fits 32, the offset narrows to a Number and
// rides serializeBits — byte-identical, the runtime's own split.
func (g *gen) emitWriteFoldedBig(expr, lo, hi string, min, max *big.Int, guardLo, guardHi bool, comment, ind string) {
	switch {
	case guardLo && guardHi:
		g.pf("%sif (%s < %s || %s > %s) {%s\n%s  return false;\n%s}\n", ind, expr, lo, expr, hi, comment, ind, ind)
	case guardLo:
		g.pf("%sif (%s < %s) {%s\n%s  return false;\n%s}\n", ind, expr, lo, comment, ind, ind)
	case guardHi:
		g.pf("%sif (%s > %s) {%s\n%s  return false;\n%s}\n", ind, expr, hi, comment, ind, ind)
	}
	bits := ir.BitsRequired(min, max)
	if bits == 0 {
		// degenerate range: ZERO bits — the refusal above is the whole write
		return
	}
	offset := fmt.Sprintf("BigInt.asUintN(64, %s)", expr)
	if min.Sign() != 0 {
		offset = fmt.Sprintf("BigInt.asUintN(64, %s - %s)", expr, lo)
	}
	if bits <= 32 {
		scratch := g.numScratch()
		g.pf("%s%s.value = Number(%s);\n", ind, scratch, offset)
		g.call(ind, fmt.Sprintf("stream.serializeBits(%s, %d)", scratch, bits), "")
		return
	}
	scratch := g.bigScratch()
	g.pf("%s%s.value = %s;\n", ind, scratch, offset)
	g.call(ind, fmt.Sprintf("stream.serializeBits64(%s, %d)", scratch, bits), "")
}

func (g *gen) emitWriteField(f *ir.Field, ind string) {
	name := "value." + ir.GoExportName(f.Name)
	if f.Array != ir.ArrayNone {
		bound := g.renderNum(f.ArrayExpr, big.NewInt(f.ArrayBound))
		if g.bulkBytes[f] {
			// statically byte-aligned [N]uint8 (ir.AlignedFixedByteArrays):
			// serializeBytes aligns zero bits here and bulk-copies —
			// byte-identical to the per-byte loop. subarray over the SCHEMA
			// bound is the C# AsSpan(0, N) slice; the short-lived view is the
			// one storage slice JavaScript cannot take without allocating.
			g.call(ind, fmt.Sprintf("stream.serializeBytes(%s.subarray(0, %s))", name, bound),
				" // byte-aligned [N]uint8 — bulk copy, wire-identical to the per-byte loop")
			return
		}
		if f.Array == ir.ArrayCounted {
			count := name + "Count"
			g.emitWriteFoldedNum(count, fmt.Sprintf("%d", f.ArrayMin), bound,
				big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound), true,
				" // the count guards the loop (§6.3)", ind)
			g.pf("%sfor (let i = 0; i < %s; i++) {\n", ind, count)
		} else {
			// the SCHEMA bound, never the storage's length: reassigned-short
			// storage must fail loudly, not silently write fewer elements
			g.pf("%sfor (let i = 0; i < %s; i++) {\n", ind, bound)
		}
		g.emitWriteScalar(f, name+"[i]", ind+"  ")
		g.pf("%s}\n", ind)
		return
	}
	g.emitWriteScalar(f, name, ind)
}

func (g *gen) emitWriteScalar(f *ir.Field, name, ind string) {
	switch f.Type.Kind {
	case ir.TFixed:
		if f.IntMin.Cmp(f.IntMax) == 0 {
			// degenerate range: ZERO bits — the folded range refusal and no
			// wire call at all (SPEC §4.6). The one legal
			// raw is min << F, an exact literal in either value domain.
			rawMin := new(big.Int).Lsh(f.IntMin, uint(f.Type.FracBits))
			g.pf("%sif (%s !== %s) {%s\n%s  return false;\n%s}\n", ind, name, g.rawLit(f.Type, rawMin), "", ind, ind)
			return
		}
		// the Q format and whole-unit bounds are compile-time constants of
		// the call site, exactly like a ranged integer's bounds (STANDARD.md,
		// fixed): one runtime call for every storage width — narrow bounds
		// are Numbers, wide bounds BigInts (streams.js fixedPointParams)
		scratch, lo, hi := g.numScratch(), "", ""
		if f.Type.Width > 32 {
			scratch = g.bigScratch()
			lo, hi = g.rangeArgsBig(f)
		} else {
			lo, hi = g.rangeArgsNum(f)
		}
		g.pf("%s%s.value = %s;\n", ind, scratch, name)
		g.call(ind, fmt.Sprintf("stream.serializeFixed(%s, %d, %d, %s, %s)", scratch, f.Type.IntBits, f.Type.FracBits, lo, hi), "")
	case ir.TInt:
		if f.Type.Width == 128 {
			scratch := g.bigScratch()
			if f.HasIntRange {
				if f.IntMin.Cmp(f.IntMax) == 0 {
					// degenerate range: ZERO bits — refusal only (SPEC §4.6)
					g.pf("%sif (%s !== %s) {%s\n%s  return false;\n%s}\n", ind, name, bigLit(f.IntMin), "", ind, ind)
					return
				}
				// int128 is ALWAYS ranged (SPEC §4.3): offset from min —
				// identical bytes to serializeInt64 wherever the range fits.
				// The bounds are plain BigInt literals: exact at any width,
				// no lane composition (unlike the Go/C#/C++ pair renderers).
				g.pf("%s%s.value = %s;\n", ind, scratch, name)
				g.call(ind, fmt.Sprintf("stream.serializeInt128(%s, %s, %s)", scratch, bigLit(f.IntMin), bigLit(f.IntMax)), "")
			} else {
				// uint128 is the raw field: 128 bits, low 64-bit half first
				g.pf("%s%s.value = %s;\n", ind, scratch, name)
				g.call(ind, fmt.Sprintf("stream.serializeUint128(%s)", scratch), "")
			}
			return
		}
		if f.HasIntRange {
			switch intRangePath(f.IntMin, f.IntMax) {
			case "int32":
				lo, hi := g.rangeArgsNum(f)
				if f.Type.Width > 32 {
					// BigInt storage with an int32-family range: truncate to
					// the call family's domain first — the C# (int) cast's
					// twin — then guard in the Number domain
					g.pf("%s{\n%s  const rangeValue = Number(BigInt.asIntN(32, %s));\n", ind, ind, name)
					g.emitWriteFoldedNum("rangeValue", lo, hi, f.IntMin, f.IntMax, false, "", ind+"  ")
					g.pf("%s}\n", ind)
					return
				}
				g.emitWriteFoldedNum(name, lo, hi, f.IntMin, f.IntMax, true, "", ind)
			case "int64":
				if f.Type.Width <= 32 {
					// Number storage on the int64 path: only uint32 storage
					// reaches here (no other Number storage holds a bound past
					// int32), so values and offsets stay below 2^32 — exact in
					// the double domain, no BigInt needed. Byte-identical: the
					// same offset in the same bit count.
					lo, hi := g.rangeArgsNum(f)
					g.emitWriteFoldedNum(name, lo, hi, f.IntMin, f.IntMax, true, "", ind)
					return
				}
				lo, hi := g.rangeArgsBig(f)
				sMin, sMax := storageBoundsBig(f.Type)
				g.emitWriteFoldedBig(name, lo, hi, f.IntMin, f.IntMax,
					f.IntMin.Cmp(sMin) > 0, f.IntMax.Cmp(sMax) < 0, "", ind)
			default:
				// full-range unsigned: raw offset bits (uint64 storage only —
				// no narrower storage can hold a range past int64); vacuous
				// guard halves are elided, and BigInt.asUintN supplies the
				// two's-complement wrap C# ulong storage performs by type
				lo, hi := g.rangeArgsBig(f)
				g.emitWriteFoldedBig(name, lo, hi, f.IntMin, f.IntMax,
					f.IntMin.Sign() != 0, f.IntMax.Cmp(maxUint64) != 0, "", ind)
			}
			return
		}
		g.emitWriteBareInt(f, name, ind)
	case ir.TBits:
		if f.Type.Width <= 32 {
			scratch := g.numScratch()
			g.pf("%s%s.value = %s;\n", ind, scratch, name)
			g.call(ind, fmt.Sprintf("stream.serializeBits(%s, %d)", scratch, f.Type.Width), "")
		} else {
			scratch := g.bigScratch()
			g.pf("%s%s.value = %s;\n", ind, scratch, name)
			g.call(ind, fmt.Sprintf("stream.serializeBits64(%s, %d)", scratch, f.Type.Width), "")
		}
	case ir.TBool:
		scratch := g.boolScratch()
		g.pf("%s%s.value = %s;\n", ind, scratch, name)
		g.call(ind, fmt.Sprintf("stream.serializeBool(%s)", scratch), "")
	case ir.TFloat32:
		scratch := g.numScratch()
		if f.HasFloatRange {
			// the holder copy is the write-through temp: the runtime's wire
			// quantization cannot write back into the input
			g.pf("%s%s.value = %s;\n", ind, scratch, name)
			g.call(ind, fmt.Sprintf("stream.serializeCompressedFloat(%s, %s, %s, %s)",
				scratch, formatFloat32(f.FMin), formatFloat32(f.FMax), formatFloat32(f.Resolution)), "")
			return
		}
		g.pf("%s%s.value = %s;\n", ind, scratch, name)
		g.call(ind, fmt.Sprintf("stream.serializeFloat(%s)", scratch), "")
	case ir.TFloat64:
		scratch := g.numScratch()
		g.pf("%s%s.value = %s;\n", ind, scratch, name)
		g.call(ind, fmt.Sprintf("stream.serializeDouble(%s)", scratch), "")
	case ir.TString, ir.TBytes:
		// length in [0, N], align, then the used bytes — the classic
		// serialize_string framing over a pre-allocated buffer (SPEC §4.7),
		// COMPOSED FROM PRIMITIVES, not the runtime's serializeString: the
		// schema contract is writer-trusted UTF-8 with no read-path decode
		// (SPEC §4.7), and serializeString's read side is UTF-8-strict — it
		// would refuse wire every other target accepts. The length guard runs
		// BEFORE the slice, so a stale length never reaches subarray (§6.3).
		// Interior nulls are writer misuse; the read side rejects them (§4.7).
		length := name + "Length"
		g.emitWriteFoldedNum(length, "0", g.renderNum(f.Type.SizeExpr, big.NewInt(f.Type.Size)),
			big.NewInt(0), big.NewInt(f.Type.Size), true,
			" // the length guards the slice (§6.3); out-of-contract writes are refused", ind)
		g.call(ind, fmt.Sprintf("stream.serializeBytes(%s.subarray(0, %s))", name, length), "")
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			g.emitWriteFoldedEnum(ref, name, ind)
		case *ir.Flags:
			g.emitWriteFlagsValue(name, ref.WireBits, ind)
		case *ir.Struct:
			g.addRef(f.Type.Name, "Write"+f.Type.Name)
			g.call(ind, fmt.Sprintf("Write%s(stream, %s)", f.Type.Name, name), "")
		case *ir.Union:
			g.addRef(f.Type.Name, "Write"+f.Type.Name)
			g.call(ind, fmt.Sprintf("Write%s(stream, %s)", f.Type.Name, name), "")
		}
	}
}

// storageBoundsBig is the representable range of a 64-bit integer storage
// type — the domain against which a BigInt-path guard half is vacuous.
func storageBoundsBig(t ir.FieldType) (*big.Int, *big.Int) {
	w := uint(t.Width)
	if t.Signed {
		half := new(big.Int).Lsh(big.NewInt(1), w-1)
		return new(big.Int).Neg(half), new(big.Int).Sub(half, big.NewInt(1))
	}
	return big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), w), big.NewInt(1))
}

// rawLit renders a raw scaled fixed-point value in the field's value domain.
func (g *gen) rawLit(t ir.FieldType, raw *big.Int) string {
	if t.Width > 32 {
		return bigLit(raw)
	}
	return raw.String()
}

// emitWriteFoldedEnum writes an enum in [0, Max] with a generation-time bit
// count — byte-identical to the runtime serializeInt form it replaces. The
// guard is the unsigned-temp discipline translated to untyped Number
// storage: negative and non-integer values are refused alongside headroom
// above the wire range, and it is never vacuous (JS storage has no backing
// type to fill).
func (g *gen) emitWriteFoldedEnum(ref *ir.Enum, name, ind string) {
	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max))
	g.pf("%sif (!Number.isInteger(%s) || %s < 0 || %s > %d) { // headroom above the wire range cannot ride\n",
		ind, name, name, name, ref.Max)
	g.pf("%s  return false;\n%s}\n", ind, ind)
	// A degenerate range (an enum with only the implicit None) costs ZERO
	// BITS — the value is known from the range alone. The bit packer requires
	// at least one bit, so the call is skipped entirely; the guard above
	// still rides, because a value outside the range must still be refused.
	if bits > 0 {
		scratch := g.numScratch()
		g.pf("%s%s.value = %s;\n", ind, scratch, name)
		g.call(ind, fmt.Sprintf("stream.serializeBits(%s, %d)", scratch, bits), "")
	}
}

// emitWriteBareInt writes a bare integer at its storage width. Signed Number
// values mask through the same-width unsigned domain first (the sign
// extension a wider write would smear corrupts neighboring wire data,
// exactly as in C++); signed BigInt values wrap inside the runtime's own
// BigInt.asUintN, so the 64-bit call takes storage directly.
func (g *gen) emitWriteBareInt(f *ir.Field, name, ind string) {
	if f.Type.Width == 64 {
		scratch := g.bigScratch()
		g.pf("%s%s.value = %s;\n", ind, scratch, name)
		g.call(ind, fmt.Sprintf("stream.serializeBits64(%s, 64)", scratch), "")
		return
	}
	scratch := g.numScratch()
	g.pf("%s%s.value = %s;\n", ind, scratch, fmtNumCast(f, name))
	g.call(ind, fmt.Sprintf("stream.serializeBits(%s, %d)", scratch, f.Type.Width), "")
}

// fmtNumCast renders the value-to-unsigned conversion for a 32-bit-or-less
// bare integer: signed values reduce to the same-width unsigned bit pattern.
func fmtNumCast(f *ir.Field, name string) string {
	if !f.Type.Signed {
		return name
	}
	switch f.Type.Width {
	case 8:
		return name + " & 0xff"
	case 16:
		return name + " & 0xffff"
	}
	return name + " >>> 0"
}

func (g *gen) emitReadField(f *ir.Field, ind string) {
	name := "value." + ir.GoExportName(f.Name)
	if f.Array != ir.ArrayNone {
		bound := g.renderNum(f.ArrayExpr, big.NewInt(f.ArrayBound))
		if g.bulkBytes[f] {
			// the write twin's bulk path exactly: zero align bits consumed
			// here, then a bulk copy into the N bytes (see emitWriteField)
			g.call(ind, fmt.Sprintf("stream.serializeBytes(%s.subarray(0, %s))", name, bound),
				" // byte-aligned [N]uint8 — bulk copy, wire-identical to the per-byte loop")
			return
		}
		if f.Array == ir.ArrayCounted {
			count := name + "Count"
			scratch := g.numScratch()
			g.call(ind, fmt.Sprintf("stream.serializeInt(%s, %d, %s)", scratch, f.ArrayMin, bound),
				" // the count guards the loop (§6.3)")
			g.pf("%s%s = %s.value;\n", ind, count, scratch)
			g.pf("%sfor (let i = 0; i < %s; i++) {\n", ind, count)
		} else {
			// the SCHEMA bound, never the storage's length (see the write twin)
			g.pf("%sfor (let i = 0; i < %s; i++) {\n", ind, bound)
		}
		g.emitReadScalar(f, name+"[i]", ind+"  ")
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
			g.pf("%s%s = %s;\n", ind, name, g.rawLit(f.Type, rawMin))
			return
		}
		// the runtime validates the raw offset against the raw bounds and
		// rejects — never clamps — latching ValueOutOfRange on a hostile
		// stream; the bool is checked BEFORE the holder is consumed
		scratch, lo, hi := g.numScratch(), "", ""
		if f.Type.Width > 32 {
			scratch = g.bigScratch()
			lo, hi = g.rangeArgsBig(f)
		} else {
			lo, hi = g.rangeArgsNum(f)
		}
		g.call(ind, fmt.Sprintf("stream.serializeFixed(%s, %d, %d, %s, %s)", scratch, f.Type.IntBits, f.Type.FracBits, lo, hi), "")
		g.pf("%s%s = %s.value;\n", ind, name, scratch)
	case ir.TInt:
		if f.Type.Width == 128 {
			scratch := g.bigScratch()
			if f.HasIntRange {
				if f.IntMin.Cmp(f.IntMax) == 0 {
					// degenerate range: zero bits — materialize (SPEC §4.6)
					g.pf("%s%s = %s;\n", ind, name, bigLit(f.IntMin))
					return
				}
				// rejects a decoded offset beyond max - min (reject, never clamp)
				g.call(ind, fmt.Sprintf("stream.serializeInt128(%s, %s, %s)", scratch, bigLit(f.IntMin), bigLit(f.IntMax)), "")
				g.pf("%s%s = %s.value;\n", ind, name, scratch)
			} else {
				g.call(ind, fmt.Sprintf("stream.serializeUint128(%s)", scratch), "")
				g.pf("%s%s = %s.value;\n", ind, name, scratch)
			}
			return
		}
		if f.HasIntRange {
			if f.IntMin.Cmp(f.IntMax) == 0 {
				// degenerate range: zero bits — the value is the range,
				// materialized with no wire call (SPEC §4.6)
				if f.Type.Width > 32 {
					g.pf("%s%s = %s;\n", ind, name, bigLit(f.IntMin))
				} else {
					g.pf("%s%s = %s;\n", ind, name, f.IntMin.String())
				}
				return
			}
			switch intRangePath(f.IntMin, f.IntMax) {
			case "int32":
				lo, hi := g.rangeArgsNum(f)
				scratch := g.numScratch()
				g.call(ind, fmt.Sprintf("stream.serializeInt(%s, %s, %s)", scratch, lo, hi), "")
				if f.Type.Width > 32 {
					g.pf("%s%s = BigInt(%s.value);\n", ind, name, scratch)
				} else {
					g.pf("%s%s = %s.value;\n", ind, name, scratch)
				}
			case "int64":
				lo, hi := g.rangeArgsBig(f)
				scratch := g.bigScratch()
				g.call(ind, fmt.Sprintf("stream.serializeInt64(%s, %s, %s)", scratch, lo, hi), "")
				if f.Type.Width > 32 {
					g.pf("%s%s = %s.value;\n", ind, name, scratch)
				} else {
					// uint32 storage whose range escapes int32: the decoded
					// value is inside | min, max, so the narrowing is exact
					g.pf("%s%s = Number(%s.value);\n", ind, name, scratch)
				}
			default:
				scratch := g.bigScratch()
				diff := new(big.Int).Sub(f.IntMax, f.IntMin)
				g.call(ind, fmt.Sprintf("stream.serializeBits64(%s, %d)", scratch, ir.BitsRequired(f.IntMin, f.IntMax)), "")
				if diff.Cmp(maxUint64) != 0 {
					// a full-width diff cannot overflow its own read — elided
					g.pf("%sif (%s.value > %s) { // a read rejects out-of-range (SPEC §5) — not latched\n", ind, scratch, bigLit(diff))
					g.pf("%s  return false;\n%s}\n", ind, ind)
				}
				if f.IntMin.Sign() == 0 {
					g.pf("%s%s = %s.value;\n", ind, name, scratch)
				} else {
					g.pf("%s%s = %s.value + %s;\n", ind, name, scratch, bigLit(f.IntMin))
				}
			}
			return
		}
		g.emitReadBareInt(f, name, ind)
	case ir.TBits:
		if f.Type.Width <= 32 {
			scratch := g.numScratch()
			g.call(ind, fmt.Sprintf("stream.serializeBits(%s, %d)", scratch, f.Type.Width), "")
			g.pf("%s%s = %s.value;\n", ind, name, scratch)
		} else {
			scratch := g.bigScratch()
			g.call(ind, fmt.Sprintf("stream.serializeBits64(%s, %d)", scratch, f.Type.Width), "")
			g.pf("%s%s = %s.value;\n", ind, name, scratch)
		}
	case ir.TBool:
		scratch := g.boolScratch()
		g.call(ind, fmt.Sprintf("stream.serializeBool(%s)", scratch), "")
		g.pf("%s%s = %s.value;\n", ind, name, scratch)
	case ir.TFloat32:
		scratch := g.numScratch()
		if f.HasFloatRange {
			g.call(ind, fmt.Sprintf("stream.serializeCompressedFloat(%s, %s, %s, %s)",
				scratch, formatFloat32(f.FMin), formatFloat32(f.FMax), formatFloat32(f.Resolution)), "")
			g.pf("%s%s = %s.value;\n", ind, name, scratch)
			return
		}
		g.call(ind, fmt.Sprintf("stream.serializeFloat(%s)", scratch), "")
		g.pf("%s%s = %s.value;\n", ind, name, scratch)
	case ir.TFloat64:
		scratch := g.numScratch()
		g.call(ind, fmt.Sprintf("stream.serializeDouble(%s)", scratch), "")
		g.pf("%s%s = %s.value;\n", ind, name, scratch)
	case ir.TString, ir.TBytes:
		// the bool is checked BEFORE the slice: a hostile length never
		// reaches subarray (a successful ranged read guarantees [0, N])
		length := name + "Length"
		scratch := g.numScratch()
		g.call(ind, fmt.Sprintf("stream.serializeInt(%s, 0, %s)",
			scratch, g.renderNum(f.Type.SizeExpr, big.NewInt(f.Type.Size))),
			" // the length guards the slice (§6.3)")
		g.pf("%s%s = %s.value;\n", ind, length, scratch)
		g.call(ind, fmt.Sprintf("stream.serializeBytes(%s.subarray(0, %s))", name, length), "")
		if f.Type.Kind == ir.TString {
			// the interior-null rule is generated-code validation (SPEC §4.7);
			// the serializeBytes bool above already surfaced a truncated
			// stream as the stream's own latched error, so this verdict only
			// ever judges bytes that arrived — false, nothing latched
			g.pf("%sfor (let i = 0; i < %s; i++) {\n", ind, length)
			g.pf("%s  if (%s[i] === 0) { // an interior null is content the read refuses (SPEC §4.7)\n", ind, name)
			g.pf("%s    return false;\n%s  }\n%s}\n", ind, ind, ind)
		}
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			scratch := g.numScratch()
			g.call(ind, fmt.Sprintf("stream.serializeInt(%s, 0, %d)", scratch, ref.Max), "")
			g.pf("%s%s = %s.value;\n", ind, name, scratch)
		case *ir.Flags:
			g.emitReadFlags(name, ref.WireBits, ind)
		case *ir.Struct:
			g.addRef(f.Type.Name, "Read"+f.Type.Name)
			g.call(ind, fmt.Sprintf("Read%s(stream, %s)", f.Type.Name, name), "")
		case *ir.Union:
			g.addRef(f.Type.Name, "Read"+f.Type.Name)
			g.call(ind, fmt.Sprintf("Read%s(stream, %s)", f.Type.Name, name), "")
		}
	}
}

// emitReadBareInt reads a bare integer at its storage width, recovering the
// sign: int32 through | 0, narrower signed widths through the shift pair
// (the same-width-unsigned round trip), int64 through BigInt.asIntN.
func (g *gen) emitReadBareInt(f *ir.Field, name, ind string) {
	if f.Type.Width == 64 {
		scratch := g.bigScratch()
		g.call(ind, fmt.Sprintf("stream.serializeBits64(%s, 64)", scratch), "")
		if f.Type.Signed {
			g.pf("%s%s = BigInt.asIntN(64, %s.value);\n", ind, name, scratch)
		} else {
			g.pf("%s%s = %s.value;\n", ind, name, scratch)
		}
		return
	}
	scratch := g.numScratch()
	g.call(ind, fmt.Sprintf("stream.serializeBits(%s, %d)", scratch, f.Type.Width), "")
	switch {
	case f.Type.Signed && f.Type.Width == 32:
		g.pf("%s%s = %s.value | 0;\n", ind, name, scratch)
	case f.Type.Signed:
		// back through the same-width unsigned so the sign bit lands right
		shift := 32 - f.Type.Width
		g.pf("%s%s = (%s.value << %d) >> %d;\n", ind, name, scratch, shift, shift)
	default:
		g.pf("%s%s = %s.value;\n", ind, name, scratch)
	}
}

// emitReadFlags reads a flags value into the BigInt (uint64) storage; at 32
// wire bits or fewer the Number read widens through BigInt().
func (g *gen) emitReadFlags(name string, wireBits int, ind string) {
	if wireBits <= 32 {
		scratch := g.numScratch()
		g.call(ind, fmt.Sprintf("stream.serializeBits(%s, %d)", scratch, wireBits), "")
		g.pf("%s%s = BigInt(%s.value);\n", ind, name, scratch)
		return
	}
	scratch := g.bigScratch()
	g.call(ind, fmt.Sprintf("stream.serializeBits64(%s, %d)", scratch, wireBits), "")
	g.pf("%s%s = %s.value;\n", ind, name, scratch)
}

// emitWriteFlagsValue is the write half used by emitWriteScalar. Storage is
// uint64 (BigInt) and wider than the wire wherever WireBits < 64, so a mask
// bit above the wire width is refused rather than silently truncated — the
// raw bit calls mask, so this refusal is generated (nothing latches; bool
// is the verdict). BigInt.asUintN(64, ...) first is the exact translation
// of C#'s ulong storage domain: a negative misuse value reinterprets to its
// huge two's-complement pattern and the guard refuses it.
func (g *gen) emitWriteFlagsValue(name string, wireBits int, ind string) {
	g.pf("%s{\n%s  const flagsValue = BigInt.asUintN(64, %s);\n", ind, ind, name)
	if wireBits < 64 {
		g.pf("%s  if (flagsValue >= 1n << %dn) { // a mask bit above the wire width cannot ride\n", ind, wireBits)
		g.pf("%s    return false;\n%s  }\n", ind, ind)
	}
	if wireBits <= 32 {
		scratch := g.numScratch()
		g.pf("%s  %s.value = Number(flagsValue);\n", ind, scratch)
		g.call(ind+"  ", fmt.Sprintf("stream.serializeBits(%s, %d)", scratch, wireBits), "")
	} else {
		scratch := g.bigScratch()
		g.pf("%s  %s.value = flagsValue;\n", ind, scratch)
		g.call(ind+"  ", fmt.Sprintf("stream.serializeBits64(%s, %d)", scratch, wireBits), "")
	}
	g.pf("%s}\n", ind)
}
