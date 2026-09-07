package tablewire

import (
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
	"math"
)

// FileRegionMeasure derives canonical region storage from framing, before
// decoding values. In particular, dropped map entries still consume the
// array extent declared on the wire. Generated runtimes never call this
// independent compiler-side oracle.
func FileRegionMeasure(u *ir.Unit, root *ir.Struct, data []byte) (bytes int64, whole, refused bool) {
	body, ids, ok := trailer(data)
	if len(data) == 0 || data[0] != ir.TableWireForm || !ok {
		return 0, false, false
	}
	align := int64(8)
	for name := range ir.TableClosure(u) {
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st != nil {
			align = max(align, ir.RecordLayout(u, st).Align)
		}
	}
	round := func(n int64) int64 { return (n + align - 1) & -align }
	size := func(st *ir.Struct, body []byte) (int64, bool) {
		walk := extentScan{u: u, ids: ids}
		if !walk.record(st, body) {
			return 0, false
		}
		return round(round(ir.RecordLayout(u, st).Size) + walk.at), true
	}
	total, ok := size(root, body)
	if !ok {
		return -1, true, true
	}
	payload, present, framed := nodeTableBytes(body, ids, &tabletext.Report{})
	if present && !framed {
		return 0, false, false
	}
	var records []nodeRecord
	if present {
		records, ok = scanNodeRecords(payload, ids)
		if !ok {
			return 0, false, false
		}
	}
	nodes := map[uint64]*ir.Struct{}
	for _, st := range ir.PointerReachable(root) {
		nodes[ir.TableWireId(st.WireName())] = st
	}
	bytesBlob, stringBlob := ir.PointerReachableBlobs(root)
	for _, rec := range records {
		if st := nodes[rec.TypeId]; st != nil {
			n, ok := size(st, rec.Body)
			if !ok {
				return -1, true, true
			}
			total += n
		} else if rec.TypeId == ir.BytesWireTypeId && bytesBlob || rec.TypeId == ir.StringWireTypeId && stringBlob {
			if uint64(len(rec.Body)) > math.MaxUint32 {
				return -1, true, true
			}
			n := int64(8 + len(rec.Body))
			if rec.TypeId == ir.StringWireTypeId {
				n++
			}
			total += round(n)
		}
	}
	return total + int64(len(records)+1)*16, true, false
}

type extentScan struct {
	u   *ir.Unit
	ids []uint64
	at  int64
}

func (s *extentScan) reader(body []byte) wireReader { return wireReader{buf: body, ids: s.ids} }
func extentBody(r *wireReader) ([]byte, bool) {
	n, ok := r.leb()
	if !ok || n > uint64(len(r.buf)-r.off) {
		r.off = len(r.buf)
		return nil, false
	}
	body := r.buf[r.off : r.off+int(n)]
	r.off += int(n)
	return body, true
}
func (s *extentScan) record(st *ir.Struct, body []byte) bool {
	r := s.reader(body)
	for {
		ref, ok := r.leb()
		if !ok || ref == 0 {
			return true
		}
		id, ok := r.id(ref)
		if !ok || !r.has(1) {
			return true
		}
		kind := int(r.u8())
		var f *ir.Field
		for _, candidate := range st.Fields {
			if ir.TableFieldWireId(candidate) == id {
				f = candidate
				break
			}
		}
		if f == nil {
			if !r.skip(uint8(kind)) {
				return true
			}
			continue
		}
		if kind == ir.TableKindUnion && f.Array == ir.ArrayNone && !f.Type.Pointer {
			if un, ok := f.Type.Ref.(*ir.Union); ok {
				if !s.union(&r, un) {
					return false
				}
				continue
			}
		}
		if kind != ir.TableKindTable && kind != ir.TableKindArray && kind != ir.TableKindKeyed {
			if !r.skip(uint8(kind)) {
				return true
			}
			continue
		}
		payload, ok := extentBody(&r)
		if !ok {
			return true
		}
		if kind != ir.TableWireFieldKind(f) {
			continue
		}
		if !s.field(f, payload) {
			return false
		}
	}
}
func (s *extentScan) field(f *ir.Field, body []byte) bool {
	if f.Type.Pointer && f.Array == ir.ArrayNone {
		return true
	}
	if f.IsMap() || f.IsList() || f.Array != ir.ArrayNone || f.KeyEnum != "" {
		r := s.reader(body)
		if !r.has(2) {
			return true
		}
		kind := int(r.u8())
		expected := ir.TableWireElemKind(f)
		if f.IsMap() {
			expected = ir.TableKindTable
		}
		if kind != expected && !ir.TableKindWidens(kind, expected) {
			return true
		}
		count, ok := r.leb()
		if !ok {
			return true
		}
		if f.IsMap() || f.IsList() {
			var size, align int64
			if f.IsMap() {
				ml := ir.RecordLayout(s.u, f.MapEntry)
				size, align = ml.Size, ml.Align
			} else {
				size, align = ir.ListElementLayout(s.u, f)
			}
			if count > math.MaxInt32 {
				return false
			}
			floor := int64(ir.TableKindWidth(kind))
			if kind == ir.TableKindPointer {
				floor = 1
			}
			if kind == ir.TableKindTable {
				floor = 2
			}
			if floor < 1 {
				floor = 1
			}
			if count > uint64(int64(len(r.buf)-r.off)/floor) {
				return false
			}
			s.at = (s.at + align - 1) & -align
			s.at += int64(count) * size
		}
		if kind != ir.TableKindTable && kind != ir.TableKindUnion {
			return true
		}
		for i := uint64(0); i < count && r.has(1); i++ {
			if f.KeyEnum != "" {
				if _, ok := r.leb(); !ok {
					return true
				}
			}
			if kind == ir.TableKindUnion {
				if un, ok := f.Type.Ref.(*ir.Union); ok {
					if !s.union(&r, un) {
						return false
					}
				}
				continue
			}
			payload, ok := extentBody(&r)
			if !ok {
				return true
			}
			st, _ := f.Type.Ref.(*ir.Struct)
			if f.IsMap() {
				st = f.MapEntry
			}
			if st != nil && !s.record(st, payload) {
				return false
			}
		}
		return true
	}
	if st, ok := f.Type.Ref.(*ir.Struct); ok {
		return s.record(st, body)
	}
	return true
}
func (s *extentScan) union(r *wireReader, un *ir.Union) bool {
	ref, ok := r.leb()
	if !ok {
		r.off = len(r.buf)
		return true
	}
	if ref == 0 {
		return true
	}
	id, ok := r.id(ref)
	if !ok || !r.has(1) {
		r.off = len(r.buf)
		return true
	}
	r.u8()
	body, ok := extentBody(r)
	if !ok {
		return true
	}
	for _, v := range un.Variants {
		if ir.TableWireId(v.WireName()) != id || v.F == nil || v.F.Type.Pointer {
			continue
		}
		if inner, ok := v.F.Type.Ref.(*ir.Union); ok && v.F.Array == ir.ArrayNone {
			arm := s.reader(body)
			return s.union(&arm, inner)
		}
		return s.field(v.F, body)
	}
	return true
}
