package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The tree's empty-row ledger parses: a malformed line there would turn every
// step that names it into a usage error.
func TestTheTreeEmptyRowLedgerParses(t *testing.T) {
	t.Parallel()
	if _, err := readEmptyRows(filepath.Join("..", "..", ".github", "empty-test-rows.txt")); err != nil {
		t.Fatal(err)
	}
}

// A record whose package ran and reported no test at all ("no tests to run")
// is red: zero passed is zero passed, with or without skips. The control is
// the same package with one test that passed.
func TestARunWithNoTestIsRed(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, record string
		exit         int
		want         string
	}{
		{"no test ran", `{"Action":"start","Package":"p"}
{"Action":"output","Package":"p","Output":"ok  \tp\t0.01s [no tests to run]\n"}
{"Action":"pass","Package":"p","Elapsed":0.01}
`, 1, "NO TEST PASSED"},
		{"control: one test passed", `{"Action":"start","Package":"p"}
{"Action":"run","Package":"p","Test":"TestA"}
{"Action":"pass","Package":"p","Test":"TestA","Elapsed":0.01}
{"Action":"pass","Package":"p","Elapsed":0.01}
`, 0, "1 passed"},
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "a.jsonl"), []byte(c.record), 0o644); err != nil {
			t.Fatal(err)
		}
		var out, errb bytes.Buffer
		if got := run([]string{"-slowest", "0", dir}, &out, &errb, ""); got != c.exit || !strings.Contains(out.String(), c.want) {
			t.Errorf("%s: exit %d (want %d), report lacks %q?\n%s%s", c.name, got, c.exit, c.want, out.String(), errb.String())
		}
	}
}

// The ledger refuses a malformed line and a row named twice, and -row and
// -empty-rows go together.
func TestTheEmptyRowLedgerRefuses(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"empty onlyname\n", "skip a b\n", "empty a why\nempty a again\n"} {
		p := filepath.Join(t.TempDir(), "rows.txt")
		if err := os.WriteFile(p, []byte(bad), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := readEmptyRows(p); err == nil {
			t.Errorf("%q: parsed, want an error", bad)
		}
	}
	var out, errb bytes.Buffer
	if got := run([]string{"-row", "x", t.TempDir()}, &out, &errb, ""); got != 2 {
		t.Errorf("-row without -empty-rows: exit %d, want 2", got)
	}
}
