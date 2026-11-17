package ctable

import (
	"cmp"
	"slices"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// fixedDeadMeta records the wire extents that close dead spans. A union tag
// and an enclosing optional also carry their active arm context: overlaid arms
// may declare different values at the same offset.
type fixedDeadMeta struct {
	counts   []fixedExtentScope
	presents []fixedExtentScope
	unions   []fixedUnionDeadScope
}

type fixedUnionDeadScope struct {
	at     int64
	guards []ir.TableFixedGuardLink
	info   fixedUnionDead
}
type fixedExtentScope struct {
	at, width int64
	guards    []ir.TableFixedGuardLink
}

type fixedUnionDead struct {
	tagWidth int64
	widest   int64
	arms     []int64 // indexed by the 1-based arm ordinal the plan carries
}

// fixedDeadMetaOf walks a fixed root's declared fields and records, at each
// wire offset it can, how many bytes past the live extent are DEAD
// (docs/FIXED-FORM-ALGORITHM.md §3.1, §4.5, §5.3; docs/SPEC-TABLES.md §3.4,
// §13.10). The walk is by declared field order and size, the same offsets the
// emitter lays the plan at.
func fixedDeadMetaOf(u *ir.Unit, st *ir.Struct) fixedDeadMeta {
	m := fixedDeadMeta{}
	m.walkStruct(u, st, 0, nil)
	for _, extents := range [][]fixedExtentScope{m.counts, m.presents} {
		slices.SortStableFunc(extents, func(a, b fixedExtentScope) int { return cmp.Compare(a.at, b.at) })
	}
	slices.SortStableFunc(m.unions, func(a, b fixedUnionDeadScope) int { return cmp.Compare(a.at, b.at) })
	return m
}

func (m *fixedDeadMeta) walkStruct(u *ir.Unit, st *ir.Struct, at int64, guards []ir.TableFixedGuardLink) {
	for _, f := range st.Fields {
		m.walkField(u, f, at, guards)
		at += ir.TableFixedFieldBytes(f)
	}
}

func (m *fixedDeadMeta) walkField(u *ir.Unit, f *ir.Field, at int64, guards []ir.TableFixedGuardLink) {
	if f.Type.Optional {
		m.presents = append(m.presents, fixedExtentScope{at: at, width: ir.TableFixedFieldBytes(f) - ir.TableFixedPresentBytes, guards: slices.Clone(guards)})
		m.walkPayload(u, f, at+ir.TableFixedPresentBytes, guards)
		return
	}
	m.walkPayload(u, f, at, guards)
}

func (m *fixedDeadMeta) walkPayload(u *ir.Unit, f *ir.Field, at int64, guards []ir.TableFixedGuardLink) {
	elem := ir.TableFixedElementBytes(f)
	switch {
	case f.KeyEnum != "":
		for i := int64(0); i < f.KeyEnumRef.Max; i++ {
			m.walkElement(u, f, at+i*elem, guards)
		}
	case f.Array == ir.ArrayFixed:
		for i := int64(0); i < f.ArrayBound; i++ {
			m.walkElement(u, f, at+i*elem, guards)
		}
	case f.Array == ir.ArrayCounted:
		m.counts = append(m.counts, fixedExtentScope{at: at, width: elem, guards: slices.Clone(guards)})
		for i := int64(0); i < f.ArrayBound; i++ {
			m.walkElement(u, f, at+ir.TableFixedCountBytes+i*elem, guards)
		}
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		// the length is at `at`; the used bytes are live and the rest is dead,
		// closed by the plan's text entry at the same offset.
	default:
		m.walkElement(u, f, at, guards)
	}
}

// presentOf is the wire offset of the INNERMOST optional present byte whose
// payload contains src, or -1 when src is under no optional. The plan carries it
// as the entry's present lane so an absent payload's entries do not run
// (docs/rulings.json fixed-dead-bytes-are-zero).
func (m fixedDeadMeta) presentOf(src int64, guards []ir.TableFixedGuardLink) int64 {
	best := int64(-1)
	for _, scope := range m.presents {
		if len(scope.guards) > len(guards) || !slices.Equal(scope.guards, guards[:len(scope.guards)]) {
			continue
		}
		if src > scope.at && src <= scope.at+scope.width && scope.at > best {
			best = scope.at
		}
	}
	return best
}

func (m *fixedDeadMeta) walkElement(u *ir.Unit, f *ir.Field, at int64, guards []ir.TableFixedGuardLink) {
	if f.Type.Kind != ir.TNamed {
		return
	}
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		m.walkStruct(u, r, at, guards)
	case *ir.Union:
		tag := ir.TableFixedUnionTagBytes(r)
		widest := int64(0)
		arms := make([]int64, len(r.Variants)+1)
		for i, v := range r.Variants {
			if v.F == nil {
				continue
			}
			size := ir.TableFixedFieldBytes(v.F)
			arms[i+1] = size
			if size > widest {
				widest = size
			}
		}
		m.unions = append(m.unions, fixedUnionDeadScope{at: at, guards: slices.Clone(guards), info: fixedUnionDead{tagWidth: tag, widest: widest, arms: arms}})
		for i, v := range r.Variants {
			if v.F == nil {
				continue
			}
			next := append(slices.Clone(guards), ir.TableFixedGuardLink{Guard: at, Arg: i + 1, ArgW: int(tag)})
			m.walkField(u, v.F, at+tag, next)
		}
	}
}

// fixedExtentAt selects a count stride or optional payload width under the
// entry's complete union context, rather than borrowing an overlaid arm's span.
func fixedExtentAt(scopes []fixedExtentScope, at int64, guards []ir.TableFixedGuardLink) int64 {
	i, _ := slices.BinarySearchFunc(scopes, at, func(scope fixedExtentScope, at int64) int { return cmp.Compare(scope.at, at) })
	for ; i < len(scopes) && scopes[i].at == at; i++ {
		if slices.Equal(scopes[i].guards, guards) {
			return scopes[i].width
		}
	}
	return 0
}

// unionAt resolves a tag in its actual enclosing arms. Overlaying arms may
// put different declarations at the same byte offset.
func (m fixedDeadMeta) unionAt(at int64, guards []ir.TableFixedGuardLink) (fixedUnionDead, bool) {
	for i := m.unionIndex(at); i < len(m.unions) && m.unions[i].at == at; i++ {
		scope := m.unions[i]
		if slices.Equal(scope.guards, guards) {
			return scope.info, true
		}
	}
	return fixedUnionDead{}, false
}

func (m fixedDeadMeta) unionIndex(at int64) int {
	i, _ := slices.BinarySearchFunc(m.unions, at, func(scope fixedUnionDeadScope, at int64) int { return cmp.Compare(scope.at, at) })
	return i
}

// splitUnionTags preserves the tag entries used by the dead-byte check. The
// IR coalescer may otherwise fold a preceding scalar and a nested tag into a
// single copy, leaving no tag row to carry the inactive-arm span. Only tags
// in the copy's exact guard context are boundaries; another overlaid arm's
// scalar must retain its own interpretation.
func (m fixedDeadMeta) splitUnionTags(plan []ir.TableFixedLeaf, guarded int) ([]ir.TableFixedLeaf, int) {
	var out []ir.TableFixedLeaf
	split := 0
	for i, e := range plan {
		if i == guarded {
			split = len(out)
		}
		cuts := []int64{e.Src, e.Src + e.Size}
		if e.Op == ir.TableFixedOpCopy {
			for j := m.unionIndex(e.Src); j < len(m.unions) && m.unions[j].at < e.Src+e.Size; j++ {
				scope := m.unions[j]
				end := scope.at + scope.info.tagWidth
				if scope.at >= e.Src && end <= e.Src+e.Size && slices.Equal(scope.guards, e.GuardLinks) {
					cuts = append(cuts, scope.at, end)
				}
			}
		}
		slices.Sort(cuts)
		cuts = slices.Compact(cuts)
		if len(cuts) == 1 {
			out = append(out, e)
			continue
		}
		for j := range len(cuts) - 1 {
			part := e
			part.Src = cuts[j]
			part.Dst = e.Dst + cuts[j] - e.Src
			part.Size = cuts[j+1] - cuts[j]
			out = append(out, part)
		}
	}
	if guarded >= len(plan) {
		split = len(out)
	}
	return out, split
}
