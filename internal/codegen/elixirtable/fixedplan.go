package elixirtable

import (
	"math/big"
	"strconv"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE IDENTITY PLAN, built here and coalesced here (docs/SPEC-TABLES.md §3.4).
//
// "THE IDENTITY PLAN IS A STATIC CONSTANT BAKED INTO THE GENERATED READER."
// This file is what bakes it: a walk of the type's fields in the image domain,
// where a field's source and destination are the same number because the
// reader's own storage IS the record image, and then the same coalescer the
// runtime's plan compiler runs — "coalescing is the only optimization a plan
// compiler performs and it is performed identically on both sides", so it is
// literally the same rule written once per side rather than two rules that have
// to be kept in step.
//
// EVERY ENTRY IS A `copy`, WHICH IS WHAT §3.4 SAYS, and this port now says it
// too. It did not always: the walk used to descend to the LEAVES and emit the
// `count`, `text` and `clamp` ops there, so a record of a hundred ranged
// integers arrived as a plan of a hundred entries and the interpreter ran over
// every one of them. The check itself was never the cost — the entries were.
//
// So on THE IDENTITY PATH the clamp rides the generated decode instead: the
// count's bound, the text length's bound and the ranged integer's bounds are
// straight-line code in the one binary pattern match that projects the image,
// and the `clamped` they count is added to the report beside it (Rowan's ruling
// for this leg). THE CHECK IS NOT WEAKENED AND NOT MOVED OFF THE READ — the
// read side always checks — it is moved OFF THE PLAN, which on this path is
// pure overhead: an op whose source and destination are the same byte.
//
// A COMPILED PLAN IS UNTOUCHED. There the interpreter runs anyway, the source
// and the destination are different layouts, and `clamp` is how a foreign
// writer's value is held to this reader's bounds — FixedRuntime.compile still
// emits it, and the decode's own clamp is then a no-op that counts nothing
// because the value it sees is already in range.
//
// WHAT IS LEFT IS ONE RUN. In the image domain any contiguous region is one
// identity copy, so the walk below is one entry per FIELD and the coalescer
// merges them; the reference walks LEAVES because ITS destination is C++
// storage, where a field's offset and its wire offset advance differently.
// Here they do not. A union arm needs no guard for the same reason: an arm's
// bytes are its own bytes whether or not the tag names them, and the projection
// reads the arm the TAG names and no other.

// fixedPlanEntry is one entry of a plan as this port spells it: a tagged tuple,
// so THE ONE READ LOOP dispatches on a function head — the BEAM's own fastest
// dispatch — rather than on a lane read.
type fixedPlanEntry struct {
	items []string // the tuple's elements
	op    string   // "copy", for the coalescer
	src   int64
	dst   int64
	size  int64
}

func (e fixedPlanEntry) String() string { return e.render(0, 0) }

// render spells the entry at a column, breaking the tuple where the formatter
// would.
func (e fixedPlanEntry) render(col, tail int) string {
	return tupleNode{items: e.items}.render(col, col, tail)
}

// fixedCopy is the one entry an identity plan carries.
func fixedCopy(src, dst, size int64) fixedPlanEntry {
	return fixedPlanEntry{
		items: []string{":copy", itoa(src), itoa(dst), itoa(size)},
		op:    "copy",
		src:   src,
		dst:   dst,
		size:  size,
	}
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// fixedCoalesce merges two neighbouring COPY entries whose source and
// destination both advance together, which is the runtime's own rule.
func fixedCoalesce(in []fixedPlanEntry) []fixedPlanEntry {
	out := make([]fixedPlanEntry, 0, len(in))
	for _, e := range in {
		if n := len(out); n > 0 && e.op == "copy" && out[n-1].op == "copy" &&
			out[n-1].src+out[n-1].size == e.src && out[n-1].dst+out[n-1].size == e.dst {
			out[n-1].size += e.size
			out[n-1].items = []string{":copy", itoa(out[n-1].src), itoa(out[n-1].dst), itoa(out[n-1].size)}
			continue
		}
		out = append(out, e)
	}
	return out
}

// fixedRangeOf answers a field's declared clamp bounds, as Elixir literals, or
// two "nil"s when the field carries no range. THE BOUNDS DO NOT RIDE and a
// value outside the reader's own range CLAMPS ON LOAD, counting one `clamped` —
// §3.4's op table, and §3's rule for the same kinds. The bounds are the same
// numbers whether the clamp rides a compiled plan's entry or the identity
// path's generated decode, which is why both sides read them from here.
func fixedRangeOf(f *ir.Field) (string, string) {
	if !f.HasIntRange || f.IntMin == nil || f.IntMax == nil {
		return "nil", "nil"
	}
	lo, hi := f.IntMin, f.IntMax
	if f.Type.Kind == ir.TFixed && f.Type.FracBits > 0 {
		// A FIXED-POINT FIELD'S DECLARED BOUNDS ARE IN VALUE UNITS AND ITS IMAGE
		// IS THE RAW SCALED INTEGER (§3.4), so the clamp's bounds are the
		// declared ones SCALED — clamping a raw Q24.8 against a bound of 65535
		// would hold the value to 255.99 and call it a range.
		scale := new(big.Int).Lsh(big.NewInt(1), uint(f.Type.FracBits))
		lo = new(big.Int).Mul(lo, scale)
		hi = new(big.Int).Mul(hi, scale)
	}
	return fixedIntLiteral(lo), fixedIntLiteral(hi)
}

// fixedFlatType reports a type whose IMAGE IS A RUN OF PLAIN BYTES WITH NOTHING
// TO CHECK IN IT: no count to clamp, no text length, no union tag to dispatch
// on, no present flag and no ranged integer anywhere in it. It is not the
// PLAN's question any more — every field is a run on the identity path — it is
// the DECODE's: a type this answers for is projected INSIDE its holder's own
// binary pattern match rather than through a call, and it can never move the
// `clamped` counter.
func fixedFlatType(st *ir.Struct) bool {
	for _, f := range st.Fields {
		if !fixedFlatField(f) {
			return false
		}
	}
	return true
}

func fixedFlatField(f *ir.Field) bool {
	if f.Type.Optional || f.Array == ir.ArrayCounted || f.HasIntRange {
		return false
	}
	switch f.Type.Kind {
	case ir.TString, ir.TWString, ir.TBytes, ir.TMap:
		return false
	}
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return fixedFlatType(r)
		case *ir.Union:
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// THE FIELD WALK, in the image domain
// ---------------------------------------------------------------------------

type fixedLeafWalk struct {
	out []fixedPlanEntry
}

func (w *fixedLeafWalk) push(e fixedPlanEntry) { w.out = append(w.out, e) }

// fixedTypeLeaves walks one TYPE's fields at a base the caller gives. A field's
// whole image — its present byte, its count, its text buffer, its array, its
// union tag and every arm behind it — is ONE identity copy, because on this
// path the destination is the source.
func fixedTypeLeaves(w *fixedLeafWalk, st *ir.Struct, base int64) {
	at := base
	for _, f := range st.Fields {
		n := fixedFieldBytes(f)
		w.push(fixedCopy(at, at, n))
		at += n
	}
}

// fixedIdentityPlan is the whole of a root's identity plan, as Elixir: the
// field walk, then the coalescer. THE ONE RUN IS A RESULT AND NOT AN ASSERTION
// — a layout whose fields ever stopped being contiguous would show up here as a
// second entry rather than as silence.
func fixedIdentityPlan(st *ir.Struct) []fixedPlanEntry {
	w := &fixedLeafWalk{}
	fixedTypeLeaves(w, st, 0)
	return fixedCoalesce(w.out)
}
