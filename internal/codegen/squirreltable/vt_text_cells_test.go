package squirreltable_test

// vt/text-form and vt/text-form-variable (docs/roadmap-evidence/squirrel.sexp):
// the strict text read — one read: the whole value or a refusal — for the JSON
// text form and for the variable root's &node labels. The cell holds when a
// table and a &node variable-graph instance run through the strict read
// (J.FromJson) in the Squirrel interpreter: a plain text-form value and a
// shared/cyclic &node graph round-trip (FromJson -> ToJson -> FromJson), and
// malformed, out-of-range, kind-mismatched, undefined-label and
// duplicate-label texts are refused whole. The negative control plants a
// fault in a copy of runtime/json.nut (the emitted schema_table_json.nut) and
// runs sqrun to verify the strict read goes red.
//
// Functional: runs the pinned Squirrel interpreter via sqrun under
// slowtest.Gate; the source gate takes no real time, sockets, or network.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// vtTextFindings checks the strict text read's rule over the real artifacts.
// It answers every way the rule is not held, empty when it is.
func vtTextFindings(driver, jsonSrc string) []string {
	var out []string
	if body, ok := squirrelSurface(driver, `if (mode == "json-read" || mode == "json-write") {`); ok {
		if !strings.Contains(body, "J.FromJson(") {
			out = append(out, "json-read/json-write: no strict FromJson")
		}
		if strings.Contains(body, "fromJsonWalk(") {
			out = append(out, "json-read/json-write: still reads through fromJsonWalk")
		}
		if strings.Contains(body, "loadWalk") {
			out = append(out, "json-read/json-write: still reads through loadWalk")
		}
		if !strings.Contains(body, "Save") {
			out = append(out, "json-read/json-write: does not write the read back")
		}
	} else {
		out = append(out, "json-read/json-write surface missing")
	}
	if body, ok := squirrelSurface(driver, `if (mode == "json-hostile") {`); ok {
		if !strings.Contains(body, "J.FromJson(") {
			out = append(out, "json-hostile: no strict FromJson")
		}
		if strings.Contains(body, "fromJsonWalk(") {
			out = append(out, "json-hostile: still reads through fromJsonWalk")
		}
	} else {
		out = append(out, "json-hostile surface missing")
	}
	for _, want := range []string{
		`_M.fromJsonWalk <- function(T, rootName, text, report) {`,
		`if (r.bad || !ok) report.bodyDamaged = true;`,
		`_M.FromJson <- function(T, rootName, text, report) {`,
		`if (reason == null && report.bodyDamaged) reason = "malformed";`,
		`if (reason != null) {`,
		`c.variable ? {} : null`,
	} {
		if !strings.Contains(jsonSrc, want) {
			out = append(out, fmt.Sprintf("json runtime: missing %q", want))
		}
	}
	return out
}

const vtTextSchema = `package textcells

fixed table FixedRoot
{
    id int32 = 1 | min = 1, max = 100
    val int32
    label string(8)
}

table Node
{
    value int32
    next *Node
}

table Root
{
    id int32 = 1 | min = 1, max = 100
    tag string(16)
    head *Node
    alias *Node
}
`

const vtTextProbe = `local schema = dofile("textcells/schema_module.nut", true);
local T = schema.require("__MOD__");
local J = schema.require("schema_table_json");
local members = function(root) {
  local m = {};
  foreach (f in T.Descriptors[root].fields) m[f.name] <- f.member;
  return m;
};
local fm = members("FixedRoot");
local rm = members("Root");
local nm = members("Node");
local fresh = function() { return {}; };
local reasonOf = function(r) { return ("reason" in r) ? r.reason : "-"; };

// text-form: strict FromJson, round-trip through ToJson/FromJson, refusals.
local textOk = true;
local v = J.FromJson(T, "FixedRoot", "{\"id\":42,\"val\":3,\"label\":\"hi\"}", fresh());
if (v == null || v[fm.id] != 42 || v[fm.val] != 3 || v[fm.label] != "hi") textOk = false;
if (textOk) {
  local txt = J.ToJson(T, "FixedRoot", v);
  local v2 = txt == null ? null : J.FromJson(T, "FixedRoot", txt, fresh());
  if (v2 == null || v2[fm.id] != 42 || v2[fm.val] != 3 || v2[fm.label] != "hi") textOk = false;
}
local rOor = fresh();
if (J.FromJson(T, "FixedRoot", "{\"id\":101,\"val\":3}", rOor) != null || reasonOf(rOor) != "out_of_range") textOk = false;
local rKind = fresh();
if (J.FromJson(T, "FixedRoot", "{\"id\":\"x\",\"val\":3}", rKind) != null) textOk = false;
local rBad = fresh();
if (J.FromJson(T, "FixedRoot", "{\"id\":42,\"val\":3} junk", rBad) != null) textOk = false;
local rLab = fresh();
if (J.FromJson(T, "FixedRoot", "{\"&node\":1,\"id\":42,\"val\":3}", rLab) != null) textOk = false;
print("text-form|" + (textOk ? "ok" : "bad") + "\n");

// text-form-variable: shared/cyclic &node graph through strict FromJson.
local varOk = true;
local gvRep = fresh();
local gv = J.FromJson(T, "Root", "{\"id\":42,\"tag\":\"hi\",\"head\":{\"&node\":1,\"value\":7,\"next\":{\"&node\":2,\"value\":8,\"next\":null}},\"alias\":{\"&node\":1}}", gvRep);
if (gv == null || gv[rm.id] != 42 || gv[rm.tag] != "hi" || gv[rm.head] == null) varOk = false;
if (varOk && (gv[rm.head] != gv[rm.alias] || gv[rm.head][nm.next] == null || gv[rm.head][nm.next][nm.value] != 8 || gv[rm.head][nm.value] != 7)) varOk = false;
if (varOk) {
  local txt = J.ToJson(T, "Root", gv);
  local gv2 = txt == null ? null : J.FromJson(T, "Root", txt, fresh());
  if (gv2 == null || gv2[rm.head] == null || gv2[rm.head] != gv2[rm.alias] || gv2[rm.head][nm.next] == null || gv2[rm.head][nm.next][nm.value] != 8 || gv2[rm.head][nm.value] != 7) varOk = false;
}
local rCycle = fresh();
if (J.FromJson(T, "Root", "{\"id\":42,\"head\":{\"&node\":1,\"value\":7,\"next\":{\"&node\":2,\"value\":8,\"next\":{\"&node\":1}}},\"alias\":null}", rCycle) != null) varOk = false;
local rUndef = fresh();
if (J.FromJson(T, "Root", "{\"id\":42,\"head\":{\"&node\":1,\"value\":7,\"next\":null},\"alias\":{\"&node\":9}}", rUndef) != null) varOk = false;
local rDup = fresh();
if (J.FromJson(T, "Root", "{\"id\":42,\"head\":{\"&node\":1,\"value\":7,\"next\":null},\"alias\":{\"&node\":1,\"value\":8,\"next\":null}}", rDup) != null) varOk = false;
print("text-form-variable|" + (varOk ? "ok" : "bad") + "\n");
`

func runVtTextProbe(t *testing.T, patchJSON func(string) string) map[string]string {
	t.Helper()
	dir := t.TempDir()
	u := loadSource(t, vtTextSchema)
	mod := emitUnit(t, dir, "textcells", u)
	if patchJSON != nil {
		p := filepath.Join(dir, "textcells", "schema_table_json.nut")
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, p, []byte(patchJSON(string(b))))
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(strings.ReplaceAll(vtTextProbe, "__MOD__", mod)))
	out := sqrun.Run(t, dir, "main.nut", nil)
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		k, v, ok := strings.Cut(line, "|")
		if !ok {
			t.Fatalf("bad probe line %q", line)
		}
		got[k] = v
	}
	return got
}

func checkVtTextProbe(got map[string]string) []string {
	var fails []string
	for _, cell := range []string{"text-form", "text-form-variable"} {
		if got[cell] != "ok" {
			fails = append(fails, fmt.Sprintf("%s: got %q, want ok", cell, got[cell]))
		}
	}
	return fails
}

func TestSquirrelVtTextCells(t *testing.T) {
	t.Parallel()
	driver := squirrelDriverSource(t)
	jsonSrc := squirrelJsonRuntimeSource(t)
	if f := vtTextFindings(driver, jsonSrc); len(f) > 0 {
		t.Fatalf("the strict text read's rule is not held: %v", f)
	}
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	got := runVtTextProbe(t, nil)
	if fails := checkVtTextProbe(got); len(fails) > 0 {
		for _, f := range fails {
			t.Error(f)
		}
	}
}

func TestSquirrelVtTextCellsNegativeControl(t *testing.T) {
	t.Parallel()
	driver := squirrelDriverSource(t)
	jsonSrc := squirrelJsonRuntimeSource(t)
	if f := vtTextFindings(driver, jsonSrc); len(f) > 0 {
		t.Fatalf("unplanted already red: %v", f)
	}
	plants := []struct{ name, src, anchor, repl string }{
		{"json-read walk", driver, "J.FromJson(", "J.fromJsonWalk("},
		{"runtime damage mark", jsonSrc, "if (r.bad || !ok) report.bodyDamaged = true;", "if (false && (r.bad || !ok)) {}"},
		{"variable label map", jsonSrc, "c.variable ? {} : null", "null"},
	}
	for _, pl := range plants {
		if strings.Count(pl.src, pl.anchor) == 0 {
			t.Fatalf("anchor %q not in the real artifact", pl.anchor)
		}
		planted := strings.Replace(pl.src, pl.anchor, pl.repl, 1)
		driverP, jsonP := driver, jsonSrc
		if pl.src == driver {
			driverP = planted
		} else {
			jsonP = planted
		}
		if f := vtTextFindings(driverP, jsonP); len(f) == 0 {
			t.Fatalf("NEGATIVE CONTROL FAILED: planting %s left the strict text read's rule green", pl.name)
		}
	}

	slowtest.Gate(t, "the pinned Squirrel interpreter")
	if fails := checkVtTextProbe(runVtTextProbe(t, nil)); len(fails) > 0 {
		t.Fatalf("unplanted probe already red: %v", fails)
	}
	// Interpreter-level plants in a copy of runtime/json.nut (emitted as
	// schema_table_json.nut): each must make its strict-read cell go red.
	runtimePlants := []struct {
		cell, anchor, replacement string
	}{
		{"text-form", "if (r.bad || !ok) report.bodyDamaged = true;", "if (false && (r.bad || !ok)) {}"},
		{"text-form-variable", "c.variable ? {} : null", "null"},
	}
	for _, rp := range runtimePlants {
		got := runVtTextProbe(t, func(src string) string {
			if strings.Count(src, rp.anchor) != 1 {
				t.Fatalf("%s: anchor %q count != 1 in emitted schema_table_json.nut", rp.cell, rp.anchor)
			}
			return strings.Replace(src, rp.anchor, rp.replacement, 1)
		})
		fails := checkVtTextProbe(got)
		found := false
		for _, f := range fails {
			if strings.HasPrefix(f, rp.cell+":") {
				found = true
			}
		}
		if !found {
			t.Fatalf("NEGATIVE CONTROL FAILED: planting fault in a copy of runtime/json.nut (%s) left %s green: fails=%v got=%v", rp.anchor, rp.cell, fails, got)
		}
	}
}
