package jstable

import (
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestListSaveLoadMeasureCoversFramingJavaScript pins the JavaScript variable
// LoadMeasure (docs/SPEC-TABLES.md §6.5, §20.3) to internal/tablewire's
// FileRegionMeasure on listdemo.Save under the wire-fuzz mutant that found it
// (tables-js-wire-fuzz-all, corpus seed list_tables, pass random #22702, run
// seed 24845619678), where the JS leg asked for 112 region bytes and the
// framing commands exactly 104. The measure must match byte for byte, and the
// JS load must then answer as the oracle.
func TestListSaveLoadMeasureCoversFramingJavaScript(t *testing.T) {
	t.Parallel()
	node := jsNode(t)

	// The random #22702 mutant of list_tables, run seed 24845619678, sha256
	// 054c06c186d3eef6355c4f996bb7ff510a0270c5111dcfc821c970ad84a09112.
	const wireHex = "01010e380d030d020a0000803f0308030000000013020a00000040040a000000400308040000000013020a00004040040a0000804003080500000000050e6b051103020200060e0e04030a000000140000001e000000070c0a0108070908070000000000094b4d57aa3347d2071702864cf563af3a706e3e9343e59d541502864cf463af3154af1d19735012b20f40270b6b9801ffffffffffffffff5f8258ac3615785e8476bc2eef83761e0900000000000000"

	wireData, err := hex.DecodeString(wireHex)
	if err != nil {
		t.Fatal(err)
	}

	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitForMeasure(t, repo, "tables/lists")
	root := findTable(t, u, "Save")

	want, whole, refused := tablewire.FileRegionMeasure(u, root, wireData)
	if refused {
		t.Fatalf("oracle refused the wire for Save")
	}
	if !whole {
		t.Fatalf("oracle did not read the whole wire for Save")
	}

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

	home := runtimeHome(u)
	script := filepath.Join(dir, "probe.mjs")
	probe := `
import { writeFileSync } from "node:fs";
import { SaveLoadMeasure, SaveLoad } from "./%[1]sVariable.js";
const wire = new Uint8Array(%[2]s);
const m = SaveLoadMeasure(wire).toString();
const report = { unknown:0, kindMismatch:0, widened:0, clamped:0, duplicate:0, malformed:false, reason:"" };
const v = SaveLoad(wire, report);
writeFileSync(process.argv[2], m + "\n" + (v === null ? "null" : "ok") + "\n");
`
	src := strings.ReplaceAll(probe, "%[1]s", home)
	src = strings.ReplaceAll(src, "%[2]s", jsu8array(wireData))
	if err := os.WriteFile(script, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "out.txt")
	cmd := exec.Command(node, script, outPath)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("node probe failed: %v\n%s", err, out)
	}
	out, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		t.Fatalf("probe output too short: %q", string(out))
	}
	got, err := strconv.ParseInt(strings.TrimSpace(lines[0]), 10, 64)
	if err != nil {
		t.Fatalf("could not parse LoadMeasure %q: %v", lines[0], err)
	}
	if got != want {
		t.Fatalf("Save LoadMeasure: JS=%d, oracle FileRegionMeasure=%d", got, want)
	}
	if strings.TrimSpace(lines[1]) != "ok" {
		t.Fatalf("Save Load did not answer as the oracle: %q", lines[1])
	}
}
