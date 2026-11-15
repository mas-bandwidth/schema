package tablewire

import (
	"encoding/binary"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The element kinds an array or an enum-keyed body may carry are the closed
// lists of docs/SPEC-TABLES.md §3: kind 14 over 1–13, 15, 17, 18–30, and kind
// 16 over 1–11, 13 and 18–30. Any other element kind is damage under ANY count,
// a count of 0 included, in a declared field and in an unknown one alike.
const elementKindsClosedUnit = `package elemkindsclosed

enum Slot { A, B }

table Inner { x int32 }

table Root
{
    nums  [..8]int32
    by    [Slot]int32
    inner [..2]Inner
}
`

func elemArrayKindExpected(kind int) bool {
	switch {
	case kind >= ir.TableKindBool && kind <= ir.TableKindTable:
		return true
	case kind == ir.TableKindUnion, kind == ir.TableKindPointer:
		return true
	case kind >= ir.TableKindI128 && kind <= ir.TableKindUFixed128:
		return true
	case kind == ir.TableKindEnum:
		return true
	}
	return false
}

func elemKeyedKindExpected(kind int) bool {
	switch {
	case kind >= ir.TableKindBool && kind <= ir.TableKindF64:
		return true
	case kind == ir.TableKindTable:
		return true
	case kind >= ir.TableKindI128 && kind <= ir.TableKindUFixed128:
		return true
	case kind == ir.TableKindEnum:
		return true
	}
	return false
}

func elemCat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func elemLeb(v uint64) []byte { return binary.AppendUvarint(nil, v) }

func elemFile(ids []uint64, body []byte) []byte {
	out := append([]byte{ir.TableWireForm}, body...)
	for _, id := range ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(len(ids)))
}

func elemField(ref uint64, kind int, content []byte) []byte {
	return elemCat(elemLeb(ref), []byte{byte(kind)}, elemLeb(uint64(len(content))), content, []byte{0})
}

func elemArrayWire(ids []uint64, ref uint64, elemKind int, count uint64, elems ...[]byte) []byte {
	content := elemCat([]byte{byte(elemKind)}, elemLeb(count), elemCat(elems...))
	return elemFile(ids, elemField(ref, ir.TableKindArray, content))
}

func elemKeyedWire(ids []uint64, ref uint64, elemKind int, count uint64, triples ...[]byte) []byte {
	content := elemCat([]byte{byte(elemKind)}, elemLeb(count), elemCat(triples...))
	return elemFile(ids, elemField(ref, ir.TableKindKeyed, content))
}

func elemKeyedTriple(keyRef uint64, payload []byte) []byte {
	return elemCat(elemLeb(keyRef), elemLeb(uint64(len(payload))), payload)
}

// elemArrayPayload is one array element under the element kind's own framing,
// or ok=false where that kind has no payload a reader could follow.
func elemArrayPayload(kind int) ([]byte, bool) {
	if fixedWidthKind(kind) {
		return make([]byte, ir.TableKindWidth(kind)), true
	}
	switch kind {
	case ir.TableKindString:
		return elemLeb(0), true // an empty string
	case ir.TableKindTable:
		return []byte{1, 0}, true // L = 1 and an empty body's terminator
	case ir.TableKindUnion, ir.TableKindPointer, ir.TableKindEnum:
		return []byte{0}, true // None, null, and the zero variant reference
	case ir.TableKindEscape, ir.TableKindNoPayload:
		return elemLeb(0), true // an empty payload under its own L
	case ir.TableKindArray, ir.TableKindKeyed:
		// an element framed by its own L: a body over element kind 6, count 0
		body := []byte{byte(ir.TableKindU8), 0}
		return elemLeb(uint64(len(body))), true
	}
	return nil, false
}

// elemKeyedPayload is one keyed element's content, already inside the slot's
// own L, or ok=false where that kind has no payload a reader could follow.
func elemKeyedPayload(kind int) ([]byte, bool) {
	if fixedWidthKind(kind) {
		return make([]byte, ir.TableKindWidth(kind)), true
	}
	switch kind {
	case ir.TableKindString:
		return nil, true // empty text
	case ir.TableKindTable:
		return []byte{0}, true // an empty body's terminator
	case ir.TableKindUnion, ir.TableKindPointer, ir.TableKindEnum:
		return []byte{0}, true
	case ir.TableKindEscape, ir.TableKindNoPayload:
		return nil, true
	case ir.TableKindArray, ir.TableKindKeyed:
		return []byte{byte(ir.TableKindU8), 0}, true
	}
	return nil, false
}

// TestArrayElementKindsClosed holds every array and enum-keyed body header to
// docs/SPEC-TABLES.md §3's closed element-kind lists. A kind on the list reads
// at count 0 and at count 1; a kind off it refuses `wire_damaged` under both
// counts, in a field this reader cannot name and in one it declares.
func TestArrayElementKindsClosed(t *testing.T) {
	t.Parallel()
	m := covModel(t, elementKindsClosedUnit)
	root := m.Lookup("Root")
	nums := ir.TableWireId("nums")
	by := ir.TableWireId("by")
	unknown := ir.TableWireId("zzz")
	slotA := ir.TableWireId("A")

	read := func(data []byte) (bool, error) {
		inst := m.New(root)
		var report tabletext.Report
		ok, err := Decode(m, inst, data, &report)
		if ok && err != nil {
			t.Fatalf("an accepted read answered an error: %v", err)
		}
		if !ok && err == nil {
			t.Fatalf("a refused read answered no error")
		}
		return ok, err
	}

	for kind := 0; kind <= 255; kind++ {
		arrayOn := elemArrayKindExpected(kind)
		keyedOn := elemKeyedKindExpected(kind)

		for _, count := range []uint64{0, 1} {
			var elems [][]byte
			if count == 1 {
				if p, ok := elemArrayPayload(kind); ok {
					elems = append(elems, p)
				}
			}
			// an unknown field is walked by the wire-alone rules
			ok, err := read(elemArrayWire([]uint64{unknown}, 1, kind, count, elems...))
			if ok != arrayOn {
				t.Errorf("unknown array element kind %d count %d: accepted=%t want=%t (%v)", kind, count, ok, arrayOn, err)
			}
			if !arrayOn && RefusalReason(err) != ReasonWireDamaged {
				t.Errorf("unknown array element kind %d count %d: refused %q, want %q", kind, count, RefusalReason(err), ReasonWireDamaged)
			}

			var triples [][]byte
			if count == 1 {
				if p, ok := elemKeyedPayload(kind); ok {
					triples = append(triples, elemKeyedTriple(2, p))
				}
			}
			ok, err = read(elemKeyedWire([]uint64{unknown, slotA}, 1, kind, count, triples...))
			if ok != keyedOn {
				t.Errorf("unknown keyed element kind %d count %d: accepted=%t want=%t (%v)", kind, count, ok, keyedOn, err)
			}
			if !keyedOn && RefusalReason(err) != ReasonWireDamaged {
				t.Errorf("unknown keyed element kind %d count %d: refused %q, want %q", kind, count, RefusalReason(err), ReasonWireDamaged)
			}
		}

		// A DECLARED array and keyed body answer the list first: an element
		// kind off it is damage under either count, never merely a kind
		// mismatch, wherever a payload can follow.
		for _, count := range []uint64{0, 1} {
			var elems [][]byte
			if count == 1 {
				if p, ok := elemArrayPayload(kind); ok {
					elems = append(elems, p)
				}
			}
			if !arrayOn {
				ok, err := read(elemArrayWire([]uint64{nums}, 1, kind, count, elems...))
				if ok || RefusalReason(err) != ReasonWireDamaged {
					t.Errorf("declared array element kind %d count %d: accepted=%t reason=%q, want refusal %q", kind, count, ok, RefusalReason(err), ReasonWireDamaged)
				}
			}
			var triples [][]byte
			if count == 1 {
				if p, ok := elemKeyedPayload(kind); ok {
					triples = append(triples, elemKeyedTriple(2, p))
				}
			}
			if !keyedOn {
				ok, err := read(elemKeyedWire([]uint64{by, slotA}, 1, kind, count, triples...))
				if ok || RefusalReason(err) != ReasonWireDamaged {
					t.Errorf("declared keyed element kind %d count %d: accepted=%t reason=%q, want refusal %q", kind, count, ok, RefusalReason(err), ReasonWireDamaged)
				}
			}
		}
	}

	// READ CONTROLS: the declared array and keyed body at their own element
	// kind, count 0 and count 1.
	for _, count := range []uint64{0, 1} {
		var elems [][]byte
		if count == 1 {
			elems = [][]byte{make([]byte, ir.TableKindWidth(ir.TableKindI32))}
		}
		if ok, err := read(elemArrayWire([]uint64{nums}, 1, ir.TableKindI32, count, elems...)); !ok {
			t.Errorf("declared array control count %d: %v", count, err)
		}
		var triples [][]byte
		if count == 1 {
			triples = [][]byte{elemKeyedTriple(2, make([]byte, ir.TableKindWidth(ir.TableKindI32)))}
		}
		if ok, err := read(elemKeyedWire([]uint64{by, slotA}, 1, ir.TableKindI32, count, triples...)); !ok {
			t.Errorf("declared keyed control count %d: %v", count, err)
		}
	}
}
