package ctable

import "github.com/mas-bandwidth/schema/v2/ir"

// fixedDeadMeta is the identity plan's dead-byte map, keyed by the WIRE offset
// of the count, present flag or union tag that closes the dead span. It is the
// same facts a compiled plan carries in the plan's dead/arm/tagw lanes.
type fixedDeadMeta struct {
	counts   map[int64]int64
	presents map[int64]int64
	unions   map[int64]fixedUnionDead
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
	m := fixedDeadMeta{
		counts:   map[int64]int64{},
		presents: map[int64]int64{},
		unions:   map[int64]fixedUnionDead{},
	}
	m.walkStruct(u, st, 0)
	return m
}

func (m fixedDeadMeta) walkStruct(u *ir.Unit, st *ir.Struct, at int64) {
	for _, f := range st.Fields {
		m.walkField(u, f, at)
		at += ir.TableFixedFieldBytes(f)
	}
}

func (m fixedDeadMeta) walkField(u *ir.Unit, f *ir.Field, at int64) {
	if f.Type.Optional {
		m.presents[at] = ir.TableFixedFieldBytes(f) - ir.TableFixedPresentBytes
		m.walkPayload(u, f, at+ir.TableFixedPresentBytes)
		return
	}
	m.walkPayload(u, f, at)
}

func (m fixedDeadMeta) walkPayload(u *ir.Unit, f *ir.Field, at int64) {
	elem := ir.TableFixedElementBytes(f)
	switch {
	case f.KeyEnum != "":
		for i := int64(0); i < f.KeyEnumRef.Max; i++ {
			m.walkElement(u, f, at+i*elem)
		}
	case f.Array == ir.ArrayFixed:
		for i := int64(0); i < f.ArrayBound; i++ {
			m.walkElement(u, f, at+i*elem)
		}
	case f.Array == ir.ArrayCounted:
		m.counts[at] = elem
		for i := int64(0); i < f.ArrayBound; i++ {
			m.walkElement(u, f, at+ir.TableFixedCountBytes+i*elem)
		}
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		// the length is at `at`; the used bytes are live and the rest is dead,
		// closed by the plan's text entry at the same offset.
	default:
		m.walkElement(u, f, at)
	}
}

func (m fixedDeadMeta) walkElement(u *ir.Unit, f *ir.Field, at int64) {
	if f.Type.Kind != ir.TNamed {
		return
	}
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		m.walkStruct(u, r, at)
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
		m.unions[at] = fixedUnionDead{tagWidth: tag, widest: widest, arms: arms}
		for _, v := range r.Variants {
			if v.F == nil {
				continue
			}
			m.walkField(u, v.F, at+tag)
		}
	}
}
