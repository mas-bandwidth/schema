package ir

// ---- the identity plan's element widths ------------------------------------
//
// A coalesced copy entry carries no element width, and a big-endian reader
// needs one per element (docs/SPEC-TABLES.md §3.4: the wire is little-endian on
// every host). The widths are recovered here from the layout walk the root
// already carries: each leaf's kind fixes its width, an array repeats its
// element, a union's arms overlap at the tag, and a counted array's count is not
// a copy. A copy entry that spans leaves of different widths is split at the
// leaf boundary when it is emitted; a range no interval covers, or one two arms
// cover at different widths, keeps a raw copy (elem 0), which is the pre-fix
// behaviour for exactly that range and no other.
//
// THE C AND C++ LEGS LAY DOWN THE SAME SPLIT PLAN. It lives here and not in
// either backend because the fixed-form twin gate (tools/fixedtwin) holds the
// two emitted identity plans to each other line for line: one answer, rendered
// twice.

type tableFixedSrcInterval struct {
	lo, hi, elem int64
}

func tableFixedKindElem(kind int, size int64) int64 {
	switch kind {
	case TableKindBool, TableKindI8, TableKindU8, TableKindFixed8, TableKindUFixed8:
		return 1
	case TableKindI16, TableKindU16, TableKindFixed16, TableKindUFixed16:
		return 2
	case TableKindI32, TableKindU32, TableKindF32, TableKindFixed32, TableKindUFixed32:
		return 4
	case TableKindI64, TableKindU64, TableKindF64, TableKindFixed64, TableKindUFixed64, TableKindTimestamp:
		return 8
	case TableKindI128, TableKindU128, TableKindFixed128, TableKindUFixed128:
		return 8
	case TableKindEnum:
		return size
	case TableKindString:
		return -1
	}
	return 0
}

func tableFixedSubtreeNext(entries []TableFixedLayoutEntry, i int) int {
	next := i + 1
	for c := 0; c < entries[i].Children; c++ {
		next = tableFixedSubtreeNext(entries, next)
	}
	return next
}

func tableFixedSrcIntervals(entries []TableFixedLayoutEntry) []tableFixedSrcInterval {
	var out []tableFixedSrcInterval
	add := func(lo, size, elem int64) {
		if size > 0 {
			out = append(out, tableFixedSrcInterval{lo: lo, hi: lo + size, elem: elem})
		}
	}
	var walk func(i int, src int64) int
	walk = func(i int, src int64) int {
		e := entries[i]
		switch {
		case e.Kind == TableKindEnum || e.Children == 0:
			add(src, e.Size, tableFixedKindElem(e.Kind, e.Size))
			return tableFixedSubtreeNext(entries, i)
		case e.Kind == TableKindTable:
			next, at := i+1, src
			for c := 0; c < e.Children; c++ {
				size := entries[next].Size
				next = walk(next, at)
				at += size
			}
			return next
		case e.Kind == TableKindOptional:
			add(src, 1, 1)
			return walk(i+1, src+1)
		case e.Kind == TableKindArray:
			child := i + 1
			childSize := entries[child].Size
			base := src
			if e.Dst.Counted != 0 {
				add(src, 4, -1)
				base += 4
			}
			if childSize <= 0 {
				return tableFixedSubtreeNext(entries, i)
			}
			next := tableFixedSubtreeNext(entries, i)
			for k := int64(0); k < (e.Size-(base-src))/childSize; k++ {
				walk(child, base+k*childSize)
			}
			return next
		case e.Kind == TableKindKeyed:
			elemAt := tableFixedSubtreeNext(entries, i+1)
			elemSize := entries[elemAt].Size
			if elemSize <= 0 {
				return tableFixedSubtreeNext(entries, i)
			}
			for k := int64(0); k < e.Size/elemSize; k++ {
				walk(elemAt, src+k*elemSize)
			}
			return tableFixedSubtreeNext(entries, i)
		case e.Kind == TableKindUnion:
			next, widest := i+1, int64(0)
			for c := 0; c < e.Children; c++ {
				if entries[next].Size > widest {
					widest = entries[next].Size
				}
				next = tableFixedSubtreeNext(entries, next)
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

// tableFixedCopySegments splits one coalesced copy entry at leaf boundaries.
// ok is false when the range is not covered by one width per position, in
// which case the caller emits the entry whole with elem 0.
func tableFixedCopySegments(e TableFixedLeaf, intervals []tableFixedSrcInterval) ([]TableFixedLeaf, []int64, bool) {
	var out []TableFixedLeaf
	var elems []int64
	limit := e.Src + e.Size
	for pos := e.Src; pos < limit; {
		var match *tableFixedSrcInterval
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

// TableFixedWidthPlan is the identity plan as the C and C++ legs emit it: the
// coalesced plan, split only where a copy entry spans leaves of different
// widths, so a big-endian copy knows its element width. Elems[i] is entry i's
// element width in bytes (0 for a raw copy and every non-copy op) and Guarded
// is the split, recounted over the split entries.
type TableFixedWidthPlan struct {
	Plan    []TableFixedLeaf
	Elems   []int64
	Guarded int
}

// TableFixedSplitByWidth splits plan (guarded entries from guarded on) at the
// leaf boundaries the layout entries name.
func TableFixedSplitByWidth(entries []TableFixedLayoutEntry, plan []TableFixedLeaf, guarded int) TableFixedWidthPlan {
	intervals := tableFixedSrcIntervals(entries)
	var out TableFixedWidthPlan
	for i, e := range plan {
		if i == guarded {
			out.Guarded = len(out.Plan)
		}
		if e.Op == TableFixedOpCopy {
			if segs, elems, ok := tableFixedCopySegments(e, intervals); ok {
				out.Plan = append(out.Plan, segs...)
				out.Elems = append(out.Elems, elems...)
				continue
			}
		}
		out.Plan = append(out.Plan, e)
		out.Elems = append(out.Elems, 0)
	}
	if guarded >= len(plan) {
		out.Guarded = len(out.Plan)
	}
	return out
}
