package main

// THE README'S SURFACES TABLE IS THE HARNESS'S SURFACES LIST. A port's driver
// test holds its `list` to the table in test/conformance/README.md ("The
// surfaces"); the harness runs the surfaces var. If the two drift, a driver
// can be red against a README the harness does not follow, or green against a
// surface the harness never runs. This test holds them equal, both ways, and
// holds the retired names out of the table.

import (
	"os"
	"slices"
	"sort"
	"strings"
	"testing"
)

// readmeSurfaceTable reads the first column of the "## The surfaces" table.
func readmeSurfaceTable(t *testing.T, readme string) []string {
	t.Helper()
	_, section, ok := strings.Cut(readme, "\n## The surfaces\n")
	if !ok {
		t.Fatal(`test/conformance/README.md has no "## The surfaces" section`)
	}
	if next := strings.Index(section, "\n## "); next >= 0 {
		section = section[:next]
	}
	var out []string
	inBody := false
	for line := range strings.SplitSeq(section, "\n") {
		if !strings.HasPrefix(line, "|") {
			continue
		}
		if !inBody {
			inBody = strings.HasPrefix(line, "|---")
			continue
		}
		cell, _, _ := strings.Cut(strings.TrimPrefix(line, "|"), "|")
		name := strings.Trim(strings.TrimSpace(cell), "`")
		if name == "" {
			t.Fatalf("the surfaces table has a row with no surface name: %q", line)
		}
		out = append(out, name)
	}
	if len(out) == 0 {
		t.Fatal("the surfaces table in test/conformance/README.md names no surface")
	}
	return out
}

// surfaceSetDiff is the names in a and not in b, sorted.
func surfaceSetDiff(a, b []string) []string {
	var out []string
	for _, s := range a {
		if !slices.Contains(b, s) {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func TestReadmeSurfacesAreTheHarnessSurfaces(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatal(err)
	}
	table := readmeSurfaceTable(t, string(data))
	listed := slices.DeleteFunc(slices.Clone(surfaces), func(s string) bool { return slices.Contains(harnessSurfaces, s) })
	if missing := surfaceSetDiff(listed, table); len(missing) > 0 {
		t.Errorf("the harness runs %v, which the README's surfaces table does not name", missing)
	}
	if extra := surfaceSetDiff(table, listed); len(extra) > 0 {
		t.Errorf("the README's surfaces table names %v, which the harness does not run or no driver lists", extra)
	}
	for _, s := range harnessSurfaces {
		if !slices.Contains(surfaces, s) {
			t.Errorf("harnessSurfaces names %q, which the harness does not run", s)
		}
		if !strings.Contains(string(data), "THE HARNESS ALSO SCORES ONE SURFACE NO DRIVER LISTS: `"+s+"`") {
			t.Errorf("the README does not say the harness scores %q, a surface no driver lists", s)
		}
	}
	for _, s := range table {
		if slices.Contains(retiredSurfaces, s) {
			t.Errorf("the README's surfaces table names the retired surface %q", s)
		}
	}
	seen := map[string]bool{}
	for _, s := range table {
		if seen[s] {
			t.Errorf("the README's surfaces table names %q twice", s)
		}
		seen[s] = true
	}
}

// TestReadmeSurfaceDiffBites is the reversed witness: a table that drops a
// surface the harness runs, and one that adds a name it does not, are both
// seen by the diff the test above uses.
func TestReadmeSurfaceDiffBites(t *testing.T) {
	t.Parallel()
	const readme = "x\n## The surfaces\n\n| surface | for each |\n|---|---|\n| `wire` | `instance` |\n| `bogus` | `x` |\n\n## Next\n| `framing` | y |\n"
	table := readmeSurfaceTable(t, readme)
	if !slices.Equal(table, []string{"wire", "bogus"}) {
		t.Fatalf("parsed %v, want [wire bogus]", table)
	}
	if d := surfaceSetDiff(table, surfaces); !slices.Equal(d, []string{"bogus"}) {
		t.Errorf("an extra README name was not seen: %v", d)
	}
	if d := surfaceSetDiff(surfaces, table); !slices.Contains(d, "message") {
		t.Errorf("a surface missing from the README was not seen: %v", d)
	}
}
