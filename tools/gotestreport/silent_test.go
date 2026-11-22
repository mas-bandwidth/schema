package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const failingRecord = `{"Action":"run","Package":"p","Test":"TestX"}
{"Action":"fail","Package":"p","Test":"TestX","Elapsed":0.1}
{"Action":"fail","Package":"p","Elapsed":0.1}
`

// A record directory is listed, never globbed: Glob read "[1]" in its name as
// a pattern, matched nothing, and reported "no go test ran" over a record that
// holds a failure.
func TestARecordDirectoryNamedLikeAPatternIsRead(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "rec[1]")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.jsonl"), []byte(failingRecord), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	run([]string{dir}, &out, &errb, "")
	if strings.Contains(out.String(), "no go test ran") || !strings.Contains(out.String(), "1 failed") {
		t.Fatalf("the record was not read:\n%s", out.String())
	}
}

// A step summary the report was told to append to and could not is said, and
// the step is red: the report it carried reached nobody.
func TestASummaryThatCannotBeWrittenIsRed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.jsonl"), []byte(strings.ReplaceAll(failingRecord, `"fail"`, `"pass"`)), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	missing := filepath.Join(dir, "no-such-dir", "summary.md")
	if code := run([]string{dir}, &out, &errb, missing); code != 2 || !strings.Contains(errb.String(), "step summary") {
		t.Fatalf("exit %d, want 2 naming the step summary:\n%s", code, errb.String())
	}
	summary := filepath.Join(dir, "summary.md")
	if err := os.WriteFile(summary, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errb.Reset()
	if code := run([]string{dir}, &out, &errb, summary); code != 0 {
		t.Fatalf("control: exit %d:\n%s", code, errb.String())
	}
	if b, err := os.ReadFile(summary); err != nil || !strings.Contains(string(b), "```") {
		t.Fatalf("control: the summary was not written: %q %v", b, err)
	}
}
