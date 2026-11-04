package luatable

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestLuaFixedProofSuiteValues runs all 49 contract-independent fixed-table
// rows through the shared fixedproof value suite against the live C++ oracle.
func TestLuaFixedProofSuiteValues(t *testing.T) {
	t.Parallel()
	fixedproof.RunValueSuite(t, luaFixedProofRunner{})
}

// TestLuaFixedProofSuiteNegativeControl runs both the file negative control
// and the writer negative control across all 49 rows under a hard 2 GiB memory
// ceiling.
func TestLuaFixedProofSuiteNegativeControl(t *testing.T) {
	t.Parallel()
	fixedproof.RunNegativeControlSuite(t, luaFixedProofRunner{})
}

// TestLuaFixedProofEvolution runs the 16 non-value fixed-table rows
// (fixed-file-roundtrip and the 15 evolution/layout/plan/lineage/file rows)
// through the shared fixedproof evolution suite.
func TestLuaFixedProofEvolution(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	fixedproof.RunEvolutionSuite(t, repo, luaFixedProofRunner{})
}

type luaFixedProofRunner struct{}

func (luaFixedProofRunner) Name() string { return "lua" }

func (luaFixedProofRunner) Save(t *testing.T, tc fixedproof.RowCase, u *ir.Unit) []byte {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("%s: luatable.Generate: %v", tc.ID, err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	mod := ModuleName(u)
	setup := luaSetupFor(tc.ID)
	script := fmt.Sprintf(`local T = require(%[1]q)
local r = {}
for i = 1, %[3]d do
  r[i] = T.New%[2]s()
end
%[4]s
local m = T.FixedMeasure(%[2]q, %[3]d)
local wire = T.FixedSave(%[2]q, r)
assert(#wire == m, string.format("FixedMeasure=%%d #FixedSave=%%d", m, #wire))
local f = io.open("out.bin", "wb")
f:write(wire)
f:close()
`, mod, tc.Root, tc.Count, setup)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	luarun.Run(t, dir, "main.lua", nil)
	out, err := os.ReadFile(filepath.Join(dir, "out.bin"))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (luaFixedProofRunner) LoadAndResave(t *testing.T, tc fixedproof.RowCase, u *ir.Unit, wireBytes []byte) ([]byte, error) {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		return nil, err
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	mod := ModuleName(u)
	check := luaCheckFor(tc.ID)
	script := fmt.Sprintf(`local T = require(%[1]q)
local I = require("inputs")
local rows = T.FixedLoad(%[2]q, I.wire)
assert(#rows == %[3]d, string.format("#rows=%%d want %[3]d", #rows))
%[4]s
local re = T.FixedSave(%[2]q, rows)
local f = io.open("resaved.bin", "wb")
f:write(re)
f:close()
`, mod, tc.Root, tc.Count, check)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(script), 0o644); err != nil {
		return nil, err
	}
	stdout, stderr, runErr := luarun.Try(t, dir, "main.lua", map[string][]byte{"wire": wireBytes})
	if runErr != nil {
		return nil, fmt.Errorf("%v: %s %s", runErr, strings.TrimSpace(stderr), strings.TrimSpace(stdout))
	}
	return os.ReadFile(filepath.Join(dir, "resaved.bin"))
}

func (luaFixedProofRunner) SaveMutated(t *testing.T, tc fixedproof.RowCase, u *ir.Unit) ([]byte, error) {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		return nil, err
	}
	files["fixed.lua"] = patchLuaFixedWriterForMutation(t, tc.ID, files["fixed.lua"])
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	mod := ModuleName(u)
	setup := luaSetupFor(tc.ID)
	script := fmt.Sprintf(`local T = require(%[1]q)
local r = {}
for i = 1, %[3]d do
  r[i] = T.New%[2]s()
end
%[4]s
local wire = T.FixedSave(%[2]q, r)
local f = io.open("out.bin", "wb")
f:write(wire)
f:close()
`, mod, tc.Root, tc.Count, setup)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(script), 0o644); err != nil {
		return nil, err
	}
	stdout, stderr, runErr := luarun.Try(t, dir, "main.lua", nil)
	if runErr != nil {
		return nil, fmt.Errorf("%v: %s %s", runErr, strings.TrimSpace(stderr), strings.TrimSpace(stdout))
	}
	return os.ReadFile(filepath.Join(dir, "out.bin"))
}

func patchLuaFixedWriterForMutation(t *testing.T, id string, runtime []byte) []byte {
	t.Helper()
	const anchor = "    local body = encode_table(T, name, v)\n"
	const repl = "    local body = encode_table(T, name, v)\n    if i == #values and #body > 0 then body = string.char(body:byte(1) ~ 1) .. body:sub(2) end\n"
	s := string(runtime)
	if strings.Count(s, anchor) != 1 {
		t.Fatalf("%s: expected 1 writer mutation anchor in fixed.lua, found %d", id, strings.Count(s, anchor))
	}
	return []byte(strings.Replace(s, anchor, repl, 1))
}

func (luaFixedProofRunner) LoadLineage(t *testing.T, u *ir.Unit, root string, lineage map[string][]fixedproof.LineageEntry, wireBytes []byte, cap int) (int, string, map[string]string, []byte) {
	t.Helper()
	var files map[string][]byte
	var err error
	if len(lineage) > 0 {
		lmap := map[string][]FixedLineageEntry{}
		for k, entries := range lineage {
			for _, e := range entries {
				lmap[k] = append(lmap[k], FixedLineageEntry{
					Wire:    e.Wire,
					Layout:  e.Layout,
					Record:  e.Record,
					Retired: e.Retired,
					Reason:  e.Reason,
				})
			}
		}
		files, err = GenerateLineage(u, lmap)
	} else {
		files, err = Generate(u)
	}
	if err != nil {
		t.Fatalf("luatable.Generate: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	mod := ModuleName(u)
	capArg := "nil"
	if cap >= 0 {
		capArg = fmt.Sprintf("%d", cap)
	}
	script := fmt.Sprintf(`local T = require(%[1]q)
local I = require("inputs")
local ok, rows = pcall(function() return T.FixedLoad(%[2]q, I.wire, %[3]s) end)
if not ok then
  local msg = tostring(rows)
  local colon = msg:match(": ([^:]+)$")
  if colon then msg = colon end
  print("REFUSED " .. msg)
  os.exit(0)
end
print("OK " .. tostring(#rows))
local function dump_fields(prefix, tbl)
  for k, v in pairs(tbl) do
    local key = prefix .. "." .. tostring(k)
    local vt = type(v)
    if vt == "number" or vt == "boolean" or vt == "string" then
      print("FIELD " .. key .. "=" .. tostring(v))
    elseif vt == "table" then
      dump_fields(key, v)
    end
  end
end
for idx, r in ipairs(rows) do
  if type(r) == "table" then
    dump_fields("r" .. tostring(idx - 1), r)
  end
end
local re = T.FixedSave(%[2]q, rows)
local f = io.open("resaved.bin", "wb")
f:write(re)
f:close()
`, mod, root, capArg)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luarun.Run(t, dir, "main.lua", map[string][]byte{"wire": wireBytes}))
	fields := map[string]string{}
	count := -1
	reason := ""
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if after, ok := strings.CutPrefix(line, "REFUSED "); ok {
			return -1, strings.TrimSpace(after), fields, nil
		}
		if after, ok := strings.CutPrefix(line, "OK "); ok {
			fmt.Sscanf(after, "%d", &count)
			reason = "ok"
		} else if after, ok := strings.CutPrefix(line, "FIELD "); ok {
			if k, v, ok := strings.Cut(after, "="); ok {
				fields[k] = v
			}
		}
	}
	var resaved []byte
	if count >= 0 {
		resaved, _ = os.ReadFile(filepath.Join(dir, "resaved.bin"))
	}
	return count, reason, fields, resaved
}

func (luaFixedProofRunner) SaveCustom(t *testing.T, u *ir.Unit, root string, scenario string) ([]byte, string) {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("luatable.Generate: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	mod := ModuleName(u)
	var body string
	switch scenario {
	case "batch_over_4096":
		body = fmt.Sprintf(`local r = {}
for i = 1, 4097 do r[i] = T.New%[1]s() end
local ok, err = pcall(function() return T.FixedSave(%[1]q, r) end)
if not ok then
  local msg = tostring(err)
  local colon = msg:match(": ([^:]+)$")
  if colon then msg = colon end
  print("REFUSED " .. msg)
  os.exit(0)
end
`, root)
	case "optional_absent_zeros":
		body = fmt.Sprintf(`local r = { T.New%[1]s() }
r[1].plain.a = 1; r[1].plain.b = 2
r[1].opt_present = false
r[1].opt.a = 0x77777777; r[1].opt.b = 0x66666666
local wire = T.FixedSave(%[1]q, r)
local f = io.open("out.bin", "wb"); f:write(wire); f:close()
print("OK")
`, root)
	case "live_extents_base":
		body = fmt.Sprintf(`local r = { T.New%[1]s() }
r[1].arr = { "One" }
r[1].opt_present = false
r[1].tail = 77
local wire = T.FixedSave(%[1]q, r)
local f = io.open("out.bin", "wb"); f:write(wire); f:close()
print("OK")
`, root)
	case "writing_slack_zeros":
		body = fmt.Sprintf(`local r = { T.New%[1]s() }
r[1].arr = { 0x1122 }
r[1].s = "A"
r[1].b = "\x42"
r[1].pick = { tag = 1, narrow = 7 }
local wire = T.FixedSave(%[1]q, r)
local f = io.open("out.bin", "wb"); f:write(wire); f:close()
print("OK")
`, root)
	default:
		t.Fatalf("unknown SaveCustom scenario %q", scenario)
	}
	script := fmt.Sprintf("local T = require(%q)\n%s", mod, body)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(luarun.Run(t, dir, "main.lua", nil)))
	if after, ok := strings.CutPrefix(out, "REFUSED "); ok {
		return nil, strings.TrimSpace(after)
	}
	wire, err := os.ReadFile(filepath.Join(dir, "out.bin"))
	if err != nil {
		t.Fatal(err)
	}
	return wire, ""
}

func (luaFixedProofRunner) HasFixedRoot(t *testing.T, u *ir.Unit, root string) bool {
	t.Helper()
	for _, st := range fixedRoots(u) {
		if st.Name == root {
			return true
		}
	}
	return false
}

func luaSetupFor(id string) string {
	switch id {
	case "bool-values":
		return `r[2].a = true; r[2].b = false`
	case "signed-integers":
		return `r[2].a = 127; r[2].b = 32767; r[2].c = 2147483647; r[2].d = 9223372036854775807
r[3].a = -1; r[3].b = 0; r[3].c = 1; r[3].d = -2`
	case "unsigned-integers":
		return `r[2].a = 0; r[2].b = 0; r[2].c = 0; r[2].d = 0
r[3].a = 1; r[3].b = 256; r[3].c = 65536; r[3].d = 4294967296`
	case "signed-128":
		return `r[2].a = { lo = -1, hi = 0x7fffffffffffffff }
r[3].a = { lo = 1, hi = 0 }`
	case "unsigned-128":
		return `r[2].a = { lo = 0, hi = 0 }
r[3].a = { lo = 1, hi = 0 }`
	case "ranged-integers":
		return `r[1].a = -5; r[1].b = -1000000000000; r[1].c = 0
r[2].a = 5; r[2].b = 1000000000000; r[2].c = 1000`
	case "bits-values":
		return `r[1].a = 7; r[1].b = 4095; r[1].c = 1048575; r[1].d = 1099511627775
r[2].a = 0; r[2].b = 0; r[2].c = 0; r[2].d = 0`
	case "float32-values":
		return `r[2].a = -1024.25; r[2].b = 2048.75`
	case "float64-values":
		return `r[2].a = -987654321.125; r[2].b = 123456789.875`
	case "compressed-floats":
		return `r[1].a = 0.25; r[1].b = -0.5
r[2].a = 0.0; r[2].b = -1.0
r[3].a = 1.0; r[3].b = 1.0`
	case "signed-fixed-point":
		return `r[1].a = -(8 << 16); r[1].b = -(100 << 16); r[1].c = { lo = 0xffffffffff000000, hi = -1 }
r[2].a = (8 << 16); r[2].b = (100 << 16); r[2].c = { lo = 0x00fa0000, hi = 0 }`
	case "unsigned-fixed-point":
		return `r[1].a = 0; r[1].b = 0; r[1].c = { lo = 0, hi = 0 }
r[2].a = (16 << 16); r[2].b = (100 << 16); r[2].c = { lo = (1000 << 16), hi = 0 }`
	case "flags-values":
		return `r[1].caps = 0; r[2].caps = 5; r[3].caps = 7`
	case "enum-values":
		return `r[1].g = nil; r[2].g = "Gold"; r[3].g = "Bronze"`
	case "utf8-values":
		return `r[2].s = "hello"`
	case "utf16-values":
		return `r[2].w = { 0x0041, 0x03b1, 0xd83d, 0xde00 }`
	case "byte-buffers":
		return `r[2].blob = "\xff\x00\x5a\x01\x02\x03"`
	case "nested-types":
		return `r[1].badge.title = "hi"`
	case "nested-fixed-tables":
		return `r[1].child.n = -2147483648; r[2].child.n = 2147483647`
	case "fixed-arrays":
		return `r[1].tag = { -2147483648, 0, 2147483647 }`
	case "counted-arrays":
		return `r[2].nums = { 10, 65535, 32768 }`
	case "keyed-arrays":
		return `r[1].slots = { -2147483648, 2147483647 }`
	case "nested-keyed-arrays":
		return `r[1].slots[1].n = 11; r[1].slots[2].n = -22`
	case "union-values":
		return `r[1].body = { tag = 1, v = { x = 11, y = -22 } }
r[2].body = { tag = 2, t = { n = 41 } }
r[3].body = { tag = 3, n = 1073741821 }`
	case "union-field-arms":
		return `r[1].body = { tag = 1, sa = -5 }
r[2].body = { tag = 2, ta = "abc" }
r[3].body = { tag = 3, aa = { -1, 0, 1 } }`
	case "payload-free-arms":
		return `r[1].lead = 10; r[1].pick = { tag = 1, alpha = 99 }; r[1].trail = 20
r[2].lead = 30; r[2].pick = { tag = 2, alpha = 777 }; r[2].trail = 40
r[3].lead = 50; r[3].pick = { tag = 3, alpha = 888 }; r[3].trail = 60`
	case "union-arrays":
		return `r[1].picks = { { tag = 1, a = 3 }, { tag = 2, b = -1099511627776 }, { tag = 1, a = 0 } }`
	case "optional-scalars":
		return `r[2].n = -123456; r[2].n_present = true; r[2].g = "Silver"; r[2].g_present = true`
	case "optional-nesting":
		return `r[2].p = { x = 99, y = -99 }; r[2].p_present = true`
	case "optional-arrays":
		return `r[2].xs = { 7, -8, 9 }; r[2].xs_present = true`
	case "scalar-defaults":
		return `r[2].x = -9; r[2].g = "Bronze"; r[2].f = -2.25`
	case "text-bytes-flags-defaults":
		return `r[2].s = "xyz"; r[2].b = "\x00\xff\x5a\x01"; r[2].c = 4`
	case "record-measurement":
		return `r[1].a = 10; r[1].b = 20; r[2].a = 30; r[2].b = 40`
	case "fixed-file-roundtrip":
		return `r[2].a = 22; r[2].b = "test"`
	case "file-envelope":
		return `r[2].x = 10; r[2].y = 20`
	case "batch-capacity":
		return `r[2].x = 3; r[2].y = 4
r[3].x = 5; r[3].y = 6`
	case "plan-selection":
		return `r[2].x = 33; r[2].y = 44`
	case "compiled-plans":
		return `r[2].a = 100; r[2].b = -20; r[2].c = 55`
	case "definition-hash":
		return `r[2].x = 9; r[2].b = "abc"`
	case "fixed-closure":
		return `r[2].n.a = 30; r[2].n.b = 40; r[2].z = 70`
	case "retirement":
		return `r[2].a = 10; r[2].b = 20; r[2].c = 30`
	case "retired-runtime":
		return `r[2].x = 44; r[2].y = 55; r[2].z = 66`
	case "optional-values":
		return `r[2].plain.a = 11; r[2].plain.b = 22
r[2].opt_present = true; r[2].opt = { a = 33, b = 44 }`
	case "field-evolution":
		return `r[2].b = 100; r[2].c = 200`
	case "live-extents":
		return `r[2].arr = { "One" }; r[2].opt_present = false; r[2].tail = 88`
	case "writing":
		return `r[2].arr = { 0x1122 }; r[2].s = "A"; r[2].b = "\x42"; r[2].pick = { tag = 1, narrow = 7 }`
	case "union-guards":
		return `r[2].u = { tag = 2, nested = { x = 12, y = 0x12345678 } }; r[2].tail = 77`
	case "plan-execution":
		return `r[2].a = 10; r[2].sub.p = 30; r[2].sub.q = 40; r[2].xs = { 50, 60 }`
	case "interoperability":
		return `r[2].a = 42; r[2].s = "test"`
	default:
		panic("missing luaSetupFor case: " + id)
	}
}

func luaCheckFor(id string) string {
	switch id {
	case "bool-values":
		return `assert(rows[1].a == false and rows[1].b == true and rows[2].a == true and rows[2].b == false)`
	case "signed-integers":
		return `assert(rows[1].a == -128 and rows[2].a == 127 and rows[2].d == 9223372036854775807 and rows[3].d == -2)`
	case "unsigned-integers":
		return `assert(rows[1].a == 255 and rows[2].d == 0 and rows[3].d == 4294967296)`
	case "signed-128":
		return `assert(rows[1].a.lo == 0 and rows[1].a.hi == 0x8000000000000000 and rows[2].a.hi == 0x7fffffffffffffff and rows[3].a.lo == 1)`
	case "unsigned-128":
		return `assert(rows[1].a.lo == -1 and rows[2].a.lo == 0 and rows[3].a.lo == 1 and rows[3].a.hi == 0)`
	default:
		return ""
	}
}

var _ = hex.EncodeToString
