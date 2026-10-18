package luautable_test

import (
	"encoding/json"
	"fmt"
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
local load = require("./load")
local inputs = require("./inputs")

local flags = {}
local function collect(mod)
	for k, v in pairs(mod) do
		if type(k) == "string" and string.sub(k, 1, 8) == "FlagName" and type(v) == "function" then
			flags[string.sub(k, 9)] = v
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

local function num(x)
	if typeof(x) == "integer" then return tostring(x) end
	if is128(x) then
		local hi = tonumber(tostring(x.hi)) or 0
		local lo = tonumber(tostring(x.lo)) or 0
		return string.format("%.0f", hi * 18446744073709551616.0 + lo)
	end
	return string.format("%.17g", x)
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

local function str_value(fd, val, owner)
	if type(val) == "string" then return val end
	if type(val) == "buffer" then
		local n = buffer.len(val)
		if owner ~= nil and owner[fd.slot .. "Length"] ~= nil then n = owner[fd.slot .. "Length"] end
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

local function map_key(fd, val)
	if fd.s == "enum" then return enum_name(fd, val) end
	if type(val) == "string" then return val end
	if type(val) == "buffer" then return buffer.readstring(val, 0, buffer.len(val)) end
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
			if arr[i] ~= nil then parts[#parts + 1] = q(kv[1]) .. ":" .. encode_value(fd.e, arr[i], owner) end
		end
		return "{" .. table.concat(parts, ",") .. "}"
	end
	local n = #arr
	if fd.array == "fixed" then n = fd.bound
	elseif fd.array == "counted" and owner ~= nil then n = owner[fd.slot .. "Count"] or 0 end
	local parts = {}
	for i = 1, n do
		local x = arr[i]
		if x == nil then x = default_value(fd.e) end
		parts[#parts + 1] = encode_value(fd.e, x, owner)
	end
	return "[" .. table.concat(parts, ",") .. "]"
end

encode_value = function(fd, val, owner)
	if fd == nil then return num(val) end
	local s = fd.s
	if s == "bool" then return tostring(val == true) end
	if s == "int" then
		local fn = flags[fd.slot]
		if fn ~= nil and owner ~= nil then
			local parts = {}
			local mask = owner[fd.slot] or 0
			if typeof(mask) == "integer" then mask = tonumber(tostring(mask)) or 0 end
			for i = 0, 63 do
				local bit = 2 ^ i
				if mask % (bit * 2) >= bit then
					local name = fn(i)
					if name ~= "???" then parts[#parts + 1] = q(name) end
				end
			end
			return "[" .. table.concat(parts, ",") .. "]"
		end
		if fd.fbits ~= nil and fd.fbits > 0 then
			local x = tonumber(tostring(val)) or 0
			return num(x / (2 ^ fd.fbits))
		end
		return num(val)
	end
	if s == "float" then return num(val) end
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
			parts[#parts + 1] = q(map_key(keyfd, e[keyfd.slot])) .. ":" .. encode_value(valfd, e[valfd.slot], e)
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
				count_walk(nil, nil)
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

for _, name in Modules do collect(name) end

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
	b.WriteString("local Modules = {}\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "Modules[#Modules + 1] = require(\"./%s\")\n", m)
	}
	b.WriteString(strings.Replace(roundtripMain, "ROOT", strconv.Quote(rootName), -1))
	return b.String()
}

func normalizeNumbers(v any) any {
	switch x := v.(type) {
	case json.Number:
		f, err := strconv.ParseFloat(x.String(), 64)
		if err != nil {
			return x.String()
		}
		return f
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
	var failures []string
	for _, row := range manifestRows(t) {
		if row.kind != "instance" {
			continue
		}
		if _, ok := units[row.unit]; !ok {
			continue
		}
		golden := filepath.Join(root, "testdata", "conformance", "tables", "json", row.caseName+".json")
		want, err := os.ReadFile(golden)
		if os.IsNotExist(err) {
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
	if len(compared) == 0 {
		t.Fatal("no instance row compared")
	}
	for _, f := range failures {
		t.Error(f)
	}
}
