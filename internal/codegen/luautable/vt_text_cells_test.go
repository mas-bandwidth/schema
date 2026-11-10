package luautable_test

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// vtTextSchema declares both a fixed-class table root (FixedDoc, for
// port/luau/vt/text-form) and a variable-class table root (VarDoc, for
// port/luau/vt/text-form-variable) in one probe unit.
const vtTextSchema = `package probe

enum Color { Red, Green, Blue }
flags Features { Fast, Safe }

type Pair {
	a int32
	b string(12)
}

union Choice {
	amount int32
	ping
}

fixed table FixedDoc {
	schema   uint8 | min = 0, max = 3
	count    int32 | min = -10, max = 10
	enabled  bool
	title    string(16)
	wide     string(4)
	raw      bytes(4)
	low      int128 | min = -1000, max = 1000
	high     uint128
	fraction fixed(16,16) | min = -32768, max = 32767
	real32   float32
	real64   float64
	time     timestamp
	color    Color
	feat     Features
	fixed2   [2]int32
	nums     [..3]int32
	keyed    [Color]int32
	opt      ?int32
	pair     Pair
	choice   Choice
}

table GNode {
	id    int32
	label string(16)
}

table Child {
	tag int32
}

union VarChoice {
	child *Child
	label string(16)
	ping
}

table VarDoc {
	on     bool
	need   int32 | required, min = -100, max = 100
	opt    ?int32
	note   *string
	note2  *string
	data   *bytes
	child  *Child
	graph  []*GNode
	tags   map[string(8)]int32
	nums   []int32
	color  Color
	feat     Features
	wide   string(4)
	real32 float32
	choice VarChoice
}
`

type vtTextCleanCase struct {
	name  string
	root  string
	input string
}

var vtTextCleanCases = []vtTextCleanCase{
	{
		name: "fixed_clean",
		root: "FixedDoc",
		input: `{
  "schema": 2,
  "count": 7,
  "enabled": true,
  "title": "hello",
  "wide": "ab",
  "raw": "YWJjZA==",
  "low": -42,
  "high": 42,
  "fraction": 1.5,
  "real32": 0.10000000149011612,
  "real64": 2.5,
  "time": "2026-01-02T03:04:05Z",
  "color": "Green",
  "feat": ["Fast", "Safe"],
  "fixed2": [10, 20],
  "nums": [1, 2],
  "keyed": {"Red": 3, "Green": 4, "Blue": 5},
  "opt": 9,
  "pair": {"a": 11, "b": "ok"},
  "choice": {"amount": 12}
}`,
	},
	{
		name:  "fixed_comments_and_unknown",
		root:  "FixedDoc",
		input: "\xEF\xBB\xBF// leading line comment\n/* block comment */ {\n  \"schema\": 1,\n  \"unknown_extra\": [1, 2, 3],\n  \"count\": -3,\n  \"enabled\": false,\n  \"title\": \"\",\n  \"wide\": \"\",\n  \"raw\": \"\",\n  \"low\": 0,\n  \"high\": 0,\n  \"fraction\": 0,\n  \"real32\": 1.25,\n  \"real64\": -0.5,\n  \"time\": \"1970-01-01T00:00:00Z\",\n  \"color\": \"Red\",\n  \"feat\": [],\n  \"fixed2\": [0, 0],\n  \"nums\": [],\n  \"keyed\": {\"Red\": 0, \"Green\": 0, \"Blue\": 0},\n  \"pair\": {\"a\": 0, \"b\": \"\"},\n  \"choice\": {\"ping\": null}\n}\n",
	},
	{
		name: "var_clean_shared_graph",
		root: "VarDoc",
		input: `{
  "on": true,
  "need": 42,
  "opt": 7,
  "note": "same",
  "note2": "same",
  "data": "YWJj",
  "child": {"tag": 9},
  "graph": [{"&node": 1, "id": 10, "label": "shared"}, {"&node": 1}],
  "tags": {"alpha": 1, "beta": 2},
  "nums": [7, -2, 0, 4],
  "color": "Blue",
  "feat": ["Fast"],
  "wide": "\ud83d\ude00",
  "real32": 0.10000000149011612,
  "choice": {"child": {"tag": 5}}
}`,
	},
	{
		name: "var_minimal_with_unknown",
		root: "VarDoc",
		input: `// comment in variable-class text
{
  "on": false,
  "need": 0,
  "ignored_field": {"nested": true},
  "tags": {},
  "nums": [],
  "color": "Red",
  "feat": [],
  "wide": "x",
  "real32": 0.5,
  "choice": {"ping": null}
}`,
	},
}

type vtTextHostileCase struct {
	name  string
	root  string
	input string
}

var vtTextHostileCases = []vtTextHostileCase{
	{name: "fixed_dup_key", root: "FixedDoc", input: `{"count": 1, "count": 2}`},
	{name: "fixed_clamped_int", root: "FixedDoc", input: `{"count": 99}`},
	{name: "fixed_clamped_string", root: "FixedDoc", input: `{"title": "0123456789abcdefX"}`},
	{name: "fixed_clamped_array", root: "FixedDoc", input: `{"nums": [1, 2, 3, 4]}`},
	{name: "fixed_kind_mismatch", root: "FixedDoc", input: `{"count": "not-an-int"}`},
	{name: "fixed_nul_string", root: "FixedDoc", input: "{\"title\": \"a\\u0000b\"}"},
	{name: "fixed_unpaired_surrogate", root: "FixedDoc", input: "{\"wide\": \"\\ud800\"}"},
	{name: "fixed_malformed_syntax", root: "FixedDoc", input: `{"count":`},
	{name: "var_missing_required", root: "VarDoc", input: `{"on": false}`},
	{name: "var_blob_not_base64", root: "VarDoc", input: `{"need": 1, "data": "not*base64!"}`},
	{name: "var_dup_map_key", root: "VarDoc", input: `{"need": 1, "tags": {"k": 1, "k": 2}}`},
	{name: "var_dup_obj_key", root: "VarDoc", input: `{"need": 1, "need": 2}`},
	{name: "var_clamped_int", root: "VarDoc", input: `{"need": 500}`},
	{name: "var_kind_mismatch", root: "VarDoc", input: `{"need": [1, 2]}`},
	{name: "var_unresolved_node", root: "VarDoc", input: `{"need": 1, "child": {"&node": 99}}`},
	{name: "var_null_array_elem", root: "VarDoc", input: `{"need": 1, "nums": [1, null]}`},
}

func ensureLuauInEnv() {
	if _, why := luaurun.Interpreter(); why == "" {
		return
	}
	if lp, err := exec.LookPath("luau"); err == nil {
		_ = os.Setenv("LUAU", lp)
		return
	}
	for _, cand := range []string{
		"/home/nova/sdk/luau-" + luaurun.Version + "/luau",
		"/home/nova/sdk/bin/luau",
	} {
		if _, err := os.Stat(cand); err == nil {
			_ = os.Setenv("LUAU", cand)
			return
		}
	}
}

func vtTextLoadUnit(t *testing.T) (*ir.Unit, *tabletext.Model) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(path, []byte(vtTextSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("vtTextSchema does not load: %v", err)
	}
	return u, tabletext.NewModel(u)
}

func vtTextOracleClean(t *testing.T, m *tabletext.Model, rootName, text string) (wire, canonicalJSON []byte) {
	t.Helper()
	d1 := t.TempDir()
	if err := os.WriteFile(filepath.Join(d1, rootName+".json"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	var err error
	var rep tabletext.Report
	wire, _, rep, err = tablepack.Pack(m, rootName, d1)
	if err != nil {
		t.Fatalf("%s: tablepack.Pack failed: %v (rep=%+v)", rootName, err, rep)
	}
	if rep.Refused {
		t.Fatalf("%s: tablepack.Pack refused clean case: %s (rep=%+v)", rootName, rep.Reason, rep)
	}
	d2 := t.TempDir()
	rep2, err := tablepack.UnpackOneFile(m, rootName, wire, d2)
	if err != nil || rep2 != (tabletext.Report{}) {
		t.Fatalf("%s: tablepack.UnpackOneFile failed: err=%v rep=%+v", rootName, err, rep2)
	}
	canonicalJSON, err = os.ReadFile(filepath.Join(d2, rootName+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return wire, canonicalJSON
}

func vtTextOracleHostileRefused(t *testing.T, m *tabletext.Model, rootName, text string) {
	t.Helper()
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, rootName+".json"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, rep, err := tablepack.Pack(m, rootName, d)
	if err == nil && !rep.Refused {
		t.Fatalf("hostile case on %s was accepted by the Go engine: %s", rootName, text)
	}
}

func vtTextCheckSuite(t *testing.T, targetFile, from, to string) error {
	t.Helper()
	ensureLuauInEnv()
	u, m := vtTextLoadUnit(t)
	g := generateFiles(t, map[string]string{"Probe.schema": vtTextSchema})
	g.flags = flagsMeta(u)
	if g.err != nil {
		t.Fatalf("generateFiles failed: %v", g.err)
	}
	if targetFile != "" {
		p := filepath.Join(g.dir, targetFile)
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(src), from) != 1 {
			t.Fatalf("the sabotage anchor %q does not occur exactly once in %s", from, targetFile)
		}
		if err := os.WriteFile(p, []byte(strings.Replace(string(src), from, to, 1)), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	inputs := map[string][]byte{}
	wantWire := map[string][]byte{}
	wantJSON := map[string][]byte{}
	for _, c := range vtTextCleanCases {
		w, j := vtTextOracleClean(t, m, c.root, c.input)
		inputs[c.name+"_in"] = []byte(c.input)
		inputs[c.name+"_wire"] = w
		wantWire[c.name] = w
		wantJSON[c.name] = j
	}
	for _, h := range vtTextHostileCases {
		vtTextOracleHostileRefused(t, m, h.root, h.input)
		inputs[h.name] = []byte(h.input)
	}

	var b strings.Builder
	b.WriteString(jsonModules(g))
	b.WriteString(jsonFlags(g))
	b.WriteString(`local json = require("./schema_json")
json.SetFlags(SCHEMA_JSON_FLAGS)
local load = require("./schema_load")
local save = require("./schema_save")
local St = require("./schema_tables")
local inputs = require("./inputs")
local function hexstr(buf: buffer): string
	local parts = table.create(buffer.len(buf))
	for i = 0, buffer.len(buf) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(buf, i))
	end
	return table.concat(parts)
end
local function runClean(name: string, root: string)
	local r1: any = {}
	local v1 = json.FromJson(root, inputs[name .. "_in"], r1)
	if v1 == nil or r1.malformed or r1.reason ~= nil then
		print("CLEAN\t" .. name .. "\tREFUSED_READ\t" .. tostring(r1.reason))
		return
	end
	local saved = save.save(St.lookup(root), v1)
	if saved == nil then
		print("CLEAN\t" .. name .. "\tREFUSED_SAVE\tnil")
		return
	end
	local r2: any = {}
	local v2 = load.ReadStrict(root, inputs[name .. "_wire"], r2)
	if v2 == nil or r2.malformed or r2.reason ~= nil then
		print("CLEAN\t" .. name .. "\tREFUSED_WIRE\t" .. tostring(r2.reason))
		return
	end
	local r3: any = {}
	local text = json.ToJson(root, v2, r3)
	if text == nil then
		print("CLEAN\t" .. name .. "\tREFUSED_TOJSON\t" .. tostring(r3.reason))
		return
	end
	print("CLEAN\t" .. name .. "\t" .. hexstr(saved) .. "\t" .. hexstr(text))
end
local function runHostile(name: string, root: string)
	local r: any = {}
	local v = json.FromJson(root, inputs[name], r)
	print("HOSTILE\t" .. name .. "\t" .. (if v == nil then "refused" else "read") .. "\t" .. tostring(r.reason))
end
`)
	for _, c := range vtTextCleanCases {
		fmt.Fprintf(&b, "runClean(%q, %q)\n", c.name, c.root)
	}
	for _, h := range vtTextHostileCases {
		fmt.Fprintf(&b, "runHostile(%q, %q)\n", h.name, h.root)
	}

	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", inputs)))

	var errs []string
	seenClean := 0
	seenHostile := 0
	for line := range strings.SplitSeq(out, "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) != 4 {
			errs = append(errs, fmt.Sprintf("malformed output line %q", line))
			continue
		}
		switch parts[0] {
		case "CLEAN":
			seenClean++
			name := parts[1]
			gotWire, err1 := hex.DecodeString(parts[2])
			gotJSON, err2 := hex.DecodeString(parts[3])
			if err1 != nil || err2 != nil {
				errs = append(errs, fmt.Sprintf("clean %s did not read/write: %s %s", name, parts[2], parts[3]))
				continue
			}
			if !bytes.Equal(gotWire, wantWire[name]) {
				errs = append(errs, fmt.Sprintf("clean %s: FromJson then save != engine wire:\ngot  %x\nwant %x", name, gotWire, wantWire[name]))
			}
			if !bytes.Equal(gotJSON, wantJSON[name]) {
				errs = append(errs, fmt.Sprintf("clean %s: ReadStrict then ToJson != engine JSON:\ngot  %s\nwant %s", name, gotJSON, wantJSON[name]))
			}
		case "HOSTILE":
			seenHostile++
			name, verdict, reason := parts[1], parts[2], parts[3]
			if verdict != "refused" || reason == "nil" || reason == "" {
				errs = append(errs, fmt.Sprintf("hostile %s: got verdict=%s reason=%s, want refused with reason", name, verdict, reason))
			}
		default:
			errs = append(errs, fmt.Sprintf("unexpected line %q", line))
		}
	}
	if seenClean != len(vtTextCleanCases) {
		errs = append(errs, fmt.Sprintf("saw %d clean cases, want %d", seenClean, len(vtTextCleanCases)))
	}
	if seenHostile != len(vtTextHostileCases) {
		errs = append(errs, fmt.Sprintf("saw %d hostile cases, want %d", seenHostile, len(vtTextHostileCases)))
	}

	// Also hold the wire-side strict read (schema_load.luau ReadStrict) that
	// feeds json-write on a forged repeated map key (decision 5).
	loadFrom, loadTo := "", ""
	if targetFile == "schema_load.luau" {
		loadFrom, loadTo = from, to
	}
	if got := repeatedKeyRun(t, loadFrom, loadTo); got != repeatedKeyWant {
		errs = append(errs, fmt.Sprintf("repeated map key wire gate:\n%s\nwant\n%s", got, repeatedKeyWant))
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "\n"))
	}
	return nil
}

// TestLuauVtTextCells pins the Luau table runtime's strict text read
// (schema_json.luau FromJson), strict text writer (ToJson), and strict wire
// read (schema_load.luau ReadStrict) against schema-go's own engine for both
// text-form (fixed-class and variable-class JSON in and out of a table) and
// text-form-variable (pointers, shared &node graphs, blobs, equal strings
// saving by value, maps, unbounded arrays, 64-bit id-table wire enums,
// wstring, required fields, and conditional fields).
func TestLuauVtTextCells(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	if err := vtTextCheckSuite(t, "", "", ""); err != nil {
		t.Fatalf("the strict text cells failed:\n%v", err)
	}
}

// TestLuauVtTextCellsNegativeControl plants three faults in the real emitted
// runtime artifacts (schema_json.luau and schema_load.luau) and verifies each
// turns TestLuauVtTextCells red:
//  1. disabling the strict refusal loop in J.FromJson lets hostile inputs pass;
//  2. capping float32 formatting in J.ToJson at six significant digits breaks
//     json-write byte equality against the Go engine;
//  3. dropping duplicate from schema_load.luau's REFUSING set lets a forged
//     repeated-key wire pass ReadStrict.
func TestLuauVtTextCellsNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	controls := []struct {
		name, file, from, to string
	}{
		{
			name: "fromjson-strict-disabled",
			file: "schema_json.luau",
			from: "  for _, k in STRICT do\n",
			to:   "  for _, k in ({} :: { { string } }) do\n",
		},
		{
			name: "tojson-float32-precision",
			file: "schema_json.luau",
			from: "local text = format_g(v, digits)",
			to:   `local text = single and string.format("%#.6g", v) or format_g(v, digits)`,
		},
		{
			name: "load-strict-duplicate-disabled",
			file: "schema_load.luau",
			from: `"blobShared", "clamped", "kindMismatch", "duplicate" }`,
			to:   `"blobShared", "clamped", "kindMismatch" }`,
		},
	}
	for _, tc := range controls {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := vtTextCheckSuite(t, tc.file, tc.from, tc.to); err == nil {
				t.Fatalf("%s: the planted defect in %s left the suite green", tc.name, tc.file)
			}
		})
	}
}
