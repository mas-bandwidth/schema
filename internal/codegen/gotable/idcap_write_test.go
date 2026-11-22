package gotable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE WRITER REFUSES AN ID TABLE ITS READERS REFUSE (docs/SPEC-TABLES.md §3,
// ruling R8). Every reader answers wire_damaged for an id table of more than
// 4096 entries, so Measure and Save refuse a value that needs 4097 and write
// the one that needs exactly 4096, which every reader accepts. The unit is
// tablewire.IdCapSchema: an enum of 4094 variants keying an array, and two
// more fields, so the three values need 4095, 4096 and 4097 ids.
//
// The test goes red when the cap is taken out of the writer: the 4097 value is
// then written and the strict read of it answers wire_damaged.
func TestGoWriterRefusesMoreThan4096Ids(t *testing.T) {
	t.Parallel()
	runGenerated(t, tablewire.IdCapSchema(), `package idcap

import "testing"

func idCapValue(x, y uint8) *Root {
	v := &Root{X: x, Y: y}
	for i := range v.Keys {
		v.Keys[i] = 1
	}
	return v
}

func idCapCount(buf []byte) int {
	n := 0
	for i := 0; i < 8; i++ {
		n |= int(buf[len(buf)-8+i]) << (8 * i)
	}
	return n
}

func TestIdCap(t *testing.T) {
	for _, row := range []struct {
		x, y uint8
		ids  int
	}{{0, 0, 4095}, {1, 0, 4096}} {
		v := idCapValue(row.x, row.y)
		n := RootMeasure(v)
		if n <= 0 {
			t.Fatalf("%d ids: measure refused", row.ids)
		}
		buf := make([]byte, n)
		if w := RootSave(v, buf); w != n {
			t.Fatalf("%d ids: save wrote %d of %d", row.ids, w, n)
		}
		if got := idCapCount(buf); got != row.ids {
			t.Fatalf("id count %d, want %d", got, row.ids)
		}
		if _, err := RootReadStrict(buf); err != nil {
			t.Fatalf("%d ids: the strict read refused the file: %v", row.ids, err)
		}
	}
	// 4097 ids: the writer refuses, in Measure and in Save, and writes nothing.
	over := idCapValue(1, 1)
	if n := RootMeasure(over); n >= 0 {
		t.Fatalf("4097 ids: measure answered %d, want a refusal", n)
	}
	buf := make([]byte, 1<<20)
	if w := RootSave(over, buf); w >= 0 {
		t.Fatalf("4097 ids: save wrote %d bytes, want a refusal", w)
	}
}
`)
}
