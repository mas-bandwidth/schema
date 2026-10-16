package jstable

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestWideningReasonsJavaScript reads the committed narrow list wire with the
// current variable-form reader.  A lawful widening is a successful read: the
// value lands in the wider field, widened counts the list element, and the
// successful read leaves the refusal reason empty (the Go variable reader's
// contract; see test/conformance/go/rows/R20_test.go).
func TestWideningReasonsJavaScript(t *testing.T) {
	t.Parallel()
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitPaths(t, []string{filepath.Join(repo, "tables", "lists")})
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatalf("generate variable files: %v", err)
	}
	home := runtimeHome(u) + "Variable.js"
	data, ok := files[home]
	if !ok {
		t.Fatalf("generated variable module %s is absent", home)
	}
	work := t.TempDir()
	module := filepath.Join(work, home)
	if err := os.MkdirAll(filepath.Dir(module), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(module, data, 0o600); err != nil {
		t.Fatal(err)
	}
	wire := filepath.Join(work, "list_scalar_widen.bin")
	bytes, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", "fuzz-vectors", "list_scalar_widen.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wire, bytes, 0o600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(work, "probe.mjs")
	const probe = `
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
const m = await import(pathToFileURL(process.argv[2]).href);
const report = new m.TableVariableReport();
const value = m.SaveLoad(new Uint8Array(readFileSync(process.argv[3])), report);
if (value === null) { throw new Error("widening wire refused: " + JSON.stringify(report)); }
if (report.reason !== "" || report.widened !== 1 || value.scores.length !== 3 ||
    value.scores[0] !== 10 || value.scores[1] !== 0 || value.scores[2] !== 0) {
  throw new Error("widening report/value mismatch: " + JSON.stringify({report, value}));
}
console.log("widening reason ok");
`
	if err := os.WriteFile(script, []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, script, module, wire)
	cmd.Dir = work
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("JavaScript widening probe failed: %v\n%s", err, out)
	}
}
