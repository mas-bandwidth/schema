package jstable

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestJsPointerTargetTypeChecked holds every JavaScript read of the variable
// form to docs/SPEC-TABLES.md §3.1's rule for a pointer's target type
// (docs/rulings.json pointer-target-type-checked): a pointer slot that names a
// record of a type this reader DECLARES but that is not the type the slot
// requires is damage, and the whole load refuses; a record of a type the
// reader cannot name at all (a newer writer's) is counted unknown once and
// every pointer naming it reads null. Each row of
// testdata/wire/pointer-target-type/MANIFEST.tsv runs through Load and
// LoadRetain, and each must answer the row's verdict; LoadMeasure, which
// frames and does not judge, must answer the engine's region measure.
func TestJsPointerTargetTypeChecked(t *testing.T) {
	t.Parallel()
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(repo, "testdata", "wire", "pointer-target-type")
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		Case    string `json:"case"`
		Root    string `json:"root"`
		Wire    string `json:"wire"`
		Read    bool   `json:"read"`
		Unknown int    `json:"unknown"`
		Measure int64  `json:"measure"`
	}
	var rows []row
	unit := ""
	files := map[string][]byte{}
	for i, line := range strings.Split(string(manifest), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("MANIFEST.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		if unit == "" {
			unit = f[1]
		} else if unit != f[1] {
			t.Fatalf("MANIFEST.tsv:%d: unit %q, the rows before read %q", i+1, f[1], unit)
		}
		if f[4] != "read" && f[4] != "refused" {
			t.Fatalf("MANIFEST.tsv:%d: verdict %q, want read or refused", i+1, f[4])
		}
		r := row{Case: f[0], Root: f[2], Wire: f[3], Read: f[4] == "read", Unknown: -1}
		if f[5] != "-" {
			n, err := strconv.Atoi(f[5])
			if err != nil {
				t.Fatalf("MANIFEST.tsv:%d: unknown %q: %v", i+1, f[5], err)
			}
			r.Unknown = n
		}
		wire, err := os.ReadFile(filepath.Join(dir, f[3]))
		if err != nil {
			t.Fatal(err)
		}
		files[f[3]] = wire
		rows = append(rows, r)
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no vectors")
	}
	u := loadUnitForMeasure(t, repo, unit)
	for i := range rows {
		// LoadMeasure is derived from framing alone (§6.5), so it answers the
		// engine's region measure, -1 only where framing refuses; a pointer's
		// target type is the load's verdict, not the measure's.
		want, _, refused := tablewire.FileRegionMeasure(u, findTable(t, u, rows[i].Root), files[rows[i].Wire])
		if refused {
			want = -1
		}
		rows[i].Measure = want
	}
	encoded, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	jsDiscardRun(t, u, files, `
for (const row of `+string(encoded)+`) {
  const wire = new Uint8Array(readFileSync(row.wire));
  const r = new m.TableVariableReport();
  const v = m[row.root + "Load"](wire, r);
  const retain = new m.TableVariableRetain(new Uint8Array(1 << 16), new Array(256));
  const rr = new m.TableVariableReport();
  const rv = m[row.root + "LoadRetain"](wire, retain, rr);
  const measure = Number(m[row.root + "LoadMeasure"](wire));
  if (row.read) {
    if (v === null || r.err() !== null) { fail(row.case + ": Load expected read, got refusal: " + JSON.stringify(r)); }
    if (rv === null || rr.err() !== null) { fail(row.case + ": LoadRetain expected read, got refusal: " + JSON.stringify(rr)); }
    if (row.unknown >= 0 && r.unknown !== row.unknown) { fail(row.case + ": Load unknown " + r.unknown + ", want " + row.unknown); }
    if (row.unknown >= 0 && rr.unknown !== row.unknown) { fail(row.case + ": LoadRetain unknown " + rr.unknown + ", want " + row.unknown); }
    if (row.unknown > 0) {
      if (v !== null && v.next !== null) { fail(row.case + ": Load next names a record of a type the reader cannot name, want null"); }
      if (rv !== null && rv.next !== null) { fail(row.case + ": LoadRetain next names a record of a type the reader cannot name, want null"); }
    }
  } else {
    if (v !== null || r.err() === null) { fail(row.case + ": Load expected refusal, got read"); }
    if (rv !== null || rr.err() === null || retain.used !== 0 || retain.count !== 0) { fail(row.case + ": LoadRetain expected refusal with its store empty: " + JSON.stringify(rr)); }
  }
  if (measure !== row.measure) { fail(row.case + ": LoadMeasure " + measure + ", want " + row.measure); }
}
`)
}
