package luatable

// LUA TABLES ARE FINITE ONLY, IN EVERY FORM (docs/SPEC-TABLES.md §3.3; Glenn,
// 2026-10-27: "Tables go finite-only too, same rule as packets"). A float32
// or float64 rides every form as the bit pattern of a FINITE value:
//
//   - -0 and the subnormals ride exact: the default elision compares BIT
//     PATTERNS, so a -0 under a +0 default rides the variable and message
//     forms, and the text reads "-0" as -0.
//   - a NaN (any sign, any payload) or an infinity is REFUSED BY EVERY
//     WRITER (variable save and measure, message, fixed, cook, JSON,
//     retaining save), and REFUSED BY EVERY READ on its raw bits, before they
//     become a number, as non_finite_float naming <Owner>.<field>: the
//     variable read (tolerant, strict and ReadStrict), the message read
//     (tolerant, strict and retaining), the fixed read (both names), the cook
//     check and the uncook, the block dump, and a retained unknown float
//     (ruling 3 of 2026-10-27). The tolerant read lands nothing.
//
// The generated Go of the same unit is the oracle for the saved bytes of the
// finite values and for the writer's refusal of the others, as in
// internal/codegen/gotable/floatspecial_test.go. Every assertion is on the RAW
// BITS: -0 == +0 and NaN != NaN under ==.
//
// TestLuaFiniteFloatNegativeControl is the reversed witness: it turns each
// read's raw-bits test off in turn, and the probe must go red on exactly that
// read's rows.

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const fsLuaSchema = `package probe

fixed table Root
{
    a float32
    b float64
    ys [2]float64
}

table Holder
{
    f float64
    next *Holder
}
`

type fsCase struct {
	name string
	b32  uint32
	b64  uint64
}

// fsFinite ride every form exact; fsNonFinite are refused by every writer
// and every read.
var fsFinite = []fsCase{
	{"plus_zero", 0x00000000, 0x0000000000000000},
	{"minus_zero", 0x80000000, 0x8000000000000000},
	{"min_subnormal", 0x00000001, 0x0000000000000001},
	{"neg_subnormal", 0x807fffff, 0x800fffffffffffff},
	{"ordinary", 0x3fc00000, 0x3ff8000000000000},
	{"max_finite", 0x7f7fffff, 0x7fefffffffffffff},
}

var fsNonFinite = []fsCase{
	{"nan_quiet", 0x7fc00000, 0x7ff8000000000000},
	{"nan_payload", 0x7fc00001, 0x7ff8000000000001},
	{"nan_signalling", 0x7f800001, 0x7ff0000000000001},
	{"nan_negative", 0xffc00000, 0xfff8000000000000},
	{"plus_inf", 0x7f800000, 0x7ff0000000000000},
	{"minus_inf", 0xff800000, 0xfff0000000000000},
}

// fsWantBits is the little-endian pattern of the four float slots: a, b,
// ys[0] and ys[1]. The Lua probe prints the same bytes through string.pack.
func fsWantBits(c fsCase) string {
	var b [4 + 8 + 8 + 8]byte
	binary.LittleEndian.PutUint32(b[0:], c.b32)
	binary.LittleEndian.PutUint64(b[4:], c.b64)
	binary.LittleEndian.PutUint64(b[12:], c.b64)
	binary.LittleEndian.PutUint64(b[20:], c.b64)
	return hex.EncodeToString(b[:])
}

// fsRetainedID is the id of the unknown float the retaining read meets.
const fsRetainedID = 0xdeadbeefcafebabe

// fsRetainedWire is a Holder file whose one field is an id Holder does not
// declare, of kind 10 or 11, holding bits.
func fsRetainedWire(kind int, bits uint64) []byte {
	payload := make([]byte, 8)
	binary.LittleEndian.PutUint64(payload, bits)
	if kind == 10 {
		payload = payload[:4]
	}
	body := append(strictField(1, kind, payload), 0)
	return strictWire(body, fsRetainedID)
}

func fsCaseList(cases []fsCase) string {
	var b strings.Builder
	b.WriteString("{")
	for _, c := range cases {
		fmt.Fprintf(&b, "\n  { name = %q, b32 = 0x%08x, b64 = 0x%016x },", c.name, c.b32, c.b64)
	}
	b.WriteString("\n}")
	return b.String()
}

// fsLuaProbe prints V|<case>|<kind>|<value> lines for the Go comparison and
// C|<check id>|PASS or C|<check id>|FAIL|<detail> lines. A check id is
// <read>-<width>:<case>:<slot>:<call>, so a sabotaged read reds its own rows.
func fsLuaProbe(module string) string {
	return `local T = require("` + module + `")
local save = require("save")
local load = require("load")
local refusal = require("refusal")
local J = require("json")
local cook = require("cook")
local uncook = require("uncook")
local cookwrite = require("cookwrite")
local B = require("block")
local BB = require("blockbuild")
local retain = require("retain")
local inputs = require("inputs")
local finite = ` + fsCaseList(fsFinite) + `
local nonfinite = ` + fsCaseList(fsNonFinite) + `

local function hex(s)
  return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end
local function f32(b) return string.unpack("<f", string.pack("<I4", b)) end
local function f64(b) return string.unpack("<d", string.pack("<I8", b)) end
local function bits(v)
  return hex(string.pack("<f", v.a)) .. hex(string.pack("<d", v.b))
    .. hex(string.pack("<d", v.ys[1])) .. hex(string.pack("<d", v.ys[2]))
end
local function check(id, ok, detail)
  if ok then print("C|" .. id .. "|PASS") else print("C|" .. id .. "|FAIL|" .. (tostring(detail):gsub("\n", " / "))) end
end
local function value(c)
  local v = T.NewRoot()
  local f = f64(c.b64)
  v.a = f32(c.b32)
  v.b = f
  v.ys[1], v.ys[2] = f, f
  return v
end
local function announced()
  local vocab = {}
  for i = 1, T.MessageEntries do vocab[i] = {} end
  assert(T.AnnounceRead(vocab, T.Announcement, {}), "announce")
  return vocab
end
local function report()
  return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0,
    clamped = 0, widened = 0, duplicate = 0, retained = 0, retainLost = 0 }
end
local vocab = announced()

-- THE FINITE VALUES ride every form exact.
for _, c in ipairs(finite) do
  local v = value(c)
  local want = bits(v)
  local wire = assert(save.Save(T, "Root", v), "variable save")
  print("V|" .. c.name .. "|var|" .. hex(wire))
  local back = assert(load.LoadTolerant(T, "Root", wire, report()), "variable load")
  print("V|" .. c.name .. "|varback|" .. bits(back))
  local msg = assert(T.SaveMessages("Root", vocab, { v }), "message save")
  print("V|" .. c.name .. "|msg|" .. hex(msg))
  local values = assert(T.LoadMessages("Root", vocab, msg, report()), "message load")
  print("V|" .. c.name .. "|msgback|" .. bits(values[1]))
  local fixed = T.WriteFixed("Root", v)
  check("finite:" .. c.name .. ":fixed", bits(T.ReadFixed("Root", fixed)) == want, "fixed")
  local n = cookwrite.CookMeasure(T, "Root", v)
  local cooked = cookwrite.Cook(T, "Root", v)
  check("finite:" .. c.name .. ":cook", n == #cooked, "cook measure")
  local h = assert(cook.Check(T, "Root", cooked), "cook check")
  check("finite:" .. c.name .. ":uncook", bits(uncook.Read(T, h)) == want, "uncook")
  local text = assert(J.ToJson(T, "Root", v), "json write")
  print("V|" .. c.name .. "|json|" .. hex(text))
  local rt = assert(J.FromJson(T, "Root", text, report()), "json read")
  print("V|" .. c.name .. "|jsonback|" .. bits(rt))
end

-- EVERY WRITER refuses a NaN or an infinity, named, in each slot.
local slots = {
  { name = "a", owner = "Root.a", set = function(v, c) v.a = f32(c.b32) end },
  { name = "b", owner = "Root.b", set = function(v, c) v.b = f64(c.b64) end },
  { name = "ys", owner = "Root.ys", set = function(v, c) v.ys[2] = f64(c.b64) end },
}
local writers = nonfinite
for _, c in ipairs(writers) do
  for _, s in ipairs(slots) do
    local v = value({ b32 = 0x3f800000, b64 = 0x3ff0000000000000 })
    s.set(v, c)
    local id = "write:" .. c.name .. ":" .. s.name .. ":"
    local named = "non_finite_float: " .. s.owner
    local w, why = save.Save(T, "Root", v)
    check(id .. "save", w == nil and why == named, why)
    local m, mwhy = save.Measure(T, "Root", v)
    check(id .. "measure", m == nil and mwhy == named, mwhy)
    local mm, mmwhy = T.MeasureMessages("Root", vocab, { v })
    check(id .. "message-measure", mm == -1 and mmwhy == named, mmwhy)
    check(id .. "message-save", T.SaveMessages("Root", vocab, { v }) == nil, "message save wrote")
    local fok, ferr = pcall(T.WriteFixed, "Root", v)
    check(id .. "fixed", not fok and ferr == "non_finite_float", ferr)
    local cm, cwhy = cookwrite.CookMeasure(T, "Root", v)
    check(id .. "cook-measure", cm == -1 and cwhy == named, cwhy)
    local cw, cwwhy = cookwrite.Cook(T, "Root", v)
    check(id .. "cook", cw == nil and cwwhy == named, cwwhy)
    local j, jwhy = J.ToJson(T, "Root", v)
    check(id .. "json", j == nil and jwhy == named, jwhy)
  end
end
-- a double past the float32 range would narrow to an infinity: refused too
do
  local v = value({ b32 = 0x3f800000, b64 = 0x3ff0000000000000 })
  v.a = 1e300
  local w, why = save.Save(T, "Root", v)
  check("write:f32_overflow:a:save", w == nil and why == "non_finite_float: Root.a", why)
  local fok = pcall(T.WriteFixed, "Root", v)
  check("write:f32_overflow:a:fixed", not fok, "fixed wrote")
end
do
  local h = T.NewHolder()
  h.f = 0.0 / 0.0
  local w, why = retain.SaveRetain(T, "Holder", h, {}, report())
  check("write:nan:f:retain-save", w == nil and why == "non_finite_float: Holder.f", why)
end

-- EVERY READ refuses a NaN or an infinity on its raw bits. Each wire is a
-- sentinel value's, with one slot's pattern replaced by the non-finite one.
local sentinel = { a = 0x3fc00000, b = 0x4004000000000000, ys = 0x400c000000000000 }
local function sentinel_value()
  local v = T.NewRoot()
  v.a = f32(sentinel.a); v.b = f64(sentinel.b)
  v.ys[1], v.ys[2] = f64(sentinel.ys), f64(0x4012000000000000)
  return v
end
local function patch_bytes(s, f32wide, from, to)
  local fmt = f32wide and "<I4" or "<I8"
  local p = string.pack(fmt, from)
  local i = s:find(p, 1, true)
  assert(i ~= nil, "sentinel not found")
  assert(s:find(p, i + 1, true) == nil, "sentinel found twice")
  return s:sub(1, i - 1) .. string.pack(fmt, to) .. s:sub(i + #p)
end
local function patch_bits(s, width, from, to)
  local t = {}
  for i = 1, #s do
    local b = s:byte(i)
    for k = 0, 7 do t[#t + 1] = (b >> k) & 1 end
  end
  local mask = width == 64 and -1 or ((1 << width) - 1)
  for i = 1, #t - width + 1 do
    local v = 0
    for k = 0, width - 1 do v = v | (t[i + k] << k) end
    if v == from & mask then
      for k = 0, width - 1 do t[i + k] = (to >> k) & 1 end
      local out = {}
      for j = 1, #t, 8 do
        local b = 0
        for k = 0, 7 do b = b | ((t[j + k] or 0) << k) end
        out[#out + 1] = string.char(b)
      end
      return table.concat(out)
    end
  end
  error("sentinel bits not found")
end

local sv = sentinel_value()
local wire = save.Save(T, "Root", sv)
local msg = T.SaveMessages("Root", vocab, { sv })
local fixed = T.WriteFixed("Root", sv)
local cooked = cookwrite.Cook(T, "Root", sv)
local checked = assert(cook.Check(T, "Root", cooked), "sentinel cook check")
local layout = B.Layout(T, "Root", true)
local block = assert(BB.Begin(T, "Root", {}), "block begin").bytes
local function block_offset(name)
  for _, fl in ipairs(layout.fields) do if fl.f.name == name then return fl.offset end end
  error("no block field " .. name)
end
local block_slots = { a = { 4, sentinel.a }, b = { 8, sentinel.b }, ys = { 8, sentinel.ys } }
for name, s in pairs(block_slots) do
  local at = block_offset(name)
  local fmt = s[1] == 4 and "=I4" or "=I8"
  block = block:sub(1, at) .. string.pack(fmt, s[2]) .. block:sub(at + s[1] + 1)
end
assert(B.Dump(assert(B.BlockOpen(T, "Root", block))), "sentinel block dump")

for _, c in ipairs(nonfinite) do
  for _, s in ipairs(slots) do
    local is32 = s.name == "a"
    local width = is32 and "f32" or "f64"
    local from, to = sentinel[s.name], is32 and c.b32 or c.b64
    local tail = ":" .. c.name .. ":" .. s.name .. ":"
    local named = "non_finite_float: " .. s.owner

    local w = patch_bytes(wire, is32, from, to)
    local r = report()
    local got = load.LoadTolerant(T, "Root", w, r)
    check("var-" .. width .. tail .. "tolerant", got == nil and r.nonFiniteFloat == s.owner and r.reason == named, r.reason)
    r = report()
    got = load.Load(T, "Root", w, r)
    check("var-" .. width .. tail .. "strict", got == nil and r.reason == "non_finite_float", r.reason)
    local ok, err = pcall(refusal.ReadStrict, T, "Root", w)
    check("var-" .. width .. tail .. "readstrict", not ok and err == "non_finite_float", err)

    local m = patch_bits(msg, is32 and 32 or 64, from, to)
    r = report()
    local values = T.LoadMessagesTolerant("Root", vocab, m, r)
    check("msg-" .. width .. tail .. "tolerant", values ~= nil and #values == 0 and r.nonFiniteFloat == s.owner and r.reason == named, r.reason)
    r = report()
    values = T.LoadMessages("Root", vocab, m, r)
    check("msg-" .. width .. tail .. "strict", values ~= nil and #values == 0 and r.reason == named, r.reason)

    local fx = patch_bytes(fixed, is32, from, to)
    ok, err = pcall(T.ReadFixed, "Root", fx)
    check("fixed-" .. width .. tail .. "strict", not ok and err == "non_finite_float", err)
    ok, err = pcall(T.ReadFixedTolerant, "Root", fx)
    check("fixed-" .. width .. tail .. "tolerant", not ok and err == "non_finite_float", err)

    local ck = patch_bytes(cooked, is32, from, to)
    local h, why = cook.Check(T, "Root", ck)
    check("cookcheck-" .. width .. tail .. "check", h == nil and why == "non_finite_float", why)
    -- a handle a caller checked itself: the uncook holds the rule on its own
    local held = {}
    for k, x in pairs(checked) do held[k] = x end
    held.bytes = ck
    local u, uwhy = uncook.Read(T, held)
    check("uncook-" .. width .. tail .. "read", u == nil and uwhy == "non_finite_float", uwhy)

    local at = block_offset(s.name)
    local fmt = is32 and "=I4" or "=I8"
    local bk = block:sub(1, at) .. string.pack(fmt, to) .. block:sub(at + (is32 and 4 or 8) + 1)
    ok, err = pcall(B.Dump, assert(B.BlockOpen(T, "Root", bk)))
    check("block-" .. width .. tail .. "dump", not ok and err == named, err)
  end
end

-- A RETAINED UNKNOWN FLOAT is tested like a known one (ruling 3); the plain
-- read skips the unknown field and reads on.
for _, k in ipairs({ { 10, "f32" }, { 11, "f64" } }) do
  for _, c in ipairs(nonfinite) do
    local w = inputs["retained_" .. k[1] .. "_" .. c.name]
    local tail = ":" .. c.name .. ":unknown:"
    local store = { list = {} }
    local r = report()
    local got = retain.LoadRetain(T, "Holder", w, store, r)
    check("retain-" .. k[2] .. tail .. "retaining", got == nil and r.reason == "non_finite_float"
      and r.nonFiniteFloat == string.format("Holder.0x%016x", ` + fmt.Sprintf("0x%x", uint64(fsRetainedID)) + `)
      and #store.list == 0, r.reason)
    r = report()
    got = load.LoadTolerant(T, "Holder", w, r)
    check("plain-" .. k[2] .. tail .. "skips", got ~= nil and r.unknown == 1 and r.nonFiniteFloat == nil, r.reason)
  end
end
`
}

func fsGoProbe(pkg string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `package %s

import (
	"encoding/hex"
	"math"
	"os"
	"testing"
)

func TestFSGo(t *testing.T) {
	cases := []struct {
		name string
		b32  uint32
		b64  uint64
	}{
`, pkg)
	for _, c := range slices.Concat(fsFinite, fsNonFinite) {
		fmt.Fprintf(&b, "\t\t{%q, 0x%08x, 0x%016x},\n", c.name, c.b32, c.b64)
	}
	b.WriteString(`	}
	put := func(name, text string) {
		if err := os.WriteFile(name, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range cases {
		var v Root
		RootReset(&v)
		f := math.Float64frombits(c.b64)
		v.A = math.Float32frombits(c.b32)
		v.B = f
		v.Ys = [2]float64{f, f}
		if n := RootMeasure(&v); n < 0 {
			put("go_var_"+c.name+".hex", "refused")
		} else {
			wire := make([]byte, n)
			if RootSave(&v, wire) != n {
				t.Fatalf("%s: variable save", c.name)
			}
			put("go_var_"+c.name+".hex", hex.EncodeToString(wire))
		}
		values := []*Root{&v}
		if m := RootMeasureMessages(values, nil); m < 0 {
			put("go_msg_"+c.name+".hex", "refused")
		} else {
			buf := make([]byte, m)
			var rep TableReport
			if RootSaveMessages(values, buf, &rep) != int64(len(buf)) {
				t.Fatalf("%s: message save", c.name)
			}
			put("go_msg_"+c.name+".hex", hex.EncodeToString(buf))
		}
		if jn := RootToJSONMeasure(&v); jn < 0 {
			put("go_json_"+c.name+".hex", "refused")
		} else {
			js := make([]byte, jn)
			if RootToJSON(&v, js) != jn {
				t.Fatalf("%s: json write", c.name)
			}
			put("go_json_"+c.name+".hex", hex.EncodeToString(js))
		}
	}
}
`)
	return b.String()
}

func fsLoadUnit(t *testing.T) *ir.Unit {
	t.Helper()
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "Special.schema")
	if err := os.WriteFile(schemaPath, []byte(fsLuaSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, dir)
	return u
}

func fsInputs() map[string][]byte {
	in := map[string][]byte{}
	for _, c := range fsNonFinite {
		in["retained_10_"+c.name] = fsRetainedWire(10, uint64(c.b32))
		in["retained_11_"+c.name] = fsRetainedWire(11, c.b64)
	}
	return in
}

// fsRun runs the probe over the unit's Lua files, each runtime file passed
// through edit, and answers the value lines and the check lines.
func fsRun(t *testing.T, u *ir.Unit, files map[string][]byte) (values map[string]string, checks map[string]string) {
	t.Helper()
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(fsLuaProbe(ModuleName(u))), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luarun.Run(t, dir, "main.lua", fsInputs()))
	values, checks = map[string]string{}, map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		part := strings.SplitN(line, "|", 4)
		switch {
		case len(part) == 4 && part[0] == "V":
			values[part[1]+"|"+part[2]] = part[3]
		case len(part) >= 3 && part[0] == "C":
			checks[part[1]] = strings.Join(part[2:], "|")
		default:
			t.Fatalf("Lua probe line %q, want V|case|kind|value or C|id|verdict", line)
		}
	}
	return values, checks
}

func fsGenerate(t *testing.T, u *ir.Unit) map[string][]byte {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate the Lua unit: %v", err)
	}
	return files
}

func TestLuaPlainFloatSpecialValuesAgreeAcrossForms(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "go and the pinned lua")
	u := fsLoadUnit(t)
	values, checks := fsRun(t, u, fsGenerate(t, u))

	failed := 0
	for _, id := range slices.Sorted(maps.Keys(checks)) {
		if checks[id] != "PASS" {
			failed++
			t.Errorf("%s: %s", id, checks[id])
		}
	}
	// the probe ran every row: 6 finite x 3, 6 non-finite x 3 slots x 8
	// writers, 3 extra writer rows, 6 x 3 x 10 reads and 2 x 6 x 2 retained
	if want := 6*3 + 6*3*8 + 3 + 6*3*10 + 2*6*2; len(checks) != want {
		t.Errorf("the probe answered %d checks, want %d", len(checks), want)
	}

	goFiles, err := golang.Generate(u)
	if err != nil {
		t.Fatalf("generate the Go unit: %v", err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatalf("generate the Go table surface: %v", err)
	}
	maps.Copy(goFiles, tables)
	goFiles["go.mod"] = []byte("module probe\n\ngo 1.26\n")
	goFiles["floatspecial_probe_test.go"] = []byte(fsGoProbe(u.Package))
	goDir := t.TempDir()
	for name, data := range goFiles {
		if err := os.WriteFile(filepath.Join(goDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("nice", "-n", "19", "go", "test", "-count=1", "-timeout", "600s", "-run", "^TestFSGo$", ".")
	cmd.Dir = goDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the generated Go oracle: %v\n%s", err, output)
	}
	read := func(kind string, c fsCase) string {
		data, err := os.ReadFile(filepath.Join(goDir, "go_"+kind+"_"+c.name+".hex"))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		return string(data)
	}

	for _, c := range fsFinite {
		want := fsWantBits(c)
		for _, kind := range []string{"var", "msg", "json"} {
			if got := values[c.name+"|"+kind]; got != read(kind, c) {
				t.Errorf("%s %s: Lua saved %s, the generated Go %s", c.name, kind, got, read(kind, c))
			}
			if got := values[c.name+"|"+kind+"back"]; got != want {
				t.Errorf("%s %s read: Lua landed %s, want the raw bits %s", c.name, kind, got, want)
			}
		}
	}
	// THE GO ORACLE REFUSES THE SAME VALUES the Lua writers refuse: its
	// measure answers -1 in the variable, message and text forms.
	for _, c := range fsNonFinite {
		for _, kind := range []string{"var", "msg", "json"} {
			if got := read(kind, c); got != "refused" {
				t.Errorf("%s %s: the generated Go wrote %s; the oracle must refuse a non-finite float", c.name, kind, got)
			}
		}
	}
}

// fsSabotage turns one read's raw-bits test off: the edit must apply exactly
// once, and the probe must red exactly the rows whose id starts with prefix.
type fsSabotage struct {
	file, from, to, prefix string
}

var fsSabotages = []fsSabotage{
	{"load.lua", "if b & F32_EXP == F32_EXP then\n      r.at = r.at + 4", "if false then\n      r.at = r.at + 4", "var-f32:"},
	{"load.lua", "if b & F64_EXP == F64_EXP then\n      r.at = r.at + 8", "if false then\n      r.at = r.at + 8", "var-f64:"},
	{"message.lua", "if v & D.F32_EXP == D.F32_EXP then return 0.0, false, true end", "if false then return 0.0, false, true end", "msg-f32:"},
	{"message.lua", "if v & D.F64_EXP == D.F64_EXP then return 0.0, false, true end", "if false then return 0.0, false, true end", "msg-f64:"},
	{"fixed.lua", `if string.unpack("<I4", buf, pos) & F32_EXP == F32_EXP then`, "if false then", "fixed-f32:"},
	{"fixed.lua", `if string.unpack("<i8", buf, pos) & F64_EXP == F64_EXP then`, "if false then", "fixed-f64:"},
	{"cook.lua", `bad = string.unpack("<I4", s.data, at + 1) & 0x7f800000 == 0x7f800000`, "bad = false", "cookcheck-f32:"},
	{"cook.lua", `bad = string.unpack("<i8", s.data, at + 1) & 0x7ff0000000000000 == 0x7ff0000000000000`, "bad = false", "cookcheck-f64:"},
	{"uncook.lua", `if string.unpack("<I4", ctx.data, at + 1) & 0x7f800000 == 0x7f800000 then`, "if false then", "uncook-f32:"},
	{"uncook.lua", `if string.unpack("<i8", ctx.data, at + 1) & 0x7ff0000000000000 == 0x7ff0000000000000 then`, "if false then", "uncook-f64:"},
	{"block.lua", "if b & 0x7f800000 == 0x7f800000 then return nil end", "if false then return nil end", "block-f32:"},
	{"block.lua", "if b & 0x7ff0000000000000 == 0x7ff0000000000000 then return nil end", "if false then return nil end", "block-f64:"},
	{"retain.lua", `kind == 10 and string.unpack("<I4", b, start + 1) & D.F32_EXP == D.F32_EXP`, "kind == 10 and false", "retain-f32:"},
	{"retain.lua", `or kind == 11 and string.unpack("<i8", b, start + 1) & D.F64_EXP == D.F64_EXP`, "or kind == 11 and false", "retain-f64:"},
}

// TestLuaFiniteFloatNegativeControl is the reversed witness of the finite
// rule's reads (docs/SPEC-TABLES.md §3.3): with one read's raw-bits test
// turned off, the probe must red on that read's rows and on no other, so
// every read's own test is what holds its rows green.
func TestLuaFiniteFloatNegativeControl(t *testing.T) {
	t.Parallel()
	u := fsLoadUnit(t)
	clean := fsGenerate(t, u)
	for _, s := range fsSabotages {
		t.Run(s.prefix, func(t *testing.T) {
			t.Parallel()
			src, ok := clean[s.file]
			if !ok {
				t.Fatalf("the unit carries no %s", s.file)
			}
			if n := strings.Count(string(src), s.from); n != 1 {
				t.Fatalf("the sabotage of %s matches %d times, want once: %q", s.file, n, s.from)
			}
			files := maps.Clone(clean)
			files[s.file] = []byte(strings.Replace(string(src), s.from, s.to, 1))
			_, checks := fsRun(t, u, files)
			var red, want []string
			for id, verdict := range checks {
				if verdict != "PASS" {
					red = append(red, id)
				}
				if strings.HasPrefix(id, s.prefix) {
					want = append(want, id)
				}
			}
			sort.Strings(red)
			sort.Strings(want)
			if len(want) == 0 {
				t.Fatalf("no probe row has the prefix %q", s.prefix)
			}
			if !slices.Equal(red, want) {
				t.Fatalf("sabotaging %s's raw-bits test redded %d rows, want exactly the %d %s rows\n  red:  %v\n  want: %v",
					s.file, len(red), len(want), s.prefix, red, want)
			}
		})
	}
}
