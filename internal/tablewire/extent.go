package tablewire

import (
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
	"math"
)

// ReasonStorageOverCap names the refusal of a wire whose region measure passes
// the storage cap, ir.MaxSizeBytes (docs/SPEC-TABLES.md §6.5, §11).
const ReasonStorageOverCap = "storage_over_cap"

// StorageRefusal is the read's refusal of a wire whose region measure passes
// the storage cap (docs/SPEC-TABLES.md §11). The read refuses it before it
// builds any value, so the model's memory is bounded by the wire rather than
// by the storage the wire commands. The report carries Refused and the name.
type StorageRefusal struct{}

func (*StorageRefusal) Error() string {
	return ReasonStorageOverCap + ": the wire's region measure passes the 2^40-byte storage cap (docs/SPEC-TABLES.md §11)"
}

// FileRegionMeasure derives canonical region storage from framing, before
// decoding values. In particular, dropped map entries still consume the
// array extent declared on the wire. Generated runtimes never call this
// independent compiler-side oracle. A wire past the storage cap answers -1,
// whole and refused; FileRegionMeasureReason names it.
func FileRegionMeasure(u *ir.Unit, root *ir.Struct, data []byte) (bytes int64, whole, refused bool) {
	bytes, whole, refused, _ = FileRegionMeasureReason(u, root, data)
	return bytes, whole, refused
}

// FileRegionMeasureReason is FileRegionMeasure with the refusal's name:
// ReasonStorageOverCap for a wire whose measure passes the storage cap, and
// empty for every other answer.
func FileRegionMeasureReason(u *ir.Unit, root *ir.Struct, data []byte) (bytes int64, whole, refused bool, reason string) {
	if len(data) == 0 || data[0] != ir.TableWireForm {
		return 0, false, false, ""
	}
	var valid bool
	data, valid = fileIntegrityBody(root, data)
	if !valid {
		return 0, false, false, ""
	}
	body, ids, ok := trailer(data)
	if len(data) == 0 || data[0] != ir.TableWireForm || !ok {
		return 0, false, false, ""
	}
	return regionMeasure(u, root, body, ids)
}

// regionMeasure is the measure over a body and its id table. Every term is
// held under the storage cap before it is added: a list or map extent
// (count times element size) in extentScan.field, a record after its scan,
// and the running total after each node record and at the end, so no sum
// passes the cap and none overflows.
func regionMeasure(u *ir.Unit, root *ir.Struct, body []byte, ids []uint64) (bytes int64, whole, refused bool, reason string) {
	over := func() (int64, bool, bool, string) { return -1, true, true, ReasonStorageOverCap }
	align := ir.TableRegionAlign(u)
	round := func(n int64) int64 { return (n + align - 1) & -align }
	size := func(st *ir.Struct, body []byte) (n int64, ok, capped bool) {
		walk := extentScan{u: u, ids: ids}
		if !walk.record(st, body) {
			return 0, false, walk.over
		}
		return round(round(ir.RecordLayout(u, st).Size) + walk.at), true, false
	}
	total, ok, capped := size(root, body)
	if capped || ok && total > ir.MaxSizeBytes {
		return over()
	}
	if !ok {
		return -1, true, true, ""
	}
	payload, present, framed := nodeTableBytes(body, ids, &tabletext.Report{})
	if present && !framed {
		return 0, false, false, ""
	}
	var records []nodeRecord
	if present {
		records, ok = scanNodeRecords(payload, ids)
		if !ok {
			return 0, false, false, ""
		}
	}
	nodes := map[uint64]*ir.Struct{}
	for _, st := range ir.PointerReachable(root) {
		nodes[ir.TableWireId(st.WireName())] = st
	}
	bytesBlob, stringBlob := ir.PointerReachableBlobs(root)
	for _, rec := range records {
		if st := nodes[rec.TypeId]; st != nil {
			n, ok, capped := size(st, rec.Body)
			if capped {
				return over()
			}
			if !ok {
				return -1, true, true, ""
			}
			total += n
		} else if rec.TypeId == ir.BytesWireTypeId && bytesBlob || rec.TypeId == ir.StringWireTypeId && stringBlob {
			if uint64(len(rec.Body)) > math.MaxUint32 {
				return -1, true, true, ""
			}
			n := int64(8 + len(rec.Body))
			if rec.TypeId == ir.StringWireTypeId {
				n++
			}
			total += round(n)
		}
		// each term is at most the cap, so the sum is checked after every
		// record and never overflows
		if total > ir.MaxSizeBytes {
			return over()
		}
	}
	total += int64(len(records)+1) * 16
	if total > ir.MaxSizeBytes {
		return over()
	}
	return total, true, false, ""
}

// storageOverCap answers whether a variable root's region measure passes the
// storage cap. The read asks it before it builds anything.
func storageOverCap(u *ir.Unit, root *ir.Struct, body []byte, ids []uint64) bool {
	_, _, _, reason := regionMeasure(u, root, body, ids)
	return reason == ReasonStorageOverCap
}

type extentScan struct {
	u   *ir.Unit
	ids []uint64
	at  int64
	// over says the scan stopped at a term past the storage cap
	over bool
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
		if kind == ir.TableKindString && f.Type.TextUnbounded() && f.Array == ir.ArrayNone && !f.Type.Pointer {
			payload, ok := extentBody(&r)
			if !ok {
				return true
			}
			if !s.unboundedText(int64(len(payload))) {
				return false
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
			// the term is held under the storage cap before it is added
			if count > 0 && size > 0 && int64(count) > (ir.MaxSizeBytes-s.at)/size {
				s.over = true
				return false
			}
			s.at += int64(count) * size
		}
		if kind == ir.TableKindString && ir.TextListUnbounded(f) {
			for i := uint64(0); i < count && r.has(1); i++ {
				payload, ok := extentBody(&r)
				if !ok {
					return true
				}
				if !s.unboundedText(int64(len(payload))) {
					return false
				}
			}
			return true
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
	if f.Type.TextUnbounded() {
		return s.unboundedText(int64(len(body)))
	}
	return true
}
func (s *extentScan) unboundedText(n int64) bool {
	if n == 0 {
		return true
	}
	if uint64(n) > math.MaxUint32 {
		return false
	}
	align := ir.TableRegionAlign(s.u)
	s.at = (s.at + 7) & -8
	s.at += (9 + n + align - 1) & -align
	if s.at > ir.MaxSizeBytes {
		s.over = true
		return false
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
