package gotable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestGoMessageFormEveryRoot asserts, over one generated unit, that the four
// message verbs — MeasureMessages, SaveMessages, LoadMessages and
// LoadRetainMessages — exist for a `fixed table` root AND for a plain `table`
// root. docs/SPEC-TABLES.md §3.3: the message form is EVERY table's; a fixed
// root rides it by value and a variable root over a region, as the C++ and C
// emitters carry it. The plain root's LoadRetainMessages is a real read, and
// the fixed root's is the named refusal (§6.6).
func TestGoMessageFormEveryRoot(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	schema := `package probe

table Loose { n int32 }
fixed table Tight { n int32 }
`
	files := generate(t, schema)
	// the verbs are the MESSAGE form's, in <Base>TableMessage.go, and a verb
	// refused by name is a constant of the core <Base>Table.go
	// (docs/SPEC-TABLES.md, "GO's optional forms"), so the unit's table output is read whole
	var table string
	for name, data := range files {
		if strings.HasSuffix(name, "Table.go") || strings.HasSuffix(name, "TableMessage.go") {
			table += string(data)
		}
	}
	if table == "" {
		t.Fatal("the unit emitted no table module")
	}
	for _, root := range []string{"Loose", "Tight"} {
		for _, verb := range []string{"MeasureMessages", "SaveMessages", "LoadMessages", "LoadRetainMessages"} {
			// every table root rides the message form (docs/SPEC-TABLES.md §3.3)
			if !strings.Contains(table, root+verb) {
				t.Errorf("the %s root lacks %s%s (docs/SPEC-TABLES.md §3.3: the message form is every table's)", root, root, verb)
			}
		}
	}
	compileTightCaller(t, files)
}

// compileTightCaller compiles and runs a caller that names both roots' four
// message verbs, proving they are real symbols and not mere source text, and
// that each root's batch round-trips.
func compileTightCaller(t *testing.T, files map[string][]byte) {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module probe\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fixedonly_test.go"), []byte(tightCaller), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("a caller naming the fixed root's message verbs does not compile: %v\n%s", err, out)
	}
}

// tightCaller references all four verbs on both roots. LoadRetainMessages is a
// named refusal for a fixed root (§6.6), so it is a constant and exists as a
// symbol the caller may name; on the plain root it is the region read.
const tightCaller = `package probe

import (
	"testing"
	"unsafe"
)

func TestTightMessageVerbs(t *testing.T) {
	var v Tight
	v.N = 7
	values := []*Tight{&v}
	n := TightMeasureMessages(values, nil)
	if n < 2 {
		t.Fatalf("measure %d", n)
	}
	buf := make([]byte, n)
	var wr TableReport
	if TightSaveMessages(values, buf, &wr) != n || wr != (TableReport{}) {
		t.Fatalf("save %+v", wr)
	}
	vocab := new(TableVocabulary)
	vocab.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	ann := make([]byte, AnnounceMeasure())
	if Announce(ann) != int64(len(ann)) {
		t.Fatal("announce")
	}
	var ar TableReport
	if !AnnounceRead(vocab, ann, &ar) {
		t.Fatalf("announce read %+v", ar)
	}
	var got [1]Tight
	count, ok := TightLoadMessages(got[:], vocab, buf, &ar)
	if !ok || count != 1 || got[0].N != 7 {
		t.Fatalf("load %d %v %+v", count, ok, ar)
	}
	_ = TightLoadRetainMessages

	// THE PLAIN ROOT rides the same wire over ONE REGION for the batch (§3.3)
	loose := []*Loose{{N: 9}, {N: -3}}
	m := LooseMeasureMessages(loose, nil)
	if m < 2 {
		t.Fatalf("loose measure %d", m)
	}
	lbuf := make([]byte, m)
	if LooseSaveMessages(loose, lbuf, nil) != m || lbuf[0] != 2 || lbuf[1] != 1 {
		t.Fatalf("loose save % x", lbuf)
	}
	size := LooseLoadMessagesMeasure(vocab, lbuf)
	if size <= 0 {
		t.Fatalf("loose load measure %d", size)
	}
	raw := make([]byte, size+63)
	off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
	region := raw[off : off+uintptr(size)]
	roots := make([]*Loose, 2)
	var lr TableReport
	lc, lok := LooseLoadMessages(roots, region, vocab, lbuf, &lr)
	if !lok || lc != 2 || roots[0].N != 9 || roots[1].N != -3 {
		t.Fatalf("loose load %d %v %+v", lc, lok, lr)
	}
	retains := make([]TableRetain, 2)
	for i := range retains {
		retains[i] = TableRetain{Bytes: make([]byte, 1024), Ids: make([]TableRetainID, 64)}
	}
	again := make([]*Loose, 2)
	rc, rok := LooseLoadRetainMessages(again, region, vocab, lbuf, retains, &lr)
	if !rok || rc != 2 || again[1].N != -3 {
		t.Fatalf("loose retaining load %d %v %+v", rc, rok, lr)
	}
}
`
