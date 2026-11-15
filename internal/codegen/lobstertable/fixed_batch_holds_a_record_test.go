package lobstertable

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// A FIXED-FORM FILE HOLDS AT LEAST ONE RECORD (docs/SPEC-TABLES.md §22.6,
// docs/FIXED-FORM-ALGORITHM.md §5.3 step 9): every fixed read entry refuses a
// header and a layout followed by no record, a one-record file still reads,
// and the fixed save refuses an empty batch. The tables' write refusal is in
// every build (docs/SPEC.md §5.1), so the program runs a second time with
// every assert compiled away and must answer the same. Only verdicts are
// compared (docs/rulings.json refusal-verdict-only).
func TestLobsterFixedBatchHoldsARecord(t *testing.T) {
	t.Parallel()
	vectors := filepath.Join("..", "..", "..", "testdata", "wire", "fixed-empty-batch")
	schema, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixed-empty-batch", "Batch.schema"))
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{}
	for _, name := range []string{"no-record.bin", "one-record.bin"} {
		data, err := os.ReadFile(filepath.Join(vectors, name))
		if err != nil {
			t.Fatal(err)
		}
		inputs[name] = data
	}
	u := lobsterUnitOf(t, string(schema))
	files, err := lobster.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	write := func(t *testing.T, program string) string {
		t.Helper()
		dir := t.TempDir()
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "main.lobster"), []byte(program), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	const prelude = `import .ProbeTable
import .load
import .schema_save

def read_in(name:string) -> string:
    let f = read_file(name)
    if f: return f
    return ""

let none = read_in("no-record.bin")
let one = read_in("one-record.bin")
assert length(none) > 0 and length(one) > length(none)
`
	const verdicts = prelude + `let plan:[schema_rt.TableFixedEntry] = []
for(4096): push(plan, schema_rt.TableFixedEntry {})
let report = schema_rt.TableReport {}
let rows = [fixedemptybatch.NewBatch(), fixedemptybatch.NewBatch()]
print("load_none " + string(fixedemptybatch.BatchFixedLoad(rows, none, plan, report) == -1))
print("codec_none " + string(not schema_rt.fixed_record_codec("Batch").read_fixed(none, schema_rt.LoadReport {})))
print("record_none " + string(not schema_rt.load("Batch", none, schema_rt.LoadReport {})))
print("load_one " + string(fixedemptybatch.BatchFixedLoad(rows, one, plan, report) == 1 and rows[0].A == 7))
print("codec_one " + string(schema_rt.fixed_record_codec("Batch").read_fixed(one, schema_rt.LoadReport {}) != nil))
print("record_one " + string(schema_rt.load("Batch", one, schema_rt.LoadReport {}) != nil))
let n, wire, why = fixedemptybatch.BatchFixedSaveChecked([rows[0]], schema_rt.zeros(fixedemptybatch.BatchFixedMeasure(1)))
print("save_one " + string(n == length(one) and why == "" and wire == one))
let empty:[fixedemptybatch.Batch] = []
let en, ewire, ewhy = fixedemptybatch.BatchFixedSaveChecked(empty, schema_rt.zeros(fixedemptybatch.BatchFixedMeasure(1)))
print("save_checked_none " + string(en == -1 and ewhy != ""))
print("save_none " + string(fixedemptybatch.BatchFixedSave(empty) == ""))
`
	const want = "load_none true\ncodec_none true\nrecord_none true\nload_one true\ncodec_one true\nrecord_one true\nsave_one true\nsave_checked_none true\nsave_none true"
	dir := write(t, verdicts)
	if got := strings.TrimSpace(string(lobsterrun.Run(t, dir, "main.lobster", inputs))); got != want {
		t.Fatalf("fixed batch of none:\n%s\nwant:\n%s", got, want)
	}
	out, code := lobsterrun.RunExitFlags(t, dir, "main.lobster", []string{"--runtime-no-asserts"}, inputs)
	if got := strings.TrimSpace(string(out)); code != 0 || got != want {
		t.Fatalf("fixed batch of none without asserts (exit %d):\n%s\nwant:\n%s", code, got, want)
	}

	// The trusted decoder asserts its input as it asserts the header: a
	// header and a layout with no record behind them (the save's own bytes,
	// before the integrity trailer) trap, after a one-record control decodes.
	const decode = prelude + `print("one " + string(length(fixedemptybatch.BatchFixedDecode(substring(one, 0, length(one) - 4)))))
let decoded = fixedemptybatch.BatchFixedDecode(substring(none, 0, length(none) - 4))
print("none " + string(length(decoded)))
`
	out, code = lobsterrun.RunExit(t, write(t, decode), "main.lobster", inputs)
	if got := strings.TrimSpace(string(out)); code == 0 || !strings.HasPrefix(got, "one 1") || strings.Contains(got, "none ") {
		t.Fatalf("trusted decode of no record (exit %d):\n%s", code, got)
	}
}
