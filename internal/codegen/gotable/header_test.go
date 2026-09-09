package gotable

import (
	"strings"
	"testing"
)

const headerSchema = `package probe
type Leaf {
 n int32
}
union Choice {
 ping
 leaf Leaf
}
table Child {
 n uint32
}
table Root {
 a uint32
 b uint32
 c uint32
 child Child
 choice Choice
 wide uint128
 payload bytes(20000)
}
`

func TestHeaderAndPut128Shape(t *testing.T) {
	files := generate(t, headerSchema)
	body := ""
	for name, data := range files {
		if strings.HasSuffix(name, "Table.go") {
			body += string(data)
		}
	}
	for _, want := range []string{
		"func (w *TableWriter) Header(ref uint64, kind uint8)",
		"func (w *TableWriter) headerPair(ref uint64, kind uint8) bool",
		"PutUint16(w.Buffer[at:]",
		"w.Ids.refAtHit(",
		"w.headerPair(ref,",
		"func (w *TableWriter) Put128(lo, hi uint64)",
		"w.Advance(16)",
		"w.Put128(",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("Header/Put128 is missing %q", want)
		}
	}
	if strings.Contains(body, "w.IdAt(") {
		t.Fatal("SaveBody still writes the field header through IdAt then Put8")
	}
	if strings.Contains(body, "w.Put64(value.Wide.Lo)") {
		t.Fatal("Put128 still writes the two lanes through Put64")
	}
	save := body
	if i := strings.Index(save, "func RootSaveBody"); i >= 0 {
		save = save[i:]
		if j := strings.Index(save[1:], "\nfunc "); j >= 0 {
			save = save[:j+1]
		}
	}
	if !strings.Contains(save, "ChildMeasureBody") || !strings.Contains(save, "w.Ids.refAtHit(") {
		t.Fatal("single-child table field does not reuse MeasureBody through refAtHit")
	}
	for _, old := range []string{"w.Header(", "w.PutLeb(", "w.Ids.RefAt("} {
		if strings.Contains(save, old) {
			t.Fatalf("RootSaveBody still calls %s", old)
		}
	}
	runGenerated(t, headerSchema, headerWireTest)
}

const headerWireTest = `package probe
import ("bytes"; "testing")

func TestHeaderMatchesPutLebPut8(t *testing.T) {
	kinds := []uint8{1, 4, 14, 32}
	refs := []uint64{0, 1, 127, 128, 255, 16384}
	for _, ref := range refs {
		for _, kind := range kinds {
			got := headerBytes(t, ref, kind, false)
			want := lebThenKind(t, ref, kind, false)
			if !bytes.Equal(got, want) {
				t.Fatalf("ref %d kind %d\n  want %x\n   got %x", ref, kind, want, got)
			}
			if headerBytes(t, ref, kind, true) == nil || lebThenKind(t, ref, kind, true) == nil {
				t.Fatalf("measuring ref %d kind %d overflowed", ref, kind)
			}
			if len(headerBytes(t, ref, kind, true)) != len(want) {
				t.Fatalf("measuring length ref %d kind %d", ref, kind)
			}
		}
	}

	var ids TableIds
	short := make([]byte, 1)
	w := TableWriter{Buffer: short, Ids: &ids}
	w.Header(1, 4)
	if !w.Overflow || w.Offset != 0 {
		t.Fatalf("one-byte buffer: overflow=%v offset=%d", w.Overflow, w.Offset)
	}
}

func TestPut128MatchesTwoPut64(t *testing.T) {
	pairs := [][2]uint64{{0, 0}, {1, 0}, {0, 1}, {0xffffffffffffffff, 0x8000000000000000}}
	for _, p := range pairs {
		var ids TableIds
		a := make([]byte, 32)
		wa := TableWriter{Buffer: a, Ids: &ids}
		wa.Put128(p[0], p[1])
		var ids2 TableIds
		b := make([]byte, 32)
		wb := TableWriter{Buffer: b, Ids: &ids2}
		wb.Put64(p[0])
		wb.Put64(p[1])
		if wa.Overflow || wb.Overflow || wa.Offset != 16 || !bytes.Equal(a[:16], b[:16]) {
			t.Fatalf("put128 %x/%x\n  want %x\n   got %x overflow %v/%v", p[0], p[1], b[:16], a[:wa.Offset], wa.Overflow, wb.Overflow)
		}
	}
	var ids TableIds
	short := make([]byte, 15)
	w := TableWriter{Buffer: short, Ids: &ids}
	w.Put128(1, 2)
	if !w.Overflow || w.Offset != 0 {
		t.Fatalf("15-byte buffer: overflow=%v offset=%d", w.Overflow, w.Offset)
	}
}

func TestHeaderSaveRoundTrip(t *testing.T) {
	var value, loaded Root
	value.A = 1
	value.B = 2
	value.C = 3
	value.Child.N = 5
	value.Choice.Type = ChoiceTypeLeaf
	value.Choice.Leaf.N = 9
	value.Wide.Lo = 7
	value.Wide.Hi = 11
	value.PayloadLength = 4
	copy(value.Payload[:], []byte("abcd"))
	n := RootMeasure(&value)
	if n < 0 {
		t.Fatal("measure")
	}
	wire := make([]byte, n)
	if RootSave(&value, wire) != n {
		t.Fatal("save")
	}
	var report TableReport
	if !RootLoad(&loaded, wire, &report) || report != (TableReport{}) || loaded.A != 1 || loaded.B != 2 || loaded.C != 3 || loaded.Child.N != 5 || loaded.Choice.Leaf.N != 9 || loaded.Wide.Lo != 7 || loaded.Wide.Hi != 11 || loaded.PayloadLength != 4 || !bytes.Equal(loaded.Payload[:4], []byte("abcd")) {
		t.Fatalf("round trip %+v loaded=%+v", report, loaded)
	}
	if RootSave(&value, make([]byte, n-1)) != -1 {
		t.Fatal("short buffer accepted")
	}
	if n := testing.AllocsPerRun(50, func() {
		if RootMeasure(&value) != int64(len(wire)) || RootSave(&value, wire) != int64(len(wire)) {
			panic("round trip")
		}
	}); n != 0 {
		t.Fatalf("Header allocated %v", n)
	}
}

func headerBytes(t *testing.T, ref uint64, kind uint8, measuring bool) []byte {
	t.Helper()
	var ids TableIds
	buf := make([]byte, 32)
	w := TableWriter{Buffer: buf, Ids: &ids, Measuring: measuring}
	w.Header(ref, kind)
	if w.Overflow {
		return nil
	}
	out := make([]byte, w.Offset)
	if !measuring {
		copy(out, buf[:w.Offset])
	}
	return out
}

func lebThenKind(t *testing.T, ref uint64, kind uint8, measuring bool) []byte {
	t.Helper()
	var ids TableIds
	buf := make([]byte, 32)
	w := TableWriter{Buffer: buf, Ids: &ids, Measuring: measuring}
	w.PutLeb(ref)
	w.Put8(kind)
	if w.Overflow {
		return nil
	}
	out := make([]byte, w.Offset)
	if !measuring {
		copy(out, buf[:w.Offset])
	}
	return out
}
`
