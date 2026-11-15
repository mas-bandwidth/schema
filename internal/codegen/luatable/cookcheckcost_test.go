package luatable

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// overlapCostSchema puts many arrays in one node: every Row of the root's
// rows list holds a non-empty list of its own, and all of those lists are
// carved from the root's node extent (docs/SPEC-TABLES.md §2.9).
const overlapCostSchema = `package overlapcost

table Row
{
    items []int32
}

table Root
{
    rows []Row
}
`

// TestLuaCookCheckArrayOverlapIsNearLinear holds that Check's no-overlap
// clause (docs/SPEC-TABLES.md §7.4) costs time near linear in the cook it
// checks (docs/SECURITY.md: hostile input costs bounded time), counted in Lua
// VM instructions and never by a clock. The leg's own writer cooks a root of
// 2,000 and of 4,000 rows, each row's list in the root's node; doubling the
// cook may at most multiply Check's instruction count by 2.5 (n log n is about
// 2.1 for this doubling, a check that compares every pair about 4). Both cooks
// are still accepted, and a cook with two of those arrays planted to overlap,
// in either order, is still refused.
func TestLuaCookCheckArrayOverlapIsNearLinear(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Lua 5.4 functional probe")
	path := filepath.Join(t.TempDir(), "overlapcost.schema")
	if err := os.WriteFile(path, []byte(overlapCostSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, path)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	luarun.WriteHexProbe(t, dir)
	probe := `local T = require("` + ModuleName(u) + `")
local cook = require("cook")
local cookwrite = require("cookwrite")
local hexline = require("hexline")

local function cooked(rows)
  local v = T.NewRoot()
  for i = 1, rows do
    local r = T.NewRow()
    r.items = { i, i + 1, i + 2 }
    v.rows[i] = r
  end
  local c = cookwrite.Cook(T, "Root", v, nil, 1)
  assert(c ~= nil, "the writer refused its own value")
  return c
end

local function steps(c)
  local n = 0
  debug.sethook(function()
    n = n + 1
  end, "", 1)
  local h, why = cook.Check(T, "Root", c)
  debug.sethook()
  return n, h ~= nil and "open" or why
end

local small, smallVerdict = steps(cooked(2000))
local large, largeVerdict = steps(cooked(4000))
print(small, smallVerdict, large, largeVerdict)
hexline(cooked(2))
`
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := luarun.Try(t, dir, "main.lua", nil)
	if err != nil {
		t.Fatalf("probe: %v\n%s", err, stderr)
	}
	lines := strings.SplitN(strings.TrimRight(stdout, "\n"), "\n", 2)
	if len(lines) != 2 {
		t.Fatalf("probe printed %q", stdout)
	}
	var small, large int64
	var smallVerdict, largeVerdict string
	if _, err := fmt.Sscanf(lines[0], "%d %s %d %s", &small, &smallVerdict, &large, &largeVerdict); err != nil || small == 0 {
		t.Fatalf("probe printed %q", lines[0])
	}
	t.Logf("Check: 2,000 rows %d VM instructions, 4,000 rows %d (x%.2f)", small, large, float64(large)/float64(small))
	if smallVerdict != "open" || largeVerdict != "open" {
		t.Fatalf("the writer's lawful cooks were refused: 2,000 rows %q, 4,000 rows %q", smallVerdict, largeVerdict)
	}
	if float64(large) > 2.5*float64(small) {
		t.Errorf("doubling the cook multiplied Check's instructions by %.2f (%d to %d), past 2.5: not near linear", float64(large)/float64(small), small, large)
	}

	// THE VERDICT IS KEPT: two rows' lists in the root's node, planted to
	// overlap, refuse whichever of the two is placed first.
	valid := luarun.HexLines(t, []byte(lines[1]))[0]
	if got := runCookProbe(t, u, "Root", valid); got != "open" {
		t.Fatalf("the unplanted two-row cook answered %q, want open", got)
	}
	m := tabletext.NewModel(u)
	h, err := tablecook.ReadHeader(valid, ir.BuildVersion(u))
	if err != nil {
		t.Fatal(err)
	}
	le := binary.LittleEndian
	rowsSlot := cookSlot(t, u, m, "Root", "rows", 0)
	rowsStart := rowsSlot + int64(le.Uint64(valid[h.DataOffset()+rowsSlot:]))
	rowSize := ir.RecordLayout(u, m.Lookup("Row")).Size
	itemsSlot := cookSlot(t, u, m, "Row", "items", 0)
	slot := func(row int) int64 { return rowsStart + int64(row)*rowSize + itemsSlot }
	target := func(row int) int64 { return slot(row) + int64(le.Uint64(valid[h.DataOffset()+slot(row):])) }
	for _, tc := range []struct {
		name     string
		from, to int
	}{
		{"the later row's list onto the earlier's", 1, 0},
		{"the earlier row's list onto the later's", 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := append([]byte(nil), valid...)
			le.PutUint64(f[h.DataOffset()+slot(tc.from):], uint64(target(tc.to)-slot(tc.from)))
			if got := runCookProbe(t, u, "Root", f); got != "bad_layout" {
				t.Errorf("got %q, want bad_layout", got)
			}
		})
	}
}
