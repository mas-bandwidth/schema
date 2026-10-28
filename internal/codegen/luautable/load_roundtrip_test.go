package luautable_test

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
)

// roundtripMain is the test-side text writer (docs/SPEC-TABLES.md §16): it
// walks the loaded value and the emitted descriptors and prints the value as
// §16's JSON text. It is written into t.TempDir(), never shipped.
const roundtripMain = `local S = require("./schema_tables")
local load = require("./schema_load")
local inputs = require("./inputs")

-- flagsOf maps a field, arm or array descriptor to its flags' variant names
-- (§16.2, the flags row), from the FLAGS table the Go side writes
local flagsOf = {}
for key, names in FLAGS do
	local dot = string.find(key, ".", 1, true)
	local ok, d = pcall(S.lookup, string.sub(key, 1, dot - 1))
	local fname = string.sub(key, dot + 1)
	if ok and d ~= nil then
		for _, f in d.fields or {} do
			if f.name == fname then flagsOf[f] = names end
		end
		for _, a in d.arms or {} do
			if a.name == fname then flagsOf[a] = names end
		end
	end
end

local function q(s)
	s = s:gsub("\\", "\\\\"):gsub('"', '\\"'):gsub("\n", "\\n"):gsub("\r", "\\r"):gsub("\t", "\\t")
	s = s:gsub("[%z\1-\31]", function(c) return string.format("\\u%04x", string.byte(c)) end)
	return '"' .. s .. '"'
end

local function is128(x)
	return type(x) == "table" and x.lo ~= nil and x.hi ~= nil
end

-- EXACT NUMBERS (§16.2, Numbers: "nothing on their path is a double"). A
-- magnitude is little-endian 16-bit limbs; an integer or Int128 is read by
-- its bits, signed or not as its field declares.
local function limbs64(x, out)
	for i = 0, 3 do
		out[#out + 1] = integer.tonumber(integer.band(integer.rshift(x, integer.create(16 * i)), integer.create(0xffff)))
	end
end

local function magnitude(x, signed)
	local m = {}
	if is128(x) then limbs64(x.lo, m); limbs64(x.hi, m)
	elseif typeof(x) == "integer" then limbs64(x, m)
	else
		local neg = x < 0
		local a = math.abs(x)
		while a > 0 do m[#m + 1] = a % 65536; a = math.floor(a / 65536) end
		return neg, m
	end
	if signed ~= false and m[#m] >= 0x8000 then
		-- two's complement: invert, add one
		local carry = 1
		for i = 1, #m do
			local y = (65535 - m[i]) + carry
			m[i] = y % 65536
			carry = math.floor(y / 65536)
		end
		return true, m
	end
	return false, m
end

local function mul_small(m, k)
	local carry = 0
	for i = 1, #m do
		local y = m[i] * k + carry
		m[i] = y % 65536
		carry = math.floor(y / 65536)
	end
	while carry > 0 do m[#m + 1] = carry % 65536; carry = math.floor(carry / 65536) end
end

local function digits(m)
	local chunks = {}
	local w = table.clone(m)
	while true do
		local zero = true
		for i = 1, #w do if w[i] ~= 0 then zero = false; break end end
		if zero then break end
		local rem = 0
		for i = #w, 1, -1 do
			local y = rem * 65536 + w[i]
			w[i] = math.floor(y / 10000)
			rem = y % 10000
		end
		table.insert(chunks, 1, rem)
	end
	if #chunks == 0 then return "0" end
	local parts = { tostring(chunks[1]) }
	for i = 2, #chunks do parts[i] = string.format("%04d", chunks[i]) end
	return table.concat(parts)
end

-- an integer field's value: its decimal digits, exact past 2^53
local function int_text(x, signed)
	local neg, m = magnitude(x, signed)
	local d = digits(m)
	if neg and d ~= "0" then return "-" .. d end
	return d
end

-- a fixed field's value: raw / 2^F as the exact decimal (§16.2, the fixed
-- row), raw * 5^F with the point F digits from the right
local function fixed_text(x, signed, fbits)
	local neg, m = magnitude(x, signed)
	for _ = 1, fbits do mul_small(m, 5) end
	local d = digits(m)
	while #d <= fbits do d = "0" .. d end
	local whole = string.sub(d, 1, #d - fbits)
	local frac = string.gsub(string.sub(d, #d - fbits + 1), "0+$", "")
	local out = whole
	if frac ~= "" then out = whole .. "." .. frac end
	if neg and out ~= "0" then out = "-" .. out end
	return out
end

-- a float's value: the shortest decimal that reads back as the same float32
-- or float64 (§16.2, the float row)
local function as_f32(x)
	local b = buffer.create(4)
	buffer.writef32(b, 0, x)
	return buffer.readf32(b, 0)
end

local function float_text(x, is32)
	if x ~= x or x == math.huge or x == -math.huge then return "null" end
	for p = 1, 17 do
		local s = string.format("%." .. p .. "g", x)
		local y = tonumber(s)
		if y ~= nil and ((is32 and as_f32(y) == x) or (not is32 and y == x)) then return s end
	end
	return string.format("%.17g", x)
end

local function num(x)
	if typeof(x) == "integer" or is128(x) then return int_text(x, true) end
	if x == math.floor(x) and math.abs(x) < 2 ^ 53 then return string.format("%.0f", x) end
	return float_text(x, false)
end

local function flags_text(names, mask)
	local m = mask or 0
	if typeof(m) ~= "integer" then m = integer.create(m) end
	local parts = {}
	for i, name in names do
		if integer.btest(m, integer.lshift(1i, integer.create(i - 1))) then parts[#parts + 1] = q(name) end
	end
	return "[" .. table.concat(parts, ",") .. "]"
end

local B64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
local function b64(s)
	local out = {}
	local n = #s
	for i = 1, n, 3 do
		local a = string.byte(s, i) or 0
		local b = string.byte(s, i + 1)
		local c = string.byte(s, i + 2)
		local v = a * 65536 + (b or 0) * 256 + (c or 0)
		out[#out + 1] = string.sub(B64, math.floor(v / 262144) % 64 + 1, math.floor(v / 262144) % 64 + 1)
		out[#out + 1] = string.sub(B64, math.floor(v / 4096) % 64 + 1, math.floor(v / 4096) % 64 + 1)
		if b ~= nil then out[#out + 1] = string.sub(B64, math.floor(v / 64) % 64 + 1, math.floor(v / 64) % 64 + 1) else out[#out + 1] = "=" end
		if c ~= nil then out[#out + 1] = string.sub(B64, v % 64 + 1, v % 64 + 1) else out[#out + 1] = "=" end
	end
	return table.concat(out)
end

local function bufstr(v)
	if type(v) == "string" then return v end
	if type(v) == "buffer" then return buffer.readstring(v, 0, buffer.len(v)) end
	return ""
end

local encode_value, encode_obj, count_walk

local function default_value(e)
	if e == nil then return 0 end
	if e.s == "tab" or e.s == "union" then return S.lookup(e.t).new() end
	if e.s == "ptr" or e.s == "sblob" or e.s == "bblob" then return nil end
	if e.s == "str" or e.s == "text" or e.s == "bytes" then return e.def or "" end
	if e.s == "bool" then return e.def or false end
	if e.s == "enum" then return e.def or "None" end
	return e.def or 0
end

-- a wstring(N) is a buffer of UTF-16LE code units with its Length in code
-- units (docs/PORT-LUAU.md, THE TYPE MAPPING): the text is its UTF-8
local function utf16_text(b, units)
	local out = {}
	local i = 0
	while i < units do
		local u = buffer.readu16(b, i * 2)
		if u >= 0xD800 and u <= 0xDBFF and i + 1 < units then
			local lo = buffer.readu16(b, (i + 1) * 2)
			if lo >= 0xDC00 and lo <= 0xDFFF then
				u = 0x10000 + (u - 0xD800) * 0x400 + (lo - 0xDC00)
				i = i + 1
			end
		end
		out[#out + 1] = utf8.char(u)
		i = i + 1
	end
	return table.concat(out)
end

local function str_value(fd, val, owner)
	if type(val) == "string" then return val end
	if type(val) == "buffer" then
		local n = buffer.len(val)
		if owner ~= nil and fd.slot ~= nil and owner[fd.slot .. "Length"] ~= nil then n = owner[fd.slot .. "Length"] end
		if fd.wide then
			if owner == nil or fd.slot == nil or owner[fd.slot .. "Length"] == nil then n = buffer.len(val) / 2 end
			return utf16_text(val, n)
		end
		return buffer.readstring(val, 0, n)
	end
	return fd.def or ""
end

local function enum_name(fd, val)
	if type(val) == "string" then return val end
	if val == nil then return "None" end
	if fd.vs ~= nil and val > 0 and fd.vs[val] ~= nil then return fd.vs[val][1] end
	return "None"
end

local function map_key(fd, val, entry)
	if fd.s == "enum" then return enum_name(fd, val) end
	if type(val) == "string" then return val end
	if type(val) == "buffer" then return str_value(fd, val, entry) end
	if typeof(val) == "integer" then return tostring(val) end
	return tostring(val)
end

local counts, labels, next_label

local function node_fields(desc, v)
	local parts = {}
	for _, fd in desc.fields do
		if fd.present == nil or v[fd.present] then
			parts[#parts + 1] = q(fd.json) .. ":" .. encode_value(fd, v[fd.slot], v)
		end
	end
	return table.concat(parts, ",")
end

encode_obj = function(desc, v)
	return "{" .. node_fields(desc, v) .. "}"
end

local function encode_array(fd, arr, owner)
	arr = arr or {}
	if fd.array == "keyed" and fd.keyvs ~= nil then
		local parts = {}
		for i, kv in fd.keyvs do
			if arr[i] ~= nil then
				local inner
				if flagsOf[fd] ~= nil then inner = flags_text(flagsOf[fd], arr[i]) else inner = encode_value(fd.e, arr[i], owner) end
				parts[#parts + 1] = q(kv[1]) .. ":" .. inner
			end
		end
		return "{" .. table.concat(parts, ",") .. "}"
	end
	local n = #arr
	if fd.array == "fixed" then n = fd.bound
	elseif fd.array == "counted" and owner ~= nil then n = owner[fd.slot .. "Count"] or 0 end
	local parts = {}
	for i = 1, n do
		local x = arr[i]
		-- a hole in a pointer list is a loader defect: a null is false (§2.1)
		if x == nil and fd.e ~= nil and fd.e.s == "ptr" and fd.array == "list" then return q("hole in a pointer list") end
		if x == nil then x = default_value(fd.e) end
		if flagsOf[fd] ~= nil then parts[#parts + 1] = flags_text(flagsOf[fd], x)
		else parts[#parts + 1] = encode_value(fd.e, x, owner) end
	end
	return "[" .. table.concat(parts, ",") .. "]"
end

encode_value = function(fd, val, owner)
	if fd == nil then return num(val) end
	local s = fd.s
	if s == "bool" then return tostring(val == true) end
	if s == "int" then
		if flagsOf[fd] ~= nil then return flags_text(flagsOf[fd], val) end
		if fd.fbits ~= nil and fd.fbits > 0 then return fixed_text(val or 0, fd.signed, fd.fbits) end
		return int_text(val or 0, fd.signed)
	end
	if s == "float" then return float_text(val or 0, fd.kw == 4 or fd.k == 10) end
	if s == "str" or s == "text" then return q(str_value(fd, val, owner)) end
	if s == "bytes" then
		if type(val) == "table" then
			local bytes = {}
			for i = 1, #val do bytes[i] = string.char(val[i]) end
			return q(b64(table.concat(bytes)))
		end
		return q(b64(str_value(fd, val, owner)))
	end
	if s == "enum" then return q(enum_name(fd, val)) end
	if s == "tab" then return encode_obj(S.lookup(fd.t), val) end
	if s == "union" then
		if val == nil or val.Type == nil or val.Type == 0 then return "{}" end
		local un = S.lookup(fd.t)
		for _, arm in un.arms do
			if arm.tag == val.Type then
				local inner
				if arm.s == "void" then inner = "null"
				else inner = encode_value(arm, val[arm.slot], val) end
				return "{" .. q(arm.json or arm.name) .. ":" .. inner .. "}"
			end
		end
		return "{}"
	end
	if s == "sblob" then
		if val == nil then return "null" end
		return q(bufstr(val))
	end
	if s == "bblob" then
		if val == nil then return "null" end
		return q(b64(bufstr(val)))
	end
	if s == "ptr" then
		if val == nil or val == false then return "null" end
		if labels[val] ~= nil then return '{"&node":' .. labels[val] .. "}" end
		local head = "{"
		if counts[val] ~= nil and counts[val] > 1 then
			labels[val] = next_label
			next_label = next_label + 1
			head = '{"&node":' .. labels[val] .. ","
		end
		return head .. node_fields(S.lookup(fd.t), val) .. "}"
	end
	if s == "arr" then return encode_array(fd, val, owner) end
	if s == "map" then
		local entry = S.lookup(fd.t)
		local keyfd, valfd = entry.fields[1], entry.fields[2]
		local parts = {}
		for _, e in val or {} do
			parts[#parts + 1] = q(map_key(keyfd, e[keyfd.slot], e)) .. ":" .. encode_value(valfd, e[valfd.slot], e)
		end
		return "{" .. table.concat(parts, ",") .. "}"
	end
	return "null"
end

count_walk = function(fd, val)
	if fd == nil or val == nil then return end
	local s = fd.s
	if s == "ptr" then
		if val ~= false then
			if counts[val] == nil then
				counts[val] = 1
				local desc = S.lookup(fd.t)
				for _, f in desc.fields do
					if f.present == nil or val[f.present] then count_walk(f, val[f.slot]) end
				end
			else
				counts[val] = counts[val] + 1
			end
		end
	elseif s == "tab" then
		local desc = S.lookup(fd.t)
		for _, f in desc.fields do
			if f.present == nil or val[f.present] then count_walk(f, val[f.slot]) end
		end
	elseif s == "union" then
		if val ~= nil and val.Type ~= nil and val.Type ~= 0 then
			local un = S.lookup(fd.t)
			for _, arm in un.arms do
				if arm.tag == val.Type and arm.s ~= "void" then count_walk(arm, val[arm.slot]) end
			end
		end
	elseif s == "arr" then
		local n = #val
		if fd.array == "fixed" then n = fd.bound end
		for i = 1, n do count_walk(fd.e, val[i]) end
	elseif s == "map" then
		local entry = S.lookup(fd.t)
		local valfd = entry.fields[2]
		for _, e in val or {} do count_walk(valfd, e[valfd.slot]) end
	end
end


local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = load.load(ROOT, inputs.wire, report)
local desc = S.lookup(ROOT)
counts, labels, next_label = {}, {}, 1
for _, f in desc.fields do
	if f.present == nil or v[f.present] then count_walk(f, v[f.slot]) end
end
print(encode_obj(desc, v))
`

func roundtripProbe(g *generatedUnit, rootName string) string {
	var b strings.Builder
	var mods []string
	runtime := map[string]bool{"load": true, "fixed": true, "schema_tables": true, "schema_runtime": true}
	for name := range g.files {
		if strings.HasSuffix(name, ".luau") && !runtime[strings.TrimSuffix(name, ".luau")] {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)
	b.WriteString("local FLAGS = {\n")
	var keys []string
	for k := range g.flags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "\t[%q] = {", k)
		for _, n := range g.flags[k] {
			fmt.Fprintf(&b, " %q,", n)
		}
		b.WriteString(" },\n")
	}
	b.WriteString("}\n")
	b.WriteString("local Modules = {}\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "Modules[#Modules + 1] = require(\"./%s\")\n", m)
	}
	b.WriteString(strings.ReplaceAll(roundtripMain, "ROOT", strconv.Quote(rootName)))
	return b.String()
}

// normalizeNumbers replaces each json.Number with its exact rational value,
// so 1000000.0 and 1000000 are one value and no wide or fixed value passes
// through a double (docs/SPEC-TABLES.md §16.2, Numbers).
func normalizeNumbers(v any) any {
	switch x := v.(type) {
	case json.Number:
		r, ok := new(big.Rat).SetString(x.String())
		if !ok {
			return "number:" + x.String()
		}
		return "number:" + r.RatString()
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = normalizeNumbers(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = normalizeNumbers(val)
		}
		return out
	}
	return v
}

func decodeText(t *testing.T, data []byte, what string) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("%s is not JSON: %v\n%s", what, err, data)
	}
	return normalizeNumbers(v)
}

// TestLuauTableLoadRoundTripsEveryCleanInstance loads every instance row the
// Luau target generates and compares the loaded value with the row's §16 text
// (testdata/conformance/tables/json/<name>.json): the test-side writer prints
// the loaded value as §16's text, and the Go side compares the two after
// encoding/json decodes both with UseNumber. Every instance row with a text is
// compared and none is skipped.
func TestLuauTableLoadRoundTripsEveryCleanInstance(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	cache := map[string]*generatedUnit{}
	var compared []string
	var noText []string
	var failures []string
	var refusedRows []string
	for _, row := range manifestRows(t) {
		if row.kind != "instance" {
			continue
		}
		if _, ok := units[row.unit]; !ok {
			continue
		}
		// a unit the Luau target refuses by name (luautable_test.go) is named, never compared
		if _, ok := refused[row.unit]; ok {
			refusedRows = append(refusedRows, row.caseName+" ("+row.unit+")")
			continue
		}
		golden := filepath.Join(root, "testdata", "conformance", "tables", "json", row.caseName+".json")
		want, err := os.ReadFile(golden)
		if os.IsNotExist(err) {
			noText = append(noText, row.caseName)
			continue
		}
		if err != nil {
			t.Fatalf("case %s: %v", row.caseName, err)
		}
		g, ok := cache[row.unit]
		if !ok {
			g = generateUnit(t, units, row.unit)
			cache[row.unit] = g
		}
		if g.err != nil {
			t.Fatalf("case %s: unit %s does not generate: %v", row.caseName, row.unit, g.err)
		}
		src := roundtripProbe(g, row.root)
		if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		wire, err := os.ReadFile(filepath.Join(root, row.wire))
		if err != nil {
			t.Fatalf("case %s: %v", row.caseName, err)
		}
		gotText := luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"wire": wire})
		got := decodeText(t, gotText, "the loaded value")
		wantV := decodeText(t, want, golden)
		compared = append(compared, row.caseName)
		if !reflect.DeepEqual(got, wantV) {
			failures = append(failures, fmt.Sprintf("%s:\nloaded: %s\ngolden: %s", row.caseName, gotText, want))
		}
	}
	sort.Strings(compared)
	t.Logf("%d instance rows compared: %s", len(compared), strings.Join(compared, " "))
	sort.Strings(noText)
	t.Logf("%d instance rows have no text under testdata/conformance/tables/json: %s", len(noText), strings.Join(noText, " "))
	sort.Strings(refusedRows)
	t.Logf("%d instance rows of refused units: %s", len(refusedRows), strings.Join(refusedRows, " "))
	if len(compared) == 0 {
		t.Fatal("no instance row compared")
	}
	for _, f := range failures {
		t.Error(f)
	}
}
