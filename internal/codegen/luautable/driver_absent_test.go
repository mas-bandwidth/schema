package luautable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// PER-CASE ABSENT (docs/PORTING.md I4; test/conformance/README.md): a case the
// Luau leg cannot answer is the line "<case> absent <reason>", and the driver
// writes it as <case>.absent holding the reason, beside the answered cases'
// bytes, so the harness counts it (`pass 16/16 +4a`) instead of failing it.
// The driver runs in a scratch tree under a stand-in interpreter that prints
// one absent line and one answer, so no conformance run's files are touched.

// driverAbsentRun runs a copy of the committed driver and of the shared answer
// parser it hands the answers to (test/conformance/answer-lines.pl), the parser
// edited by patch, over the stand-in's two lines and reports what landed in the out directory.
func driverAbsentRun(t *testing.T, patch func(string) string) (absent string, absentOK bool, answer []byte) {
	t.Helper()
	root := repoRoot(t)
	src, err := os.ReadFile(filepath.Join(root, "test", "conformance", "luau", "driver"))
	if err != nil {
		t.Fatal(err)
	}
	driver := string(src)
	parser, err := os.ReadFile(filepath.Join(root, "test", "conformance", "answer-lines.pl"))
	if err != nil {
		t.Fatal(err)
	}
	parserText := string(parser)
	if patch != nil {
		parserText = patch(parserText)
	}
	dir := t.TempDir()
	leg := filepath.Join(dir, "test", "conformance", "luau")
	if err := os.MkdirAll(leg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(leg, "driver"), []byte(driver), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "test", "conformance", "answer-lines.pl"), []byte(parserText), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(leg, "main.luau"), []byte("-- stand-in\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stand := filepath.Join(dir, "luau")
	if err := os.WriteFile(stand, []byte("#!/bin/sh\nprintf 'case_a absent no variable class here\\ncase_b 0a0b\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(dir, "MANIFEST.txt")
	if err := os.WriteFile(manifest, []byte("# derived manifest\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	cmd := testproc.Command(t, "sh", filepath.Join(leg, "driver"), manifest, "wire", out)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LUAU="+stand)
	if b, err := cmd.CombinedOutput(); err != nil && patch == nil {
		t.Fatalf("the driver failed: %v\n%s", err, b)
	}
	if b, err := os.ReadFile(filepath.Join(out, "case_a.absent")); err == nil {
		absent, absentOK = string(b), true
	}
	answer, _ = os.ReadFile(filepath.Join(out, "case_b"))
	return absent, absentOK, answer
}

func TestLuauDriverWritesAbsentPerCase(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "sh and perl (it runs the Luau conformance driver)")
	absent, ok, answer := driverAbsentRun(t, nil)
	if !ok {
		t.Fatal("the driver wrote no case_a.absent for an absent line")
	}
	if absent != "no variable class here" {
		t.Fatalf("case_a.absent holds %q, want the reason the leg gave", absent)
	}
	if string(answer) != "\x0a\x0b" {
		t.Fatalf("case_b holds %x, want 0a0b beside the absent case", answer)
	}
}

// TestLuauDriverAbsentNegativeControl removes the parser's absent branch and
// requires the gate's first check to see it: the absent line is then refused as
// a non-protocol line, and no case_a.absent is written.
func TestLuauDriverAbsentNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "sh and perl (it runs the Luau conformance driver)")
	const anchor = `elsif ($rest =~ /\Aabsent(?: (.+))?\z/s) {`
	_, ok, _ := driverAbsentRun(t, func(s string) string {
		if strings.Count(s, anchor) != 1 {
			t.Fatalf("the absent branch is not in the parser exactly once")
		}
		return strings.Replace(s, anchor, `elsif (0) {`, 1)
	})
	if ok {
		t.Fatal("with the absent branch removed the parser still wrote case_a.absent: the gate cannot see the branch")
	}
}
