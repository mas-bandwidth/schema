// The flat word codec: the Go target's self-contained wire form.
//
// The per-field form this replaces asked the runtime to place one field at a
// time. In Go that is never free. serialize.go's per-field entry points sit
// either side of the compiler's fixed 80-unit inlining budget: SerializeBits
// is a thin wrapper the compiler does inline, but the writeBits (cost 92) and
// readBits (cost 106) bodies underneath it are NOT inlinable, and the ranged
// entry points (SerializeInt 168, SerializeInt64 328, SerializeBits64 278)
// are further out still. So every field cost one real call into the runtime,
// on both paths — a fourteen-field struct paid fourteen of them, and a CPU
// profile of the generated codec put ~86% of its time inside those calls
// against ~2% in the generated code itself.
//
// The flat form removes the per-field dependence. Every bit offset INSIDE a
// message is a generation-time constant regardless of where the message
// starts, so the emitter folds the field placement itself: field values are
// computed into locals, OR'd into word-sized chunk locals at literal shifts,
// and handed to the stream one whole chunk at a time. A run of B bits costs
// ceil(B/64) stream calls instead of one per field, and the packing that
// remains is register arithmetic with literal shifts and literal masks.
//
// TWO DELIBERATE DEVIATIONS FROM THE RUST TEMPLATE (PR #183), both because Go
// physics differ:
//
//  1. CHUNKS ARE 64 BITS, not 32. SerializeBits64 places a whole 64-bit word
//     in ONE call with one bounds check (its two internal tryWriteBits calls
//     are inlined into it), where two SerializeBits are two calls with two
//     bounds checks. The wire is identical because SerializeBits64 splits a
//     wide value low-dword-first-then-remainder, which IS the 32-bit chunk
//     order. A chunk of 32 bits or fewer still goes through SerializeBits,
//     whose wrapper inlines.
//
//  2. EVERY PIECE IS MASKED TO ITS WIDTH. serialize.go's write path MASKS a
//     too-wide value to the field width; the Rust runtime only debug_asserts,
//     so PR #183 had to REFUSE such values to stay faithful. Masking here is
//     what keeps the Go form observably identical to the per-field form it
//     replaces, and it doubles as the guarantee that no piece can corrupt its
//     neighbours in the chunk.
//
// Chunk widths sum to B EXACTLY (the last chunk carries the remainder), so the
// flat form reads and writes precisely the bits the per-field form did: same
// wire, byte for byte, proven by the goldens and the cross-language
// conformance suite.
//
// Runs break at every construct whose width or content is not a
// generation-time constant — align, string/bytes, arrays, branches, nested
// struct and union calls — and at the fixed-point and 128-bit families, whose
// value arithmetic lives in the runtime. Those items keep the per-field form
// and start a fresh run after themselves.
//
// COMPRESSED FLOATS DO NOT BREAK A RUN HERE, and that is a Go-specific win
// over the Rust template: this emitter already folds the quantization into
// generated arithmetic that ends in a plain bit write, so the quantized code
// is just another piece. In Rust the quantization lives in the runtime, which
// is why PR #183's compressed-float-heavy shapes barely moved.
//
// The read side fuses its checks the same way: one bounds check per chunk
// instead of one per field, and ONE sticky-error check per run. The
// consequence that fusion always has is worth naming — a stream that is BOTH
// truncated inside a run AND carries an out-of-range value before the
// truncation now surfaces the stream's overflow error where the per-field form
// surfaced the range refusal. Both refuse the packet; the set of ACCEPTED
// streams is unchanged, which is the property that matters at the trust
// boundary, and it is the same consequence the Java, Dart, JS-flat and Rust
// backends already carry.
package golang

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// maxRunBits caps one flat run. Past it the run closes and a fresh one opens
// at the next field boundary: a run holds every one of its field values live
// across its chunk assembly, and an unbounded run on a hundred-field message
// spills them to the stack, which is the cost the form exists to avoid.
//
// Measured on the corpus shape, M2, one sitting (bench_mixed write / round_trip
// M msg/s): 256 -> 2.66/1.40, 384 -> 2.78/1.49, 512 -> 2.75/1.46,
// 1024 -> 2.62/1.43. The curve peaks and then falls: past the peak the run
// holds more field values live than the register file has room for and they
// spill, which is exactly the cost this cap exists to bound.
const maxRunBits = 384

// chunkBits is the word the stream sees. 64 is the measured choice for Go:
// see the deviation note in this file's header.
const chunkBits = 64

// flatPiece is one statically-sized contribution to a flat run: a scalar
// field, one element of an unrolled fixed array, a nested struct's field, a
// const item or a reserved item.
//
// A piece deliberately does NOT carry the ir.Item it came from. A piece is a
// bit-placement recipe closed over an expression that may name a nested
// struct's field, or one element of an array — expressions no item-level
// emitter can reproduce. Items are held one level up, by flatGroup, which is
// the only unit a fallback may re-emit.
type flatPiece struct {
	bits int64 // wire width; 0 for a degenerate range, which rides no bits

	// guard emits the write-side range refusal and any typed temp the value
	// expression reads. Never nil; it may emit nothing.
	guard func(ind string, idx int)

	// emit defines fIdx as a uint64 holding the piece's wire bits, masked to
	// bits. Nil when bits is 0.
	emit func(ind string, idx int)

	// read emits the read-side validation and field store. src names the
	// uint64 local holding the extracted bits; a zero-bit piece is handed the
	// literal "0", the only value its range can carry (SPEC §4.6).
	read func(ind, src string)
}

// flatRun accumulates consecutive flat pieces: the packing unit the chunk
// arithmetic works over.
type flatRun struct {
	pieces []flatPiece
	bits   int64
}

// flatGroup is one whole ITEM's contribution to a run — the pieces it
// classified into, kept beside the item itself.
//
// The group is the invariant that makes a run's fallback safe. PR #183's Rust
// template mapped one item to exactly one piece, so re-emitting a run's pieces
// WAS re-emitting its items. This emitter's levers made classification 1:N —
// an unrolled fixed array contributes one piece per element, and a flattened
// nested struct contributes pieces that name ITS fields against a different
// base expression. Falling back over pieces therefore emitted an array's loop
// once per element and named a nested field on the outer type. Falling back
// over GROUPS re-emits each item exactly once, against the base the item
// itself owns, which is the property #183 had by construction.
type flatGroup struct {
	item   ir.Item
	pieces []flatPiece
	bits   int64
}

// flatSeq accumulates groups across a struct body, so a run splits only on
// item boundaries and can always fall back to whole items.
type flatSeq struct {
	groups []flatGroup
	bits   int64
}

func (s *flatSeq) add(item ir.Item, pieces []flatPiece) {
	g := flatGroup{item: item, pieces: pieces}
	for _, p := range pieces {
		g.bits += p.bits
	}
	s.groups = append(s.groups, g)
	s.bits += g.bits
}

// run flattens the accumulated groups into the packing unit.
func (s *flatSeq) run() *flatRun {
	r := &flatRun{bits: s.bits}
	for _, g := range s.groups {
		r.pieces = append(r.pieces, g.pieces...)
	}
	return r
}

// worthFlattening is the policy: flatten only where it REDUCES the number of
// stream calls, which is the entire cost the form exists to remove.
//
// The count-based rule matters. A run whose pieces are each exactly one whole
// chunk — a struct of float64s, say — packs into as many chunks as it has
// fields, so flattening removes no call and instead ADDS one materialized
// local per field plus an address-taken chunk local, where the per-field form
// simply passed the struct field's own address. Measured, that cost the
// rigidbody shapes 9% on write before this rule was in place.
func (r *flatRun) worthFlattening() bool {
	n := int64(0)
	for _, p := range r.pieces {
		if p.bits > 0 {
			n++
		}
	}
	if n < 2 {
		return false
	}
	return int64(len(chunkWidths(r.bits))) < n
}

// chunkWidths splits a run of `bits` wire bits into the chunks the stream
// sees. The widths sum to `bits` exactly.
func chunkWidths(bits int64) []int64 {
	var out []int64
	for left := bits; left > 0; left -= chunkBits {
		if left >= chunkBits {
			out = append(out, chunkBits)
		} else {
			out = append(out, left)
		}
	}
	return out
}

// maskLit renders the low-`bits` mask as a Go uint64 literal, or "" at 64 bits
// where no mask is needed.
func maskLit(bits int64) string {
	if bits >= 64 {
		return ""
	}
	m := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))
	return fmt.Sprintf("0x%x", m)
}

// masked wraps a uint64-valued expression in its width mask. The runtime's
// per-field write masks to the field width, so the flat form must too, or a
// too-wide value that used to be truncated would corrupt its neighbours.
func masked(expr string, bits int64) string {
	m := maskLit(bits)
	if m == "" {
		return expr
	}
	return "(" + expr + ") & " + m
}

// shiftLeft renders `expr << n`, dropping the shift at zero.
func shiftLeft(expr string, n int64) string {
	if n == 0 {
		return expr
	}
	return fmt.Sprintf("(%s << %d)", expr, n)
}

// shiftRight renders `expr >> n`, dropping the shift at zero.
func shiftRight(expr string, n int64) string {
	if n == 0 {
		return expr
	}
	return fmt.Sprintf("(%s >> %d)", expr, n)
}

// ---- write ----------------------------------------------------------------

// emitFlatWriteRun packs the run's fields into chunks with literal shifts and
// hands each whole chunk to the stream.
func (g *gen) emitFlatWriteRun(r *flatRun, ind string) {
	// Refusals first, in declaration order: the set of refused values and the
	// error each raises are exactly the per-field form's. What moves is how
	// many bits reached the stream before the refusal — a refused write leaves
	// a partial message either way, and the run has not started packing yet.
	for i, p := range r.pieces {
		p.guard(ind, i)
	}

	offsets := make([]int64, len(r.pieces))
	var at int64
	for i, p := range r.pieces {
		offsets[i] = at
		if p.bits > 0 {
			p.emit(ind, i)
			at += p.bits
		}
	}

	for j, width := range chunkWidths(r.bits) {
		lo := int64(j) * chunkBits
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
			if o >= lo {
				terms = append(terms, shiftLeft(fmt.Sprintf("f%d", i), o-lo))
			} else {
				terms = append(terms, shiftRight(fmt.Sprintf("f%d", i), lo-o))
			}
		}
		packed := strings.Join(terms, " | ")
		if width <= 32 {
			// SerializeBits: the runtime wrapper the Go compiler inlines
			g.pf("%sw%d := uint32(%s)\n", ind, j, packed)
			g.pf("%sstream.SerializeBits(&w%d, %d)\n", ind, j, width)
			continue
		}
		g.pf("%sw%d := %s\n", ind, j, packed)
		g.pf("%sstream.SerializeBits64(&w%d, %d)\n", ind, j, width)
	}
}

// ---- read -----------------------------------------------------------------

// emitFlatReadRun reads the run's chunks — one bounds check per chunk instead
// of one per field, and ONE sticky-error check for the whole run — then
// unpacks, validates and stores each field with literal shifts and masks.
//
// Reading every chunk before the error check is safe and is the point: a
// failed chunk latches the stream's error, and every later read returns on it
// immediately leaving its destination at zero. So the run pays one error test
// where the per-field form paid one per field.
func (g *gen) emitFlatReadRun(r *flatRun, ind string) {
	widths := chunkWidths(r.bits)
	for j, width := range widths {
		if width <= 32 {
			g.pf("%sn%d := uint32(0)\n", ind, j)
			g.pf("%sstream.SerializeBits(&n%d, %d)\n", ind, j, width)
			g.pf("%sc%d := uint64(n%d)\n", ind, j, j)
			continue
		}
		g.pf("%sc%d := uint64(0)\n", ind, j)
		g.pf("%sstream.SerializeBits64(&c%d, %d)\n", ind, j, width)
	}
	g.pf("%sif stream.Err() != nil {\n%s\treturn stream.Err()\n%s}\n", ind, ind, ind)

	var at int64
	for i, p := range r.pieces {
		if p.bits == 0 {
			p.read(ind, "0")
			continue
		}
		o := at
		at += p.bits
		first := o / chunkBits
		last := (o + p.bits - 1) / chunkBits
		var terms []string
		for j := first; j <= last; j++ {
			lo := j * chunkBits
			if lo <= o {
				terms = append(terms, shiftRight(fmt.Sprintf("c%d", j), o-lo))
			} else {
				terms = append(terms, fmt.Sprintf("(c%d << %d)", j, lo-o))
			}
		}
		expr := strings.Join(terms, " | ")
		if m := maskLit(p.bits); m != "" {
			if len(terms) > 1 {
				expr = "(" + expr + ") & " + m
			} else {
				expr = expr + " & " + m
			}
		}
		g.pf("%sv%d := %s\n", ind, i, expr)
		p.read(ind, fmt.Sprintf("v%d", i))
	}
}

// maxUnrollBits caps the fixed scalar array a run will absorb. A small array
// of scalars is a static run of bound*width bits like any other; past this it
// is left to its loop, which keeps generated code proportional to the schema.
const maxUnrollBits = 128

// flatPiecesOf classifies one item into the pieces it contributes to a run.
// Most items are one piece. A nested struct that flattens whole contributes
// its fields, so the parent packs them into ITS chunks rather than paying a
// call; a small fixed array of scalars contributes one piece per element.
func (g *gen) flatPiecesOf(item ir.Item, base string) ([]flatPiece, bool) {
	it, isField := item.(*ir.FieldItem)
	if !isField {
		p, ok := g.flatPieceOf(item, base)
		if !ok {
			return nil, false
		}
		return []flatPiece{p}, true
	}
	f := it.F
	name := base + ir.GoExportName(f.Name)

	if f.Array == ir.ArrayFixed && !g.bulkBytes[f] {
		// a fixed bound: the element offsets are
		// generation-time constants like any other field's
		elem, ok := g.flatFieldPiece(f, base)
		if ok && elem.bits > 0 && f.ArrayBound*elem.bits <= maxUnrollBits {
			out := make([]flatPiece, 0, f.ArrayBound)
			for k := int64(0); k < f.ArrayBound; k++ {
				p, ok := g.flatArrayElemPiece(f, fmt.Sprintf("%s[%d]", name, k))
				if !ok {
					return nil, false
				}
				out = append(out, p)
			}
			return out, true
		}
	}

	if f.Array == ir.ArrayNone && f.Type.Kind == ir.TNamed {
		if st, ok := f.Type.Ref.(*ir.Struct); ok {
			run, ok := g.flatStructRun(st, name+".")
			if !ok {
				return nil, false
			}
			return run.pieces, true
		}
	}

	p, ok := g.flatPieceOf(item, base)
	if !ok {
		return nil, false
	}
	return []flatPiece{p}, true
}

// flatStructRun builds the whole-body run for a nested struct against a base
// expression, or reports false if any of its items breaks a run. It is what
// lets a nested struct's fields be placed by the ENCLOSING function instead of
// through a call: an array of eighty small structs paid eighty function calls
// and eighty sticky-error loads per message, which the profile showed costing
// more than the bit placement itself once the flat form had removed the
// per-field calls. The nested Write/Read functions are still emitted — the
// public surface does not change — they are simply no longer the path the
// generated caller takes.
func (g *gen) flatStructRun(st *ir.Struct, base string) (*flatRun, bool) {
	if len(st.Items) == 0 {
		return nil, false
	}
	run := &flatRun{}
	for _, item := range st.Items {
		ps, ok := g.flatPiecesOf(item, base)
		if !ok {
			return nil, false
		}
		for _, p := range ps {
			run.pieces = append(run.pieces, p)
			run.bits += p.bits
		}
	}
	if run.bits > maxRunBits || !run.worthFlattening() {
		return nil, false
	}
	return run, true
}

// flatStructRunRaw is flatStructRun without the call-count gate: a group of
// elements is judged as a whole, so one element that would not pay on its own
// can still pay when three of them share a word.
func (g *gen) flatStructRunRaw(st *ir.Struct, base string) (*flatRun, bool) {
	if len(st.Items) == 0 {
		return nil, false
	}
	run := &flatRun{}
	for _, item := range st.Items {
		ps, ok := g.flatPiecesOf(item, base)
		if !ok {
			return nil, false
		}
		for _, p := range ps {
			run.pieces = append(run.pieces, p)
			run.bits += p.bits
		}
	}
	return run, true
}

// maxGroupElems caps how many array elements one run absorbs. The group is
// unrolled in the generated source, so this trades code size for stream calls
// and eight is where the corpus stops gaining.
const maxGroupElems = 8

// flatGroupRun builds one run spanning K consecutive elements of an array of
// nested structs, indexed off idx. A single small element wastes most of the
// word it is placed in — eighty 18-bit stats cost eighty calls that each carry
// 18 of a possible 64 bits — so consecutive elements share chunks and the call
// count falls by roughly K.
func (g *gen) flatGroupRun(f *ir.Field, name, idx string, k int64) (*flatRun, bool) {
	st, ok := flatElemStruct(f)
	if !ok {
		return nil, false
	}
	run := &flatRun{}
	for e := range k {
		at := fmt.Sprintf("%s[%s]", name, idx)
		if e > 0 {
			at = fmt.Sprintf("%s[%s+%d]", name, idx, e)
		}
		sub, ok := g.flatStructRunRaw(st, at+".")
		if !ok {
			return nil, false
		}
		run.pieces = append(run.pieces, sub.pieces...)
		run.bits += sub.bits
	}
	if run.bits > maxRunBits {
		return nil, false
	}
	return run, true
}

// flatGroupSize picks K for an array of nested structs: the most elements
// whose combined run still fits maxRunBits and the unroll cap, and only where
// grouping actually reduces the call count against one element at a time.
func (g *gen) flatGroupSize(f *ir.Field) int64 {
	st, ok := flatElemStruct(f)
	if !ok {
		return 1
	}
	one, ok := g.flatStructRunRaw(st, "x.")
	if !ok || one.bits <= 0 {
		return 1
	}
	best := int64(1)
	perElem := float64(len(chunkWidths(one.bits)))
	for k := int64(2); k <= maxGroupElems; k++ {
		if k*one.bits > maxRunBits {
			break
		}
		if f.ArrayBound < k {
			break
		}
		if float64(len(chunkWidths(k*one.bits)))/float64(k) < perElem {
			best = k
			perElem = float64(len(chunkWidths(k*one.bits))) / float64(k)
		}
	}
	return best
}

func flatElemStruct(f *ir.Field) (*ir.Struct, bool) {
	if f.Type.Kind != ir.TNamed {
		return nil, false
	}
	st, ok := f.Type.Ref.(*ir.Struct)
	return st, ok
}

// flatElementRun builds the run for one element of an array of nested structs,
// so the loop body places the element's fields directly instead of calling the
// element's own wire function once per iteration.
func (g *gen) flatElementRun(f *ir.Field, name string) (*flatRun, bool) {
	if f.Type.Kind != ir.TNamed {
		return nil, false
	}
	st, ok := f.Type.Ref.(*ir.Struct)
	if !ok {
		return nil, false
	}
	return g.flatStructRun(st, name+"[i].")
}

// ---- classification -------------------------------------------------------

// flatPieceOf classifies one item. The second result is false for every
// construct whose width or content is not a generation-time constant; those
// break the run and keep the per-field form.
func (g *gen) flatPieceOf(item ir.Item, base string) (flatPiece, bool) {
	switch it := item.(type) {
	case *ir.ConstItem:
		return g.flatConstPiece(it), true
	case *ir.ReservedItem:
		return g.flatReservedPiece(it), true
	case *ir.FieldItem:
		if it.F.Array != ir.ArrayNone {
			return flatPiece{}, false
		}
		return g.flatFieldPiece(it.F, base)
	}
	return flatPiece{}, false
}

// noGuard is the guard of a piece that cannot refuse anything.
func noGuard(string, int) {}

// constEmit builds an emit that defines fIdx from a plain uint64 expression.
func (g *gen) constEmit(expr string, bits int64, note string) func(string, int) {
	return func(ind string, idx int) {
		g.pf("%sf%d := %s%s\n", ind, idx, masked(expr, bits), note)
	}
}

func (g *gen) flatConstPiece(it *ir.ConstItem) flatPiece {
	v := it.Value.String()
	return flatPiece{
		bits:  it.Bits,
		guard: noGuard,
		emit: g.constEmit("uint64("+v+")", it.Bits,
			fmt.Sprintf(" // const(%s, %d) — SPEC §4.3", v, it.Bits)),
		read: func(ind, src string) {
			g.pf("%sif %s != %s { // const(%s, %d): a read rejects any other value (SPEC §4.3)\n", ind, src, v, v, it.Bits)
			g.pf("%s\treturn ErrValidation\n%s}\n", ind, ind)
		},
	}
}

func (g *gen) flatReservedPiece(it *ir.ReservedItem) flatPiece {
	return flatPiece{
		bits:  it.Bits,
		guard: noGuard,
		emit:  g.constEmit("uint64(0)", it.Bits, fmt.Sprintf(" // reserved(%d) — zeros on the wire", it.Bits)),
		read: func(ind, src string) {
			g.pf("%sif %s != 0 { // reserved(%d): a read rejects nonzero (SPEC §4.3)\n", ind, src, it.Bits)
			g.pf("%s\treturn ErrValidation\n%s}\n", ind, ind)
		},
	}
}

// flatFieldPiece classifies a scalar field. Fixed point and the 128-bit
// family stay on the per-field path and break the run: their value arithmetic
// lives in the runtime.
// flatArrayElemPiece classifies one element of an unrolled fixed array: the
// same scalar classification, against the element's own expression.
func (g *gen) flatArrayElemPiece(f *ir.Field, name string) (flatPiece, bool) {
	return g.flatScalarPiece(f, name)
}

func (g *gen) flatFieldPiece(f *ir.Field, base string) (flatPiece, bool) {
	return g.flatScalarPiece(f, base+ir.GoExportName(f.Name))
}

func (g *gen) flatScalarPiece(f *ir.Field, name string) (flatPiece, bool) {
	switch f.Type.Kind {
	case ir.TBool:
		return flatPiece{
			bits: 1, guard: noGuard,
			emit: func(ind string, idx int) {
				g.pf("%sf%d := uint64(0)\n", ind, idx)
				g.pf("%sif %s {\n%s\tf%d = 1\n%s}\n", ind, name, ind, idx, ind)
			},
			read: func(ind, src string) {
				g.pf("%s%s = %s != 0\n", ind, name, src)
			},
		}, true

	case ir.TBits:
		w := int64(f.Type.Width)
		storage := g.goFieldType(f.Type)
		return flatPiece{
			bits: w, guard: noGuard,
			emit: g.constEmit("uint64("+name+")", w, ""),
			read: func(ind, src string) {
				if storage == "uint64" {
					g.pf("%s%s = %s\n", ind, name, src)
					return
				}
				g.pf("%s%s = %s(%s)\n", ind, name, storage, src)
			},
		}, true

	// The float cases are the only classification that reaches the math
	// package, and they set needsMath from inside their emit/read closures —
	// at EMISSION, never here. Classification is speculative: flatGroupSize,
	// flatStructRun and the run accumulators all classify runs that may never
	// be emitted, and a run that falls back to the per-field form calls
	// serialize's own float entry points and imports no math. Setting the flag
	// at classification left an unused "math" import on every file whose float
	// runs all fell back — a bare Vec2 did not compile.
	case ir.TFloat32:
		if f.HasFloatRange {
			return g.flatCompressedPiece(f, name)
		}
		emit := g.constEmit("uint64(math.Float32bits("+name+"))", 32, "")
		return flatPiece{
			bits: 32, guard: noGuard,
			emit: func(ind string, idx int) {
				g.needsMath = true
				emit(ind, idx)
			},
			read: func(ind, src string) {
				g.needsMath = true
				g.pf("%s%s = math.Float32frombits(uint32(%s))\n", ind, name, src)
			},
		}, true

	case ir.TFloat64:
		emit := g.constEmit("math.Float64bits("+name+")", 64, "")
		return flatPiece{
			bits: 64, guard: noGuard,
			emit: func(ind string, idx int) {
				g.needsMath = true
				emit(ind, idx)
			},
			read: func(ind, src string) {
				g.needsMath = true
				g.pf("%s%s = math.Float64frombits(%s)\n", ind, name, src)
			},
		}, true

	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			bits := ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max))
			typeName := f.Type.Name
			max := ref.Max
			return flatPiece{
				bits: bits,
				guard: func(ind string, idx int) {
					g.pf("%senumValue%d := int32(%s)\n", ind, idx, name)
					g.pf("%sif enumValue%d < 0 || enumValue%d > %d {\n", ind, idx, idx, max)
					g.pf("%s\treturn serialize.ErrValueOutOfRange\n%s}\n", ind, ind)
				},
				emit: func(ind string, idx int) {
					g.pf("%sf%d := %s\n", ind, idx, masked(fmt.Sprintf("uint64(uint32(enumValue%d))", idx), bits))
				},
				read: func(ind, src string) {
					// a tag above the exported extent reaches the storage's bit
					// headroom, and a read rejects it (SPEC §4.2)
					if !rangeFillsBits(big.NewInt(max), bits) {
						g.pf("%sif %s > %d {\n", ind, src, max)
						g.pf("%s\treturn serialize.ErrValueOutOfRange\n%s}\n", ind, ind)
					}
					g.pf("%s%s = %s(int32(%s))\n", ind, name, typeName, src)
				},
			}, true

		case *ir.Flags:
			wire := int64(ref.WireBits)
			typeName := f.Type.Name
			return flatPiece{
				bits: wire,
				guard: func(ind string, idx int) {
					if ref.WireBits >= 64 {
						return
					}
					g.pf("%sif %s >= 1<<%d { // a mask bit above the wire width cannot ride\n", ind, name, ref.WireBits)
					g.pf("%s\treturn serialize.ErrValueOutOfRange\n%s}\n", ind, ind)
				},
				emit: g.constEmit("uint64("+name+")", wire, ""),
				read: func(ind, src string) {
					g.pf("%s%s = %s(%s)\n", ind, name, typeName, src)
				},
			}, true
		}
		return flatPiece{}, false // nested struct or union: its own call

	case ir.TInt:
		return g.flatIntPiece(f, name)
	}
	return flatPiece{}, false
}

// flatCompressedPiece folds a ranged float's quantization into the run. The
// arithmetic is emitWriteCompressedFold's and emitReadCompressedFold's,
// statement for statement — including the float32() conversions that force the
// intermediate rounding, which are load bearing on arm64 (see those functions).
func (g *gen) flatCompressedPiece(f *ir.Field, name string) (flatPiece, bool) {
	steps, bits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
	min32 := float32(f.FMin)
	delta := float32(f.FMax) - min32
	return flatPiece{
		bits: bits, guard: noGuard,
		emit: func(ind string, idx int) {
			g.pf("%sf%d := uint64(0)\n", ind, idx)
			g.pf("%s{\n", ind)
			if min32 == 0 {
				g.pf("%s\tnormalizedValue := %s / %s\n", ind, name, f32lit(delta))
			} else {
				g.pf("%s\tnormalizedValue := (%s - (%s)) / %s\n", ind, name, f32lit(min32), f32lit(delta))
			}
			g.pf("%s\tif !(normalizedValue >= 0) { // the runtime's clamp form — it forces NaN into range too\n", ind)
			g.pf("%s\t\tnormalizedValue = 0\n%s\t} else if !(normalizedValue <= 1) {\n%s\t\tnormalizedValue = 1\n%s\t}\n", ind, ind, ind, ind)
			g.pf("%s\tf%d = %s\n", ind, idx,
				masked(fmt.Sprintf("uint64(uint32(float32(normalizedValue*%s) + 0.5))", f32lit(float32(steps))), bits))
			g.pf("%s}\n", ind)
		},
		read: func(ind, src string) {
			if steps != uint64(1)<<uint(bits)-1 {
				g.pf("%sif %s > %d { // a value smuggled into the bit headroom is refused (SPEC §4.3)\n", ind, src, steps)
				g.pf("%s\treturn ErrValidation\n%s}\n", ind, ind)
			}
			g.pf("%s{\n%s\tnormalizedValue := float32(%s) / %s\n", ind, ind, src, f32lit(float32(steps)))
			if min32 == 0 {
				g.pf("%s\t%s = float32(normalizedValue * %s)\n%s}\n", ind, name, f32lit(delta), ind)
			} else {
				g.pf("%s\t%s = float32(normalizedValue*%s) + (%s)\n%s}\n", ind, name, f32lit(delta), f32lit(min32), ind)
			}
		},
	}, true
}

// flatIntPiece classifies an integer field across the wire paths the per-field
// form uses, so the folded offsets and the refusals are the same arithmetic in
// the same order.
func (g *gen) flatIntPiece(f *ir.Field, name string) (flatPiece, bool) {
	if f.Type.Width == 128 {
		return flatPiece{}, false // the 128-bit family stays on the runtime path
	}
	storage := goInt2(f.Type.Signed, f.Type.Width)

	if !f.HasIntRange {
		w := int64(f.Type.Width)
		var value string
		switch {
		case f.Type.Width == 64 && f.Type.Signed:
			value = "uint64(" + name + ")"
		case f.Type.Width == 64:
			value = name
		case !f.Type.Signed:
			value = "uint64(" + name + ")"
		case f.Type.Width == 32:
			value = "uint64(uint32(" + name + "))"
		default:
			// through the same-width unsigned so the sign bit cannot extend
			value = fmt.Sprintf("uint64(uint%d(%s))", f.Type.Width, name)
		}
		signed, width := f.Type.Signed, f.Type.Width
		return flatPiece{
			bits: w, guard: noGuard,
			emit: g.constEmit(value, w, ""),
			read: func(ind, src string) {
				switch {
				case width == 64 && signed:
					g.pf("%s%s = int64(%s)\n", ind, name, src)
				case width == 64:
					g.pf("%s%s = %s\n", ind, name, src)
				case signed && width < 32:
					// back through the same-width unsigned so the sign bit lands right
					g.pf("%s%s = int%d(uint%d(%s))\n", ind, name, width, width, src)
				default:
					g.pf("%s%s = %s(%s)\n", ind, name, storage, src)
				}
			},
		}, true
	}

	lo, hi := g.rangeArgs(f)
	loZero := f.IntMin.Sign() == 0

	// degenerate range: ZERO BITS — the value is known from the range alone
	// (SPEC §4.6). The write keeps its refusal; the read materializes.
	if f.IntMin.Cmp(f.IntMax) == 0 {
		return flatPiece{
			bits: 0,
			guard: func(ind string, idx int) {
				g.pf("%sif %s != %s {\n", ind, name, g.renderInt(f.IntMinExpr, f.IntMin))
				g.pf("%s\treturn serialize.ErrValueOutOfRange\n%s}\n", ind, ind)
			},
			read: func(ind, _ string) {
				g.pf("%s%s = %s(%s)\n", ind, name, storage, g.renderInt(f.IntMinExpr, f.IntMin))
			},
		}, true
	}

	bits := ir.BitsRequired(f.IntMin, f.IntMax)
	diff := new(big.Int).Sub(f.IntMax, f.IntMin)

	switch intRangePath(f.IntMin, f.IntMax) {
	case "int32":
		exprIs32 := f.Type.Signed && f.Type.Width == 32
		return flatPiece{
			bits: bits,
			guard: func(ind string, idx int) {
				src := name
				if !exprIs32 {
					g.pf("%srangeValue%d := int32(%s)\n", ind, idx, name)
					src = fmt.Sprintf("rangeValue%d", idx)
				}
				g.pf("%sif %s < %s || %s > %s {\n", ind, src, lo, src, hi)
				g.pf("%s\treturn serialize.ErrValueOutOfRange\n%s}\n", ind, ind)
			},
			emit: func(ind string, idx int) {
				src := name
				if !exprIs32 {
					src = fmt.Sprintf("rangeValue%d", idx)
				}
				expr := "uint64(uint32(" + src + "))"
				if !loZero {
					expr = "uint64(uint32(" + src + " - (" + lo + ")))"
				}
				g.pf("%sf%d := %s\n", ind, idx, masked(expr, bits))
			},
			read: func(ind, src string) {
				if !rangeFillsBits(diff, bits) {
					g.pf("%sif %s > %s { // a value smuggled into the bit headroom is refused (SPEC §4.3)\n", ind, src, diff.String())
					g.pf("%s\treturn serialize.ErrValueOutOfRange\n%s}\n", ind, ind)
				}
				if !loZero {
					// add in the unsigned domain, as the runtime does: the range
					// may be wider than 2^31. The bound binds to a typed local
					// first — converting a NEGATIVE typed constant to uint32 is a
					// compile error in Go, the same conversion of a variable is
					// not. Its own block keeps the name free for the next piece.
					g.pf("%s{\n%s\tlowValue := int32(%s)\n", ind, ind, lo)
					decoded := fmt.Sprintf("int32(uint32(%s) + uint32(lowValue))", src)
					if exprIs32 {
						g.pf("%s\t%s = %s\n%s}\n", ind, name, decoded, ind)
					} else {
						g.pf("%s\t%s = %s(%s)\n%s}\n", ind, name, storage, decoded, ind)
					}
					return
				}
				decoded := fmt.Sprintf("int32(%s)", src)
				if exprIs32 {
					g.pf("%s%s = %s\n", ind, name, decoded)
					return
				}
				g.pf("%s%s = %s(%s)\n", ind, name, storage, decoded)
			},
		}, true

	case "int64":
		exprIs64 := f.Type.Signed && f.Type.Width == 64
		return flatPiece{
			bits: bits,
			guard: func(ind string, idx int) {
				src := name
				if !exprIs64 {
					g.pf("%srangeValue%d := int64(%s)\n", ind, idx, name)
					src = fmt.Sprintf("rangeValue%d", idx)
				}
				g.pf("%sif %s < %s || %s > %s {\n", ind, src, lo, src, hi)
				g.pf("%s\treturn serialize.ErrValueOutOfRange\n%s}\n", ind, ind)
			},
			emit: func(ind string, idx int) {
				src := name
				if !exprIs64 {
					src = fmt.Sprintf("rangeValue%d", idx)
				}
				expr := "uint64(" + src + ")"
				if !loZero {
					expr = "uint64(" + src + " - (" + lo + "))"
				}
				g.pf("%sf%d := %s\n", ind, idx, masked(expr, bits))
			},
			read: func(ind, src string) {
				if !rangeFillsBits(diff, bits) {
					g.pf("%sif %s > %s { // a value smuggled into the bit headroom is refused (SPEC §4.3)\n", ind, src, diff.String())
					g.pf("%s\treturn serialize.ErrValueOutOfRange\n%s}\n", ind, ind)
				}
				if !loZero {
					g.pf("%s{\n%s\tlowValue := int64(%s)\n", ind, ind, lo)
					decoded := fmt.Sprintf("int64(%s + uint64(lowValue))", src)
					if exprIs64 {
						g.pf("%s\t%s = %s\n%s}\n", ind, name, decoded, ind)
					} else {
						g.pf("%s\t%s = %s(%s)\n%s}\n", ind, name, storage, decoded, ind)
					}
					return
				}
				decoded := fmt.Sprintf("int64(%s)", src)
				if exprIs64 {
					g.pf("%s%s = %s\n", ind, name, decoded)
					return
				}
				g.pf("%s%s = %s(%s)\n", ind, name, storage, decoded)
			},
		}, true
	}

	// full-range unsigned: raw offset bits over uint64 storage
	loVacuous := f.IntMin.Sign() == 0
	hiVacuous := f.IntMax.Cmp(maxUint64) == 0
	return flatPiece{
		bits: bits,
		guard: func(ind string, idx int) {
			// This path bypasses the runtime's ranged calls, so it supplies
			// their write-side range refusal; vacuous halves are elided.
			switch {
			case !loVacuous && !hiVacuous:
				g.pf("%sif %s < %s || %s > %s {\n", ind, name, lo, name, f.IntMax.String())
			case !loVacuous:
				g.pf("%sif %s < %s {\n", ind, name, lo)
			case !hiVacuous:
				g.pf("%sif %s > %s {\n", ind, name, f.IntMax.String())
			default:
				return
			}
			g.pf("%s\treturn serialize.ErrValueOutOfRange\n%s}\n", ind, ind)
		},
		emit: func(ind string, idx int) {
			expr := name
			if !loVacuous {
				expr = name + " - " + lo
			}
			g.pf("%sf%d := %s\n", ind, idx, masked(expr, bits))
		},
		read: func(ind, src string) {
			if diff.Cmp(maxUint64) != 0 {
				// a full-width diff cannot overflow its own read — elided
				g.pf("%sif %s > %s { // a read rejects out-of-range (SPEC §5)\n", ind, src, diff.String())
				g.pf("%s\treturn ErrValidation\n%s}\n", ind, ind)
			}
			if loVacuous {
				g.pf("%s%s = %s\n", ind, name, src)
				return
			}
			g.pf("%s%s = %s + %s\n", ind, name, src, lo)
		},
	}, true
}
