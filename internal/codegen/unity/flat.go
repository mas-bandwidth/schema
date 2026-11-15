// The flat word form. Every bit offset inside a run of statically placed items
// is a generation-time constant wherever the run starts, so the placement is
// folded here: each value is computed into a ulong local, OR'd into 64-bit
// chunk words at literal shifts, and handed to the stream a whole chunk at a
// time. A run of B bits costs ceil(B/64) packer steps instead of one per item.
// The wire is the per-item form's, bit for bit; test/unity holds it to the
// C++ pins.
//
// THE THREE INVARIANTS, re-derived for this generator:
//
//  1. CHUNKS ARE 64 BITS. SerializeBits64 writes the low 32 bits and then the
//     rest, which is the order two 32-bit calls would write, so a 64-bit chunk
//     lands exactly where the per-item bits did. A chunk of 32 bits or fewer
//     goes through SerializeBits.
//
//  2. EVERY PIECE IS MASKED TO ITS WIDTH, at the one place its value
//     expression is built (masked). The per-item form's bit writer truncates
//     a too-wide value; unmasked, it would corrupt its neighbours in the chunk.
//
//  3. WRITE CONTRACTS COME FIRST, in declaration order: the same debug
//     assertions the per-item form carries, ahead of the packing.
//
// A run breaks at align, arrays, text, branches, nested calls, and the float,
// compressed-float, fixed-point and 128-bit families, whose arithmetic is the
// runtime's. A run that would not reduce the packer steps is not flattened.
//
// THE READ HALF fuses one bounds check per chunk and one sticky-error test for
// the run. A ranged or enum read whose range check can fire stays on the
// runtime's SerializeInt, which LATCHES ValueOutOfRange; a piece joins a read
// run only where its check is vacuous (the range fills its bit width) or where
// this emitter's own read already refuses without latching (the full-range
// unsigned path). Two consequences, both refusals either way: a stream
// truncated inside a run that also carries a refused value reports the
// truncation, and a failed run leaves the destination as it was.
package unity

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const (
	flatChunkBits = 64
	// flatMaxRunBits caps one run so its live values stay in registers; the
	// cs and Go legs' measured value, unmeasured for Unity.
	flatMaxRunBits = 384
)

// flatPiece is one item's contribution to a run. The item rides along so a
// run that does not pay is re-emitted item by item.
type flatPiece struct {
	item  ir.Item
	bits  int64
	guard func(ind string)      // write contracts; may be nil
	expr  string                // write: ulong value, masked; "" at zero bits
	read  func(ind, src string) // read: validate and store from a ulong local
}

func flatChunkWidths(bits int64) []int64 {
	var out []int64
	for left := bits; left > 0; left -= flatChunkBits {
		out = append(out, min(left, flatChunkBits))
	}
	return out
}

func maskLit(bits int64) string {
	if bits >= 64 {
		return ""
	}
	return fmt.Sprintf("0x%xUL", new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1)))
}

// masked is invariant 2, in one place.
func masked(expr string, bits int64) string {
	if m := maskLit(bits); m != "" {
		return "(" + expr + ") & " + m
	}
	return expr
}

func shifted(expr string, n int64, left bool) string {
	switch {
	case n == 0:
		return expr
	case left:
		return fmt.Sprintf("(%s << %d)", expr, n)
	}
	return fmt.Sprintf("(%s >> %d)", expr, n)
}

func flatWorthwhile(pieces []flatPiece, bits int64) bool {
	n := 0
	for _, p := range pieces {
		if p.bits > 0 {
			n++
		}
	}
	return n >= 2 && len(flatChunkWidths(bits)) < n
}

func (g *gen) emitFlatWriteRun(pieces []flatPiece, bits int64, ind string) {
	widths := flatChunkWidths(bits)
	g.sf("%s{\n", ind)
	in := ind + "    "
	g.sf("%s// %d bits folded into %d word(s)\n", in, bits, len(widths))
	for _, p := range pieces {
		if p.guard != nil {
			p.guard(in)
		}
	}
	offsets := make([]int64, len(pieces))
	var at int64
	for i, p := range pieces {
		offsets[i] = at
		if p.bits > 0 {
			g.sf("%sulong f%d = %s;\n", in, i, p.expr)
			at += p.bits
		}
	}
	for j, width := range widths {
		lo, hi := int64(j)*flatChunkBits, int64(j)*flatChunkBits+width
		var terms []string
		for i, p := range pieces {
			o := offsets[i]
			if p.bits == 0 || o >= hi || o+p.bits <= lo {
				continue
			}
			if o >= lo {
				terms = append(terms, shifted(fmt.Sprintf("f%d", i), o-lo, true))
			} else {
				terms = append(terms, shifted(fmt.Sprintf("f%d", i), lo-o, false))
			}
		}
		packed := strings.Join(terms, " | ")
		if width <= 32 {
			g.sf("%suint w%d = (uint)(%s);\n", in, j, packed)
			g.call(in, fmt.Sprintf("%s.SerializeBits(ref w%d, %d)", g.rv(), j, width), "")
			continue
		}
		g.sf("%sulong w%d = %s;\n", in, j, packed)
		g.call(in, fmt.Sprintf("%s.SerializeBits64(ref w%d, %d)", g.rv(), j, width), "")
	}
	g.sf("%s}\n", ind)
}

func (g *gen) emitFlatReadRun(pieces []flatPiece, bits int64, ind string) {
	widths := flatChunkWidths(bits)
	g.sf("%s{\n", ind)
	in := ind + "    "
	g.sf("%s// %d bits read as %d word(s): one bounds check per word, one error test\n", in, bits, len(widths))
	for j, width := range widths {
		g.sf("%sulong c%d = 0;\n%s%s.SerializeBits64(ref c%d, %d);\n", in, j, in, g.rv(), j, width)
	}
	g.reject(in, "!"+g.rv()+".Ok", "")
	var at int64
	for i, p := range pieces {
		if p.bits == 0 {
			p.read(in, "0UL")
			continue
		}
		o := at
		at += p.bits
		var terms []string
		for j := o / flatChunkBits; j <= (o+p.bits-1)/flatChunkBits; j++ {
			lo := j * flatChunkBits
			if lo <= o {
				terms = append(terms, shifted(fmt.Sprintf("c%d", j), o-lo, false))
			} else {
				terms = append(terms, fmt.Sprintf("(c%d << %d)", j, lo-o))
			}
		}
		expr := strings.Join(terms, " | ")
		if len(terms) > 1 {
			expr = "(" + expr + ")"
		}
		if m := maskLit(p.bits); m != "" {
			expr += " & " + m
		}
		g.sf("%sulong v%d = %s;\n", in, i, expr)
		p.read(in, fmt.Sprintf("v%d", i))
	}
	g.sf("%s}\n", ind)
}

// vacuous reports a range whose offset domain fills its bit width exactly, so
// no value the bits can carry is out of range.
func vacuous(min, max *big.Int) bool {
	bits := ir.BitsRequired(min, max)
	if bits == 0 {
		return true
	}
	full := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))
	return new(big.Int).Sub(max, min).Cmp(full) == 0
}

// flatPieceOf classifies one item for a write or read run, or reports false.
func (g *gen) flatPieceOf(item ir.Item, writing bool, aligned map[*ir.Field]bool) (flatPiece, bool) {
	switch item := item.(type) {
	case *ir.ConstItem:
		if item.Bits < 1 || item.Bits > 64 {
			return flatPiece{}, false
		}
		lit := item.Value.String() + "UL"
		return flatPiece{item: item, bits: item.Bits, expr: masked(lit, item.Bits), read: func(ind, src string) {
			g.reject(ind, src+" != "+lit, fmt.Sprintf(" // const(%s, %d): any other value is refused (SPEC §4.3)", item.Value, item.Bits))
		}}, true
	case *ir.ReservedItem:
		if item.Bits < 1 || item.Bits > 64 {
			return flatPiece{}, false
		}
		return flatPiece{item: item, bits: item.Bits, expr: "0UL", read: func(ind, src string) {
			g.reject(ind, src+" != 0UL", fmt.Sprintf(" // reserved(%d): nonzero is refused (SPEC §4.3)", item.Bits))
		}}, true
	case *ir.FieldItem:
		if item.F.Array != ir.ArrayNone || aligned[item.F] {
			return flatPiece{}, false
		}
		return g.flatFieldPiece(item, writing)
	}
	return flatPiece{}, false
}

func (g *gen) flatFieldPiece(item *ir.FieldItem, writing bool) (flatPiece, bool) {
	f := item.F
	name := "value." + member(f)
	p := flatPiece{item: item}
	switch f.Type.Kind {
	case ir.TBool:
		p.bits = 1
		p.expr = name + " ? 1UL : 0UL"
		p.read = func(ind, src string) { g.sf("%s%s = %s != 0UL;\n", ind, name, src) }
		return p, true

	case ir.TBits:
		w := int64(f.Type.Width)
		p.bits = w
		p.expr = masked("(ulong)"+name, w)
		p.read = func(ind, src string) {
			if w <= 32 {
				g.sf("%s%s = (uint)%s;\n", ind, name, src)
			} else {
				g.sf("%s%s = %s;\n", ind, name, src)
			}
		}
		return p, true

	case ir.TInt:
		if f.Type.Width > 64 {
			return flatPiece{}, false
		}
		if !f.HasIntRange {
			return g.flatBarePiece(p, f, name), true
		}
		return g.flatRangedPiece(p, f, name, writing)

	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			bits := ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max))
			if !writing && (bits == 0 || !vacuous(big.NewInt(0), big.NewInt(ref.Max))) {
				return flatPiece{}, false // the runtime's ranged read refuses, and latches
			}
			typ := "uint"
			if ref.StorageBits > 32 {
				typ = "ulong"
			}
			if big.NewInt(ref.Max).Cmp(new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(ref.StorageBits)), big.NewInt(1))) < 0 {
				p.guard = func(ind string) {
					g.writeAssert(ind, fmt.Sprintf("(%s)%s <= %d", typ, name, ref.Max), fmt.Sprintf("%s above the enum wire range [0, %d]", name, ref.Max))
				}
			}
			p.bits = bits
			if bits > 0 {
				p.expr = masked(fmt.Sprintf("(ulong)(%s)%s", typ, name), bits)
			}
			p.read = func(ind, src string) { g.sf("%s%s = (%s)%s;\n", ind, name, f.Type.Name, src) }
			return p, true
		case *ir.Flags:
			w := int64(ref.WireBits)
			if w < 1 || w > 64 {
				return flatPiece{}, false
			}
			if w < 64 {
				p.guard = func(ind string) {
					g.writeAssert(ind, fmt.Sprintf("(ulong)%s < 1UL << %d", name, w), fmt.Sprintf("%s has a mask bit above the %d-bit wire width", name, w))
				}
			}
			p.bits = w
			p.expr = masked("(ulong)"+name, w)
			p.read = func(ind, src string) { g.sf("%s%s = (%s)%s;\n", ind, name, f.Type.Name, src) }
			return p, true
		}
	}
	return flatPiece{}, false
}

// flatBarePiece is a bare integer at its storage width; a signed value goes
// through the same-width unsigned, so no sign extension reaches the chunk.
func (g *gen) flatBarePiece(p flatPiece, f *ir.Field, name string) flatPiece {
	w := int64(f.Type.Width)
	p.bits = w
	switch {
	case w == 64 && f.Type.Signed:
		p.expr = "unchecked((ulong)" + name + ")"
	case w == 64:
		p.expr = name
	case f.Type.Signed:
		p.expr = masked(fmt.Sprintf("unchecked((ulong)(%s)%s)", csUint(f.Type.Width), name), w)
	default:
		p.expr = masked("(ulong)"+name, w)
	}
	p.read = func(ind, src string) {
		switch {
		case w == 64 && f.Type.Signed:
			g.sf("%s%s = unchecked((long)%s);\n", ind, name, src)
		case w == 64:
			g.sf("%s%s = %s;\n", ind, name, src)
		case f.Type.Signed:
			g.sf("%s%s = unchecked((%s)(%s)%s);\n", ind, name, csInt(f.Type.Width), csUint(f.Type.Width), src)
		default:
			g.sf("%s%s = (%s)%s;\n", ind, name, csUint(f.Type.Width), src)
		}
	}
	return p
}

// flatRangedPiece is a ranged integer's offset from min in BitsRequired bits,
// carrying the per-item form's write contract. On read it joins a run only
// where no refusal is lost (see the file header).
func (g *gen) flatRangedPiece(p flatPiece, f *ir.Field, name string, writing bool) (flatPiece, bool) {
	bits := ir.BitsRequired(f.IntMin, f.IntMax)
	storage := g.storage(f.Type)
	path := intPath(f.IntMin, f.IntMax)
	if !writing && path != "bits64" && !vacuous(f.IntMin, f.IntMax) {
		return flatPiece{}, false // the runtime's ranged read refuses, and latches
	}
	var assertLo, assertHi bool
	if path == "bits64" {
		assertLo, assertHi = f.IntMin.Sign() > 0, f.IntMax.Cmp(maxUint64) < 0
	} else {
		sMin, sMax := storageBounds(f.Type)
		assertLo, assertHi = f.IntMin.Cmp(sMin) > 0, f.IntMax.Cmp(sMax) < 0
	}
	p.guard = func(ind string) {
		msg := fmt.Sprintf("%s out of range [%s, %s]", name, f.IntMin, f.IntMax)
		switch {
		case assertLo && assertHi:
			g.writeAssert(ind, fmt.Sprintf("%s >= %s && %s <= %s", name, f.IntMin, name, f.IntMax), msg)
		case assertLo:
			g.writeAssert(ind, fmt.Sprintf("%s >= %s", name, f.IntMin), msg)
		case assertHi:
			g.writeAssert(ind, fmt.Sprintf("%s <= %s", name, f.IntMax), msg)
		}
	}
	p.bits = bits
	if bits > 0 {
		// the offset in the unsigned domain, as the runtime computes it
		switch {
		case f.IntMin.Sign() == 0:
			p.expr = masked(fmt.Sprintf("unchecked((ulong)%s)", name), bits)
		case f.IntMin.Sign() < 0:
			p.expr = masked(fmt.Sprintf("unchecked((ulong)%s - (ulong)(%s))", name, f.IntMin), bits)
		default:
			p.expr = masked(fmt.Sprintf("(ulong)%s - %sUL", name, f.IntMin), bits)
		}
	}
	p.read = func(ind, src string) {
		if bits == 0 {
			g.sf("%s%s = unchecked((%s)%s); // a degenerate range: zero bits (SPEC §4.6)\n", ind, name, storage, f.IntMin)
			return
		}
		if path == "bits64" && !vacuous(f.IntMin, f.IntMax) {
			g.reject(ind, fmt.Sprintf("%s > %sUL", src, new(big.Int).Sub(f.IntMax, f.IntMin)), " // out of range: refused, not latched (SPEC §5)")
		}
		if f.IntMin.Sign() == 0 {
			g.sf("%s%s = unchecked((%s)%s);\n", ind, name, storage, src)
			return
		}
		g.sf("%s%s = unchecked((%s)(%s + (ulong)(%s)));\n", ind, name, storage, src, f.IntMin)
	}
	return p, true
}
