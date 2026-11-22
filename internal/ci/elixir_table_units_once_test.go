package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// elixirTableUnitRepeats reads the ELIXIR_TABLE_UNITS assignment out of a
// make file and returns every unit name listed more than once.
func elixirTableUnitRepeats(mk string) ([]string, bool) {
	m := regexp.MustCompile(`(?ms)^ELIXIR_TABLE_UNITS := (.*?[^\\])\n`).FindStringSubmatch(mk)
	if m == nil {
		return nil, false
	}
	seen := map[string]int{}
	var repeats []string
	for field := range strings.FieldsSeq(strings.ReplaceAll(m[1], "\\\n", " ")) {
		name, _, _ := strings.Cut(field, ":")
		seen[name]++
		if seen[name] == 2 {
			repeats = append(repeats, name)
		}
	}
	return repeats, true
}

// The Elixir table units generate side by side (build/tables-generated-elixir
// /.stamp runs them through xargs -P), and each removes its own directory
// first: a unit listed twice is two writers racing on one directory, and the
// loser's compile finds a file the winner just removed. The list names each
// unit once, and the control is main's list as it stood, with tblrt1 twice.
func TestElixirTableUnitsAreListedOnce(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "make", "elixir.mk"))
	if err != nil {
		t.Fatal(err)
	}
	repeats, ok := elixirTableUnitRepeats(string(raw))
	if !ok {
		t.Fatal("make/elixir.mk sets no ELIXIR_TABLE_UNITS")
	}
	if len(repeats) > 0 {
		t.Errorf("make/elixir.mk lists these Elixir table units more than once, so their generation races: %v", repeats)
	}

	control := "ELIXIR_TABLE_UNITS := tabledemo:tables/examples \\\n" +
		"\ttblp3:test/tables/P3.schema tblrt1:test/tables/RT1.schema \\\n" +
		"\ttblrt1:test/tables/RT1.schema tblrt2:test/tables/RT2.schema\n\nnext: rule\n"
	repeats, ok = elixirTableUnitRepeats(control)
	if !ok || len(repeats) != 1 || repeats[0] != "tblrt1" {
		t.Errorf("the control list with tblrt1 twice gives %v (parsed %v), want [tblrt1]", repeats, ok)
	}
}
