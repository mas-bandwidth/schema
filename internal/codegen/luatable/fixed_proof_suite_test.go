package luatable_test

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luatable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Functional (luaBinary): run with SCHEMA_SLOW=1.
//
// TestLuaFixedProofSuiteValues runs all 49 contract-independent fixed-table
// rows through the shared fixedproof value suite against the live C++ oracle.
func TestLuaFixedProofSuiteValues(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lua and c++ (it runs the emitted fixed-form runtime against a compiled C++ reference)")
	fixedproof.RunValueSuite(t, luaFixedProofRunner{})
}

// Functional (luaBinary): run with SCHEMA_SLOW=1.
//
// TestLuaFixedProofSuiteNegativeControl runs both the file negative control
// and the writer negative control across all 49 rows under a hard 2 GiB memory
// ceiling.
func TestLuaFixedProofSuiteNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lua and c++ (it runs the emitted fixed-form runtime against a compiled C++ reference)")
	fixedproof.RunNegativeControlSuite(t, luaFixedProofRunner{})
}

// Functional (luaBinary): run with SCHEMA_SLOW=1.
//
// TestLuaFixedProofEvolution runs the 16 non-value fixed-table rows
// (fixed-file-roundtrip and the 15 evolution/layout/plan/lineage/file rows)
// through the shared fixedproof evolution suite.
func TestLuaFixedProofEvolution(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lua and c++ (it runs the emitted fixed-form runtime against a compiled C++ reference)")
	repo := filepath.Join("..", "..", "..")
	fixedproof.RunEvolutionSuite(t, repo, luaFixedProofRunner{})
}

// Functional (luaBinary): run with SCHEMA_SLOW=1.
//
// TestLuaFixedBatch4097Roundtrip proves that 4,097 fixed records save and read
// back when the caller sets capacity 4,097, that a caller's 4,096 refuses them,
// and that omitting the capacity (or passing nil) applies the 4,096 default.
func TestLuaFixedBatch4097Roundtrip(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lua and c++ (it runs the emitted fixed-form runtime against a compiled C++ reference)")
	u := fixedproof.ParseCheckUnit(t, "package probe\nfixed table Point {\n    x int32 = 1\n    y int32 = 2\n}\n")
	files, err := luatable.Generate(u)
	if err != nil {
		t.Fatalf("luatable.Generate: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	script := fmt.Sprintf(`local T = require(%q)
local r = {}
for i = 1, 4097 do
  local p = T.NewPoint()
  p.x = i
  p.y = -i
  r[i] = p
end
local m = T.FixedMeasure("Point", 4097)
assert(m > 0, "FixedMeasure(4097) returned " .. tostring(m))
local wire = T.FixedSave("Point", r)
assert(#wire == m, string.format("#wire=%%d want %%d", #wire, m))
local okOmitted, errOmitted = pcall(function() return T.FixedLoad("Point", wire) end)
assert(not okOmitted, "expected refusal for 4097 records with the capacity omitted, got ok")
assert(tostring(errOmitted):find("batch_too_large", 1, true), "omitted capacity refused without batch_too_large: " .. tostring(errOmitted))
local okNil, errNil = pcall(function() return T.FixedLoad("Point", wire, nil) end)
assert(not okNil, "expected refusal for 4097 records with a nil capacity, got ok")
assert(tostring(errNil):find("batch_too_large", 1, true), "nil capacity refused without batch_too_large: " .. tostring(errNil))
local back = T.FixedLoad("Point", wire, 4097)
assert(#back == 4097, "#back=" .. tostring(#back))
assert(back[1].x == 1 and back[1].y == -1, "record 1 mismatch")
assert(back[2049].x == 2049 and back[2049].y == -2049, "record 2049 mismatch")
assert(back[4097].x == 4097 and back[4097].y == -4097, "record 4097 mismatch")
local ok4096, err4096 = pcall(function() return T.FixedLoad("Point", wire, 4096) end)
assert(not ok4096, "expected refusal for 4097 records into cap=4096, got ok")
assert(tostring(err4096):find("batch_too_large", 1, true), "cap=4096 refused without batch_too_large: " .. tostring(err4096))
print("OK 4097")
`, luatable.ModuleName(u))
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(luarun.Run(t, dir, "main.lua", nil)))
	if out != "OK 4097" {
		t.Fatalf("got %q, want OK 4097", out)
	}
}

type luaFixedProofRunner struct{}

func (luaFixedProofRunner) Name() string { return "lua" }

func (luaFixedProofRunner) Save(t *testing.T, tc fixedproof.RowCase, u *ir.Unit) []byte {
	t.Helper()
	files, err := luatable.Generate(u)
	if err != nil {
		t.Fatalf("%s: luatable.Generate: %v", tc.ID, err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	mod := luatable.ModuleName(u)
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
	files, err := luatable.Generate(u)
	if err != nil {
		return nil, err
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	mod := luatable.ModuleName(u)
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
		return nil, fmt.Errorf("%w: %s %s", runErr, strings.TrimSpace(stderr), strings.TrimSpace(stdout))
	}
	return os.ReadFile(filepath.Join(dir, "resaved.bin"))
}

func (luaFixedProofRunner) SaveMutated(t *testing.T, tc fixedproof.RowCase, u *ir.Unit) ([]byte, error) {
	t.Helper()
	files, err := luatable.Generate(u)
	if err != nil {
		return nil, err
	}
	files["fixed.lua"] = patchLuaFixedWriterForMutation(t, tc.ID, files["fixed.lua"])
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	mod := luatable.ModuleName(u)
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
		return nil, fmt.Errorf("%w: %s %s", runErr, strings.TrimSpace(stderr), strings.TrimSpace(stdout))
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

func toLuaLineage(lineage map[string][]fixedproof.LineageEntry) map[string][]luatable.FixedLineageEntry {
	if len(lineage) == 0 {
		return nil
	}
	lmap := map[string][]luatable.FixedLineageEntry{}
	for k, entries := range lineage {
		for _, e := range entries {
			lmap[k] = append(lmap[k], luatable.FixedLineageEntry{
				Wire:    e.Wire,
				Layout:  e.Layout,
				Record:  e.Record,
				Retired: e.Retired,
				Reason:  e.Reason,
			})
		}
	}
	return lmap
}

func (luaFixedProofRunner) CheckLineage(t *testing.T, u *ir.Unit, root string, lineage map[string][]fixedproof.LineageEntry) string {
	t.Helper()
	files, err := luatable.GenerateLineage(u, toLuaLineage(lineage))
	if err != nil {
		t.Fatalf("luatable.GenerateLineage: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	mod := luatable.ModuleName(u)
	script := fmt.Sprintf(`local T = require(%[1]q)
local fixed = T.Fixed[%[2]q]
if fixed == nil then
  print("REFUSED no_fixed_root")
  os.exit(0)
end
for _, entry in ipairs(fixed.known or {}) do
  if not entry.tree then
    print("REFUSED layout_malformed")
    os.exit(0)
  end
  if entry.tree.refusal then
    print("REFUSED " .. tostring(entry.tree.refusal))
    os.exit(0)
  end
  if entry.tree.size <= 0 or entry.tree.size > 65536 or entry.record ~= 8 + entry.tree.size then
    print("REFUSED layout_record_too_large")
    os.exit(0)
  end
end
print("OK")
`, mod, root)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(luarun.Run(t, dir, "main.lua", nil)))
	if after, ok := strings.CutPrefix(out, "REFUSED "); ok {
		return strings.TrimSpace(after)
	}
	return ""
}

// luaDefaultBatchCapacity is the runtime's default when the caller omits the
// capacity or passes nil; a caller that wants more passes its own bound.
const luaDefaultBatchCapacity = 4096

// luaWireRecords is the record count a fixed file states for root, or -1 when
// the header, layout length or record size does not parse.
func luaWireRecords(u *ir.Unit, root string, lineage map[string][]fixedproof.LineageEntry, wire []byte) int {
	if len(wire) < 20 {
		return -1
	}
	hash := binary.LittleEndian.Uint64(wire[8:16])
	layoutLen := int(binary.LittleEndian.Uint32(wire[16:20]))
	if layoutLen < 0 || len(wire) < 20+layoutLen {
		return -1
	}
	record := int64(0)
	for _, e := range lineage[root] {
		if e.Wire == hash {
			record = e.Record
			break
		}
	}
	if record == 0 {
		if own, ok := fixedproof.LineageEntryOf(u, root); ok && own.Wire == hash {
			record = own.Record
		}
	}
	if record <= 8 {
		return -1
	}
	rest := int64(len(wire) - 20 - layoutLen)
	if rest < 0 || rest%record != 0 {
		return -1
	}
	return int(rest / record)
}

func (luaFixedProofRunner) LoadLineage(t *testing.T, u *ir.Unit, root string, lineage map[string][]fixedproof.LineageEntry, wireBytes []byte, cap int) (int, string, map[string]string, []byte, fixedproof.LoadCounters) {
	t.Helper()
	if cap < 0 {
		if n := luaWireRecords(u, root, lineage, wireBytes); n > luaDefaultBatchCapacity {
			t.Skipf("%d records with no capacity (schema-fixedproof-x1's row): lua's default batch capacity stays 4,096 by contract, and only a caller-set capacity goes above it. TestLuaFixedBatch4097Roundtrip proves the caller-set 4,097 path and the 4,096 refusal.", n)
		}
	}
	var files map[string][]byte
	var err error
	if lmap := toLuaLineage(lineage); lmap != nil {
		files, err = luatable.GenerateLineage(u, lmap)
	} else {
		files, err = luatable.Generate(u)
	}
	if err != nil {
		t.Fatalf("luatable.Generate: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	mod := luatable.ModuleName(u)
	// The shared driver's cap < 0 means "the leg's default capacity": pass nil
	// so the runtime applies its own 4,096 default rather than an explicit
	// caller bound. A caller-set capacity is passed through unchanged, and
	// TestLuaFixedBatch4097Roundtrip owns the caller-set larger path.
	capArg := "nil"
	if cap >= 0 {
		capArg = fmt.Sprintf("%d", cap)
	}
	presentBool := root == "T" || root == "Outer"
	script := fmt.Sprintf(`local T = require(%[1]q)
local I = require("inputs")
local present_bool = %[4]v
local ok, rows, _, _, widened = pcall(function() return T.FixedLoad(%[2]q, I.wire, %[3]s) end)
if not ok then
  local msg = tostring(rows)
  local colon = msg:match(": ([^:]+)$")
  if colon then msg = colon end
  print("REFUSED " .. msg)
  os.exit(0)
end
print(string.format("OK %%d %%d", #rows, widened or 0))
local dump_record, dump_value
dump_value = function(d, v, p)
  local s = d.shape
  if s == "int" then
    if not d.signed and d.width == 8 then
      print("FIELD " .. p .. "=" .. string.format("%%u", v or 0))
    else
      print("FIELD " .. p .. "=" .. string.format("%%d", v or 0))
    end
  elseif s == "bool" then
    print("FIELD " .. p .. "=" .. (v and "1" or "0"))
  elseif s == "float" then
    if d.f32 then
      print("FIELD " .. p .. "=" .. string.format("0x%%08X", string.unpack("<I4", string.pack("<f", v or 0.0))))
    else
      print("FIELD " .. p .. "=" .. string.format("0x%%016X", string.unpack("<I8", string.pack("<d", v or 0.0))))
    end
  elseif s == "string" then
    local str = v or ""
    print("FIELD " .. p .. "=\"" .. str .. "\"")
    print("FIELD " .. p .. "_length=" .. tostring(#str))
  elseif s == "wstring" then
    local arr = v or {}
    print("FIELD " .. p .. "_length=" .. tostring(#arr))
  elseif s == "bytes" then
    local bs = v or ""
    print("FIELD " .. p .. "_length=" .. tostring(#bs))
    for idx = 1, #bs do
      print("FIELD " .. p .. "[" .. tostring(idx - 1) .. "]=" .. tostring(string.byte(bs, idx)))
    end
  elseif s == "enum" then
    local ord = 0
    if v ~= nil and v ~= false then
      for idx, var in ipairs(d.variants) do
        if var[1] == v then ord = idx; break end
      end
    end
    print("FIELD " .. p .. "=" .. tostring(ord))
  elseif s == "flags" then
    print("FIELD " .. p .. "=" .. string.format("%%u", v or 0))
  elseif s == "table" then
    dump_record(d.target, v, p)
  elseif s == "union" then
    local tag = (v and v.tag) or 0
    print("FIELD " .. p .. ".type=" .. tostring(tag))
    local un = T.Unions[d.target]
    if tag > 0 and un ~= nil and un.arms[tag] ~= nil then
      local arm = un.arms[tag]
      if arm.field ~= nil then
        dump_value(arm.field, v[arm.slot], p .. "." .. arm.slot)
      end
    end
  elseif s == "array" then
    local arr = v or {}
    if d.fixed or d.keyed then
      local n = d.bound or #d.keyed
      for idx = 1, n do
        dump_value(d.elem, arr[idx], p .. "[" .. tostring(idx - 1) .. "]")
      end
    else
      print("FIELD " .. p .. "_count=" .. tostring(#arr))
      for idx = 1, #arr do
        dump_value(d.elem, arr[idx], p .. "[" .. tostring(idx - 1) .. "]")
      end
    end
  end
end
dump_record = function(name, rec, prefix)
  local desc = T.Tables[name]
  if desc == nil or rec == nil then return end
  for _, f in ipairs(desc.fields) do
    local p = prefix .. "." .. f.name
    if f.optional then
      local pres = rec[f.name .. "_present"] == true
      if present_bool then
        print("FIELD " .. p .. "_present=" .. tostring(pres))
      else
        print("FIELD " .. p .. "_present=" .. (pres and "1" or "0"))
      end
      if pres then
        dump_value(f, rec[f.name], p)
      end
    else
      dump_value(f, rec[f.name], p)
    end
  end
end
for idx, r in ipairs(rows) do
  if idx <= 8 and type(r) == "table" then
    dump_record(%[2]q, r, "r" .. tostring(idx - 1))
  end
end
local re = T.FixedSave(%[2]q, rows)
local f = io.open("resaved.bin", "wb")
f:write(re)
f:close()
`, mod, root, capArg, presentBool)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luarun.Run(t, dir, "main.lua", map[string][]byte{"wire": wireBytes}))
	fields := map[string]string{}
	count := -1
	reason := ""
	var counters fixedproof.LoadCounters
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if after, ok := strings.CutPrefix(line, "REFUSED "); ok {
			return -1, strings.TrimSpace(after), fields, nil, fixedproof.LoadCounters{}
		}
		if after, ok := strings.CutPrefix(line, "OK "); ok {
			fmt.Sscanf(after, "%d %d", &count, &counters.Widened)
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
	return count, reason, fields, resaved, counters
}

func (luaFixedProofRunner) SaveCustom(t *testing.T, u *ir.Unit, root string, scenario string) ([]byte, string) {
	t.Helper()
	files, err := luatable.Generate(u)
	if err != nil {
		t.Fatalf("luatable.Generate: %v", err)
	}
	if scenario == "writing_slack_unzeroed" {
		const anchor = "  parts[#parts + 1] = zeros(size - used)\n"
		const planted = "  parts[#parts + 1] = string.rep(\"\\xaa\", size - used)\n"
		s := string(files["fixed.lua"])
		if strings.Count(s, anchor) != 1 {
			t.Fatal("slack anchor not found in fixed.lua")
		}
		files["fixed.lua"] = []byte(strings.Replace(s, anchor, planted, 1))
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	mod := luatable.ModuleName(u)
	var body string
	switch scenario {
	case "optional_absent_zeros":
		body = fmt.Sprintf(`local r = { T.New%[1]s() }
r[1].plain.a = 11; r[1].plain.b = 22
r[1].opt_present = false
r[1].opt.a = 0x12345678; r[1].opt.b = 0x23456789
`, root)
	case "live_extents_base":
		body = fmt.Sprintf(`local r = { T.New%[1]s() }
r[1].arr = { "One" }
r[1].opt_present = false
r[1].tail = 77
`, root)
	case "writing_slack_batch", "writing_slack_unzeroed":
		body = fmt.Sprintf(`local r = { T.New%[1]s(), T.New%[1]s() }
r[2].arr = { 0x1122 }
r[2].s = "A"
r[2].b = "\x42"
r[2].pick = { tag = 1, narrow = 7 }
`, root)
	case "writing_overcount":
		body = fmt.Sprintf(`local r = { T.New%[1]s() }
r[1].arr = { 1, 2, 3, 4 }
`, root)
	case "closure_small_42":
		body = fmt.Sprintf(`local r = { T.New%[1]s() }
r[1].x = 42
`, root)
	case "outer_nested":
		body = fmt.Sprintf(`local r = { T.New%[1]s() }
r[1].head = 7
r[1].inner.a = 11; r[1].inner.b = "one"
r[1].items[1].a = 10; r[1].items[1].b = "hi"
r[1].items[2].a = 33; r[1].items[2].b = "six"
r[1].maybe_present = true
r[1].maybe.a = 44; r[1].maybe.b = "hi"
r[1].tail = 9
`, root)
	case "w14_writer_offsets":
		body = fmt.Sprintf(`local r = { T.New%[1]s() }
r[1].a = 0xA1
r[1].b = 0x0B0B0B0B
r[1].inner.p = 0x1E1E1E1E
r[1].inner.q = 0x2E2E
r[1].opt_present = true
r[1].opt = 0x0D0D0D0D
r[1].arr = { 0x3E3E, 0x4E4E }
r[1].s = "seven"
r[1].tail = 0x7A7A7A7A
`, root)
	default:
		t.Fatalf("unknown SaveCustom scenario %q", scenario)
	}
	script := fmt.Sprintf(`local T = require(%[1]q)
%[2]s
local ok, wire = pcall(function() return T.FixedSave(%[3]q, r) end)
if not ok then
  print("REFUSED " .. tostring(wire))
  os.exit(0)
end
local f = io.open("out.bin", "wb"); f:write(wire); f:close()
print("OK")
`, mod, body, root)
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
	for _, st := range ir.TableFixedFormRoots(u) {
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
