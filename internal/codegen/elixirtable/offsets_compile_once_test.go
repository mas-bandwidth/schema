package elixirtable

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// compileLogPlaceholder stands in each committed .ex file of the compile-once
// unit for the log its top-level line appends to; the test writes a fresh log
// path in its place.
const compileLogPlaceholder = "@@COMPILE_LOG@@"

// checkOffsetsCompileOnce runs the offsets gate's variant runner
// (runOrdinaryVariants) over the committed multi-module unit in dir and holds
// it to dir/cases.tsv (PR #4484, F-209): every shared module is compiled once
// over all the variants, the runtime once per variant, and every variant
// answers what it owes. Every file of the unit appends its own name to a log
// each time it is compiled (top-level code in an .ex file runs at compile
// time), so the check is a count, never a timing.
func checkOffsetsCompileOnce(t *testing.T, elixirBin, dir string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var (
		variants []ordinaryVariant
		runtime  string
		compiled = map[string]int{}
		answers  = map[string]map[string]string{}
	)
	for n, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			t.Fatalf("cases.tsv line %d: %d fields, want 4", n+1, len(f))
		}
		switch f[0] {
		case "variant":
			variants = append(variants, ordinaryVariant{f[1], f[3] + "\n"})
		case "runtime":
			runtime = f[1]
		case "compiled":
			c, err := strconv.Atoi(f[3])
			if err != nil {
				t.Fatalf("cases.tsv line %d: %v", n+1, err)
			}
			compiled[f[1]] = c
		case "answer":
			if answers[f[1]] == nil {
				answers[f[1]] = map[string]string{}
			}
			answers[f[1]][f[2]] = f[3]
		default:
			t.Fatalf("cases.tsv line %d: unknown kind %q", n+1, f[0])
		}
	}
	if len(variants) == 0 || runtime == "" || variants[0].name != "real" {
		t.Fatalf("cases.tsv names no runtime or no variants, or its first variant is not real: %d variants, runtime %q", len(variants), runtime)
	}
	if compiled[runtime] != len(variants) {
		t.Fatalf("cases.tsv: the runtime %s is owed %d compiles, want one per variant (%d)", runtime, compiled[runtime], len(variants))
	}

	log := filepath.Join(t.TempDir(), "compiled.log")
	files := map[string][]byte{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".ex") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(src), compileLogPlaceholder) {
			t.Fatalf("%s has no %s line: the file's compiles cannot be counted", e.Name(), compileLogPlaceholder)
		}
		files[e.Name()] = []byte(strings.ReplaceAll(string(src), compileLogPlaceholder, elixirString(log)))
		if _, ok := compiled[e.Name()]; !ok {
			t.Errorf("%s has no compiled row in cases.tsv", e.Name())
		}
	}
	probe, err := os.ReadFile(filepath.Join(dir, "probe.exs"))
	if err != nil {
		t.Fatal(err)
	}

	outs := runOrdinaryVariants(t, elixirBin, files, runtime, variants[0].text, variants, string(probe))

	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for name := range strings.FieldsSeq(string(raw)) {
		seen[name]++
	}
	for name, want := range compiled {
		if got := seen[name]; got != want {
			t.Errorf("%s was compiled %d times over %d variants, want %d: the variant runner compiles the shared unit once and only the runtime per variant", name, got, len(variants), want)
		}
	}
	for _, v := range variants {
		got := map[string]string{}
		for line := range strings.SplitSeq(outs[v.name], "\n") {
			if f := strings.Fields(line); len(f) == 3 && f[0] == "ANSWER" {
				got[f[1]] = f[2]
			}
		}
		for key, want := range answers[v.name] {
			if got[key] != want {
				t.Errorf("variant %s answered %s = %q, want %q\n%s", v.name, key, got[key], want, outs[v.name])
			}
		}
	}
}
