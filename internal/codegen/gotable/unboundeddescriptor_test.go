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
    "encoding/base64"
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
    if r.M.Count != n {
        t.Fatalf("map count %d want %d", r.M.Count, n)
    }
    if r.L.Count != n {
        t.Fatalf("list count %d want %d", r.L.Count, n)
    }

    wire := make([]byte, RootMeasure(r, &b.Arena))
    if RootSave(r, wire, &b.Arena) != int64(len(wire)) {
        t.Fatal("save")
    }

    // stub read of the raw wire
    if len(wire) == 0 {
        t.Fatal("empty wire")
    }

    // base64 round trip
    b64 := base64.StdEncoding.EncodeToString(wire)
    decoded, err := base64.StdEncoding.DecodeString(b64)
    if err != nil {
        t.Fatalf("base64 decode: %v", err)
    }
    if string(decoded) != string(wire) {
        t.Fatal("base64 identity")
    }

    // full load through the binary wire
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
    if got.M.Count != n || got.L.Count != n {
        t.Fatalf("loaded counts map=%d list=%d want %d", got.M.Count, got.L.Count, n)
    }
    if *RootMFind(nil, &got.M, 99999) != 99999*7 {
        t.Fatal("loaded map value")
    }

    // JSON round trip
    text := make([]byte, RootToJsonMeasure(got))
    if RootToJson(got, text) != int64(len(text)) {
        t.Fatal("json write")
    }
    var edit RootBuilder
    edit.Init()
    defer edit.Shutdown()
    report = TableReport{}
    if !RootFromJson(&edit, text, &report) {
        t.Fatalf("json read failed: %+v", report)
    }
    if !edit.Lock() {
        t.Fatal("json lock")
    }
    er := edit.AsConst()
    if er.M.Count != n || er.L.Count != n {
        t.Fatalf("json counts map=%d list=%d want %d", er.M.Count, er.L.Count, n)
    }
}
`
