package jstable

import (
	"encoding/hex"
	"encoding/json"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestRetainsUnknownOnEveryRootJavaScript is the retain-unknown core of this
// leg (docs/SPEC-TABLES.md §6.6): every `retain` row of the conformance
// manifest is one focused round trip through the generated retaining Load and
// Save: the load reads, the save writes, and every row that pins a save is held
// to the Go reference's bytes, byte for byte. The retain counters are
// per-language diagnostics and are not compared (strict-only, layer 3: read,
// then write; docs/rulings.json refusal-verdict-only). The rows together carry
// the construct families the RT pair
// was written for: nested tables, an array of tables, an enum-keyed array, a
// union's arm body, a map entry's value, a list element, and the exclusion
// classes (node index, unknown enum variant, unknown union arm, unknown keyed
// slot, unknown node record).
func TestRetainsUnknownOnEveryRootJavaScript(t *testing.T) {
	slowtest.Gate(t, "an external toolchain")

	t.Parallel()
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitForMeasure(t, repo, "test/tables/RT1.schema")
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatalf("generateVariableFiles: %v", err)
	}
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	module := runtimeHome(u) + "Variable.js"
	probe := filepath.Join(dir, "retain.mjs")
	src := `
import { readFileSync } from "node:fs";
import { NodeLoadMeasure, NodeLoadRetain, NodeMeasureRetain, NodeSaveRetain, TableVariableReport, TableVariableRetain } from "./` + module + `";
const input = JSON.parse(readFileSync(process.argv[2], "utf8"));
const wire = new Uint8Array(Buffer.from(input.wire, "hex"));
if (NodeLoadMeasure(wire) < 0) { throw new Error("load measure refused"); }
const bytes = new Uint8Array(input.capacity === "full" ? 1 << 20 : 0);
const ids = input.ids === "full" ? 1 << 17 : Number(input.ids);
let store = new TableVariableRetain(new Uint8Array(1 << 20), new Array(ids));
let report = new TableVariableReport();
if (input.capacity === "short") {
  if (NodeLoadRetain(wire, store, report) === null) { throw new Error("capacity probe refused"); }
  store = new TableVariableRetain(new Uint8Array(Math.max(0, store.used - 1)), new Array(ids));
  report = new TableVariableReport();
}
const root = NodeLoadRetain(wire, store, report);
if (root === null || report.damaged) { throw new Error("retained load refused: " + JSON.stringify(report)); }
const size = NodeMeasureRetain(root, store);
if (size < 0) { throw new Error("retaining measure refused"); }
const out = NodeSaveRetain(root, store, report);
if (out === null) { throw new Error("retaining save refused: " + JSON.stringify(report)); }
if (size !== out.length) { throw new Error("measure " + size + " is not the save's " + out.length + " bytes"); }
const again = NodeSaveRetain(root, store, new TableVariableReport());
if (again === null || again.length !== out.length) { throw new Error("save is not repeatable"); }
for (let i = 0; i < out.length; i++) { if (again[i] !== out[i]) { throw new Error("save is not repeatable"); } }
process.stdout.write(JSON.stringify({ hex: Buffer.from(out).toString("hex") }));
`
	if err := os.WriteFile(probe, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	inputPath := filepath.Join(dir, "input.json")
	run := func(t *testing.T, wire []byte, capacity, ids string) []byte {
		t.Helper()
		payload, err := json.Marshal(map[string]any{
			"wire": hex.EncodeToString(wire), "capacity": capacity, "ids": ids,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(inputPath, payload, 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(node, probe, inputPath)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("node retain probe failed: %v\n%s", err, out)
		}
		var answer struct {
			Hex string `json:"hex"`
		}
		if err := json.Unmarshal(out, &answer); err != nil {
			t.Fatalf("decode node answer %q: %v", out, err)
		}
		saved, err := hex.DecodeString(answer.Hex)
		if err != nil {
			t.Fatalf("decode saved hex: %v", err)
		}
		return saved
	}

	manifest, err := os.ReadFile(filepath.Join(repo, "testdata/conformance/tables/MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for raw := range strings.SplitSeq(string(manifest), "\n") {
		text := strings.TrimSpace(raw)
		if text == "" || text[0] == '#' {
			continue
		}
		f := strings.Fields(text)
		if f[0] != "retain" {
			continue
		}
		// retain <case> <unit> <root> <wire> <capacity> <ids> <saves|->
		if len(f) != 8 {
			t.Fatalf("%s: retain row has %d fields, want 8", f[1], len(f))
		}
		if f[2] != "tblrt1" || f[3] != "Node" {
			continue
		}
		rows++
		t.Run(f[1], func(t *testing.T) {
			wire, err := os.ReadFile(filepath.Join(repo, f[4]))
			if err != nil {
				t.Fatal(err)
			}
			got := run(t, wire, f[5], f[6])
			if f[7] == "-" {
				return
			}
			var want []byte
			for path := range strings.SplitSeq(f[7], ",") {
				pinned, err := os.ReadFile(filepath.Join(repo, path))
				if err != nil {
					t.Fatal(err)
				}
				want = append(want, pinned...)
			}
			if !strings.EqualFold(hex.EncodeToString(got), hex.EncodeToString(want)) {
				t.Fatalf("saved bytes: JS %d bytes, Go oracle %d bytes", len(got), len(want))
			}
		})
	}
	if rows != 8 {
		t.Fatalf("the manifest carries %d retain rows on tblrt1.Node, want 8", rows)
	}
}
