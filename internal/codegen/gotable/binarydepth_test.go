package gotable

import "testing"

// THE BINARY NESTING BOUND ON THE GENERATED READ (docs/SPEC-TABLES.md §4,
// binary_max_depth): inside an unknown field, a framed payload at level 128
// (a table body, or an array body) reads and one at level 129 refuses, on the
// region load, the builder load, ReadStrict and the retaining load.
const goBinaryDepthTest = oneReadPrelude + `
func nested(ids *TableIds, levels int, array bool) []byte {
	unknown := uint64(0x1234567890abcdef)
	inner, kind := []byte{0}, uint8(13)
	if array {
		inner, kind = []byte{4, 1, 7, 0, 0, 0}, 14
	}
	for level := levels - 1; level >= 1; level-- {
		inner = body(ids, func(w *TableWriter) { framed(w, unknown, kind, inner); w.Put8(0) })
		kind = 13
	}
	return file(ids, func(w *TableWriter) { framed(w, unknown, kind, inner) })
}

func unions(ids *TableIds, n int, inner []byte) []byte {
	payload := inner
	for range n {
		w := TableWriter{Buffer: make([]byte, len(payload)+16), Ids: ids}
		w.Put8(1)
		w.Put8(15)
		w.PutLeb(uint64(len(payload)))
		w.Raw(payload)
		payload = w.Buffer[:w.Offset]
	}
	return file(ids, func(w *TableWriter) { w.ID(0x1234567890abcdef); w.Put8(15); w.Raw(payload) })
}

func TestUnionDepth(t *testing.T) {
	var ids TableIds
	scalar := []byte{1, 4, 4, 7, 0, 0, 0}
	table := []byte{1, 13, 1, 0}
	for _, c := range []struct {
		name string
		wire []byte
		want bool
	}{
		{"128 unions around None", unions(&ids, 128, []byte{0}), true},
		{"129 unions around None", unions(&ids, 129, []byte{0}), false},
		{"a scalar arm under 128 unions", unions(&ids, 128, scalar), true},
		{"a table arm under 127 unions", unions(&ids, 127, table), false},
		{"a table arm under 126 unions", unions(&ids, 126, table), true},
	} {
		if verdict(t, c.name, c.wire) != c.want {
			t.Fatalf("%s: the read answered %v", c.name, !c.want)
		}
		retain := TableRetain{Bytes: make([]byte, 1<<16), Ids: make([]TableRetainID, 64)}
		var r TableReport
		got := NodeLoadRetain(region(1<<16), c.wire, &retain, &r)
		if (got != nil) != c.want {
			t.Fatalf("%s: the retaining load answered %v %+v", c.name, got != nil, r)
		}
		if got != nil && r.Retained != 1 {
			t.Fatalf("%s: the retaining load kept %d records: %+v", c.name, r.Retained, r)
		}
	}
}

func TestBinaryDepth(t *testing.T) {
	_ = rand.Intn
	var ids TableIds
	for _, array := range []bool{false, true} {
		for _, levels := range []int{128, 129} {
			wire := nested(&ids, levels, array)
			want := levels <= 128
			if verdict(t, "nested", wire) != want {
				t.Fatalf("array=%v, a framed payload at level %d: the read answered %v", array, levels, !want)
			}
			retain := TableRetain{Bytes: make([]byte, 1<<16), Ids: make([]TableRetainID, 64)}
			var r TableReport
			if got := NodeLoadRetain(region(1<<16), wire, &retain, &r) != nil; got != want {
				t.Fatalf("array=%v, a framed payload at level %d: the retaining load answered %v %+v", array, levels, got, r)
			}
		}
	}
}
`

func TestGoBinaryDepthBoundEveryPath(t *testing.T) {
	t.Parallel()
	runGenerated(t, oneReadSchema, goBinaryDepthTest)
}
