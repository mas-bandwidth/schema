package viewlisting_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/viewlisting"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// corpus is every unit the C++ backend emits a view for: the units that
// declare a table (docs/SPEC-TABLES.md §8.3, §8.5).
var corpus = []string{
	"../../tables/examples",
	"../../tables/arms",
	"../../tables/backend",
	"../../tables/blobs",
	"../../tables/block",
	"../../tables/blockhome",
	"../../tables/lists",
	"../../tables/maps",
	"../../tables/messages",
	"../../tables/pointers",
	"../../tables/scalars",
	"../../tables/stream",
	"../../tables/vocab",
	"../../tables/vocab9",
}

// TestUnitViewListingMatchesTheIR is the CORPUS GATE (docs/SPEC-TABLES.md
// §8.7). The listing the compiler produces from its own IR is the PIN, and
// the listing a generated program prints from UnitView() must equal it byte
// for byte. The program's half is produced by `make tables-view`, which
// points SCHEMA_VIEW_LISTING_DIR at the directory it wrote the listings into;
// without it this test still holds the pin producible over the whole corpus,
// which is what keeps `go test ./...` honest about the IR half.
func TestUnitViewListingMatchesTheIR(t *testing.T) {
	dir := os.Getenv("SCHEMA_VIEW_LISTING_DIR")
	// SCHEMA_VIEW_LISTING_UNITS narrows the comparison to named packages,
	// which is what a NEGATIVE CONTROL needs: a control sabotages one unit,
	// and a run that also went red over thirteen missing listings would be
	// red for the wrong reason.
	only := map[string]bool{}
	for name := range strings.SplitSeq(os.Getenv("SCHEMA_VIEW_LISTING_UNITS"), ",") {
		if name != "" {
			only[name] = true
		}
	}
	for _, path := range corpus {
		unit := load(t, path)
		if len(only) > 0 && !only[unit.Package] {
			continue
		}
		pin := viewlisting.Listing(unit)
		if strings.Count(pin, "\n") < 3 {
			t.Fatalf("%s: the listing carries %d lines. A unit's registry is never that small",
				path, strings.Count(pin, "\n"))
		}
		if dir == "" {
			continue
		}
		file := filepath.Join(dir, unit.Package+".listing")
		printed, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("%s: the generated program's listing is missing: %v", path, err)
		}
		if string(printed) != pin {
			t.Errorf("%s: the generated program's listing is not the compiler's.\n%s", path, firstDifference(pin, string(printed)))
		}
	}
}

// firstDifference names the first line the two listings disagree about, which
// is what a reader needs to see rather than two whole listings.
func firstDifference(pin, printed string) string {
	want, got := strings.Split(pin, "\n"), strings.Split(printed, "\n")
	for i := 0; i < len(want) || i < len(got); i++ {
		w, g := "", ""
		if i < len(want) {
			w = want[i]
		}
		if i < len(got) {
			g = got[i]
		}
		if w != g {
			return "line " + itoa(i+1) + ":\n  compiler: " + w + "\n  program:  " + g
		}
	}
	return "the listings differ in trailing bytes alone"
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}

func load(t *testing.T, path string) *ir.Unit {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{path})
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	unit, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return unit
}
