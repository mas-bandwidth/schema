// The CLI's §4.6 target-name safety, per generated target: generate --lang go
// on a unit holding only `self` (a Rust reserved word) succeeds, and schema
// check — which names no target — refuses it.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// A target with no reserved-word list of its own has no words to narrow to, so
// generate on it checks every target's words rather than none: `class` is in
// the C++ and C# lists, and `generate --lang java` on a field named class still
// exits 1 rather than emitting Java that spells a method `class`.
func TestGenerateWithNoReservedListChecksEveryTarget(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	work := t.TempDir()
	dir := filepath.Join(work, "unit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "Class.schema")
	src := "package t\ntable W { class int32 }\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"java", "c", "js", "elixir"} {
		out, status := runFail(t, bin, "generate", "--lang", lang, "--out", filepath.Join(work, "out-"+lang), dir)
		if status != 1 {
			t.Errorf("generate --lang %s exited %d, want 1\n%s", lang, status, out)
		}
		if !strings.Contains(out, `"class" is a reserved word`) {
			t.Errorf("generate --lang %s should refuse class as a reserved word, got %q", lang, out)
		}
	}
}
