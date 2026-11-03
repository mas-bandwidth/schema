package jstable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestStrictReadReasonJavaScript pins the strict read's one typed reason
// (docs/SPEC-TABLES.md §6.5, §11): CfgReadStrict answers the value for a clean
// wire and, for every other wire, null with the one reason the report's err()
// names, in the precedence the reference legs use. The wires are the
// conformance corpus's two generations of Cfg, read by the first: its own
// wire reads, the second generation's clamps and kind mismatches, a cut id
// table and an empty wire are damage, and a form byte this read does not
// carry is unknown_form whichever form it is. The tolerant CfgLoad still reads
// the clamped and kind-mismatched wires, and hands this non-variable root back
// at its declared defaults for a refused form byte (§3), which holds the two
// reads apart.
func TestStrictReadReasonJavaScript(t *testing.T) {
	t.Parallel()
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitForMeasure(t, repo, "test/tables/V1.schema")
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatalf("generateVariableFiles: %v", err)
	}
	work := t.TempDir()
	home := runtimeHome(u)
	data, exists := files[home+"Variable.js"]
	if !exists {
		t.Fatalf("no variable module %sVariable.js generated", home)
	}
	module := filepath.Join(work, home+"Variable.js")
	if err := os.MkdirAll(filepath.Dir(module), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(module, data, 0o600); err != nil {
		t.Fatal(err)
	}
	wire := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(repo, "testdata/wire/tables", name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	own := wire("v1_cfg.bin")
	cut := append([]byte(nil), own[:len(own)-1]...)
	newer := append([]byte{9}, own[1:]...)
	message := append([]byte{2}, own[1:]...)
	cases := []struct {
		name     string
		wire     []byte
		reason   string
		tolerant bool
	}{
		{"own", own, "ok", true},
		{"clamped", wire("v2_cfg.bin"), "clamped", true},
		{"kind_mismatch", wire("v2_seams.bin"), "kind_mismatch", true},
		{"cut_id_table", cut, "wire_damaged", false},
		{"empty", []byte{}, "wire_damaged", false},
		{"newer_form", newer, "unknown_form", true},
		{"message_form", message, "unknown_form", true},
	}
	var probe strings.Builder
	fmt.Fprintf(&probe, "import { pathToFileURL } from \"node:url\";\nconst m = await import(pathToFileURL(%q).href);\nconst report = new m.TableVariableReport();\n", module)
	for _, c := range cases {
		bytes := make([]string, len(c.wire))
		for i, b := range c.wire {
			bytes[i] = fmt.Sprint(b)
		}
		fmt.Fprintf(&probe, "{ const wire = new Uint8Array([%s]); const v = m.CfgReadStrict(wire, report); const why = report.err(); const tolerant = m.CfgLoad(wire, new m.TableVariableReport()) !== null; console.log(%q + \" \" + (why === null ? \"ok\" : why) + \" \" + (v !== null) + \" \" + tolerant); }\n",
			strings.Join(bytes, ","), c.name)
	}
	script := filepath.Join(work, "probe.mjs")
	if err := os.WriteFile(script, []byte(probe.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, script)
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node probe failed: %v\n%s", err, out)
	}
	got := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(got) != len(cases) {
		t.Fatalf("probe answered %d lines, want %d:\n%s", len(got), len(cases), out)
	}
	for i, c := range cases {
		want := fmt.Sprintf("%s %s %v %v", c.name, c.reason, c.reason == "ok", c.tolerant)
		if got[i] != want {
			t.Errorf("%s: got %q, want %q", c.name, got[i], want)
		}
	}
}
