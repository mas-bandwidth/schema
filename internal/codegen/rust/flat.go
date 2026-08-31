// The flat word codec: the Rust target's self-contained wire form.
//
// The per-field form this replaces asked the runtime to place one field at a
// time. That is free when the spine lands inlined in a caller whose stream was
// just constructed — every scratch offset is then a compile-time constant and
// the whole sequence folds — and it is expensive whenever it does not: entered
// with an unknown bit position, each serialize_bits reloads the packer's
// scratch, shifts by a runtime amount, tests a runtime carry and stores the
// state back, so an eleven-field write pays eleven memory round trips through
// the stream.
//
// The flat form removes that dependence. Every bit offset INSIDE a message is
// a generation-time constant regardless of where the message starts, so the
// emitter folds the field placement itself: field values are computed into
// locals, OR'd into 32-bit chunk locals at literal shifts, and handed to the
// stream one whole chunk at a time. A run of B bits costs ceil(B/32) stream
// calls instead of one per field, and the packing that remains is register
// arithmetic with literal shifts and literal masks — the same shape the Java
// and C-family backends emit, spelled against the one buffer interface
// serialize.rs exposes.
//
// Chunk widths sum to B EXACTLY (the last chunk carries B mod 32, or 32), so
// the flat form reads and writes precisely the bits the per-field form did:
// same wire, byte for byte, proven by the goldens and the cross-language
// conformance suite.
//
// Runs break at every construct whose width or content is not a
// generation-time constant — align, string/bytes, arrays, branches, nested
// struct and union calls — and at the fixed-point, compressed-float and
// 128-bit families, whose value arithmetic lives in the runtime. Those items
// keep the per-field form and start a fresh run after themselves.
//
// The read side fuses its bounds checks the same way, which is the technique
// the Java, Dart and JS-flat backends already carry ("bounds checks fused per
// maximal static run"): one check per chunk instead of one per field. The
// consequence that fusion always has is worth naming — a stream that is BOTH
// truncated inside a run AND carries an out-of-range value before the
// truncation now surfaces Overflow where the per-field form surfaced the range
// refusal. Both refuse the packet; the set of accepted streams is unchanged,
// which is the property that matters at the trust boundary.
package rust

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// maxRunBits caps one flat run. Past it the run closes and a fresh one opens
// at the next field boundary: a run holds every one of its field values live
// across its chunk assembly, and an unbounded run on a hundred-field message
// spills them to the stack, which is the cost the form exists to avoid. 256
// bits is eight chunks — well inside the register file on every target this
// family builds for, and long enough that every corpus shape's straight-line
// stretches flatten whole.
const maxRunBits = 256

// flatPiece is one statically-sized contribution to a flat run: a scalar
// field, a const item or a reserved item.
type flatPiece struct {
	item ir.Item
	bits int64 // wire width; 0 for a degenerate range, which rides no bits

	// guard emits the write-side range refusal. Never nil; it may emit
	// nothing when both halves are vacuous.
	guard func(ind string)

	// value is the u64-valued write expression for the piece. Empty when
	// bits is 0.
	value string

	// note is a trailing comment for the value's let binding.
	note string

	// read emits the read-side validation and field store. src names the u64
	// local holding the extracted bits; a zero-bit piece is handed the literal
	// "0", the only value its range can carry (SPEC §4.6).
	read func(ind, src string)
}

// flatRun accumulates consecutive flat pieces and the items they came from,
// so a run too short to be worth flattening can fall back item by item.
type flatRun struct {
	pieces []flatPiece
	bits   int64
}

// worthFlattening is the policy: two or more bit-carrying pieces. One piece
// packs and unpacks to exactly what the per-field form already emits, so
// flattening it would churn the goldens for nothing.
func (r *flatRun) worthFlattening() bool {
	n := 0
	for _, p := range r.pieces {
		if p.bits > 0 {
			n++
		}
	}
	return n >= 2
}

// chunkWidths splits a run of `bits` wire bits into the 32-bit chunks the
// stream sees. The widths sum to `bits` exactly.
func chunkWidths(bits int64) []int64 {
	var out []int64
	for left := bits; left > 0; left -= 32 {
		if left >= 32 {
			out = append(out, 32)
		} else {
			out = append(out, left)
		}
	}
	return out
}

// mask64 renders the low-`bits` mask as a u64 literal, or "" at 64 bits where
// no mask is needed.
func mask64(bits int64) string {
	if bits >= 64 {
		return ""
	}
	m := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))
	return fmt.Sprintf("0x%x", m)
}

// ---- write ----------------------------------------------------------------

// emitFlatWriteRun packs the run's fields into 32-bit chunks with literal
// shifts and hands each whole chunk to the stream.
func (g *gen) emitFlatWriteRun(r *flatRun, ind string) {
	g.needsStreamTrait = true

	// Refusals first, in declaration order: the set of refused values and the
	// error each raises are exactly the per-field form's. What moves is how
	// many bits reached the stream before the refusal — a refused write leaves
	// a partial message either way, and the run has not started packing yet.
	for _, p := range r.pieces {
		p.guard(ind)
	}

	// one local per bit-carrying field, then the chunks
	offsets := make([]int64, len(r.pieces))
	var at int64
	for i, p := range r.pieces {
		offsets[i] = at
		if p.bits > 0 {
			g.pf("%slet f%d: u64 = %s;%s\n", ind, i, p.value, p.note)
			at += p.bits
		}
	}

	for j, width := range chunkWidths(r.bits) {
		lo := int64(j) * 32
		hi := lo + width
		var terms []string
		for i, p := range r.pieces {
			if p.bits == 0 {
				continue
			}
			o := offsets[i]
			if o >= hi || o+p.bits <= lo {
				continue
			}
			switch {
			case o >= lo:
				terms = append(terms, shiftLeft(fmt.Sprintf("f%d", i), o-lo))
			default:
				terms = append(terms, fmt.Sprintf("(f%d >> %d)", i, lo-o))
			}
		}
		packed := strings.Join(terms, " | ")
		if len(terms) > 1 {
			packed = "(" + packed + ")"
		}
		g.pf("%slet mut w%d = %s as u32;\n", ind, j, packed)
		g.pf("%sstream.serialize_bits(&mut w%d, %d)?;\n", ind, j, width)
	}
}

// shiftLeft renders `expr << n`, dropping the shift at zero.
func shiftLeft(expr string, n int64) string {
	if n == 0 {
		return expr
	}
	return fmt.Sprintf("(%s << %d)", expr, n)
}

// ---- read -----------------------------------------------------------------

// emitFlatReadRun reads the run's chunks — one bounds check per chunk instead
// of one per field — then unpacks, validates and stores each field with
// literal shifts and masks.
func (g *gen) emitFlatReadRun(r *flatRun, ind string) {
	g.needsStreamTrait = true

	widths := chunkWidths(r.bits)
	g.pf("%slet mut c: u32 = 0;\n", ind)
	for j, width := range widths {
		g.pf("%sstream.serialize_bits(&mut c, %d)?;\n", ind, width)
		g.pf("%slet c%d = u64::from(c);\n", ind, j)
	}

	var at int64
	for i, p := range r.pieces {
		if p.bits == 0 {
			p.read(ind, "0")
			continue
		}
		o := at
		at += p.bits
		first := o / 32
		last := (o + p.bits - 1) / 32
		var terms []string
		for j := first; j <= last; j++ {
			lo := j * 32
			if lo <= o {
				terms = append(terms, shiftRight(fmt.Sprintf("c%d", j), o-lo))
			} else {
				terms = append(terms, fmt.Sprintf("(c%d << %d)", j, lo-o))
			}
		}
		expr := strings.Join(terms, " | ")
		if m := mask64(p.bits); m != "" {
			if len(terms) > 1 {
				expr = fmt.Sprintf("(%s) & %s", expr, m)
			} else {
				expr = fmt.Sprintf("%s & %s", expr, m)
			}
		}
		g.pf("%slet v%d: u64 = %s;\n", ind, i, expr)
		p.read(ind, fmt.Sprintf("v%d", i))
	}
}

// shiftRight renders `expr >> n`, dropping the shift at zero.
func shiftRight(expr string, n int64) string {
	if n == 0 {
		return expr
	}
	return fmt.Sprintf("(%s >> %d)", expr, n)
}

// ---- classification -------------------------------------------------------

// flatPieceOf classifies one item. The second result is false for every
// construct whose width or content is not a generation-time constant; those
// break the run and keep the per-field form.
func (g *gen) flatPieceOf(item ir.Item) (flatPiece, bool) {
	switch it := item.(type) {
	case *ir.ConstItem:
		return g.flatConstPiece(it), true
	case *ir.ReservedItem:
		return g.flatReservedPiece(it), true
	case *ir.FieldItem:
		if it.F.Array != ir.ArrayNone {
			return flatPiece{}, false
		}
		return g.flatFieldPiece(item, it.F)
	}
	return flatPiece{}, false
}

func (g *gen) flatConstPiece(it *ir.ConstItem) flatPiece {
	return flatPiece{
		item:  it,
		bits:  it.Bits,
		guard: func(string) {},
		value: fmt.Sprintf("%s_u64", it.Value.String()),
		note:  fmt.Sprintf(" // const(%s, %d) — SPEC §4.3", it.Value.String(), it.Bits),
		read: func(ind, src string) {
			g.pf("%sif %s != %s {\n", ind, src, it.Value.String())
			g.pf("%s    // const(%s, %d): a read rejects any other value (SPEC §4.3)\n", ind, it.Value.String(), it.Bits)
			g.pf("%s    return Err(Error::Validation);\n%s}\n", ind, ind)
		},
	}
}

func (g *gen) flatReservedPiece(it *ir.ReservedItem) flatPiece {
	return flatPiece{
		item:  it,
		bits:  it.Bits,
		guard: func(string) {},
		value: "0",
		note:  fmt.Sprintf(" // reserved(%d) — zeros on the wire", it.Bits),
		read: func(ind, src string) {
			g.pf("%sif %s != 0 {\n", ind, src)
			g.pf("%s    // reserved(%d): a read rejects nonzero (SPEC §4.3)\n", ind, it.Bits)
			g.pf("%s    return Err(Error::Validation);\n%s}\n", ind, ind)
		},
	}
}

// flatFieldPiece classifies a scalar field. Everything whose value arithmetic
// lives in the runtime — fixed point, compressed float, the 128-bit family —
// stays on the per-field path and breaks the run.
func (g *gen) flatFieldPiece(item ir.Item, f *ir.Field) (flatPiece, bool) {
	name := "value." + f.Name
	noGuard := func(string) {}

	switch f.Type.Kind {
	case ir.TBool:
		return flatPiece{
			item: item, bits: 1, guard: noGuard,
			value: fmt.Sprintf("u64::from(%s)", name),
			read: func(ind, src string) {
				g.pf("%s%s = %s != 0;\n", ind, name, src)
			},
		}, true

	case ir.TBits:
		w := int64(f.Type.Width)
		guard := noGuard
		if f.Type.Width != 32 && f.Type.Width != 64 {
			// storage is the wider unsigned type: bits above the wire width
			// are refused, not wrapped (the runtime's write side only
			// debug_asserts)
			guard = func(ind string) {
				g.pf("%sif %s >= 1 << %d {\n", ind, name, f.Type.Width)
				g.pf("%s    return Err(Error::Stream(serialize::Error::ValueOutOfRange));\n%s}\n", ind, ind)
			}
		}
		storage := g.rustFieldType(f.Type)
		value := fmt.Sprintf("u64::from(%s)", name)
		if storage == "u64" {
			value = name
		}
		return flatPiece{
			item: item, bits: w, guard: guard,
			value: value,
			read: func(ind, src string) {
				if storage == "u64" {
					g.pf("%s%s = %s;\n", ind, name, src)
					return
				}
				g.pf("%s%s = %s as %s;\n", ind, name, src, storage)
			},
		}, true

	case ir.TFloat32:
		if f.HasFloatRange {
			return flatPiece{}, false // the quantization lives in the runtime
		}
		return flatPiece{
			item: item, bits: 32, guard: noGuard,
			value: fmt.Sprintf("u64::from(%s.to_bits())", name),
			read: func(ind, src string) {
				g.pf("%s%s = f32::from_bits(%s as u32);\n", ind, name, src)
			},
		}, true

	case ir.TFloat64:
		return flatPiece{
			item: item, bits: 64, guard: noGuard,
			value: fmt.Sprintf("%s.to_bits()", name),
			read: func(ind, src string) {
				g.pf("%s%s = f64::from_bits(%s);\n", ind, name, src)
			},
		}, true

	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			bits := ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max))
			guard := noGuard
			if ref.Max < (int64(1)<<uint(ref.StorageBits))-1 {
				// headroom storage can exceed the wire range: refused, not
				// wrapped (the runtime's write side only debug_asserts)
				guard = func(ind string) {
					g.pf("%sif %s.0 > %d {\n", ind, name, ref.Max)
					g.pf("%s    return Err(Error::Stream(serialize::Error::ValueOutOfRange));\n%s}\n", ind, ind)
				}
			}
			storage := rustUint(ref.StorageBits)
			typeName := f.Type.Name
			max := ref.Max
			return flatPiece{
				item: item, bits: bits, guard: guard,
				value: fmt.Sprintf("u64::from(%s.0)", name),
				read: func(ind, src string) {
					// serialize_int's own refusal: a tag above the exported
					// extent reaches the storage's bit headroom, and a read
					// rejects it rather than admitting it (SPEC §4.2)
					if !rangeIsFull(big.NewInt(0), big.NewInt(max), bits) {
						g.pf("%sif %s > %d {\n", ind, src, max)
						g.pf("%s    return Err(Error::Stream(serialize::Error::ValueOutOfRange));\n%s}\n", ind, ind)
					}
					g.pf("%s%s = %s(%s as %s);\n", ind, name, typeName, src, storage)
				},
			}, true

		case *ir.Flags:
			wire := int64(ref.WireBits)
			guard := noGuard
			if ref.WireBits < 64 {
				guard = func(ind string) {
					g.pf("%sif %s >= 1 << %d {\n", ind, name, ref.WireBits)
					g.pf("%s    // a mask bit above the wire width cannot ride\n", ind)
					g.pf("%s    return Err(Error::Stream(serialize::Error::ValueOutOfRange));\n%s}\n", ind, ind)
				}
			}
			return flatPiece{
				item: item, bits: wire, guard: guard,
				value: name, // the flags alias IS u64 storage (SPEC §4.2)
				read: func(ind, src string) {
					g.pf("%s%s = %s;\n", ind, name, src)
				},
			}, true
		}
		return flatPiece{}, false // nested struct or union: its own call

	case ir.TInt:
		return g.flatIntPiece(item, f)
	}
	return flatPiece{}, false
}

// rangeIsFull reports whether [min,max] covers every value the wire's bit
// count can carry, which makes the read-side range refusal provably vacuous.
func rangeIsFull(min, max *big.Int, bits int64) bool {
	if bits >= 64 {
		return new(big.Int).Sub(max, min).Cmp(maxUint64) == 0
	}
	span := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))
	return new(big.Int).Sub(max, min).Cmp(span) == 0
}

// flatIntPiece classifies an integer field across the four wire paths the
// per-field form uses, so the folded offsets and the refusals are the same
// arithmetic in the same order.
func (g *gen) flatIntPiece(item ir.Item, f *ir.Field) (flatPiece, bool) {
	name := "value." + f.Name
	if f.Type.Width == 128 {
		return flatPiece{}, false // the 128-bit family stays on the runtime path
	}
	storage := g.rustFieldType(f.Type)

	if !f.HasIntRange {
		w := int64(f.Type.Width)
		var value string
		switch {
		case f.Type.Width == 64 && f.Type.Signed:
			value = name + " as u64"
		case f.Type.Width == 64:
			value = name
		case !f.Type.Signed:
			value = fmt.Sprintf("u64::from(%s)", name)
		case f.Type.Width == 32:
			value = fmt.Sprintf("u64::from(%s as u32)", name)
		default:
			// through the same-width unsigned so the sign bit cannot extend
			value = fmt.Sprintf("u64::from(%s as u%d)", name, f.Type.Width)
		}
		signed, width := f.Type.Signed, f.Type.Width
		return flatPiece{
			item: item, bits: w, guard: func(string) {},
			value: value,
			read: func(ind, src string) {
				switch {
				case width == 64 && signed:
					g.pf("%s%s = %s as i64;\n", ind, name, src)
				case width == 64:
					g.pf("%s%s = %s;\n", ind, name, src)
				case signed && width < 32:
					// back through the same-width unsigned so the sign bit lands right
					g.pf("%s%s = %s as u%d as i%d;\n", ind, name, src, width, width)
				default:
					g.pf("%s%s = %s as %s;\n", ind, name, src, storage)
				}
			},
		}, true
	}

	// degenerate range: ZERO BITS — the value is known from the range alone
	// (SPEC §4.6). The write keeps its refusal; the read materializes.
	if f.IntMin.Cmp(f.IntMax) == 0 {
		return flatPiece{
			item: item, bits: 0,
			guard: func(ind string) { g.emitWriteRangeGuard(name, f, ind) },
			read: func(ind, _ string) {
				g.pf("%s%s = %s;\n", ind, name, rustIntLitStorage(f.IntMin, f.Type.Signed, f.Type.Width))
			},
		}, true
	}

	bits := ir.BitsRequired(f.IntMin, f.IntMax)
	switch intRangePath(f.IntMin, f.IntMax) {
	case "int32":
		lo := g.foldArg32(f.IntMinExpr, f.IntMin)
		loZero := f.IntMin.Sign() == 0
		exprIsU32 := !f.Type.Signed && f.Type.Width == 32
		readLo := g.foldArg32(f.IntMinExpr, f.IntMin)
		full := rangeIsFull(f.IntMin, f.IntMax, bits)
		diff := new(big.Int).Sub(f.IntMax, f.IntMin)
		return flatPiece{
			item: item, bits: bits,
			guard: func(ind string) { g.emitWriteRangeGuard(name, f, ind) },
			value: fmt.Sprintf("u64::from(%s)", foldOffset32(name, exprIsU32, lo, loZero)),
			read: func(ind, src string) {
				if !full {
					// a malicious packet can smuggle an out-of-range value
					// into the encoding's bit headroom (SPEC §4.3)
					g.pf("%sif %s > %s {\n", ind, src, diff.String())
					g.pf("%s    return Err(Error::Stream(serialize::Error::ValueOutOfRange));\n%s}\n", ind, ind)
				}
				decoded := fmt.Sprintf("(%s as u32).wrapping_add((%s) as u32) as i32", src, readLo)
				if loZero {
					decoded = fmt.Sprintf("%s as i32", src)
				}
				if f.Type.Signed && f.Type.Width == 32 {
					g.pf("%s%s = %s;\n", ind, name, decoded)
					return
				}
				g.pf("%s%s = (%s) as %s;\n", ind, name, decoded, storage)
			},
		}, true

	case "int64":
		lo := g.foldArg64(f.IntMinExpr, f.IntMin)
		loZero := f.IntMin.Sign() == 0
		exprIsU64 := !f.Type.Signed && f.Type.Width == 64
		readLo := g.foldArg64(f.IntMinExpr, f.IntMin)
		full := rangeIsFull(f.IntMin, f.IntMax, bits)
		diff := new(big.Int).Sub(f.IntMax, f.IntMin)
		cast := name + " as u64"
		if exprIsU64 {
			cast = name
		}
		value := cast
		if !loZero {
			value = fmt.Sprintf("(%s).wrapping_sub((%s) as u64)", cast, lo)
		}
		return flatPiece{
			item: item, bits: bits,
			guard: func(ind string) { g.emitWriteRangeGuard(name, f, ind) },
			value: value,
			read: func(ind, src string) {
				if !full {
					g.pf("%sif %s > %s {\n", ind, src, diff.String())
					g.pf("%s    return Err(Error::Stream(serialize::Error::ValueOutOfRange));\n%s}\n", ind, ind)
				}
				decoded := fmt.Sprintf("%s.wrapping_add((%s) as u64) as i64", src, readLo)
				if loZero {
					decoded = fmt.Sprintf("%s as i64", src)
				}
				if f.Type.Signed && f.Type.Width == 64 {
					g.pf("%s%s = %s;\n", ind, name, decoded)
					return
				}
				g.pf("%s%s = (%s) as %s;\n", ind, name, decoded, storage)
			},
		}, true
	}

	// full-range unsigned: raw offset bits over u64 storage
	lo, _ := g.rangeArgs(f, "u64")
	loVacuous := f.IntMin.Sign() == 0
	hiVacuous := f.IntMax.Cmp(maxUint64) == 0
	diff := new(big.Int).Sub(f.IntMax, f.IntMin)
	value := name
	if !loVacuous {
		value = fmt.Sprintf("%s - %s", name, lo)
	}
	return flatPiece{
		item: item, bits: bits,
		guard: func(ind string) {
			// Like every bounded write path in this target, the range refusal
			// is generated: serialize.rs's write side only debug_asserts, so a
			// misuse value must be refused here or it wraps into valid-looking
			// wire; vacuous halves are elided.
			switch {
			case !loVacuous && !hiVacuous:
				g.pf("%sif %s < %s || %s > %s {\n", ind, name, lo, name, f.IntMax.String())
				g.pf("%s    return Err(Error::Stream(serialize::Error::ValueOutOfRange));\n%s}\n", ind, ind)
			case !loVacuous:
				g.pf("%sif %s < %s {\n", ind, name, lo)
				g.pf("%s    return Err(Error::Stream(serialize::Error::ValueOutOfRange));\n%s}\n", ind, ind)
			case !hiVacuous:
				g.pf("%sif %s > %s {\n", ind, name, f.IntMax.String())
				g.pf("%s    return Err(Error::Stream(serialize::Error::ValueOutOfRange));\n%s}\n", ind, ind)
			}
		},
		value: value,
		read: func(ind, src string) {
			if diff.Cmp(maxUint64) != 0 {
				// a full-width diff cannot overflow its own read — elided
				g.pf("%sif %s > %s {\n", ind, src, diff.String())
				g.pf("%s    // a read rejects out-of-range (SPEC §5)\n", ind)
				g.pf("%s    return Err(Error::Validation);\n%s}\n", ind, ind)
			}
			if loVacuous {
				g.pf("%s%s = %s;\n", ind, name, src)
				return
			}
			g.pf("%s%s = %s + %s;\n", ind, name, src, lo)
		},
	}, true
}
