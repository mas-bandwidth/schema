package luautable_test

import (
	"encoding/hex"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
)

// TestLuauTableSaveSkipsFalseGuards holds the elision of a field under a
// FALSE GUARD (docs/SPEC-TABLES.md §3): whatever its storage holds, an if
// branch that does not run writes none of its fields, and measure agrees with
// save byte for byte (§7). The control in each case is the same instance with
// the guard flipped, which must write the guarded field.
func TestLuauTableSaveSkipsFalseGuards(t *testing.T) {
	t.Parallel()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, "tables", "examples", "Guarded.schema")})
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
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	desc := string(files["GuardedTable.luau"])
	assertGuard := func(field string, want bool) {
		t.Helper()
		idx := strings.Index(desc, `name = "`+field+`"`)
		if idx < 0 {
			t.Fatalf("field %q not found in the descriptor", field)
		}
		end := strings.Index(desc[idx:], "\n")
		line := desc[idx : idx+end]
		if got := strings.Contains(line, "guard"); got != want {
			t.Errorf("field %q: guard present = %v, want %v; line: %s", field, got, want, line)
		}
	}
	assertGuard("active", false)
	assertGuard("speed", true)
	assertGuard("has_target", true)
	assertGuard("target_id", true)
	assertGuard("wander", true)
	assertGuard("note", true)

	main := `local save = require("./schema_save")
local St = require("./schema_tables")
require("./GuardedTable")
local d = St.lookup("Patrol")
local function patrol(o)
	local v = {
		Active = o.active or false,
		Speed = o.speed or 1.0,
		HasTarget = o.has_target or false,
		TargetId = o.target_id or 0,
		Wander = o.wander or 0.5,
		Note = buffer.create(8),
		NoteLength = 0,
	}
	if o.note then
		buffer.fill(v.Note, 0, 0)
		buffer.writestring(v.Note, 0, o.note)
		v.NoteLength = #o.note
	end
	return v
end
local function emit(name, v)
	local out = save.save(d, v)
	local m = save.measure(d, v)
	if out == nil or m == nil then print("R " .. name .. " NIL"); return end
	local parts = table.create(buffer.len(out))
	for i = 0, buffer.len(out) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(out, i))
	end
	print("R " .. name .. " " .. table.concat(parts))
	print("M " .. name .. " " .. m)
end
emit("a", patrol({active = false, speed = 7.0, note = "hi"}))
emit("b", patrol({active = false, note = "hi"}))
emit("c", patrol({active = true, speed = 7.0, note = "hi"}))
emit("d", patrol({active = true, has_target = false, target_id = 5, wander = 2.0}))
emit("e", patrol({active = true, has_target = false, wander = 2.0}))
emit("f", patrol({active = true, has_target = true, target_id = 5}))
`
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", nil)))
	saved := map[string][]byte{}
	measure := map[string]int{}
	for line := range strings.SplitSeq(out, "\n") {
		f := strings.Fields(line)
		if len(f) != 3 {
			t.Fatalf("unexpected output line %q", line)
		}
		switch f[0] {
		case "R":
			if f[2] == "NIL" {
				t.Fatalf("the save refused for %q:\n%s", f[1], out)
			}
			b, derr := hex.DecodeString(f[2])
			if derr != nil {
				t.Fatalf("bad hex line %q: %v", line, derr)
			}
			saved[f[1]] = b
		case "M":
			n, aerr := strconv.Atoi(f[2])
			if aerr != nil {
				t.Fatalf("bad measure line %q: %v", line, aerr)
			}
			measure[f[1]] = n
		default:
			t.Fatalf("unexpected output line %q", line)
		}
	}
	for _, name := range []string{"a", "b", "c", "d", "e", "f"} {
		if _, ok := saved[name]; !ok {
			t.Fatalf("no save output for %q; got:\n%s", name, out)
		}
		if measure[name] != len(saved[name]) {
			t.Errorf("case %s: measure %d != saved length %d", name, measure[name], len(saved[name]))
		}
	}
	if string(saved["a"]) != string(saved["b"]) {
		t.Errorf("case a: active=false with speed=7.0 and note=\"hi\" saved %d bytes; active=false with only note=\"hi\" saved %d bytes; %s", len(saved["a"]), len(saved["b"]), firstDiff(saved["a"], saved["b"]))
	}
	if string(saved["c"]) == string(saved["a"]) {
		t.Errorf("control a: active=true with speed=7.0 and note=\"hi\" must write speed, but saved the same %d bytes as active=false", len(saved["c"]))
	}
	if string(saved["d"]) != string(saved["e"]) {
		t.Errorf("case b: active=true has_target=false with target_id=5 saved %d bytes; without target_id saved %d bytes; %s", len(saved["d"]), len(saved["e"]), firstDiff(saved["d"], saved["e"]))
	}
	if string(saved["f"]) == string(saved["d"]) {
		t.Errorf("control b: active=true has_target=true with target_id=5 must write target_id, but saved the same %d bytes as has_target=false", len(saved["f"]))
	}
}
