package squirreltable_test

// A TABLE FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3; Glenn, 2026-10-27,
// "tables go finite-only too, same rule as packets"). On the variable form:
//
//   - -0, the subnormals and the largest finite value ride as their bits: the
//     tolerant load lands them exactly and counts nothing, the strict read
//     accepts them, and Save writes the Go oracle's bytes.
//   - a NaN (quiet, signalling, any payload, either sign) and both
//     infinities are refused by every read on their RAW BITS: the tolerant
//     load answers null with refused and reason
//     `non_finite_float: <Owner>.<field>`, and the strict read answers
//     non_finite_float naming that field. The wire is the oracle's own bytes
//     for a finite value with the field's payload replaced, and the Go oracle
//     (internal/tablewire) refuses the same wire naming the same field.
//   - every writer refuses one: Save answers null and Measure -1, with
//     SaveReason `non_finite_float: <Owner>.<field>`, exactly the field the
//     oracle's encoder refuses; a float32 field holding a double past
//     float32's range (it narrows to an infinity) is refused too.
//   - a RETAINED unknown entry of a float kind is tested like a known one:
//     LoadRetain refuses it, named `<Table>.0x<id>`, as the oracle's
//     DecodeRetain does.
//
// TestSquirrelTableFiniteFloatNegativeControl removes the load's raw-bits
// test in memory and requires exactly the non-finite read rows to go red.

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
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
	"github.com/mas-bandwidth/schema/v2/ir"
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

// nfSchemaNext is the same table one field later: its z is an entry the
// nfSchema reader cannot name, which LoadRetain retains.
const nfSchemaNext = `package fs

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
    z float32
}
`

const nfProbe = `local inputs = dofile("inputs.nut", true);
local schema = dofile("fs/schema_module.nut", true);
local T = schema.require("__MOD__");
local L = schema.require("schema_table_load");
local S = schema.require("schema_table_save");
local R = schema.require("schema_table_refusal");
local RT = schema.require("schema_table_retain");
local hex = function(b) {
  if (b == null) return "nil";
  local s = "";
  for (local i = 0; i < b.len(); i++) s += format("%02x", b[i] & 0xff);
  return s;
};
local bits = function(x) {
  local b = castf2i(x);
  return format("%08x%08x", (b >>> 32) & 0xffffffff, b & 0xffffffff);
};
local nn = function(x) { return x == null ? "-" : x; };
local newReport = function() {
  return { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0,
    duplicate = 0, malformed = false, refused = false };
};
// a value from its bits: 16 hex digits are a double, 8 a float32
local hexInt = function(h) {
  local n = 0;
  foreach (c in h) n = (n << 4) | (c <= '9' ? c - '0' : c - 'a' + 10);
  return n;
};
local value = function(h) {
  if (h.len() == 16) return casti2f(hexInt(h));
  local b = blob(4);
  b.writen(hexInt(h), 'i');
  b.seek(0);
  return b.readn('f');
};
// LOAD rows: the tolerant load, the strict read
foreach (key in __LOADS__) {
  local report = newReport();
  local v = L.loadTolerant(T, "Root", inputs[key], report);
  if (v == null) {
    print(key + " tol=nil refused=" + report.refused + " reason=" + nn(report.reason) + "\n");
  } else {
    print(key + " tol=" + (report.malformed ? 1 : 0).tostring() + "," +
          (report.refused ? 1 : 0).tostring() + "," + report.unknown.tostring() + "," +
          report.kindMismatch.tostring() + "," + report.clamped.tostring() + "," +
          report.widened.tostring() + "," + report.duplicate.tostring() + "\n");
    print(key + " bits=" + bits(v.A) + "," + bits(v.B) + "," + bits(v.N.C) + "," +
          bits(v.N.D) + "," + bits(v.Xs[1]) + "," + bits(v.Ys[1]) + "\n");
    local b = S.save(T, "Root", v);
    print(key + " save=" + hex(b) + " measure=" + S.measure(T, "Root", v).tostring() + "\n");
  }
  local r2 = {};
  local sv = R.ReadStrict(T, "Root", inputs[key], r2);
  print(key + " strict=" + (r2.reason == null ? "ok" : r2.reason) + " field=" +
        nn(r2.field) + " value=" + (sv == null ? "nil" : "some") + "\n");
}
// SAVE rows: one value at one location, Save and Measure
foreach (row in __SAVES__) {
  local v = T.Root();
  v.Xs = [2.0, 0.0, 0.0];
  v.Ys[0] = 2.0;
  local x = value(row[2]);
  switch (row[1]) {
    case "top_f32": v.A = x; break;
    case "top_f64": v.B = x; break;
    case "nested_f32": v.N.C = x; break;
    case "nested_f64": v.N.D = x; break;
    case "list_f32": v.Xs[1] = x; break;
    case "fixed_f64": v.Ys[1] = x; break;
  }
  local b = S.save(T, "Root", v);
  local why = S.SaveReason;
  print(row[0] + " write=" + (b == null ? "nil" : hex(b)) + " measure=" +
        S.measure(T, "Root", v).tostring() + " reason=" + nn(why) + "\n");
}
// RETAIN rows: LoadRetain over a wire carrying an entry this reader cannot name
foreach (key in __RETAINS__) {
  local report = newReport();
  local store = { capacity = 1048576, idCapacity = 1024 };
  local v = RT.LoadRetain(T, "Root", inputs[key], store, report);
  print(key + " retain=" + (v == null ? "nil" : "some") + " refused=" + report.refused +
        " reason=" + nn(("reason" in report) ? report.reason : null) + "\n");
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
	{"largest_subnormal", 0x007FFFFF, 0x000FFFFFFFFFFFFF, true},
	{"minus_smallest_subnormal", 0x80000001, 0x8000000000000001, true},
	{"largest_finite", 0x7F7FFFFF, 0x7FEFFFFFFFFFFFFF, true},
	{"nan_quiet", 0x7FC00000, 0x7FF8000000000000, false},
	{"nan_minus_quiet", 0xFFC00000, 0xFFF8000000000000, false},
	{"nan_signalling", 0x7F800001, 0x7FF0000000000001, false},
	{"nan_payload", 0xFFA5A5A5, 0x7FF00DEFACED0001, false},
	{"plus_inf", 0x7F800000, 0x7FF0000000000000, false},
	{"minus_inf", 0xFF800000, 0xFFF0000000000000, false},
}

// the sentinel each location holds while the oracle writes the wire a
// non-finite row patches: distinctive finite bits, found exactly once.
const (
	nfSentinel32 = 0x3E1A2B3C
	nfSentinel64 = 0x3FB1A2B3C4D5E6F7
)

type nfLocation struct {
	name  string
	owner string // the <Owner>.<field> the refusal names
	f32   bool
	set   func(*tabletext.Instance, float64)
}

func nfField(t *testing.T, inst *tabletext.Instance, name string) *tabletext.Field {
	t.Helper()
	f, ok := inst.FieldByKey(name)
	if !ok {
		t.Fatalf("%s declares no field %q", inst.Def.Name, name)
	}
	return f
}

func nfLocations(t *testing.T) []nfLocation {
	return []nfLocation{
		{"top_f32", "Root.a", true, func(inst *tabletext.Instance, v float64) { nfField(t, inst, "a").Cell.F = v }},
		{"top_f64", "Root.b", false, func(inst *tabletext.Instance, v float64) { nfField(t, inst, "b").Cell.F = v }},
		{"nested_f32", "Inner.c", true, func(inst *tabletext.Instance, v float64) {
			nfField(t, nfField(t, inst, "n").Cell.Tab, "c").Cell.F = v
		}},
		{"nested_f64", "Inner.d", false, func(inst *tabletext.Instance, v float64) {
			nfField(t, nfField(t, inst, "n").Cell.Tab, "d").Cell.F = v
		}},
		{"list_f32", "Root.xs", true, func(inst *tabletext.Instance, v float64) { nfField(t, inst, "xs").Elems[1].F = v }},
		{"fixed_f64", "Root.ys", false, func(inst *tabletext.Instance, v float64) { nfField(t, inst, "ys").Elems[1].F = v }},
	}
}

func nfLoad(t *testing.T, src string) (*ir.Unit, *tabletext.Model, *ir.Struct) {
	t.Helper()
	u, model, _ := nfLoadNamed(t, src)
	st := model.Lookup("Root")
	if st == nil {
		t.Fatal("the unit declares no Root table")
	}
	return u, model, st
}

// nfLoadNamed compiles src as one schema file and returns its unit and model.
func nfLoadNamed(t *testing.T, src string) (*ir.Unit, *tabletext.Model, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Fs.schema")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("the schema does not load: %v", err)
	}
	return u, tabletext.NewModel(u), path
}

// nfInstance is the base value every row writes: xs holds three elements and
// ys[0] a non-default, so both arrays ride.
func nfInstance(t *testing.T, model *tabletext.Model, st *ir.Struct) *tabletext.Instance {
	inst := model.New(st)
	xs := nfField(t, inst, "xs")
	xs.Count = 3
	xs.Elems[0].F = 2
	nfField(t, inst, "ys").Elems[0].F = 2
	return inst
}

// nfPatch replaces the one occurrence of the sentinel's little-endian bytes
// in wire with the pattern's.
func nfPatch(t *testing.T, wire []byte, f32 bool, b32 uint32, b64 uint64) []byte {
	t.Helper()
	var from, to []byte
	if f32 {
		from = binary.LittleEndian.AppendUint32(nil, nfSentinel32)
		to = binary.LittleEndian.AppendUint32(nil, b32)
	} else {
		from = binary.LittleEndian.AppendUint64(nil, nfSentinel64)
		to = binary.LittleEndian.AppendUint64(nil, b64)
	}
	if n := bytes.Count(wire, from); n != 1 {
		t.Fatalf("the sentinel appears %d times in the oracle's wire, want once", n)
	}
	return bytes.Replace(wire, from, to, 1)
}

// nfRun generates the unit, runs the probe over the rows and returns its
// lines by key; patch may rewrite the emitted runtime first.
func nfRun(t *testing.T, u *ir.Unit, loads, retains []string, saveRows [][3]string, inputs map[string][]byte, patch func(map[string][]byte)) map[string][]string {
	t.Helper()
	dir := t.TempDir()
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	if patch != nil {
		patch(tables)
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
	quote := func(keys []string) string {
		q := make([]string, len(keys))
		for i, k := range keys {
			q[i] = fmt.Sprintf("%q", k)
		}
		return "[" + strings.Join(q, ", ") + "]"
	}
	rows := make([]string, len(saveRows))
	for i, r := range saveRows {
		rows[i] = fmt.Sprintf("[%q, %q, %q]", r[0], r[1], r[2])
	}
	probe := strings.ReplaceAll(nfProbe, "__MOD__", squirreltable.ModuleName(u))
	probe = strings.ReplaceAll(probe, "__LOADS__", quote(loads))
	probe = strings.ReplaceAll(probe, "__SAVES__", "["+strings.Join(rows, ", ")+"]")
	probe = strings.ReplaceAll(probe, "__RETAINS__", quote(retains))
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(probe))
	got := map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", inputs)), "\n"), "\n") {
		key, rest, ok := strings.Cut(line, " ")
		if !ok {
			t.Fatalf("probe line %q is not a case and a result", line)
		}
		got[key] = append(got[key], rest)
	}
	return got
}

// nfVectors builds every row from the Go oracle: the wires, the expected
// probe lines, and the save rows. Each expectation is checked against the
// oracle's own answer on the same bytes before it is used.
func nfVectors(t *testing.T) (u *ir.Unit, loads, retains []string, saveRows [][3]string, inputs map[string][]byte, wants map[string][]string) {
	t.Helper()
	u, model, st := nfLoad(t, nfSchema)
	inputs = map[string][]byte{}
	wants = map[string][]string{}
	for _, c := range nfCases {
		for _, loc := range nfLocations(t) {
			key := c.name + "/" + loc.name
			// the SAVE row: Squirrel writes the case's value at the location
			hexBits := fmt.Sprintf("%016x", c.b64)
			if loc.f32 {
				hexBits = fmt.Sprintf("%08x", c.b32)
				if !c.finite {
					// a float32 slot holds a double; a non-finite one is the
					// double of the same class
					hexBits = fmt.Sprintf("%016x", c.b64)
				}
			}
			saveKey := "save:" + key
			saveRows = append(saveRows, [3]string{saveKey, loc.name, hexBits})
			value := math.Float64frombits(c.b64)
			if loc.f32 {
				value = float64(math.Float32frombits(c.b32))
			}
			inst := nfInstance(t, model, st)
			loc.set(inst, value)
			wire, encErr := tablewire.Encode(model, inst)
			if !c.finite {
				var refusal *tablewire.NonFiniteRefusal
				if !errors.As(encErr, &refusal) || refusal.Field != loc.owner {
					t.Fatalf("%s: the oracle's writer answered %v, want non_finite_float: %s", key, encErr, loc.owner)
				}
				wants[saveKey] = []string{fmt.Sprintf("write=nil measure=-1 reason=non_finite_float: %s", loc.owner)}
				// the LOAD row: the oracle's finite wire, the payload patched
				base := nfInstance(t, model, st)
				if loc.f32 {
					loc.set(base, float64(math.Float32frombits(nfSentinel32)))
				} else {
					loc.set(base, math.Float64frombits(nfSentinel64))
				}
				clean, err := tablewire.Encode(model, base)
				if err != nil {
					t.Fatalf("%s: the oracle refused the sentinel: %v", key, err)
				}
				wire = nfPatch(t, clean, loc.f32, c.b32, c.b64)
				var rep tabletext.Report
				_, derr := tablewire.Decode(model, model.New(st), wire, &rep)
				if !errors.As(derr, &refusal) || refusal.Field != loc.owner {
					t.Fatalf("%s: the oracle's reader answered %v, want non_finite_float: %s", key, derr, loc.owner)
				}
				loads = append(loads, key)
				inputs[key] = wire
				wants[key] = []string{
					fmt.Sprintf("tol=nil refused=true reason=non_finite_float: %s", loc.owner),
					fmt.Sprintf("strict=non_finite_float field=%s value=nil", loc.owner),
				}
				continue
			}
			if encErr != nil {
				t.Fatalf("%s: the Go writer refused a finite value: %v", key, encErr)
			}
			wants[saveKey] = []string{fmt.Sprintf("write=%s measure=%d reason=-", hex.EncodeToString(wire), len(wire))}
			decoded := model.New(st)
			var rep tabletext.Report
			ok, err := tablewire.Decode(model, decoded, wire, &rep)
			if err != nil || !ok || !rep.Silent() {
				t.Fatalf("%s: the Go reader answered ok %v err %v report %+v on its own bytes", key, ok, err, rep)
			}
			got := []uint64{
				math.Float64bits(nfField(t, decoded, "a").Cell.F),
				math.Float64bits(nfField(t, decoded, "b").Cell.F),
				math.Float64bits(nfField(t, nfField(t, decoded, "n").Cell.Tab, "c").Cell.F),
				math.Float64bits(nfField(t, nfField(t, decoded, "n").Cell.Tab, "d").Cell.F),
				math.Float64bits(nfField(t, decoded, "xs").Elems[1].F),
				math.Float64bits(nfField(t, decoded, "ys").Elems[1].F),
			}
			slot := map[string]int{"top_f32": 0, "top_f64": 1, "nested_f32": 2, "nested_f64": 3, "list_f32": 4, "fixed_f64": 5}[loc.name]
			if got[slot] != math.Float64bits(value) {
				t.Fatalf("%s: the Go reader landed %016x, want %016x", key, got[slot], math.Float64bits(value))
			}
			loads = append(loads, key)
			inputs[key] = wire
			wants[key] = []string{
				"tol=0,0,0,0,0,0,0",
				fmt.Sprintf("bits=%016x,%016x,%016x,%016x,%016x,%016x", got[0], got[1], got[2], got[3], got[4], got[5]),
				fmt.Sprintf("save=%s measure=%d", hex.EncodeToString(wire), len(wire)),
				"strict=ok field=- value=some",
			}
		}
	}

	// a float32 field holding a double past float32's range narrows to an
	// infinity: every writer refuses it
	for _, loc := range []string{"top_f32", "nested_f32", "list_f32"} {
		key := "save:overflow/" + loc
		saveRows = append(saveRows, [3]string{key, loc, fmt.Sprintf("%016x", math.Float64bits(1e300))})
		owner := map[string]string{"top_f32": "Root.a", "nested_f32": "Inner.c", "list_f32": "Root.xs"}[loc]
		wants[key] = []string{"write=nil measure=-1 reason=non_finite_float: " + owner}
	}

	// THE RETAINED UNKNOWN FLOAT: the next schema's z, which this reader
	// cannot name, read through LoadRetain
	_, nextModel, nextRoot := nfLoad(t, nfSchemaNext)
	var zID uint64
	for _, f := range nextRoot.Fields {
		if f.Name == "z" {
			zID = ir.TableFieldWireId(f)
		}
	}
	for _, c := range []nfCase{nfCases[0], nfCases[5], nfCases[9]} {
		key := "retain:" + c.name
		inst := nfInstance(t, nextModel, nextRoot)
		nfField(t, inst, "z").Cell.F = float64(math.Float32frombits(nfSentinel32))
		clean, err := tablewire.Encode(nextModel, inst)
		if err != nil {
			t.Fatal(err)
		}
		wire := nfPatch(t, clean, true, c.b32, 0)
		var rep tabletext.Report
		_, derr := tablewire.DecodeRetain(model, model.New(st), wire, &tablewire.Retain{Capacity: 1 << 20, IdCapacity: 1024}, &rep)
		name := fmt.Sprintf("Root.0x%016x", zID)
		var refusal *tablewire.NonFiniteRefusal
		if c.finite {
			if derr != nil {
				t.Fatalf("%s: the oracle refused a finite retained float: %v", key, derr)
			}
			wants[key] = []string{"retain=some refused=false reason=-"}
		} else {
			if !errors.As(derr, &refusal) || refusal.Field != name {
				t.Fatalf("%s: the oracle's retaining reader answered %v, want non_finite_float: %s", key, derr, name)
			}
			wants[key] = []string{"retain=nil refused=true reason=non_finite_float: " + name}
		}
		retains = append(retains, key)
		inputs[key] = wire
	}
	return u, loads, retains, saveRows, inputs, wants
}

func TestSquirrelTableFloatsAreFinite(t *testing.T) {
	t.Parallel()
	sqrun.Require(t)
	u, loads, retains, saveRows, inputs, wants := nfVectors(t)
	got := nfRun(t, u, loads, retains, saveRows, inputs, nil)
	n := 0
	for key, want := range wants {
		n++
		have, ok := got[key]
		if !ok {
			t.Errorf("%s: the probe printed no row", key)
			continue
		}
		if len(have) != len(want) {
			t.Errorf("%s: the probe printed %q, want %q", key, have, want)
			continue
		}
		for i := range want {
			if have[i] != want[i] {
				t.Errorf("%s: line %d is %q, want %q", key, i+1, have[i], want[i])
			}
		}
	}
	t.Logf("compared %d finite-float rows (loads, saves, retained) against the Go oracle", n)
}

// nfSabotage is one rule of the finite-float leg removed in memory: the
// runtime file, its anchors and their replacements, and the rows that must go
// red (and only those).
type nfSabotage struct {
	name  string
	file  string
	edits [][2]string
	red   func(key string, want []string) bool
}

var nfSabotages = []nfSabotage{
	{
		// the variable load's raw-bits test, both widths: every non-finite
		// known-field load row goes red
		name: "read-check",
		file: "schema_table_load.nut",
		edits: [][2]string{
			{"    case 10: { local bits = readU32(r); return finiteF32(bits) ? widenF32(bits) : NONFINITE; }",
				"    case 10: { local bits = readU32(r); return widenF32(bits); }"},
			{"    case 11: { local bits = readU64(r); return finiteF64(bits) ? casti2f(bits) : NONFINITE; }",
				"    case 11: { local bits = readU64(r); return casti2f(bits); }"},
		},
		red: func(key string, want []string) bool {
			return !strings.Contains(key, ":") && strings.HasPrefix(want[0], "tol=nil")
		},
	},
	{
		// the retained unknown entry's test: every non-finite retain row
		name: "retained-check",
		file: "schema_table_load.nut",
		edits: [][2]string{
			{"        if (!retainedFinite(r.b, start, r.at, kind)) {", "        if (false) {"},
		},
		red: func(key string, want []string) bool {
			return strings.HasPrefix(key, "retain:") && strings.HasPrefix(want[0], "retain=nil")
		},
	},
	{
		// the writer's refusal: every non-finite save row, the float32
		// overflow included
		name: "write-check",
		file: "schema_table_save.nut",
		edits: [][2]string{
			{"    if (!floatFinite(d, val)) return refuseNonFinite(d);\n", ""},
		},
		red: func(key string, want []string) bool {
			return strings.HasPrefix(key, "save:") && strings.Contains(want[0], "reason=non_finite_float")
		},
	},
}

// TestSquirrelTableFiniteFloatNegativeControl removes each rule of the leg in
// turn (each anchor matched exactly once) and requires exactly that rule's
// rows to go red while every other row stays green.
func TestSquirrelTableFiniteFloatNegativeControl(t *testing.T) {
	t.Parallel()
	sqrun.Require(t)
	u, loads, retains, saveRows, inputs, wants := nfVectors(t)
	for _, sab := range nfSabotages {
		t.Run(sab.name, func(t *testing.T) {
			got := nfRun(t, u, loads, retains, saveRows, inputs, func(tables map[string][]byte) {
				src := string(tables[sab.file])
				for _, e := range sab.edits {
					if n := strings.Count(src, e[0]); n != 1 {
						t.Fatalf("the control's anchor %q appears %d times in %s, want exactly once", e[0], n, sab.file)
					}
					src = strings.Replace(src, e[0], e[1], 1)
				}
				tables[sab.file] = []byte(src)
			})
			red, wantRed := 0, 0
			for key, want := range wants {
				mustRed := sab.red(key, want)
				if mustRed {
					wantRed++
				}
				same := len(got[key]) == len(want)
				for i := 0; same && i < len(want); i++ {
					same = got[key][i] == want[i]
				}
				switch {
				case !same && mustRed:
					red++
				case !same:
					t.Errorf("%s went red under the %s sabotage: %q, want %q", key, sab.name, got[key], want)
				case mustRed:
					t.Errorf("%s stayed green with the %s removed", key, sab.name)
				}
			}
			if red != wantRed || red == 0 {
				t.Fatalf("the %s sabotage red %d of its %d rows", sab.name, red, wantRed)
			}
			t.Logf("the %s sabotage reds exactly its %d rows", sab.name, red)
		})
	}
}
