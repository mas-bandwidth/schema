package mootable_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// listdemoUnit is the conformance unit listdemo (tables/lists).
func listdemoUnit(t *testing.T) *ir.Unit {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "tables", "lists", "*.schema"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no listdemo schema files: %v", err)
	}
	sort.Strings(paths)
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("listdemo does not load: %v", err)
	}
	return u
}

// countCrossProbe reads the input at listdemo's Album through the one read
// and prints whether a root came back.
const countCrossProbe = `local report = load.newReport()
local v = load.ReadStrict("Album", inputs.wire, report)
print(tostring(v ~= nil))
`

// TestMooCountCrossingLengthIsRefused: a list count whose spelling crosses
// its L (an Album whose photos count spells a value above the int32 storage
// cap) is refused whole. An array header lies inside its own L
// (docs/plans/strict-only.md decision 7), so the crossing is damage; the Go
// engine's region load refuses the same wire. The verdict is the contract:
// which reason each names is not compared (docs/rulings.json
// refusal-verdict-only), so the old crossing-versus-cap control is retired.
func TestMooCountCrossingLengthIsRefused(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_load.moo)")
	t.Parallel()
	wire, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", "tables", "fuzz-vectors", "list_count_cross_length.bin"))
	if err != nil {
		t.Fatal(err)
	}
	u := listdemoUnit(t)
	m := tabletext.NewModel(u)
	var rep tabletext.Report
	if _, err := tablewire.DecodeRegion(m, m.New(u.Tables["Album"]), wire, &rep); !tablewire.ReadRefusal(err) || !rep.Refused {
		t.Fatalf("the Go engine's region load does not refuse: %v %+v", err, rep)
	}
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	g := writeUnit(t, u, files)
	if got := g.run(t, "--!strict\nlocal inputs = require(\"./inputs\")\nlocal load = require(\"./table_load\")\n"+g.requireAll()+countCrossProbe,
		map[string][]byte{"wire": wire}); got != "false" {
		t.Fatalf("moo's one read returns a root for the crossing count: %s", got)
	}
}
