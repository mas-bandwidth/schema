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
		"func (ids *TableIds) RefAt(ordinal int, id uint64)",
		"func (w *TableWriter) IdAt(ordinal int, id uint64)",
		"ids.slot[i] = 0",
		"w.IdAt(",
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
