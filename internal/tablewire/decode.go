// The wire's READ half (docs/SPEC-TABLES.md §4): any reader reads any data, and
// the differences are reported, never fatal. Unknown ids are skipped by their
// length and counted, a kind mismatch is skipped rather than misdecoded,
// out-of-range values clamp, framing damage stops the damaged nesting level
// and the parent reads on. Every decision mirrors the generated C++ reader.
package tablewire

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// FormRefusal is the wire a reader will not decode at all: a FORM BYTE it does
// not know (docs/SPEC-TABLES.md §3). The refusal is not one of §4's events and
// moves none of the report's five counters, because nothing was decoded and
// there is nothing to count — so it is returned rather than folded into the
// report, and a caller must not carry on as if it had a value.
type FormRefusal struct{ Form uint8 }

func (e *FormRefusal) Error() string {
	return fmt.Sprintf("wire form %d is newer than form %d, the one this reader carries — refused, and no damage is reported (docs/SPEC-TABLES.md §3)", e.Form, ir.TableWireForm)
}

// Decode fills one instance from a root table's wire bytes, counting every
// evolution event in the report. The error is a REFUSAL — a wire this engine
// does not decode at all — and is returned rather than folded into the report;
// false with a nil error is framing damage past the point the walk could
// continue, and the instance keeps what it decoded.
func Decode(m *tabletext.Model, inst *tabletext.Instance, data []byte, report *tabletext.Report) (bool, error) {
	// THE FORM BYTE IS READ FIRST, before the trailer and before any body, so
	// a file that is both a newer form and damaged is a refusal and never
	// damage (§3).
	if len(data) < 1 {
		report.Malformed = true
		return false, nil
	}
	if data[0] != ir.TableWireForm {
		report.Refused = true
		return false, &FormRefusal{Form: data[0]}
	}
	body, ids, ok := trailer(data)
	if !ok {
		// the one malformed case that stops the FILE rather than a nesting
		// level: every id in every body resolves through the table, and a body
		// read without it would be read without identity (§3)
		report.Malformed = true
		return false, nil
	}
	// ANY BYTE BETWEEN THE ROOT'S TERMINATOR AND THE TABLE'S FIRST ENTRY IS
	// MALFORMED, because no field claims it and the two ends of the file have
	// met (§3). Nothing is decoded, and one event is counted.
	if end, terminated := bodyExtent(body, ids); terminated && end != len(body) {
		report.Malformed = true
		return false, nil
	}
	if !ir.VariableTables(m.Unit)[inst.Def.Name] {
		// a value-only table has no pointer, therefore no node table, therefore
		// exactly the bytes §3 already describes
		r := &wireReader{buf: body, report: report, m: m, ids: ids}
		return r.body(inst), nil
	}
	return decodeVariable(m, inst, body, ids, report)
}

// trailer locates the ID TABLE from the END of the wire (docs/SPEC-TABLES.md
// §3): the final eight bytes are the ENTRY COUNT, a fixed little-endian u64,
// the `8 × count` bytes before them are the ENTRIES, and the body ends where
// the first entry begins.
//
// A table that cannot be read whole is malformed: fewer than eight bytes in
// the file, a count whose `8 × count + 8` runs past the front, a count that
// leaves no room for the form byte, or ONE ID IN TWO ENTRIES.
func trailer(data []byte) (body []byte, ids []uint64, ok bool) {
	if len(data) < 9 {
		return nil, nil, false
	}
	count := binary.LittleEndian.Uint64(data[len(data)-8:])
	if count > uint64(len(data))/8 {
		return nil, nil, false
	}
	span := int(count)*8 + 8
	if span+1 > len(data) {
		return nil, nil, false
	}
	first := len(data) - span
	ids = make([]uint64, count)
	seen := make(map[uint64]bool, count)
	for i := range ids {
		id := binary.LittleEndian.Uint64(data[first+i*8:])
		if seen[id] {
			// A TABLE THAT CARRIES ONE ID TWICE: the whole wire is malformed.
			// A reader that resolved both entries would buy nothing, because no
			// wire this schema writes carries a repeat, and it would leave one
			// more shape of table for a hostile writer to aim at (§3).
			return nil, nil, false
		}
		seen[id] = true
		ids[i] = id
	}
	return data[1:first], ids, true
}

// bodyExtent walks a body's framing to the zero reference that ends it, so the
// caller can tell a body that ENDED EARLY — leaving bytes no field claims —
// from one that is merely damaged. `terminated` is false where the walk cannot
// reach a terminator at all, which is §4's ordinary framing damage on that body
// and not the file's.
func bodyExtent(body []byte, ids []uint64) (end int, terminated bool) {
	r := &wireReader{buf: body, report: &tabletext.Report{}, ids: ids}
	for {
		ref, next, good := readLeb(r.buf, r.off)
		if !good {
			return 0, false
		}
		r.off = next
		if ref == 0 {
			return r.off, true
		}
		if ref > uint64(len(ids)) {
			return 0, false
		}
		if !r.has(1) {
			return 0, false
		}
		if !r.skip(r.u8()) {
			return 0, false
		}
	}
}

type wireReader struct {
	buf    []byte
	off    int
	report *tabletext.Report
	m      *tabletext.Model
	ids    []uint64     // the file's id table, resolved once at open (§3)
	st     *decodeState // the save's numbering, shared by every sub-reader
}

func (r *wireReader) has(n int) bool { return n >= 0 && r.off+n <= len(r.buf) }

func (r *wireReader) u8() uint8 {
	v := r.buf[r.off]
	r.off++
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

// leb reads one length, count or index. ok is false where the encoding is
// truncated or NON-CANONICAL, which is framing damage on the body carrying it
// (§3).
func (r *wireReader) leb() (v uint64, ok bool) {
	v, next, good := readLeb(r.buf, r.off)
	if !good {
		return 0, false
	}
	r.off = next
	return v, true
}

// id resolves one reference against the file's table. ok is false where the
// reference is ABOVE the entry count, which is framing damage on the body that
// carries it. Reference `0` never reaches here: it names NO ID, and the three
// places it is a value read it before asking.
func (r *wireReader) id(ref uint64) (uint64, bool) {
	if ref == 0 || ref > uint64(len(r.ids)) {
		return 0, false
	}
	return r.ids[ref-1], true
}

func (r *wireReader) sub(n int) *wireReader {
	return &wireReader{buf: r.buf[r.off : r.off+n], report: r.report, m: r.m, ids: r.ids, st: r.st}
}

// skip steps past a field whose id this reader cannot name — the kind byte and
// nothing else is needed, which is what makes an unknown field survivable (§3).
// Four rules cover the set.
func (r *wireReader) skip(kind uint8) bool {
	switch kind {
	case ir.TableKindBool, ir.TableKindI8, ir.TableKindU8,
		ir.TableKindI16, ir.TableKindU16,
		ir.TableKindI32, ir.TableKindU32, ir.TableKindF32,
		ir.TableKindI64, ir.TableKindU64, ir.TableKindF64,
		ir.TableKindI128, ir.TableKindU128,
		ir.TableKindFixed8, ir.TableKindFixed16, ir.TableKindFixed32, ir.TableKindFixed64, ir.TableKindFixed128,
		ir.TableKindUFixed8, ir.TableKindUFixed16, ir.TableKindUFixed32, ir.TableKindUFixed64, ir.TableKindUFixed128:
		w := ir.TableKindWidth(int(kind))
		if !r.has(w) {
			return false
		}
		r.off += w
		return true
	case ir.TableKindPointer, ir.TableKindEnum:
		// kinds 17 and 30 read one LEB128 value and stop
		_, ok := r.leb()
		return ok
	case ir.TableKindString, ir.TableKindTable, ir.TableKindArray, ir.TableKindKeyed,
		ir.TableKindEscape, ir.TableKindNoPayload:
		n, ok := r.leb()
		if !ok || !r.has(int(n)) || n > uint64(len(r.buf)) {
			return false
		}
		r.off += int(n)
		return true
	case ir.TableKindUnion:
		// kind 15 reads the arm id reference and stops there if it is 0, else
		// reads the kind byte, then L, and skips L bytes
		arm, ok := r.leb()
		if !ok {
			return false
		}
		if arm == 0 {
			return true
		}
		if !r.has(1) {
			return false
		}
		r.off++ // the arm's kind byte
		n, ok := r.leb()
		if !ok || !r.has(int(n)) || n > uint64(len(r.buf)) {
			return false
		}
		r.off += int(n)
		return true
	}
	// A kind a reader does not know at all is not skippable, and is framing
	// damage — which is why the set is closed and why kind 31 exists (§3).
	return false
}

// wireKind is the kind byte a field's payload rides under. It is
// ir.TableWireFieldKind with the one case that function does not see: a POINTER
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
	return ir.TableWireFieldKind(f)
}

func (r *wireReader) body(inst *tabletext.Instance) bool {
	return r.bodyAt(inst, false)
}

// bodyAt decodes one table body. `nested` says the body is not the root's, and
// it decides one rule: THE RESERVED ID INSIDE A NESTED BODY IS MALFORMED, on
// the numbering's own rule — a second numbering cannot exist, so a body
// claiming one is damaged (§3.1).
func (r *wireReader) bodyAt(inst *tabletext.Instance, nested bool) bool {
	index := map[uint64]int{}
	for i := range inst.Fields {
		index[ir.TableFieldWireId(inst.Fields[i].Def)] = i
	}
	for {
		ref, ok := r.leb()
		if !ok {
			r.report.Malformed = true
			return false
		}
		if ref == 0 {
			return true // the body ENDS AT ITS OWN ZERO REFERENCE
		}
		id, named := r.id(ref)
		if !named {
			r.report.Malformed = true
			return false
		}
		if !r.has(1) {
			r.report.Malformed = true
			return false
		}
		kind := r.u8()
		if id == ir.TableNodeWireId {
			if nested {
				r.report.Malformed = true
				return false
			}
			if r.st != nil && r.m.IsVariable(inst.Def.Name) {
				// the reserved id is this reader's OWN (§3.1), read before any
				// body and not a field of the table: a VARIABLE-class root
				// steps over it and never counts it unknown. A reader that
				// cannot name it — a fixed-class body, or a build without kind
				// 17 — counts one, which is the case §4's counter describes.
				if !r.skip(kind) {
					r.report.Malformed = true
					return false
				}
				continue
			}
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
			// AT A POSITION THE READER DOES NAME, a field that arrives under
			// kind 31 or kind 32 where the declaration says otherwise takes
			// this same rule and no other (§3)
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
		index, ok := r.leb()
		if !ok {
			r.report.Malformed = true
			return false
		}
		r.resolve(fv, index)
		return true
	case f.KeyEnum != "":
		return r.keyed(fv)
	case f.Type.Kind == ir.TString:
		n, ok := r.leb()
		if !ok || !r.has(int(n)) || n > uint64(len(r.buf)) {
			r.report.Malformed = true
			return false
		}
		keep := int(n)
		if bound := int(f.Type.Size); keep > bound {
			keep = bound
			r.report.Clamped++
		}
		fv.Cell.Str = append([]byte(nil), r.buf[r.off:r.off+keep]...)
		fv.Count = keep
		r.off += int(n)
		return true
	case f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		return r.array(fv)
	case tabletext.UnionOf(f) != nil:
		return r.union(fv)
	case tabletext.EnumOf(f) != nil:
		return r.enumCell(&fv.Cell, f)
	case tabletext.StructOf(f) != nil:
		n, ok := r.leb()
		if !ok || !r.has(int(n)) || n > uint64(len(r.buf)) {
			r.report.Malformed = true
			return false
		}
		sub := r.sub(int(n))
		fv.Cell.Tab = r.m.New(tabletext.StructOf(f))
		sub.bodyAt(fv.Cell.Tab, true)
		// A BODY'S TERMINATOR IS THE END OF ITS PAYLOAD (§3): a body whose
		// terminator is not the last byte of its L is framing damage — the
		// payload stops, the field reads its declared defaults, and the
		// enclosing body continues past it by L.
		if sub.off != int(n) {
			r.report.Malformed = true
			fv.Cell.Tab = r.m.New(tabletext.StructOf(f))
		}
		r.off += int(n)
		return true
	}
	return r.scalar(&fv.Cell, f, true)
}

// enumCell reads an enum value: kind 30's payload is the reference to the
// VARIANT NAME's id, `0` for None. A reference this reader's enum cannot name
// is §4's ordinary `unknown` — the field reads None and one event counts — and
// a reference ABOVE the entry count is framing damage like any other (§3).
func (r *wireReader) enumCell(cell *tabletext.Cell, f *ir.Field) bool {
	ref, ok := r.leb()
	if !ok {
		r.report.Malformed = true
		return false
	}
	if ref == 0 {
		cell.U = 0 // the zero reference is the enum's None
		return true
	}
	id, named := r.id(ref)
	if !named {
		r.report.Malformed = true
		return false
	}
	v := enumValueForId(tabletext.EnumOf(f), id)
	if v < 0 {
		cell.U = 0
		r.report.Unknown++
		return true
	}
	cell.U = uint64(v)
	return true
}

func (r *wireReader) array(fv *tabletext.Field) bool {
	ok, _ := r.arrayBody(fv, -1)
	return ok
}

// arrayBody decodes an array payload. A FIELD carries its own length first; an
// ARM does not — the arm's L already framed it (docs/SPEC-TABLES.md §2.6,
// §3), so the caller passes the length it was framed by.
//
// `ok` is false where the framing is damaged and the walk stops. `selected` is
// false where the body declares a DIFFERENT ELEMENT KIND: the payload is not
// this array's, so a field keeps its declared default and an ARM holding it is
// not selected — the union reads None (§3).
func (r *wireReader) arrayBody(fv *tabletext.Field, framed int) (ok, selected bool) {
	f := fv.Def
	bound := int(f.ArrayBound)
	if f.Type.Kind == ir.TBytes {
		bound = int(f.Type.Size)
	}
	counted := f.Type.Kind == ir.TBytes || f.Array == ir.ArrayCounted
	bodyLen := framed
	if framed < 0 {
		n, good := r.leb()
		if !good || n > uint64(len(r.buf)) {
			r.report.Malformed = true
			return false, false
		}
		bodyLen = int(n)
	}
	if !r.has(bodyLen) {
		r.report.Malformed = true
		return false, false
	}
	end := r.off + bodyLen
	// A body too short for its own header — the element kind byte and the
	// count, so fewer than two bytes — is INERT (§4): the field keeps the value
	// it has, a repeat under this id replaces nothing, no counter is raised,
	// and the walk continues past the length.
	if bodyLen >= 2 {
		decoded := 0
		ek := r.u8()
		count, good := r.leb()
		if !good {
			// A DAMAGED COUNT is framing damage inside the body's own length.
			// A FIELD rode, so an optional is still PRESENT and the array
			// keeps its declared default (§2.3, §4); an ARM whose payload is
			// damaged inside its `L` is that arm's own framing damage, so the
			// union reads None and the parent reads on past `L` (§3) — which
			// is what `selected` false says to the caller.
			r.report.Malformed = true
			r.off = end
			if f.Type.Optional {
				fv.Present = true
			}
			return true, false
		}
		if int(ek) != ir.TableWireElemKind(f) {
			// the element kind is part of the array's identity (§3): counted,
			// the field left at its declared default — for an OPTIONAL array
			// that default is ABSENT, so Present is not set below. AN ELEMENT
			// KIND OF 31 OR 32 TAKES THAT SAME RULE AND NO OTHER.
			r.report.KindMismatch++
			r.off = end
			return true, false
		}
		keep := int(count)
		if count > uint64(bound) {
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
		if counted {
			fv.Count = decoded
		}
	}
	if f.Type.Optional {
		// the field rode with its own element kind (or a body too short to
		// carry one), so it is PRESENT (§2.3); the element-kind mismatch above
		// returned before this line
		fv.Present = true
	}
	r.off = end // excess elements and slack skip via the length
	return true, true
}

func (r *wireReader) element(fv *tabletext.Field, i int) bool {
	f := fv.Def
	if tabletext.UnionOf(f) != nil {
		// an element of an ARRAY OF UNIONS is an ARM HEADER and carries its own
		// kind, so the arm rules apply once per element (§3). The element is
		// RESET the moment its arm id is READ, before the kind byte and the arm
		// length that follow are checked, so a repeat under the field id leaves
		// no arm an earlier occurrence decoded standing — the last occurrence
		// wins whole, even when its own framing is damaged (§3, §4). An element
		// the body cannot even reach is not touched.
		if !r.has(1) {
			r.report.Malformed = true
			return false
		}
		fv.Elems[i].U = 0
		fv.Elems[i].Tab = nil
		return r.unionCell(&fv.Elems[i], f)
	}
	if f.Type.Pointer {
		// an element of an array of pointers: a node index, bounds-checked and
		// stored, never followed (§3.1)
		index, ok := r.leb()
		if !ok {
			r.report.Malformed = true
			return false
		}
		r.resolveCell(&fv.Elems[i], f, index)
		return true
	}
	if tabletext.EnumOf(f) != nil {
		return r.enumCell(&fv.Elems[i], f)
	}
	if tabletext.StructOf(f) != nil {
		n, ok := r.leb()
		if !ok || !r.has(int(n)) || n > uint64(len(r.buf)) {
			r.report.Malformed = true
			return false
		}
		sub := r.sub(int(n))
		fv.Elems[i].Tab = r.m.New(tabletext.StructOf(f))
		sub.bodyAt(fv.Elems[i].Tab, true)
		r.off += int(n)
		return true
	}
	return r.scalar(&fv.Elems[i], f, false)
}

// keyed places an enum-keyed array's triples BY KEY REFERENCE, so a slot lands
// by name however the enum moved (docs/SPEC-TABLES.md §3.2). A key this reader
// cannot name is skipped by its length and counted unknown; a slot the writer
// never sent keeps its declared default; a stored key reference of `0` is
// MALFORMED rather than an unknown variant, because `0` names no id at all and
// a slot must say which variant it keys.
func (r *wireReader) keyed(fv *tabletext.Field) bool {
	f := fv.Def
	n, ok := r.leb()
	if !ok || n > uint64(len(r.buf)) {
		r.report.Malformed = true
		return false
	}
	bodyLen := int(n)
	if !r.has(bodyLen) {
		r.report.Malformed = true
		return false
	}
	end := r.off + bodyLen
	if bodyLen < 2 {
		r.off = end
		return true
	}
	elemKind := r.u8()
	count, good := r.leb()
	if !good {
		r.report.Malformed = true
		r.off = end
		return true
	}
	if int(elemKind) != ir.TableWireElemKind(f) {
		r.report.KindMismatch++
		r.off = end
		return true
	}
	sub := r.sub(end - r.off)
	for range count {
		key, good := sub.leb()
		if !good {
			r.report.Malformed = true
			break
		}
		if key == 0 {
			// None is the enum's null and keys no slot, and `0` names no id at
			// all, so a body carrying one is damaged rather than merely
			// foreign: the read stops this body and keeps what it decoded (§3.2)
			r.report.Malformed = true
			break
		}
		keyID, named := sub.id(key)
		if !named {
			r.report.Malformed = true
			break
		}
		elemLen, good := sub.leb()
		if !good || elemLen > uint64(len(sub.buf)) || !sub.has(int(elemLen)) {
			r.report.Malformed = true
			break
		}
		slot := tabletext.KeyedValueSlot(f, enumValueForId(f.KeyEnumRef, keyID))
		if slot < 0 {
			r.report.Unknown++ // a slot this reader cannot name
			sub.off += int(elemLen)
			continue
		}
		elem := sub.sub(int(elemLen))
		switch {
		case tabletext.EnumOf(f) != nil:
			elem.enumCell(&fv.Elems[slot], f)
		case tabletext.StructOf(f) != nil:
			fv.Elems[slot].Tab = r.m.New(tabletext.StructOf(f))
			elem.bodyAt(fv.Elems[slot].Tab, true)
		default:
			elem.scalar(&fv.Elems[slot], f, false)
		}
		sub.off += int(elemLen)
	}
	r.off = end // unread triples and slack skip via the length
	return true
}

func (r *wireReader) union(fv *tabletext.Field) bool { return r.unionCell(&fv.Cell, fv.Def) }

// unionCell decodes the union framing into one cell — a field's, or an element
// of an array of unions (§3). AN ARM HEADER IS A FIELD HEADER: the arm id
// reference, the arm's KIND byte, `L`, then `L` bytes of arm payload.
func (r *wireReader) unionCell(cell *tabletext.Cell, f *ir.Field) bool {
	un := tabletext.UnionOf(f)
	ref, ok := r.leb()
	if !ok {
		r.report.Malformed = true
		return false
	}
	if ref == 0 {
		cell.U = 0
		cell.Tab = nil
		cell.Arm = nil
		return true // empty: the id is the whole payload, not even a kind byte
	}
	armID, named := r.id(ref)
	if !named {
		r.report.Malformed = true
		return false
	}
	if !r.has(1) {
		r.report.Malformed = true
		return false
	}
	kind := r.u8()
	n, good := r.leb()
	if !good || n > uint64(len(r.buf)) || !r.has(int(n)) {
		r.report.Malformed = true
		return false
	}
	length := int(n)
	sub := r.sub(length)
	tag := 0
	for i, v := range un.Variants {
		if ir.TableWireId(v.Name) == armID {
			tag = i + 1
			break
		}
	}
	switch {
	case tag == 0:
		// an arm this reader cannot name: the value reads EMPTY and the body
		// is skipped by its length, never misdecoded. The reset is explicit: a
		// repeated field id must not leave an arm an earlier occurrence
		// decoded standing (§4).
		cell.U = 0
		cell.Tab = nil
		cell.Arm = nil
		r.report.Unknown++
	case int(kind) != armWireKind(un.Variants[tag-1]):
		// A RETYPED ARM IS JUDGED BY THE FIELD RULES: an arm arriving under a
		// kind the reader does not declare for it is a KIND MISMATCH — the arm
		// skips by L, the union reads None, and the parent reads on (§3).
		cell.U = 0
		cell.Tab = nil
		cell.Arm = nil
		r.report.KindMismatch++
	default:
		arm := un.Variants[tag-1]
		if arm.Body() {
			payload := r.m.New(arm.Ref)
			sub.bodyAt(payload, true)
			if sub.off != length {
				// a body whose terminator is not the last byte of its L is
				// framing damage: the union reads None and the enclosing body
				// continues past the arm by L (§3)
				r.report.Malformed = true
				cell.U = 0
				cell.Tab = nil
			} else {
				cell.U = uint64(tag)
				cell.Tab = payload
			}
		} else {
			cell.Tab = nil
			if !sub.arm(cell, arm, tag, length) {
				cell.U = 0
				cell.Arm = nil
			}
		}
	}
	r.off += length
	return true
}

// arm decodes ONE ARM's payload out of a reader bounded to the arm's `L`
// (docs/SPEC-TABLES.md §3's arm payload table). The kind byte already agreed,
// so what is left to check is the LENGTH: a fixed-width arm whose `L` is not
// its kind's width, a reference-shaped arm whose `L` is not the byte count of
// the reference it frames, and a length-shaped arm damaged inside its `L` are
// that arm's own framing damage — the union reads None, `malformed` counts,
// and the parent reads on past `L`.
func (r *wireReader) arm(cell *tabletext.Cell, arm ir.UnionVariant, tag, n int) bool {
	if arm.Void() {
		if n != 0 {
			// a payload-free arm carries nothing: an L that is not zero is
			// that arm's own framing damage (§3)
			r.report.Malformed = true
			return false
		}
		cell.U = uint64(tag)
		cell.Arm = nil
		return true
	}
	f := arm.F
	if w := ir.ArmWireFixedWidth(f); w > 0 && n != w {
		r.report.Malformed = true
		return false
	}
	fv := r.m.NewArm(arm)
	ok := true
	switch {
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		index, good := r.leb()
		if !good || r.off != n {
			r.report.Malformed = true
			return false
		}
		r.resolveCell(&fv.Cell, f, index)
	case f.Type.Kind == ir.TString:
		keep := n
		if bound := int(f.Type.Size); keep > bound {
			keep = bound
			r.report.Clamped++
		}
		fv.Cell.Str = append([]byte(nil), r.buf[r.off:r.off+keep]...)
		fv.Count = keep
		r.off += n
	case f.Type.Kind == ir.TBytes, f.Array != ir.ArrayNone:
		var selected bool
		ok, selected = r.arrayBody(fv, n)
		if ok && !selected {
			// the payload declares another array's element kind: the arm is
			// not this one, so the union reads None and the parent reads on
			// past L, exactly as a field's element-kind mismatch does (§3)
			return false
		}
	case tabletext.UnionOf(f) != nil:
		ok = r.unionCell(&fv.Cell, f)
	case tabletext.EnumOf(f) != nil:
		ok = r.enumCell(&fv.Cell, f)
		if ok && r.off != n {
			r.report.Malformed = true
			return false
		}
	default:
		ok = r.scalar(&fv.Cell, f, true)
	}
	if !ok {
		return false
	}
	cell.U = uint64(tag)
	cell.Arm = fv
	return true
}

// scalar decodes one fixed-width value. atField says whether truncation is
// outer framing damage (a scalar FIELD stops the decode) or an element's (the
// prefix is kept and the loop breaks).
func (r *wireReader) scalar(cell *tabletext.Cell, f *ir.Field, atField bool) bool {
	kind := ir.TableScalarKind(f)
	width := ir.TableKindWidth(kind)
	if !r.has(width) {
		r.report.Malformed = true
		return false
	}
	switch kind {
	case ir.TableKindBool:
		cell.B = r.u8() != 0
		return true
	case ir.TableKindF32:
		bits := r.u32()
		if bits&0x7F800000 == 0x7F800000 && bits&0x007FFFFF != 0 {
			// a NaN's PAYLOAD IS DATA the reference carries bit for bit: the
			// hardware float32→float64 conversion would set the quiet bit, so
			// the widening is done on the bits. A NaN compares outside every
			// range, so the clamp below could not fire on it anyway.
			cell.F = widenF32NaN(bits)
			return true
		}
		v := float64(math.Float32frombits(bits))
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
		raw = uint64(binary.LittleEndian.Uint16(r.buf[r.off:]))
		r.off += 2
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
func enumValueForId(e *ir.Enum, id uint64) int64 {
	for i, v := range e.Variants {
		if ir.TableWireId(v) == id {
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
