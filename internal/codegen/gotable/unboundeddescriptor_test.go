package gotable

import "testing"

// TestGoUnboundedDescriptorIsExplicit holds docs/SPEC-TABLES.md §8.1 in the Go
// backend: a map, an unbounded []T and a text list []string(M) carry
// Unbounded true and ArrayBound 0 in their descriptor, while a fixed [..N]T
// carries Unbounded false and its bound. A round trip of 100000 map entries
// and list elements through binary, base64 and JSON still reads every entry.
func TestGoUnboundedDescriptorIsExplicit(t *testing.T) {
	t.Parallel()
	runGenerated(t, unboundedDescriptorSchema, unboundedDescriptorProbe)
}

const unboundedDescriptorSchema = `package probe
table Root
{
    m  map[int32]int32
    l  []int32
    tl []string(8)
    f  [..4]int32
}
`

const unboundedDescriptorProbe = `package probe
import (
    "testing"
    "unsafe"
)

// descriptor reads the field named want from RootTableFields and returns its
// Unbounded and ArrayBound columns.
func descriptor(t *testing.T, want string) (bool, int32) {
    t.Helper()
    for i := range RootTableFields {
        f := &RootTableFields[i]
        if f.Name == want {
            return f.Unbounded, f.ArrayBound
        }
    }
    t.Fatalf("no field %q in RootTableFields", want)
    return false, 0
}

// checkEvery verifies every one of the 100000 map entries, list elements and
// text-list elements of root reads back correct.
func checkEvery(t *testing.T, what string, root *Root, arena ...*TableArena) {
    t.Helper()
    const n = 100000
    if root.M.Count != n || root.L.Count != n {
        t.Fatalf("%s: counts map=%d list=%d want %d", what, root.M.Count, root.L.Count, n)
    }
    if root.Tl.Count != n/1000 {
        t.Fatalf("%s: text-list count %d want %d", what, root.Tl.Count, n/1000)
    }
    for i := int32(0); i < n; i++ {
        v := RootMFind(nil, &root.M, i)
        if v == nil || *v != i*7 {
            t.Fatalf("%s: map[%d]=%v want %d", what, i, v, i*7)
        }
        e := root.L.At(i)
        if e == nil || *e != i {
            t.Fatalf("%s: list[%d]=%v want %d", what, i, e, i)
        }
    }
    for i := int32(0); i < root.Tl.Count; i++ {
        p, ok := RootTlTextAt(root, i)
        if !ok || string(p) != "x" {
            t.Fatalf("%s: text-list[%d]=%q ok=%v want x", what, i, p, ok)
        }
    }
}

func TestGoUnboundedDescriptorIsExplicit(t *testing.T) {
    // an unbounded map reports Unbounded true and ArrayBound 0
    if u, b := descriptor(t, "m"); !u || b != 0 {
        t.Fatalf("map: Unbounded=%v ArrayBound=%d want true 0", u, b)
    }
    // an unbounded []T reports Unbounded true and ArrayBound 0
    if u, b := descriptor(t, "l"); !u || b != 0 {
        t.Fatalf("list: Unbounded=%v ArrayBound=%d want true 0", u, b)
    }
    // a text list []string(8) reports Unbounded true and ArrayBound 0
    if u, b := descriptor(t, "tl"); !u || b != 0 {
        t.Fatalf("text list: Unbounded=%v ArrayBound=%d want true 0", u, b)
    }
    // a fixed [..4]T reports Unbounded false and ArrayBound 4
    if u, b := descriptor(t, "f"); u || b != 4 {
        t.Fatalf("fixed: Unbounded=%v ArrayBound=%d want false 4", u, b)
    }

    // round trip of 100000 map entries and list elements
    const n = 100000
    var b RootBuilder
    b.Init()
    defer b.Shutdown()
    r := b.GetRoot()
    for i := int32(0); i < n; i++ {
        *RootMInsert(&b.Main, &r.M, i) = i * 7
        *RootLAdd(&b.Main, &r.L) = i
        if i%1000 == 0 {
            RootTlAppend(&b.Main, r, "x")
        }
    }

    // BINARY round trip
    wire := make([]byte, RootMeasure(r, &b.Arena))
    if RootSave(r, wire, &b.Arena) != int64(len(wire)) {
        t.Fatal("save")
    }
    size := RootLoadMeasure(wire)
    if size < 0 {
        t.Fatal("load measure")
    }
    raw := make([]byte, size+16)
    off := (-uintptr(unsafe.Pointer(&raw[0]))) & 15
    region := raw[off : off+uintptr(size)]
    var report TableReport
    got := RootLoad(region, wire, &report)
    if got == nil {
        t.Fatalf("load failed: %+v", report)
    }
    checkEvery(t, "binary", got)

    // BASE64 round trip through the text carrier (SaveText is base64 of Save)
    text, err := RootSaveText(r, &b.Arena)
    if err != nil {
        t.Fatalf("save text: %v", err)
    }
    gotB, err := RootLoadText(text, nil)
    if err != nil || gotB == nil {
        t.Fatalf("load text: %v", err)
    }
    checkEvery(t, "base64", gotB)

    // JSON round trip
    jtext := make([]byte, RootToJsonMeasure(gotB))
    if RootToJson(gotB, jtext) != int64(len(jtext)) {
        t.Fatal("json write")
    }
    var edit RootBuilder
    edit.Init()
    defer edit.Shutdown()
    report = TableReport{}
    if !RootFromJson(&edit, jtext, &report) {
        t.Fatalf("json read failed: %+v", report)
    }
    if !edit.Lock() {
        t.Fatal("json lock")
    }
    checkEvery(t, "json", edit.AsConst())
}
`
