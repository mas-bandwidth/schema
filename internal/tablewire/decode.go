// The wire's READ half (docs/SPEC-TABLES.md §4): any reader reads any data, and
// the differences are reported, never fatal. Unknown ids are skipped by their
// length and counted, a kind mismatch is skipped rather than misdecoded,
// out-of-range values clamp, framing damage stops the damaged nesting level
// and the parent reads on. Every decision mirrors the generated C++ reader.
package tablewire

import (
	"encoding/binary"
	"math"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Decode fills one instance from a root table's wire bytes, counting every
// evolution event in the report. The error is a REFUSAL — a root this engine
// does not decode at all — and is returned rather than folded into the report,
// because a caller must not carry on as if it had a value; false with a nil
// error is framing damage past the point the walk could continue, and the
// instance keeps what it decoded.
func Decode(m *tabletext.Model, inst *tabletext.Instance, data []byte, report *tabletext.Report) (bool, error) {
	if !ir.VariableTables(m.Unit)[inst.Def.Name] {
		// a value-only table has no pointer, therefore no node table, therefore
		// exactly the bytes §3 already describes
		r := &wireReader{buf: data, report: report, m: m}
		return r.body(inst), nil
	}
	return decodeVariable(m, inst, data, report)
}

type wireReader struct {
	buf    []byte
	off    int
	report *tabletext.Report
	m      *tabletext.Model
	st     *decodeState // the save's numbering, shared by every sub-reader
}

func (r *wireReader) has(n int) bool { return n >= 0 && r.off+n <= len(r.buf) }

func (r *wireReader) u8() uint8 {
	v := r.buf[r.off]
	r.off++
	return v
}

func (r *wireReader) u16() uint16 {
	v := binary.LittleEndian.Uint16(r.buf[r.off:])
	r.off += 2
	return v
}

func (r *wireReader) u32() uint32 {
	v := binary.LittleEndian.Uint32(r.buf[r.off:])
	r.off += 4
	return v
}

func (r *wireReader) u64() uint64 {
	v := binary.LittleEndian.Uint64(r.buf[r.off:])
	r.off += 8
	return v
}

func (r *wireReader) sub(n int) *wireReader {
	s := &wireReader{buf: r.buf[r.off : r.off+n], report: r.report, m: r.m, st: r.st}
	return s
}

// skip steps past a field whose id this reader cannot name — the kind byte and
// nothing else is needed, which is what makes an unknown field survivable (§3).
func (r *wireReader) skip(kind uint8) bool {
	switch kind {
	case ir.TableKindBool, ir.TableKindI8, ir.TableKindU8,
		ir.TableKindI16, ir.TableKindU16,
		ir.TableKindI32, ir.TableKindU32, ir.TableKindF32,
		ir.TableKindI64, ir.TableKindU64, ir.TableKindF64,
		ir.TableKindPointer,
		ir.TableKindI128, ir.TableKindU128,
		ir.TableKindFixed8, ir.TableKindFixed16, ir.TableKindFixed32, ir.TableKindFixed64, ir.TableKindFixed128,
		ir.TableKindUFixed8, ir.TableKindUFixed16, ir.TableKindUFixed32, ir.TableKindUFixed64, ir.TableKindUFixed128:
		w := tabletext.KindWidth(int(kind))
		if !r.has(w) {
			return false
		}
		r.off += w
		return true
	case ir.TableKindString, ir.TableKindTable, ir.TableKindArray, ir.TableKindKeyed:
		if !r.has(4) {
			return false
		}
		n := int(r.u32())
		if !r.has(n) {
			return false
		}
		r.off += n
		return true
	case ir.TableKindUnion:
		if !r.has(2) {
			return false
		}
		if r.u16() == 0 {
			return true // an arm id of 0 is the empty union, and carries nothing
		}
		if !r.has(4) {
			return false
		}
		n := int(r.u32())
		if !r.has(n) {
			return false
		}
		r.off += n
		return true
	}
	return false // a kind outside the closed set is framing damage
}

// wireKind is the kind byte a field's payload rides under. It is
// ir.TableFieldKind with the one case that function does not see: a POINTER
// field rides as a NODE INDEX under its own kind `17`, because a body that may
// be named twice cannot also sit inline at one of its names (docs/SPEC-TABLES.md
// §3.1). `*T` and a by-value `T` are therefore no longer one framing: `T` and
// `?T` share kind `13`, and an edit between a pointer and either of the others
// — or between a pointer and a plain `uint32` — is §4's kind mismatch, counted
// and never misdecoded.
func wireKind(f *ir.Field) int {
	if f.Type.Pointer && f.Array == ir.ArrayNone {
		return ir.TableKindPointer // an ARRAY of pointers is kind 14 with element kind 17 (§2.1)
	}
	if f.Type.Kind == ir.TBytes {
		return ir.TableKindArray
	}
	return ir.TableFieldKind(f)
}

func (r *wireReader) body(inst *tabletext.Instance) bool {
	index := map[uint16]int{}
	for i := range inst.Fields {
		index[ir.TableFieldId(inst.Fields[i].Def)] = i
	}
	for {
		if !r.has(2) {
			r.report.Malformed = true
			return false
		}
		id := r.u16()
		if id == 0 {
			return true
		}
		if !r.has(1) {
			r.report.Malformed = true
			return false
		}
		kind := r.u8()
		if id == ir.NodeTableFieldId && r.st != nil && r.m.IsVariable(inst.Def.Name) {
			// the reserved id is this reader's OWN (§3.1), read before any body
			// and not a field of the table: a VARIABLE-class body steps over it
			// and never counts it unknown, as the C++ reference does. A reader
			// that cannot name it — a fixed-class body, or a build without kind
			// 17 — counts one per transport field, which is the case §4's
			// counter describes.
			if !r.skip(kind) {
				r.report.Malformed = true
				return false
			}
			continue
		}
		i, known := index[id]
		if !known {
			r.report.Unknown++
			if !r.skip(kind) {
				r.report.Malformed = true
				return false
			}
			continue
		}
		fv := &inst.Fields[i]
		if int(kind) != wireKind(fv.Def) {
			r.report.KindMismatch++
			if !r.skip(kind) {
				r.report.Malformed = true
				return false
			}
			continue
		}
		if !r.field(fv) {
			return false
		}
		if fv.Def.Type.Optional && fv.Def.Array == ir.ArrayNone {
			// the field rode, so it is PRESENT — content decides nothing here
			// either (docs/SPEC-TABLES.md §2.3). An optional ARRAY's presence is
			// settled inside r.array instead: a foreign ELEMENT kind leaves it
			// absent, exactly as the generated reader does (§2.3).
			fv.Present = true
		}
	}
}

func (r *wireReader) field(fv *tabletext.Field) bool {
	f := fv.Def
	switch {
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		// A pointer field's payload is a NUMBER: it is bounds-checked and
		// stored, never followed. There is no traversal on the load path, and
		// therefore no traversal bound — no depth cap, no visited set, no
		// ordering rule on the indices (docs/SPEC-TABLES.md §3.1).
		if !r.has(4) {
			r.report.Malformed = true
			return false
		}
		r.resolve(fv, r.u32())
		return true
	case f.KeyEnum != "":
		return r.keyed(fv)
	case f.Type.Kind == ir.TString:
		if !r.has(4) {
			r.report.Malformed = true
			return false
		}
		n := int(r.u32())
		if !r.has(n) {
			r.report.Malformed = true
			return false
		}
		keep := n
		if bound := int(f.Type.Size); keep > bound {
			keep = bound
			r.report.Clamped++
		}
		fv.Cell.Str = append([]byte(nil), r.buf[r.off:r.off+keep]...)
		fv.Count = keep
		r.off += n
		return true
	case f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		return r.array(fv)
	case tabletext.UnionOf(f) != nil:
		return r.union(fv)
	case tabletext.StructOf(f) != nil:
		if !r.has(4) {
			r.report.Malformed = true
			return false
		}
		n := int(r.u32())
		if !r.has(n) {
			r.report.Malformed = true
			return false
		}
		sub := r.sub(n)
		fv.Cell.Tab = r.m.New(tabletext.StructOf(f))
		sub.body(fv.Cell.Tab)
		r.off += n
		return true
	}
	return r.scalar(&fv.Cell, f, true)
}

func (r *wireReader) array(fv *tabletext.Field) bool {
	f := fv.Def
	bound := int(f.ArrayBound)
	if f.Type.Kind == ir.TBytes {
		bound = int(f.Type.Size)
	}
	counted := f.Type.Kind == ir.TBytes || f.Array == ir.ArrayCounted
	if !r.has(4) {
		r.report.Malformed = true
		return false
	}
	bodyLen := int(r.u32())
	if !r.has(bodyLen) {
		r.report.Malformed = true
		return false
	}
	end := r.off + bodyLen
	decoded := 0
	if bodyLen >= 5 {
		ek := r.u8()
		count := int(r.u32())
		if int(ek) != ir.TableElemKind(f) {
			// the element kind is part of the array's identity (§3): counted,
			// the field left at its declared default — for an OPTIONAL array
			// that default is ABSENT, so Present is not set below
			r.report.KindMismatch++
			r.off = end
			return true
		}
		keep := count
		if keep > bound {
			keep = bound
			r.report.Clamped++
		}
		// elements are BOUNDED by the field body: a count the length cannot
		// cover keeps the decoded prefix, flags malformed, and the parent
		// continues at the next field — a neighbour's bytes are never
		// fabricated into elements (§4)
		sub := r.sub(end - r.off)
		if f.Type.Kind == ir.TBytes {
			payload := make([]byte, 0, keep)
			for i := 0; i < keep; i++ {
				if !sub.has(1) {
					r.report.Malformed = true
					break
				}
				payload = append(payload, sub.u8())
				decoded = i + 1
			}
			fv.Cell.Str = payload
		} else {
			for i := 0; i < keep; i++ {
				if !sub.element(fv, i) {
					break
				}
				decoded = i + 1
			}
		}
	}
	if counted {
		fv.Count = decoded
	}
	if f.Type.Optional {
		// the field rode with its own element kind (or a body too short to
		// carry one), so it is PRESENT (§2.3); the element-kind mismatch above
		// returned before this line
		fv.Present = true
	}
	r.off = end // excess elements and slack skip via the length
	return true
}

func (r *wireReader) element(fv *tabletext.Field, i int) bool {
	f := fv.Def
	if tabletext.UnionOf(f) != nil {
		// an element of an ARRAY OF UNIONS: the union payload in its place (§3)
		return r.unionCell(&fv.Elems[i], f)
	}
	if f.Type.Pointer {
		// an element of an array of pointers: a node index, bounds-checked and
		// stored, never followed (§3.1)
		if !r.has(4) {
			r.report.Malformed = true
			return false
		}
		r.resolveCell(&fv.Elems[i], f, r.u32())
		return true
	}
	if tabletext.StructOf(f) != nil && tabletext.EnumOf(f) == nil {
		if !r.has(4) {
			r.report.Malformed = true
			return false
		}
		n := int(r.u32())
		if !r.has(n) {
			r.report.Malformed = true
			return false
		}
		sub := r.sub(n)
		fv.Elems[i].Tab = r.m.New(tabletext.StructOf(f))
		sub.body(fv.Elems[i].Tab)
		r.off += n
		return true
	}
	return r.scalar(&fv.Elems[i], f, false)
}

// keyed places an enum-keyed array's pairs BY VARIANT ID, so a slot lands by
// name however the enum moved (docs/SPEC-TABLES.md §3.2). A key this reader cannot
// name is skipped by its length and counted unknown; a slot the writer never
// sent keeps its declared default; a key of 0 is None, which keys no slot,
// and is framing damage.
func (r *wireReader) keyed(fv *tabletext.Field) bool {
	f := fv.Def
	if !r.has(4) {
		r.report.Malformed = true
		return false
	}
	bodyLen := int(r.u32())
	if !r.has(bodyLen) {
		r.report.Malformed = true
		return false
	}
	end := r.off + bodyLen
	if bodyLen < 5 {
		r.off = end
		return true
	}
	elemKind := r.u8()
	count := int(r.u32())
	if elemKind != uint8(ir.TableElemKind(f)) {
		r.report.KindMismatch++
		r.off = end
		return true
	}
	sub := r.sub(end - r.off)
	for range count {
		if !sub.has(2) {
			r.report.Malformed = true
			break
		}
		key := sub.u16()
		if !sub.has(4) {
			r.report.Malformed = true
			break
		}
		elemLen := int(sub.u32())
		if !sub.has(elemLen) {
			r.report.Malformed = true
			break
		}
		if key == 0 {
			// None is the null key and 0 the id no name folds to, so a body
			// carrying one is damaged: the read stops this body and keeps what
			// it decoded (docs/SPEC-TABLES.md §3.2)
			r.report.Malformed = true
			break
		}
		slot := tabletext.KeyedValueSlot(f, enumValueForId(f.KeyEnumRef, key))
		if slot < 0 {
			r.report.Unknown++ // a slot this reader cannot name
			sub.off += elemLen
			continue
		}
		elem := sub.sub(elemLen)
		if tabletext.StructOf(f) != nil {
			fv.Elems[slot].Tab = r.m.New(tabletext.StructOf(f))
			elem.body(fv.Elems[slot].Tab)
		} else {
			elem.scalar(&fv.Elems[slot], f, false)
		}
		sub.off += elemLen
	}
	r.off = end // unread pairs and slack skip via the length
	return true
}

func (r *wireReader) union(fv *tabletext.Field) bool { return r.unionCell(&fv.Cell, fv.Def) }

// unionCell decodes the union framing into one cell — a field's, or an element
// of an array of unions (§3).
func (r *wireReader) unionCell(cell *tabletext.Cell, f *ir.Field) bool {
	un := tabletext.UnionOf(f)
	if !r.has(2) {
		r.report.Malformed = true
		return false
	}
	armID := r.u16()
	if armID == 0 {
		cell.U = 0
		cell.Tab = nil
		return true // empty: the id is the whole payload
	}
	if !r.has(4) {
		r.report.Malformed = true
		return false
	}
	n := int(r.u32())
	if !r.has(n) {
		r.report.Malformed = true
		return false
	}
	sub := r.sub(n)
	tag := 0
	for i, v := range un.Variants {
		if ir.VariantId(v.Name) == armID {
			tag = i + 1
			break
		}
	}
	if tag == 0 {
		// an arm this reader cannot name: the value reads EMPTY and the body
		// is skipped by its length, never misdecoded. The reset is explicit: a
		// repeated field id must not leave an arm an earlier occurrence
		// decoded standing (§4).
		cell.U = 0
		cell.Tab = nil
		r.report.Unknown++
	} else {
		payload := r.m.New(un.Variants[tag-1].Ref)
		sub.body(payload)
		cell.U = uint64(tag)
		cell.Tab = payload
	}
	r.off += n
	return true
}

// scalar decodes one fixed-width value. atField says whether truncation is
// outer framing damage (a scalar FIELD stops the decode) or an element's (the
// prefix is kept and the loop breaks).
func (r *wireReader) scalar(cell *tabletext.Cell, f *ir.Field, atField bool) bool {
	kind := ir.TableScalarKind(f)
	width := tabletext.KindWidth(kind)
	if !r.has(width) {
		r.report.Malformed = true
		return false
	}
	if e := tabletext.EnumOf(f); e != nil {
		// identity is the variant's NAME (§5): an id this build cannot name
		// reads as None and counts as unknown, exactly as an unknown FIELD id
		// does — same event, one counter
		id := r.u16()
		v := enumValueForId(e, id)
		if v < 0 {
			cell.U = 0
			r.report.Unknown++
		} else {
			cell.U = uint64(v)
		}
		return true
	}
	switch kind {
	case ir.TableKindBool:
		cell.B = r.u8() != 0
		return true
	case ir.TableKindF32:
		v := float64(math.Float32frombits(r.u32()))
		if f.HasFloatRange {
			if v < f.FMin {
				v = f.FMin
				r.report.Clamped++
			} else if v > f.FMax {
				v = f.FMax
				r.report.Clamped++
			}
		}
		cell.F = float64(float32(v))
		return true
	case ir.TableKindF64:
		cell.F = math.Float64frombits(r.u64())
		return true
	}
	if ir.TableKindWide(kind) {
		raw := tabletext.WideFromBytes(r.buf[r.off:r.off+width], kind)
		r.off += width
		// the declared range on the RAW scale — a fixed field's whole-unit
		// bounds shifted by F — clamps and counts as every bounded scalar does (§4)
		clamped := false
		cell.Wide, clamped = tabletext.WideClamp(raw, f)
		if clamped {
			r.report.Clamped++
		}
		return true
	}
	signed := f.Type.Kind == ir.TInt && f.Type.Signed
	var raw uint64
	switch width {
	case 1:
		raw = uint64(r.u8())
	case 2:
		raw = uint64(r.u16())
	case 4:
		raw = uint64(r.u32())
	default:
		raw = r.u64()
	}
	value := int64(raw)
	if signed && width < 8 {
		shift := uint(64 - width*8)
		value = int64(raw<<shift) >> shift
	}
	if f.HasIntRange {
		lo, hi := bigInt64(f.IntMin), bigInt64(f.IntMax)
		if signed {
			if value < lo {
				value = lo
				r.report.Clamped++
			} else if value > hi {
				value = hi
				r.report.Clamped++
			}
		} else {
			if raw < uint64(lo) {
				raw = uint64(lo)
				r.report.Clamped++
			} else if raw > uint64(hi) {
				raw = uint64(hi)
				r.report.Clamped++
			}
			value = int64(raw)
		}
	}
	if f.Type.Kind == ir.TBits && f.Type.Width < width*8 {
		max := uint64(1)<<uint(f.Type.Width) - 1
		if uint64(value) > max {
			value = int64(max)
			r.report.Clamped++
		}
	}
	cell.I = value
	cell.U = uint64(value)
	if !signed && width < 8 {
		cell.U = uint64(value) & (uint64(1)<<uint(width*8) - 1)
		cell.I = int64(cell.U)
	}
	return true
}

// enumValueForId is the declaration-side value an id names, -1 when no variant
// does.
func enumValueForId(e *ir.Enum, id uint16) int64 {
	if id == 0 {
		return 0
	}
	for i, v := range e.Variants {
		if ir.VariantId(v) == id {
			return int64(i + 1)
		}
	}
	return -1
}

func bigInt64(v *big.Int) int64 {
	if v == nil {
		return 0
	}
	return v.Int64()
}
