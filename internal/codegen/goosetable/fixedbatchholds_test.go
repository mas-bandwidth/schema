package goosetable

// TestGooseFixedBatchHoldsARecord holds the Goose fixed form to the rule that
// a fixed-form file holds at least one record (docs/FIXED-FORM-ALGORITHM.md
// §5.3 step 9, docs/SPEC-TABLES.md §3.4 and §22.6): the shared no-record
// vector is refused by every fixed read entry, the one-record file still reads
// and saves back byte for byte, and a save of zero records refuses before it
// writes a byte.

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGooseFixedBatchHoldsARecord(t *testing.T) {
	t.Parallel()
	vector := filepath.Join(fixedRoot, "testdata", "wire", "fixed-empty-batch")
	noRecord, err := os.ReadFile(filepath.Join(vector, "no-record.bin"))
	if err != nil {
		t.Fatal(err)
	}
	oneRecord, err := os.ReadFile(filepath.Join(vector, "one-record.bin"))
	if err != nil {
		t.Fatal(err)
	}
	u := fixedUnit(t, "test/fixed-empty-batch/Batch.schema")
	const main = `
fn main() {
    var no_rec: u8[>..] = [];
    var one_rec: u8[>..] = [];
    if !read_file("no-record.bin", no_rec) { print("read no-record failed"); return; }
    if !read_file("one-record.bin", one_rec) { print("read one-record failed"); return; }

    var values: fixedemptybatch::Batch[4] = [fixedemptybatch::Batch { .. }; 4];

    var refused_rep = schema_rt::FixedReport { .. };
    let refused_n = fixedemptybatch::BatchFixedLoad(values, no_rec, refused_rep);
    print("load no-record ", refused_n);

    var refused_evolve = schema_rt::FixedEvolveReport { .. };
    let refused_evolve_n = fixedemptybatch::BatchFixedEvolveLoad(values, no_rec, refused_evolve);
    print("evolve no-record ", refused_evolve_n);

    var read_rep = schema_rt::FixedReport { .. };
    let read_n = fixedemptybatch::BatchFixedLoad(values, one_rec, read_rep);
    print("load one-record ", read_n);

    var out: u8[>..] = [];
    let written = fixedemptybatch::BatchFixedSave(values[0..read_n], out);
    print("save one-record ", written, " ", out.len);
    hex(out);

    var evolve_rep = schema_rt::FixedEvolveReport { .. };
    let evolve_n = fixedemptybatch::BatchFixedEvolveLoad(values, one_rec, evolve_rep);
    print("evolve one-record ", evolve_n);

    var empty: u8[>..] = [];
    let empty_written = fixedemptybatch::BatchFixedSave(values[0..0], empty);
    print("save no-record ", empty_written, " ", empty.len);
}
`
	out := runCustomFixedGoose(t, u, nil, main, map[string][]byte{
		"no-record.bin":  noRecord,
		"one-record.bin": oneRecord,
	})
	want := []string{
		"load no-record -1",
		"evolve no-record -1",
		"load one-record 1",
		fmt.Sprintf("save one-record %d %d", len(oneRecord), len(oneRecord)),
		hex.EncodeToString(oneRecord),
		"evolve one-record 1",
		"save no-record -1 0",
	}
	got := strings.Split(out, "\n")
	if len(got) != len(want) {
		t.Fatalf("the probe printed %d lines, want %d:\n%s", len(got), len(want), out)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i+1, got[i], want[i])
		}
	}
}
