// UNCOOK: a cook read back into the instance model, and from there onto the
// tolerant wire.
//
// It is a TOOL and never a runtime path — a runtime points at a cook and reads
// it where it lies (§7). It exists because the wire is the FORMAT OF RECORD: a
// cook is an accelerator produced beside a wire file, so the proof that no fact
// was lost in producing one is that the wire comes back out of it byte for
// byte, in both byte orders, over the whole corpus.
package tablecook

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Uncook reads a cooked file back into the root instance it was produced from.
// It needs the ATTRIBUTION part, because a region on its own says where nothing
// is: the directory is what names each node's start and type, and a file that
// carries none cannot be read back any more than it can be checked.
func Uncook(m *tabletext.Model, root *ir.Struct, file []byte) (*tabletext.Instance, error) {
	h, err := ReadHeader(file, ir.BuildVersion(m.Unit))
	if err != nil {
		return nil, err
	}
	dir, err := h.Directory(file)
	if err != nil {
		return nil, err
	}
	if err := checkDirectory(m, h, dir); err != nil {
		return nil, err
	}
	byTypeId := typeIdIndex(m)
	if dir[0].TypeId != ir.TableTypeId(root.Name) {
		return nil, fmt.Errorf("this cook's root is %s and --root names %s: the root is the directory's first entry and it is not guessed at", nameOf(byTypeId, dir[0].TypeId), root.Name)
	}

	r := &regionReader{m: m, ord: h.Order(), buf: h.Data(file), dir: dir}
	r.nodes = make([]*tabletext.Instance, len(dir))
	r.blobs = make([]*tabletext.Blob, len(dir))
	for i, e := range dir {
		if st := byTypeId[e.TypeId]; st != nil {
			r.nodes[i] = m.New(st)
			continue
		}
		// a BYTE BUFFER's node (§6.3): the length, then the bytes at eight —
		// checkDirectory bounded the header, and the length is bounded here
		// against the node's extent exactly as cook-check bounds it
		length := int64(r.ord.Uint32(r.buf[e.Offset:]))
		extent := h.DataLength - e.Offset
		if i+1 < len(dir) {
			extent = dir[i+1].Offset - e.Offset
		}
		need := BlobHeader + length
		if e.TypeId == ir.StringTypeId {
			need++
		}
		if need > extent {
			return nil, fmt.Errorf("node %d (%s at offset %d): the byte buffer's length is %d and its node's extent is %d: the bytes leave the node", i+1, nameOf(byTypeId, e.TypeId), e.Offset, length, extent)
		}
		r.blobs[i] = &tabletext.Blob{Data: append([]byte(nil), r.buf[e.Offset+BlobHeader:e.Offset+BlobHeader+length]...)}
	}
	for i, e := range dir {
		if r.nodes[i] == nil {
			continue
		}
		if err := r.record(e.Offset, byTypeId[e.TypeId], r.nodes[i]); err != nil {
			return nil, err
		}
	}
	return r.nodes[0], nil
}

type regionReader struct {
	m     *tabletext.Model
	ord   order
	buf   []byte
	dir   []DirectoryEntry
	nodes []*tabletext.Instance // by directory position; nil where the entry is a blob
	blobs []*tabletext.Blob     // by directory position; nil where the entry is a table
}

func (r *regionReader) record(base int64, st *ir.Struct, inst *tabletext.Instance) error {
	ml := ir.RecordLayout(r.m.Unit, st)
	for i := range ml.Fields {
		fl := &ml.Fields[i]
		fv := instField(inst, fl.Field)
		if fv == nil {
			continue
		}
		if err := r.field(base+fl.Offset, fl.Field, fv); err != nil {
			return err
		}
	}
	return nil
}

func (r *regionReader) field(at int64, f *ir.Field, fv *tabletext.Field) error {
	pieces := ir.FieldPieces(r.m.Unit, f, at)
	if len(pieces) == 0 {
		return nil
	}
	value := pieces[0]
	if f.Type.Optional {
		fv.Present = r.buf[pieces[len(pieces)-1].Offset] != 0
	}
	switch {
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		return r.ref(value.Offset, f, &fv.Cell)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		n := int64(int32(r.ord.Uint32(r.buf[pieces[1].Offset:])))
		bound := f.Type.Size
		if n < 0 || n > bound {
			return fmt.Errorf("field %s: the used length %d is outside its declared bound %d", f.Name, n, bound)
		}
		fv.Cell.Str = append([]byte(nil), r.buf[value.Offset:value.Offset+n]...)
		fv.Count = int(n)
		return nil
	case f.KeyEnum != "", f.Array == ir.ArrayFixed:
		return r.slots(value.Offset, f, fv.Elems, len(fv.Elems))
	case f.Array == ir.ArrayCounted:
		if err := r.slots(value.Offset, f, fv.Elems, len(fv.Elems)); err != nil {
			return err
		}
		n := int64(int32(r.ord.Uint32(r.buf[pieces[1].Offset:])))
		if n < 0 || n > f.ArrayBound {
			return fmt.Errorf("field %s: the used count %d is outside its declared bound %d", f.Name, n, f.ArrayBound)
		}
		fv.Count = int(n)
		return nil
	}
	return r.element(value.Offset, f, &fv.Cell)
}

func (r *regionReader) slots(at int64, f *ir.Field, elems []tabletext.Cell, n int) error {
	step := elementBytes(r.m.Unit, f)
	for i := range n {
		if err := r.element(at+int64(i)*step, f, &elems[i]); err != nil {
			return err
		}
	}
	return nil
}

// ref reads one reference slot — a field's, or an element of an array of
// pointers (§2.1): the self-relative delta, resolved through the directory.
func (r *regionReader) ref(at int64, f *ir.Field, cell *tabletext.Cell) error {
	delta := int64(r.ord.Uint64(r.buf[at:]))
	if delta == RefNull {
		cell.Node = nil
		cell.Blob = nil
		return nil
	}
	target := at + delta
	slot := r.entryAt(target)
	if slot < 0 {
		return fmt.Errorf("field %s: the reference resolves to offset %d, which the directory does not name", f.Name, target)
	}
	if f.Type.Blob() {
		// a byte buffer's slot names a blob node under its reserved id (§2.5)
		if r.blobs[slot] == nil || r.dir[slot].TypeId != ir.BlobTypeId(f) {
			return fmt.Errorf("field %s: the reference resolves to a node the directory names as type id 0x%016x, and the declaration requires a byte buffer", f.Name, r.dir[slot].TypeId)
		}
		cell.Blob = r.blobs[slot]
		return nil
	}
	if r.nodes[slot] == nil {
		return fmt.Errorf("field %s: the reference resolves to a byte buffer, and the declaration requires %s", f.Name, f.Type.Name)
	}
	cell.Node = r.nodes[slot]
	return nil
}

func (r *regionReader) element(at int64, f *ir.Field, cell *tabletext.Cell) error {
	t := f.Type
	if t.Pointer {
		return r.ref(at, f, cell)
	}
	switch t.Kind {
	case ir.TBool:
		cell.B = r.buf[at] != 0
		return nil
	case ir.TFloat32:
		cell.F = float64(math.Float32frombits(r.ord.Uint32(r.buf[at:])))
		return nil
	case ir.TFloat64:
		cell.F = math.Float64frombits(r.ord.Uint64(r.buf[at:]))
		return nil
	case ir.TInt, ir.TFixed:
		if kind := ir.TableScalarKind(f); ir.TableKindWide(kind) {
			n := int64(ir.TableKindWidth(kind))
			le := make([]byte, n)
			if r.ord == binary.LittleEndian {
				copy(le, r.buf[at:at+n])
			} else {
				for i := range le {
					le[i] = r.buf[at+n-1-int64(i)]
				}
			}
			cell.Wide = tabletext.WideFromBytes(le, kind)
			return nil
		}
		r.width(cell, at, int64(t.Width)/8, t.Signed)
		return nil
	case ir.TBits:
		if t.Width <= 32 {
			r.width(cell, at, 4, false)
		} else {
			r.width(cell, at, 8, false)
		}
		return nil
	case ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Enum:
			r.width(cell, at, int64(ir.StorageBitsFor(ref.Max))/8, false)
			return nil
		case *ir.Flags:
			r.width(cell, at, 8, false)
			return nil
		case *ir.Struct:
			if cell.Tab == nil {
				cell.Tab = r.m.New(ref)
			}
			return r.record(at, ref, cell.Tab)
		case *ir.Union:
			_, _, tag, armOffset := ir.UnionLayout(r.m.Unit, ref)
			var t tabletext.Cell
			r.width(&t, at, tag, false)
			cell.U = t.U
			if cell.U == 0 {
				cell.Tab = nil
				return nil
			}
			if int(cell.U) > len(ref.Variants) {
				return fmt.Errorf("union %s: the stored tag %d names no arm", ref.Name, cell.U)
			}
			arm := ref.Variants[cell.U-1]
			cell.Tab = r.m.New(arm.Ref)
			return r.record(at+armOffset, arm.Ref, cell.Tab)
		}
	}
	return fmt.Errorf("field %s: a type with no cooked storage reached a region", f.Name)
}

func (r *regionReader) width(cell *tabletext.Cell, at, width int64, signed bool) {
	var raw uint64
	switch width {
	case 1:
		raw = uint64(r.buf[at])
	case 2:
		raw = uint64(r.ord.Uint16(r.buf[at:]))
	case 4:
		raw = uint64(r.ord.Uint32(r.buf[at:]))
	case 8:
		raw = r.ord.Uint64(r.buf[at:])
	}
	cell.U = raw
	cell.I = int64(raw)
	if signed && width < 8 {
		shift := uint(64 - width*8)
		cell.I = int64(raw<<shift) >> shift
		cell.U = uint64(cell.I)
	}
}

// entryAt is the directory slot an offset names, or -1. The offsets ASCEND
// (§6.3), so it is one binary search — which is where the `P log N` in §7's
// `O(R + P log N)` comes from.
func (r *regionReader) entryAt(offset int64) int {
	lo, hi := 0, len(r.dir)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		switch {
		case r.dir[mid].Offset == offset:
			return mid
		case r.dir[mid].Offset < offset:
			lo = mid + 1
		default:
			hi = mid - 1
		}
	}
	return -1
}

// typeIdIndex maps a wire type id onto the declaration it names, over the whole
// unit closure. A table name is scoped to a WHOLE unit closure, which is why the
// id is 64 bits and why this is the only lookup a scan needs.
func typeIdIndex(m *tabletext.Model) map[uint64]*ir.Struct {
	out := map[uint64]*ir.Struct{}
	for name := range ir.TableClosure(m.Unit) {
		if st := m.Lookup(name); st != nil {
			out[ir.TableTypeId(name)] = st
		}
	}
	return out
}

func nameOf(byTypeId map[uint64]*ir.Struct, id uint64) string {
	if st := byTypeId[id]; st != nil {
		return st.Name
	}
	switch id {
	case ir.BytesTypeId:
		return "*bytes" // a BYTE BUFFER's node under its reserved id (§2.5)
	case ir.StringTypeId:
		return "*string"
	}
	return fmt.Sprintf("an unknown type (id 0x%016x)", id)
}
