package tablewire_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestFileRegionMeasureUnboundedTextMinimal(t *testing.T) {
	m := listModel(t, `package probe
table T {
    a [..2]string
}
`)
	root := m.Lookup("T")

	// Empty element "" takes no extent blob: 24-byte root record + 16-byte directory = 40.
	emptyWire, err := tablewire.Encode(m, place(t, m, "T", `{"a":[""]}`))
	if err != nil {
		t.Fatal(err)
	}
	bytes, whole, refused := tablewire.FileRegionMeasure(m.Unit, root, emptyWire)
	if !whole || refused || bytes != 40 {
		t.Fatalf("empty element measure = (%d, whole=%v, refused=%v), want (40, true, false)", bytes, whole, refused)
	}

	// Single "x" element carves round8(9 + 1) = 16 extent bytes: 24 + 16 + 16 = 56.
	oneWire, err := tablewire.Encode(m, place(t, m, "T", `{"a":["x"]}`))
	if err != nil {
		t.Fatal(err)
	}
	bytes, whole, refused = tablewire.FileRegionMeasure(m.Unit, root, oneWire)
	if !whole || refused || bytes != 56 {
		t.Fatalf("single element measure = (%d, whole=%v, refused=%v), want (56, true, false)", bytes, whole, refused)
	}
}

func TestFileRegionMeasureUnboundedTextMatchesCook(t *testing.T) {
	m := listModel(t, `package probe
table Note {
    lines []string
}
table Where {
    capped [..3]string
    maybe_capped [..3]?string
    open []string
    maybe_open []?string
    by_name map[string]int32
    note *Note
}
`)
	root := m.Lookup("Where")
	longA := strings.Repeat("prefix-", 40) + "alpha"
	longB := strings.Repeat("prefix-", 40) + "beta"
	js := `{"capped":["","x","` + longA + `"],` +
		`"maybe_capped":[null,"","y"],` +
		`"open":["hello","","` + longB + `"],` +
		`"maybe_open":[null,"world",""],` +
		`"by_name":{"":1,"` + longB + `":2,"` + longA + `":3}}`
	inst := place(t, m, "Where", js)
	note := m.New(m.Lookup("Note"))
	var r tabletext.Report
	if !m.Read(note, []byte(`{"lines":["child-one","","child-two"]}`), &r) {
		t.Fatal(r)
	}
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == "note" {
			inst.Fields[i].Cell.Node = note
		}
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	measured, whole, refused := tablewire.FileRegionMeasure(m.Unit, root, wire)
	if !whole || refused {
		t.Fatalf("FileRegionMeasure: bytes=%d whole=%v refused=%v", measured, whole, refused)
	}
	decoded := m.New(root)
	if ok, err := tablewire.Decode(m, decoded, wire, &r); !ok || err != nil || !r.Silent() {
		t.Fatalf("Decode: ok=%v err=%v report=%+v", ok, err, r)
	}
	cooked, err := tablecook.Cook(m, decoded, tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := int64(len(cooked)) - tablecook.HeaderBytes
	if measured != want {
		t.Fatalf("FileRegionMeasure = %d, want cooked data+attrib %d", measured, want)
	}
}
