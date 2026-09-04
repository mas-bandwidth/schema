// THE RESOLVED FORM (docs/SPEC-TABLES.md §3.3, §6.6): a body with EVERY
// REFERENCE REPLACED BY THE SIXTY-FOUR-BIT ID IT NAMES and every length
// recomputed to frame that substitution.
//
// It exists because the two wire forms do not write the same reference bytes
// for the same value: a file's slots are its own FIRST-USE order and a
// connection's are the unit's PROJECTION order. What IS invariant is this
// normal form, and that invariance is the claim §3.3 makes and the one a
// golden can go red on.
package tablewire

import (
	"encoding/binary"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Resolve is one BODY's resolved form: the same fields in the same order, each
// reference written as the eight bytes of the id it names, little-endian, and
// every length recomputed over the substitution. ok is false where the body's
// framing does not hold, which is the caller's answer that there is nothing to
// compare.
//
// The body terminator stays a single zero BYTE rather than eight, because it
// is a position and not an id: reference `0` names no id, and the three places
// it is a value — the terminator, the enum's None and the union's empty arm —
// are the wire's own way of spelling "no id" and have nothing to substitute.
func Resolve(body []byte, ids []uint64) (out []byte, ok bool) {
	r := &resolver{ids: ids}
	out, ok = r.body(body)
	return out, ok
}

type resolver struct{ ids []uint64 }

// id substitutes one reference, which must name an entry.
func (r *resolver) id(ref uint64) (uint64, bool) {
	if ref == 0 || ref > uint64(len(r.ids)) {
		return 0, false
	}
	return r.ids[ref-1], true
}

func appendID(out []byte, id uint64) []byte { return binary.LittleEndian.AppendUint64(out, id) }

// body resolves a whole body, terminator included.
func (r *resolver) body(buf []byte) ([]byte, bool) {
	var out []byte
	at := 0
	for {
		ref, next, good := readLeb(buf, at)
		if !good {
			return nil, false
		}
		at = next
		if ref == 0 {
			return append(out, 0), true
		}
		id, named := r.id(ref)
		if !named || at >= len(buf) {
			return nil, false
		}
		kind := buf[at]
		at++
		payload, size, good := r.payload(kind, buf[at:], id)
		if !good {
			return nil, false
		}
		at += size
		out = appendID(out, id)
		out = append(out, kind)
		out = append(out, payload...)
	}
}

// payload resolves one field's payload by kind, answering the resolved bytes
// and how many SOURCE bytes it consumed. `id` is the field's own id, which the
// node table's reserved one is recognised by.
func (r *resolver) payload(kind uint8, buf []byte, id uint64) ([]byte, int, bool) {
	switch kind {
	case ir.TableKindBool, ir.TableKindI8, ir.TableKindU8,
		ir.TableKindI16, ir.TableKindU16,
		ir.TableKindI32, ir.TableKindU32, ir.TableKindF32,
		ir.TableKindI64, ir.TableKindU64, ir.TableKindF64,
		ir.TableKindI128, ir.TableKindU128,
		ir.TableKindFixed8, ir.TableKindFixed16, ir.TableKindFixed32, ir.TableKindFixed64, ir.TableKindFixed128,
		ir.TableKindUFixed8, ir.TableKindUFixed16, ir.TableKindUFixed32, ir.TableKindUFixed64, ir.TableKindUFixed128:
		w := ir.TableKindWidth(int(kind))
		if w > len(buf) {
			return nil, 0, false
		}
		return buf[:w], w, true
	case ir.TableKindPointer:
		// a NODE INDEX is a position in the numbering and not an id: it names
		// no entry and nothing substitutes for it (§3.1)
		_, next, good := readLeb(buf, 0)
		if !good {
			return nil, 0, false
		}
		return buf[:next], next, true
	case ir.TableKindEnum:
		ref, next, good := readLeb(buf, 0)
		if !good {
			return nil, 0, false
		}
		if ref == 0 {
			return buf[:next], next, true // the enum's None: no id at all
		}
		variant, named := r.id(ref)
		if !named {
			return nil, 0, false
		}
		return appendID(nil, variant), next, true
	case ir.TableKindString, ir.TableKindEscape, ir.TableKindNoPayload:
		n, next, good := readLeb(buf, 0)
		if !good || n > uint64(len(buf)-next) {
			return nil, 0, false
		}
		if id == ir.TableNodeWireId {
			// THE NODE TABLE rides under the reserved id at kind 12 and its
			// records name their type ids through the SAME table every other
			// reference resolves against (§3.1, §3.3), so its payload resolves
			// too — a resolved form that left them alone would compare two
			// numberings' reference bytes.
			inner, ok := r.nodeTable(buf[next : next+int(n)])
			if !ok {
				return nil, 0, false
			}
			return framedPayload(inner), next + int(n), true
		}
		return buf[:next+int(n)], next + int(n), true
	case ir.TableKindTable:
		n, next, good := readLeb(buf, 0)
		if !good || n > uint64(len(buf)-next) {
			return nil, 0, false
		}
		inner, ok := r.body(buf[next : next+int(n)])
		if !ok {
			return nil, 0, false
		}
		return framedPayload(inner), next + int(n), true
	case ir.TableKindArray, ir.TableKindKeyed:
		n, next, good := readLeb(buf, 0)
		if !good || n > uint64(len(buf)-next) {
			return nil, 0, false
		}
		inner, ok := r.elements(kind, buf[next:next+int(n)])
		if !ok {
			return nil, 0, false
		}
		return framedPayload(inner), next + int(n), true
	case ir.TableKindUnion:
		return r.union(buf)
	}
	return nil, 0, false
}

// framedPayload frames a resolved body or array with its own recomputed
// length, which is the whole of "every length recomputed" (§3.3).
func framedPayload(inner []byte) []byte {
	return append(appendLeb(nil, uint64(len(inner))), inner...)
}

// elements resolves an ARRAY or KEYED body: the element kind byte, the count,
// and the elements at their own framing (§3, §3.2).
func (r *resolver) elements(kind uint8, buf []byte) ([]byte, bool) {
	if len(buf) == 0 {
		return nil, true // an empty body carries no element kind at all
	}
	elemKind := buf[0]
	count, at, good := readLeb(buf, 1)
	if !good {
		return nil, false
	}
	out := append([]byte{elemKind}, appendLeb(nil, count)...)
	for range count {
		if kind == ir.TableKindKeyed {
			// a KEYED slot leads with its KEY REFERENCE, which names the
			// variant the slot keys (§3.2), and then an L framing the value
			key, next, ok := readLeb(buf, at)
			if !ok {
				return nil, false
			}
			at = next
			id, named := r.id(key)
			if !named {
				return nil, false
			}
			out = appendID(out, id)
			n, next, ok := readLeb(buf, at)
			if !ok || n > uint64(len(buf)-next) {
				return nil, false
			}
			inner, good := r.element(elemKind, buf[next:next+int(n)])
			if !good {
				return nil, false
			}
			out = append(out, framedPayload(inner)...)
			at = next + int(n)
			continue
		}
		resolved, next, ok := r.elementAt(elemKind, buf, at)
		if !ok {
			return nil, false
		}
		out = append(out, resolved...)
		at = next
	}
	return out, true
}

// element resolves one element whose own L already framed it — the KEYED
// spelling, where the slot's value is length-framed whatever its kind.
func (r *resolver) element(elemKind uint8, buf []byte) ([]byte, bool) {
	if elemKind == ir.TableKindTable {
		return r.body(buf)
	}
	out, next, ok := r.elementAt(elemKind, buf, 0)
	if !ok || next != len(buf) {
		return nil, false
	}
	return out, true
}

// elementAt resolves one element of a POSITIONAL array body, at its own
// framing, answering the offset it ends at.
func (r *resolver) elementAt(elemKind uint8, buf []byte, at int) ([]byte, int, bool) {
	switch elemKind {
	case ir.TableKindTable:
		n, next, good := readLeb(buf, at)
		if !good || n > uint64(len(buf)-next) {
			return nil, 0, false
		}
		inner, ok := r.body(buf[next : next+int(n)])
		if !ok {
			return nil, 0, false
		}
		return framedPayload(inner), next + int(n), true
	case ir.TableKindUnion:
		out, size, ok := r.union(buf[at:])
		if !ok {
			return nil, 0, false
		}
		return out, at + size, true
	default:
		out, size, ok := r.payload(elemKind, buf[at:], 0)
		if !ok {
			return nil, 0, false
		}
		return out, at + size, true
	}
}

// union resolves a union payload: the ARM REFERENCE, then — where the arm is
// not the empty one — its kind byte, its L and its own payload (§2.6, §3).
func (r *resolver) union(buf []byte) ([]byte, int, bool) {
	arm, at, good := readLeb(buf, 0)
	if !good {
		return nil, 0, false
	}
	if arm == 0 {
		return buf[:at], at, true // the union's empty arm: no id at all
	}
	id, named := r.id(arm)
	if !named || at >= len(buf) {
		return nil, 0, false
	}
	kind := buf[at]
	at++
	n, next, good := readLeb(buf, at)
	if !good || n > uint64(len(buf)-next) {
		return nil, 0, false
	}
	inner, ok := r.element(kind, buf[next:next+int(n)])
	if !ok {
		return nil, 0, false
	}
	out := appendID(nil, id)
	out = append(out, kind)
	return append(out, framedPayload(inner)...), next + int(n), true
}

// nodeTable resolves the FLAT NODE TABLE's payload (§3.1): the record count,
// then each record's type id REFERENCE, its length and its body.
func (r *resolver) nodeTable(buf []byte) ([]byte, bool) {
	count, at, good := readLeb(buf, 0)
	if !good {
		return nil, false
	}
	out := appendLeb(nil, count)
	for range count {
		ref, next, ok := readLeb(buf, at)
		if !ok {
			return nil, false
		}
		at = next
		id, named := r.id(ref)
		if !named {
			return nil, false
		}
		out = appendID(out, id)
		n, next, ok := readLeb(buf, at)
		if !ok || n > uint64(len(buf)-next) {
			return nil, false
		}
		var inner []byte
		if id == ir.BytesWireTypeId || id == ir.StringWireTypeId || id == ir.WstringWireTypeId {
			// a BLOB's record IS its bytes (§2.5): there is no body to walk
			// and no reference inside it
			inner = buf[next : next+int(n)]
		} else {
			resolved, good := r.body(buf[next : next+int(n)])
			if !good {
				return nil, false
			}
			inner = resolved
		}
		out = append(out, framedPayload(inner)...)
		at = next + int(n)
	}
	return out, true
}
