// THE VALUED FIXTURE (docs/SPEC-TABLES.md §7.5).
//
// A chain of value-initialized nodes pins every node offset, every deref and
// every visit order — and almost no VALUES, because there are almost none in
// it. `--values` fills one, so a `cook` dump locks what a reader reads out of a
// node as well as where the node is. Nothing else about the fixture moves: the
// chain, the pitch and the deltas are the same arithmetic, and the file is
// still written STREAMING out of one reused record buffer.
//
// The values are a pure function of (record, field, seed), so the fixture stays
// deterministic — the harness regenerates it on every run and the dump is a
// pinned golden. They are chosen INSIDE every declared bound: a count companion
// never exceeds its array's bound, a used length never exceeds its string's,
// and an integer with a declared [min, max] lands inside it. A fixture that
// broke one would be testing the walk's refusal path, not its read.
package main

import (
	"encoding/binary"
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// filler writes one record's leaves.
type filler struct {
	unit *ir.Unit
	ord  binary.ByteOrder
	seed uint64
	leaf uint64 // bumped per leaf, so two fields of one record differ
}

// next is the raw number a leaf starts from: a pure function of the seed and
// the leaf's position in the walk, and nothing else.
func (f *filler) next() uint64 {
	f.leaf++
	return f.seed*1000003 + f.leaf*97 + 13
}

// fillRecord writes every leaf of one record into `at`, leaving POINTER slots
// alone — a reference is the chain's arithmetic and not a value to invent.
func (f *filler) fillRecord(at []byte, st *ir.Struct) error {
	ml := ir.RecordLayout(f.unit, st)
	for _, fl := range ml.Fields {
		field := fl.Field
		if field.Type.Pointer {
			continue
		}
		pieces := ir.BlockFieldPieceOffsets(f.unit, field, fl.Offset, false)
		facts := ir.BlockFieldOf(f.unit, field, fl.Offset, false)
		if facts.Optional {
			// an optional rides PRESENT, so the dump carries a value beside a
			// `#present = true` rather than only the absent shape
			at[facts.PresentOffset] = 1
		}
		switch {
		case field.Type.Kind == ir.TString || field.Type.Kind == ir.TBytes:
			// the buffer, then the used length — never past the declared max,
			// and the tail stays zero (§7.2)
			used := int64(f.next() % uint64(facts.ArrayBound+1))
			for i := range used {
				// printable ASCII, so a dump reads as text rather than escapes
				at[pieces[0].Offset+i] = byte('a' + (f.next()+uint64(i))%26)
			}
			f.putInt(at, pieces[1].Offset, 4, uint64(used))
		case facts.IsArray:
			for s := int64(0); s < facts.ArrayBound; s++ {
				if err := f.fillSlot(at, fl.Offset+s*facts.ElemSize, field); err != nil {
					return err
				}
			}
			if facts.Counted {
				// a counted array's live count, inside its bound
				f.putInt(at, facts.CountOffset, 4, f.next()%uint64(facts.ArrayBound+1))
			}
		default:
			if err := f.fillSlot(at, fl.Offset, field); err != nil {
				return err
			}
		}
	}
	return nil
}

// fillSlot writes ONE value of a field's declared type.
func (f *filler) fillSlot(at []byte, offset int64, field *ir.Field) error {
	t := field.Type
	switch t.Kind {
	case ir.TBool:
		at[offset] = byte(f.next() & 1)
		return nil
	case ir.TFloat32, ir.TFloat64:
		// The canonical node dump has no cross-language spelling for a float
		// and REFUSES one rather than inventing it (§7.5). A fixture that
		// carried one could not be dumped, so this refuses at the source.
		return fmt.Errorf("%s.%s is a float, which the canonical node dump has no spelling for", field.Name, t.Name)
	case ir.TInt:
		width := int64(t.Width) / 8
		f.putInt(at, offset, width, f.bounded(field, width, t.Signed))
		return nil
	case ir.TBits:
		width := int64(4)
		if t.Width > 32 {
			width = 8
		}
		f.putInt(at, offset, width, f.next()%(uint64(1)<<uint(t.Width)))
		return nil
	case ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Enum:
			width := int64(ir.StorageBitsFor(ref.Max)) / 8
			// an ORDINAL, and every value in [0, Max] is one (§7.2)
			f.putInt(at, offset, width, f.next()%uint64(ref.Max+1))
			return nil
		case *ir.Flags:
			// a MASK, bounded by the declared bit count: a bit no variant
			// names is a value the form does not have
			bits := uint(len(ref.Variants))
			f.putInt(at, offset, 8, f.next()&((uint64(1)<<bits)-1))
			return nil
		case *ir.Struct:
			return f.fillRecord(at[offset:], ref)
		}
	}
	return fmt.Errorf("%s has a type this fixture generator cannot value", field.Name)
}

// bounded is a value inside the field's declared [min, max], or inside its
// storage width when it declares none.
func (f *filler) bounded(field *ir.Field, width int64, signed bool) uint64 {
	raw := f.next()
	if field.HasIntRange && field.IntMin != nil && field.IntMax != nil &&
		field.IntMin.IsInt64() && field.IntMax.IsInt64() {
		lo, hi := field.IntMin.Int64(), field.IntMax.Int64()
		if hi >= lo {
			span := uint64(hi-lo) + 1
			return uint64(lo + int64(raw%span))
		}
	}
	if width >= 8 {
		return raw
	}
	bits := uint(width * 8)
	if signed {
		// stay in the POSITIVE half, so a narrowing cast in any reader cannot
		// turn the fixture's value into a sign question
		return raw % (uint64(1) << (bits - 1))
	}
	return raw % (uint64(1) << bits)
}

func (f *filler) putInt(at []byte, offset, width int64, v uint64) {
	switch width {
	case 1:
		at[offset] = byte(v)
	case 2:
		f.ord.PutUint16(at[offset:], uint16(v))
	case 4:
		f.ord.PutUint32(at[offset:], uint32(v))
	default:
		f.ord.PutUint64(at[offset:], v)
	}
}
