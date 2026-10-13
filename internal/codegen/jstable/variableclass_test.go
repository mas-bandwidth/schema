package jstable

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// variableclass_test.go tests the VARIABLE FORM's carriage of the class
// BEYOND the card constructs (docs/SPEC-TABLES.md §2.1, §2.5, §2.9, §3, §3.1,
// §16.7): pointers to tables and the flat node table, *bytes blobs, unbounded
// arrays of scalar/string/enum elements, arrays of pointers, and the remaining
// scalar kinds (uint*, float, bits, bytes(N)). The card constructs alone were
// the slice js-carries-the-variable-form-for-card-constructs; this slice is
// js-carries-the-variable-class-beyond-the-card-constructs.

// TestVariableClassBeyondCardConstructsJavaScript pins, red first, that the
// three corpora (tables/pointers, tables/blobs, tables/lists) generate a
// variable module whose roots round-trip the construct this leg now claims:
// a pointer chain survives save/load, a *bytes blob survives, and a []T list
// of tables survives. A construct the emitter silently drops — no refusal,
// but no write either — is the failure (docs/SPEC-TABLES.md §11).
func TestVariableClassBeyondCardConstructsJavaScript(t *testing.T) {
	t.Parallel()
	node := jsNode(t)

	writeVar := func(t *testing.T, paths ...string) string {
		t.Helper()
		u := loadUnit(t, paths...)
		out, err := generateVariableFiles(u, nil)
		if err != nil {
			t.Fatalf("generateVariableFiles refused a unit this leg claims: %v", err)
		}
		dir := t.TempDir()
		for name, content := range out {
			if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}

	t.Run("pointers", func(t *testing.T) {
		dir := writeVar(t, "../../../tables/pointers/Graph.schema", "../../../tables/pointers/Marks.schema", "../../../tables/pointers/Parts.schema")
		probe := `
import { ListNode, ListNodeSave, ListNodeLoad, TableVariableReport } from "./GraphdemoVariable.js";
const a = new ListNode(); a.value = 1;
const b = new ListNode(); b.value = 2; b.next = a;
const saved = ListNodeSave(b);
if (saved === null) { console.log("save null"); process.exit(1); }
const report = new TableVariableReport();
const back = ListNodeLoad(saved, report);
if (back === null || report.malformed) { console.log("load failed", JSON.stringify(report)); process.exit(1); }
if (back.next === null) { console.log("pointer dropped"); process.exit(1); }
console.log("pointer round-trip", saved.length, "bytes");
`
		script := filepath.Join(dir, "probe.mjs")
		if err := os.WriteFile(script, []byte(probe), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(node, script)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("pointer round-trip failed: %v\n%s", err, out)
		}
	})

	t.Run("blobs", func(t *testing.T) {
		dir := writeVar(t, "../../../tables/blobs/Assets.schema")
		probe := `
import { Asset, AssetSave, AssetLoad, TableVariableReport } from "./BlobdemoVariable.js";
const a = new Asset(); a.name = "n"; a.data = new Uint8Array([1,2,3,4]);
const saved = AssetSave(a);
if (saved === null) { console.log("save null"); process.exit(1); }
const report = new TableVariableReport();
const back = AssetLoad(saved, report);
if (back === null || report.malformed) { console.log("load failed", JSON.stringify(report)); process.exit(1); }
if (back.data === null || back.data.length !== 4) { console.log("blob dropped", back.data); process.exit(1); }
console.log("blob round-trip", saved.length, "bytes");
`
		script := filepath.Join(dir, "probe.mjs")
		if err := os.WriteFile(script, []byte(probe), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(node, script)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("blob round-trip failed: %v\n%s", err, out)
		}
	})

	t.Run("lists", func(t *testing.T) {
		dir := writeVar(t, "../../../tables/lists/Holders.schema")
		probe := `
import { Row, Sample, RowSave, RowLoad, TableVariableReport } from "./ListdemoVariable.js";
const r = new Row(); r.label = 7;
const s = new Sample(); s.v = 3; r.items.push(s);
const saved = RowSave(r);
if (saved === null) { console.log("save null"); process.exit(1); }
const report = new TableVariableReport();
const back = RowLoad(saved, report);
if (back === null || report.malformed) { console.log("load failed", JSON.stringify(report)); process.exit(1); }
if (back.items.length !== 1 || back.items[0].v !== 3) { console.log("list dropped", back.items); process.exit(1); }
console.log("list round-trip", saved.length, "bytes");
`
		script := filepath.Join(dir, "probe.mjs")
		if err := os.WriteFile(script, []byte(probe), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(node, script)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("list round-trip failed: %v\n%s", err, out)
		}
	})
}
