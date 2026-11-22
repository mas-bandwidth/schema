package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// elixirUnitsListedTwice reads the ELIXIR_TABLE_UNITS assignment out of a
// makefile and returns each unit name listed more than once.
func elixirUnitsListedTwice(mk string) ([]string, error) {
	_, rest, ok := strings.Cut("\n"+mk, "\nELIXIR_TABLE_UNITS := ")
	if !ok {
		return nil, fmt.Errorf("no ELIXIR_TABLE_UNITS := assignment")
	}
	var body strings.Builder
	for line := range strings.SplitSeq(rest, "\n") {
		trimmed := strings.TrimSpace(line)
		body.WriteString(strings.TrimSuffix(trimmed, "\\") + " ")
		if !strings.HasSuffix(trimmed, "\\") {
			break
		}
	}
	seen := map[string]int{}
	var twice []string
	for unit := range strings.FieldsSeq(body.String()) {
		name, _, ok := strings.Cut(unit, ":")
		if !ok {
			return nil, fmt.Errorf("unit %q is not name:path", unit)
		}
		if seen[name]++; seen[name] == 2 {
			twice = append(twice, name)
		}
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("ELIXIR_TABLE_UNITS lists no unit")
	}
	return twice, nil
}

// TestElixirTableUnitsAreListedOnce: make/elixir.mk generates the table units
// side by side (xargs -P), each removing its output directory first, so a
// unit named twice is generated twice at once and one run's directory is
// removed under the other: lang-clean (elixir 4/4) failed on #4530 with
// "openat TableRuntime.ex: no such file or directory" (tblrt1 was listed
// twice). The fixtures: one list with each name once (passes), the same
// list with tblrt1 twice (refused, naming it).
func TestElixirTableUnitsAreListedOnce(t *testing.T) {
	t.Parallel()
	read := func(p string) string {
		t.Helper()
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if twice, err := elixirUnitsListedTwice(read(filepath.Join(repoRoot(t), "make", "elixir.mk"))); err != nil || len(twice) != 0 {
		t.Errorf("make/elixir.mk ELIXIR_TABLE_UNITS lists %v more than once (%v): their side-by-side generation races", twice, err)
	}
	if twice, err := elixirUnitsListedTwice(read(filepath.Join("testdata", "elixir-table-units", "once.mk"))); err != nil || len(twice) != 0 {
		t.Errorf("the control once.mk: %v, %v", twice, err)
	}
	if twice, err := elixirUnitsListedTwice(read(filepath.Join("testdata", "elixir-table-units", "twice.mk"))); err != nil || len(twice) != 1 || twice[0] != "tblrt1" {
		t.Errorf("twice.mk lists tblrt1 twice; the check found %v (%v)", twice, err)
	}
}

// TestElixirBenchStartsItsLongestGatesFirst: make -j starts prerequisites in
// the order they are listed, and the per-leg rules elsewhere append theirs
// after _verify-elixir-bench's own, so the row's two longest gates (the
// tablegen compile and the timed quick run) are named first, or they start
// last and end the row past the cap on a loaded runner (#4530).
func TestElixirBenchStartsItsLongestGatesFirst(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "make", "verify-compiled.mk"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "\n_verify-elixir-bench: verify-tablegen-elixir _verify-elixir-bench-quick ") {
		t.Error("make/verify-compiled.mk _verify-elixir-bench does not name verify-tablegen-elixir and _verify-elixir-bench-quick first")
	}
}
