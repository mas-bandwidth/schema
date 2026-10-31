package jstable

// THE WIRE AND REPORT SURFACES' ROW LIST (docs/SPEC-TABLES.md §3, §4;
// test/conformance/README.md, "Exit codes"): the driver's `wireRows` table.
//
// The wire and report surfaces answer a manifest row only when its (unit, root)
// pair is in `wireRows`; every other row is written `<case>.absent`, which the
// matrix counts as the work left. A pair the manifest names and the table
// leaves out therefore reads ABSENT however completely the generated tree
// carries it, which is how the stream, list, backend, vocabulary and
// evolution-pair units sat at 36 wire and 23 report absences while their
// json-read and json-write rows, through the same modules, passed. So the
// table is held to the manifest's own pairs, minus a NAMED set of pairs this
// target does not generate, each with the reason beside it.

import (
	"bufio"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// wireRowsUncarried is the manifest's (unit, root) pairs the wire and report
// surfaces answer `<case>.absent` for, each by the reason the target refuses
// the unit. It is pinned exactly: a pair dropping out of `wireRows` without a
// line here fails, and a line here whose pair the table binds fails too.
// It is empty: every pair the manifest names is bound.
var wireRowsUncarried = map[string]string{}

// TestWireRowsConformanceJavaScript holds the driver's wireRows table to the
// manifest's instance and report pairs, less the named uncarried set.
func TestWireRowsConformanceJavaScript(t *testing.T) {
	t.Parallel()

	manifest := manifestWireRoots(t)
	driver := driverWireRoots(t)

	t.Run("row_list_is_the_manifest", func(t *testing.T) {
		t.Parallel()
		if problems := wireRowGaps(manifest, driver, wireRowsUncarried); len(problems) > 0 {
			t.Fatalf("the JavaScript driver's wireRows table is not the manifest's pairs less the uncarried set:\n  %s", strings.Join(problems, "\n  "))
		}
	})

	// THE REVERSED WITNESS: plant the old table back (a manifest pair the
	// driver does not bind and no uncarried line names) and the check must
	// name it, and a pair the driver binds that the uncarried set still lists
	// must be named too. A check that passes both plantings guards nothing.
	t.Run("reversed_witness", func(t *testing.T) {
		t.Parallel()
		for _, dropped := range []string{"streamdemo/Feed", "listdemo/Save", "tbla2/Root", "backenddemo/LoginRequest"} {
			if !slices.Contains(driver, dropped) {
				t.Fatalf("the witness pair %s is not in the driver's wireRows table", dropped)
			}
			var planted []string
			for _, r := range driver {
				if r != dropped {
					planted = append(planted, r)
				}
			}
			if len(wireRowGaps(manifest, planted, wireRowsUncarried)) == 0 {
				t.Errorf("dropping %s from wireRows was not caught", dropped)
			}
		}
		stale := map[string]string{"streamdemo/Feed": "planted"}
		maps.Copy(stale, wireRowsUncarried)
		if len(wireRowGaps(manifest, driver, stale)) == 0 {
			t.Error("an uncarried line naming a pair the driver binds was not caught")
		}
	})
}

// wireRowGaps names every way the driver's table differs from the manifest's
// pairs less the uncarried set: a pair left unbound, a pair bound that the
// manifest does not name, and an uncarried line whose pair is bound or absent
// from the manifest.
func wireRowGaps(manifest, driver []string, uncarried map[string]string) []string {
	bound := map[string]bool{}
	for _, r := range driver {
		bound[r] = true
	}
	named := map[string]bool{}
	var problems []string
	for _, r := range manifest {
		named[r] = true
		_, exempt := uncarried[r]
		switch {
		case !bound[r] && !exempt:
			problems = append(problems, r+": the manifest names it and wireRows does not bind it, so every row reads ABSENT")
		case bound[r] && exempt:
			problems = append(problems, r+": wireRows binds it and the uncarried set still lists it")
		}
	}
	for _, r := range driver {
		if !named[r] {
			problems = append(problems, r+": wireRows binds a pair no manifest instance or report row names")
		}
	}
	for r := range uncarried {
		if !named[r] {
			problems = append(problems, r+": the uncarried set names a pair the manifest does not")
		}
	}
	sort.Strings(problems)
	return problems
}

// manifestWireRoots is the manifest's distinct (unit, root) pairs over its
// `instance` and `report` lines, the rows the wire and report surfaces read.
func manifestWireRoots(t *testing.T) []string {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "..", "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	seen := map[string]bool{}
	var roots []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) < 4 || (fields[0] != "instance" && fields[0] != "report") {
			continue
		}
		key := fields[2] + "/" + fields[3]
		if !seen[key] {
			seen[key] = true
			roots = append(roots, key)
		}
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(roots)
	if len(roots) == 0 {
		t.Fatal("the manifest names no instance or report row — this gate would pass over the empty set")
	}
	return roots
}

// driverWireRoots is the driver's `wireRows` table, one
// `["<unit>", "<root>", "<module>"]` line a row, as sorted pairs.
func driverWireRoots(t *testing.T) []string {
	t.Helper()
	source := driverSource(t)
	start := strings.Index(source, "const wireRows = [")
	if start < 0 {
		t.Fatal("the JavaScript driver declares no wireRows table (test/conformance/js/main.mjs)")
	}
	end := strings.Index(source[start:], "];")
	if end < 0 {
		t.Fatal("the wireRows table is not closed: no `];` follows `const wireRows = [`")
	}
	re := regexp.MustCompile(`\["([A-Za-z0-9_]+)", "([A-Za-z0-9_]+)", "[A-Za-z0-9_./]+"\]`)
	var roots []string
	for _, m := range re.FindAllStringSubmatch(source[start:start+end], -1) {
		roots = append(roots, m[1]+"/"+m[2])
	}
	sort.Strings(roots)
	if len(roots) == 0 {
		t.Fatal("the wireRows table carries no row (test/conformance/js/main.mjs)")
	}
	return roots
}
