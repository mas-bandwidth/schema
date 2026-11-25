package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const registry = `# a registry
a | go | ^a/ | make tables-a
b | - | ^b/ | make tables-b
c | all | - | make tables-c
`

type row struct{ Name, Leg, Run string }

func runIn(t *testing.T, args ...string) (int, []row, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	var rows []row
	if code == 0 {
		if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
			t.Fatalf("stdout is not the matrix JSON: %v\n%s", err, out.String())
		}
	}
	return code, rows, errb.String()
}

func names(rows []row) []string {
	var n []string
	for _, r := range rows {
		n = append(n, r.Name)
	}
	return n
}

func write(t *testing.T, dir, name, text string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// -all prints every row, in file order, with each field as written: the
// nightly-gates job runs every one of them.
func TestAllPrintsEveryRow(t *testing.T) {
	t.Parallel()
	reg := write(t, t.TempDir(), "rows.txt", registry)
	code, rows, stderr := runIn(t, "-all", "-rows", reg)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !slices.Equal(names(rows), []string{"a", "b", "c"}) {
		t.Errorf("rows = %v, want a b c", names(rows))
	}
	if rows[2] != (row{"c", "all", "make tables-c"}) {
		t.Errorf("row c = %+v", rows[2])
	}
}

// -all selects nothing, so every selecting flag beside it is refused by name
// rather than silently ignored.
func TestAllRefusesSelectingFlags(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	reg := write(t, dir, "rows.txt", registry)
	files := write(t, dir, "files.txt", "a/x\n")
	for _, extra := range [][]string{{"-files", files}, {"-legs", "go"}, {"-broad", "no"}, {"stray"}} {
		code, _, stderr := runIn(t, append([]string{"-all", "-rows", reg}, extra...)...)
		if code != 2 || stderr == "" {
			t.Errorf("-all %v: exit %d, stderr %q; want a named refusal, exit 2", extra, code, stderr)
		}
	}
}

// A broken or empty registry is red under -all, naming the file: a nightly
// matrix of nothing would be a green night that ran nothing.
func TestAllRefusesABadRegistry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for name, text := range map[string]string{
		"empty.txt":     "# nothing\n",
		"short.txt":     "a | go | make tables-a\n",
		"duplicate.txt": "a | go | - | make x\na | go | - | make y\n",
	} {
		reg := write(t, dir, name, text)
		code, _, stderr := runIn(t, "-all", "-rows", reg)
		if code != 1 || !strings.Contains(stderr, name) {
			t.Errorf("%s: exit %d, stderr %q; want exit 1 naming the file", name, code, stderr)
		}
	}
	if code, _, _ := runIn(t, "-all", "-rows", filepath.Join(dir, "missing.txt")); code != 1 {
		t.Errorf("a missing registry: exit %d, want 1", code)
	}
}

// Without -all the selection is the diff's, as before: the path, the leg and
// the broad rule each pick, and -files and -broad stay required.
func TestSelectionIsUnchanged(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	reg := write(t, dir, "rows.txt", registry)
	files := write(t, dir, "files.txt", "b/x\n")
	code, rows, stderr := runIn(t, "-rows", reg, "-files", files, "-broad", "no")
	if code != 0 || !slices.Equal(names(rows), []string{"b"}) {
		t.Errorf("path selection: exit %d rows %v %s", code, names(rows), stderr)
	}
	code, rows, _ = runIn(t, "-rows", reg, "-files", files, "-legs", "go", "-broad", "no")
	if code != 0 || !slices.Equal(names(rows), []string{"a", "b"}) {
		t.Errorf("leg selection: exit %d rows %v", code, names(rows))
	}
	code, rows, _ = runIn(t, "-rows", reg, "-files", files, "-broad", "yes")
	if code != 0 || len(rows) != 3 {
		t.Errorf("broad: exit %d rows %v", code, names(rows))
	}
	if code, _, _ := runIn(t, "-rows", reg, "-broad", "no"); code != 2 {
		t.Errorf("no -files: exit %d, want 2", code)
	}
	if code, _, _ := runIn(t, "-rows", reg, "-files", files); code != 2 {
		t.Errorf("no -broad: exit %d, want 2", code)
	}
}

// The real nightly registry plans, and every row it prints is one the file
// names: the certify job's matrix is the file.
func TestTheNightlyGatesRegistryPlans(t *testing.T) {
	t.Parallel()
	reg := filepath.Join("..", "..", ".github", "nightly-gates.txt")
	code, rows, stderr := runIn(t, "-all", "-rows", reg)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	raw, err := os.ReadFile(reg)
	if err != nil {
		t.Fatal(err)
	}
	want := 0
	for l := range strings.SplitSeq(string(raw), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			want++
		}
	}
	if want == 0 || len(rows) != want {
		t.Errorf("planned %d rows, the file has %d", len(rows), want)
	}
}
