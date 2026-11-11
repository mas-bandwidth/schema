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

// offContractSchema carries one field per write-contract row the text writer
// owes (docs/SPEC.md §5.1 rows 1 to 4 and 10): a ranged integer signed,
// unsigned past 2^63 and 128 bits wide, a ranged array element, a bits(N), a
// counted numeric array and a text-bearing array with a minimum, a fixed array,
// a string(N), a bytes(N), a text list's count and element, and a compressed float.
// A direct text list cannot declare a positive minimum (§2.13), so tm uses a
// counted array of records whose JSON values contain bounded text fields.
const offContractSchema = `package probe
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
}
`

// offContractCase is one value of one field: in Squirrel storage (an
// expression), and as the JSON value an unchecked writer would spell it.
// lawful says the declaration admits it: the twin at the boundary.
type offContractCase struct {
	name, field, squirrel, text string
	lawful                      bool
}

var offContractCases = []offContractCase{
	{"range_high", "r", "101", "101", false},
	{"range_low", "r", "-6", "-6", false},
	{"range_high_edge", "r", "100", "100", true},
	{"range_low_edge", "r", "-5", "-5", true},
	{"urange_high", "u", "0x8000000000000001", "9223372036854775809", false},
	{"urange_low", "u", "9", "9", false},
	{"urange_high_edge", "u", "0x8000000000000000", "9223372036854775808", true},
	{"urange_low_edge", "u", "10", "10", true},
	{"wide_high", "q", "[101, 0]", "101", false},
	{"wide_low", "q", "[-6, -1]", "-6", false},
	{"wide_high_edge", "q", "[100, 0]", "100", true},
	{"wide_low_edge", "q", "[-5, -1]", "-5", true},
	{"bits_wide", "m", "32", "32", false},
	{"bits_edge", "m", "31", "31", true},
	{"count_past", "c", "[1, 2, 3, 4]", "[1,2,3,4]", false},
	{"count_edge", "c", "[1, 2, 3]", "[1,2,3]", true},
	{"count_past_max", "k", "[1, 2, 3, 4, 5]", "[1,2,3,4,5]", false},
	{"count_below_min", "k", "[1]", "[1]", false},
	{"count_min_edge", "k", "[1, 2]", "[1,2]", true},
	{"text_array_below_min", "tm", `[textEntry("a")]`, `[{"value":"a"}]`, false},
	{"text_array_min_edge", "tm", `[textEntry("a"), textEntry("b")]`, `[{"value":"a"},{"value":"b"}]`, true},
	{"count_max_edge", "k", "[1, 2, 3, 4]", "[1,2,3,4]", true},
	{"elem_high", "e", "[0, 10, 0]", "[0,10,0]", false},
	{"elem_low", "e", "[0, -1, 0]", "[0,-1,0]", false},
	{"elem_past_bound", "e", "[0, 0, 0, 0]", "[0,0,0,0]", false},
	{"elem_edge", "e", "[0, 9, 0]", "[0,9,0]", true},
	{"string_past", "s", `"abcde"`, `"abcde"`, false},
	{"string_edge", "s", `"abcd"`, `"abcd"`, true},
	{"bytes_past", "b", `"\x01\x02\x03\x04"`, `"AQIDBA=="`, false},
	{"bytes_edge", "b", `"\x01\x02\x03"`, `"AQID"`, true},
	{"textlist_count_past", "ts", `textList(["a", "b", "c"])`, `["a","b","c"]`, false},
	{"textlist_elem_past", "ts", `textList(["abcde"])`, `["abcde"]`, false},
	{"textlist_edge", "ts", `textList(["abcd", "b"])`, `["abcd","b"]`, true},
	{"float_high", "f", "1.5", "1.5", false},
	{"float_low", "f", "-1.5", "-1.5", false},
	{"float_high_edge", "f", "1.0", "1", true},
	{"float_low_edge", "f", "-1.0", "-1", true},
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
local norm;
norm = function(x) {
  local t = typeof x;
  if (t == "array") { local s = "["; foreach (e in x) s += norm(e) + ","; return s + "]"; }
  if (t == "string" || t == "blob") {
    local s = "b:";
    for (local i = 0; i < x.len(); i++) s += format("%%02x", x[i] & 0xff);
    return s;
  }
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
	writeFile(t, schemaPath, []byte(offContractSchema))
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
			if c.field == "k" {
				prefix = `{"tm":[{"value":"a"},{"value":"b"}],`
			} else if c.field == "tm" {
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
