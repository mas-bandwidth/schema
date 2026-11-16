package luatable

// TestLuaJsonWideIntegersAreStrings holds docs/SPEC-TABLES.md §16.2 and §16.14's
// two JSON integer rules (docs/rulings.json json-wide-integers-are-strings and
// json-integer-token-only) over the committed vectors under
// testdata/wire/json-integers/: a wide integer is written as a JSON STRING of
// decimal digits and reads as a string only; every integer field takes
// an integer token only — digits with an optional minus, no fraction, no
// exponent, no negative zero — and the value never passes through a double.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// luaJsonIntegersSchema is the unit the json-integers vectors read.
const luaJsonIntegersSchema = `package jsonintegers

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

type luaIntegerVector struct {
	name    string
	json    string
	verdict string
}

func luaIntegerVectors(t *testing.T, dir, manifest string) []luaIntegerVector {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, manifest))
	if err != nil {
		t.Fatal(err)
	}
	var rows []luaIntegerVector
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			t.Fatalf("%s/%s:%d: want five columns, got %d", dir, manifest, i+1, len(fields))
		}
		if fields[0] == "" || fields[2] == "" || (fields[3] != "read" && fields[3] != "refused") {
			t.Fatalf("%s/%s:%d: bad vector row %q", dir, manifest, i+1, line)
		}
		rows = append(rows, luaIntegerVector{name: fields[0], json: fields[2], verdict: fields[3]})
	}
	if len(rows) == 0 {
		t.Fatal("no vector rows; the gate would pass over an empty set")
	}
	return rows
}

func luaIntegerProbe(module string, rows []luaIntegerVector) string {
	var b strings.Builder
	b.WriteString(`local T = require("` + module + `")
local J = require("json")
local inputs = require("inputs")
local rows = {
`)
	for i := range rows {
		fmt.Fprintf(&b, "  %q,\n", fmt.Sprintf("row%d", i))
	}
	b.WriteString(`}
local function hex(s)
  if s == nil then return "nil" end
  return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end
for i, key in ipairs(rows) do
  local report = { damaged = false, refused = 0, unknown = 0, kindChanged = 0, outOfRange = 0, widened = 0, repeatedKey = 0 }
  local v = J.FromJson(T, "Integers", inputs[key], report)
  if v == nil then
    print(key .. "|refused|" .. tostring(report.Reason))
  else
    local wlo, whi = v.wide128.lo, v.wide128.hi
    local keys = {}
    for k in pairs(v.counts) do keys[#keys + 1] = k end
    table.sort(keys)
    local counts = {}
    for _, k in ipairs(keys) do
      counts[#counts + 1] = string.format("%d:%d", k, v.counts[k])
    end
    local text = J.ToJson(T, "Integers", v)
    print(string.format("%s|ok|%d|%d|%016x|%016x:%016x|%d|%d|%s|%s",
      key, v.small, v.wide,
      v.uwide & 0xffffffffffffffff,
      wlo, whi, v.ranged, v.frac,
      table.concat(counts, ","), hex(text)))
  end
end
`)
	return b.String()
}

// TestLuaJsonWideIntegersAreStrings reads every accepted string row exactly,
// refuses every numeric-token and other refusal row,
// and writes each accepted value back as the committed canonical text byte for
// byte (plus the form's own trailing line feed).
func TestLuaJsonWideIntegersAreStrings(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Lua interpreter")
	dir := filepath.Join("..", "..", "..", "testdata", "wire", "json-integers")
	rows := luaIntegerVectors(t, dir, "MANIFEST.tsv")
	ordinaryRows := len(rows)
	numberRows := luaIntegerVectors(t, dir, "NUMBER-TOKEN.tsv")
	if len(numberRows) != 13 {
		t.Fatalf("NUMBER-TOKEN.tsv: got %d rows, want 13", len(numberRows))
	}
	for _, row := range numberRows {
		if row.verdict != "refused" {
			t.Fatalf("%s: NUMBER-TOKEN.tsv verdict %q, want refused", row.name, row.verdict)
		}
	}
	rows = append(rows, numberRows...)
	canonical, err := os.ReadFile(filepath.Join(dir, "canonical.read.json"))
	if err != nil {
		t.Fatal(err)
	}

	work := t.TempDir()
	schema := filepath.Join(work, "Integers.schema")
	if err := os.WriteFile(schema, []byte(luaJsonIntegersSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, schema)
	files, err := generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	out := t.TempDir()
	luarun.WriteFiles(t, out, files)
	if err := os.WriteFile(filepath.Join(out, "main.lua"), []byte(luaIntegerProbe(ModuleName(u), rows)), 0o644); err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{}
	for i, row := range rows {
		body, err := os.ReadFile(filepath.Join(dir, row.json))
		if err != nil {
			t.Fatalf("%s: %v", row.name, err)
		}
		inputs[fmt.Sprintf("row%d", i)] = body
	}
	got := string(luarun.Run(t, out, "main.lua", inputs))

	wantValue := "ok|7|-42|ffffffffffffffff|ffffffffffffffff:7fffffffffffffff|9007199254740993|32768|0:1,9007199254740993:2|" + hexBytes(append(append([]byte{}, canonical...), '\n'))
	byRow := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(got, "\n"), "\n") {
		key, rest, ok := strings.Cut(line, "|")
		if !ok {
			t.Fatalf("probe line %q has no verdict", line)
		}
		byRow[key] = rest
	}
	for i, row := range rows {
		key := fmt.Sprintf("row%d", i)
		line, ok := byRow[key]
		if !ok {
			t.Errorf("%s: the probe printed no line", row.name)
			continue
		}
		if row.verdict == "refused" {
			if !strings.HasPrefix(line, "refused|") {
				t.Errorf("%s: read accepted the row: %s", row.name, line)
			}
			if i >= ordinaryRows && line != "refused|kind_mismatch" {
				t.Errorf("%s: got %s, want kind_mismatch", row.name, line)
			}
			continue
		}
		if line != wantValue {
			t.Errorf("%s: read or wrote the row differently\n got: %s\nwant: %s", row.name, line, wantValue)
		}
	}

	t.Run("integer sites", func(t *testing.T) {
		t.Parallel()
		luaJsonIntegerSites(t)
	})
}

// luaJsonSitesSchema holds every integer site the two rules reach that the
// shared vectors' unit does not name: an array element (wide and bit-typed),
// an enum-keyed slot, a wide map value, an optional and a union arm.
const luaJsonSitesSchema = `package jsonsites

enum Slot { A, B }

flags Marks { X, Y }

union Pick
{
    wide   int64
    narrow int32
}

table Sites
{
    wide    int64
    uwide   uint64
    bits64  bits(64)
    bits33  bits(33)
    u128    uint128
    fixed64 fixed(48, 16) | min = -10, max = 10
    fixed128 fixed(64, 64) | min = -10, max = 10
    counts  map[uint64]int64
    list    [3]int64
    ulist   [3]uint64
    keyed   [Slot]int64
    opt     ?int64
    marks   Marks
    pick    Pick
}
`

// luaJsonSitesCanonical is the committed canonical text for the sites unit: a
// wide integer at every site is a string of decimal digits, a narrow one a
// number.
const luaJsonSitesCanonical = `{
  "wide": "-9007199254740993",
  "uwide": "18446744073709551615",
  "bits64": "18446744073709551615",
  "bits33": "8589934591",
  "u128": "340282366920938463463374607431768211455",
  "fixed64": "-1.5",
  "fixed128": "0.5",
  "counts": {
    "1": "-9007199254740993",
    "9007199254740993": "9007199254740993"
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
  "keyed": {
    "A": "-42",
    "B": "42"
  },
  "opt": "-9007199254740993",
  "marks": [
    "X"
  ],
  "pick": {
    "wide": "-9007199254740993"
  }
}`

// luaJsonSitesNumbers is the same value with every wide integer a bare number
// token: it must be refused as a kind mismatch.
const luaJsonSitesNumbers = `{
  "wide": -9007199254740993,
  "uwide": 18446744073709551615,
  "bits64": 18446744073709551615,
  "bits33": 8589934591,
  "u128": 340282366920938463463374607431768211455,
  "fixed64": -1.5,
  "fixed128": 0.5,
  "counts": {
    "1": -9007199254740993,
    "9007199254740993": 9007199254740993
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
  "keyed": {
    "A": -42,
    "B": 42
  },
  "opt": -9007199254740993,
  "marks": [
    "X"
  ],
  "pick": {
    "wide": -9007199254740993
  }
}`

var luaJsonSitesNumberTokens = map[string]string{
	"plain-number":    `{"wide": 42}`,
	"array-number":    `{"list": [42, "0", "0"]}`,
	"optional-number": `{"opt": 42}`,
	"union-number":    `{"pick": {"wide": 42}}`,
	"map-number":      `{"counts": {"1": 42}}`,
	"keyed-number":    `{"keyed": {"A": 42}}`,
	"bits-number":     `{"bits64": 42}`,
	"bits33-number":   `{"bits33": 42}`,
	"uint128-number":  `{"u128": 42}`,
	"fixed-number":    `{"fixed64": 1.5}`,
	"fixed128-number": `{"fixed128": 0.5}`,
}

var luaJsonSitesRefused = map[string]string{
	"list-fraction":        `{"list": [-1.5, "0", "9007199254740993"]}`,
	"list-exponent":        `{"list": [1e3, "0", "9007199254740993"]}`,
	"list-neg-zero":        `{"list": [-0, "0", "9007199254740993"]}`,
	"list-string-fraction": `{"list": ["-1.5", "0", "9007199254740993"]}`,
	"ulist-one-past":       `{"ulist": ["18446744073709551616", "0", "0"]}`,
	"keyed-fraction":       `{"keyed": {"A": -1.5}}`,
	"map-value-fraction":   `{"counts": {"1": -1.5}}`,
	"optional-fraction":    `{"opt": -1.5}`,
	"arm-fraction":         `{"pick": {"wide": -1.5}}`,
	"flags-string":         `{"marks": "X"}`,
}

func luaJsonSitesProbe(module string, keys []string) string {
	var b strings.Builder
	b.WriteString(`local T = require("` + module + `")
local J = require("json")
local inputs = require("inputs")
local keys = {
`)
	for _, key := range keys {
		fmt.Fprintf(&b, "  %q,\n", key)
	}
	b.WriteString(`}
local function hex(s)
  if s == nil then return "nil" end
  return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end
for _, key in ipairs(keys) do
  local report = { damaged = false, refused = 0, unknown = 0, kindChanged = 0, outOfRange = 0, widened = 0, repeatedKey = 0 }
  local v = J.FromJson(T, "Sites", inputs[key], report)
  if v == nil then
    print(key .. "|refused|" .. tostring(report.Reason))
  else
    print(key .. "|ok|" .. hex(J.ToJson(T, "Sites", v)))
  end
end
`)
	return b.String()
}

// luaJsonIntegerSites holds the two rules at every integer site: a wide field,
// an array element, an enum-keyed slot, a map value, an optional and a union
// arm each take a string and reject a number token; a narrow int32 still takes
// a number, and fraction, exponent, negative zero or an out-of-range value is refused.
func luaJsonIntegerSites(t *testing.T) {
	t.Helper()
	work := t.TempDir()
	schema := filepath.Join(work, "Sites.schema")
	if err := os.WriteFile(schema, []byte(luaJsonSitesSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, schema)
	files, err := generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	out := t.TempDir()
	luarun.WriteFiles(t, out, files)

	inputs := map[string][]byte{
		"canonical": []byte(luaJsonSitesCanonical),
		"numbers":   []byte(luaJsonSitesNumbers),
	}
	keys := []string{"canonical", "numbers", "narrow"}
	inputs["narrow"] = []byte(`{"pick": {"narrow": 42}}`)
	for key, body := range luaJsonSitesNumberTokens {
		inputs[key] = []byte(body)
		keys = append(keys, key)
	}
	for key, body := range luaJsonSitesRefused {
		inputs[key] = []byte(body)
		keys = append(keys, key)
	}
	if err := os.WriteFile(filepath.Join(out, "main.lua"), []byte(luaJsonSitesProbe(ModuleName(u), keys)), 0o644); err != nil {
		t.Fatal(err)
	}
	got := string(luarun.Run(t, out, "main.lua", inputs))
	byRow := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(got, "\n"), "\n") {
		key, rest, ok := strings.Cut(line, "|")
		if !ok {
			t.Fatalf("probe line %q has no verdict", line)
		}
		byRow[key] = rest
	}
	want := "ok|" + hexBytes(append(append([]byte{}, luaJsonSitesCanonical...), '\n'))
	for _, key := range []string{"canonical"} {
		if byRow[key] != want {
			t.Errorf("%s: read or wrote the sites unit differently\n got: %s\nwant: %s", key, byRow[key], want)
		}
	}
	if !strings.HasPrefix(byRow["narrow"], "ok|") || !strings.Contains(byRow["narrow"], hexBytes([]byte(`"narrow": 42`))) {
		t.Errorf("narrow: got %s, want numeric int32 accepted", byRow["narrow"])
	}
	for _, key := range append([]string{"numbers"}, mapKeys(luaJsonSitesNumberTokens)...) {
		if byRow[key] != "refused|kind_mismatch" {
			t.Errorf("%s: got %s, want kind_mismatch", key, byRow[key])
		}
	}
	for key := range luaJsonSitesRefused {
		if !strings.HasPrefix(byRow[key], "refused|") {
			t.Errorf("%s: read accepted the row: %s", key, byRow[key])
		}
	}
}

func mapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
