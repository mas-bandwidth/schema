// The REGION: what `Lock` packs and what a cook writes verbatim
// (SPEC-TABLES.md §6.2, §6.3, §7).
//
// A region is the nodes of one pointer graph laid back to back with zero slack,
// the root at its base, each at its own type's alignment, in the DEPTH-FIRST
// PRE-ORDER the wire numbers nodes in — it is the same walk, so a wire and a
// region agree on which node is which without either carrying an order.
//
// A record's layout is the compiler's own C ABI model (§20.3), taken from
// ir.RecordLayout: the one place the numbers come from, so a cook's bytes, a
// C++ static_assert, a C# generated check and the build version's `record`
// lines can never disagree about what the layout IS.
package tablecook

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// RefBytes is a `*T` reference slot's width in a record: four bytes, at four
// (SPEC-TABLES.md §6.3). In the arena it is the node's arena offset; IN A
// REGION IT IS THE SELF-RELATIVE BYTE DELTA FROM THE SLOT'S OWN ADDRESS, so a
// deref is one add and needs no base pointer at all, and a whole region
// relocates by plain memcpy with zero fix-up.
const RefBytes = int64(4)

// RefNull is the region encoding of a null reference: a delta of zero. A slot
// can never hold the address of the node that contains it, so zero names no
// node and is free to mean null — which is the same value null takes on the
// wire, where index `0` is null (§3.1).
const RefNull = int32(0)

// Node is one node of a laid-out region: where it begins, what it is, and the
// instance whose values fill it.
type Node struct {
	Offset int64
	Def    *ir.Struct
	Inst   *tabletext.Instance
	Layout *ir.MemberLayout
}

// Region is one packed region: its nodes in index order — position `i` is node
// index `i + 1`, so position `0` is the root at offset `0` — the byte extent
// they occupy, and the alignment its base requires.
type Region struct {
	Nodes []Node
	Bytes int64
	Align int64
}

// Layout packs one numbering into a region: the same walk `Lock` does, with the
// same result. Every node starts at its own type's alignment and the
// directory's offsets are those PADDED STARTS, so "is a directory entry" and
// "is aligned" are one check rather than two (§6.3).
func Layout(m *tabletext.Model, g *tablewire.NodeGraph) (*Region, error) {
	r := &Region{Align: ir.RegionAlignFloor}
	offset := int64(0)
	for _, inst := range g.Nodes {
		ml := ir.RecordLayout(m.Unit, inst.Def)
		if ml.Align > r.Align {
			r.Align = ml.Align
		}
		offset = alignUp(offset, ml.Align)
		r.Nodes = append(r.Nodes, Node{Offset: offset, Def: inst.Def, Inst: inst, Layout: ml})
		offset += ml.Size
	}
	// the data part's LENGTH is rounded to the region's alignment, which is what
	// puts the attribution part that follows it on an eight-byte boundary and
	// keeps a second cook's data part placeable at the same offset
	r.Bytes = alignUp(offset, r.Align)
	if len(r.Nodes) > 0 && r.Nodes[0].Offset != 0 {
		return nil, fmt.Errorf("internal: the root is not at the region's base")
	}
	return r, nil
}

// order is the byte order a cook is produced in. It is settled AT COOK TIME for
// the target build (§7), so the reading side runs no fix-up pass at all — which
// is what makes `Open` a match and a point rather than a pass over the region.
type order = binary.ByteOrder

// Write renders the region's bytes in one byte order. Every scalar is written
// through `order`, never memcpy'd out of a Go value, because a big-endian cook
// is a byte-swap of every scalar the region holds and a copy would swap none of
// them.
//
// EVERY BYTE THE FIELDS DO NOT COVER IS ZERO — interior padding, a record's
// trailing padding, a string's unused tail, the bytes of a union outside its set
// arm, and the slack between the last node and the data part's rounded length.
// A cook is content-addressed by (asset hash, build version) (§7), so two cooks
// of one wire must be one artifact, and an uninitialized pad byte would make
// them two.
func (r *Region) Write(m *tabletext.Model, ord order) ([]byte, error) {
	w := &regionWriter{m: m, ord: ord, buf: make([]byte, r.Bytes), region: r}
	for i := range r.Nodes {
		n := &r.Nodes[i]
		if err := w.record(n.Offset, n.Def, n.Inst); err != nil {
			return nil, err
		}
	}
	return w.buf, nil
}

type regionWriter struct {
	m      *tabletext.Model
	ord    order
	buf    []byte
	region *Region
	index  map[*tabletext.Instance]int64 // node -> its region offset
}

// nodeOffset is where one referenced node sits. The map is built lazily because
// a region with no pointer slot never needs it.
func (w *regionWriter) nodeOffset(inst *tabletext.Instance) (int64, bool) {
	if w.index == nil {
		w.index = make(map[*tabletext.Instance]int64, len(w.region.Nodes))
		for _, n := range w.region.Nodes {
			w.index[n.Inst] = n.Offset
		}
	}
	at, ok := w.index[inst]
	return at, ok
}

func (w *regionWriter) record(base int64, st *ir.Struct, inst *tabletext.Instance) error {
	ml := ir.RecordLayout(w.m.Unit, st)
	for i := range ml.Fields {
		fl := &ml.Fields[i]
		fv := instField(inst, fl.Field)
		if fv == nil {
			continue
		}
		if err := w.field(base+fl.Offset, fl.Field, fv); err != nil {
			return err
		}
	}
	return nil
}

// field writes one field's whole storage, PIECE BY PIECE. A field is not always
// one piece — a `string(N)` is a buffer AND an int32 used length, a counted
// array is its elements AND an int32 count, an optional adds a presence bool —
// and the C ABI aligns each piece on its own, so the offsets come from
// ir.FieldPieces rather than from arithmetic here (§19.3).
func (w *regionWriter) field(at int64, f *ir.Field, fv *tabletext.Field) error {
	pieces := ir.FieldPieces(w.m.Unit, f, at)
	if len(pieces) == 0 {
		return nil
	}
	value := pieces[0]
	if f.Type.Optional {
		// the presence companion is the LAST piece, and it is a slot the other
		// side reads (§20.2's `optional=true`)
		p := pieces[len(pieces)-1]
		w.putBool(p.Offset, fv.Present)
	}
	switch {
	case f.Type.Pointer:
		return w.ref(value.Offset, f, fv.Cell.Node)
	case f.Type.Kind == ir.TString:
		// char[N+1]: the used bytes, then a zero tail — the terminator the
		// generated buffer carries is the first byte of that tail
		w.putBytes(value.Offset, value.Size, fv.Cell.Str)
		w.putI32(pieces[1].Offset, int32(len(fv.Cell.Str)))
		return nil
	case f.Type.Kind == ir.TBytes:
		w.putBytes(value.Offset, value.Size, fv.Cell.Str)
		w.putI32(pieces[1].Offset, int32(len(fv.Cell.Str)))
		return nil
	case f.KeyEnum != "":
		// EVERY SLOT EXISTS in an enum-keyed array, slot 0 included: the array
		// is indexed by the enum value itself, so slot `v` is variant `v`'s and
		// slot 0 is None's, which names no record and holds the element's zero
		// (§2.4). The wire rides them BY NAME (§3.2); the region rides them by
		// position, and the two meet in the enum's own values.
		return w.slots(value.Offset, f, fv.Elems, len(fv.Elems))
	case f.Array == ir.ArrayFixed:
		return w.slots(value.Offset, f, fv.Elems, len(fv.Elems))
	case f.Array == ir.ArrayCounted:
		// all N slots are written: the storage is allocate-max, and a slot past
		// the live count holds the VALUE-INITIALIZED element — zero for a
		// scalar, the element type's own declared defaults for a record — which
		// is what `T x[N] = {}` produces and what a wire load leaves there
		if err := w.slots(value.Offset, f, fv.Elems, len(fv.Elems)); err != nil {
			return err
		}
		w.putI32(pieces[1].Offset, int32(fv.Count))
		return nil
	}
	return w.element(value.Offset, f, &fv.Cell)
}

func (w *regionWriter) slots(at int64, f *ir.Field, elems []tabletext.Cell, n int) error {
	step := elementBytes(w.m.Unit, f)
	for i := range n {
		if err := w.element(at+int64(i)*step, f, &elems[i]); err != nil {
			return err
		}
	}
	return nil
}

// ref writes a reference slot: the SELF-RELATIVE byte delta from the slot's own
// address to the node's start, and zero for null.
func (w *regionWriter) ref(at int64, f *ir.Field, target *tabletext.Instance) error {
	if target == nil {
		w.putI32(at, RefNull)
		return nil
	}
	to, ok := w.nodeOffset(target)
	if !ok {
		return fmt.Errorf("field %s references a node the numbering does not carry — a cook refuses a partial region (SPEC-TABLES.md §7)", f.Name)
	}
	delta := to - at
	if delta < math.MinInt32 || delta > math.MaxInt32 {
		return fmt.Errorf("field %s: the region reference is %d bytes and a reference slot is four (SPEC-TABLES.md §6.3) — this region is past the reach of a self-relative delta; cook refuses it rather than widening a slot the build version was taken over", f.Name, delta)
	}
	if delta == int64(RefNull) {
		return fmt.Errorf("internal: a reference slot resolved to itself")
	}
	w.putI32(at, int32(delta))
	return nil
}

// element writes one VALUE of a field's declared type — an array element, or
// the scalar itself.
func (w *regionWriter) element(at int64, f *ir.Field, cell *tabletext.Cell) error {
	t := f.Type
	switch t.Kind {
	case ir.TBool:
		w.putBool(at, cell.B)
		return nil
	case ir.TFloat32:
		w.putU32(at, math.Float32bits(float32(cell.F)))
		return nil
	case ir.TFloat64:
		w.putU64(at, math.Float64bits(cell.F))
		return nil
	case ir.TInt:
		w.putWidth(at, int64(t.Width)/8, cell.U)
		return nil
	case ir.TBits:
		if t.Width <= 32 {
			w.putU32(at, uint32(cell.U))
		} else {
			w.putU64(at, cell.U)
		}
		return nil
	case ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Enum:
			// the slot stores the ORDINAL, not the wire's name hash: what group
			// 3 of the build version captures is what a slot HOLDS (§20.2)
			w.putWidth(at, int64(ir.StorageBitsFor(ref.Max))/8, cell.U)
			return nil
		case *ir.Flags:
			w.putU64(at, cell.U) // a mask rides raw, in every target
			return nil
		case *ir.Struct:
			sub := cell.Tab
			if sub == nil {
				sub = w.m.New(ref)
			}
			return w.record(at, ref, sub)
		case *ir.Union:
			return w.union(at, ref, cell)
		}
	}
	return fmt.Errorf("field %s: a type with no cooked storage reached a region", f.Name)
}

// union writes the generated union: the TAG at offset 0 at its own alignment,
// the SET ARM at the arms' shared offset, and nothing else — every byte of the
// extent outside the set arm stays zero, which is the arm-zeroing shape the
// stale-leak test pins (§4.8) taken to a region.
func (w *regionWriter) union(at int64, un *ir.Union, cell *tabletext.Cell) error {
	_, _, tag, armOffset := ir.UnionLayout(w.m.Unit, un)
	w.putWidth(at, tag, cell.U)
	if cell.U == 0 || cell.Tab == nil {
		return nil // None is the tag alone
	}
	if int(cell.U) > len(un.Variants) {
		return fmt.Errorf("union %s: tag %d names no arm", un.Name, cell.U)
	}
	arm := un.Variants[cell.U-1]
	return w.record(at+armOffset, arm.Ref, cell.Tab)
}

// ---- the scalar stores, every one of them through the target's byte order ----

func (w *regionWriter) putBool(at int64, v bool) {
	if v {
		w.buf[at] = 1
	} else {
		w.buf[at] = 0
	}
}

func (w *regionWriter) putU32(at int64, v uint32) { w.ord.PutUint32(w.buf[at:], v) }
func (w *regionWriter) putU64(at int64, v uint64) { w.ord.PutUint64(w.buf[at:], v) }
func (w *regionWriter) putI32(at int64, v int32)  { w.ord.PutUint32(w.buf[at:], uint32(v)) }

func (w *regionWriter) putWidth(at, width int64, v uint64) {
	switch width {
	case 1:
		w.buf[at] = uint8(v)
	case 2:
		w.ord.PutUint16(w.buf[at:], uint16(v))
	case 4:
		w.ord.PutUint32(w.buf[at:], uint32(v))
	case 8:
		w.ord.PutUint64(w.buf[at:], v)
	}
}

// putBytes writes a buffer piece: the used bytes, and a zero tail to its full
// declared width.
func (w *regionWriter) putBytes(at, size int64, src []byte) {
	n := min(int64(len(src)), size)
	copy(w.buf[at:at+n], src[:n])
	for i := at + n; i < at+size; i++ {
		w.buf[i] = 0
	}
}

// ---- shared helpers ----

func alignUp(v, a int64) int64 {
	if a <= 1 {
		return v
	}
	return (v + a - 1) / a * a
}

// elementBytes is the stride of one array slot: the element's own storage size,
// which is what ir.FieldPieces already sized the whole array by.
func elementBytes(u *ir.Unit, f *ir.Field) int64 {
	single := *f
	single.Array = ir.ArrayNone
	single.ArrayBound = 0
	single.KeyEnum = ""
	single.KeyEnumRef = nil
	single.Type.Optional = false
	pieces := ir.FieldPieces(u, &single, 0)
	var end int64
	for _, p := range pieces {
		if p.Offset+p.Size > end {
			end = p.Offset + p.Size
		}
	}
	return end
}

// instField is one instance's storage for a declaration's field. A layout entry
// and an instance are two views of one record and their fields agree in order,
// but the lookup is by identity rather than by index so a caller may hand in an
// instance built from the same declaration by any route.
func instField(inst *tabletext.Instance, f *ir.Field) *tabletext.Field {
	for i := range inst.Fields {
		if inst.Fields[i].Def == f {
			return &inst.Fields[i]
		}
	}
	return nil
}
