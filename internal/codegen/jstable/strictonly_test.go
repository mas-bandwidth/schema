package jstable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestStrictOnlyJsNoTolerantEntry is the strict-only layer-4 grep gate for
// the JavaScript leg (docs/plans/strict-only.md): the generator's output for
// tables/examples and internal/tablenames/js.go hold none of the retired
// tolerant names — no Tolerant or ReadStrict entry point, and none of the
// retired counters or verdict flags (clamped, kindMismatch, duplicate,
// .malformed, blobShared). The one read returns the whole value or refuses the
// whole load; it never returns a partial value beside a counter.
func TestStrictOnlyJsNoTolerantEntry(t *testing.T) {
	t.Parallel()
	retired := []string{
		"Tolerant",
		"tolerant",
		"ReadStrict",
		"readStrict",
		"kindMismatch",
		"clamped",
		"Clamped",
		"duplicate",
		"Duplicate",
		".malformed",
		"malformed =",
		".Malformed",
		"Malformed =",
		"blobShared",
	}

	u := loadUnitPaths(t, []string{filepath.Join("..", "..", "..", "tables", "examples")})
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("generator emitted no files for tables/examples")
	}
	for name, body := range files {
		for _, word := range retired {
			if strings.Contains(string(body), word) {
				t.Errorf("generated %s retains retired tolerant name %q", name, word)
			}
		}
	}

	names, err := os.ReadFile(filepath.Join("..", "..", "tablenames", "js.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, word := range retired {
		if strings.Contains(string(names), word) {
			t.Errorf("internal/tablenames/js.go retains retired tolerant name %q", word)
		}
	}
}

// TestStrictReadReasonJavaScript pins the strict read's one typed reason
// (docs/SPEC-TABLES.md §6.5, §11): CfgLoad with TableVariableReport.err()
// names the one refusal reason in the precedence the reference legs use.
func TestStrictReadReasonJavaScript(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "node (the JavaScript runtime)")
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
		name   string
		wire   []byte
		reason string
	}{
		{"own", own, "ok"},
		{"out_of_range", wire("v2_cfg.bin"), "out_of_range"},
		{"kind_mismatch", wire("v2_seams.bin"), "kind_mismatch"},
		{"cut_id_table", cut, "wire_damaged"},
		{"empty", []byte{}, "wire_damaged"},
		{"newer_form", newer, "unknown_form"},
		{"message_form", message, "unknown_form"},
	}
	var probe strings.Builder
	fmt.Fprintf(&probe, "import { pathToFileURL } from \"node:url\";\nconst m = await import(pathToFileURL(%q).href);\nconst report = new m.TableVariableReport();\n", module)
	for _, c := range cases {
		bytes := make([]string, len(c.wire))
		for i, b := range c.wire {
			bytes[i] = fmt.Sprint(b)
		}
		fmt.Fprintf(&probe, "{ report.reset(); const wire = new Uint8Array([%s]); const v = m.CfgLoad(wire, report); const why = report.err(); console.log(%q + \" \" + (why === null ? \"ok\" : why) + \" \" + (v !== null && why === null)); }\n",
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
		want := fmt.Sprintf("%s %s %v", c.name, c.reason, c.reason == "ok")
		if got[i] != want {
			t.Errorf("%s: got %q, want %q", c.name, got[i], want)
		}
	}
}
