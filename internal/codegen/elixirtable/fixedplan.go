package elixirtable

import (
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE IDENTITY PLAN, built here and coalesced here (docs/SPEC-TABLES.md §3.4).
//
// "THE IDENTITY PLAN IS A STATIC CONSTANT BAKED INTO THE GENERATED READER."
// This file is what bakes it: a walk of the type's LEAVES in the image domain,
// where a leaf's source and destination are the same number because the
// reader's own storage IS the record image, and then the same coalescer the
// runtime's plan compiler runs — "coalescing is the only optimization a plan
// compiler performs and it is performed identically on both sides", so it is
// literally the same rule written once per side rather than two rules that have
// to be kept in step.
//
// WHAT THE REFERENCE'S IDENTITY PLAN CARRIES, and §3.4 says something slightly
// different from what the reference does: the page says "every entry a `copy`",
// and internal/codegen/cpptable/fixedform.go's own leaf walk emits `count` and
// `text` entries for a counted array and for a string. The reference is right
// and the sentence is loose — a length that arrived hostile has to be clamped
// whoever wrote it — so this port emits `count` and `text` on the identity path
// too, and adds `clamp` where §3.4's op table says a RANGED integer clamps on
// load. Clamp and ordinal count LIVE elements only: a counted array's slack
// and an absent optional's payload move no counter, the same as C++ walking
// items_count. That is stated rather than left to be found.

// fixedPlanEntry is one entry of a plan as this port spells it: a tagged tuple,
// so THE ONE READ LOOP dispatches on a function head — the BEAM's own fastest
// dispatch — rather than on a lane read.
type fixedPlanEntry struct {
	items []string // the tuple's elements
	op    string   // "copy" and the rest, for the coalescer
	src   int64
	dst   int64
	size  int64

	guard    string // "" outside a union arm
	guardSrc string
	guardTag string

	// lives and present wrap COUNTER-MOVING ops so slack and an absent
	// optional's payload never count. A copy is unspecified on read and is
	// not wrapped. C++ walks items_count / present on the storage pass;
	// this plan is static, so the skip is a runtime guard on the same numbers.
	lives      []liveRef
	hasPresent bool
	presentSrc int64
}

type liveRef struct {
	dst, index int64
}

func (e fixedPlanEntry) String() string { return e.render(0, 0) }

// render spells the entry at a column, breaking the tuple where the formatter
// would — a 128-bit range is the one bound that outgrows a line.
func (e fixedPlanEntry) render(col, tail int) string {
	s := tupleNode{items: e.items}.render(col, col, tail)
	if e.guard != "" {
		// AN ENTRY BELONGING TO AN ARM is wrapped in its guard.
		s = tupleNode{items: []string{":guard", e.guardSrc, e.guardTag, s}}.render(col, col, tail)
	}
	if e.hasPresent {
		s = tupleNode{items: []string{":present", itoa(e.presentSrc), s}}.render(col, col, tail)
	}
	// OUTERMOST LIVE IS THE PARENT ARRAY, so a slack slot never inspects a
	// nested count, a present flag or a union tag that nobody wrote.
	for _, live := range slices.Backward(e.lives) {
		s = tupleNode{items: []string{":live", itoa(live.dst), itoa(live.index), s}}.render(col, col, tail)
	}
	return s
}

// fixedCopy is the one entry the coalescer merges.
func fixedCopy(src, dst, size int64, guard string) fixedPlanEntry {
	e := fixedPlanEntry{
		items: []string{":copy", itoa(src), itoa(dst), itoa(size)},
		op:    "copy",
		src:   src,
		dst:   dst,
		size:  size,
	}
	return e.under(guard)
}

func fixedOther(items []string, op string, guard string) fixedPlanEntry {
	e := fixedPlanEntry{items: items, op: op}
	return e.under(guard)
}

// under puts an entry inside a union arm's guard, spelled "src:tag".
func (e fixedPlanEntry) under(guard string) fixedPlanEntry {
	if guard == "" {
		return e
	}
	src, tag, _ := strings.Cut(guard, ":")
	e.guard, e.guardSrc, e.guardTag = guard, src, tag
	return e
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// fixedCoalesce merges two neighbouring COPY entries whose source and
// destination both advance together, which is the runtime's own rule.
func fixedCoalesce(in []fixedPlanEntry) []fixedPlanEntry {
	out := make([]fixedPlanEntry, 0, len(in))
	for _, e := range in {
		if n := len(out); n > 0 && e.op == "copy" && out[n-1].op == "copy" &&
			out[n-1].guard == e.guard &&
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
// two empty strings when the field carries no range. THE BOUNDS DO NOT RIDE and
// a value outside the reader's own range CLAMPS ON LOAD, counting one
// `clamped` — §3.4's op table, and §3's rule for the same kinds.
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

// fixedFlatType reports a type whose IMAGE IS A RUN OF PLAIN BYTES: no count to
// clamp, no text length, no union tag to guard on, no present flag and no
// ranged integer anywhere in it. An array of such a type is ONE run however
// many elements it has, which is what keeps a plan small and what makes the
// whole of a big array one move.
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
		case *ir.Enum:
			// AN ORDINAL IS HELD TO THE VARIANTS THE WRITER DECLARED, which is
			// a check and not a move, so an enum is not a plain byte run.
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// THE LEAF WALK, in the image domain
// ---------------------------------------------------------------------------

type fixedLeafWalk struct {
	out []fixedPlanEntry
}

func (w *fixedLeafWalk) push(e fixedPlanEntry) { w.out = append(w.out, e) }

type leafCtx struct {
	guard      string
	lives      []liveRef
	hasPresent bool
	presentSrc int64
}

// applyCtx hangs LIVE / present / arm guards on a counter-moving op. A copy
// is unspecified on read (§3.4) and is not wrapped, so slack bytes that
// nobody wrote do not become a clamp.
func applyCtx(e fixedPlanEntry, ctx leafCtx) fixedPlanEntry {
	e = e.under(ctx.guard)
	if e.op == "copy" {
		return e
	}
	if n := len(ctx.lives); n > 0 {
		e.lives = append([]liveRef(nil), ctx.lives...)
	}
	e.hasPresent = ctx.hasPresent
	e.presentSrc = ctx.presentSrc
	return e
}

// fixedTypeLeaves walks one TYPE's leaves at a base the caller gives.
func fixedTypeLeaves(w *fixedLeafWalk, st *ir.Struct, base int64, ctx leafCtx) {
	at := base
	for _, f := range st.Fields {
		fixedFieldLeaves(w, f, at, ctx)
		at += fixedFieldBytes(f)
	}
}

func fixedFieldLeaves(w *fixedLeafWalk, f *ir.Field, at int64, ctx leafCtx) {
	base := at
	if f.Type.Optional {
		// THE PRESENT BYTE IS COPIED. Payload ops wrap :present so an absent
		// optional's payload is ignored on read and moves no counter (§3.4).
		w.push(applyCtx(fixedCopy(base, base, fixedPresentBytes, ""), ctx))
		ctx.hasPresent = true
		ctx.presentSrc = base
		base += fixedPresentBytes
	}
	switch {
	case f.KeyEnum != "":
		fixedElementLoop(w, f, base, f.KeyEnumRef.Max, ctx, -1)
	case f.Array == ir.ArrayFixed:
		fixedElementLoop(w, f, base, f.ArrayBound, ctx, -1)
	case f.Array == ir.ArrayCounted:
		w.push(applyCtx(fixedOther([]string{":count", itoa(base), itoa(base), itoa(f.ArrayBound)}, "count", ""), ctx))
		fixedElementLoop(w, f, base+fixedCountBytes, f.ArrayBound, ctx, base)
	case f.Type.Kind == ir.TString:
		fixedTextLeaf(w, base, f.Type.Size, fixedTextUtf8, ctx)
	case f.Type.Kind == ir.TWString:
		fixedTextLeaf(w, base, 2*f.Type.Size, fixedTextWide, ctx)
	case f.Type.Kind == ir.TBytes:
		fixedTextLeaf(w, base, f.Type.Size, fixedTextBytes, ctx)
	default:
		fixedElementLeaves(w, f, base, ctx)
	}
}

func fixedTextLeaf(w *fixedLeafWalk, base, size int64, flavour int, ctx leafCtx) {
	w.push(applyCtx(fixedOther([]string{":text", itoa(base), itoa(base), itoa(base + fixedCountBytes), itoa(size), strconv.Itoa(flavour)}, "text", ""), ctx))
}

// fixedOrdinalLeaf emits the op that holds an ORDINAL — an enum variant's, or
// a union tag's — to the variants the writer declared. THE IDENTITY PLAN NEEDS
// NO REMAP, so where a compiled plan carries the writer's table this carries
// the plain VARIANT COUNT, which is the same check written in one number.
func fixedOrdinalLeaf(w *fixedLeafWalk, at, size int64, variants int, ctx leafCtx) {
	w.push(applyCtx(fixedOther([]string{":ordinal", itoa(at), itoa(at), itoa(size), itoa(size), strconv.Itoa(variants)}, "ordinal", ""), ctx))
}

func fixedElementLoop(w *fixedLeafWalk, f *ir.Field, base, count int64, ctx leafCtx, liveDst int64) {
	elem := fixedElementBytes(f)
	if fixedFlatElem(f) {
		// THE WHOLE ARRAY IS ONE RUN: its image is a run of plain bytes, so the
		// elements need no walk at all and the plan carries ONE entry however
		// many of them there are. A copy moves no counter.
		w.push(applyCtx(fixedCopy(base, base, count*elem, ""), ctx))
		return
	}
	for i := range count {
		c := ctx
		if liveDst >= 0 {
			c.lives = append(append([]liveRef(nil), ctx.lives...), liveRef{dst: liveDst, index: i})
		}
		fixedElementLeaves(w, f, base+i*elem, c)
	}
}

func fixedFlatElem(f *ir.Field) bool {
	if f.HasIntRange {
		return false
	}
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return fixedFlatType(r)
		case *ir.Union:
			return false
		case *ir.Enum:
			return false
		}
	}
	switch f.Type.Kind {
	case ir.TString, ir.TWString, ir.TBytes, ir.TMap:
		return false
	}
	return true
}

// fixedElementLeaves emits ONE element's leaves at an absolute offset.
func fixedElementLeaves(w *fixedLeafWalk, f *ir.Field, at int64, ctx leafCtx) {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			fixedTypeLeaves(w, r, at, ctx)
			return
		case *ir.Union:
			// THE TAG, then each arm under its own guard: an entry belonging to
			// an arm runs only when the tag it was compiled for is the tag the
			// record carries.
			tag := fixedUnionTagBytes(r)
			fixedOrdinalLeaf(w, at, tag, len(r.Variants), ctx)
			for i, v := range r.Variants {
				arm := ctx
				arm.guard = fmt.Sprintf("%d:%d", at, i+1)
				fixedElementLeaves(w, v.F, at+tag, arm)
			}
			return
		case *ir.Enum:
			// AN ORDINAL PAST THE LAST VARIANT IS NOT A VARIANT: it lands None
			// and counts one `clamped`, which is the same answer §3 gives a
			// value outside its range. Nothing but a plan op can say so, so on
			// this path the ordinal is an op and not a byte of a run.
			fixedOrdinalLeaf(w, at, fixedElementBytes(f), len(r.Variants), ctx)
			return
		}
	}
	size := fixedElementBytes(f)
	lo, hi := fixedRangeOf(f)
	if lo == "nil" {
		w.push(applyCtx(fixedCopy(at, at, size, ""), ctx))
		return
	}
	// A RANGED INTEGER CLAMPS ON LOAD, counting one `clamped` (§3.4). The
	// bounds do not ride, so the clamp is against the READER's own and nothing
	// else.
	signed := "false"
	if fixedSignedLeaf(f) {
		signed = "true"
	}
	w.push(applyCtx(fixedOther([]string{":clamp", itoa(at), itoa(at), itoa(size), signed, lo, hi}, "clamp", ""), ctx))
}

// fixedSignedLeaf reports whether a leaf's image is TWO'S COMPLEMENT, which is
// what a clamp has to read it as.
func fixedSignedLeaf(f *ir.Field) bool {
	switch f.Type.Kind {
	case ir.TInt, ir.TFixed:
		return f.Type.Signed
	}
	return false
}

// fixedIdentityPlan is the whole of a root's identity plan, as Elixir.
func fixedIdentityPlan(st *ir.Struct) []fixedPlanEntry {
	w := &fixedLeafWalk{}
	fixedTypeLeaves(w, st, 0, leafCtx{})
	return fixedCoalesce(w.out)
}
