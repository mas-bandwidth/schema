package gotable

import "testing"

// TestGoWriteOfUnlockedBuilderWithoutArenaRefuses pins docs/SPEC-TABLES.md
// §6.3: while a builder is mutable its reference slots hold arena offsets, so
// a write handed no arena cannot resolve one. Every writer refuses by name
// (arena_required), and writes nothing, rather than reading an offset as a
// region delta and writing a silent zero. The arena, and the locked region
// whose references are already deltas, write the same 104-byte wire, and a
// table with no reference is unchanged.
func TestGoWriteOfUnlockedBuilderWithoutArenaRefuses(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
table Root {
    n int32
    s *string
    xs []int32
    names []string(8)
}
table Plain {
    n int32
}
`, `package probe
import (
	"bytes"
	"errors"
	"testing"
	"unsafe"
)

func filled(t *testing.T) *RootBuilder {
	t.Helper()
	b := new(RootBuilder)
	if !b.Init() {
		t.Fatal("builder allocation failed")
	}
	var report TableReport
	if !RootFromJson(b, []byte("{\"n\":7,\"s\":\"hello\",\"xs\":[1,2,3],\"names\":[\"a\",\"b\"]}"), &report) || report != (TableReport{}) {
		b.Shutdown()
		t.Fatalf("RootFromJson: %+v", report)
	}
	return b
}

func reason(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, TableRefuseArenaRequired) {
		t.Fatalf("%s must refuse arena_required, got %v", what, err)
	}
}

func zero(t *testing.T, what string, b []byte) {
	t.Helper()
	for i, c := range b {
		if c != 0 {
			t.Fatalf("%s wrote byte %d (%#x); a refused write writes nothing", what, i, c)
		}
	}
}

func TestUnlockedRefuses(t *testing.T) {
	b := filled(t)
	defer b.Shutdown()
	root := b.GetRoot()

	if n := RootMeasure(root); n != -1 {
		t.Fatalf("Measure wrote %d with no arena", n)
	}
	if n, err := RootMeasureReason(root); n != -1 {
		t.Fatalf("MeasureReason measured %d with no arena", n)
	} else {
		reason(t, "MeasureReason", err)
	}

	buf := make([]byte, 4096)
	if n := RootSave(root, buf); n != -1 {
		t.Fatalf("Save wrote %d with no arena", n)
	}
	zero(t, "Save", buf)
	if s, err := RootSaveText(root); s != "" {
		t.Fatalf("SaveText wrote %q with no arena", s)
	} else {
		reason(t, "SaveText", err)
	}

	json := make([]byte, 4096)
	if n := RootToJsonMeasure(root); n != -1 {
		t.Fatalf("ToJsonMeasure wrote %d with no arena", n)
	}
	if n := RootToJson(root, json); n != -1 {
		t.Fatalf("ToJson wrote %d with no arena", n)
	}
	zero(t, "ToJson", json)

	if n := RootCookMeasure(root); n != -1 {
		t.Fatalf("CookMeasure wrote %d with no arena", n)
	}
	if n, err := RootCookMeasureReason(root); n != -1 {
		t.Fatalf("CookMeasureReason wrote %d with no arena", n)
	} else {
		reason(t, "CookMeasureReason", err)
	}

	var retain TableRetain
	if n := RootMeasureRetain(root, &retain); n != -1 {
		t.Fatalf("MeasureRetain wrote %d with no arena", n)
	}
	rbuf := make([]byte, 4096)
	var report TableReport
	if n := RootSaveRetain(root, &retain, rbuf, &report); n != -1 {
		t.Fatalf("SaveRetain wrote %d with no arena", n)
	}
	zero(t, "SaveRetain", rbuf)

	mw := &TableMessageWriter{TableBitWriter: TableBitWriter{Buffer: make([]byte, 4096)}}
	if tableMessageRegionWrite(mw, unsafe.Pointer(root), RootTableType(), nil) {
		t.Fatal("a message write accepted an unlocked builder with no arena")
	}
}

func TestArenaAndLockedWrite(t *testing.T) {
	b := filled(t)
	defer b.Shutdown()
	root := b.GetRoot()
	if n := RootMeasure(root, &b.Arena); n != 104 {
		t.Fatalf("Measure with the arena = %d, want 104", n)
	}
	wire := make([]byte, 104)
	if n := RootSave(root, wire, &b.Arena); n != 104 {
		t.Fatalf("Save with the arena = %d, want 104", n)
	}
	got, err := RootReadStrict(wire)
	if err != nil || got.N != 7 || got.S == 0 || string(TableStringAt(&got.S).Data) != "hello" {
		t.Fatalf("the arena wire did not reload: %+v %v", got, err)
	}
	if text, err := RootSaveText(root, &b.Arena); err != nil || text == "" {
		t.Fatalf("SaveText with the arena: %q %v", text, err)
	}
	if n := RootCookMeasure(root, &b.Arena); n <= 0 {
		t.Fatalf("CookMeasure with the arena = %d", n)
	}
	mw := &TableMessageWriter{TableBitWriter: TableBitWriter{Buffer: make([]byte, 4096)}}
	if !tableMessageRegionWrite(mw, unsafe.Pointer(root), RootTableType(), &b.Arena) {
		t.Fatal("the message write refused an arena it was given")
	}

	if !b.Lock() {
		t.Fatal("Lock failed")
	}
	constRoot := b.AsConst()
	if n := RootMeasure(constRoot); n != 104 {
		t.Fatalf("Measure of the locked region = %d, want 104", n)
	}
	locked := make([]byte, 104)
	if n := RootSave(constRoot, locked); n != 104 {
		t.Fatalf("Save of the locked region = %d, want 104", n)
	}
	if !bytes.Equal(wire, locked) {
		t.Fatal("the locked region must write the same wire the arena did")
	}
	got, err = RootReadStrict(locked)
	if err != nil || got.N != 7 || got.S == 0 || *got.Xs.At(0) != 1 || *got.Xs.At(1) != 2 || *got.Xs.At(2) != 3 || func() bool { v, ok := RootNamesTextAt(got, 1); return !ok || string(v) != "b" }() {
		t.Fatalf("the locked wire did not reload: %+v %v", got, err)
	}
	// the text and retaining writers resolve the region's own deltas
	if n := RootToJsonMeasure(constRoot); n <= 0 {
		t.Fatalf("ToJsonMeasure of the locked region = %d", n)
	}
	jtext := make([]byte, 4096)
	if n := RootToJson(constRoot, jtext); n <= 0 || !bytes.Contains(jtext[:n], []byte("\"hello\"")) {
		t.Fatalf("ToJson of the locked region: %d %.80s", n, jtext)
	}
	var retain TableRetain
	if n := RootMeasureRetain(constRoot, &retain); n <= 0 {
		t.Fatalf("MeasureRetain of the locked region = %d", n)
	}
	var report TableReport
	if n := RootSaveRetain(constRoot, &retain, make([]byte, 4096), &report); n <= 0 {
		t.Fatalf("SaveRetain of the locked region = %d %+v", n, report)
	}
	mw = &TableMessageWriter{TableBitWriter: TableBitWriter{Buffer: make([]byte, 4096)}}
	if !tableMessageRegionWrite(mw, unsafe.Pointer(constRoot), RootTableType(), nil) {
		t.Fatal("the message write refused a locked region")
	}
}

func TestNoReferenceTableIsUnchanged(t *testing.T) {
	var b PlainBuilder
	if !b.Init() {
		t.Fatal("builder allocation failed")
	}
	defer b.Shutdown()
	b.GetRoot().N = 7
	n := PlainMeasure(b.GetRoot())
	if n <= 0 {
		t.Fatalf("a reference-free table must write unlocked with no arena, got %d", n)
	}
	wire := make([]byte, n)
	if PlainSave(b.GetRoot(), wire) != n {
		t.Fatalf("a reference-free table must save %d bytes", n)
	}
	var report TableReport
	got := PlainLoad(make([]byte, 4096), wire, &report)
	if got == nil || report != (TableReport{}) || got.N != 7 {
		t.Fatalf("the reference-free wire did not reload: %+v %+v", got, report)
	}
}
`)
}
