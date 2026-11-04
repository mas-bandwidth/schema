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
