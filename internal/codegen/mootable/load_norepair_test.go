package mootable_test

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/mootable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestMooLoadBuildsNoRepairedValue holds the variable form's rule that a load
// refuses at the point it detects a fault and never builds a repaired value
// (docs/SPEC-TABLES.md §4: "There is no partial value, no repair, no clamp").
// The embedded runtime carries none of the tolerant read's repair arms, and a
// wire carrying each fault kind is refused by the one strict read.
func TestMooLoadBuildsNoRepairedValue(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_load.moo)")
	t.Parallel()

	src := string(mootable.Runtime()["table_load.moo"])
	if src == "" {
		t.Fatal("the embedded runtime carries no table_load.moo")
	}
	for _, arm := range []struct{ name, pattern string }{
		{"an integer range clamp (below)", "x = lo\n\t\t\treport.outOfRange += 1"},
		{"an integer range clamp (above)", "x = hi\n\t\t\treport.outOfRange += 1"},
		{"a bits(N) clamp", "x = cap\n\t\t\treport.outOfRange += 1"},
		{"a float32 range clamp (below)", "return fd.flo32"},
		{"a float32 range clamp (above)", "return fd.fhi32"},
		{"a string element cut at its bound", "keep = e.bound\n\t\t\treport.outOfRange += 1"},
		{"a string field cut at its bound", "keep = fd.bound\n\t\t\t_report.outOfRange += 1"},
		{"a bytes payload cut to the body", "have = sub.e - sub.at"},
		{"a non-finite float32 replaced by its default", "non_finite(report, named)\n\t\t\treturn fd.def or 0"},
		{"a non-finite float64 replaced by its default", "non_finite(report, named)\n\t\t\treturn fd.def or V.Uint64.new(0, 0)"},
		{"a damaged keyed slot refilled with defaults", "A SLOT WHOSE BODY DID NOT SPEND ITS L RESETS TO ITS DECLARED"},
		{"a damaged nested element refilled with defaults", "report.bodyDamaged = true\n\t\t\tif _retain ~= nil then\n\t\t\t\t_retain.refill(d, inst)"},
		{"a damaged nested field refilled with defaults", "A NESTED BODY THAT DID NOT SPEND ITS L FALLS BACK ON ITS DECLARED"},
	} {
		if strings.Contains(src, arm.pattern) {
			t.Errorf("table_load.moo still builds a repaired value: %s", arm.name)
		}
	}

	g := generateFiles(t, map[string]string{"NoRepair.schema": noRepairSchema})
	for _, c := range noRepairWires() {
		if got := runWire(t, g, "Root", c.wire); got != "refused" {
			t.Errorf("%s: the one strict read answers %s, want refused", c.name, got)
		}
	}
}

// noRepairSchema declares one field per repair arm the load used to build.
const noRepairSchema = `package probe

table Child
{
    n int8 | max = 5
}

table Root
{
    small int8 | max = 5
    tiny  bits(3)
    s     string(4)
    b     bytes(4)
    f     float32
    child Child
}
`

// noRepairWire is a form-1 wire writer: the form byte, the body, the id table
// in first-use order, then its u64 count.
type noRepairWire struct {
	body []byte
	ids  []uint64
}

func (w *noRepairWire) ref(name string) uint64 {
	id := ir.TableWireId(name)
	for i, e := range w.ids {
		if e == id {
			return uint64(i) + 1
		}
	}
	w.ids = append(w.ids, id)
	return uint64(len(w.ids))
}

func (w *noRepairWire) u8(v byte) { w.body = append(w.body, v) }

func (w *noRepairWire) leb(v uint64) {
	for v >= 0x80 {
		w.u8(byte(v) | 0x80)
		v >>= 7
	}
	w.u8(byte(v))
}

func (w *noRepairWire) field(name string, kind byte) {
	w.leb(w.ref(name))
	w.u8(kind)
}

// payload writes a field of a framed kind: its L (a one-byte canonical LEB is
// enough for every wire here) and the payload the body writes.
func (w *noRepairWire) payload(name string, kind byte, body func(*noRepairWire)) {
	w.field(name, kind)
	at := len(w.body)
	w.u8(0)
	body(w)
	n := len(w.body) - at - 1
	if n >= 0x80 {
		panic("noRepairWire payload is not one LEB byte")
	}
	w.body[at] = byte(n)
}

func (w *noRepairWire) end() { w.u8(0) }

func (w *noRepairWire) finish() []byte {
	out := append([]byte{1}, w.body...)
	for _, id := range w.ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(len(w.ids)))
}

// noRepairWires is one file per fault kind: a value below its declared range,
// a bits(N) value above its ceiling, a string past its bound, a bytes body
// shorter than its count, a non-finite float, and a nested body that leaves
// bytes after its terminator.
func noRepairWires() []struct{ name string; wire []byte } {
	var out []struct{ name string; wire []byte }
	add := func(name string, w *noRepairWire) {
		out = append(out, struct{ name string; wire []byte }{name, w.finish()})
	}

	rangeBelow := &noRepairWire{}
	rangeBelow.field("small", byte(ir.TableKindI8))
	rangeBelow.u8(9)
	rangeBelow.end()
	add("a value past its declared range", rangeBelow)

	bitsAbove := &noRepairWire{}
	bitsAbove.field("tiny", byte(ir.TableKindU8))
	bitsAbove.u8(8)
	bitsAbove.end()
	add("a bits(N) value past its ceiling", bitsAbove)

	stringOver := &noRepairWire{}
	stringOver.field("s", byte(ir.TableKindString))
	stringOver.leb(5)
	stringOver.body = append(stringOver.body, "abcde"...)
	stringOver.end()
	add("a string past its bound", stringOver)

	bytesShort := &noRepairWire{}
	bytesShort.payload("b", byte(ir.TableKindArray), func(w *noRepairWire) {
		w.u8(byte(ir.TableKindU8))
		w.leb(4)
		w.body = append(w.body, 'a', 'b')
	})
	bytesShort.end()
	add("a bytes payload shorter than its count", bytesShort)

	nonFinite := &noRepairWire{}
	nonFinite.field("f", byte(ir.TableKindF32))
	nonFinite.body = binary.LittleEndian.AppendUint32(nonFinite.body, 0x7fc00000)
	nonFinite.end()
	add("a non-finite float", nonFinite)

	nestedSkew := &noRepairWire{}
	nestedSkew.payload("child", byte(ir.TableKindTable), func(w *noRepairWire) {
		w.field("n", byte(ir.TableKindI8))
		w.u8(1)
		w.end()
		w.u8(0xaa)
	})
	nestedSkew.end()
	add("a nested body that does not spend its L", nestedSkew)

	return out
}
