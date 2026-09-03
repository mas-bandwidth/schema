// The flat word codec: the C# target's chunked wire form.
//
// The per-field form asks the stream to place one field at a time. In C# the
// call itself is free — the JitDisasm audit of the bench shape found ZERO
// direct calls into serialize.cs from any generated method; every entry point
// the generated code touches inlines. What is not free is what each of those
// inlined bodies does: BitWriter's packer step masks, shifts, ORs into the
// scratch word, tests `newScratchBits >= 64`, conditionally stores a qword and
// recovers the spill — a data-dependent branch and a store PER FIELD, against
// state the JIT can only keep in registers inside a batch.
//
// The flat form removes the per-field dependence the same way the Rust (#183)
// and Go (#198) ports did. Every bit offset INSIDE a message is a
// generation-time constant regardless of where the message starts, so the
// emitter folds the field placement itself: field values are computed into
// ulong locals, OR'd into word-sized chunk locals at literal shifts, and
// handed to the stream one whole chunk at a time. A run of B bits costs
// ceil(B/64) packer steps instead of one per field, and what remains is
// register arithmetic with literal shifts and literal masks.
//
// THREE INVARIANTS, RE-DERIVED FOR C# RATHER THAN INHERITED. #198's review
// record is the reason this list is spelled out: the Go port shipped silent
// wire corruption by carrying a Rust fallback across without re-deriving what
// made it safe.
//
//  1. CHUNKS ARE 64 BITS. serialize.cs's SerializeBits64 splits a wide value
//     low-dword-first-then-remainder, which IS the 32-bit chunk order, so a
//     64-bit chunk lands exactly where two 32-bit chunks would have. It costs
//     one bounds check (Debug.Assert on the write path) and two packer steps
//     where two SerializeBits calls cost two of each. A chunk of 32 bits or
//     fewer goes through SerializeBits.
//
//  2. EVERY PIECE IS MASKED TO ITS WIDTH. BitWriter.WriteBitsUnchecked opens
//     with `value &= (uint)((1UL << bits) - 1)` — the per-field form TRUNCATES
//     a too-wide value rather than refusing it. The flat form must mask too,
//     or a value with bits above its field width would corrupt its neighbors
//     in the chunk instead of being truncated. This is the invariant #198
//     lost; it is enforced here at the single place a piece's value
//     expression is built (flatMasked), never at the call sites.
//
//  3. WRITE REFUSALS COME FIRST, IN DECLARATION ORDER. The set of refused
//     values and the verdict each produces are exactly the per-field form's —
//     a generated guard returning false without latching, the family's own
//     form. What moves is how many bits reached the stream before the
//     refusal: a refused write leaves a partial message either way, and the
//     run has simply not started packing yet.
//
// A run breaks at every construct whose width or content is not a
// generation-time constant — align, string/bytes, arrays, branches, nested
// struct and union calls — and at the float, fixed-point, compressed-float
// and 128-bit families, whose value arithmetic lives in the runtime. Those
// items keep the per-field form and a fresh run opens after them.
//
// A run that would not REDUCE the packer-step count is not flattened
// (flatWorthwhile): a body of whole-chunk fields packs into as many chunks as
// it has fields, so flattening would add a materialized local per field and
// remove nothing.
//
// The read half follows the write half in this file, with its own header:
// what it fuses, the two consequences that fusion always has, and the one
// class of item it will NOT absorb.
package csharp

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// flatChunkBits is the word the stream sees — see invariant 1.
const flatChunkBits = 64

// flatMaxRunBits caps one flat run. Past it the run closes and a fresh one
// opens at the next item boundary: a run holds every one of its field values
// live across its chunk assembly, and an unbounded run on a hundred-field
// message spills them to the stack, which is the cost the form exists to
// avoid.
//
// 384 is the Go port's measured peak, carried here UNMEASURED-BY-EVIDENCE:
// swept on the bench shape at 128 / 256 / 512 the C# numbers are flat
// (write 3.30 / 3.32 / 3.30, round_trip 1.18 / 1.18 / 1.19 M msg/s), because
// this corpus's longest run is MixedEntity's 135 bits and no cap in that
// range binds it. The value is a placeholder for a corpus that does reach
// it — a schema with a 400-bit scalar body is what would move it, and the
// number here is Go's answer to that question, not C#'s.
const flatMaxRunBits = 384

// flatPiece is one item's contribution to a run. The item rides along so a
// run that does not pay can be re-emitted through the per-field path ITEM by
// item — the property #198 lost when it fell back over pieces instead.
type flatPiece struct {
	item  ir.Item
	bits  int64
	guard func(ind string) // write-side refusals and headroom guards; may be nil
	expr  string           // ulong-valued, already masked to bits; "" when bits == 0

	// read emits the read-side validation and field store. src names the ulong
	// local holding the extracted bits; a zero-bit piece is handed the literal
	// "0UL", the only value its range can carry (SPEC §4.6).
	read func(ind, src string)
}

// flatChunkWidths splits a run of `bits` wire bits into the chunks the stream
// sees. The widths sum to `bits` EXACTLY, which is what makes the flat form
// write precisely the bits the per-field form wrote.
func flatChunkWidths(bits int64) []int64 {
	var out []int64
	for left := bits; left > 0; left -= flatChunkBits {
		if left >= flatChunkBits {
			out = append(out, flatChunkBits)
			continue
		}
		out = append(out, left)
	}
	return out
}

// flatMaskLit renders the low-`bits` mask as a C# ulong literal, or "" at 64
// bits where no mask is needed.
func flatMaskLit(bits int64) string {
	if bits >= 64 {
		return ""
	}
	m := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))
	return fmt.Sprintf("0x%xUL", m)
}

// flatMasked wraps a ulong-valued expression in its width mask — invariant 2,
// enforced in one place.
func flatMasked(expr string, bits int64) string {
	m := flatMaskLit(bits)
	if m == "" {
		return expr
	}
	return "(" + expr + ") & " + m
}

func flatShiftLeft(expr string, n int64) string {
	if n == 0 {
		return expr
	}
	return fmt.Sprintf("(%s << %d)", expr, n)
}

func flatShiftRight(expr string, n int64) string {
	if n == 0 {
		return expr
	}
	return fmt.Sprintf("(%s >> %d)", expr, n)
}

// flatWorthwhile is the policy: flatten only where it REDUCES the number of
// packer steps, which is the entire cost the form exists to remove.
func flatWorthwhile(pieces []flatPiece, bits int64) bool {
	n := 0
	for _, p := range pieces {
		if p.bits > 0 {
			n++
		}
	}
	if n < 2 {
		return false
	}
	return len(flatChunkWidths(bits)) < n
}

// emitFlatWriteRun packs the run's values into chunk words at literal shifts
// and hands each whole chunk to the stream. The whole run lives in a block so
// its locals are scoped to it and sibling runs may reuse the names.
func (g *gen) emitFlatWriteRun(pieces []flatPiece, bits int64, ind string) {
	g.sf("%s{\n", ind)
	inner := ind + "    "
	g.sf("%s// flat run: %d bits in %d chunk(s) — the field placement is folded\n",
		inner, bits, len(flatChunkWidths(bits)))

	// invariant 3: every refusal first, in declaration order
	for _, p := range pieces {
		if p.guard != nil {
			p.guard(inner)
		}
	}

	offsets := make([]int64, len(pieces))
	var at int64
	for i, p := range pieces {
		offsets[i] = at
		if p.bits == 0 {
			continue
		}
		g.sf("%sulong f%d = %s;\n", inner, i, p.expr)
		at += p.bits
	}

	for j, width := range flatChunkWidths(bits) {
		lo := int64(j) * flatChunkBits
		hi := lo + width
		var terms []string
		for i, p := range pieces {
			if p.bits == 0 {
				continue
			}
			o := offsets[i]
			if o >= hi || o+p.bits <= lo {
				continue
			}
			if o >= lo {
				terms = append(terms, flatShiftLeft(fmt.Sprintf("f%d", i), o-lo))
			} else {
				terms = append(terms, flatShiftRight(fmt.Sprintf("f%d", i), lo-o))
			}
		}
		packed := strings.Join(terms, " | ")
		if width <= 32 {
			g.sf("%suint w%d = (uint)(%s);\n", inner, j, packed)
			g.call(inner, fmt.Sprintf("%s.SerializeBits(ref w%d, %d)", g.rv(), j, width), "")
			continue
		}
		g.sf("%sulong w%d = %s;\n", inner, j, packed)
		g.call(inner, fmt.Sprintf("%s.SerializeBits64(ref w%d, %d)", g.rv(), j, width), "")
	}
	g.sf("%s}\n", ind)
}

// ---- classification --------------------------------------------------------

// flatWritePieceOf classifies one item into its contribution to a write run,
// or reports false — which closes the run and sends the item down the
// per-field path.
func (g *gen) flatWritePieceOf(item ir.Item) (flatPiece, bool) {
	switch item := item.(type) {
	case *ir.ConstItem:
		if item.Bits < 1 || item.Bits > 64 {
			return flatPiece{}, false
		}
		return flatPiece{item: item, bits: item.Bits,
			expr: flatMasked(item.Value.String()+"UL", item.Bits)}, true
	case *ir.ReservedItem:
		if item.Bits < 1 || item.Bits > 64 {
			return flatPiece{}, false
		}
		return flatPiece{item: item, bits: item.Bits, expr: "0UL"}, true
	case *ir.FieldItem:
		return g.flatWriteFieldPiece(item)
	}
	return flatPiece{}, false // align, branch: not statically sized here
}

func (g *gen) flatWriteFieldPiece(item *ir.FieldItem) (flatPiece, bool) {
	f := item.F
	if f.Array != ir.ArrayNone {
		return flatPiece{}, false // arrays keep their loop (v1)
	}
	name := "value." + g.fieldBase(f)
	switch f.Type.Kind {
	case ir.TBool:
		return flatPiece{item: item, bits: 1, expr: fmt.Sprintf("%s ? 1UL : 0UL", name)}, true

	case ir.TBits:
		w := int64(f.Type.Width)
		if w < 1 || w > 64 {
			return flatPiece{}, false
		}
		return flatPiece{item: item, bits: w,
			expr: flatMasked("(ulong)"+name, w)}, true

	case ir.TInt:
		if f.Type.Width > 64 {
			return flatPiece{}, false // the 128-bit family lives in the runtime
		}
		if !f.HasIntRange {
			return g.flatWriteBarePiece(item, name)
		}
		return g.flatWriteRangedPiece(item, name)

	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			return g.flatWriteEnumPiece(item, ref, name)
		case *ir.Flags:
			w := int64(ref.WireBits)
			if w < 1 || w > 64 {
				return flatPiece{}, false
			}
			return flatPiece{item: item, bits: w,
				guard: func(ind string) {
					g.sf("%sif (%s >= 1ul << %d) // a mask bit above the wire width cannot ride\n", ind, name, w)
					g.sf("%s{\n%s    return false;\n%s}\n", ind, ind, ind)
				},
				expr: flatMasked(name, w)}, true
		}
	}
	return flatPiece{}, false
}

// flatWriteBarePiece is a bare integer at its storage width. Signed values
// cast through the same-width unsigned first — a sign extension into the
// chunk word would corrupt its neighbors, exactly as it would in C++.
func (g *gen) flatWriteBarePiece(item *ir.FieldItem, name string) (flatPiece, bool) {
	f := item.F
	w := int64(f.Type.Width)
	if w < 1 || w > 64 {
		return flatPiece{}, false
	}
	// at 64 the storage IS the chunk word's type, so no narrowing cast and no
	// mask; below it the value narrows through fmt32Cast and masks to width
	expr := flatMasked("(ulong)("+fmt32Cast(f, name)+")", w)
	if w == 64 {
		expr = "(ulong)" + name
	}
	return flatPiece{item: item, bits: w, expr: expr}, true
}

// flatWriteRangedPiece is a ranged integer: the offset from min in a
// generation-time bit count, with the same refusal emitWriteFoldedRange
// emits — the same guard, the same vacuous halves elided, the same
// non-latching false.
func (g *gen) flatWriteRangedPiece(item *ir.FieldItem, name string) (flatPiece, bool) {
	f := item.F
	bits := ir.BitsRequired(f.IntMin, f.IntMax)
	if bits > 64 {
		return flatPiece{}, false
	}
	path := intRangePath(f.IntMin, f.IntMax)
	var guardLo, guardHi bool
	var lo, hi, cast string
	if path == "bits64" {
		// full-range unsigned: ulong storage only, bounds in the ulong domain
		guardLo = f.IntMin.Sign() != 0
		guardHi = f.IntMax.Cmp(maxUint64) != 0
		lo, hi = g.rangeArgs(f, "ulong")
		cast = "ulong"
	} else {
		sMin, sMax := storageBounds(f.Type)
		guardLo = f.IntMin.Cmp(sMin) > 0
		guardHi = f.IntMax.Cmp(sMax) < 0
		typ := "int"
		cast = "uint"
		if path != "int32" {
			typ, cast = "long", "ulong"
			if !f.Type.Signed && f.Type.Width == 64 {
				typ = "ulong"
			}
		}
		lo, hi = g.rangeArgs(f, typ)
	}
	guard := func(ind string) {
		switch {
		case guardLo && guardHi:
			g.sf("%sif (%s < %s || %s > %s)\n%s{\n%s    return false;\n%s}\n", ind, name, lo, name, hi, ind, ind, ind)
		case guardLo:
			g.sf("%sif (%s < %s)\n%s{\n%s    return false;\n%s}\n", ind, name, lo, ind, ind, ind)
		case guardHi:
			g.sf("%sif (%s > %s)\n%s{\n%s    return false;\n%s}\n", ind, name, hi, ind, ind, ind)
		}
	}
	if bits == 0 {
		// a degenerate range costs ZERO BITS — the refusal is the whole write
		return flatPiece{item: item, bits: 0, guard: guard}, true
	}
	loCast := fmt.Sprintf("(%s)(%s)", cast, lo)
	if f.IntMin.Sign() < 0 {
		loCast = fmt.Sprintf("unchecked((%s)(%s))", cast, lo)
	}
	offset := fmt.Sprintf("(%s)(%s)", cast, name)
	if f.IntMin.Sign() != 0 {
		offset = fmt.Sprintf("(%s)(%s) - %s", cast, name, loCast)
	}
	return flatPiece{item: item, bits: bits, guard: guard,
		expr: flatMasked("(ulong)("+offset+")", bits)}, true
}

// flatWriteEnumPiece is an enum in [0, Max] with a generation-time bit count,
// carrying the headroom guard the per-field form carries.
func (g *gen) flatWriteEnumPiece(item *ir.FieldItem, ref *ir.Enum, name string) (flatPiece, bool) {
	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max))
	if bits > 64 {
		return flatPiece{}, false
	}
	typ := "uint"
	if ref.StorageBits > 32 {
		typ = "ulong"
	}
	var guard func(ind string)
	if big.NewInt(ref.Max).Cmp(new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(ref.StorageBits)), big.NewInt(1))) < 0 {
		guard = func(ind string) {
			g.sf("%sif ((%s)%s > %d) // headroom above the wire range cannot ride\n", ind, typ, name, ref.Max)
			g.sf("%s{\n%s    return false;\n%s}\n", ind, ind, ind)
		}
	}
	if bits == 0 {
		return flatPiece{item: item, bits: 0, guard: guard}, true
	}
	return flatPiece{item: item, bits: bits, guard: guard,
		expr: flatMasked(fmt.Sprintf("(ulong)(%s)%s", typ, name), bits)}, true
}

// ---- the read half ---------------------------------------------------------
//
// The read run fuses what the per-field form paid per field: ONE bounds check
// per chunk instead of one per field, and ONE sticky-error test for the whole
// run. Reading every chunk before the error test is safe and is the point — a
// failed chunk latches the stream's error and every later chunk read returns
// on it immediately, leaving its destination at zero.
//
// TWO CONSEQUENCES, both named rather than discovered later:
//
//   - A stream that is BOTH truncated inside a run AND carries, before the
//     truncation, a value the run's own validation refuses — a wrong const,
//     a nonzero reserved, an out-of-range value — now surfaces the stream's
//     overflow error where the per-field form surfaced that refusal.
//     Both refuse the packet; the set of ACCEPTED streams is unchanged, which
//     is the property that matters at the trust boundary. Java, Dart, JS-flat,
//     Rust and Go already carry this consequence.
//
//   - A failed run leaves the destination object untouched where the
//     per-field form left the fields before the failure updated. Both are
//     partial states a failed read gives no contract over (SPEC §5).
//
// WHAT A READ RUN WILL NOT ABSORB, and why. A ranged read whose range check
// can actually fire goes through serialize.cs's SerializeInt/SerializeInt64,
// which LATCH SerializeError.ValueOutOfRange; a generated comparison returns
// false without latching, and cs publishes checks=always with test/cs pinning
// that latch. So a ranged piece joins a read run only where its check is
// VACUOUS — max - min is exactly 2^bits - 1, so no value the bits can hold is
// out of range — or where the emitter's own read path ALREADY refuses without
// latching (the full-range unsigned bits64 path, which has always carried a
// generated refusal). Everything else closes the run and keeps the runtime
// call. Folding the rest waits on a public refusal-latch on
// ReadStream/ReadBatch — a serialize.cs change.

// flatVacuousRange reports whether a ranged read's refusal can never fire:
// the offset domain [0, max-min] fills the bit width exactly.
func flatVacuousRange(min, max *big.Int) bool {
	bits := ir.BitsRequired(min, max)
	if bits == 0 {
		return true // a degenerate range rides no bits and reads no value
	}
	diff := new(big.Int).Sub(max, min)
	full := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))
	return diff.Cmp(full) == 0
}

// emitFlatReadRun reads the run's chunks, tests the latch once, then unpacks,
// validates and stores each value with literal shifts and masks.
func (g *gen) emitFlatReadRun(pieces []flatPiece, bits int64, ind string) {
	g.sf("%s{\n", ind)
	inner := ind + "    "
	widths := flatChunkWidths(bits)
	g.sf("%s// flat run: %d bits in %d chunk(s) — one bounds check per chunk,\n", inner, bits, len(widths))
	g.sf("%s// one sticky-error test for the whole run\n", inner)
	for j, width := range widths {
		g.sf("%sulong c%d = 0;\n", inner, j)
		g.sf("%s%s.SerializeBits64(ref c%d, %d);\n", inner, g.rv(), j, width)
	}
	g.sf("%sif (!%s.Ok)\n%s{\n%s    return false;\n%s}\n", inner, g.rv(), inner, inner, inner)

	var at int64
	for i, p := range pieces {
		if p.bits == 0 {
			p.read(inner, "0UL")
			continue
		}
		o := at
		at += p.bits
		first := o / flatChunkBits
		last := (o + p.bits - 1) / flatChunkBits
		var terms []string
		for j := first; j <= last; j++ {
			lo := j * flatChunkBits
			if lo <= o {
				terms = append(terms, flatShiftRight(fmt.Sprintf("c%d", j), o-lo))
			} else {
				terms = append(terms, fmt.Sprintf("(c%d << %d)", j, lo-o))
			}
		}
		expr := strings.Join(terms, " | ")
		if len(terms) > 1 {
			expr = "(" + expr + ")"
		}
		if m := flatMaskLit(p.bits); m != "" {
			expr = expr + " & " + m
		}
		g.sf("%sulong v%d = %s;\n", inner, i, expr)
		p.read(inner, fmt.Sprintf("v%d", i))
	}
	g.sf("%s}\n", ind)
}

// flatReadPieceOf classifies one item into its contribution to a read run, or
// reports false — which closes the run and sends the item down the per-field
// path.
func (g *gen) flatReadPieceOf(item ir.Item) (flatPiece, bool) {
	switch item := item.(type) {
	case *ir.ConstItem:
		if item.Bits < 1 || item.Bits > 64 {
			return flatPiece{}, false
		}
		lit := item.Value.String() + "UL"
		return flatPiece{item: item, bits: item.Bits, read: func(ind, src string) {
			g.sf("%sif (%s != %s) // const(%s, %d): a read rejects any other value (SPEC §4.3)\n",
				ind, src, lit, item.Value.String(), item.Bits)
			g.sf("%s{\n%s    return false;\n%s}\n", ind, ind, ind)
		}}, true
	case *ir.ReservedItem:
		if item.Bits < 1 || item.Bits > 64 {
			return flatPiece{}, false
		}
		return flatPiece{item: item, bits: item.Bits, read: func(ind, src string) {
			g.sf("%sif (%s != 0UL) // reserved(%d): a read rejects nonzero (SPEC §4.3)\n", ind, src, item.Bits)
			g.sf("%s{\n%s    return false;\n%s}\n", ind, ind, ind)
		}}, true
	case *ir.FieldItem:
		return g.flatReadFieldPiece(item)
	}
	return flatPiece{}, false
}

func (g *gen) flatReadFieldPiece(item *ir.FieldItem) (flatPiece, bool) {
	f := item.F
	if f.Array != ir.ArrayNone {
		return flatPiece{}, false
	}
	name := "value." + g.fieldBase(f)
	switch f.Type.Kind {
	case ir.TBool:
		return flatPiece{item: item, bits: 1, read: func(ind, src string) {
			g.sf("%s%s = %s != 0UL;\n", ind, name, src)
		}}, true

	case ir.TBits:
		w := int64(f.Type.Width)
		if w < 1 || w > 64 {
			return flatPiece{}, false
		}
		return flatPiece{item: item, bits: w, read: func(ind, src string) {
			if w <= 32 {
				g.sf("%s%s = (uint)%s;\n", ind, name, src)
				return
			}
			g.sf("%s%s = %s;\n", ind, name, src)
		}}, true

	case ir.TInt:
		if f.Type.Width > 64 {
			return flatPiece{}, false
		}
		if !f.HasIntRange {
			return g.flatReadBarePiece(item, name)
		}
		return g.flatReadRangedPiece(item, name)

	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			bits := ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max))
			// the runtime's ranged read is the refuser for a non-vacuous enum
			// range, and it latches; leave those on the call
			if bits < 1 || bits > 64 ||
				big.NewInt(ref.Max).Cmp(new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))) != 0 {
				return flatPiece{}, false
			}
			return flatPiece{item: item, bits: bits, read: func(ind, src string) {
				g.sf("%s%s = (%s)(int)%s;\n", ind, name, f.Type.Name, src)
			}}, true
		case *ir.Flags:
			w := int64(ref.WireBits)
			if w < 1 || w > 64 {
				return flatPiece{}, false
			}
			return flatPiece{item: item, bits: w, read: func(ind, src string) {
				g.sf("%s%s = %s;\n", ind, name, src)
			}}, true
		}
	}
	return flatPiece{}, false
}

func (g *gen) flatReadBarePiece(item *ir.FieldItem, name string) (flatPiece, bool) {
	f := item.F
	w := int64(f.Type.Width)
	if w < 1 || w > 64 {
		return flatPiece{}, false
	}
	typ := g.csFieldType(f.Type)
	return flatPiece{item: item, bits: w, read: func(ind, src string) {
		switch {
		case w == 64 && f.Type.Signed:
			g.sf("%s%s = (long)%s;\n", ind, name, src)
		case w == 64:
			g.sf("%s%s = %s;\n", ind, name, src)
		case f.Type.Signed && w < 32:
			// back through the same-width unsigned so the sign bit lands right
			g.sf("%s%s = (%s)(%s)(uint)%s;\n", ind, name, csInt(f.Type.Width), csUint(f.Type.Width), src)
		default:
			g.sf("%s%s = (%s)(uint)%s;\n", ind, name, typ, src)
		}
	}}, true
}

// flatReadRangedPiece folds a ranged read into the run only where the refusal
// cannot fire, or where the emitter's own read path already refuses without
// latching — see the read half's header.
func (g *gen) flatReadRangedPiece(item *ir.FieldItem, name string) (flatPiece, bool) {
	f := item.F
	bits := ir.BitsRequired(f.IntMin, f.IntMax)
	if bits > 64 {
		return flatPiece{}, false
	}
	typ := g.csFieldType(f.Type)
	if f.IntMin.Cmp(f.IntMax) == 0 {
		// degenerate range: zero bits — the value is the range (SPEC §4.6)
		lit := f.IntMin.String() + "L"
		if !f.Type.Signed && !f.IntMin.IsInt64() {
			lit = f.IntMin.String() + "UL"
		}
		return flatPiece{item: item, bits: 0, read: func(ind, src string) {
			g.sf("%s%s = unchecked((%s)(%s));\n", ind, name, typ, lit)
		}}, true
	}
	switch intRangePath(f.IntMin, f.IntMax) {
	case "int32":
		if !flatVacuousRange(f.IntMin, f.IntMax) {
			return flatPiece{}, false // the runtime is the refuser, and it latches
		}
		lo, _ := g.rangeArgs(f, "int")
		return flatPiece{item: item, bits: bits, read: func(ind, src string) {
			expr := fmt.Sprintf("(int)(uint)%s", src)
			if f.IntMin.Sign() != 0 {
				expr = fmt.Sprintf("(int)((uint)%s + unchecked((uint)(%s)))", src, lo)
			}
			g.sf("%s%s = (%s)(%s);\n", ind, name, typ, expr)
		}}, true
	case "int64":
		if !flatVacuousRange(f.IntMin, f.IntMax) {
			return flatPiece{}, false
		}
		lo, _ := g.rangeArgs(f, "long")
		return flatPiece{item: item, bits: bits, read: func(ind, src string) {
			expr := fmt.Sprintf("(long)%s", src)
			if f.IntMin.Sign() != 0 {
				expr = fmt.Sprintf("(long)(%s + unchecked((ulong)(%s)))", src, lo)
			}
			g.sf("%s%s = (%s)(%s);\n", ind, name, typ, expr)
		}}, true
	default:
		// full-range unsigned: the emitter's own read path already refuses
		// out-of-range WITHOUT latching, so the fold changes nothing
		lo, _ := g.rangeArgs(f, "ulong")
		diff := new(big.Int).Sub(f.IntMax, f.IntMin)
		return flatPiece{item: item, bits: bits, read: func(ind, src string) {
			if diff.Cmp(maxUint64) != 0 {
				g.sf("%sif (%s > %s) // a read rejects out-of-range (SPEC §5) — not latched\n", ind, src, diff.String())
				g.sf("%s{\n%s    return false;\n%s}\n", ind, ind, ind)
			}
			if f.IntMin.Sign() == 0 {
				g.sf("%s%s = %s;\n", ind, name, src)
				return
			}
			g.sf("%s%s = %s + %s;\n", ind, name, src, lo)
		}}, true
	}
}
