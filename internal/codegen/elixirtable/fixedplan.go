package elixirtable

import (
	"fmt"
	"math/big"
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
// load. That is stated rather than left to be found.

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
}

func (e fixedPlanEntry) String() string { return e.render(0, 0) }

// render spells the entry at a column, breaking the tuple where the formatter
// would — a 128-bit range is the one bound that outgrows a line.
func (e fixedPlanEntry) render(col, tail int) string {
	inner := tupleNode{items: e.items}
	if e.guard == "" {
		return inner.render(col, col, tail)
	}
	// AN ENTRY BELONGING TO AN ARM is wrapped in its guard.
	outer := tupleNode{items: []string{":guard", e.guardSrc, e.guardTag, inner.render(col+1, col+1, 1)}}
	return outer.render(col, col, tail)
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

// fixedTypeLeaves walks one TYPE's leaves at a base the caller gives.
func fixedTypeLeaves(w *fixedLeafWalk, st *ir.Struct, base int64, guard string) {
	at := base
	for _, f := range st.Fields {
		fixedFieldLeaves(w, f, at, guard)
		at += fixedFieldBytes(f)
	}
}

func fixedFieldLeaves(w *fixedLeafWalk, f *ir.Field, at int64, guard string) {
	base := at
	if f.Type.Optional {
		w.push(fixedCopy(base, base, fixedPresentBytes, guard))
		base += fixedPresentBytes
	}
	switch {
	case f.KeyEnum != "":
		fixedElementLoop(w, f, base, f.KeyEnumRef.Max, guard)
	case f.Array == ir.ArrayFixed:
		fixedElementLoop(w, f, base, f.ArrayBound, guard)
	case f.Array == ir.ArrayCounted:
		w.push(fixedOther([]string{":count", itoa(base), itoa(base), itoa(f.ArrayBound)}, "count", guard))
		fixedElementLoop(w, f, base+fixedCountBytes, f.ArrayBound, guard)
	case f.Type.Kind == ir.TString:
		fixedTextLeaf(w, base, f.Type.Size, fixedTextUtf8, guard)
	case f.Type.Kind == ir.TWString:
		fixedTextLeaf(w, base, 2*f.Type.Size, fixedTextWide, guard)
	case f.Type.Kind == ir.TBytes:
		fixedTextLeaf(w, base, f.Type.Size, fixedTextBytes, guard)
	default:
		fixedElementLeaves(w, f, base, guard)
	}
}

func fixedTextLeaf(w *fixedLeafWalk, base, size int64, flavour int, guard string) {
	w.push(fixedOther([]string{":text", itoa(base), itoa(base), itoa(base + fixedCountBytes), itoa(size), strconv.Itoa(flavour)}, "text", guard))
}

func fixedElementLoop(w *fixedLeafWalk, f *ir.Field, base, count int64, guard string) {
	elem := fixedElementBytes(f)
	if fixedFlatElem(f) {
		// THE WHOLE ARRAY IS ONE RUN: its image is a run of plain bytes, so the
		// elements need no walk at all and the plan carries ONE entry however
		// many of them there are.
		w.push(fixedCopy(base, base, count*elem, guard))
		return
	}
	for i := range count {
		fixedElementLeaves(w, f, base+i*elem, guard)
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
		}
	}
	switch f.Type.Kind {
	case ir.TString, ir.TWString, ir.TBytes, ir.TMap:
		return false
	}
	return true
}

// fixedElementLeaves emits ONE element's leaves at an absolute offset.
func fixedElementLeaves(w *fixedLeafWalk, f *ir.Field, at int64, guard string) {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			fixedTypeLeaves(w, r, at, guard)
			return
		case *ir.Union:
			// THE TAG, then each arm under its own guard: an entry belonging to
			// an arm runs only when the tag it was compiled for is the tag the
			// record carries.
			tag := fixedUnionTagBytes(r)
			w.push(fixedCopy(at, at, tag, guard))
			for i, v := range r.Variants {
				arm := fmt.Sprintf("%d:%d", at, i+1)
				fixedElementLeaves(w, v.F, at+tag, arm)
			}
			return
		}
	}
	size := fixedElementBytes(f)
	lo, hi := fixedRangeOf(f)
	if lo == "nil" {
		w.push(fixedCopy(at, at, size, guard))
		return
	}
	// A RANGED INTEGER CLAMPS ON LOAD, counting one `clamped` (§3.4). The
	// bounds do not ride, so the clamp is against the READER's own and nothing
	// else.
	signed := "false"
	if fixedSignedLeaf(f) {
		signed = "true"
	}
	w.push(fixedOther([]string{":clamp", itoa(at), itoa(at), itoa(size), signed, lo, hi}, "clamp", guard))
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
	fixedTypeLeaves(w, st, 0, "")
	return fixedCoalesce(w.out)
}
