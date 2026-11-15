package gotable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGoReadsRefuseNonCanonical holds the generated Go reads to the shared
// non-canonical vectors (docs/SPEC-TABLES.md §3, §3.1, §3.3, §4;
// docs/rulings.json reads-refuse-non-canonical): every manifest row of
// testdata/wire/non-canonical/MANIFEST.tsv is read through every read entry
// the leg has for its form, and each entry holds the row's VERDICT. A
// canonical row reads, and the value it read re-saves to the same bytes; every
// other row is refused. A variable-form row goes through the region load, the
// builder load, the strict read, the retaining load and the load measure; a
// message-form row through the message load under the unit's announcement. The
// refusal's name is the language's own diagnostic and is not compared
// (docs/rulings.json refusal-verdict-only). A row the test cannot place in a
// unit is a red. The per-kind rows (nonCanonicalKindRows) run the same way, and
// the id table's first-use order is held to the load's own walk.
func TestGoReadsRefuseNonCanonical(t *testing.T) {
	t.Parallel()
	vectors, err := filepath.Abs("../../../testdata/wire/non-canonical")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(vectors, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	// each vector directory is one unit: the schema its vectors were written
	// from, the package it generates and the root of its message form
	units := map[string]*nonCanonicalUnit{
		vectors: {schema: "test/non-canonical/NonCanonical.schema", pkg: "noncanonical", message: "Message", variable: "Root"},
		filepath.Join(filepath.Dir(vectors), "message-one-encoding"): {schema: "test/message-one-encoding/One.schema", pkg: "messageone", message: "One"},
	}
	rows := 0
	for i, line := range strings.Split(string(manifest), "\n") {
		line = strings.TrimRight(line, "\r")
		if i == 0 || line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 4 {
			t.Fatalf("MANIFEST.tsv:%d: want file, form, case, verdict; got %d columns", i+1, len(cols))
		}
		path := filepath.Clean(filepath.Join(vectors, cols[0]))
		u := units[filepath.Dir(path)]
		if u == nil {
			t.Errorf("MANIFEST.tsv:%d: %s lies in no unit this test generates", i+1, cols[0])
			continue
		}
		switch {
		case cols[3] != "read" && cols[3] != "refused":
			t.Errorf("MANIFEST.tsv:%d: verdict %q is neither read nor refused", i+1, cols[3])
			continue
		case cols[1] == "variable" && u.variable == "", cols[1] != "variable" && cols[1] != "message" && cols[1] != "announcement":
			t.Errorf("MANIFEST.tsv:%d: form %q has no read entry in its unit", i+1, cols[1])
			continue
		}
		u.rows = append(u.rows, fmt.Sprintf("{name: %q, path: %q, form: %q, read: %v, resave: %v, wireAlone: %v},", cols[0], path, cols[1], cols[3] == "read", cols[3] == "read", nonCanonicalWireAlone[cols[2]]))
		rows++
	}
	if rows == 0 {
		t.Fatal("the manifest names no row; the gate would pass over an empty set")
	}
	for dir, u := range units {
		u.announcement = fmt.Sprintf("readFile(t, %q)", filepath.Join(dir, "announcement.bin"))
	}
	// and the per-kind rows (nonCanonicalKindRows), written from a unit of
	// their own
	kinds, announcement := nonCanonicalKindRows(t)
	nonCanonicalFirstUseInTheWalk(t, tableGoSource(t, nonCanonicalKindsSchema))
	units["kinds"] = &nonCanonicalUnit{source: nonCanonicalKindsSchema, pkg: "kinds", message: "Msg", variable: "Root", rows: kinds, announcement: fmt.Sprintf("[]byte(%q)", string(announcement))}
	for _, u := range units {
		if len(u.rows) == 0 {
			continue
		}
		t.Run(u.pkg, func(t *testing.T) {
			t.Parallel()
			src := []byte(u.source)
			if u.schema != "" {
				var err error
				if src, err = os.ReadFile(filepath.Join("../../..", u.schema)); err != nil {
					t.Fatal(err)
				}
			}
			probe := strings.NewReplacer(
				"PACKAGE", u.pkg,
				"ANNOUNCEMENT", u.announcement,
				"ROWS", strings.Join(u.rows, "\n\t"),
				"VARIABLE", nonCanonicalVariable(u.variable),
				"MESSAGE", u.message,
			).Replace(nonCanonicalProbe)
			out, err := runGeneratedEdited(t, string(src), probe, nil)
			if err != nil {
				t.Fatalf("the generated Go reads do not hold the non-canonical vectors of %s: %v\n%s", u.schema, err, out)
			}
		})
	}
}

// nonCanonicalWireAlone is the manifest's cases the load measure answers: the
// node table's count and place, which its framing walk reads. It is held to
// refuse them too. The id table's first-use order is not among them: the loads
// hold it in the walk that resolves every reference, and the measure resolves
// none, so it sizes such a file and the load refuses it. The other cases need
// the declaration, which the measure does not read.
var nonCanonicalWireAlone = map[string]bool{
	"empty-node-table":        true,
	"node-table-out-of-place": true,
}

type nonCanonicalUnit struct {
	schema, source, pkg, message, variable string
	announcement                           string // the probe's expression for the unit's announcement
	rows                                   []string
}

// nonCanonicalVariable is the probe's variable-form reader for a unit whose
// variable root is root, or a stub that fails for a unit that has none.
func nonCanonicalVariable(root string) string {
	if root == "" {
		return `func readVariable(t *testing.T, name string, wire []byte, want, wireAlone, resave bool) { t.Errorf("%s: this unit has no variable root", name) }`
	}
	return strings.ReplaceAll(nonCanonicalVariableRead, "ROOT", root)
}

const nonCanonicalVariableRead = `func readVariable(t *testing.T, name string, wire []byte, want, wireAlone, again bool) {
	// the measure answers the wire's verdict: it sizes a canonical file and
	// refuses one whose id table or node table no writer produces
	n := ROOTLoadMeasure(wire)
	if want && n < 0 {
		t.Errorf("%s: the load measure refused a canonical file", name)
	}
	if wireAlone && n >= 0 {
		t.Errorf("%s: the load measure sized a file whose id table or node table no writer produces", name)
	}
	size := n
	if size < 0 {
		size = int64(len(wire))*64 + 4096
	}
	resave := func(entry string, measure func() int64, save func([]byte) int64) {
		m := measure()
		if m < 0 {
			t.Errorf("%s: %s: the value read does not measure", name, entry)
			return
		}
		out := make([]byte, m)
		if save(out) != m || !bytes.Equal(out, wire) {
			t.Errorf("%s: %s: the value read re-saves to %x, not the file's %x", name, entry, out, wire)
		}
	}
	verdict := func(entry string, read bool) {
		if read != want {
			t.Errorf("%s: %s read=%v, want read=%v", name, entry, read, want)
		}
	}

	var report TableReport
	got := ROOTLoad(region(size), wire, &report)
	verdict("the region load", got != nil && !report.Refused())
	if again && got != nil {
		resave("the region load", func() int64 { return ROOTMeasure(got) }, func(out []byte) int64 { return ROOTSave(got, out) })
	}

	var b ROOTBuilder
	if !b.Init() {
		t.Fatal("builder init")
	}
	defer b.Shutdown()
	report = TableReport{}
	ok := ROOTLoadBuilder(&b, wire, &report)
	verdict("the builder load", ok && !report.Refused())
	if again && ok {
		resave("the builder load", func() int64 { return ROOTMeasure(b.GetRoot(), &b.Arena) }, func(out []byte) int64 { return ROOTSave(b.GetRoot(), out, &b.Arena) })
	}

	_, err := ROOTReadStrict(wire)
	verdict("the strict read", err == nil)

	retain := TableRetain{Bytes: make([]byte, 1<<16), Ids: make([]TableRetainID, 64)}
	report = TableReport{}
	kept := ROOTLoadRetain(region(size), wire, &retain, &report)
	verdict("the retaining load", kept != nil && !report.Refused())
	if again && kept != nil {
		resave("the retaining load", func() int64 { return ROOTMeasureRetain(kept, &retain) }, func(out []byte) int64 { return ROOTSaveRetain(kept, &retain, out, &report) })
	}
}
`

const nonCanonicalProbe = `package PACKAGE

import (
	"bytes"
	"os"
	"testing"
	"unsafe"
)

type row struct {
	name, path, form, wire  string
	read, resave, wireAlone bool
	other                   bool // under another build's announcement of the same shape
}

var rows = []row{
	ROWS
}

func region(n int64) []byte {
	raw := make([]byte, n+64)
	off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
	return raw[off : off+uintptr(n)]
}

VARIABLE

func readFile(t *testing.T, path string) []byte {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// announce reads the unit's own announcement, or, other set, the same shape
// under another build version (its low byte flipped).
func announce(t *testing.T, vocab *TableVocabulary, storage []TableMessageEntry, other bool) bool {
	data := append([]byte(nil), ANNOUNCEMENT...)
	if other {
		data[3] ^= 1
	}
	vocab.Init(storage)
	var report TableReport
	return AnnounceRead(vocab, data, &report) && !report.Refused()
}

func readMessage(t *testing.T, name string, wire []byte, want, again, other bool) {
	var storage [256]TableMessageEntry
	var vocab TableVocabulary
	if !announce(t, &vocab, storage[:], other) {
		t.Fatalf("%s: the unit's announcement does not read", name)
	}
	values := make([]MESSAGE, TableMessageBatchMax)
	var report TableReport
	n, ok := MESSAGELoadMessages(values, &vocab, wire, &report)
	read := ok && !report.Refused()
	if read != want {
		t.Errorf("%s: the message load read=%v, want read=%v", name, read, want)
	}
	if !again || !read {
		return
	}
	batch := make([]*MESSAGE, n)
	for i := range batch {
		batch[i] = &values[i]
	}
	size := MESSAGEMeasureMessages(batch, &report)
	if size < 0 {
		t.Errorf("%s: the batch read does not measure", name)
		return
	}
	out := make([]byte, size)
	if MESSAGESaveMessages(batch, out, &report) != size || !bytes.Equal(out, wire) {
		t.Errorf("%s: the batch read re-saves to %x, not the file's %x", name, out, wire)
	}
}

func TestReadsRefuseNonCanonical(t *testing.T) {
	for _, r := range rows {
		wire := []byte(r.wire)
		if r.path != "" {
			wire = readFile(t, r.path)
		}
		switch r.form {
		case "variable":
			readVariable(t, r.name, wire, r.read, r.wireAlone, r.resave)
		case "message":
			readMessage(t, r.name, wire, r.read, r.resave, r.other)
		case "announcement":
			var storage [256]TableMessageEntry
			var vocab TableVocabulary
			vocab.Init(storage[:])
			var report TableReport
			if got := AnnounceRead(&vocab, wire, &report) && !report.Refused(); got != r.read {
				t.Errorf("%s: the announcement read=%v, want read=%v", r.name, got, r.read)
			}
		}
	}
}
`
