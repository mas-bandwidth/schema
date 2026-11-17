package luautable_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// luauJsonIntegersSchema is the unit testdata/wire/json-integers/ reads.
const luauJsonIntegersSchema = `package jsonintegers

table Integers
{
    small   int32
    wide    int64
    uwide   uint64
    wide128 int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    ranged  int64 | min = 0, max = 9007199254740993
    frac    fixed(48, 16) | min = -10, max = 10
    counts  map[uint64]int32
}
`

type luauIntegerVector struct {
	name    string
	json    string
	verdict string
}

func luauIntegerVectors(t *testing.T, dir, manifest string) []luauIntegerVector {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, manifest))
	if err != nil {
		t.Fatal(err)
	}
	var rows []luauIntegerVector
	i := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		i++
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			t.Fatalf("%s/%s:%d: want five columns, got %d", dir, manifest, i, len(fields))
		}
		if fields[0] == "" || fields[2] == "" || (fields[3] != "read" && fields[3] != "refused") {
			t.Fatalf("%s/%s:%d: bad vector row %q", dir, manifest, i, line)
		}
		rows = append(rows, luauIntegerVector{name: fields[0], json: fields[2], verdict: fields[3]})
	}
	if len(rows) == 0 {
		t.Fatal("no vector rows; the gate would pass over an empty set")
	}
	return rows
}

func luauIntegerProbe(g *generatedUnit, rows []luauIntegerVector) string {
	var b strings.Builder
	b.WriteString(jsonModules(g))
	b.WriteString(`local T = require("./IntegersTable")
local json = require("./schema_json")
json.SetFlags(SCHEMA_JSON_FLAGS)
local inputs = require("./inputs")

local function hex(buf)
	if buf == nil then return "nil" end
	local n = buffer.len(buf)
	local parts = table.create(n)
	for i = 0, n - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(buf, i))
	end
	return table.concat(parts)
end

local function safe_to_json(root, v)
	local ok, res = pcall(function() return json.ToJson(root, v, nil) end)
	if not ok then return "err:" .. tostring(res):gsub("[\r\n|]+", " ") end
	return hex(res)
end

local function check_value(v)
	if v.Small ~= 7 then return "small=" .. tostring(v.Small) end
	if v.Wide ~= -42i then return "wide=" .. string.format("%d", v.Wide) end
	if v.Uwide ~= -1i then return "uwide=" .. string.format("%u", v.Uwide) end
	if v.Wide128.lo ~= -1i or v.Wide128.hi ~= 0x7fffffffffffffffi then
		return string.format("wide128=%x:%x", v.Wide128.hi, v.Wide128.lo)
	end
	if v.Ranged ~= 9007199254740993i then return "ranged=" .. string.format("%d", v.Ranged) end
	if v.Frac ~= 32768i then return "frac=" .. string.format("%d", v.Frac) end
	if #v.Counts ~= 2 then return "counts_len=" .. tostring(#v.Counts) end
	local got0, gotBig = nil, nil
	for _, e in v.Counts do
		if e.Key == 0i then got0 = e.Value
		elseif e.Key == 9007199254740993i then gotBig = e.Value end
	end
	if got0 ~= 1 or gotBig ~= 2 then
		return "counts=" .. tostring(got0) .. "," .. tostring(gotBig)
	end
	return "exact"
end

local built = T.NewIntegers()
built.Small = 7
built.Wide = -42i
built.Uwide = -1i
built.Wide128 = { lo = -1i, hi = 0x7fffffffffffffffi }
built.Ranged = 9007199254740993i
built.Frac = 32768i
built.Counts = {
	{ Key = 0i, Value = 1 },
	{ Key = 9007199254740993i, Value = 2 },
}
print("built|ok|" .. safe_to_json("Integers", built))

local rows = {
`)
	for i := range rows {
		fmt.Fprintf(&b, "\t%q,\n", fmt.Sprintf("row%d", i))
	}
	b.WriteString(`}
for _, key in rows do
	local report = {}
	local v = json.FromJson("Integers", inputs[key], report)
	if v == nil then
		print(key .. "|refused|" .. tostring(report.reason))
	else
		print(key .. "|ok|" .. check_value(v) .. "|" .. safe_to_json("Integers", v))
	end
end
`)
	return b.String()
}

// TestLuauJsonWideIntegersAreStrings holds docs/SPEC-TABLES.md §16.2 and
// §16.14's two JSON integer rules (docs/rulings.json
// json-wide-integers-are-strings and json-integer-token-only) over the
// committed vectors under testdata/wire/json-integers/ and across every
// integer site: a wide integer is written as a JSON string of decimal digits
// and reads as a string only; every integer field takes an integer
// token only, never passing through a double.
func TestLuauJsonWideIntegersAreStrings(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	dir := filepath.Join(root, "testdata", "wire", "json-integers")
	rows := luauIntegerVectors(t, dir, "MANIFEST.tsv")
	ordinaryRows := len(rows)
	numberRows := luauIntegerVectors(t, dir, "NUMBER-TOKEN.tsv")
	if len(numberRows) != 13 {
		t.Fatalf("NUMBER-TOKEN.tsv: got %d rows, want 13", len(numberRows))
	}
	for _, row := range numberRows {
		if row.verdict != "refused" {
			t.Fatalf("%s: numeric-token verdict %q, want refused", row.name, row.verdict)
		}
	}
	rows = append(rows, numberRows...)
	canonical, err := os.ReadFile(filepath.Join(dir, "canonical.read.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantCanonical := append(bytes.Clone(canonical), '\n')
	if len(canonical) > 0 && canonical[len(canonical)-1] == '\n' {
		wantCanonical = bytes.Clone(canonical)
	}
	wantHex := hexBytes(wantCanonical)

	g := generateFiles(t, map[string]string{"Integers.schema": luauJsonIntegersSchema})
	inputs := map[string][]byte{}
	for i, row := range rows {
		body, err := os.ReadFile(filepath.Join(dir, row.json))
		if err != nil {
			t.Fatal(err)
		}
		inputs[fmt.Sprintf("row%d", i)] = body
	}
	got := runProbe(t, g, luauIntegerProbe(g, rows), inputs)
	byKey := map[string]string{}
	for line := range strings.SplitSeq(got, "\n") {
		key, rest, ok := strings.Cut(line, "|")
		if !ok {
			t.Fatalf("probe line %q has no verdict", line)
		}
		byKey[key] = rest
	}
	if byKey["built"] != "ok|"+wantHex {
		t.Errorf("direct ToJson mismatch:\n got: %s\nwant: ok|%s", byKey["built"], wantHex)
	}
	for i, row := range rows {
		key := fmt.Sprintf("row%d", i)
		line := byKey[key]
		if row.verdict == "refused" {
			if !strings.HasPrefix(line, "refused|") {
				t.Errorf("%s: read accepted refused vector: %s", row.name, line)
			}
			if i >= ordinaryRows && line != "refused|kind_mismatch" {
				t.Errorf("%s: got %s, want kind_mismatch", row.name, line)
			}
			continue
		}
		if line != "ok|exact|"+wantHex {
			t.Errorf("%s: read or wrote vector differently:\n got: %s\nwant: ok|exact|%s", row.name, line, wantHex)
		}
	}

	t.Run("integer sites", func(t *testing.T) {
		t.Parallel()
		luauJsonIntegerSites(t)
	})
}

const luauJsonSitesSchema = `package jsonsites

enum Slot { A, B }

flags Marks { X, Y }

union Pick
{
    wide    int64
    wide128 uint128
    narrow  int32
}

table Sites
{
    narrow      int32
    bits32      bits(32)
    fixed32     fixed(16, 16) | min = -10, max = 10
    wide        int64
    uwide       uint64
    ranged_u64  uint64 | min = 0, max = 18446744073709551615
    uwide128    uint128
    bits33      bits(33)
    bits54      bits(54)
    bits64      bits(64)
    fixed64     fixed(48, 16) | min = -10, max = 10
    fixed128    fixed(2, 126) | min = -1, max = 1
    counts      map[uint64]int64
    narrow_map  map[int32]int32
    list        [3]int64
    ulist       [..3]uint64
    list128     []uint128
    narrow_list [2]int32
    keyed       [Slot]int64
    opt         ?int64
    opt128      ?uint128
    opt_narrow  ?int32
    marks       Marks
    pick        Pick
}
`

const luauJsonSitesCanonical = `{
  "narrow": -7,
  "bits32": 4294967295,
  "fixed32": 0.5,
  "wide": "-9007199254740993",
  "uwide": "18446744073709551615",
  "ranged_u64": "18446744073709551615",
  "uwide128": "340282366920938463463374607431768211455",
  "bits33": "8589934591",
  "bits54": "18014398509481983",
  "bits64": "18446744073709551615",
  "fixed64": "-1.5",
  "fixed128": "-0.5",
  "counts": {
    "1": "-9007199254740993",
    "9007199254740993": "9007199254740993"
  },
  "narrow_map": {
    "-3": 9
  },
  "list": [
    "-9007199254740993",
    "0",
    "9007199254740993"
  ],
  "ulist": [
    "1",
    "9007199254740993",
    "18446744073709551615"
  ],
  "list128": [
    "0",
    "340282366920938463463374607431768211455"
  ],
  "narrow_list": [
    -1,
    2
  ],
  "keyed": {
    "A": "-9007199254740993",
    "B": "9007199254740993"
  },
  "opt": "-9007199254740993",
  "opt128": "340282366920938463463374607431768211455",
  "opt_narrow": 12,
  "marks": [
    "X"
  ],
  "pick": {
    "wide": "-9007199254740993"
  }
}`

const luauJsonSitesNumbers = `{
  "narrow": -7,
  "bits32": 4294967295,
  "fixed32": 0.5,
  "wide": -9007199254740993,
  "uwide": 18446744073709551615,
  "ranged_u64": 18446744073709551615,
  "uwide128": 340282366920938463463374607431768211455,
  "bits33": 8589934591,
  "bits54": 18014398509481983,
  "bits64": 18446744073709551615,
  "fixed64": -1.5,
  "fixed128": -0.5,
  "counts": {
    "1": -9007199254740993,
    "9007199254740993": 9007199254740993
  },
  "narrow_map": {
    "-3": 9
  },
  "list": [
    -9007199254740993,
    0,
    9007199254740993
  ],
  "ulist": [
    1,
    9007199254740993,
    18446744073709551615
  ],
  "list128": [
    0,
    340282366920938463463374607431768211455
  ],
  "narrow_list": [
    -1,
    2
  ],
  "keyed": {
    "A": -9007199254740993,
    "B": 9007199254740993
  },
  "opt": -9007199254740993,
  "opt128": 340282366920938463463374607431768211455,
  "opt_narrow": 12,
  "marks": [
    "X"
  ],
  "pick": {
    "wide": -9007199254740993
  }
}`

var luauJsonSitesNumberTokens = map[string]string{
	"plain-number":    `{"wide": 42}`,
	"array-number":    `{"list": [42, "0", "0"]}`,
	"optional-number": `{"opt": 42}`,
	"union-number":    `{"pick": {"wide": 42}}`,
	"map-number":      `{"counts": {"1": 42}}`,
	"keyed-number":    `{"keyed": {"A": 42}}`,
	"bits33-number":   `{"bits33": 42}`,
	"bits64-number":   `{"bits64": 42}`,
	"uint128-number":  `{"uwide128": 42}`,
	"fixed64-number":  `{"fixed64": 1.5}`,
	"fixed128-number": `{"fixed128": 0.5}`,
}

var luauJsonSitesRefused = map[string]string{
	"list-fraction":           `{"list": [-1.5, "0", "9007199254740993"]}`,
	"list-exponent":           `{"list": [1e3, "0", "9007199254740993"]}`,
	"list-neg-zero":           `{"list": [-0, "0", "9007199254740993"]}`,
	"list-string-fraction":    `{"list": ["-1.5", "0", "9007199254740993"]}`,
	"list-string-neg-zero":    `{"list": ["-0", "0", "9007199254740993"]}`,
	"ulist-one-past":          `{"ulist": ["18446744073709551616"]}`,
	"list128-one-past-string": `{"list128": ["340282366920938463463374607431768211456"]}`,
	"list128-one-past-number": `{"list128": [340282366920938463463374607431768211456]}`,
	"keyed-fraction":          `{"keyed": {"A": -1.5}}`,
	"keyed-string-fraction":   `{"keyed": {"A": "-1.5"}}`,
	"keyed-string-neg-zero":   `{"keyed": {"A": "-0"}}`,
	"map-value-fraction":      `{"counts": {"1": -1.5}}`,
	"map-value-string-exp":    `{"counts": {"1": "1e2"}}`,
	"map-value-neg-zero":      `{"counts": {"1": "-0"}}`,
	"map-key-fraction":        `{"counts": {"1.0": "1"}}`,
	"map-key-exponent":        `{"counts": {"1e0": "1"}}`,
	"map-key-neg-zero":        `{"counts": {"-0": "1"}}`,
	"map-key-leading-space":   `{"counts": {" 1": "1"}}`,
	"map-key-one-past":        `{"counts": {"18446744073709551616": "1"}}`,
	"narrow-map-key-past":     `{"narrow_map": {"2147483648": 1}}`,
	"optional-fraction":       `{"opt": -1.5}`,
	"optional-neg-zero":       `{"opt": -0}`,
	"optional-str-neg-zero":   `{"opt": "-0"}`,
	"optional128-str-frac":    `{"opt128": "1.0"}`,
	"arm-fraction":            `{"pick": {"wide": -1.5}}`,
	"arm-str-neg-zero":        `{"pick": {"wide": "-0"}}`,
	"arm128-str-exp":          `{"pick": {"wide128": "1e3"}}`,
	"bits33-past-string":      `{"bits33": "8589934592"}`,
	"bits33-past-number":      `{"bits33": 8589934592}`,
	"bits54-past-string":      `{"bits54": "18014398509481984"}`,
	"bits54-past-number":      `{"bits54": 18014398509481984}`,
	"narrow-string":           `{"narrow": "7"}`,
	"bits32-string":           `{"bits32": "7"}`,
	"fixed32-string":          `{"fixed32": "0.5"}`,
	"narrow-list-string":      `{"narrow_list": ["1", 2]}`,
	"opt-narrow-string":       `{"opt_narrow": "1"}`,
	"arm-narrow-string":       `{"pick": {"narrow": "1"}}`,
	"fixed64-space":           `{"fixed64": " -1.5"}`,
	"fixed64-plus":            `{"fixed64": "+1.5"}`,
	"fixed64-junk":            `{"fixed64": "abc"}`,
	"fixed128-lead-zero":      `{"fixed128": "01.0"}`,
	"flags-string":            `{"marks": "X"}`,
}

func luauJsonSitesProbe(g *generatedUnit, keys []string) string {
	var b strings.Builder
	b.WriteString(jsonModules(g))
	b.WriteString(`local T = require("./SitesTable")
local json = require("./schema_json")
json.SetFlags(SCHEMA_JSON_FLAGS)
local save = require("./schema_save")
local St = require("./schema_tables")
local inputs = require("./inputs")

local function hex(buf)
	if buf == nil then return "nil" end
	local n = buffer.len(buf)
	local parts = table.create(n)
	for i = 0, n - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(buf, i))
	end
	return table.concat(parts)
end

local function safe_to_json(root, v)
	local ok, res = pcall(function() return json.ToJson(root, v, nil) end)
	if not ok then return "err:" .. tostring(res):gsub("[\r\n|]+", " ") end
	return hex(res)
end

local bad54 = T.NewSites()
bad54.Bits54 = 18014398509481984i
print("write-bits54-past|" .. (json.ToJson("Sites", bad54, nil) == nil and "refused" or "accepted"))

local keys = {
`)
	for _, key := range keys {
		fmt.Fprintf(&b, "\t%q,\n", key)
	}
	b.WriteString(`}
local desc = St.lookup("Sites")
for _, key in keys do
	local report = {}
	local v = json.FromJson("Sites", inputs[key], report)
	if v == nil then
		print(key .. "|refused|" .. tostring(report.reason))
	else
		local wire = save.save(desc, v)
		print(key .. "|ok|" .. hex(wire) .. "|" .. safe_to_json("Sites", v))
	end
end
`)
	return b.String()
}

func luauJsonIntegerSites(t *testing.T) {
	t.Helper()
	g := generateFiles(t, map[string]string{"Sites.schema": luauJsonSitesSchema})
	inputs := map[string][]byte{
		"canonical": []byte(luauJsonSitesCanonical),
		"numbers":   []byte(luauJsonSitesNumbers),
	}
	keys := []string{"canonical", "numbers"}
	for key, body := range luauJsonSitesNumberTokens {
		inputs[key] = []byte(body)
		keys = append(keys, key)
	}
	for key, body := range luauJsonSitesRefused {
		inputs[key] = []byte(body)
		keys = append(keys, key)
	}
	got := runProbe(t, g, luauJsonSitesProbe(g, keys), inputs)
	byRow := map[string]string{}
	for line := range strings.SplitSeq(got, "\n") {
		key, rest, ok := strings.Cut(line, "|")
		if !ok {
			t.Fatalf("probe line %q has no verdict", line)
		}
		byRow[key] = rest
	}
	if byRow["write-bits54-past"] != "refused" {
		t.Errorf("ToJson accepted bits(54) = 2^54: %s", byRow["write-bits54-past"])
	}
	wantTextHex := hexBytes(append([]byte(luauJsonSitesCanonical), '\n'))
	var canonicalWire string
	for _, key := range []string{"canonical"} {
		parts := strings.Split(byRow[key], "|")
		if len(parts) != 3 || parts[0] != "ok" {
			t.Errorf("%s: expected ok read, got %q", key, byRow[key])
			continue
		}
		if key == "canonical" {
			canonicalWire = parts[1]
		} else if canonicalWire != "" && parts[1] != canonicalWire {
			t.Errorf("numbers wire %s != canonical wire %s", parts[1], canonicalWire)
		}
		if parts[2] != wantTextHex {
			t.Errorf("%s: ToJson mismatch:\n got: %s\nwant: %s", key, parts[2], wantTextHex)
		}
	}
	if byRow["numbers"] != "refused|kind_mismatch" {
		t.Errorf("numbers: got %s, want kind_mismatch", byRow["numbers"])
	}
	for key := range luauJsonSitesNumberTokens {
		if byRow[key] != "refused|kind_mismatch" {
			t.Errorf("%s: got %s, want kind_mismatch", key, byRow[key])
		}
	}
	for key := range luauJsonSitesRefused {
		if !strings.HasPrefix(byRow[key], "refused|") {
			t.Errorf("%s: read accepted refused row: %s", key, byRow[key])
		}
	}
}
