package compiler

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// An enum-keyed slot whose key names no variant of the reader's enum is an
// unknown payload of the element kind (docs/SPEC-TABLES.md §3.2, §4): its
// bytes are validated as any unknown field's are, and damage inside it
// refuses the whole load. The verdicts below are the Go engine's; the C read
// must answer the same.
func TestCTableKeyedUnnamedSlotChecked(t *testing.T) {
	t.Parallel()
	t.Run("variable", testCTableKeyedUnnamedVariable)
	t.Run("fixed", testCTableKeyedUnnamedFixed)
}

type keyedWireSlot struct {
	keyRef uint64
	body   []byte
}

type keyedWireField struct {
	id       uint64
	elemKind byte
	slots    []keyedWireSlot
	start    int
	end      int
}

func keyedSplit(t *testing.T, wire []byte) (body []byte, ids []uint64, trailer []byte) {
	t.Helper()
	if len(wire) < 9 {
		t.Fatalf("wire too short: %d", len(wire))
	}
	count := binary.LittleEndian.Uint64(wire[len(wire)-8:])
	span := int(count)*8 + 8
	if span+1 > len(wire) {
		t.Fatalf("id table count %d does not fit %d bytes", count, len(wire))
	}
	first := len(wire) - span
	ids = make([]uint64, count)
	for i := range ids {
		ids[i] = binary.LittleEndian.Uint64(wire[first+i*8:])
	}
	return wire[1:first], ids, wire[first:]
}

func keyedReadLeb(t *testing.T, b []byte, off int) (uint64, int) {
	t.Helper()
	var v uint64
	var shift uint
	for i := 0; i < 10; i++ {
		if off+i >= len(b) {
			t.Fatalf("truncated leb at %d", off)
		}
		c := b[off+i]
		v |= uint64(c&0x7f) << shift
		if c < 0x80 {
			return v, off + i + 1
		}
		shift += 7
	}
	t.Fatalf("overlong leb at %d", off)
	return 0, 0
}

func keyedAppendLeb(dst []byte, v uint64) []byte {
	for v >= 0x80 {
		dst = append(dst, byte(v)|0x80)
		v >>= 7
	}
	return append(dst, byte(v))
}

func keyedParse(t *testing.T, body []byte, ids []uint64) []keyedWireField {
	t.Helper()
	var fields []keyedWireField
	for off := 0; ; {
		ref, next := keyedReadLeb(t, body, off)
		if ref == 0 {
			if next != len(body) {
				t.Fatalf("bytes after root terminator: %d of %d", next, len(body))
			}
			return fields
		}
		if ref > uint64(len(ids)) {
			t.Fatalf("field reference %d past %d ids", ref, len(ids))
		}
		start := off
		off = next
		kind := body[off]
		off++
		if kind != ir.TableKindKeyed {
			t.Fatalf("field %016x is kind %d, not keyed", ids[ref-1], kind)
		}
		length, after := keyedReadLeb(t, body, off)
		off = after
		if off+int(length) > len(body) {
			t.Fatalf("keyed L %d crosses body", length)
		}
		payload := body[off : off+int(length)]
		off += int(length)
		if len(payload) < 2 {
			t.Fatalf("keyed body too short: %d", len(payload))
		}
		f := keyedWireField{id: ids[ref-1], elemKind: payload[0], start: start, end: off}
		count, at := keyedReadLeb(t, payload, 1)
		for i := uint64(0); i < count; i++ {
			keyRef, k := keyedReadLeb(t, payload, at)
			elemLen, e := keyedReadLeb(t, payload, k)
			at = e
			if at+int(elemLen) > len(payload) {
				t.Fatalf("slot L %d crosses keyed body", elemLen)
			}
			f.slots = append(f.slots, keyedWireSlot{keyRef: keyRef, body: append([]byte(nil), payload[at:at+int(elemLen)]...)})
			at += int(elemLen)
		}
		if at != len(payload) {
			t.Fatalf("keyed body has %d unclaimed bytes", len(payload)-at)
		}
		fields = append(fields, f)
	}
}

func keyedBuild(ids []uint64, fieldID uint64, elemKind byte, slots []keyedWireSlot) []byte {
	fieldRef := uint64(0)
	for i, id := range ids {
		if id == fieldID {
			fieldRef = uint64(i) + 1
			break
		}
	}
	payload := []byte{elemKind}
	payload = keyedAppendLeb(payload, uint64(len(slots)))
	for _, slot := range slots {
		payload = keyedAppendLeb(payload, slot.keyRef)
		payload = keyedAppendLeb(payload, uint64(len(slot.body)))
		payload = append(payload, slot.body...)
	}
	out := keyedAppendLeb(nil, fieldRef)
	out = append(out, ir.TableKindKeyed)
	out = keyedAppendLeb(out, uint64(len(payload)))
	return append(out, payload...)
}

// keyedDamage replaces the body of the slot keyed by keyName inside fieldName,
// leaving every other field byte for byte as the writer emitted it.
func keyedDamage(t *testing.T, wire []byte, fieldName, keyName string, damage func([]byte) []byte) []byte {
	t.Helper()
	body, ids, trailer := keyedSplit(t, wire)
	fields := keyedParse(t, body, ids)
	fieldID := ir.TableWireId(fieldName)
	keyID := ir.TableWireId(keyName)
	var rebuilt []byte
	hitField, hitSlot := false, false
	for _, field := range fields {
		if field.id != fieldID {
			rebuilt = append(rebuilt, body[field.start:field.end]...)
			continue
		}
		hitField = true
		slots := append([]keyedWireSlot(nil), field.slots...)
		for i := range slots {
			if slots[i].keyRef >= 1 && int(slots[i].keyRef) <= len(ids) && ids[slots[i].keyRef-1] == keyID {
				slots[i].body = damage(append([]byte(nil), slots[i].body...))
				hitSlot = true
			}
		}
		rebuilt = append(rebuilt, keyedBuild(ids, fieldID, field.elemKind, slots)...)
	}
	if !hitField || !hitSlot {
		t.Fatalf("field %q slot %q not found (field %v, slot %v)", fieldName, keyName, hitField, hitSlot)
	}
	if body[len(body)-1] != 0 {
		t.Fatalf("root body does not end with its terminator: %x", body[len(body)-1])
	}
	out := append([]byte{wire[0]}, rebuilt...)
	out = append(out, body[len(body)-1])
	return append(out, trailer...)
}

type keyedVerdictCase struct {
	name    string
	wire    []byte
	accept  bool
	unknown int
}

func keyedGoVerdict(t *testing.T, older *ir.Unit, root string, wire []byte) (bool, int) {
	t.Helper()
	model := tabletext.NewModel(older)
	var report tabletext.Report
	ok, _ := tablewire.DecodeVariable(model, model.New(older.Tables[root]), wire, &report)
	return ok, report.Unknown
}

func keyedCases(t *testing.T, older *ir.Unit, root string, newer *ir.Unit, fixture, tableField, scalarField string) []keyedVerdictCase {
	t.Helper()
	model := tabletext.NewModel(newer)
	value := model.New(newer.Tables[root])
	var report tabletext.Report
	if !model.Read(value, []byte(fixture), &report) || !report.Silent() {
		t.Fatalf("fixture: %+v", report)
	}
	wire, err := tablewire.EncodeVariable(model, value)
	if err != nil {
		t.Fatal(err)
	}
	cases := []keyedVerdictCase{{name: "control", wire: wire}}
	if tableField != "" {
		cases = append(cases,
			keyedVerdictCase{name: "table body names an id past the table", wire: keyedDamage(t, wire, tableField, "C", func([]byte) []byte {
				return []byte{0x7f}
			})},
			keyedVerdictCase{name: "table body carries a byte after its terminator", wire: keyedDamage(t, wire, tableField, "C", func([]byte) []byte {
				return []byte{0x00, 0x99}
			})},
		)
	}
	if scalarField != "" {
		cases = append(cases, keyedVerdictCase{name: "scalar body is shorter than its kind", wire: keyedDamage(t, wire, scalarField, "C", func([]byte) []byte {
			return []byte{0x01, 0x02, 0x03}
		})})
	}
	for i := range cases {
		cases[i].accept, cases[i].unknown = keyedGoVerdict(t, older, root, cases[i].wire)
	}
	return cases
}

func testCTableKeyedUnnamedVariable(t *testing.T) {
	t.Parallel()
	const olderSource = `package probe
enum Key {
A
B
}
table Leaf { value uint32 }
table Root {
leaves [Key]Leaf
numbers [Key]uint64
}
`
	const newerSource = `package probe
enum Key {
A
B
C
}
table Leaf { value uint32 }
table Root {
leaves [Key]Leaf
numbers [Key]uint64
}
`
	older := unitFromSource(t, olderSource)
	newer := unitFromSource(t, newerSource)
	cases := keyedCases(t, older, "Root", newer, `{"leaves":{"A":{"value":1},"C":{"value":7}},"numbers":{"A":5,"C":1311768467463790320}}`, "leaves", "numbers")
	var source strings.Builder
	source.WriteString(`#include "ProbeTable.h"
#include <stdio.h>
#include <stdlib.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)
`)
	for i, c := range cases {
		fmt.Fprintf(&source, "static const uint8_t c%d[]={%s};\n", i, cOctets(c.wire))
	}
	source.WriteString("int main(void) {\n TableReport report; const Root * root; uint8_t * region; int64_t need;\n")
	for i, c := range cases {
		accept, unknown := 0, 0
		if c.accept {
			accept = 1
		}
		if c.name == "control" {
			unknown = c.unknown
		}
		fmt.Fprintf(&source, ` { need=root_load_measure(c%d,sizeof(c%d)); CHECK(need>0); region=(uint8_t*)malloc((size_t)need); CHECK(region!=NULL);
   memset(&report,0,sizeof(report)); root=root_load(region,need,c%d,sizeof(c%d),&report);
   CHECK((root!=NULL)==%d); CHECK(report.refused==%d);`, i, i, i, i, accept, 1-accept)
		if c.name == "control" {
			fmt.Fprintf(&source, " CHECK(report.unknown==%d);", unknown)
		}
		source.WriteString(" free(region); }\n")
	}
	source.WriteString(" return 0;\n}\n")
	runCTableWireProbe(t, older, source.String())
}

func testCTableKeyedUnnamedFixed(t *testing.T) {
	t.Parallel()
	const olderSource = `package probe
enum Key {
A
B
}
fixed table Root {
numbers [Key]uint64
}
`
	const newerSource = `package probe
enum Key {
A
B
C
}
fixed table Root {
numbers [Key]uint64
}
`
	older := unitFromSource(t, olderSource)
	newer := unitFromSource(t, newerSource)
	cases := keyedCases(t, older, "Root", newer, `{"numbers":{"A":5,"C":1311768467463790320}}`, "", "numbers")
	var source strings.Builder
	source.WriteString(`#include "ProbeTable.h"
#include <stdio.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)
`)
	for i, c := range cases {
		fmt.Fprintf(&source, "static const uint8_t c%d[]={%s};\n", i, cOctets(c.wire))
	}
	source.WriteString("int main(void) {\n Root value; TableReport report;\n")
	for i, c := range cases {
		accept, unknown := 0, 0
		if c.accept {
			accept = 1
		}
		if c.name == "control" {
			unknown = c.unknown
		}
		fmt.Fprintf(&source, ` { memset(&report,0,sizeof(report)); CHECK(root_load(&value,c%d,sizeof(c%d),&report)==%d); CHECK(report.refused==%d);`, i, i, accept, 1-accept)
		if c.name == "control" {
			fmt.Fprintf(&source, " CHECK(report.unknown==%d);", unknown)
		}
		source.WriteString(" }\n")
	}
	source.WriteString(" return 0;\n}\n")
	runCTableWireProbe(t, older, source.String())
}
