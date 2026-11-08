package luatable

// TestLuaStrictReadRefusesByName drives the strict read (runtime/refusal.lua):
// every report row's verdict is reports.txt's, a repeated map key refuses as
// damage (docs/plans/strict-only.md decision 5), one forged form byte per form
// refusal is named, and integrity_mismatch refuses where the unit declares
// integrity (docs/SPEC-TABLES.md §11, §6.5, §2.14).

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

func TestLuaStrictReadRefusesByName(t *testing.T) {
	t.Parallel()
	repo := "../../.."
	manifestPath := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	reportsPath := filepath.Join(repo, "testdata", "conformance", "tables", "reports.txt")
	units := manifestUnits(t, manifestPath)
	expect := reportsExpect(t, reportsPath)

	// THE VERDICT IS THE CONTRACT (reports.txt; docs/rulings.json
	// refusal-verdict-only): every report row reports.txt calls refused, the
	// strict read refuses, and every row it calls read, the strict read
	// accepts. The name each refusal carries is this leg's own diagnostic.
	t.Run("verdicts", func(t *testing.T) {
		byUnit := map[string][]manifestRow{}
		for _, row := range manifestRows(t, manifestPath, "report") {
			if _, ok := expect[row.caseName]; ok {
				byUnit[row.unit] = append(byUnit[row.unit], row)
			}
		}
		refused, read := 0, 0
		for unit, all := range byUnit {
			var full []string
			for _, p := range units[unit] {
				full = append(full, filepath.Join(repo, p))
			}
			u := luarun.LoadUnit(t, full...)
			// A DECLARED FIXED ROOT's rows are the fixed form's
			// (TestLuaTableLoadMatchesTheReports reads them through ReadFixed);
			// ReadStrict here is the variable root's strict read.
			var rows []manifestRow
			for _, row := range all {
				if st := u.Tables[row.root]; st == nil || !st.FixedDeclared {
					rows = append(rows, row)
				}
			}
			if len(rows) == 0 {
				continue
			}
			files, err := Generate(u)
			if err != nil {
				t.Fatalf("unit %s: %v", unit, err)
			}
			dir := t.TempDir()
			luarun.WriteFiles(t, dir, files)
			var probe strings.Builder
			probe.WriteString("local T = require(\"" + ModuleName(u) + "\")\nlocal R = require(\"refusal\")\nlocal inputs = require(\"inputs\")\n")
			inputs := map[string][]byte{}
			for i, row := range rows {
				wire, err := os.ReadFile(filepath.Join(repo, row.wire))
				if err != nil {
					t.Fatalf("case %s: %v", row.caseName, err)
				}
				key := "w" + strconv.Itoa(i)
				inputs[key] = wire
				probe.WriteString("do\n  local ok, v = pcall(R.ReadStrict, T, \"" + row.root + "\", inputs." + key + ")\n")
				probe.WriteString("  print(\"" + row.caseName + " \" .. ((ok and v ~= nil) and \"read\" or \"refused\"))\nend\n")
			}
			if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe.String()), 0o644); err != nil {
				t.Fatal(err)
			}
			for line := range strings.SplitSeq(strings.TrimSpace(string(luarun.Run(t, dir, "main.lua", inputs))), "\n") {
				name, got, _ := strings.Cut(line, " ")
				if want := expect[name]; got != want {
					t.Errorf("%s (%s): ReadStrict %s, reports.txt %s", name, unit, got, want)
				}
				if got == "refused" {
					refused++
				} else {
					read++
				}
			}
		}
		if refused == 0 || read == 0 {
			t.Fatalf("the verdict rows are one-sided: %d refused, %d read", refused, read)
		}
	})

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
assert(not ok and tostring(err):find("malformed", 1, true), "a repeated map key must refuse as damage, got " .. tostring(err))
print("strict-duplicate-refused-ok")
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
