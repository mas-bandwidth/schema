// The CLI's §4.6 target-name safety, per generated target: generate --lang go
// on a unit holding only `self` (a Rust reserved word) succeeds, and schema
// check — which names no target — refuses it.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGenerateChecksOnlyItsTargetsWords(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	work := t.TempDir()
	dir := filepath.Join(work, "unit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "Self.schema")
	src := "package t\ntype T { self int32 }\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	// generate --lang go succeeds: `self` is reserved in Rust, not Go.
	run(t, bin, "generate", "--lang", "go", "--out", filepath.Join(work, "out"), dir)
	// schema check names no target, so it checks every target's words and
	// refuses the unit.
	out, err := exec.Command(bin, "check", dir).CombinedOutput()
	if err == nil {
		t.Fatalf("schema check on a unit holding only `self` must exit 1\n%s", out)
	}
}
