package listwalk_test

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/listwalk"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestListWalkHalfRidesTheListBearingUnits is the LIST-WALK GATE
// (docs/SPEC-TABLES.md §2.9, §13.5, §16). Three claims, one per unit of the
// C++ table corpus, and the LIST-FREE SET IS DERIVED FROM THE SCHEMAS:
//
//  1. a LIST-BEARING unit's every generated .cpp carries the list half;
//  2. a LIST-FREE unit's carries none of it, which is §2.2's zero-cost
//     property holding for the text form;
//  3. the half is ONE half, the same bytes wherever it rides.
//
// `make tables-json-list-walk` points SCHEMA_LIST_WALK_DIR at the generated
// tree. Without it this test still derives the answer for every unit of the
// corpus and holds the derivation itself honest, which is what keeps
// `go test ./...` saying something about the fact the gate is built on.
func TestListWalkHalfRidesTheListBearingUnits(t *testing.T) {
	dir := os.Getenv("SCHEMA_LIST_WALK_DIR")
	// SCHEMA_LIST_WALK_UNITS narrows the scan to named generated directories,
	// which is what a NEGATIVE CONTROL needs: a control regenerates one or two
	// units, and a run that also went red over thirty-five missing trees would
	// be red for the wrong reason.
	only := map[string]bool{}
	for name := range strings.SplitSeq(os.Getenv("SCHEMA_LIST_WALK_UNITS"), ",") {
		if name != "" {
			only[name] = true
		}
	}

	units := corpus(t)
	var bearingUnits, freeUnits, bearingFiles, freeFiles int
	firstName, firstHalf := "", ""

	for _, unit := range units {
		if len(only) > 0 && !only[unit.Dir] {
			continue
		}
		lists := listwalk.Bearing(load(t, unit.Source))
		if len(lists) > 0 {
			bearingUnits++
		} else {
			freeUnits++
		}
		if dir == "" {
			continue
		}
		sources, err := filepath.Glob(filepath.Join(dir, unit.Dir, "*Table.cpp"))
		if err != nil {
			t.Fatalf("%s: %v", unit.Dir, err)
		}
		sort.Strings(sources)
		for _, source := range sources {
			text, err := os.ReadFile(source)
			if err != nil {
				t.Fatalf("%s: %v", source, err)
			}
			half, carried := listwalk.Half(string(text))
			switch {
			case len(lists) == 0 && carried:
				freeFiles++
				t.Errorf("LIST-WALK GATE FAILED: the list half reached the list-free unit %s.\n"+
					"No table in %s's closure carries an unbounded array", source, unit.Source)
			case len(lists) > 0 && !carried:
				bearingFiles++
				t.Errorf("LIST-WALK GATE FAILED: no list half in %s.\n"+
					"%s declares %s", source, unit.Source, strings.Join(lists, ", "))
			case carried:
				bearingFiles++
				if firstHalf == "" {
					firstName, firstHalf = source, half
				} else if half != firstHalf {
					t.Errorf("LIST-WALK GATE FAILED: the list half in %s is not the list half in %s.\n%s",
						source, firstName, firstDifference(firstHalf, half))
				}
			default:
				freeFiles++
			}
		}
	}

	// The two claims below are about the WHOLE corpus, so a narrowed run,
	// which is a negative control's, is not asked for them.
	if len(only) == 0 {
		if bearingUnits == 0 || freeUnits == 0 {
			// A derivation that answers the same for every unit holds nothing:
			// it would report green over a corpus of one kind. The corpus
			// carries both, so the instrument owes both answers.
			t.Fatalf("the derived list-free set is %d list-bearing and %d list-free units. "+
				"The corpus carries both kinds, and an instrument that reports one is not deriving anything",
				bearingUnits, freeUnits)
		}
		if dir != "" && firstHalf == "" {
			t.Fatal("LIST-WALK GATE FAILED: not one generated .cpp of the corpus carries a list half")
		}
	}
	// The counts below are what the gate COUNTED, so they are reported only
	// when it held: a red run's numbers describe the failure, not the corpus.
	if dir == "" || t.Failed() {
		return
	}
	summary := "tables list-walk gate: one list half, byte-identical in " + strconv.Itoa(bearingFiles) +
		" .cpp files across " + strconv.Itoa(bearingUnits) + " list-bearing units, and in none of the " +
		strconv.Itoa(freeFiles) + " .cpp files of the " + strconv.Itoa(freeUnits) +
		" list-free ones, the set derived from the schemas\n"
	if path := os.Getenv("SCHEMA_LIST_WALK_SUMMARY"); path != "" {
		if err := os.WriteFile(path, []byte(summary), 0o644); err != nil {
			t.Fatalf("the gate's summary cannot be written: %v", err)
		}
	}
	t.Log(summary)
}

// corpus is every unit `tables_generate` emits, read out of the Makefile
// rather than restated here, so the tree the gate scans and the units it
// derives an answer for are ONE list. A unit added to the build is under the
// gate with no edit to this file.
func corpus(t *testing.T) []listwalk.GeneratedUnit {
	t.Helper()
	text, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatalf("the Makefile carries tables_generate and cannot be read: %v", err)
	}
	units := listwalk.Corpus(string(text))
	if len(units) == 0 {
		t.Fatal("the Makefile's tables_generate names no unit. That define IS the gate's corpus")
	}
	return units
}

// firstDifference names the first line two halves disagree about, which is
// what a reader needs rather than two whole walkers.
func firstDifference(want, got string) string {
	a, b := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(a) || i < len(b); i++ {
		x, y := "", ""
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return "line " + strconv.Itoa(i+1) + " of the half:\n  first: " + x + "\n  this:  " + y
		}
	}
	return "the halves differ in trailing bytes alone"
}

func load(t *testing.T, source string) *ir.Unit {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{"../../" + source})
	if err != nil {
		t.Fatalf("%s: %v", source, err)
	}
	unit, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("%s: %v", source, err)
	}
	return unit
}
