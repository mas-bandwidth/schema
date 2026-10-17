package luautable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
)

// TestLuauStrictReadRefusesByName drives the strict read's NAMED refusals
// (docs/SPEC-TABLES.md §6.5, §3, §2.14): a report carrying any of clamped,
// unknown, kind_mismatch, duplicate or malformed refuses naming the counter
// and the first field it saw, and the form byte's refusals — a form-3 file
// at a variable root is previous_form, an unknown form byte is named.
func TestLuauStrictReadRefusesByName(t *testing.T) {
	t.Parallel()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, "tables", "examples")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range tables {
		files[name] = data
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const main = `local M = require("./refusal")

local expected = {
	"clamped:a",
	"unknown:b",
	"kind_mismatch:c",
	"duplicate:d",
	"malformed:e",
	"previous_form",
	"unknown_form_byte",
	"malformed",
}

local counters = {
	{ name = "clamped", field = "a", clamped = 1 },
	{ name = "unknown", field = "b", unknown = 1 },
	{ name = "kind_mismatch", field = "c", kind_mismatch = 1 },
	{ name = "duplicate", field = "d", duplicate = 1 },
	{ name = "malformed", field = "e", malformed = true },
}

for _, c in counters do
	local function load(_: buffer, _: any, rep: any): (any, boolean)
		if c.clamped then rep.clamped = c.clamped end
		if c.unknown then rep.unknown = c.unknown end
		if c.kind_mismatch then rep.kind_mismatch = c.kind_mismatch end
		if c.duplicate then rep.duplicate = c.duplicate end
		if c.malformed then rep.malformed = true end
		rep.field = c.field
		return {}, true
	end
	local w = buffer.create(1)
	buffer.writeu8(w, 0, 1)
	local _, reason = M.ReadStrict(w, {}, load)
	print(reason)
end

local function formwire(b: number): buffer
	local w = buffer.create(1)
	buffer.writeu8(w, 0, b)
	return w
end
local function okload(): (any, boolean) return {}, true end
local _, r3 = M.ReadStrict(formwire(3), {}, okload)
print(r3)
local _, r5 = M.ReadStrict(formwire(5), {}, okload)
print(r5)
local _, r0 = M.ReadStrict(buffer.create(0), {}, okload)
print(r0)
`
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", nil)))
	got := strings.Split(out, "\n")
	want := []string{
		"clamped:a", "unknown:b", "kind_mismatch:c", "duplicate:d", "malformed:e",
		"previous_form", "unknown_form_byte", "malformed",
	}
	if len(got) != len(want) {
		t.Fatalf("printed %d lines, want %d:\n%s", len(got), len(want), out)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %q, want %q", i, got[i], want[i])
		}
	}
}
