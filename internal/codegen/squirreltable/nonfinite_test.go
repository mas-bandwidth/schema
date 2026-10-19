package squirreltable_test

// A PLAIN FLOAT'S SPECIAL VALUES TAKE ONE RULE (docs/SPEC-TABLES.md §3.3).
// A float32 or float64 field with no declared range lands -0, a NaN (quiet or
// signalling, any payload), either infinity and the largest subnormal as its
// bit pattern. The tolerant variable load counts nothing and names the first
// non-finite field on report.nonFinite; the strict read refuses it as
// non_finite, naming that field through the same witness the other counter
// refusals use. Save writes the same bytes as the Go oracle's tablewire.
//
// The wire and the expected landed bits come from schema-go's own engine over
// the same unit (internal/tablewire and internal/tabletext), never typed by
// hand. Every assertion is on RAW BITS: -0 == +0 and NaN != NaN under ==.

import (
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const nfSchema = `package fs

type Inner
{
    c float32 = 1.0
    d float64 = 1.0
}

table Root
{
    a float32 = 1.0
    b float64 = 1.0
    n Inner
    xs [..3]float32
    ys [3]float64
}
`

const nfProbe = `local inputs = dofile("inputs.nut", true);
local schema = dofile("fs/schema_module.nut", true);
local T = schema.require("__MOD__");
local L = schema.require("schema_table_load");
local S = schema.require("schema_table_save");
local R = schema.require("schema_table_refusal");
local hex = function(b) {
  local s = "";
  for (local i = 0; i < b.len(); i++) s += format("%02x", b[i] & 0xff);
  return s;
};
local bits = function(x) {
  local b = castf2i(x);
  return format("%08x%08x", (b >>> 32) & 0xffffffff, b & 0xffffffff);
};
local nn = function(x) { return x == null ? "-" : x; };
local keys = __KEYS__;
foreach (key in keys) {
  local report = { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0,
    duplicate = 0, malformed = false, refused = false, nonFinite = 0 };
  local v = L.loadTolerant(T, "Root", inputs[key], report);
  if (v == null) { print(key + " LOADNIL\n"); continue; }
  print(key + " tol=" + (report.malformed ? 1 : 0).tostring() + "," +
        (report.refused ? 1 : 0).tostring() + "," + report.unknown.tostring() + "," +
        report.kindMismatch.tostring() + "," + report.clamped.tostring() + "," +
        report.widened.tostring() + "," + report.duplicate.tostring() + "," +
        report.nonFinite.tostring() + "\n");
  print(key + " bits=" + bits(v.A) + "," + bits(v.B) + "," + bits(v.N.C) + "," +
        bits(v.N.D) + "," + bits(v.Xs[1]) + "," + bits(v.Ys[1]) + "\n");
  local b = S.save(T, "Root", v);
  print(key + " save=" + hex(b) + " measure=" + S.measure(T, "Root", v).tostring() + "\n");
  local r2 = {};
  local sv = R.ReadStrict(T, "Root", inputs[key], r2);
  print(key + " strict=" + (r2.reason == null ? "ok" : r2.reason) + " field=" +
        nn(r2.field) + " value=" + (sv == null ? "nil" : "some") + "\n");
}
`

type nfCase struct {
	name   string
	b32    uint32
	b64    uint64
	finite bool
}

var nfCases = []nfCase{
	{"plus_zero", 0x00000000, 0x0000000000000000, true},
	{"minus_zero", 0x80000000, 0x8000000000000000, true},
	{"nan_quiet", 0x7FC00000, 0x7FF8000000000000, false},
	{"nan_signalling", 0x7F800001, 0x7FF0000000000001, false},
	{"nan_payload", 0xFFA5A5A5, 0x7FF00DEFACED0001, false},
	{"plus_inf", 0x7F800000, 0x7FF0000000000000, false},
	{"minus_inf", 0xFF800000, 0xFFF0000000000000, false},
	{"largest_subnormal", 0x007FFFFF, 0x000FFFFFFFFFFFFF, true},
}

type nfLocation struct {
	name    string
	witness string
	f32     bool
	set     func(*tabletext.Instance, float64)
}

func nfField(t *testing.T, inst *tabletext.Instance, name string) *tabletext.Field {
	t.Helper()
	f, ok := inst.FieldByKey(name)
	if !ok {
		t.Fatalf("%s declares no field %q", inst.Def.Name, name)
	}
	return f
}

func TestSquirrelPlainFloatSpecialValuesAgreeAcrossForms(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "Fs.schema")
	if err := os.WriteFile(src, []byte(nfSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{src})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("the schema does not load: %v", err)
	}
	model := tabletext.NewModel(u)
	st := model.Lookup("Root")
	if st == nil {
		t.Fatal("the unit declares no Root table")
	}
	inner := model.Lookup("Inner")
	if inner == nil {
		t.Fatal("the unit declares no Inner type")
	}

	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	out := filepath.Join(dir, "fs")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}

	locations := []nfLocation{
		{name: "top_f32", witness: "a", f32: true, set: func(inst *tabletext.Instance, v float64) {
			nfField(t, inst, "a").Cell.F = v
		}},
		{name: "top_f64", witness: "b", set: func(inst *tabletext.Instance, v float64) {
			nfField(t, inst, "b").Cell.F = v
		}},
		{name: "nested_f32", witness: "c", f32: true, set: func(inst *tabletext.Instance, v float64) {
			nfField(t, nfField(t, inst, "n").Cell.Tab, "c").Cell.F = v
		}},
		{name: "nested_f64", witness: "d", set: func(inst *tabletext.Instance, v float64) {
			nfField(t, nfField(t, inst, "n").Cell.Tab, "d").Cell.F = v
		}},
		{name: "list_f32", witness: "xs", f32: true, set: func(inst *tabletext.Instance, v float64) {
			f := nfField(t, inst, "xs")
			f.Count = 3
			f.Elems[1].F = v
		}},
		{name: "fixed_f64", witness: "ys", set: func(inst *tabletext.Instance, v float64) {
			nfField(t, inst, "ys").Elems[1].F = v
		}},
	}

	inputs := map[string][]byte{}
	keys := make([]string, 0, len(nfCases)*len(locations))
	wants := map[string][]string{}
	for _, c := range nfCases {
		for _, loc := range locations {
			key := c.name + "/" + loc.name
			keys = append(keys, key)

			value := math.Float64frombits(c.b64)
			if loc.f32 {
				value = tablewire.WidenF32(c.b32)
			}
			inst := model.New(st)
			xs := nfField(t, inst, "xs")
			xs.Count = 3
			xs.Elems[0].F = 2
			ys := nfField(t, inst, "ys")
			ys.Elems[0].F = 2
			loc.set(inst, value)
			wire, err := tablewire.Encode(model, inst)
			if err != nil {
				t.Fatalf("%s: the Go writer refused: %v", key, err)
			}
			decoded := model.New(st)
			var rep tabletext.Report
			ok, err := tablewire.Decode(model, decoded, wire, &rep)
			if err != nil || !ok {
				t.Fatalf("%s: the Go reader refused the writer's own bytes: ok %v err %v report %+v", key, ok, err, rep)
			}
			if !rep.Silent() {
				t.Fatalf("%s: the Go reader counted %+v, want every counter zero", key, rep)
			}
			bits := []uint64{
				math.Float64bits(nfField(t, decoded, "a").Cell.F),
				math.Float64bits(nfField(t, decoded, "b").Cell.F),
				math.Float64bits(nfField(t, nfField(t, decoded, "n").Cell.Tab, "c").Cell.F),
				math.Float64bits(nfField(t, nfField(t, decoded, "n").Cell.Tab, "d").Cell.F),
				math.Float64bits(nfField(t, decoded, "xs").Elems[1].F),
				math.Float64bits(nfField(t, decoded, "ys").Elems[1].F),
			}
			one := math.Float64bits(1)
			wantBits := []uint64{one, one, one, one, 0, 0}
			switch loc.name {
			case "top_f32":
				wantBits[0] = math.Float64bits(tablewire.WidenF32(c.b32))
			case "top_f64":
				wantBits[1] = c.b64
			case "nested_f32":
				wantBits[2] = math.Float64bits(tablewire.WidenF32(c.b32))
			case "nested_f64":
				wantBits[3] = c.b64
			case "list_f32":
				wantBits[4] = math.Float64bits(tablewire.WidenF32(c.b32))
			case "fixed_f64":
				wantBits[5] = c.b64
			}
			for i := range wantBits {
				if bits[i] != wantBits[i] {
					t.Fatalf("%s: the Go reader landed %016x in slot %d, want %016x", key, bits[i], i, wantBits[i])
				}
			}
			bits = wantBits
			nonFinite := 0
			reason, field, strictValue := "ok", "-", "some"
			if !c.finite {
				nonFinite = 1
				reason, field, strictValue = "non_finite", loc.witness, "nil"
			}
			inputs[key] = wire
			wants[key] = []string{
				fmt.Sprintf("tol=0,0,0,0,0,0,0,%d", nonFinite),
				fmt.Sprintf("bits=%08x%08x,%08x%08x,%08x%08x,%08x%08x,%08x%08x,%08x%08x",
					bits[0]>>32, uint32(bits[0]), bits[1]>>32, uint32(bits[1]),
					bits[2]>>32, uint32(bits[2]), bits[3]>>32, uint32(bits[3]),
					bits[4]>>32, uint32(bits[4]), bits[5]>>32, uint32(bits[5])),
				fmt.Sprintf("save=%s measure=%d", hex.EncodeToString(wire), len(wire)),
				fmt.Sprintf("strict=%s field=%s value=%s", reason, field, strictValue),
			}
		}
	}

	quoted := make([]string, len(keys))
	for i, key := range keys {
		quoted[i] = fmt.Sprintf("%q", key)
	}
	probe := strings.ReplaceAll(nfProbe, "__MOD__", squirreltable.ModuleName(u))
	probe = strings.ReplaceAll(probe, "__KEYS__", "["+strings.Join(quoted, ", ")+"]")
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(probe))
	lines := strings.Split(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", inputs)), "\n"), "\n")
	got := map[string][]string{}
	for _, line := range lines {
		key, rest, ok := strings.Cut(line, " ")
		if !ok {
			t.Fatalf("probe line %q is not a case and a result", line)
		}
		got[key] = append(got[key], rest)
	}
	for _, key := range keys {
		want := wants[key]
		have, ok := got[key]
		if !ok {
			t.Errorf("%s: the probe printed no row", key)
			continue
		}
		if len(have) != len(want) {
			t.Errorf("%s: the probe printed %d lines, want %d: %q", key, len(have), len(want), have)
			continue
		}
		for i := range want {
			if have[i] != want[i] {
				t.Errorf("%s: line %d is %q, want %q", key, i+1, have[i], want[i])
			}
		}
	}
	t.Logf("compared %d plain-float special-value cases across %d locations", len(keys), len(locations))
}
