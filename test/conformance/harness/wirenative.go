package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Native sizes are compile-time facts, announced once by a target. The
// independent scanner below combines them with the mutant's framing, never
// with its decoded values or a size supplied for that particular mutant.
type nativeSequence struct {
	size  uint64
	align uint32
}
type nativeRecord struct {
	size      uint64
	sequences map[uint64]nativeSequence
}
type nativeLayout struct {
	alignment uint32
	reason    uint32
	records   map[uint64]nativeRecord
}

func readNativeLayout(in io.Reader) (*nativeLayout, error) {
	var header [3]uint32
	if err := binary.Read(in, binary.LittleEndian, &header); err != nil {
		return nil, err
	}
	if header[0] != 2 || header[1] < 8 || header[1] > 64 || header[1]&(header[1]-1) != 0 || header[2] == 0 || header[2] > 100000 {
		return nil, fmt.Errorf("invalid native layout header %v", header)
	}
	layout := &nativeLayout{alignment: header[1], records: map[uint64]nativeRecord{}}
	for i := uint32(0); i < header[2]; i++ {
		var row [2]uint64
		var count uint32
		if err := binary.Read(in, binary.LittleEndian, &row); err != nil {
			return nil, err
		}
		if err := binary.Read(in, binary.LittleEndian, &count); err != nil {
			return nil, err
		}
		if row[1] > 1<<40 || count > 100000 {
			return nil, fmt.Errorf("invalid native record size/count")
		}
		record := nativeRecord{size: row[1], sequences: map[uint64]nativeSequence{}}
		for k := uint32(0); k < count; k++ {
			var field [2]uint64
			var align uint32
			if err := binary.Read(in, binary.LittleEndian, &field); err != nil {
				return nil, err
			}
			if err := binary.Read(in, binary.LittleEndian, &align); err != nil {
				return nil, err
			}
			if field[1] == 0 || field[1] > 1<<40 || align == 0 || align > 64 || align&(align-1) != 0 {
				return nil, fmt.Errorf("invalid native sequence layout")
			}
			if _, dup := record.sequences[field[0]]; dup {
				return nil, fmt.Errorf("duplicate native field")
			}
			record.sequences[field[0]] = nativeSequence{field[1], align}
		}
		if _, dup := layout.records[row[0]]; dup {
			return nil, fmt.Errorf("duplicate native record")
		}
		layout.records[row[0]] = record
	}
	return layout, nil
}

type nativeCursor struct {
	data []byte
	ids  []uint64
	at   int
	zero bool // Only reference zero is a terminator; id-table entries may themselves be zero.
}

func (r *nativeCursor) leb() (uint64, bool) {
	start := r.at
	var value uint64
	for i := 0; i < 10 && r.at < len(r.data); i++ {
		b := r.data[r.at]
		r.at++
		if i == 9 && b > 1 {
			break
		}
		value |= uint64(b&127) << uint(7*i)
		if b < 128 {
			if i > 0 && b == 0 {
				break
			}
			return value, true
		}
	}
	r.at = start
	return 0, false
}
func (r *nativeCursor) id() (uint64, bool) {
	ref, ok := r.leb()
	r.zero = ok && ref == 0
	if !ok || ref > uint64(len(r.ids)) {
		return 0, false
	}
	if ref == 0 {
		return 0, true
	}
	return r.ids[ref-1], true
}
func (r *nativeCursor) byte() (byte, bool) {
	if r.at >= len(r.data) {
		return 0, false
	}
	b := r.data[r.at]
	r.at++
	return b, true
}
func (r *nativeCursor) take() (nativeCursor, bool) {
	n, ok := r.leb()
	if !ok || n > uint64(len(r.data)-r.at) {
		return nativeCursor{}, false
	}
	out := nativeCursor{data: r.data[r.at : r.at+int(n)], ids: r.ids}
	r.at += int(n)
	return out, true
}
func (r *nativeCursor) skip(kind byte) bool {
	if kind == 17 || kind == 30 {
		_, ok := r.leb()
		return ok
	}
	if kind == 15 {
		ref, ok := r.leb()
		if !ok {
			return false
		}
		if ref == 0 {
			return true
		}
		if _, ok = r.byte(); !ok {
			return false
		}
		_, ok = r.take()
		return ok
	}
	if n := ir.TableKindWidth(int(kind)); n != 0 {
		if n > len(r.data)-r.at {
			return false
		}
		r.at += n
		return true
	}
	if kind == 12 || kind == 13 || kind == 14 || kind == 16 || kind == 31 || kind == 32 || kind == 33 {
		_, ok := r.take()
		return ok
	}
	return false
}
func nativeAlign(at uint64, alignment uint32) uint64 {
	return (at + uint64(alignment) - 1) &^ (uint64(alignment) - 1)
}
func (l *nativeLayout) refuse(reason uint32) bool { l.reason = reason; return false }

func (l *nativeLayout) extent(st *ir.Struct, body nativeCursor) (uint64, bool) {
	record, ok := l.records[ir.TableWireId(st.WireName())]
	if !ok {
		l.reason = 8
		return 0, false
	}
	at := record.size
	ok = l.body(st, body, &at)
	return nativeAlign(at, l.alignment), ok
}
func (l *nativeLayout) body(st *ir.Struct, r nativeCursor, at *uint64) bool {
	for {
		id, ok := r.id()
		if !ok || r.zero {
			return true
		}
		kind, ok := r.byte()
		if !ok {
			return true
		}
		var field *ir.Field
		for _, f := range st.Fields {
			if ir.TableFieldWireId(f) == id {
				field = f
				break
			}
		}
		if field == nil || kind != byte(ir.TableWireFieldKind(field)) {
			if !r.skip(kind) {
				return true
			}
			continue
		}
		if field.IsList() || field.IsMap() {
			body, ok := r.take()
			if !ok {
				return true
			}
			layout, ok := l.records[ir.TableWireId(st.WireName())].sequences[id]
			if !ok {
				return false
			}
			if !l.sequence(field, body, layout, at) {
				return false
			}
			continue
		}
		if field.Type.Pointer {
			if !r.skip(kind) {
				return true
			}
			continue
		}
		if field.Array != ir.ArrayNone {
			body, ok := r.take()
			if !ok {
				return true
			}
			if !l.inner(field, body, at) {
				return false
			}
			continue
		}
		if _, yes := field.Type.Ref.(*ir.Union); yes {
			item, ok := nativeElement(&r, 15)
			if !ok {
				return true
			}
			if !l.inner(field, item, at) {
				return false
			}
			continue
		}
		if _, yes := field.Type.Ref.(*ir.Struct); yes {
			item, ok := r.take()
			if !ok {
				return true
			}
			if !l.inner(field, item, at) {
				return false
			}
			continue
		}
		if !r.skip(kind) {
			return true
		}
	}
}
func nativeElement(r *nativeCursor, kind byte) (nativeCursor, bool) {
	if kind == 13 {
		return r.take()
	}
	start := r.at
	if !r.skip(kind) {
		return nativeCursor{}, false
	}
	return nativeCursor{data: r.data[start:r.at], ids: r.ids}, true
}
func (l *nativeLayout) inner(f *ir.Field, body nativeCursor, at *uint64) bool {
	if f.Type.Pointer {
		return true
	}
	if f.Array != ir.ArrayNone {
		kind, ok := body.byte()
		if !ok || kind != byte(ir.TableWireElemKind(f)) {
			return true
		}
		n, ok := body.leb()
		if !ok {
			return true
		}
		element := *f
		element.Array = ir.ArrayNone
		element.KeyEnum = ""
		for i := uint64(0); i < n; i++ {
			var item nativeCursor
			if f.KeyEnum != "" {
				if _, ok = body.id(); !ok {
					break
				}
				item, ok = body.take()
			} else {
				item, ok = nativeElement(&body, kind)
			}
			if !ok {
				break
			}
			if !l.inner(&element, item, at) {
				return false
			}
		}
		return true
	}
	if st, ok := f.Type.Ref.(*ir.Struct); ok {
		return l.body(st, body, at)
	}
	if union, ok := f.Type.Ref.(*ir.Union); ok {
		id, good := body.id()
		if !good || body.zero {
			return true
		}
		kind, good := body.byte()
		if !good {
			return true
		}
		payload, good := body.take()
		if !good {
			return true
		}
		for _, arm := range union.Variants {
			if ir.TableWireId(arm.WireName()) == id && arm.F != nil && byte(ir.TableWireFieldKind(arm.F)) == kind {
				return l.inner(arm.F, payload, at)
			}
		}
	}
	return true
}
func (l *nativeLayout) sequence(f *ir.Field, r nativeCursor, layout nativeSequence, at *uint64) bool {
	if len(r.data) < 2 {
		return true
	}
	kind, _ := r.byte()
	if kind != byte(ir.TableWireElemKind(f)) {
		return true
	}
	n, ok := r.leb()
	if !ok {
		return true
	}
	if n > math.MaxInt32 {
		return l.refuse(11)
	}
	floor := ir.TableKindWidth(int(kind))
	if kind == 17 {
		floor = 1
	}
	if floor == 0 {
		floor = 1
		if kind == 13 {
			floor = 2
		}
	}
	if n > uint64(len(r.data)-r.at)/uint64(floor) {
		return l.refuse(10)
	}
	*at = nativeAlign(*at, layout.align) + n*layout.size
	element := *f
	element.Array = ir.ArrayNone
	element.KeyEnum = ""
	if f.IsMap() {
		element.MapEntry = nil
		element.Type = ir.FieldType{Kind: ir.TNamed, Name: f.MapEntry.Name, Ref: f.MapEntry}
	}
	if element.Type.Pointer {
		return true
	}
	switch element.Type.Ref.(type) {
	case *ir.Struct, *ir.Union:
	default:
		return true
	}
	for i := uint64(0); i < n; i++ {
		item, ok := nativeElement(&r, kind)
		if !ok {
			break
		}
		if !l.inner(&element, item, at) {
			return false
		}
	}
	return true
}
func (r *wireRoot) nativeMeasure(data []byte) (int64, uint32) {
	layout := *r.native
	layout.reason = 0
	l := &layout
	if len(data) != 0 && data[0] != 1 {
		return -1, 9
	}
	body, ids, ok := tablewire.Trailer(data)
	if !ok || len(data) == 0 || data[0] != 1 {
		return -1, l.reason
	}
	size, ok := l.extent(r.def, nativeCursor{data: body, ids: ids})
	if !ok {
		return -1, l.reason
	}
	records := uint64(1)
	known := map[uint64]*ir.Struct{}
	for _, st := range ir.PointerReachable(r.def) {
		known[ir.TableWireId(st.WireName())] = st
	}
	bytes, text := ir.PointerReachableBlobs(r.def)
	cursor := nativeCursor{data: body, ids: ids}
	var nodes nativeCursor
	for {
		id, ok := cursor.id()
		if !ok || cursor.zero {
			break
		}
		kind, ok := cursor.byte()
		if !ok {
			break
		}
		if id != math.MaxUint64 {
			if !cursor.skip(kind) {
				break
			}
			continue
		}
		if kind != 12 {
			nodes = nativeCursor{}
			break
		}
		nodes, ok = cursor.take()
		if !ok {
			nodes = nativeCursor{}
			break
		}
	}
	if _, ok := nodes.leb(); ok {
		for nodes.at < len(nodes.data) {
			id, good := nodes.id()
			if !good || nodes.zero {
				break
			}
			node, good := nodes.take()
			if !good {
				break
			}
			records++
			if st := known[id]; st != nil {
				extent, good := l.extent(st, node)
				if !good {
					return -1, l.reason
				}
				size += extent
			}
			if id == ir.BytesWireTypeId && bytes || id == ir.StringWireTypeId && text {
				n := uint64(len(node.data))
				if n > math.MaxUint32 {
					return -1, 12
				}
				extra := uint64(0)
				if id == ir.StringWireTypeId {
					extra = 1
				}
				size += nativeAlign(8+n+extra, l.alignment)
			}
		}
	}
	if size > math.MaxInt64-records*16 {
		return -1, l.reason
	}
	return int64(size + records*16), 0
}
