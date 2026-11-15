package squirreltable_test

// A FIXED-FORM FILE HOLDS AT LEAST ONE RECORD (docs/FIXED-FORM-ALGORITHM.md
// §5.3 step 9, docs/SPEC-TABLES.md §3.4 and §22.6). The Squirrel fixed load
// refuses a file whose header and layout are followed by no record, every
// fixed read entry refuses it, a file holding one record still reads, and the
// fixed save refuses an empty batch before it writes a byte. The vector under
// testdata/wire/fixed-empty-batch/ is the same header, layout and integrity
// trailer twice, once with a record and once without.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestSquirrelFixedBatchHoldsARecord(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Squirrel interpreter")

	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	vector := filepath.Join(root, "testdata", "wire", "fixed-empty-batch")
	inputs := map[string][]byte{}
	for _, name := range []string{"one-record.bin", "no-record.bin"} {
		data, err := os.ReadFile(filepath.Join(vector, name))
		if err != nil {
			t.Fatalf("the vector has no %s: %v", name, err)
		}
		inputs[name] = data
	}

	u := fixedLoadUnit(t, root, "test/fixed-empty-batch/Batch.schema")
	sqDir := t.TempDir()
	writeGenerated(t, sqDir, u)

	const probe = `local S = dofile("schema_module.nut", true);
local T = S.require("FixedemptybatchTables");
local L = S.require("schema_table_load");
local R = S.require("schema_table_refusal");
local inputs = dofile("inputs.nut", true);
local fails = 0;
local fail = function(why) { print("FAIL " + why + "\n"); fails++; };
local fresh = function(n) { local a = []; for (local i = 0; i < n; i++) a.append(T.Batch()); return a; };

// EVERY FIXED READ ENTRY refuses a file with no record behind the header and
// the layout: the root read (no capacity), room for one, and room for more.
foreach (cap in [null, 1, 4]) {
  local rows = fresh(4);
  local n = null;
  try { n = T.BatchFixedLoad(inputs["no-record.bin"], rows, cap); } catch (e) { fail("fixed load (cap " + cap + ") raised " + e); continue; }
  if (n != -1) fail("fixed load (cap " + cap + ") read " + n + " records");
  else if (T.FixedReason == null) fail("fixed load (cap " + cap + ") refused without a reason");
}

// THE READS BUILT OVER THE FIXED READER refuse it too: the root's own load,
// the module's plain strict load, and the strict read that dispatches by form.
foreach (entry in ["root", "plain", "strict"]) {
  local report = {};
  local v = null;
  try {
    if (entry == "root") v = T.BatchLoad(inputs["no-record.bin"], report);
    else if (entry == "plain") v = L.load(T, "Batch", inputs["no-record.bin"], report);
    else v = R.ReadStrict(T, "Batch", inputs["no-record.bin"], report);
  } catch (e) { fail(entry + " read raised " + e); continue; }
  if (v != null) fail(entry + " read accepted a file holding no record");
  else if (report.reason == null) fail(entry + " read refused without a reason");
}

// THE CONTROL: A ONE-RECORD FILE STILL READS through every entry.
{
  local rows = fresh(1);
  local n = T.BatchFixedLoad(inputs["one-record.bin"], rows, null);
  if (n != 1 || rows[0].A != 7) fail("one-record file read " + n + " records, A=" + rows[0].A);
  local byRoot = T.BatchLoad(inputs["one-record.bin"], {});
  if (byRoot == null || byRoot.A != 7) fail("one-record file did not read through the root load");
  local byPlain = L.load(T, "Batch", inputs["one-record.bin"], {});
  if (byPlain == null || byPlain.A != 7) fail("one-record file did not read through the plain load");
  local report = {};
  local v = R.ReadStrict(T, "Batch", inputs["one-record.bin"], report);
  if (v == null || v.A != 7) fail("one-record file did not read through the strict read");
}

// A SAVE OF ZERO RECORDS IS REFUSED before any byte is written, in every
// build (docs/rulings.json write-refusal), and raises nothing.
{
  local sentinel = 0x5a;
  local out = blob(T.BatchFixedMeasure(0) + 16);
  for (local i = 0; i < out.len(); i++) out[i] = sentinel;
  local n = null;
  try { n = T.BatchFixedSave(out, []); } catch (e) { fail("save of zero records raised " + e); }
  if (n != -1) fail("save of zero records wrote " + n + " bytes");
  else if (T.FixedReason == null) fail("save of zero records refused without a reason");
  for (local i = 0; i < out.len(); i++) if (out[i] != sentinel) { fail("save of zero records changed byte " + i); break; }
}

// THE CONTROL: a one-record save still writes and reads back.
{
  local rows = fresh(1); rows[0].A = 7;
  local out = blob(T.BatchFixedMeasure(1));
  local n = T.BatchFixedSave(out, rows);
  if (n != out.len()) fail("one-record save wrote " + n + " bytes, want " + out.len());
  else {
    local back = fresh(1);
    local m = T.BatchFixedLoad(out, back, null);
    if (m != 1 || back[0].A != 7) fail("one-record save did not read back");
  }
}

if (fails != 0) { print("FAILURES " + fails + "\n"); }
else { print("ok\n"); }
`
	if err := os.WriteFile(filepath.Join(sqDir, "batch.nut"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(sqrun.Run(t, sqDir, "batch.nut", inputs)))
	if out != "ok" {
		t.Fatalf("the fixed batch rule did not hold:\n%s", out)
	}
}
