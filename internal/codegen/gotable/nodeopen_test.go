package gotable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestTableOpenHasNoWirePrepass is the red the card names: the open must not
// run tableWireVerify over the whole file before the body read, because the
// body read is the one walk that holds the wire-alone conditions
// (docs/SPEC-TABLES.md §4, §6.5). A node table is verified by tableWireVerify
// in the region load, the reader that carries it, so the plain open has
// nothing left to prepass.
func TestTableOpenHasNoWirePrepass(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("wirecheck.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	at := strings.Index(text, "func tableOpenChecked(")
	if at < 0 {
		t.Fatal("tableOpenChecked is not in wirecheck.go")
	}
	body := text[at:]
	if end := strings.Index(body, "\n}\n"); end >= 0 {
		body = body[:end]
	}
	if strings.Contains(body, "tableWireVerify") {
		t.Fatal("tableOpenChecked still runs the separate tableWireVerify prepass")
	}
}

const nodeWalkPrelude = `package probe
import ("testing"; "unsafe")

func region(n int64) []byte {
	raw := make([]byte, n+64)
	off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
	return raw[off : off+uintptr(n)]
}

func body(ids *TableIds, f func(w *TableWriter)) []byte {
	w := TableWriter{Buffer: make([]byte, 4096), Ids: ids}
	f(&w)
	if w.Overflow { panic("overflow") }
	return w.Buffer[:w.Offset]
}

func field(i int) uint64 { return NodeTableFields[i].ID }
const (fN, fLeaf = 0, 2)

func ptr(w *TableWriter, id uint64, index uint64) { w.ID(id); w.Put8(17); w.PutLeb(index) }
func framed(w *TableWriter, id uint64, kind uint8, payload []byte) { w.ID(id); w.Put8(kind); w.PutLeb(uint64(len(payload))); w.Raw(payload) }

type record struct { typ uint64; body []byte }

func file(ids *TableIds, root func(w *TableWriter), records ...record) []byte {
	w := TableWriter{Buffer: make([]byte, 8192), Ids: ids}
	w.Put8(1)
	root(&w)
	if len(records) > 0 {
		payload := body(ids, func(p *TableWriter) {
			p.PutLeb(uint64(len(records)))
			for _, r := range records { p.ID(r.typ); p.PutLeb(uint64(len(r.body))); p.Raw(r.body) }
		})
		framed(&w, 0xffffffffffffffff, 12, payload)
	}
	w.Put8(0)
	w.Trailer()
	if w.Overflow || ids.Overflow { panic("overflow") }
	return w.Buffer[:w.Offset]
}

func leaf(ids *TableIds, v int32) record {
	return record{LeafTableType().ID, body(ids, func(w *TableWriter) { w.ID(LeafTableFields[0].ID); w.Put8(4); w.Put32(uint32(v)); w.Put8(0) })}
}
`

const nodeWalkConditions = `
func TestNodeTableInTheBodyWalk(t *testing.T) {
	var ids TableIds
	// a node-table file is read and verified by the region load's one walk
	good := file(&ids, func(w *TableWriter) { ptr(w, field(fLeaf), 2) }, leaf(&ids, 3))
	if NodeLoad(region(max(NodeLoadMeasure(good), 1)), good, nil) == nil {
		t.Fatal("the valid node-table file was refused")
	}
	// a record no pointer reaches is refused by the same walk
	bad := file(&ids, func(w *TableWriter) {}, leaf(&ids, 3))
	if NodeLoad(region(1<<16), bad, nil) != nil {
		t.Fatal("a record no pointer reaches was accepted")
	}
	// a fixed root refuses the reserved node-table id in its own body walk,
	// with no partial value
	var report TableReport
	var flat Flat
	if FlatLoad(&flat, good, &report) || report.Err() == nil {
		t.Fatalf("a fixed root accepted a node table: %+v", report)
	}
	if flat.A != 0 {
		t.Fatalf("a refused load left a partial value: %+v", flat)
	}
}
`

// runGeneratedTables builds only the table surfaces, so the unit needs no
// sibling serialize.go checkout: the wire, fixed and text forms this test reads
// are the compiler's own table packages.
func runGeneratedTables(t *testing.T, schema, testSource string) {
	t.Helper()
	u := unitFrom(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	files["go.mod"] = []byte("module probe\n\ngo 1.26\n")
	files["values_test.go"] = []byte(testSource)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-timeout=600s", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated table unit: %v\n%s", err, out)
	}
}

func TestGoNodeTableIsVerifiedInTheBodyWalk(t *testing.T) {
	t.Parallel()
	runGeneratedTables(t, oneReadSchema+"\nfixed table Flat { a int32 = 0 }\n", nodeWalkPrelude+nodeWalkConditions)
}
