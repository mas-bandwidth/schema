package squirreltable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// toJSONOffContractSchema carries one field per write-contract row the text writer
// owes (docs/SPEC.md §5.1 rows 1 to 4 and 10): a ranged integer signed,
// unsigned past 2^63 and 128 bits wide, a ranged array element, a bits(N), a
// counted numeric array and a text-bearing array with a minimum, a fixed array,
// a string(N), a bytes(N), a text list's count and element, and a compressed float.
// A direct text list cannot declare a positive minimum (§2.13), so tm uses a
// counted array of records whose JSON values contain bounded text fields.
// A map key past its string(N), a float field holding a value that is not a
// number and a bool field holding a value that is not a bool are refused too.
const toJSONOffContractSchema = `package probe
type TextEntry
{
    value string(4)
}
table T
{
    r int32 | min = -5, max = 100
    u uint64 = 10 | min = 10, max = 9223372036854775808
    q int128 | min = -5, max = 100
    m bits(5)
    c [..3]int32
    k [2..4]int32
    e [3]int32 | min = 0, max = 9
    tm [2..4]TextEntry
    s string(4)
    b bytes(3)
    ts [..2]string(4)
    f float32 | min = -1.0, max = 1.0, resolution = 0.5
    mp map[string(4)]int32
    fl float32
    bo bool
}
`

// offContractCase is one value of one field, shared by the text and variable
// probes. The text form names the value in Squirrel storage (an expression)
// and as the JSON value an unchecked writer would spell it; lawful says the
// declaration admits it: the twin at the boundary. The variable form carries
// the same value in the Go engine's model and in Squirrel: goBad and goGood
// are the model's bad and good twin, nil where the engine has no way to hold
// it (a wrong Squirrel type) or where its writer does not refuse it today (a
// zero byte in a *string), and goGood is nil for the enum-keyed array row,
// whose twin is held on the Squirrel side. nutBad and nutGood are the
// Squirrel statements that set each twin.
type offContractCase struct {
	name, field, squirrel, text string
	lawful                      bool
	goBad                       func(*tabletext.Model, *tabletext.Instance)
	goGood                      func(*tabletext.Model, *tabletext.Instance)
	nutBad                      string
	nutGood                     string
}

var offContractCases = []offContractCase{
	{name: "range_high", field: "r", squirrel: "101", text: "101", lawful: false},
	{name: "range_low", field: "r", squirrel: "-6", text: "-6", lawful: false},
	{name: "range_high_edge", field: "r", squirrel: "100", text: "100", lawful: true},
	{name: "range_low_edge", field: "r", squirrel: "-5", text: "-5", lawful: true},
	{name: "urange_high", field: "u", squirrel: "0x8000000000000001", text: "9223372036854775809", lawful: false},
	{name: "urange_low", field: "u", squirrel: "9", text: "9", lawful: false},
	{name: "urange_high_edge", field: "u", squirrel: "0x8000000000000000", text: "9223372036854775808", lawful: true},
	{name: "urange_low_edge", field: "u", squirrel: "10", text: "10", lawful: true},
	{name: "wide_high", field: "q", squirrel: "[101, 0]", text: "101", lawful: false},
	{name: "wide_low", field: "q", squirrel: "[-6, -1]", text: "-6", lawful: false},
	{name: "wide_high_edge", field: "q", squirrel: "[100, 0]", text: "100", lawful: true},
	{name: "wide_low_edge", field: "q", squirrel: "[-5, -1]", text: "-5", lawful: true},
	{name: "bits_wide", field: "m", squirrel: "32", text: "32", lawful: false},
	{name: "bits_edge", field: "m", squirrel: "31", text: "31", lawful: true},
	{name: "count_past", field: "c", squirrel: "[1, 2, 3, 4]", text: "[1,2,3,4]", lawful: false},
	{name: "count_edge", field: "c", squirrel: "[1, 2, 3]", text: "[1,2,3]", lawful: true},
	{name: "count_past_max", field: "k", squirrel: "[1, 2, 3, 4, 5]", text: "[1,2,3,4,5]", lawful: false},
	{name: "count_below_min", field: "k", squirrel: "[1]", text: "[1]", lawful: false},
	{name: "count_min_edge", field: "k", squirrel: "[1, 2]", text: "[1,2]", lawful: true},
	{name: "text_array_below_min", field: "tm", squirrel: `[textEntry("a")]`, text: `[{"value":"a"}]`, lawful: false},
	{name: "text_array_min_edge", field: "tm", squirrel: `[textEntry("a"), textEntry("b")]`, text: `[{"value":"a"},{"value":"b"}]`, lawful: true},
	{name: "count_max_edge", field: "k", squirrel: "[1, 2, 3, 4]", text: "[1,2,3,4]", lawful: true},
	{name: "elem_high", field: "e", squirrel: "[0, 10, 0]", text: "[0,10,0]", lawful: false},
	{name: "elem_low", field: "e", squirrel: "[0, -1, 0]", text: "[0,-1,0]", lawful: false},
	{name: "elem_past_bound", field: "e", squirrel: "[0, 0, 0, 0]", text: "[0,0,0,0]", lawful: false},
	{name: "elem_edge", field: "e", squirrel: "[0, 9, 0]", text: "[0,9,0]", lawful: true},
	{name: "string_past", field: "s", squirrel: `"abcde"`, text: `"abcde"`, lawful: false},
	{name: "string_edge", field: "s", squirrel: `"abcd"`, text: `"abcd"`, lawful: true},
	{name: "bytes_past", field: "b", squirrel: `"\x01\x02\x03\x04"`, text: `"AQIDBA=="`, lawful: false},
	{name: "bytes_edge", field: "b", squirrel: `"\x01\x02\x03"`, text: `"AQID"`, lawful: true},
	{name: "textlist_count_past", field: "ts", squirrel: `textList(["a", "b", "c"])`, text: `["a","b","c"]`, lawful: false},
	{name: "textlist_elem_past", field: "ts", squirrel: `textList(["abcde"])`, text: `["abcde"]`, lawful: false},
	{name: "textlist_edge", field: "ts", squirrel: `textList(["abcd", "b"])`, text: `["abcd","b"]`, lawful: true},
	{name: "float_high", field: "f", squirrel: "1.5", text: "1.5", lawful: false},
	{name: "float_low", field: "f", squirrel: "-1.5", text: "-1.5", lawful: false},
	{name: "float_high_edge", field: "f", squirrel: "1.0", text: "1", lawful: true},
	{name: "float_low_edge", field: "f", squirrel: "-1.0", text: "-1", lawful: true},
	{name: "map_key_past", field: "mp", squirrel: `mapOf("abcde", 1)`, text: `{"abcde":1}`, lawful: false},
	{name: "map_key_edge", field: "mp", squirrel: `mapOf("abcd", 1)`, text: `{"abcd":1}`, lawful: true},
	{name: "float_string", field: "fl", squirrel: `"x"`, text: `"x"`, lawful: false},
	{name: "float_table", field: "fl", squirrel: "{}", text: "{}", lawful: false},
	{name: "float_number", field: "fl", squirrel: "0.5", text: "0.5", lawful: true},
	{name: "bool_string", field: "bo", squirrel: `"x"`, text: `"x"`, lawful: false},
	{name: "bool_integer", field: "bo", squirrel: "1", text: "1", lawful: false},
	{name: "bool_edge", field: "bo", squirrel: "true", text: "true", lawful: true},
}

// offContractPrelude sets each case's field on an otherwise lawful T (k holds
// its minimum count), then ToJson and ToJsonMeasure inside a try: a refusal is
// null and -1 with nothing raised; a write is read back by FromJson and the
// field compared with the value written.
const offContractPrelude = `local schema = dofile("probe/schema_module.nut", true);
local M = schema.require(%q);
local J = schema.require("schema_table_json");
local member = {};
local textTable = null;
foreach (fd in M.Descriptors.T.fields) {
  member[fd.name] <- fd.member;
  if (fd.name == "ts") textTable = fd.elem.table;
}
local textEntryName = null, textEntryMember = null;
foreach (fd in M.Descriptors.T.fields) if (fd.name == "tm") textEntryName = fd.elem.table;
foreach (fd in M.Descriptors[textEntryName].fields) if (fd.name == "value") textEntryMember = fd.member;
local textEntry = function(s) { local rec = M.Descriptors[textEntryName].make(); rec[textEntryMember] = s; return rec; };
local textList = function(a) {
  local out = [];
  foreach (s in a) { local rec = M.Descriptors[textTable].make(); rec.Value = s; out.append(rec); }
  return out;
};
local mapEntryName = null, mapKeyMember = null, mapValueMember = null;
foreach (fd in M.Descriptors.T.fields) if (fd.name == "mp") {
  mapEntryName = fd.entry; mapKeyMember = fd.mapkey.member; mapValueMember = fd.mapvalue.member;
}
local mapOf = function(k, x) {
  local rec = M.Descriptors[mapEntryName].make();
  rec[mapKeyMember] = k; rec[mapValueMember] = x;
  return [rec];
};
local norm;
norm = function(x) {
  local t = typeof x;
  if (t == "array") { local s = "["; foreach (e in x) s += norm(e) + ","; return s + "]"; }
  if (t == "string" || t == "blob") {
    local s = "b:";
    for (local i = 0; i < x.len(); i++) s += format("%%02x", x[i] & 0xff);
    return s;
  }
  if ((t == "instance" || t == "table") && (mapKeyMember in x) && mapKeyMember != "Value")
    return "ent:" + norm(x[mapKeyMember]) + "=" + norm(x[mapValueMember]);
  if (t == "instance" || t == "table") return "rec:" + norm(x.Value);
  if (t == "float") return "f:" + format("%%.17g", x);
  return t + ":" + x;
};
local cases = %s;
foreach (c in cases) {
  local v = M.Descriptors.T.make();
  v[member.k] = [0, 0];
  v[member[c[1]]] = c[2];
  local w = null, m = null;
  try {
    w = J.ToJson(M, "T", v);
    m = J.ToJsonMeasure(M, "T", v);
  } catch (e) {
    print(c[0] + " raised\n");
    continue;
  }
  if (w == null) { print(c[0] + " refused " + m + "\n"); continue; }
  if (m != w.len()) { print(c[0] + " measure " + m + "\n"); continue; }
  local r = { unknown = 0, mismatch = 0, widened = 0, oor = 0, repeated = 0,
    bodyDamaged = false, refused = false };
  local x = J.FromJson(M, "T", w, r);
  if (x == null) { print(c[0] + " unread\n"); continue; }
  local want = norm(c[2]), got = norm(x[member[c[1]]]);
  print(c[0] + " wrote " + (want == got ? "same" : "differs:" + got) + "\n");
}
`

// TestSquirrelToJsonRefusesOffContract pins the text writer to the write
// contract (docs/SPEC.md §5.1, rows 1 to 4 and 10; docs/SPEC-TABLES.md §16):
// Squirrel refuses in every build, so ToJson answers null and ToJsonMeasure
// -1, with nothing raised, for every value the declaration does not admit,
// and every lawful twin at the boundary writes text FromJson reads back to
// the same value. The Go engine's reader first confirms the premise: the text
// an unchecked writer would spell for each off-contract value is refused, and
// each twin's text is read.
func TestSquirrelToJsonRefusesOffContract(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "probe.schema")
	writeFile(t, schemaPath, []byte(toJSONOffContractSchema))
	paths, err := compiler.GatherPaths([]string{schemaPath})
	if err != nil {
		t.Fatal(err)
	}
	unit, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("go_engine_reads", func(t *testing.T) {
		t.Parallel()
		model := tabletext.NewModel(unit)
		st := model.Lookup("T")
		for _, c := range offContractCases {
			prefix := `{"k":[0,0],"tm":[{"value":"a"},{"value":"b"}],`
			switch c.field {
			case "k":
				prefix = `{"tm":[{"value":"a"},{"value":"b"}],`
			case "tm":
				prefix = `{"k":[0,0],`
			}
			text := prefix + `"` + c.field + `":` + c.text + `}`
			inst := model.New(st)
			var rep tabletext.Report
			parsed := model.Read(inst, []byte(text), &rep)
			admitted := parsed && !rep.Refused
			if admitted && model.CheckValue(inst) != nil {
				admitted = false
			}
			if admitted != c.lawful {
				t.Errorf("%s: Go tabletext parse/check on %s admitted=%v, want admitted=%v", c.name, text, admitted, c.lawful)
			}
		}
	})

	files, err := squirrel.Generate(unit)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(unit)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	out := filepath.Join(dir, "probe")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}
	rows := make([]string, len(offContractCases))
	for i, c := range offContractCases {
		rows[i] = "[" + strconv.Quote(c.name) + ", " + strconv.Quote(c.field) + ", " + c.squirrel + "]"
	}
	writeFile(t, filepath.Join(dir, "main.nut"),
		[]byte(fmt.Sprintf(offContractPrelude, squirreltable.ModuleName(unit), "[\n"+strings.Join(rows, ",\n")+"\n]")))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", nil)), "\n"), "\n") {
		name, rest, ok := strings.Cut(line, " ")
		if !ok {
			t.Fatalf("probe line %q is not a row", line)
		}
		got[name] = rest
	}
	for _, c := range offContractCases {
		g, ok := got[c.name]
		switch {
		case !ok:
			t.Errorf("%s: the probe printed no row", c.name)
		case c.lawful && g != "wrote same":
			t.Errorf("%s: ToJson of the lawful value %s answered %q, want it written and read back the same", c.name, c.squirrel, g)
		case !c.lawful && g != "refused -1":
			t.Errorf("%s: ToJson of %s = %s answered %q, want refused with ToJsonMeasure -1", c.name, c.field, c.squirrel, g)
		}
	}
	t.Logf("compared %d values against the write contract", len(offContractCases))
}
