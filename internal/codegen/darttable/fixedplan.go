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

// fixedPlanEntry is one plan entry, in the eight lanes the Dart runtime reads
// it through.
type fixedPlanEntry struct {
	op    int
	src   int64
	dst   int64
	size  int64
	aux   int64
	guard int64
	arg   int
	meta  int
	note  string
}

type fixedPlanBuild struct {
	entries []fixedPlanEntry
}

func (p *fixedPlanBuild) push(e fixedPlanEntry) { p.entries = append(p.entries, e) }

// fixedIdentityPlan is this type's leaf walk, coalesced — the same coalescer
// the plan compiler runs, so the two are one array read by one loop.
func fixedIdentityPlan(st *ir.Struct) []fixedPlanEntry {
	p := &fixedPlanBuild{}
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
	for i := int64(0); i < count; i++ {
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
			tag := fixedUnionTagBytes(r)
			p.push(fixedPlanEntry{op: fixedOpCopy, src: src, dst: dst, size: tag,
				guard: guard, arg: arg, note: "the tag"})
			for i, v := range r.Variants {
				fixedPlanElement(p, v.F, src+tag, dst+tag, src, i+1)
			}
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
// its closure needs the count or the text op, so no byte of it is anything but
// moved. Every other construct — an enum, a union, an optional, a nested table
// of plain fields, a fixed array — is byte-identical between the image and the
// wire in this backend, because the image IS the wire's layout.
func fixedFlatElem(f *ir.Field) bool {
	switch f.Type.Kind {
	case ir.TString, ir.TWString, ir.TBytes:
		return false
	}
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
// any other, so the two are the same array read by the same loop.
func fixedCoalesce(in []fixedPlanEntry) []fixedPlanEntry {
	out := make([]fixedPlanEntry, 0, len(in))
	for _, e := range in {
		if n := len(out); n > 0 {
			p := &out[n-1]
			if p.op == fixedOpCopy && e.op == fixedOpCopy && p.guard == e.guard && p.arg == e.arg &&
				p.src+p.size == e.src && p.dst+p.size == e.dst {
				p.size += e.size
				continue
			}
		}
		out = append(out, e)
	}
	return out
}
