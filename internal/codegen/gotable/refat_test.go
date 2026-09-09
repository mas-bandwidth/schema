package gotable

import (
	"strings"
	"testing"
)

const refAtSchema = `package probe
type Child {
 x uint32 = 0
}
table Root {
 n uint32
 children [..4]Child
 payload bytes(20000)
}
`

func TestRefAtShape(t *testing.T) {
	files := generate(t, refAtSchema)
	body := ""
	for name, data := range files {
		if strings.HasSuffix(name, "Table.go") {
			body += string(data)
		}
	}
	for _, want := range []string{
		"[tableIdCapacity]uint32",
		"ordinalOf [tableIdCapacity]int32",
		"func (ids *TableIds) RefAt(ordinal int, id uint64)",
		"func (w *TableWriter) IdAt(ordinal int, id uint64)",
		"ids.slot[o] = 0",
		"w.Header(w.Ids.RefAt(",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("RefAt is missing %q", want)
		}
	}
	if !strings.Contains(body, "w.Ids.RefAt(") && !strings.Contains(body, ".RefAt(") {
		t.Fatal("generated Save never calls RefAt")
	}
	runGenerated(t, refAtSchema, refAtWireTest)
}

// A field named alpha and enum variant alpha share TableWireId("alpha"). The
// keyed array interns the key through Ref before measuring the element, so
// the element's RefAt on that id is a chain hit. Recording the slot only on
// an intern miss leaves truncate nothing to clear (schema #775).
const refAtSharedIdSchema = `package probe
enum Tag { alpha, beta }
type Leaf { alpha int32 }
table Root {
 t Tag
 banks [Tag]Leaf
 n int32
}
`

func TestRefAtSharedFieldAndEnumKeyId(t *testing.T) {
	runGenerated(t, refAtSharedIdSchema, refAtSharedIdTest)
}

func TestEnumWriterUsesRefAt(t *testing.T) {
	files := generate(t, refAtSharedIdSchema)
	body := ""
	for name, data := range files {
		if strings.HasSuffix(name, "Table.go") {
			body += string(data)
		}
	}
	save := body
	if i := strings.Index(save, "func RootSaveBody"); i >= 0 {
		save = save[i:]
		if j := strings.Index(save[1:], "\nfunc "); j >= 0 {
			save = save[:j+1]
		}
	}
	if !strings.Contains(save, "w.IdAt(") {
		t.Fatal("enum field writer does not intern through IdAt")
	}
	if strings.Contains(save, "TableEnumId()") && strings.Contains(save, ".Id(id)") {
		t.Fatal("enum field writer still hashes through TableEnumId then Id")
	}
}

const refAtWireTest = `package probe
import ("bytes"; "encoding/binary"; "testing")

func TestRefAtFirstUseAndElision(t *testing.T) {
	var value, loaded Root
	value.N = 9
	value.ChildrenCount = 2
	value.Children[1].X = 3
	n := RootMeasure(&value)
	if n < 0 {
		t.Fatal("measure")
	}
	wire := make([]byte, n)
	if RootSave(&value, wire) != n {
		t.Fatal("save")
	}
	var report TableReport
	if !RootLoad(&loaded, wire, &report) || report != (TableReport{}) || loaded.N != 9 || loaded.ChildrenCount != 2 || loaded.Children[0].X != 0 || loaded.Children[1].X != 3 {
		t.Fatalf("round trip %+v loaded=%+v", report, loaded)
	}

	var again Root
	again.N = 9
	again.ChildrenCount = 2
	again.Children[1].X = 3
	second := make([]byte, n)
	if RootSave(&again, second) != n || !bytes.Equal(wire, second) {
		t.Fatal("second save differed")
	}

	ids := trailerIds(wire)
	if len(ids) < 1 || ids[0] != RootTableFields[0].Id {
		t.Fatalf("first-use order: %x want n first", ids)
	}

	var empty Root
	empty.N = 1
	m := RootMeasure(&empty)
	short := make([]byte, m)
	if RootSave(&empty, short) != m {
		t.Fatal("empty children save")
	}
	if got := trailerIds(short); len(got) != 1 || got[0] != RootTableFields[0].Id {
		t.Fatalf("elided children leaked ids: %x", got)
	}
}

func trailerIds(wire []byte) []uint64 {
	count := binary.LittleEndian.Uint64(wire[len(wire)-8:])
	out := make([]uint64, count)
	start := len(wire) - 8 - int(count)*8
	for i := range out {
		out[i] = binary.LittleEndian.Uint64(wire[start+i*8 : start+i*8+8])
	}
	return out
}
`

const refAtSharedIdTest = `package probe
import ("encoding/binary"; "testing")

func TestSharedIdRoundTrip(t *testing.T) {
	var value, loaded Root
	value.Banks[0].Alpha = 7
	value.N = 1
	n := RootMeasure(&value)
	if n < 0 {
		t.Fatal("measure")
	}
	wire := make([]byte, n)
	if RootSave(&value, wire) != n {
		t.Fatal("save")
	}
	var report TableReport
	if !RootLoad(&loaded, wire, &report) || report.Malformed || loaded.Banks[0].Alpha != 7 || loaded.N != 1 {
		t.Fatalf("alpha field gone: n=%d loaded.alpha=%d report=%+v", n, loaded.Banks[0].Alpha, report)
	}
	count := binary.LittleEndian.Uint64(wire[len(wire)-8:])
	hasAlpha := false
	start := len(wire) - 8 - int(count)*8
	if start < 0 || start > len(wire) {
		t.Fatalf("trailer start %d count %d len %d", start, count, len(wire))
	}
	for i := 0; i < int(count); i++ {
		id := binary.LittleEndian.Uint64(wire[start+i*8:])
		if id == LeafTableFields[0].Id {
			hasAlpha = true
		}
	}
	if !hasAlpha {
		t.Fatalf("alpha id missing from trailer, %d bytes, %d ids", n, count)
	}
}
`
