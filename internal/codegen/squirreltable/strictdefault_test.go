package squirreltable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

// strictDefaultPrelude is the strict-by-default probe (docs/SPEC-TABLES.md
// §6.5): for one input it runs the plain name and the Tolerant twin, printing
// whether each answered a value, the plain name's reason and field, and the
// twin's §4 counters. A fixed read also prints the count and FixedReason each
// name answered.
const strictDefaultPrelude = `local inputs = dofile("inputs.nut", true);
local fresh = function() {
  return { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0,
    malformed = false, refused = false, reason = null, field = null };
};
local nn = function(x) { return x == null ? "-" : x; };
local counters = function(r) {
  return r.unknown + "," + r.kindMismatch + "," + r.widened + "," + r.clamped + "," +
    r.duplicate + "," + (r.malformed ? "true" : "false");
};
local runLoad = function(unit, mod, root, key) {
  local schema = dofile(unit + "/schema_module.nut", true);
  local T = schema.require(mod);
  local L = schema.require("schema_table_load");
  local pr = fresh(); local pv = L.load(T, root, inputs[key], pr);
  local tr = fresh(); local tv = L.loadTolerant(T, root, inputs[key], tr);
  print("load|" + key + "|" + (pv == null ? "null" : "val") + "|" + nn(pr.reason) + "|" +
        (tv == null ? "null" : "val") + "|" + counters(tr) + "\n");
};
local runJson = function(unit, mod, root, key) {
  local schema = dofile(unit + "/schema_module.nut", true);
  local T = schema.require(mod);
  local J = schema.require("schema_table_json");
  local S = schema.require("schema_table_save");
  local pr = fresh(); local pv = J.FromJson(T, root, inputs[key], pr);
  local tr = fresh(); local tv = J.FromJsonTolerant(T, root, inputs[key], tr);
  local tb = tv == null ? "null" : (S.save(T, root, tv) == null ? "refused" : "saved");
  print("json|" + key + "|" + (pv == null ? "null" : "val") + "|" + nn(pr.reason) + "|" +
        nn(pr.field) + "|" + counters(tr) + "|" + tb + "\n");
};
local fixedOne = function(T, root, b, out) {
  local n = T[root + "FixedLoad"](b, out, null);
  local r = T.FixedReason;
  local out2 = [T.Descriptors[root].make()];
  local n2 = T[root + "FixedLoadTolerant"](b, out2, null);
  local r2 = T.FixedReason;
  return [n, r, n2, r2];
};
local runFixed = function(unit, mod, root, key) {
  local schema = dofile(unit + "/schema_module.nut", true);
  local T = schema.require(mod);
  local out = [T.Descriptors[root].make()];
  local a = fixedOne(T, root, inputs[key], out);
  print("fixed|" + key + "|" + a[0] + "|" + nn(a[1]) + "|" + a[2] + "|" + nn(a[3]) + "\n");
};
local runFixedClean = function(unit, mod, root, key) {
  local schema = dofile(unit + "/schema_module.nut", true);
  local T = schema.require(mod);
  local rec = T.Descriptors[root].make();
  local b = blob(256);
  local n = T[root + "FixedSave"](b, [rec]);
  local wire = blob(n);
  for (local i = 0; i < n; i++) wire[i] = b[i];
  local out = [T.Descriptors[root].make()];
  local a = fixedOne(T, root, wire, out);
  print("fixed|" + key + "|" + a[0] + "|" + nn(a[1]) + "|" + a[2] + "|" + nn(a[3]) + "\n");
};
`

// TestSquirrelDefaultReadIsStrict holds docs/SPEC-TABLES.md §6.5's rule for
// the Squirrel leg: every read a caller reaches by the plain name refuses
// whatever ReadStrict refuses, leaves the reason and field in the report and
// answers null (FixedLoad -1); the old tolerant answer lives only under the
// explicit *Tolerant twin. It drives the plain load over every report row of
// testdata/conformance/tables/reports.txt, FromJson over every corpus text
// row, and FixedLoad over a forged fixed file beside a clean one.
func TestSquirrelDefaultReadIsStrict(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")
	rows := readReportRows(t, manifest)
	jsonRows := readJsonRows(t, manifest)
	pinned := readReportCounts(t, filepath.Join(root, "testdata", "conformance", "tables", "reports.txt"))

	paths := map[string][]string{}
	for _, u := range manifestUnits(t, manifest) {
		paths[u.key] = u.paths
	}
	dir := t.TempDir()
	mods := map[string]string{}
	refused := map[string]bool{}
	ensure := func(key string) bool {
		if _, ok := mods[key]; ok {
			return true
		}
		if refused[key] {
			return false
		}
		ps, ok := paths[key]
		if !ok {
			t.Fatalf("a corpus row names unit %q, which the manifest does not declare", key)
		}
		u := loadUnit(t, root, ps)
		files, err := squirrel.Generate(u)
		if err != nil {
			refused[key] = true
			return false
		}
		tables, err := squirreltable.Generate(u)
		if err != nil {
			t.Logf("refused %s: %v", key, err)
			refused[key] = true
			return false
		}
		outDir := filepath.Join(dir, key)
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			writeFile(t, filepath.Join(outDir, name), data)
		}
		for name, data := range tables {
			writeFile(t, filepath.Join(outDir, name), data)
		}
		mods[key] = squirreltable.ModuleName(u)
		return true
	}

	var p strings.Builder
	p.WriteString(strictDefaultPrelude)
	inputs := map[string][]byte{}
	readInput := func(rel string) []byte {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}

	type loadCase struct{ key, unit, root, want string }
	var loadCases []loadCase
	for _, r := range rows {
		if !ensure(r.unit) {
			continue
		}
		pin, ok := pinned[r.name]
		if !ok {
			t.Fatalf("report row %s has no pinned report", r.name)
		}
		inputs[r.name] = readInput(r.wire)
		want := strictName(t, r.name, pin)
		if len(inputs[r.name]) == 0 {
			want = "wire_damaged"
		} else if f := formName(inputs[r.name][0], false); f != "" {
			want = f
		}
		fmt.Fprintf(&p, "runLoad(%q, %q, %q, %q);\n", r.unit, mods[r.unit], r.root, r.name)
		loadCases = append(loadCases, loadCase{key: r.name, unit: r.unit, root: r.root, want: want})
	}

	type jsonCase struct{ key, unit, root, hostile, verdict string }
	var jsonCases []jsonCase
	for _, in := range jsonRows.instances {
		if in.noText || !ensure(in.unit) {
			continue
		}
		key := "inst:" + in.name
		inputs[key] = readInput("testdata/conformance/tables/json/" + in.name + ".json")
		fmt.Fprintf(&p, "runJson(%q, %q, %q, %q);\n", in.unit, mods[in.unit], in.root, key)
		jsonCases = append(jsonCases, jsonCase{key: key, unit: in.unit, root: in.root})
	}
	for _, h := range jsonRows.hostile {
		if !ensure(h.unit) {
			continue
		}
		key := "host:" + h.name
		inputs[key] = readInput(h.tree + "/" + h.root + ".json")
		fmt.Fprintf(&p, "runJson(%q, %q, %q, %q);\n", h.unit, mods[h.unit], h.root, key)
		jsonCases = append(jsonCases, jsonCase{key: key, unit: h.unit, root: h.root, hostile: h.name, verdict: h.verdict})
	}

	// A fixed root of this card's own: the fixed reader carries no report, so
	// its plain and Tolerant names are compared by count and FixedReason.
	fixedUnit := loadSource(t, `package strictfix
fixed table Fix { n int32 = 1 | min = 0, max = 10
 m int32 = 2 }
`)
	fixedMod := emitUnit(t, dir, "strictfix", fixedUnit)
	fixedInputs := map[string][]byte{}
	for _, c := range []struct {
		key  string
		data []byte
	}{
		{"empty", []byte{}},
		{"form1", []byte{1}},
		{"form2", []byte{2}},
		{"form9", []byte{9}},
		{"form3_short", []byte{3}},
	} {
		fixedInputs[c.key] = c.data
	}
	// the clean file and one byte short of it are built from the unit's own
	// writer inside the probe, so the test needs no fixed layout here.
	for _, key := range []string{"empty", "form1", "form2", "form9", "form3_short"} {
		inputs[key+"@fix"] = fixedInputs[key]
		fmt.Fprintf(&p, "runFixed(%q, %q, %q, %q);\n", "strictfix", fixedMod, "Fix", key+"@fix")
	}
	fmt.Fprintf(&p, "runFixedClean(%q, %q, %q, %q);\n", "strictfix", fixedMod, "Fix", "clean@fix")

	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	out := string(sqrun.Run(t, dir, "main.nut", inputs))
	got := map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "|")
		if len(f) < 2 {
			t.Fatalf("probe line %q is not a kind and a key", line)
		}
		got[f[1]] = f[2:]
	}

	loads, jsons, fixeds := 0, 0, 0
	loadNames := map[string]int{}
	for _, c := range loadCases {
		g, ok := got[c.key]
		if !ok || len(g) != 4 {
			t.Fatalf("load %s: the probe printed no row", c.key)
		}
		plainValue, reason, twinValue, twinCounters := g[0], g[1], g[2], g[3]
		pin := pinned[c.key]
		wantCounters := strings.Join(strings.Split(pin, ",")[:6], ",")
		if twinCounters != wantCounters {
			t.Errorf("load %s: loadTolerant counted %s, the pinned report is %s", c.key, twinCounters, wantCounters)
		}
		loadNames[reason]++
		if c.want == "ok" {
			if plainValue != "val" || reason != "-" {
				t.Errorf("load %s: the plain load answered %s/%s, want the value", c.key, plainValue, reason)
			}
			if twinValue != "val" {
				t.Errorf("load %s: loadTolerant lost the clean value", c.key)
			}
		} else {
			if plainValue != "null" {
				t.Errorf("load %s: the plain load handed back a value on %s", c.key, c.want)
			}
			if reason != c.want {
				t.Errorf("load %s: the plain load refused %q, ReadStrict names %q", c.key, reason, c.want)
			}
			if c.want != "wire_damaged" && c.want != "newer_form" && c.want != "message_form_as_file" && twinValue != "val" {
				t.Errorf("load %s: loadTolerant did not keep the recovered value", c.key)
			}
		}
		loads++
	}

	jsonStrict := map[string]int{}
	for _, c := range jsonCases {
		g, ok := got[c.key]
		if !ok || len(g) != 5 {
			t.Fatalf("json %s: the probe printed no row", c.key)
		}
		plainValue, reason, field, twinCounters, twinSaved := g[0], g[1], g[2], g[3], g[4]
		if c.verdict == "" {
			// an instance row's text is clean: both names take it, and the
			// tolerant read still saves the corpus wire.
			if plainValue != "val" || reason != "-" {
				t.Errorf("%s: the plain FromJson refused a clean corpus text as %s/%s", c.key, plainValue, reason)
			}
			if twinSaved != "saved" {
				t.Errorf("%s: FromJsonTolerant did not save the corpus text", c.key)
			}
			if twinCounters != "0,0,0,0,0,false" {
				t.Errorf("%s: FromJsonTolerant counted %s on a clean text", c.key, twinCounters)
			}
			jsons++
			continue
		}
		if c.verdict == "refused" {
			if plainValue != "null" || reason == "-" {
				t.Errorf("%s: the plain FromJson answered %s/%s on a refused hostile row", c.key, plainValue, reason)
			}
			if reason == "missing_required" && field == "-" {
				t.Errorf("%s: missing_required named no field", c.key)
			}
			jsonStrict[reason]++
			jsons++
			continue
		}
		var want string
		f := strings.Split(c.verdict, ",")
		if len(f) != 7 {
			t.Fatalf("%s: hostile verdict %q is not u,k,w,c,d,m,verdict", c.key, c.verdict)
		}
		switch f[5] {
		case "true":
			want = "malformed"
		default:
			if dc, _ := strconv.Atoi(f[3]); dc > 0 {
				want = "clamped"
			} else if k, _ := strconv.Atoi(f[1]); k > 0 {
				want = "kind_mismatch"
			}
		}
		if want == "clamped" || want == "kind_mismatch" || want == "malformed" {
			if plainValue != "null" || reason != want {
				t.Errorf("%s: the plain FromJson answered %s/%s, want a %s refusal", c.key, plainValue, reason, want)
			}
			jsonStrict[reason]++
		} else if plainValue != "val" || reason != "-" {
			// unknown or widened alone are evolution, not loss (§6.5)
			t.Errorf("%s: the plain FromJson refused %s/%s on an accepted row", c.key, plainValue, reason)
		}
		jsons++
	}
	if len(jsonStrict) == 0 {
		t.Error("no hostile corpus row made the plain FromJson refuse")
	}

	fixedNames := map[string]int{}
	for _, key := range []string{"empty", "form1", "form2", "form9", "form3_short"} {
		g, ok := got[key+"@fix"]
		if !ok || len(g) != 4 {
			t.Fatalf("fixed %s: the probe printed no row", key)
		}
		if g[0] != "-1" {
			t.Errorf("fixed %s: the plain FixedLoad answered %s, want -1", key, g[0])
		}
		if g[0] != g[2] || g[1] != g[3] {
			t.Errorf("fixed %s: plain %s/%s and Tolerant %s/%s disagree", key, g[0], g[1], g[2], g[3])
		}
		fixedNames[g[1]]++
		fixeds++
	}
	if g, ok := got["clean@fix"]; !ok || len(g) != 4 {
		t.Fatal("fixed clean: the probe printed no row")
	} else if g[0] != "1" || g[2] != "1" {
		t.Errorf("fixed clean: plain %s and Tolerant %s, want 1 and 1", g[0], g[2])
	}
	fixeds++

	if loads == 0 || jsons == 0 || fixeds == 0 {
		t.Fatalf("cases: load %d, json %d, fixed %d; every entry point must be reached", loads, jsons, fixeds)
	}
	t.Logf("plain-vs-tolerant cases: load %d (%v), FromJson %d (%v), FixedLoad %d (%v)",
		loads, loadNames, jsons, jsonStrict, fixeds, fixedNames)
}
