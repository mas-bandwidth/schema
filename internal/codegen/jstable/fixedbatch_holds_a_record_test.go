package jstable

// A FIXED-FORM FILE HOLDS AT LEAST ONE RECORD (docs/FIXED-FORM-ALGORITHM.md
// §5.3 step 9, docs/SPEC-TABLES.md §3.4 and §22.6): a header and a layout with
// no record behind them is refused by the read, and a save of zero records is
// refused before it writes a byte. The vector under
// testdata/wire/fixed-empty-batch/ is the same header, layout and integrity
// trailer twice, once with a record and once without.
//
// The module is generated from the vector's own schema and run under the leg's
// own node, so the verdict is the generated reader's and not a reading of the
// emitted text.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestJsFixedBatchHoldsARecord(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "node")

	node := jsNode(t)
	u := loadUnit(t, "../../../test/fixed-empty-batch/Batch.schema")

	var batch string
	for _, st := range jsFixedUnitRoots(u) {
		if st.Name == "Batch" {
			batch = st.Name
		}
	}
	if batch == "" {
		t.Fatal("the vector's schema declares no fixed root Batch")
	}

	files := generatedTree(u)
	root := ""
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if strings.Contains(string(data), "export function "+batch+"FixedLoad") {
			root = strings.TrimSuffix(name, ".js")
		}
	}
	if root == "" {
		t.Fatalf("no generated module carries %sFixedLoad", batch)
	}
	home := runtimeHome(u)

	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	vector := "../../../testdata/wire/fixed-empty-batch"
	for _, name := range []string{"one-record.bin", "no-record.bin"} {
		data, err := os.ReadFile(filepath.Join(vector, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	probe := fmt.Sprintf(`import { readFileSync } from "node:fs";
import { %[1]s, Init%[1]s, TableFixedReport } from "./%[2]sTable.js";
import { %[1]sFixedLoad, %[1]sFixedNewPlan, %[1]sFixedSave, %[1]sFixedMeasure } from "./%[3]s.js";

let failures = 0;
function fail(why) { console.error("FAIL: " + why); failures++; }
function fresh() { const v = new %[1]s(); Init%[1]s(v); return v; }

const one = readFileSync("one-record.bin");
const none = readFileSync("no-record.bin");

// A ONE-RECORD FILE STILL READS.
{
  const back = [fresh()];
  const report = new TableFixedReport();
  const n = %[1]sFixedLoad(back, 1, one, one.length, %[1]sFixedNewPlan(4096, 4096), report);
  if (n !== 1 || report.refused !== 0 || report.damaged) {
    fail("a one-record file did not read: n=" + n + " refused=" + report.refused + " damaged=" + report.damaged);
  }
}

// THE HEADER AND LAYOUT WITH NO RECORD BEHIND THEM IS REFUSED.
{
  const back = [fresh()];
  const report = new TableFixedReport();
  const n = %[1]sFixedLoad(back, 1, none, none.length, %[1]sFixedNewPlan(4096, 4096), report);
  if (n !== -1) { fail("a file holding no record was read as a batch of " + n); }
  if (report.refused === 0 && !report.damaged) { fail("a file holding no record was not refused"); }
}

// A SAVE OF ZERO RECORDS IS REFUSED BEFORE ANY BYTE IS WRITTEN.
{
  const buf = new Uint8Array(%[1]sFixedMeasure(0) + 16);
  buf.fill(0xa5);
  let got;
  try {
    got = %[1]sFixedSave([], 0, buf);
  } catch (e) {
    fail("a save of zero records threw " + e.name + " instead of answering -1");
  }
  if (got !== -1) { fail("a save of zero records was written (" + got + ")"); }
  for (let i = 0; i < buf.length; i++) {
    if (buf[i] !== 0xa5) { fail("a refused save changed the buffer at byte " + i); break; }
  }
}

// THE CONTROL: ONE RECORD STILL WRITES AND READS.
{
  const v = fresh();
  v.a = 7;
  const buf = new Uint8Array(%[1]sFixedMeasure(1));
  if (%[1]sFixedSave([v], 1, buf) !== buf.length) { fail("a one-record save was refused"); }
  const back = [fresh()];
  const report = new TableFixedReport();
  const n = %[1]sFixedLoad(back, 1, buf, buf.length, %[1]sFixedNewPlan(4096, 4096), report);
  if (n !== 1 || report.refused !== 0 || report.damaged || back[0].a !== 7) {
    fail("a one-record write did not read back: n=" + n + " a=" + back[0].a);
  }
}

if (failures !== 0) { process.exit(1); }
console.log("ok");
`, batch, home, root)

	if err := os.WriteFile(filepath.Join(dir, "probe.mjs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, filepath.Join(dir, "probe.mjs"))
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "ok" {
		t.Fatalf("the fixed batch rule did not hold:\n%s", out)
	}
}
