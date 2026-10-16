// THE IDENTITY PLAN, built here rather than at run time (docs/SPEC-TABLES.md
// §3.4). "THE IDENTITY PLAN IS A STATIC CONSTANT BAKED INTO THE GENERATED
// READER": when a record's hash equals this build's own, the plan is the one
// the compiler already wrote, every entry a copy with ADJACENT RUNS COALESCED.
//
// THIS IS THE C++ REFERENCE'S LEAF WALK, NOT A WHOLE-BODY COPY. The reader's
// own storage in Dart is the canonical body image, so source and destination
// advance together and almost everything coalesces into one run — but a COUNT
// and a TEXT LENGTH still get their own entries, exactly as the reference's
// constexpr walk gives them, because those two are the only places a record's
// bytes are not merely moved: a hostile count or length has to be CLAMPED TO
// THIS READER'S OWN BOUND before it reaches a value a consumer will index
// with. A one-run identity plan would move those bytes verbatim and leave a
// Dart consumer to throw a RangeError on the first read, which is the one
// thing the forgery fuzzer's oracle refuses.
package darttable

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// the plan's ops, and the whole set. The IDENTITY plan carries the first
// three; the other four are what a plan compiled from another writer's layout
// adds.
const (
	fixedOpCopy    = 0 // move size bytes
	fixedOpCount   = 1 // a count: clamp it to the reader's own bound
	fixedOpText    = 2 // a length, then the units
	fixedOpOrdinal = 3 // a variant ordinal, remapped through the plan's own table
	fixedOpWiden   = 4 // a narrower source into a wider destination
	fixedOpConst   = 5 // a constant this reader's own storage takes: a remapped union tag
	fixedOpWidenF  = 6 // f32 into f64, §4's float rung
)

// fixedNoGuard is the guard an entry that belongs to no union arm carries.
const fixedNoGuard = -1

// fixedPlanEntry is one plan entry, in the fourteen lanes the Dart runtime
// reads it through (the last two, poolAt and the chain's length, name its run
// in the plan's pool).
//
// argw IS THE GUARD'S WIDTH IN BYTES and it is the ninth lane. A union tag is
// one, two, four or eight bytes wide and the guard is compared at ALL of them:
// tested at its first byte, a foreign tag of 0x0101 fires arm 1 and runs that
// arm's entries over a stranger's record — wrong bytes on the IDENTITY plan as
// much as on a compiled one (docs/FIXED-FORM-ALGORITHM.md §5.2, THE GUARD'S
// WIDTH; §7 fix 12). An entry under no guard carries 1, and zero reads as one at
// run time, so a plan laid down before this lane existed still reads.
type fixedPlanEntry struct {
	op     int
	src    int64
	dst    int64
	size   int64
	aux    int64
	guard  int64
	arg    int
	argw   int
	meta   int
	guard2 int64
	arg2   int
	argw2  int
	// chain IS EVERY TAG PAST THE TWO INLINE LANES, OUTERMOST FIRST
	// (docs/FIXED-FORM-ALGORITHM.md §4.1, §5.8 row 6): a union nested three
	// deep has a third condition two lanes cannot carry. The emitter lays each
	// distinct chain down ONCE as a run in the plan's pool of (guard, arg,
	// argW) links and the entry carries the run's offset (poolAt) and its
	// length; an entry with no chain carries 0, 0 and reads exactly as the
	// two-lane plan did.
	chain  []fixedGuardLink
	poolAt int
	note   string
}

// fixedGuardLink is ONE LINK OF A GUARD CHAIN (docs/FIXED-FORM-ALGORITHM.md
// §4.1, §5.8 row 6): the byte offset of a union tag, the ordinal it must hold
// for the entry to run, and the width it is compared at. The reference's
// TableFixedGuard (internal/codegen/rusttable/fixedruntime.go) is the same
// three facts.
type fixedGuardLink struct {
	guard int64
	arg   int
	argw  int
}

type fixedPlanBuild struct {
	entries []fixedPlanEntry
	// argw is THE CURRENT UNION'S TAG WIDTH, stamped onto every entry pushed
	// beneath it. One outside a union; the union case saves it, sets its own and
	// restores it, so a nested union's arms carry their OWN tag's width.
	argw int
}

// push appends one entry to the plan builder, defaulting guard2 to fixedNoGuard
// and argw2 to 1 (docs/FIXED-FORM-ALGORITHM.md §4.1).
func (p *fixedPlanBuild) push(e fixedPlanEntry) {
	if e.argw = p.argw; e.argw <= 0 {
		e.argw = 1
	}
	if e.guard2 == 0 && e.argw2 == 0 {
		e.guard2 = fixedNoGuard
		e.argw2 = 1
	}
	p.entries = append(p.entries, e)
}

// fixedIdentityPlan is this type's leaf walk, coalesced — the same coalescer
// the plan compiler runs, so the two are one array read by one loop. THE PLAN
// IS PARTITIONED (docs/SPEC-TABLES.md §3.4, docs/FIXED-FORM-ALGORITHM.md §4.1,
// §4.4, §5.9 #42): every unguarded entry first, then every guarded one, and
// the split is the index the second half starts at — the number of UNGUARDED
// entries, not of guarded ones.
func fixedIdentityPlan(st *ir.Struct) ([]fixedPlanEntry, int) {
	p := &fixedPlanBuild{argw: 1}
	var at int64
	for _, f := range st.Fields {
		fixedPlanField(p, f, at)
		at += fixedFieldBytes(f)
	}
	return fixedCoalesce(p.entries)
}

// fixedPlanField walks one field of a table at its offset in the body image.
func fixedPlanField(p *fixedPlanBuild, f *ir.Field, at int64) {
	base := at
	if f.Type.Optional {
		p.push(fixedPlanEntry{op: fixedOpCopy, src: base, dst: base, size: fixedPresentBytes,
			guard: fixedNoGuard, note: f.Name + " present"})
		base += fixedPresentBytes
	}
	switch {
	case f.KeyEnum != "":
		fixedPlanElements(p, f, base, f.KeyEnumRef.Max)
	case f.Array == ir.ArrayFixed:
		fixedPlanElements(p, f, base, f.ArrayBound)
	case f.Array == ir.ArrayCounted:
		// THE COUNT IS ITS OWN ENTRY, and clamping it is why: a count the
		// writer states past this reader's bound is clamped and counted.
		p.push(fixedPlanEntry{op: fixedOpCount, src: base, dst: base, size: f.ArrayBound,
			guard: fixedNoGuard, note: f.Name + " count"})
		fixedPlanElements(p, f, base+fixedCountBytes, f.ArrayBound)
	case f.Type.Kind == ir.TString:
		fixedPlanText(p, f, base, f.Type.Size, fixedTextUtf8)
	case f.Type.Kind == ir.TWString:
		fixedPlanText(p, f, base, 2*f.Type.Size, fixedTextWide)
	case f.Type.Kind == ir.TBytes:
		fixedPlanText(p, f, base, f.Type.Size, fixedTextBytes)
	default:
		fixedPlanElement(p, f, base, base, fixedNoGuard, 0)
	}
}

func fixedPlanText(p *fixedPlanBuild, f *ir.Field, at, units int64, flavour int) {
	p.push(fixedPlanEntry{op: fixedOpText, src: at, dst: at, size: units, aux: at + fixedCountBytes,
		guard: fixedNoGuard, meta: flavour, note: f.Name})
}

func fixedPlanElements(p *fixedPlanBuild, f *ir.Field, at, count int64) {
	elem := fixedElementBytes(f)
	if fixedFlatElem(f) {
		// THE WHOLE ARRAY IS ONE RUN: its image is its wire image and nothing
		// in it needs clamping, so the elements need no walk at all and the
		// plan carries one entry however many of them there are.
		p.push(fixedPlanEntry{op: fixedOpCopy, src: at, dst: at, size: count * elem,
			guard: fixedNoGuard, note: f.Name + ", whole"})
		return
	}
	for i := range count {
		fixedPlanElement(p, f, at+i*elem, at+i*elem, fixedNoGuard, 0)
	}
}

func fixedPlanElement(p *fixedPlanBuild, f *ir.Field, src, dst, guard int64, arg int) {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			var sub int64
			for _, sf := range r.Fields {
				fixedPlanSub(p, sf, src+sub, guard, arg)
				sub += fixedFieldBytes(sf)
			}
			return
		case *ir.Union:
			// the tag, then every arm under its own tag value. An arm the tag
			// does not name is not moved, which is what leaves the slack behind
			// a narrower arm as the prefill put it.
			// AN ARM INSIDE AN ARM ANSWERS TO THE OUTER TAG TOO
			// (docs/FIXED-FORM-ALGORITHM.md §4.1, §5.8 row 6): each arm walks
			// with fixedNoGuard, and entries an inner union already guarded
			// receive this union's tag on guard2/arg2/argw2 instead of
			// overwriting guard. An entry whose two inline lanes are both
			// spent — the third nested union and every one past it — takes
			// this tag at the FRONT of its chain: the walk runs inner to
			// outer, so prepending is what keeps the chain OUTERMOST FIRST.
			tag := fixedUnionTagBytes(r)
			p.push(fixedPlanEntry{op: fixedOpCopy, src: src, dst: dst, size: tag,
				guard: guard, arg: arg, note: "the tag"})
			// THE ARMS' GUARD IS THIS UNION'S TAG, AT THIS UNION'S WIDTH.
			saved := p.argw
			p.argw = int(tag)
			for i, v := range r.Variants {
				if v.F == nil {
					continue // §3.4: the tag is copied; this arm has no payload to move.
				}
				guardAt := len(p.entries)
				if v.F.Array == ir.ArrayNone && v.F.KeyEnum == "" && !v.F.Type.Optional {
					if _, ok := v.F.Type.Ref.(*ir.Struct); ok {
						// Nested record arms keep the established storage walk,
						// whose child offsets follow the union's wire address.
						fixedPlanElement(p, v.F, src+tag, dst+tag, fixedNoGuard, 0)
					} else {
						fixedPlanSub(p, v.F, src+tag, fixedNoGuard, 0)
					}
				} else {
					fixedPlanSub(p, v.F, src+tag, fixedNoGuard, 0)
				}
				for q := guardAt; q < len(p.entries); q++ {
					if p.entries[q].guard == fixedNoGuard {
						p.entries[q].guard = src
						p.entries[q].arg = i + 1
						p.entries[q].argw = int(tag)
					} else if p.entries[q].guard2 == fixedNoGuard {
						p.entries[q].guard2 = src
						p.entries[q].arg2 = i + 1
						p.entries[q].argw2 = int(tag)
					} else {
						link := fixedGuardLink{guard: src, arg: i + 1, argw: int(tag)}
						p.entries[q].chain = append([]fixedGuardLink{link}, p.entries[q].chain...)
					}
				}
			}
			p.argw = saved
			return
		}
	}
	p.push(fixedPlanEntry{op: fixedOpCopy, src: src, dst: dst, size: fixedElementBytes(f),
		guard: guard, arg: arg})
}

// fixedPlanSub walks a nested table's field under a guard, which is the one
// shape fixedPlanField cannot take: an arm's sub-plan runs only under its tag.
func fixedPlanSub(p *fixedPlanBuild, f *ir.Field, at, guard int64, arg int) {
	if guard == fixedNoGuard {
		fixedPlanField(p, f, at)
		return
	}
	base := at
	if f.Type.Optional {
		p.push(fixedPlanEntry{op: fixedOpCopy, src: base, dst: base, size: fixedPresentBytes,
			guard: guard, arg: arg, note: f.Name + " present"})
		base += fixedPresentBytes
	}
	switch {
	case f.KeyEnum != "", f.Array == ir.ArrayFixed:
		count := f.ArrayBound
		if f.KeyEnum != "" {
			count = f.KeyEnumRef.Max
		}
		elem := fixedElementBytes(f)
		if fixedFlatElem(f) {
			p.push(fixedPlanEntry{op: fixedOpCopy, src: base, dst: base, size: count * elem,
				guard: guard, arg: arg, note: f.Name + ", whole"})
			return
		}
		for i := int64(0); i < count; i++ {
			fixedPlanElement(p, f, base+i*elem, base+i*elem, guard, arg)
		}
	case f.Array == ir.ArrayCounted:
		p.push(fixedPlanEntry{op: fixedOpCount, src: base, dst: base, size: f.ArrayBound,
			guard: guard, arg: arg, note: f.Name + " count"})
		elem := fixedElementBytes(f)
		for i := int64(0); i < f.ArrayBound; i++ {
			fixedPlanElement(p, f, base+fixedCountBytes+i*elem, base+fixedCountBytes+i*elem, guard, arg)
		}
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TWString, f.Type.Kind == ir.TBytes:
		units, flavour := f.Type.Size, fixedTextUtf8
		switch f.Type.Kind {
		case ir.TWString:
			units, flavour = 2*f.Type.Size, fixedTextWide
		case ir.TBytes:
			flavour = fixedTextBytes
		}
		// THE FLAVOUR RIDES ON `meta` AND NOT ON `arg`, which is where this
		// port departs from the reference's PLAN ENCODING (never from its
		// bytes): the reference spends one lane on both the text flavour and
		// the arm's tag value, so a text field reached through a union arm
		// gets a guard whose comparison value is a flavour. The plan is not a
		// wire structure — it is private to each reader — so a second lane
		// costs nothing and the guard keeps working. See the PR body.
		p.push(fixedPlanEntry{op: fixedOpText, src: base, dst: base, size: units,
			aux: base + fixedCountBytes, guard: guard, arg: arg, meta: flavour, note: f.Name})
	default:
		fixedPlanElement(p, f, base, base, guard, arg)
	}
}

// fixedFlatElem reports an element whose whole image is one run: nothing in
// its closure needs a COUNT of its own, so no byte of it is anything but
// moved. A string/wstring/bytes field's image IS its wire image — the length
// and the units sit in order — and the TEXT op that clamps a hostile length
// lives in the decode both paths share, over LIVE elements only. A counted
// array of wrapped strings is therefore one COPY, never a TEXT per slack slot.
// Every other construct — an enum, a union, an optional, a nested table of
// plain fields, a fixed array — is byte-identical between the image and the
// wire in this backend, because the image IS the wire's layout.
func fixedFlatElem(f *ir.Field) bool {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return fixedFlatType(r)
		case *ir.Union:
			for _, v := range r.Variants {
				if v.F == nil || !fixedFlatElem(v.F) {
					return false
				}
			}
			return true
		}
	}
	return true
}

func fixedFlatType(st *ir.Struct) bool {
	for _, f := range st.Fields {
		if f.Array == ir.ArrayCounted {
			return false
		}
		if !fixedFlatElem(f) {
			return false
		}
	}
	return true
}

// fixedCoalesce is THE ONLY OPTIMIZATION A PLAN COMPILER PERFORMS: two
// neighbouring COPY entries whose source and destination both advance together
// are one entry. It runs here on the identity plan and in the Dart runtime on
// any other, so the two are the same array read by the same loop. THE PLAN IS
// PARTITIONED (docs/SPEC-TABLES.md §3.4, docs/FIXED-FORM-ALGORITHM.md §4.1,
// §4.4, §5.9 #42): the walk runs TWICE — unguarded entries first, guarded ones
// second — and coalescing runs inside each half, never across the split, because
// a copy that merged a guarded entry into an unguarded run would land the arm's
// bytes unconditionally. The returned split is the number of UNGUARDED entries,
// the index the second half starts at.
func fixedCoalesce(in []fixedPlanEntry) ([]fixedPlanEntry, int) {
	out := make([]fixedPlanEntry, 0, len(in))
	split := 0
	for pass := range 2 {
		for _, e := range in {
			if (e.guard != fixedNoGuard) != (pass == 1) {
				continue
			}
			if n := len(out); n > split {
				p := &out[n-1]
				// ONLY EQUAL CHAINS MERGE (§4.1, §5.8 row 6): a run that took
				// in an entry under a longer chain would land its bytes under
				// fewer conditions than that entry answers to.
				if p.op == fixedOpCopy && e.op == fixedOpCopy && p.guard == e.guard && p.arg == e.arg &&
					p.argw == e.argw && p.guard2 == e.guard2 && p.arg2 == e.arg2 && p.argw2 == e.argw2 &&
					slices.Equal(p.chain, e.chain) &&
					p.src+p.size == e.src && p.dst+p.size == e.dst {
					p.size += e.size
					continue
				}
			}
			out = append(out, e)
		}
		if pass == 0 {
			split = len(out)
		}
	}
	return out, split
}

// fixedChainLength is how many tags an entry answers to: the two inline lanes
// and every pooled link (docs/FIXED-FORM-ALGORITHM.md §4.1, §5.8 row 6).
func fixedChainLength(e fixedPlanEntry) int {
	n := len(e.chain)
	if e.guard != fixedNoGuard {
		n++
	}
	if e.guard2 != fixedNoGuard {
		n++
	}
	return n
}

// fixedPlanChainRefusal answers why a plan cannot be laid down for its guard
// chains, or "" when it can. THE CHAIN'S ONLY BOUND IS THE LAYOUT'S 64
// (docs/FIXED-FORM-ALGORITHM.md §4.1, §5.8 row 6; ir.TableFixedMaxDepth): a
// chain past it is refused BY NAME at generate time rather than laid down with
// a condition dropped. The layout's own depth bound refuses such a table
// first, because every nested union is at least one level of the layout; this
// is the second fence, asked of the plan itself, so the two bounds can never
// disagree in the direction that drops a guard.
func fixedPlanChainRefusal(plan []fixedPlanEntry) string {
	longest := 0
	for _, e := range plan {
		longest = max(longest, fixedChainLength(e))
	}
	if longest > ir.TableFixedMaxDepth {
		return fmt.Sprintf("its plan's guard chain is %d links long, past the layout's %d-link bound, so no plan carries it without dropping a condition (`plan_too_large`) — it keeps form 1 (docs/FIXED-FORM-ALGORITHM.md §4.1, §5.8 row 6)",
			longest, ir.TableFixedMaxDepth)
	}
	return ""
}

// fixedLayPool lays every entry's chain down in the plan's POOL and stamps the
// entry with where its run starts (docs/FIXED-FORM-ALGORITHM.md §4.1, §5.8
// row 6). The pool is append-only and each DISTINCT chain is one contiguous
// run, outermost first, so every entry under one arm shares one run — the
// reference's shape (internal/codegen/rusttable/fixedruntime.go, conjoin),
// where an arm lays its whole chain down once as it is entered. An entry with
// no chain keeps offset 0 and count 0.
func fixedLayPool(plan []fixedPlanEntry) []fixedGuardLink {
	var pool []fixedGuardLink
	runs := map[string]int{}
	for i := range plan {
		e := &plan[i]
		e.poolAt = 0
		if len(e.chain) == 0 {
			continue
		}
		var key strings.Builder
		for _, l := range e.chain {
			fmt.Fprintf(&key, "%d:%d:%d;", l.guard, l.arg, l.argw)
		}
		at, ok := runs[key.String()]
		if !ok {
			at = len(pool)
			pool = append(pool, e.chain...)
			runs[key.String()] = at
		}
		e.poolAt = at
	}
	return pool
}

// fixedFill is one prefill range: the bytes a plan does not land. Packed as
// (dst, size) in the generated cover and in the plan's fill list.
type fixedFill struct {
	dst, size int64
}

// fixedEntryLands is the bytes of THIS BUILD's image one identity-plan entry
// writes. A COUNT lands four bytes (its size is the bound, not the move). A
// TEXT lands the length and the units; Dart's image IS the wire, so there is
// no terminator past the bound the way C++ storage has one.
func fixedEntryLands(e fixedPlanEntry) []fixedFill {
	switch e.op {
	case fixedOpCount:
		return []fixedFill{{dst: e.dst, size: fixedCountBytes}}
	case fixedOpText:
		return []fixedFill{
			{dst: e.dst, size: fixedCountBytes},
			{dst: e.aux, size: e.size},
		}
	default:
		return []fixedFill{{dst: e.dst, size: e.size}}
	}
}

// fixedIdentityCover is the type's value bytes: every destination the identity
// plan lands, sorted and merged. THE PREFILL IS THIS SET MINUS WHAT A PLAN
// LANDS, so against the identity plan it is empty.
func fixedIdentityCover(plan []fixedPlanEntry) []fixedFill {
	var raw []fixedFill
	for _, e := range plan {
		raw = append(raw, fixedEntryLands(e)...)
	}
	return mergeFixedFills(raw)
}

func mergeFixedFills(raw []fixedFill) []fixedFill {
	sort.Slice(raw, func(i, j int) bool { return raw[i].dst < raw[j].dst })
	var out []fixedFill
	for _, r := range raw {
		if r.size <= 0 {
			continue
		}
		if len(out) > 0 {
			last := &out[len(out)-1]
			if r.dst <= last.dst+last.size {
				if end := r.dst + r.size; end > last.dst+last.size {
					last.size = end - last.dst
				}
				continue
			}
		}
		out = append(out, r)
	}
	return out
}
