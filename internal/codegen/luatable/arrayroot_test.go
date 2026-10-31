package luatable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestLuaJsonArrayRoot holds docs/SPEC-TABLES.md §16.10 (the array root) in
// the Lua backend, through a generated unit compiled and run in a t.TempDir(),
// against the Go oracle (internal/codegen/gotable,
// TestGoTableStringArraysAndJSONArrayRoot): a table `| json = "array"` over
// one array field writes and reads as a bare JSON array, an object where the
// array must be is refused by name (json_root_not_array) at the root, and
// nested a by-value field of the array-root table is the array too, an object
// there being an ordinary kind mismatch.
func TestLuaJsonArrayRoot(t *testing.T) {
	t.Parallel()
	work := t.TempDir()
	schema := `package probe

table GoalList | json = "array"
{
    goals []string(8)
}

table Holder
{
    g GoalList
}
`
	path := filepath.Join(work, "probe.schema")
	if err := os.WriteFile(path, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, path)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)

	main := `local T = require("ProbeTables")
local J = require("json")

local function report()
  return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end

-- THE ARRAY ROOT WRITE (§16.10): the table's text IS its one field's array
local v = T.NewGoalList()
v.goals = { { value = "a" }, { value = "b" } }
io.write(J.ToJson(T, "GoalList", v))

-- THE ARRAY ROOT READ: a top-level array fills the field by its row
local r1 = report()
local g = J.FromJsonTolerant(T, "GoalList", '["a","b"]', r1)
print(#g.goals, g.goals[1].value, g.goals[2].value, r1.malformed)

-- AN OBJECT AT AN ARRAY ROOT: refused by name
local r2 = report()
J.FromJsonTolerant(T, "GoalList", '{"goals":["a"]}', r2)
print(r2.malformed, r2.Reason or "")

-- NESTED (§16.10): a by-value field of the array-root table is the array
local r3 = report()
local h = J.FromJsonTolerant(T, "Holder", '{"g":["x","y"]}', r3)
print(h.g.goals[1].value, h.g.goals[2].value, r3.malformed)
io.write(J.ToJson(T, "Holder", h))

-- AN OBJECT WHERE THE NESTED ARRAY ROOT IS: the ordinary kind mismatch
local r4 = report()
J.FromJsonTolerant(T, "Holder", '{"g":{"goals":["a"]}}', r4)
print(r4.kindMismatch, r4.malformed)
`
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", nil)), "\n")
	want := strings.Join([]string{
		`[
  "a",
  "b"
]`,
		"2	a	b	false",
		"true	json_root_not_array: GoalList",
		"x	y	false",
		`{
  "g": [
    "x",
    "y"
  ]
}`,
		"1	false",
	}, "\n")
	if got != want {
		t.Fatalf("array root\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
