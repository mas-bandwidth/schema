package luautable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestLuauFixedFormRaggedTail is the row fixed_form_ragged_tail (schema#876
// F8): a record region that is not a whole number of records is MALFORMED,
// named `truncated`, the flag and the name set together and nothing landed
// (docs/FIXED-FORM-ALGORITHM.md §5.3 step 9). recordBytes is the selected
// lineage entry's, compiled into the reader, so the guard's `<= 8` arm is not
// reachable from a file; this row forges the other arm, rest % recordBytes ~= 0,
// by appending 1 to recordBytes - 1 bytes. extra == recordBytes is one more
// whole record and owes a different answer: this leg's read takes no capacity
// (no `batch_too_large`), so the appended bytes are a record whose own hash is
// zeros, `no_layout`.
func TestLuauFixedFormRaggedTail(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	g := generateFiles(t, map[string]string{"Point.schema": "package probe\n\nfixed table Point\n{\n    x int32 = 0\n    y int32 = 0\n}\n"})
	if g.err != nil {
		t.Fatalf("the Luau unit does not generate: %v", g.err)
	}
	probe := `--!strict
local T = require("./PointTable")
local F = require("./schema_fixed")
local fixed = { hash = T.PointFixedHash, recordBytes = T.PointFixedRecordBytes, layout = T.PointFixedLayout,
	lineage = T.PointFixedLineage, floor = T.PointFixedFloor }
local p = T.NewPoint()
p.X, p.Y = 7, -9
local good = F.save(T.PointDescriptor, fixed, { p })
local fails = 0
local function check(ok: boolean, what: string)
	if not ok then fails += 1; print("FAIL: " .. what) end
end
local report: any = {}
local back = F.read(T.PointDescriptor, fixed, good, report)
check(back ~= nil and #back == 1 and back[1].X == 7 and back[1].Y == -9, "the good file does not read back")
local stride = 8 + T.PointFixedRecordBytes
for extra = 1, stride do
	local ragged = buffer.create(buffer.len(good) + extra)
	buffer.copy(ragged, 0, good, 0, buffer.len(good))
	local r: any = {}
	local out = F.read(T.PointDescriptor, fixed, ragged, r)
	if extra < stride then
		check(out == nil and r.reason == "truncated" and r.malformed == true,
			"extra=" .. extra .. ": a ragged tail owes truncated with malformed: " .. tostring(r.reason) .. " " .. tostring(r.malformed))
		check(r.clamped == 0 and r.unknown == 0 and r.widened == 0, "extra=" .. extra .. ": a malformed read moved a counter")
	else
		check(out == nil and r.reason == "no_layout" and r.malformed == false,
			"extra == recordBytes is a whole record whose hash names no layout: " .. tostring(r.reason))
	end
end
if fails == 0 then print("ok") end
`
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", nil)))
	if out != "ok" {
		t.Fatalf("ragged tail:\n%s", out)
	}
}
