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
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// the plan's ops, and the whole set. The IDENTITY plan carries copy, count,
// text and the two dead-byte checks; the other four move or remap a value from
// a plan compiled from another writer's layout.
const (
	fixedOpCopy    = 0 // move size bytes
	fixedOpCount   = 1 // a count: clamp it to the reader's own bound
	fixedOpText    = 2 // a length, then the units
	fixedOpOrdinal = 3 // a variant ordinal, remapped through the plan's own table
	fixedOpWiden   = 4 // a narrower source into a wider destination
	fixedOpConst   = 5 // a constant this reader's own storage takes: a remapped union tag
	fixedOpWidenF  = 6 // f32 into f64, §4's float rung

	// THE TWO CHECK OPS. `dead` refuses a non-zero byte in a source range and
	// lands nothing, which is how an inactive union arm's bytes are held to
	// zero (docs/FIXED-FORM-ALGORITHM.md §4.5, §5.3 step 11). `presentCopy`
	// lands an optional's present byte and, under a clear flag, refuses a
	// non-zero byte in the dead payload behind it (§3.1, §4.5).
	fixedOpDead        = 8
	fixedOpPresentCopy = 9
)

// fixedNoGuard is the guard an entry that belongs to no union arm carries.
const fixedNoGuard = -1

// fixedPlanEntry is one plan entry, in the nine lanes the Dart runtime reads
// it through.
//
// argw IS THE GUARD'S WIDTH IN BYTES and it is the ninth lane. A union tag is
// one, two, four or eight bytes wide and the guard is compared at ALL of them:
// tested at its first byte, a foreign tag of 0x0101 fires arm 1 and runs that
// arm's entries over a stranger's record — wrong bytes on the IDENTITY plan as
// much as on a compiled one (docs/FIXED-FORM-ALGORITHM.md §5.2, THE GUARD'S
// WIDTH; §7 fix 12). An entry under no guard carries 1, and zero reads as one at
// run time, so a plan laid down before this lane existed still reads.
type fixedPlanEntry struct {
	op    int
	src   int64
	dst   int64
	size  int64
	aux   int64
	guard int64
	arg   int
	argw  int
	meta  int
	note  string
	// links is THE GUARD CHAIN (docs/FIXED-FORM-ALGORITHM.md §4.1) of an entry
	// under more than one union, OUTERMOST FIRST; guards and gcount are its
	// offset+count into the identity guard pool once laid (fixedIdentityGuards),
	// three ints a link. An entry under one union or none carries no chain: its
	// guard lane is the whole of it.
	links  []fixedLink
	guards int
	gcount int
}

// fixedLink is ONE LINK of a guard chain: the tag's offset, the ordinal it must
// hold and the width it is compared at (§4.1).
type fixedLink struct {
	guard int64
	arg   int
	argw  int
}

type fixedPlanBuild struct {
	entries []fixedPlanEntry
	// chain is the GUARD CHAIN of the arm being walked, OUTERMOST FIRST: one
	// link per enclosing union, pushed on the way into an arm and popped on the
	// way out (§4.1).
	chain []fixedLink
	// argw is THE CURRENT UNION'S TAG WIDTH, stamped onto every entry pushed
	// beneath it. One outside a union; the union case saves it, sets its own and
	// restores it, so a nested union's arms carry their OWN tag's width.
	argw int
}

func (p *fixedPlanBuild) push(e fixedPlanEntry) {
	if e.argw = p.argw; e.argw <= 0 {
		e.argw = 1
	}
	// THE GUARD CHAIN (§4.1): an entry beneath a union answers to EVERY
	// enclosing tag. Its guard lane is the chain's innermost link, and a chain
	// past one rides the pool whole.
	if k := len(p.chain); k > 0 && e.guard != fixedNoGuard {
		in := p.chain[k-1]
		e.guard, e.arg, e.argw = in.guard, in.arg, in.argw
		if k > 1 {
			e.links = append([]fixedLink(nil), p.chain...)
		}
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
		p.push(fixedPlanEntry{op: fixedOpPresentCopy, src: base, dst: base, size: fixedPresentBytes,
			aux: fixedFieldBytes(f) - fixedPresentBytes, guard: fixedNoGuard, note: f.Name + " present"})
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
		// `aux` is the element stride, so the run can hold the dead slots
		// past the live count to zero (§4.5, §5.3 step 11).
		p.push(fixedPlanEntry{op: fixedOpCount, src: base, dst: base, size: f.ArrayBound,
			aux: fixedElementBytes(f), guard: fixedNoGuard, note: f.Name + " count"})
		fixedPlanElements(p, f, base+fixedCountBytes, f.ArrayBound)
	case f.Type.Kind == ir.TString:
		fixedPlanText(p, f, base, f.Type.Size, fixedTextUtf8)
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
			// does not name is not moved, and the DEAD entries below hold its
			// bytes (and the bytes past a narrower taken arm) to zero
			// (docs/FIXED-FORM-ALGORITHM.md §3.1, §4.5, §5.3 step 11).
			tag := fixedUnionTagBytes(r)
			widest := fixedElementBytes(f) - tag
			p.push(fixedPlanEntry{op: fixedOpCopy, src: src, dst: dst, size: tag,
				guard: guard, arg: arg, note: "the tag"})
			// THE ARMS' GUARD IS THIS UNION'S TAG, AT THIS UNION'S WIDTH.
			saved := p.argw
			p.argw = int(tag)
			if widest > 0 {
				// TAG 0 NAMES NO ARM: every arm byte under it is dead.
				p.chain = append(p.chain, fixedLink{guard: src, arg: 0, argw: int(tag)})
				p.push(fixedPlanEntry{op: fixedOpDead, src: src + tag, dst: src + tag, size: widest,
					guard: src, note: "tag 0's dead arm"})
				p.chain = p.chain[:len(p.chain)-1]
			}
			for i, v := range r.Variants {
				armBytes := int64(0)
				if v.F != nil {
					armBytes = fixedFieldBytes(v.F)
				}
				// THE ARM ANSWERS TO THIS TAG AND TO EVERY TAG OUTSIDE IT
				// (§4.1, the guard chain): one link pushed on the way in, popped
				// on the way out.
				p.chain = append(p.chain, fixedLink{guard: src, arg: i + 1, argw: int(tag)})
				if past := widest - armBytes; past > 0 {
					p.push(fixedPlanEntry{op: fixedOpDead, src: src + tag + armBytes,
						dst: src + tag + armBytes, size: past,
						guard: src, note: "dead past the taken arm"})
				}
				if v.F == nil {
					// §3.4: the tag is copied; this arm has no payload to move.
					p.chain = p.chain[:len(p.chain)-1]
					continue
				}
				if v.F.Array == ir.ArrayNone && v.F.KeyEnum == "" && !v.F.Type.Optional {
					if _, ok := v.F.Type.Ref.(*ir.Struct); ok {
						// Nested record arms keep the established storage walk,
						// whose child offsets follow the union's wire address.
						fixedPlanElement(p, v.F, src+tag, dst+tag, src, i+1)
						p.chain = p.chain[:len(p.chain)-1]
						continue
					}
				}
				fixedPlanSub(p, v.F, src+tag, src, i+1)
				p.chain = p.chain[:len(p.chain)-1]
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
		p.push(fixedPlanEntry{op: fixedOpPresentCopy, src: base, dst: base, size: fixedPresentBytes,
			aux: fixedFieldBytes(f) - fixedPresentBytes, guard: guard, arg: arg, note: f.Name + " present"})
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
		elem := fixedElementBytes(f)
		p.push(fixedPlanEntry{op: fixedOpCount, src: base, dst: base, size: f.ArrayBound,
			aux: elem, guard: guard, arg: arg, note: f.Name + " count"})
		for i := int64(0); i < f.ArrayBound; i++ {
			fixedPlanElement(p, f, base+fixedCountBytes+i*elem, base+fixedCountBytes+i*elem, guard, arg)
		}
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		units, flavour := f.Type.Size, fixedTextUtf8
		if f.Type.Kind == ir.TBytes {
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

// fixedFlatElem reports an element whose whole image is one run with no dead
// byte of its own: nothing in its closure needs a COUNT or a TEXT of its own,
// and every byte of it is a value the image and the wire agree on. An
// optional's absent payload, a text/bytes field's slack, a union's inactive
// arm (and the bytes past a narrower taken arm), a counted array's dead slots
// and any nested type holding one are DEAD BYTES (docs/FIXED-FORM-
// ALGORITHM.md §3.1, §4.5, §5.3 step 11), so an element carrying any of them
// is WALKED one element at a time and its presentCopy, text and dead entries
// are pushed per live element. Only a closure with no dead byte folds into
// one copy.
func fixedFlatElem(f *ir.Field) bool {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return fixedFlatType(r)
		case *ir.Union:
			return false
		}
	}
	switch f.Type.Kind {
	case ir.TString, ir.TBytes, ir.TMap:
		return false
	}
	return true
}

func fixedFlatType(st *ir.Struct) bool {
	for _, f := range st.Fields {
		if f.Type.Optional || f.KeyEnum != "" || f.Array == ir.ArrayCounted {
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
				if p.op == fixedOpCopy && e.op == fixedOpCopy && p.guard == e.guard && p.arg == e.arg &&
					p.argw == e.argw && fixedLinksKey(p.links) == fixedLinksKey(e.links) &&
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
	case fixedOpDead:
		// a check lands nothing
		return nil
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

// fixedLinksKey is a chain's identity, for the coalescer and the pool's dedupe.
func fixedLinksKey(links []fixedLink) string {
	var b strings.Builder
	for _, l := range links {
		fmt.Fprintf(&b, "%d:%d:%d;", l.guard, l.arg, l.argw)
	}
	return b.String()
}

// fixedIdentityGuards lays THE GUARD CHAIN's pool for the identity plan
// (docs/FIXED-FORM-ALGORITHM.md §4.1): every chain past one link, OUTERMOST
// FIRST, three ints a link, deduped by content, and each such entry's
// guards/gcount set to its offset+count. The load hands the pool to the run as
// the identity plan's own pool.
func fixedIdentityGuards(plan []fixedPlanEntry) []int64 {
	var pool []int64
	index := map[string]int{}
	for i := range plan {
		if len(plan[i].links) < 2 {
			continue
		}
		key := fixedLinksKey(plan[i].links)
		at, ok := index[key]
		if !ok {
			at = len(pool)
			index[key] = at
			for _, l := range plan[i].links {
				pool = append(pool, l.guard, int64(l.arg), int64(l.argw))
			}
		}
		plan[i].guards = at
		plan[i].gcount = len(plan[i].links)
	}
	return pool
}
