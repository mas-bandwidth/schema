package squirreltable_test

// THE MESSAGE WRITER REFUSES A VALUE ITS DECLARATION DOES NOT ADMIT
// (docs/SPEC.md §5.1, docs/SPEC-TABLES.md §3.3, §5). Squirrel has no assert,
// so RootSaveMessages and RootMeasureMessages refuse in every build: -1, the
// report refused, nothing raised and nothing written, for a ranged integer
// off its min or max, a bits(N) value wider than N bits, an integer its
// storage cannot carry (a bare scalar, an array element, an arm payload, an
// optional), ill-formed text, an enum name no variant has, a union tag no arm
// has, a compressed float off its range, and a value of the wrong Squirrel
// type. The lawful twin at each boundary writes the Go engine's batch byte for
// byte and reads back exactly through RootLoadMessages. The Go engine's own
// verdict on every row is checked first, in its own subtest.

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const offContractSchema = `package offcontract

enum Mode { Idle, Run, Fly }

type Child
{
    n uint8
}

union Arm
{
    c Child
    k uint8
}

fixed table Root
{
    r     int32 | min = -5, max = 5
    b     bits(6)
    xs    [..3]int16
    fx    [2]int16
    s     string(4)
    mode  Mode
    arm   Arm
    ratio float32 | min = 0, max = 10, resolution = 0.01
    small int8
    opt   ?int32
}
`

// offContractRow is one rule: the off-contract value (nil in Go for a value
// only Squirrel's dynamic types can hold) and its lawful twin at the boundary,
// each as a Go setter and as Squirrel statements on v, and the Squirrel test
// the twin's read-back must pass on w.
type offContractRow struct {
	name        string
	goOff, goOK func(t *testing.T, m *tabletext.Model, inst *tabletext.Instance)
	sqOff, sqOK string
	sqSame      string
}

func ocInt(name string, v int64) func(*testing.T, *tabletext.Model, *tabletext.Instance) {
	return func(t *testing.T, _ *tabletext.Model, inst *tabletext.Instance) {
		c := &nfField(t, inst, name).Cell
		c.I, c.U = v, uint64(v)
	}
}

func ocElem(v int64) func(*testing.T, *tabletext.Model, *tabletext.Instance) {
	return func(t *testing.T, _ *tabletext.Model, inst *tabletext.Instance) {
		f := nfField(t, inst, "xs")
		f.Count = 1
		f.Elems[0].I, f.Elems[0].U = v, uint64(v)
	}
}

func ocFixedFirst(v int64) func(*testing.T, *tabletext.Model, *tabletext.Instance) {
	return func(t *testing.T, _ *tabletext.Model, inst *tabletext.Instance) {
		f := nfField(t, inst, "fx")
		f.Elems[0].I, f.Elems[0].U = v, uint64(v)
	}
}

func ocText(s string) func(*testing.T, *tabletext.Model, *tabletext.Instance) {
	return func(t *testing.T, _ *tabletext.Model, inst *tabletext.Instance) {
		f := nfField(t, inst, "s")
		f.Cell.Str = []byte(s)
		f.Count = len(s)
	}
}

func ocFloat(v float64) func(*testing.T, *tabletext.Model, *tabletext.Instance) {
	return func(t *testing.T, _ *tabletext.Model, inst *tabletext.Instance) { nfField(t, inst, "ratio").Cell.F = v }
}

func ocOpt(v int64) func(*testing.T, *tabletext.Model, *tabletext.Instance) {
	return func(t *testing.T, _ *tabletext.Model, inst *tabletext.Instance) {
		f := nfField(t, inst, "opt")
		f.Present = true
		f.Cell.I, f.Cell.U = v, uint64(v)
	}
}

// ocArm selects arm tag; a k arm (tag 2) carries k.
func ocArm(tag uint64, k int64) func(*testing.T, *tabletext.Model, *tabletext.Instance) {
	return func(t *testing.T, m *tabletext.Model, inst *tabletext.Instance) {
		c := &nfField(t, inst, "arm").Cell
		c.U = tag
		if tag == 2 {
			un := tabletext.UnionOf(nfField(t, inst, "arm").Def)
			c.Arm = m.NewArm(un.Variants[1])
			c.Arm.Cell.I, c.Arm.Cell.U = k, uint64(k)
		}
	}
}

func offContractRows() []offContractRow {
	sqStr := func(s string) string { return squirrel.StringLit([]byte(s)) }
	return []offContractRow{
		// 1: a ranged integer off its min or max
		{"range_high", ocInt("r", 6), ocInt("r", 5), "v.R = 6;", "v.R = 5;", "w.R == 5"},
		{"range_low", ocInt("r", -6), ocInt("r", -5), "v.R = -6;", "v.R = -5;", "w.R == -5"},
		{"range_far", ocInt("r", 9), ocInt("r", 5), "v.R = 9;", "v.R = 5;", "w.R == 5"},
		// 2: a bits(N) value wider than N bits
		{"bits_wide", ocInt("b", 64), ocInt("b", 63), "v.B = 64;", "v.B = 63;", "w.B == 63"},
		{"bits_negative", ocInt("b", -1), ocInt("b", 63), "v.B = -1;", "v.B = 63;", "w.B == 63"},
		// a bare integer its storage width cannot carry
		{"int8_high", ocInt("small", 128), ocInt("small", 127), "v.Small = 128;", "v.Small = 127;", "w.Small == 127"},
		{"int8_low", ocInt("small", -129), ocInt("small", -128), "v.Small = -129;", "v.Small = -128;", "w.Small == -128"},
		{"int8_far", ocInt("small", 300), ocInt("small", 127), "v.Small = 300;", "v.Small = 127;", "w.Small == 127"},
		{"elem_high", ocElem(32768), ocElem(32767), "v.Xs = [32768];", "v.Xs = [32767];", "w.Xs.len() == 1 && w.Xs[0] == 32767"},
		{"elem_low", ocElem(-32769), ocElem(-32768), "v.Xs = [-32769];", "v.Xs = [-32768];", "w.Xs.len() == 1 && w.Xs[0] == -32768"},
		{"elem_far", ocElem(70000), ocElem(32767), "v.Xs = [70000];", "v.Xs = [32767];", "w.Xs.len() == 1 && w.Xs[0] == 32767"},
		{"opt_high", ocOpt(1 << 40), ocOpt(2147483647), "v.OptPresent = true; v.Opt = 1 << 40;", "v.OptPresent = true; v.Opt = 2147483647;", "w.OptPresent && w.Opt == 2147483647"},
		{"opt_low", ocOpt(-2147483649), ocOpt(-2147483648), "v.OptPresent = true; v.Opt = -2147483649;", "v.OptPresent = true; v.Opt = -2147483648;", "w.OptPresent && w.Opt == -2147483648"},
		{"arm_payload", ocArm(2, 256), ocArm(2, 255), "v.Arm.Type = 2; v.Arm.K = 256;", "v.Arm.Type = 2; v.Arm.K = 255;", "w.Arm.Type == 2 && w.Arm.K == 255"},
		// 5: an enum name no variant has
		{"enum_unknown", ocInt("mode", 4), ocInt("mode", 3), `v.Mode = "Walk";`, `v.Mode = "Fly";`, `w.Mode == "Fly"`},
		// 6: a union tag no arm has
		{"union_tag", ocArm(3, 0), ocArm(2, 255), "v.Arm.Type = 3;", "v.Arm.Type = 2; v.Arm.K = 255;", "w.Arm.Type == 2 && w.Arm.K == 255"},
		// 8: ill-formed text
		{"text_invalid", ocText("\xff\xfe"), ocText("\xc3\xa9ab"), "v.S = " + sqStr("\xff\xfe") + ";", "v.S = " + sqStr("\xc3\xa9ab") + ";", "w.S == " + sqStr("\xc3\xa9ab")},
		{"text_zero", ocText("a\x00b"), ocText("abcd"), "v.S = " + sqStr("a\x00b") + ";", `v.S = "abcd";`, `w.S == "abcd"`},
		{"text_surrogate", ocText("\xed\xa0\x80"), ocText("\xe2\x82\xac"), "v.S = " + sqStr("\xed\xa0\x80") + ";", "v.S = " + sqStr("\xe2\x82\xac") + ";", "w.S == " + sqStr("\xe2\x82\xac")},
		// 10: a compressed float off its range
		{"float_high", ocFloat(25), ocFloat(10), "v.Ratio = 25.0;", "v.Ratio = 10.0;", "w.Ratio == 10.0"},
		{"float_low", ocFloat(-3), ocFloat(0), "v.Ratio = -3.0;", "v.Ratio = 0.0;", "w.Ratio == 0.0"},
		{"float_just_over", ocFloat(10.000001), ocFloat(10), "v.Ratio = 10.000001;", "v.Ratio = 10.0;", "w.Ratio == 10.0"},
		// a value of the wrong Squirrel type (Go's cells cannot hold one)
		{"type_int_string", nil, ocInt("r", 1), `v.R = "1";`, "v.R = 1;", "w.R == 1"},
		{"type_int_float", nil, ocInt("small", 1), "v.Small = 1.5;", "v.Small = 1;", "w.Small == 1"},
		{"type_int_float_default", nil, ocInt("r", 5), "v.R = 0.0;", "v.R = 5;", "w.R == 5"},
		{"type_bits_float_default", nil, ocInt("b", 63), "v.B = 0.0;", "v.B = 63;", "w.B == 63"},
		{"type_int8_float_default", nil, ocInt("small", 127), "v.Small = 0.0;", "v.Small = 127;", "w.Small == 127"},
		{"type_elem_float_default", nil, ocElem(32767), "v.Xs = [0.0];", "v.Xs = [32767];", "w.Xs.len() == 1 && w.Xs[0] == 32767"},
		{"type_fixed_elem_float_default", nil, ocFixedFirst(32767), "v.Fx = [0.0, 0];", "v.Fx = [32767, 0];", "w.Fx[0] == 32767 && w.Fx[1] == 0"},
		{"type_bits_bool", nil, ocInt("b", 1), "v.B = true;", "v.B = 1;", "w.B == 1"},
		{"type_optional_string", nil, ocOpt(7), `v.OptPresent = true; v.Opt = "7";`, "v.OptPresent = true; v.Opt = 7;", "w.OptPresent && w.Opt == 7"},
		{"type_text_int", nil, ocText("5"), "v.S = 5;", `v.S = "5";`, `w.S == "5"`},
		{"type_enum_int", nil, ocInt("mode", 3), "v.Mode = 3;", `v.Mode = "Fly";`, `w.Mode == "Fly"`},
		{"type_float_string", nil, ocFloat(1), `v.Ratio = "1";`, "v.Ratio = 1.0;", "w.Ratio == 1.0"},
		{"type_array_string", nil, ocElem(1), `v.Xs = "abc";`, "v.Xs = [1];", "w.Xs.len() == 1 && w.Xs[0] == 1"},
		{"type_elem_string", nil, ocElem(1), `v.Xs = ["1"];`, "v.Xs = [1];", "w.Xs.len() == 1 && w.Xs[0] == 1"},
		{"type_union_int", nil, ocArm(2, 1), "v.Arm = 5;", "v.Arm.Type = 2; v.Arm.K = 1;", "w.Arm.Type == 2 && w.Arm.K == 1"},
		{"type_union_tag_string", nil, ocArm(2, 1), `v.Arm.Type = "2"; v.Arm.K = 1;`, "v.Arm.Type = 2; v.Arm.K = 1;", "w.Arm.Type == 2 && w.Arm.K == 1"},
	}
}

func TestSquirrelSaveMessagesRefusesOffContract(t *testing.T) {
	t.Parallel()
	sqrun.Require(t)
	u, model, _ := nfLoadNamed(t, offContractSchema)
	st := model.Lookup("Root")
	if st == nil {
		t.Fatal("the unit declares no Root table")
	}
	rows := offContractRows()

	// THE GO ENGINE'S VERDICT FIRST: every off-contract value is refused with
	// nothing written, and every lawful twin writes and reads back whole.
	lawful := map[string]string{}
	t.Run("go", func(t *testing.T) {
		var vocabulary tablewire.Vocabulary
		var announced tabletext.Report
		if err := vocabulary.AnnounceRead(tablewire.Announce(u), &announced); err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			if r.goOff != nil {
				inst := model.New(st)
				r.goOff(t, model, inst)
				if out, err := tablewire.EncodeMessage(model, inst); err == nil || out != nil {
					t.Errorf("%s: the Go engine wrote %x %v for the off-contract value, want a refusal", r.name, out, err)
				}
			}
			inst := model.New(st)
			r.goOK(t, model, inst)
			batch, err := tablewire.EncodeMessage(model, inst)
			if err != nil {
				t.Errorf("%s: the Go engine refused the lawful twin: %v", r.name, err)
				continue
			}
			back := model.New(st)
			var rep tabletext.Report
			if n, ok, err := tablewire.DecodeMessages(model, []*tabletext.Instance{back}, batch, &vocabulary, &rep); n != 1 || !ok || err != nil {
				t.Errorf("%s: the Go engine's reader answered %d %v %v", r.name, n, ok, err)
				continue
			}
			again, err := tablewire.EncodeMessage(model, back)
			if err != nil || hex.EncodeToString(again) != hex.EncodeToString(batch) {
				t.Errorf("%s: the lawful twin did not read back whole: %x then %x %v", r.name, batch, again, err)
			}
			lawful[r.name] = hex.EncodeToString(batch)
		}
	})
	if t.Failed() {
		t.Fatal("the Go engine does not hold the rule this test pins on Squirrel")
	}

	dir := t.TempDir()
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	out := filepath.Join(dir, "oc")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}
	var b strings.Builder
	b.WriteString(`local schema = dofile("oc/schema_module.nut", true);
local T = schema.require("` + squirreltable.ModuleName(u) + `");
local hex = function(b, n) {
  local s = "";
  for (local i = 0; i < n; i++) s += format("%02x", b[i] & 0xff);
  return s;
};
local cut = function(b, n) {
  local c = blob(n);
  for (local i = 0; i < n; i++) c[i] = b[i];
  return c;
};
local voc = {};
local ar = {};
if (!T.AnnounceRead(voc, T.TableMessageAnnouncement, ar)) throw "announce: " + ar.reason;
// probe writes v through the measure and the save into a buffer of 0xa5
// and says what each answered, whether the buffer was touched, and, for a
// write, the batch and whether it reads back as same says
local probe = function(key, v, same) {
  try {
    local mr = {};
    local m = T.RootMeasureMessages([v], mr);
    local b = blob(64);
    for (local i = 0; i < 64; i++) b[i] = 0xa5;
    local sr = {};
    local n = T.RootSaveMessages([v], b, sr);
    if (n < 0) {
      local clean = true;
      for (local i = 0; i < 64; i++) if (b[i] != 0xa5) clean = false;
      print(key + " refused " + (m < 0 && mr.refused ? "measure-refused" : "measure-answered") + " " + (sr.refused ? "reported" : "unreported") + " " + (clean ? "untouched" : "touched") + "\n");
      return;
    }
    local out = [T.Root()];
    local rr = {};
    local k = T.RootLoadMessages(out, voc, cut(b, n), rr);
    print(key + " wrote " + (m == n ? "measured" : "mismeasured") + " " + hex(b, n) + " " + (k == 1 && same(out[0]) ? "same" : "differs") + "\n");
  } catch (e) {
    print(key + " raised " + e + "\n");
  }
};
`)
	want := map[string]string{}
	for _, r := range rows {
		fmt.Fprintf(&b, "{ local v = T.Root(); %s probe(%q, v, function(w) { return %s; }); }\n", r.sqOff, "off:"+r.name, r.sqSame)
		fmt.Fprintf(&b, "{ local v = T.Root(); %s probe(%q, v, function(w) { return %s; }); }\n", r.sqOK, "ok:"+r.name, r.sqSame)
		want["off:"+r.name] = "off:" + r.name + " refused measure-refused reported untouched"
		want["ok:"+r.name] = "ok:" + r.name + " wrote measured " + lawful[r.name] + " same"
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(b.String()))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", nil)), "\n"), "\n") {
		key, _, _ := strings.Cut(line, " ")
		got[key] = line
	}
	for _, r := range rows {
		for _, key := range []string{"off:" + r.name, "ok:" + r.name} {
			if got[key] != want[key] {
				t.Errorf("%s: %q, want %q", key, got[key], want[key])
			}
		}
	}
}
