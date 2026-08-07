// Fixed-run planning: the generator-side lowering that packs consecutive
// statically-fixed-width wire elements into chunked stream operations, so a
// backend whose compiler cannot fold per-field stream calls (Go's inline
// budget, Rust's outlined wire functions) emits a handful of wide fused ops
// per message instead of one runtime call per field. This is the tiny-message
// twin of the const-emit doctrine (C++ schema#8): the GENERATOR is the
// constexpr evaluator — apple-clang folds a whole tiny message into one wide
// store at -O3, and this analysis hands the same folding to the targets whose
// optimizers never see the whole message in one place.
//
// The wire is unchanged BY CONSTRUCTION: the bit stream is defined as the
// LSB-first concatenation of the elements' bits (SPEC §4.3), and any chunking
// of the same bit sequence through the runtimes' bit packers produces
// byte-identical output — re-proven by the wire golden gate in every target.
//
// Error semantics are preserved exactly on the read side by the chunk
// boundary rule: a chunk never extends past the end of a FALLIBLE element
// (one whose read can reject: const, reserved, ranged int, enum), so every
// validation runs at exactly the wire position it runs at today, before any
// later bits are demanded from the stream. Truncation granularity within a
// chunk only ever groups INFALLIBLE bits with the fallible element that ends
// the chunk — every such packet class latches the same stream overflow error
// it latches today. The write side has no read-order hazard (range refusals
// are emitted per element, in wire order, before the chunk they feed).
package ir

import "math/big"

// RunOp is one fixed-width wire element inside a run.
type RunOp struct {
	Item     Item  // *FieldItem, *ConstItem or *ReservedItem
	Bits     int64 // exact wire bits (fixed by the schema, not the value)
	Fallible bool  // the READ side can reject this element after decoding it
}

// RunPiece places part (or all) of an op's bits into one chunk: bits
// [SrcShift, SrcShift+Bits) of the op's value land at [DstShift,
// DstShift+Bits) of chunk Chunk. LSB-first, like the wire itself.
type RunPiece struct {
	Chunk    int
	SrcShift int64
	DstShift int64
	Bits     int64
}

// PlannedOp pairs a run op with its chunk placement, in wire order.
type PlannedOp struct {
	Op     RunOp
	Pieces []RunPiece
}

// RunPlan is a run lowered onto chunks. ChunkBits[i] is the exact wire width
// of chunk i (1..cap). Ops are in wire order; a fallible op's last piece
// always ends its chunk when the plan was built with cutAtFallible.
type RunPlan struct {
	ChunkBits []int64
	Ops       []PlannedOp
}

// RunEligible classifies an item for run membership. Eligible items have a
// statically fixed wire width; everything else (arrays, strings, nested
// structs, compressed floats, branches, aligns, 128-bit families, the
// full-range unsigned path) breaks the run and stays on its existing
// emission. Alignment never joins a run because its width depends on the
// dynamic entry offset of the enclosing struct.
func RunEligible(item Item) (RunOp, bool) {
	switch it := item.(type) {
	case *ConstItem:
		if it.Bits >= 1 && it.Bits <= 64 {
			return RunOp{Item: item, Bits: it.Bits, Fallible: true}, true
		}
	case *ReservedItem:
		if it.Bits >= 1 && it.Bits <= 64 {
			return RunOp{Item: item, Bits: it.Bits, Fallible: true}, true
		}
	case *FieldItem:
		f := it.F
		if f.Array != ArrayNone {
			return RunOp{}, false
		}
		switch f.Type.Kind {
		case TInt:
			if f.HasIntRange {
				// the int32/int64 encodings only: the full-range unsigned
				// path keeps its own emission (its read rejection differs)
				if intRangeFamily(f) == "" {
					return RunOp{}, false
				}
				return RunOp{Item: item, Bits: BitsRequired(f.IntMin, f.IntMax), Fallible: true}, true
			}
			if f.Type.Width >= 8 && f.Type.Width <= 64 {
				return RunOp{Item: item, Bits: int64(f.Type.Width), Fallible: false}, true
			}
		case TBits:
			if f.Type.Width >= 1 && f.Type.Width <= 64 {
				return RunOp{Item: item, Bits: int64(f.Type.Width), Fallible: false}, true
			}
		case TBool:
			return RunOp{Item: item, Bits: 1, Fallible: false}, true
		case TFloat32:
			if !f.HasFloatRange {
				return RunOp{Item: item, Bits: 32, Fallible: false}, true
			}
		case TFloat64:
			return RunOp{Item: item, Bits: 64, Fallible: false}, true
		case TNamed:
			switch ref := f.Type.Ref.(type) {
			case *Enum:
				return RunOp{Item: item, Bits: BitsRequired(big.NewInt(0), big.NewInt(ref.Max)), Fallible: true}, true
			case *Flags:
				// the write-side mask refusal is emitted before the chunk;
				// the read side accepts any wire value, so not fallible
				return RunOp{Item: item, Bits: int64(ref.WireBits), Fallible: false}, true
			}
		}
	}
	return RunOp{}, false
}

// intRangeFamily is intRangePath's target-independent core: "int32", "int64",
// or "" for the full-range unsigned family.
func intRangeFamily(f *Field) string {
	if f.IntMin.IsInt64() && f.IntMax.IsInt64() {
		lo, hi := f.IntMin.Int64(), f.IntMax.Int64()
		if lo >= -2147483648 && hi <= 2147483647 {
			return "int32"
		}
		return "int64"
	}
	return ""
}

// PlanRun lowers ops onto chunks of at most capBits, splitting ops across
// chunk boundaries where needed. With cutAtFallible (the read side), a chunk
// closes at the end of every fallible op, so validations keep their exact
// wire positions relative to the stream bounds checks; without it (the write
// side), chunks fill to capBits.
func PlanRun(ops []RunOp, capBits int64, cutAtFallible bool) RunPlan {
	var plan RunPlan
	fill := int64(0) // bits in the open chunk; the open chunk is not yet appended
	chunk := 0
	for _, op := range ops {
		placed := int64(0)
		p := PlannedOp{Op: op}
		for placed < op.Bits {
			if fill == capBits {
				plan.ChunkBits = append(plan.ChunkBits, fill)
				chunk++
				fill = 0
			}
			take := min(capBits-fill, op.Bits-placed)
			p.Pieces = append(p.Pieces, RunPiece{
				Chunk:    chunk,
				SrcShift: placed,
				DstShift: fill,
				Bits:     take,
			})
			fill += take
			placed += take
		}
		plan.Ops = append(plan.Ops, p)
		if cutAtFallible && op.Fallible && fill > 0 {
			plan.ChunkBits = append(plan.ChunkBits, fill)
			chunk++
			fill = 0
		}
	}
	if fill > 0 {
		plan.ChunkBits = append(plan.ChunkBits, fill)
	}
	return plan
}
