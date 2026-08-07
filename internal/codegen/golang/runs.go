// Fixed-run emission for the Go target: consecutive statically-fixed-width
// items lower onto TryWriteBits64/TryReadBits chunks (ir.PlanRun), so a tiny
// message costs a handful of fused, fully-inlined stream ops instead of one
// outlined runtime call per field — the Go rendering of apple-clang's
// whole-header folding on the C++ target (see internal/ir/runs.go for the
// doctrine and the wire/error-semantics argument).
//
// Write chunks fill to 64 bits; a failed chunk write LATCHES overflow and
// falls through (stream.Fail without return), which preserves the existing
// generated code's error precedence exactly: refusal checks of later fields
// still return their own errors after an earlier overflow, and the final
// `return stream.Err()` surfaces the latch. Read chunks are 32-bit window
// reads cut at every fallible element (ir.PlanRun cutAtFallible), overflow
// returns immediately with the same latched ErrOverflow the runtime latches
// today, and every validation runs in wire order at its existing position.
package golang

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/internal/ir"
)

// gatherRun collects the maximal run of eligible items starting at items[i],
// returning the ops and the index just past the run.
func gatherRun(items []ir.Item, i int) ([]ir.RunOp, int) {
	var ops []ir.RunOp
	for i < len(items) {
		op, ok := ir.RunEligible(items[i])
		if !ok {
			break
		}
		ops = append(ops, op)
		i++
	}
	return ops, i
}

// maskLit renders ((1<<bits)-1) as a hex literal; bits must be in [1,64].
func maskLit(bits int64) string {
	if bits >= 64 {
		return "0xffffffffffffffff"
	}
	return fmt.Sprintf("0x%x", (uint64(1)<<bits)-1)
}

// runFieldName renders the storage reference for a run op's field.
func runFieldName(op ir.RunOp) string {
	return "value." + ir.GoExportName(op.Item.(*ir.FieldItem).F.Name)
}

// ---- write side ----

// emitWriteRun lowers one run to refusal checks + chunk writes. Emitted
// inside its own block so temp names stay local.
func (g *gen) emitWriteRun(ops []ir.RunOp, ind string) {
	plan := ir.PlanRun(ops, 64, false)
	g.pf("%s{ // fixed run: %d ops -> %d fused chunk write(s), wire-identical (SPEC §4.3; ir.PlanRun)\n", ind, len(ops), len(plan.ChunkBits))
	in := ind + "\t"

	// per-chunk: refusal checks for ops whose first piece lands in the
	// chunk (wire order), then the chunk build and its fused write
	for chunk := range plan.ChunkBits {
		for i, p := range plan.Ops {
			if p.Pieces[0].Chunk == chunk {
				g.emitRunWriteChecks(p.Op, i, in)
			}
		}
		g.emitRunChunkWrite(plan, chunk, in)
	}
	g.pf("%s}\n", ind)
}

// emitRunWriteChecks emits the write-side refusal checks and the masked
// uint64 value temp (runV<i>) for one op, exactly the checks the existing
// per-field emission performs, in the same order.
func (g *gen) emitRunWriteChecks(op ir.RunOp, i int, ind string) {
	v := fmt.Sprintf("runV%d", i)
	switch it := op.Item.(type) {
	case *ir.ConstItem:
		g.pf("%s%s := uint64(%s) // const(%s, %d) — SPEC §4.3\n", ind, v, it.Value.String(), it.Value.String(), it.Bits)
	case *ir.ReservedItem:
		// zeros on the wire: contributes nothing to the chunk
	case *ir.FieldItem:
		f := it.F
		name := runFieldName(op)
		switch f.Type.Kind {
		case ir.TInt:
			if f.HasIntRange {
				lo, hi := g.rangeArgs(f)
				g.pf("%sif %s < %s || %s > %s { // the runtime range refusal, folded (SPEC §5)\n%s\treturn serialize.ErrValueOutOfRange\n%s}\n",
					ind, name, lo, name, hi, ind, ind)
				family := goInt2(true, 32)
				if intRangePathFamily(f) == "int64" {
					family = goInt2(true, 64)
				}
				if f.IntMin.Sign() == 0 {
					g.pf("%s%s := uint64(%s(%s))\n", ind, v, family, name)
				} else {
					g.pf("%s%s := uint64(%s(%s) - (%s)) & %s // offset from min, unsigned domain\n",
						ind, v, family, name, lo, maskLit(op.Bits))
				}
				return
			}
			if f.Type.Signed {
				g.pf("%s%s := uint64(uint%d(%s))\n", ind, v, f.Type.Width, name)
			} else {
				g.pf("%s%s := uint64(%s)\n", ind, v, name)
			}
		case ir.TBits:
			if op.Bits == 64 {
				g.pf("%s%s := %s\n", ind, v, name)
			} else {
				g.pf("%s%s := uint64(%s) & %s\n", ind, v, name, maskLit(op.Bits))
			}
		case ir.TBool:
			g.pf("%s%s := uint64(0)\n%sif %s {\n%s\t%s = 1\n%s}\n", ind, v, ind, name, ind, v, ind)
		case ir.TFloat32:
			g.needsMath = true
			g.pf("%s%s := uint64(math.Float32bits(%s))\n", ind, v, name)
		case ir.TFloat64:
			g.needsMath = true
			g.pf("%s%s := math.Float64bits(%s)\n", ind, v, name)
		case ir.TNamed:
			switch ref := f.Type.Ref.(type) {
			case *ir.Enum:
				g.pf("%sif int32(%s) < 0 || int32(%s) > %d { // the runtime range refusal, folded (SPEC §5)\n%s\treturn serialize.ErrValueOutOfRange\n%s}\n",
					ind, name, name, ref.Max, ind, ind)
				g.pf("%s%s := uint64(int32(%s))\n", ind, v, name)
			case *ir.Flags:
				if ref.WireBits < 64 {
					g.pf("%sif %s >= 1<<%d { // a mask bit above the wire width cannot ride\n%s\treturn serialize.ErrValueOutOfRange\n%s}\n",
						ind, name, ref.WireBits, ind, ind)
				}
				g.pf("%s%s := uint64(%s)\n", ind, v, name)
			}
		}
	}
}

// emitRunChunkWrite builds chunk <chunk> from the ops' pieces and emits the
// fused write. A failed write latches overflow and falls through — the
// existing precedence (writes latch and continue; checks return).
func (g *gen) emitRunChunkWrite(plan ir.RunPlan, chunk int, ind string) {
	var parts []string
	for i, p := range plan.Ops {
		if _, isReserved := p.Op.Item.(*ir.ReservedItem); isReserved {
			continue // zeros: no contribution
		}
		v := fmt.Sprintf("runV%d", i)
		for _, piece := range p.Pieces {
			if piece.Chunk != chunk {
				continue
			}
			expr := v
			if piece.SrcShift > 0 {
				expr = fmt.Sprintf("%s>>%d", v, piece.SrcShift)
			}
			if piece.SrcShift+piece.Bits < p.Op.Bits {
				// an interior piece: mask off the bits that belong to the
				// next chunk
				expr = fmt.Sprintf("%s&%s", expr, maskLit(piece.Bits))
			}
			if piece.DstShift > 0 {
				expr = fmt.Sprintf("%s<<%d", expr, piece.DstShift)
			}
			parts = append(parts, expr)
		}
	}
	name := fmt.Sprintf("runChunk%d", chunk)
	if len(parts) == 0 {
		g.pf("%s%s := uint64(0)\n", ind, name)
	} else {
		g.pf("%s%s := %s\n", ind, name, strings.Join(parts, " | "))
	}
	g.pf("%sif !stream.TryWriteBits64(%s, %d) {\n%s\tstream.Fail(serialize.ErrOverflow) // latch and fall through: writes latch, checks return (§6.3)\n%s}\n",
		ind, name, plan.ChunkBits[chunk], ind, ind)
}

// ---- read side ----

// emitReadRun lowers one run to fused chunk reads + in-order extraction and
// validation. Chunks are cut at fallible ops (ir.PlanRun), so every
// validation runs at exactly its current wire position.
func (g *gen) emitReadRun(ops []ir.RunOp, ind string) {
	plan := ir.PlanRun(ops, 32, true)
	g.pf("%s{ // fixed run: %d ops <- %d fused chunk read(s), wire-identical (SPEC §4.3; ir.PlanRun)\n", ind, len(ops), len(plan.ChunkBits))
	in := ind + "\t"
	for chunk, bits := range plan.ChunkBits {
		g.pf("%srunChunk%d, ok := stream.TryReadBits(%d)\n", in, chunk, bits)
		g.pf("%sif !ok {\n%s\treturn stream.Fail(serialize.ErrOverflow)\n%s}\n", in, in, in)
		// ops whose LAST piece is in this chunk have all their bits: extract,
		// validate, assign — in wire order
		for i, p := range plan.Ops {
			if p.Pieces[len(p.Pieces)-1].Chunk == chunk {
				g.emitRunReadOp(plan, i, in)
			}
		}
	}
	g.pf("%s}\n", ind)
}

// runExtract renders the op's decoded bits from its chunk pieces: a uint32
// expression for ops of 32 bits or fewer, uint64 above.
func runExtract(p ir.PlannedOp) string {
	wide := p.Op.Bits > 32
	var parts []string
	for _, piece := range p.Pieces {
		expr := fmt.Sprintf("runChunk%d", piece.Chunk)
		if piece.DstShift > 0 {
			expr = fmt.Sprintf("%s>>%d", expr, piece.DstShift)
		}
		if piece.DstShift+piece.Bits < 32 {
			expr = fmt.Sprintf("%s&%s", expr, maskLit(piece.Bits))
		}
		if wide {
			expr = "uint64(" + expr + ")"
		}
		if piece.SrcShift > 0 {
			expr = fmt.Sprintf("%s<<%d", expr, piece.SrcShift)
		}
		parts = append(parts, expr)
	}
	return strings.Join(parts, " | ")
}

// emitRunReadOp extracts, validates and assigns one op, reproducing the
// existing per-field read semantics: overflow already returned, validation
// errors return in wire order (const/reserved return ErrValidation; ranged
// and enum latch ErrValueOutOfRange through stream.Fail).
func (g *gen) emitRunReadOp(plan ir.RunPlan, i int, ind string) {
	p := plan.Ops[i]
	extract := runExtract(p)
	switch it := p.Op.Item.(type) {
	case *ir.ConstItem:
		g.pf("%sif %s != %s { // const(%s, %d): a read rejects any other value (SPEC §4.3)\n%s\treturn ErrValidation\n%s}\n",
			ind, extract, it.Value.String(), it.Value.String(), it.Bits, ind, ind)
	case *ir.ReservedItem:
		g.pf("%sif %s != 0 { // reserved(%d): a read rejects nonzero (SPEC §4.3)\n%s\treturn ErrValidation\n%s}\n",
			ind, extract, it.Bits, ind, ind)
	case *ir.FieldItem:
		f := it.F
		name := runFieldName(p.Op)
		v := fmt.Sprintf("runV%d", i)
		switch f.Type.Kind {
		case ir.TInt:
			if f.HasIntRange {
				g.emitRunReadRanged(f, name, v, extract, p.Op.Bits, ind)
				return
			}
			switch {
			case f.Type.Width == 64 && f.Type.Signed:
				g.pf("%s%s = int64(%s)\n", ind, name, extract)
			case f.Type.Width == 64:
				g.pf("%s%s = %s\n", ind, name, extract)
			case f.Type.Signed:
				// back through the same-width unsigned so the sign bit lands right
				g.pf("%s%s = int%d(uint%d(%s))\n", ind, name, f.Type.Width, f.Type.Width, extract)
			case f.Type.Width == 32:
				g.pf("%s%s = %s\n", ind, name, extract)
			default:
				g.pf("%s%s = uint%d(%s)\n", ind, name, f.Type.Width, extract)
			}
		case ir.TBits:
			if f.Type.Width <= 32 {
				g.pf("%s%s = %s\n", ind, name, extract)
			} else {
				g.pf("%s%s = %s\n", ind, name, extract)
			}
		case ir.TBool:
			g.pf("%s%s = %s != 0\n", ind, name, extract)
		case ir.TFloat32:
			g.needsMath = true
			g.pf("%s%s = math.Float32frombits(%s)\n", ind, name, extract)
		case ir.TFloat64:
			g.needsMath = true
			g.pf("%s%s = math.Float64frombits(%s)\n", ind, name, extract)
		case ir.TNamed:
			switch ref := f.Type.Ref.(type) {
			case *ir.Enum:
				g.pf("%s%s := %s\n", ind, v, extract)
				g.pf("%sif %s > %d { // a read rejects out-of-range (SPEC §5)\n%s\treturn stream.Fail(serialize.ErrValueOutOfRange)\n%s}\n",
					ind, v, ref.Max, ind, ind)
				g.pf("%s%s = %s(%s)\n", ind, name, f.Type.Name, v)
			case *ir.Flags:
				g.pf("%s%s = %s(%s)\n", ind, name, f.Type.Name, extract)
			}
		}
	}
}

// emitRunReadRanged validates and assigns a ranged integer decoded from a
// run: the runtime SerializeInt/SerializeInt64 semantics exactly (compare
// and add in the unsigned domain; smuggled headroom values latch
// ErrValueOutOfRange).
func (g *gen) emitRunReadRanged(f *ir.Field, name, v, extract string, bits int64, ind string) {
	diff := new(big.Int).Sub(f.IntMax, f.IntMin)
	g.pf("%s%s := %s\n", ind, v, extract)
	g.pf("%sif %s > %s { // a read rejects smuggled headroom values (SPEC §5)\n%s\treturn stream.Fail(serialize.ErrValueOutOfRange)\n%s}\n",
		ind, v, diff.String(), ind, ind)
	if intRangePathFamily(f) == "int64" {
		// unsigned-domain add, exactly SerializeInt64's reconstruction
		var uMin uint64
		if f.IntMin.Sign() >= 0 {
			uMin = f.IntMin.Uint64()
		} else {
			uMin = uint64(f.IntMin.Int64())
		}
		inner := v
		if uMin != 0 {
			inner = fmt.Sprintf("%s + %d", v, uMin) // uint64(min), two's complement
		}
		if f.Type.Signed && f.Type.Width == 64 {
			g.pf("%s%s = int64(%s)\n", ind, name, inner)
		} else {
			g.pf("%s%s = %s(int64(%s))\n", ind, name, goInt2(f.Type.Signed, f.Type.Width), inner)
		}
		return
	}
	// int32 family: uint32 domain
	uMin := uint32(f.IntMin.Int64())
	inner := v
	if bits > 32 {
		inner = "uint32(" + v + ")"
	}
	if uMin != 0 {
		inner = fmt.Sprintf("%s + %d", inner, uMin) // uint32(min), two's complement
	}
	if f.Type.Signed && f.Type.Width == 32 {
		g.pf("%s%s = int32(%s)\n", ind, name, inner)
	} else {
		g.pf("%s%s = %s(int32(%s))\n", ind, name, goInt2(f.Type.Signed, f.Type.Width), inner)
	}
}

// intRangePathFamily mirrors intRangePath for run emission ("int32"/"int64").
func intRangePathFamily(f *ir.Field) string {
	return intRangePath(f.IntMin, f.IntMax)
}
