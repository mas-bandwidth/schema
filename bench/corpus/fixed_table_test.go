package corpus

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"os"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// These are the C++ producer's same 64 populated values on native form 3.
// The independent Go engine must read and reproduce each complete file; a
// different form cannot satisfy the corpus merely by retaining its length.
func TestBenchFixedTableCorpus(t *testing.T) {
	read := func(path string) []byte {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	golden := read("fixed-table/bench_table.bin")
	variants := read("fixed-table/bench_table.variants.bin")
	if len(golden) < 20 || golden[0] != 3 || len(variants) != numVariants*len(golden) {
		t.Fatalf("fixed corpus shape: golden=%d variants=%d", len(golden), len(variants))
	}
	if !bytes.Equal(golden, variants[:len(golden)]) {
		t.Fatal("fixed variant zero is not the fixed golden")
	}
	// BENCH-STANDARD §1.6 hashes basenames, a zero separator, and contents
	// in sorted basename order. A fixed run cannot share the historical id.
	id := fixedTableCorpusID(golden, variants)
	if id != "65b15368d40f6544" {
		t.Fatalf("fixed corpus_id changed to %s; investigate before repinning", id)
	}
	legacyID := fixedTableCorpusID(read("../../testdata/wire/bench_table.bin"), read("variants/bench_table.variants.bin"))
	if id == legacyID {
		t.Fatal("fixed and historical variable corpora have one corpus_id")
	}
	t.Logf("corpus_id=%s; historical form1 corpus_id=%s; 64 files of %d bytes", id, legacyID, len(golden))
	u, err := compiler.New().Load([]string{"BenchTable.schema"})
	if err != nil {
		t.Fatal(err)
	}
	model := tabletext.NewModel(u)
	root := model.Lookup("TableMixed")
	if root == nil || !tablewire.FixedRoot(model, root) {
		t.Fatal("TableMixed must be a declared fixed root")
	}
	seen := map[string]int{}
	for k := range numVariants {
		wire := variants[k*len(golden) : (k+1)*len(golden)]
		if prev, dup := seen[string(wire)]; dup {
			t.Fatalf("fixed variants %d and %d are identical", prev, k)
		}
		seen[string(wire)] = k
		value := model.New(root)
		var report tabletext.Report
		ok, err := tablewire.Decode(model, value, wire, &report)
		if err != nil || !ok || !report.Silent() {
			t.Fatalf("variant %d: independent read: ok=%v err=%v report=%+v", k, ok, err, report)
		}
		again, err := tablewire.Encode(model, value)
		if err != nil || !bytes.Equal(wire, again) {
			t.Fatalf("variant %d: independent writeback changed the C++ file: %v", k, err)
		}
	}
	for _, at := range []int{0, 8} { // wrong form, then unknown layout hash
		bad := bytes.Clone(golden)
		bad[at] ^= 1
		var report tabletext.Report
		if ok, err := tablewire.Decode(model, model.New(root), bad, &report); ok || err == nil || !report.Refused {
			t.Fatalf("changed byte %d was not refused: ok=%v err=%v report=%+v", at, ok, err, report)
		}
	}
}

func fixedTableCorpusID(golden, variants []byte) string {
	h := fnv.New64a()
	for _, file := range []struct {
		name string
		data []byte
	}{{"bench_table.bin", golden}, {"bench_table.variants.bin", variants}} {
		h.Write([]byte(file.name))
		h.Write([]byte{0})
		h.Write(file.data)
	}
	return fmt.Sprintf("%016x", h.Sum64())
}
