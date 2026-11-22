package squirreltable_test

// A COUNTED ARRAY [Min..Max]T BELOW ITS MINIMUM IS REFUSED on every read and
// every write, and a fresh value is born at the minimum (docs/SPEC.md §5.1
// row 3, §4.2). An ARM PAYLOAD past its own bound is refused by the variable
// Save as the same field outside a union is (§5.1 row 4). Squirrel refuses in
// every build: Save answers null, Measure -1, a read refuses.
//
// The reads are fed by a writer unit whose declaration is the same but for
// its minimum ([..3] where the reader says [2..3]): the field ids are the
// names', so the writer's lawful one-element array is the reader's
// below-minimum one, and only the reader's low bound can decide the read.

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
)

const countMinSchema = `package probe

union Pack
{
    s string(3)
    b bytes(3)
}

type Item
{
    least [2..3]uint16
}

table Root
{
    least [2..3]uint16
    pack Pack
    items [..2]Item
}

fixed table Msg
{
    least [2..3]uint16
}
`

func TestSquirrelVariableSaveRefusesCountBelowMinAndArmPastBound(t *testing.T) {
	sqrun.Require(t)
	t.Parallel()
	unit := unitFromSchema(t, countMinSchema)

	cases := []offContractCase{
		{name: "least_under", nutBad: "v.Least = [7];", nutGood: "v.Least = [7, 8];"},
		{name: "least_empty", nutBad: "v.Least = [];", nutGood: "v.Least = [0, 0];"},
		{name: "item_least_under", nutBad: "local i = T.Item(); i.Least = [7]; v.Items = [i];", nutGood: "local i = T.Item(); i.Least = [7, 8]; v.Items = [i];"},
		{name: "pack_string_long", nutBad: `v.Pack.Type = 1; v.Pack.S = "abcd";`, nutGood: `v.Pack.Type = 1; v.Pack.S = "abc";`},
		{name: "pack_bytes_long", nutBad: "v.Pack.Type = 2; v.Pack.B = blob(4);", nutGood: "v.Pack.Type = 2; v.Pack.B = blob(3);"},
	}

	// the Go engine's variable writer gives the same verdict on the count
	t.Run("go", func(t *testing.T) {
		t.Parallel()
		m := tabletext.NewModel(unit)
		for _, n := range []int{0, 1} {
			inst := m.New(m.Lookup("Root"))
			f, ok := inst.FieldByKey("least")
			if !ok {
				t.Fatal("Root declares no field least")
			}
			f.Count = n
			if out, err := tablewire.Encode(m, inst); err == nil || out != nil {
				t.Errorf("a count of %d under [2..3]: the Go variable writer answered %x, %v, want a refusal", n, out, err)
			}
		}
	})

	dir := t.TempDir()
	mod := emitUnit(t, dir, "probe", unit)
	var p strings.Builder
	p.WriteString(strings.ReplaceAll(offContractSavePrelude, "__MOD__", mod))
	p.WriteString(`print("born " + T.Root().Least.len() + " " + T.Item().Least.len() + "\n");` + "\n")
	for _, c := range cases {
		fmt.Fprintf(&p, "probe(%q, function(v) { %s });\n", c.name, c.nutBad)
		fmt.Fprintf(&p, "probe(%q, function(v) { %s });\n", c.name+"_twin", c.nutGood)
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	out := string(sqrun.Run(t, dir, "main.nut", nil))
	got := map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) > 0 {
			got[f[0]] = f[1:]
		}
	}
	if b := strings.Join(got["born"], " "); b != "2 2" {
		t.Errorf("a fresh Root and Item hold %q elements of [2..3], want both born at the minimum", b)
	}
	for _, c := range cases {
		row := got[c.name]
		if len(row) != 5 || row[0] != "refused" || row[2] != "-1" {
			t.Errorf("%s: Save/Measure answered %v, want refused and -1 (docs/SPEC.md §5.1)", c.name, row)
		}
		twin := got[c.name+"_twin"]
		if len(twin) != 5 || twin[0] != "wrote" || twin[1] != twin[2] || twin[3] != "read" || twin[4] != "same" {
			t.Errorf("%s twin: Save/Load answered %v, want the boundary value written, measured and read back exactly", c.name, twin)
		}
	}
}

// countMinWriterSchema is countMinSchema with no minimum on the counted arrays.
var countMinWriterSchema = strings.ReplaceAll(countMinSchema, "[2..3]", "[..3]")

func TestSquirrelReadsRefuseCountBelowMin(t *testing.T) {
	sqrun.Require(t)
	t.Parallel()
	dir := t.TempDir()
	modW := emitUnit(t, dir, "w", unitFromSchema(t, countMinWriterSchema))
	modR := emitUnit(t, dir, "r", unitFromSchema(t, countMinSchema))
	script := fmt.Sprintf(`local W = dofile("w/schema_module.nut", true).require(%q);
local R = dofile("r/schema_module.nut", true).require(%q);
local verdict = function(x) { return x == null ? "refused" : "read"; };
local variable = function(key, xs) {
  local v = W.Root(); v.Least = xs;
  local b = W.RootSave(v);
  if (b == null) { print(key + " unwritten\n"); return; }
  local back = R.RootLoad(b);
  print(key + " " + verdict(back) + (back == null ? "" : " " + back.Least.len()) + "\n");
};
local nested = function(key, xs) {
  local v = W.Root(); local i = W.Item(); i.Least = xs; v.Items = [i];
  local b = W.RootSave(v);
  if (b == null) { print(key + " unwritten\n"); return; }
  print(key + " " + verdict(R.RootLoad(b)) + "\n");
};
local json = function(key, text) { print(key + " " + verdict(R.RootFromJson(text)) + "\n"); };
local voc = {};
local ar = {};
if (!R.AnnounceRead(voc, W.TableMessageAnnouncement, ar)) throw "announce: " + ar.reason;
local message = function(key, xs) {
  local v = W.Msg(); v.Least = xs;
  local b = blob(64);
  local n = W.MsgSaveMessages([v], b, {});
  if (n < 0) { print(key + " unwritten\n"); return; }
  local c = blob(n);
  for (local i = 0; i < n; i++) c[i] = b[i];
  local rr = {};
  local k = R.MsgLoadMessages([R.Msg()], voc, c, rr);
  print(key + " " + (rr.refused ? "refused" : "read " + k) + "\n");
};
variable("variable_under", [7]);
variable("variable_min", [7, 8]);
nested("nested_under", [7]);
nested("nested_min", [7, 8]);
json("json_under", "{\"least\":[7]}");
json("json_empty", "{\"least\":[]}");
json("json_min", "{\"least\":[7,8]}");
message("message_under", [7]);
message("message_min", [7, 8]);
`, modW, modR)
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(script))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", nil)), "\n"), "\n") {
		if k, v, ok := strings.Cut(line, " "); ok {
			got[k] = v
		}
	}
	for key, want := range map[string]string{
		"variable_under": "refused", "variable_min": "read 2",
		"nested_under": "refused", "nested_min": "read",
		"json_under": "refused", "json_empty": "refused", "json_min": "read",
		"message_under": "refused", "message_min": "read 1",
	} {
		if got[key] != want {
			t.Errorf("%s: %q, want %q (docs/SPEC.md §5.1 row 3)", key, got[key], want)
		}
	}
}
