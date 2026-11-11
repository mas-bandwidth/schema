package ccommon

import "github.com/mas-bandwidth/schema/v2/ir"

// ---- the identity plan's element widths, shared by the C and C++ emitters ----
//
// A coalesced copy entry carries no element width, and a big-endian reader
// needs one per element (§3.4: the wire is little-endian on every host). The
// widths are recovered here from the layout walk the root already carries:
// each leaf's kind fixes its width, an array repeats its element, a union's
// arms overlap at the tag, and a counted array's count is not a copy. A copy
// entry that spans leaves of different widths is split at the leaf boundary
// when it is emitted; a range no interval covers, or one two arms cover at
// different widths, keeps a raw copy (elem 0), which is the pre-fix behaviour
// for exactly that range and no other.

type fixedSrcInterval struct {
	lo, hi, elem int64
}

func fixedKindElem(kind int, size int64) int64 {
	switch kind {
	case ir.TableKindBool, ir.TableKindI8, ir.TableKindU8, ir.TableKindFixed8, ir.TableKindUFixed8:
		return 1
	case ir.TableKindI16, ir.TableKindU16, ir.TableKindFixed16, ir.TableKindUFixed16:
		return 2
	case ir.TableKindI32, ir.TableKindU32, ir.TableKindF32, ir.TableKindFixed32, ir.TableKindUFixed32:
		return 4
	case ir.TableKindI64, ir.TableKindU64, ir.TableKindF64, ir.TableKindFixed64, ir.TableKindUFixed64, ir.TableKindTimestamp:
		return 8
	case ir.TableKindI128, ir.TableKindU128, ir.TableKindFixed128, ir.TableKindUFixed128:
		return 8
	case ir.TableKindEnum:
		return size
	case ir.TableKindString:
		return -1
	}
	return 0
}

func fixedSubtreeNext(entries []ir.TableFixedLayoutEntry, i int) int {
	next := i + 1
	for c := 0; c < entries[i].Children; c++ {
		next = fixedSubtreeNext(entries, next)
	}
	return next
}

func fixedSrcIntervals(entries []ir.TableFixedLayoutEntry) []fixedSrcInterval {
	var out []fixedSrcInterval
	add := func(lo, size, elem int64) {
		if size > 0 {
			out = append(out, fixedSrcInterval{lo: lo, hi: lo + size, elem: elem})
		}
	}
	var walk func(i int, src int64) int
	walk = func(i int, src int64) int {
		e := entries[i]
		switch {
		case e.Kind == ir.TableKindEnum || e.Children == 0:
			add(src, e.Size, fixedKindElem(e.Kind, e.Size))
			return fixedSubtreeNext(entries, i)
		case e.Kind == ir.TableKindTable:
			next, at := i+1, src
			for c := 0; c < e.Children; c++ {
				size := entries[next].Size
				next = walk(next, at)
				at += size
			}
			return next
		case e.Kind == ir.TableKindOptional:
			add(src, 1, 1)
			return walk(i+1, src+1)
		case e.Kind == ir.TableKindArray:
			child := i + 1
			childSize := entries[child].Size
			base := src
			if e.Dst.Counted != 0 {
				add(src, 4, -1)
				base += 4
			}
			if childSize <= 0 {
				return fixedSubtreeNext(entries, i)
			}
			next := fixedSubtreeNext(entries, i)
			for k := int64(0); k < (e.Size-(base-src))/childSize; k++ {
				walk(child, base+k*childSize)
			}
			return next
		case e.Kind == ir.TableKindKeyed:
			elemAt := fixedSubtreeNext(entries, i+1)
			elemSize := entries[elemAt].Size
			if elemSize <= 0 {
				return fixedSubtreeNext(entries, i)
			}
			for k := int64(0); k < e.Size/elemSize; k++ {
				walk(elemAt, src+k*elemSize)
			}
			return fixedSubtreeNext(entries, i)
		case e.Kind == ir.TableKindUnion:
			next, widest := i+1, int64(0)
			for c := 0; c < e.Children; c++ {
				if entries[next].Size > widest {
					widest = entries[next].Size
				}
				next = fixedSubtreeNext(entries, next)
			}
			tag := e.Size - widest
			add(src, tag, tag)
			next = i + 1
			for c := 0; c < e.Children; c++ {
				next = walk(next, src+tag)
			}
			return next
		default:
			next, at := i+1, src
			for c := 0; c < e.Children; c++ {
				size := entries[next].Size
				next = walk(next, at)
				at += size
			}
			return next
		}
	}
	if len(entries) > 0 {
		walk(0, 0)
	}
	return out
}

// fixedCopySegments splits one coalesced copy entry at leaf boundaries.
// ok is false when the range is not covered by one width per position, in
// which case the caller emits the entry whole with elem 0.
func fixedCopySegments(e ir.TableFixedLeaf, intervals []fixedSrcInterval) ([]ir.TableFixedLeaf, []int64, bool) {
	var out []ir.TableFixedLeaf
	var elems []int64
	limit := e.Src + e.Size
	for pos := e.Src; pos < limit; {
		var match *fixedSrcInterval
		for i := range intervals {
			iv := &intervals[i]
			if iv.lo <= pos && pos < iv.hi {
				if match != nil && match.elem != iv.elem {
					return nil, nil, false
				}
				if match == nil {
					match = iv
				}
			}
		}
		if match == nil || match.elem <= 0 {
			return nil, nil, false
		}
		end := min(match.hi, limit)
		sub := e
		sub.Src = pos
		sub.Dst = e.Dst + (pos - e.Src)
		sub.Size = end - pos
		out = append(out, sub)
		elems = append(elems, match.elem)
		pos = end
	}
	return out, elems, true
}

// FixedEmitEntry is one row of the emitted identity plan: a coalesced plan
// entry, or one width-uniform segment of one, and its element width (0 is a
// raw byte copy).
type FixedEmitEntry struct {
	Leaf ir.TableFixedLeaf
	Elem int64
}

// FixedElemPlan is the identity plan as both C emitters lay it down: the
// coalesced plan, split only where a copy entry spans leaves of different
// widths, so a big-endian copy knows its element width. It answers the rows
// and the new guarded split. The order inside each half is the plan's own.
func FixedElemPlan(entries []ir.TableFixedLayoutEntry, plan []ir.TableFixedLeaf, guarded int) ([]FixedEmitEntry, int) {
	intervals := fixedSrcIntervals(entries)
	var out []FixedEmitEntry
	outGuarded := 0
	for i, e := range plan {
		if i == guarded {
			outGuarded = len(out)
		}
		if e.Op == ir.TableFixedOpCopy {
			if segs, elems, ok := fixedCopySegments(e, intervals); ok {
				for j, s := range segs {
					out = append(out, FixedEmitEntry{s, elems[j]})
				}
				continue
			}
		}
		out = append(out, FixedEmitEntry{e, 0})
	}
	if guarded >= len(plan) {
		outGuarded = len(out)
	}
	return out, outGuarded
}
