package luatable

// TestLuaStrictReadRefusesByName drives the strict read (runtime/refusal.lua)
// one wire per refusal name from the tolerant load's report: each counter's
// nonzero case refuses by name, one forged form byte per form refusal is
// named, and integrity_mismatch refuses where the unit declares integrity
// (docs/SPEC-TABLES.md §11, §6.5, §2.14).

import (
	"encoding/binary"
	"hash/crc32"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// rowRefusal is the name a report row's counters carry, in the strict read's
// precedence (docs/SPEC-TABLES.md §4, §6.5). A clean row answers "".
func rowRefusal(line string) string {
	f := strings.Split(line, ",")
	if len(f) < 6 {
		return ""
	}
	mismatch, _ := strconv.Atoi(f[1])
	clamped, _ := strconv.Atoi(f[3])
	malformed := f[5] == "true"
	switch {
	case malformed:
		return "malformed"
	case clamped > 0:
		return "clamped"
	case mismatch > 0:
		return "kind_mismatch"
	}
	return ""
}

// onePerRefusal picks the first manifest report row for each refusal the
// strict read owns, from reports.txt's counters.
func onePerRefusal(t *testing.T, manifestPath, reportsPath string) map[string]manifestRow {
	t.Helper()
	expect := reportsExpect(t, reportsPath)
	rows := map[string]manifestRow{}
	for _, row := range manifestRows(t, manifestPath, "report") {
		line, ok := expect[row.caseName]
		if !ok {
			continue
		}
		name := rowRefusal(line)
		if name == "" {
			continue
		}
		if _, have := rows[name]; !have {
			rows[name] = row
		}
	}
	return rows
}

func TestLuaStrictReadRefusesByName(t *testing.T) {
	t.Parallel()
	repo := "../../.."
	manifestPath := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	reportsPath := filepath.Join(repo, "testdata", "conformance", "tables", "reports.txt")
	units := manifestUnits(t, manifestPath)
	rows := onePerRefusal(t, manifestPath, reportsPath)

	for _, name := range []string{"malformed", "clamped", "kind_mismatch"} {
		row, ok := rows[name]
		if !ok {
			t.Fatalf("no manifest report row refuses %s", name)
		}
		t.Run(name, func(t *testing.T) {
			paths := units[row.unit]
			if len(paths) == 0 {
				t.Fatalf("row %s names unknown unit %s", row.caseName, row.unit)
			}
			var full []string
			for _, p := range paths {
				full = append(full, filepath.Join(repo, p))
			}
			u := luarun.LoadUnit(t, full...)
			files, err := Generate(u)
			if err != nil {
				t.Fatalf("unit %s: %v", row.unit, err)
			}
			dir := t.TempDir()
			luarun.WriteFiles(t, dir, files)
			wire, err := os.ReadFile(filepath.Join(repo, row.wire))
			if err != nil {
				t.Fatalf("case %s: %v", row.caseName, err)
			}
			probe := `local T = require("` + ModuleName(u) + `")
local R = require("refusal")
local inputs = require("inputs")
local ok, err = pcall(function() return R.ReadStrict(T, "` + row.root + `", inputs.wire) end)
assert(not ok, "the strict read accepted a ` + name + ` wire")
assert(tostring(err):find("` + name + `", 1, true), "want ` + name + `, got " .. tostring(err))
print("strict-` + name + `-ok")
`
			if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
				t.Fatal(err)
			}
			luarun.Run(t, dir, "main.lua", map[string][]byte{"wire": wire})
		})
	}

	t.Run("duplicate", func(t *testing.T) {
		schemaDir := t.TempDir()
		schema := filepath.Join(schemaDir, "Dup.schema")
		src := `package dupprobe

table Root
{
    entries map[int32]int32
    past    int32
}
`
		if err := os.WriteFile(schema, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		u := luarun.LoadUnit(t, schema)
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		luarun.WriteFiles(t, dir, files)
		probe := `local T = require("` + ModuleName(u) + `")
local R = require("refusal")
local f = T.Tables.Root.fields
local e, a = f[1], f[2]
local k, v = e.key_field, e.value_field
local function leb(n)
  local out = {}
  repeat
    local b = n % 128
    n = n // 128
    if n > 0 then b = b + 128 end
    out[#out + 1] = string.char(b)
  until n == 0
  return table.concat(out)
end
local function fld(ref, kind, payload) return leb(ref) .. string.char(kind) .. payload end
local function i32(n) return string.pack("<i4", n) end
local entry = fld(2, k.kind, i32(7)) .. fld(3, v.kind, i32(1)) .. "\0"
local mapbody = string.char(13) .. leb(2) .. leb(#entry) .. entry .. leb(#entry) .. entry
local wire = "\1" .. fld(1, e.kind, leb(#mapbody) .. mapbody)
  .. fld(4, a.kind, i32(42)) .. "\0"
  .. string.pack("<i8", e.id) .. string.pack("<i8", k.id)
  .. string.pack("<i8", v.id) .. string.pack("<i8", a.id)
  .. string.pack("<i8", 4)
local ok, err = pcall(function() return R.ReadStrict(T, "Root", wire) end)
assert(ok, "the strict read refused duplicate alone: " .. tostring(err))
print("strict-duplicate-clean-ok")
`
		if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
		luarun.Run(t, dir, "main.lua", nil)
	})

	t.Run("forms", func(t *testing.T) {
		u := luarun.LoadUnit(t, filepath.Join(repo, "test", "tables", "G1.schema"))
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		luarun.WriteFiles(t, dir, files)
		probe := `local T = require("` + ModuleName(u) + `")
local R = require("refusal")
local function refuses(kind, wire)
  local ok, err = pcall(function() return R.ReadStrict(T, "Guarded", wire) end)
  assert(not ok and tostring(err):find(kind, 1, true), "want " .. kind .. ", got " .. tostring(err))
end
refuses("unknown_form", "\3" .. string.rep("\0", 20))
refuses("unknown_form", "\9" .. string.rep("\0", 20))
print("strict-forms-ok")
`
		if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
		luarun.Run(t, dir, "main.lua", nil)
	})

	t.Run("integrity", func(t *testing.T) {
		schemaDir := t.TempDir()
		schema := filepath.Join(schemaDir, "Ledger.schema")
		src := `package integrityprobe

table Ledger | integrity = "crc32c"
{
    balance int64
}
`
		if err := os.WriteFile(schema, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		u := luarun.LoadUnit(t, schema)
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		st := u.Tables["Ledger"]
		field := st.Fields[0]
		id := ir.TableFieldWireId(field)
		kind := ir.TableWireFieldKind(field)
		body := append([]byte{1}, byte(kind))
		body = append(body, 7, 0, 0, 0, 0, 0, 0, 0)
		body = append(body, 0)
		wire := append([]byte{1}, body...)
		wire = append(wire, 0, 0, 0, 0, 0, 0, 0, 0)
		binary.LittleEndian.PutUint64(wire[len(wire)-8:], id)
		wire = append(wire, 0, 0, 0, 0, 0, 0, 0, 0)
		binary.LittleEndian.PutUint64(wire[len(wire)-8:], 1)
		wire = append(wire, 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(wire[len(wire)-4:], crc32.Checksum(wire[:len(wire)-4], crc32.MakeTable(crc32.Castagnoli)))
		good := append([]byte(nil), wire...)
		bad := append([]byte(nil), wire...)
		bad[0] = 1
		bad[len(bad)-5] ^= 0xff
		dir := t.TempDir()
		luarun.WriteFiles(t, dir, files)
		probe := `local T = require("` + ModuleName(u) + `")
local R = require("refusal")
local inputs = require("inputs")
local v = R.ReadStrict(T, "Ledger", inputs.good, "` + st.Integrity + `")
assert(v.balance == 7, "the good trailer did not read")
local ok, err = pcall(function() return R.ReadStrict(T, "Ledger", inputs.bad, "` + st.Integrity + `") end)
assert(not ok and tostring(err):find("integrity_mismatch", 1, true), "want integrity_mismatch, got " .. tostring(err))
print("strict-integrity-ok")
`
		if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
		luarun.Run(t, dir, "main.lua", map[string][]byte{"good": good, "bad": bad})
	})
}
