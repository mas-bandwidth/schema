package squirreltable_test

// Functional test (runs the pinned Squirrel interpreter via sqrun.Run):
// STRICT-100 gate for the eight Squirrel variable-table pointer, union, array,
// and id-table cells in docs/roadmap-evidence/squirrel.sexp:
//   - port/squirrel/vt/variable-class
//   - port/squirrel/vt/union-table-arms
//   - port/squirrel/vt/pointer-arrays
//   - port/squirrel/vt/union-arrays
//   - port/squirrel/vt/optional-arrays
//   - port/squirrel/vt/union-arms-any
//   - port/squirrel/vt/id-table-wire
//   - port/squirrel/vt/unbounded-arrays
//
// Every case reads through schema_table_load.load (ReadStrict: one read, the
// whole value or a refusal) and writes back through schema_table_save.save and
// measure. Negative controls plant real faults in the emitted load, refusal,
// and save runtimes on disk and require every cell to go red.

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type vtPointerCellRow struct {
	name       string
	cases      []string
	constructs []string
}

var vtPointerCellTable = []vtPointerCellRow{
	{
		name: "variable-class",
		cases: []string{
			"graph_tree", "graph_shared", "graph_empty", "graph_deep",
			"guard_false_no_edge", "guard_true_both_edges",
			"list_shared", "list_before_pointer",
		},
		constructs: []string{"blobs_pointers"},
	},
	{
		name: "union-table-arms",
		cases: []string{
			"m1_open", "m1_save", "m2_close", "m2_open",
			"m1_open_as_m2", "m1_save_as_m2", "m2_close_as_m1", "m2_open_as_m1",
			"message_arms", "message_arm_edits",
		},
		constructs: []string{"unions_maps_keyed"},
	},
	{
		name: "pointer-arrays",
		cases: []string{
			"stream_parts", "stream_chain",
			"parts_elem_kind", "parts_count_past_bound", "parts_index_out_of_range",
			"w1_fleet", "w2_fleet",
		},
		constructs: []string{"blobs_pointers"},
	},
	{
		name: "union-arrays",
		cases: []string{
			"message_batch", "message_arm_edits",
			"batch_elem_kind", "batch_unknown_arm",
		},
		constructs: []string{"unions_maps_keyed"},
	},
	{
		name: "optional-arrays",
		cases: []string{
			"message_trace", "message_save", "message_empty",
			"trace_elem_kind", "trace_count_past_bound", "marks_unknown_variant",
		},
	},
	{
		name: "union-arms-any",
		cases: []string{
			"stream_arm_first", "stream_arm_pointer", "stream_arm_tag",
			"a2_arm_enum_as_int", "a2_arm_pointer_as_int", "a2_arm_bytes_as_string", "a2_arm_length_off_width",
			"a1_arm_moved_width_as_a2", "a1_arm_one_width_as_a2",
			"a1_arm_as_string_as_a2", "a1_arm_as_body_as_a2", "a1_arm_body_as_a2", "a1_arm_unknown_as_a2",
			"nested_arm_trailing", "escape_arm",
		},
		constructs: []string{"unions_maps_keyed"},
	},
	{
		name: "id-table-wire",
		cases: []string{
			"vocab_wide", "vocab_low", "peer_a", "peer_b",
			"vocab9_wide", "vocab9_low", "login_full", "match_full",
			"escape_unnamed_field", "escape_named_field", "escape_element", "nopayload_named_field",
		},
	},
	{
		name: "unbounded-arrays",
		cases: []string{
			"list_tables", "list_scalars", "list_empty", "list_erased",
			"list_mixed", "list_shared", "list_nested", "list_of_maps",
			"list_migrates", "list_scalar_widen",
		},
		constructs: []string{"blobs_pointers"},
	},
}

func vtPointerFindings(loadSrc, saveSrc, refusalSrc string) []string {
	var out []string
	checks := []struct{ src, name, want string }{
		{loadSrc, "load delegates to ReadStrict", "return R.ReadStrict(T, rootName, bytes, report);"},
		{loadSrc, "load id-table bound check", "if (count < 0 || count > (bytes.len() - 9) / 8 || count > ID_TABLE_BOUND)"},
		{loadSrc, "load id-table distinctness", "if (id in seen) { report.bodyDamaged = true; return retainClear(retain, report, \"wire_damaged\"); }"},
		{loadSrc, "load cycle check", `if (_require("schema_table_save").cyclic(T, rootName, v, verts)) report.bodyDamaged = true;`},
		{loadSrc, "load union arm payload framing", "readArmPayload = function(arm, sub, lpos, wireKind, T, table, inst, report, st)"},
		{saveSrc, "save node deduplication", "if (found >= 0) continue;"},
		{saveSrc, "save id-table trailer", "foreach (id in ids.list) wfixed(w, id, 8);\n  wfixed(w, ids.list.len(), 8);"},
		{refusalSrc, "refusal strict entry point", "_M.ReadStrict <- function(T, root, bytes, report)"},
	}
	for _, c := range checks {
		if !strings.Contains(c.src, c.want) {
			out = append(out, fmt.Sprintf("%s: missing %q", c.name, c.want))
		}
	}
	return out
}

func vtPointerSources(t *testing.T) (string, string, string) {
	t.Helper()
	return squirrelRepoFile(t, "internal", "codegen", "squirreltable", "runtime", "load.nut"),
		squirrelRepoFile(t, "internal", "codegen", "squirreltable", "runtime", "save.nut"),
		squirrelRepoFile(t, "internal", "codegen", "squirreltable", "runtime", "refusal.nut")
}

type vtPointerConstruct struct {
	name   string
	schema string
	root   string
	json   string
}

func vtPointerConstructs() map[string]vtPointerConstruct {
	return map[string]vtPointerConstruct{
		"blobs_pointers": {
			name:   "blobs_pointers",
			schema: "package probe\ntable Node\n{\n    name *string\n    data *bytes\n    next *Node\n    kids []*Node\n}\n",
			root:   "Node",
			json:   `{"name":"root","data":"AQID","next":{"name":"n1"},"kids":[{"name":"k1"},null,{"name":"k2"}]}`,
		},
		"unions_maps_keyed": {
			name:   "unions_maps_keyed",
			schema: "package probe\nenum Slot { head, body, feet }\ntable Item\n{\n    w uint16\n}\nunion Pick\n{\n    item Item\n    count uint32\n    label string(8)\n    empty\n}\ntable Kit\n{\n    gear [Slot]Item\n    pick Pick\n    picks []Pick\n    byid map[uint32]Item\n    flags_ok bool\n}\n",
			root:   "Kit",
			json:   `{"gear":{"head":{"w":1},"feet":{"w":3}},"pick":{"label":"x"},"picks":[{"count":4},{"empty":null},{"item":{"w":2}}],"byid":{"9":{"w":9},"2":{"w":2}},"flags_ok":true}`,
		},
	}
}

func vtPointerEngineWire(t *testing.T, u *ir.Unit, root, text string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	st := u.Tables[root]
	if st == nil {
		t.Fatalf("no table %s", root)
	}
	inst := m.New(st)
	var rep tabletext.Report
	if !m.Read(inst, []byte(text), &rep) || rep.Refused || rep.Unknown > 0 {
		t.Fatalf("the Go engine does not read the JSON cleanly: %+v", rep)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the Go engine does not write it: %v", err)
	}
	return wire
}

const vtPointerPrelude = `local inputs = dofile("inputs.nut", true);
local HEXB = array(256);
for (local i = 0; i < 256; i++) HEXB[i] = format("%02x", i);
local hexOf = function(b) {
  if (b == null) return "null";
  local s = "";
  for (local i = 0; i < b.len(); i++) s += HEXB[b[i] & 0xff];
  return s;
};
local MODS = {};
local getMod = function(unit, mod) {
  if (!(unit in MODS)) MODS[unit] <- dofile(unit + "/schema_module.nut", true);
  local schema = MODS[unit];
  return {
    T = schema.require(mod),
    L = schema.require("schema_table_load"),
    S = schema.require("schema_table_save")
  };
};
local runInst = function(unit, mod, root, key) {
  local m = getMod(unit, mod);
  local d = m.T.Descriptors[root];
  local wasFixed = d.fixed;
  local wire = inputs[key];
  if (wasFixed && wire.len() > 0 && (wire[0] & 0xff) == 1) d.fixed = false;
  local r = {};
  local v = m.L.load(m.T, root, wire, r);
  if (v == null) {
    d.fixed = wasFixed;
    print("I|" + key + "|refused|-1|null\n");
    return;
  }
  local b = m.S.save(m.T, root, v);
  local len = m.S.measure(m.T, root, v);
  d.fixed = wasFixed;
  if (b == null) {
    print("I|" + key + "|save_null|" + len.tostring() + "|null\n");
    return;
  }
  print("I|" + key + "|read|" + len.tostring() + "|" + hexOf(b) + "\n");
};
local runReport = function(unit, mod, root, key) {
  local m = getMod(unit, mod);
  local r = {};
  local v = m.L.load(m.T, root, inputs[key], r);
  print("R|" + key + "|" + (v == null ? "refused" : "read") + "\n");
};
local runConstruct = function(unit, mod, root, key) {
  local m = getMod(unit, mod);
  local r = {};
  local v = m.L.load(m.T, root, inputs[key], r);
  if (v == null) {
    print("C|" + key + "|refused|-1|null\n");
    return;
  }
  local b = m.S.save(m.T, root, v);
  local len = m.S.measure(m.T, root, v);
  print("C|" + key + "|read|" + len.tostring() + "|" + hexOf(b) + "\n");
};
`

type vtPointerResults struct {
	cellFailures map[string][]string
}

func runVtPointerCellsSuite(t *testing.T, patch map[string]func(string) string) vtPointerResults {
	t.Helper()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")
	reportsPath := filepath.Join(root, "testdata", "conformance", "tables", "reports.txt")

	units := manifestUnits(t, manifestPath)
	unitPaths := map[string][]string{}
	for _, u := range units {
		unitPaths[u.key] = u.paths
	}
	instances := readInstanceRows(t, manifestPath)
	instByName := map[string]instanceRow{}
	for _, in := range instances {
		instByName[in.name] = in
	}
	reports := readReportRows(t, manifestPath)
	repByName := map[string]reportRow{}
	for _, r := range reports {
		repByName[r.name] = r
	}
	pinnedReports := readReportCounts(t, reportsPath)
	constructs := vtPointerConstructs()

	neededUnits := map[string]*ir.Unit{}
	var unitOrder []string
	needUnit := func(key string) {
		if _, ok := neededUnits[key]; ok {
			return
		}
		paths, ok := unitPaths[key]
		if !ok {
			t.Fatalf("manifest declares no unit %q", key)
		}
		neededUnits[key] = loadUnit(t, root, paths)
		unitOrder = append(unitOrder, key)
	}

	neededInsts := map[string]bool{}
	neededReps := map[string]bool{}
	neededConstructs := map[string]bool{}
	for _, cell := range vtPointerCellTable {
		for _, c := range cell.cases {
			found := false
			if in, ok := instByName[c]; ok {
				neededInsts[c] = true
				needUnit(in.unit)
				found = true
			}
			if r, ok := repByName[c]; ok {
				neededReps[c] = true
				needUnit(r.unit)
				found = true
			}
			if !found {
				t.Fatalf("cell %s: case %q is in neither instance nor report rows", cell.name, c)
			}
		}
		for _, k := range cell.constructs {
			if _, ok := constructs[k]; !ok {
				t.Fatalf("cell %s: unknown construct %q", cell.name, k)
			}
			neededConstructs[k] = true
		}
	}

	dir := t.TempDir()
	mods := map[string]string{}
	emitAndPatch := func(key string, u *ir.Unit) {
		files, err := squirrel.Generate(u)
		if err != nil {
			t.Fatalf("unit %s: squirrel.Generate: %v", key, err)
		}
		tables, err := squirreltable.Generate(u)
		if err != nil {
			t.Fatalf("unit %s: squirreltable.Generate: %v", key, err)
		}
		out := filepath.Join(dir, key)
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			writeFile(t, filepath.Join(out, name), data)
		}
		for name, data := range tables {
			s := string(data)
			if fn, ok := patch[name]; ok {
				s = fn(s)
			}
			writeFile(t, filepath.Join(out, name), []byte(s))
		}
		mods[key] = squirreltable.ModuleName(u)
	}

	for _, key := range unitOrder {
		emitAndPatch(key, neededUnits[key])
	}

	var script strings.Builder
	script.WriteString(vtPointerPrelude)
	inputs := map[string][]byte{}
	instWires := map[string][]byte{}
	for name := range neededInsts {
		in := instByName[name]
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(in.wire)))
		if err != nil {
			t.Fatal(err)
		}
		key := "i_" + name
		inputs[key] = data
		instWires[name] = data
		fmt.Fprintf(&script, "runInst(%q, %q, %q, %q);\n", in.unit, mods[in.unit], in.root, key)
	}
	for name := range neededReps {
		r := repByName[name]
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(r.wire)))
		if err != nil {
			t.Fatal(err)
		}
		key := "r_" + name
		inputs[key] = data
		fmt.Fprintf(&script, "runReport(%q, %q, %q, %q);\n", r.unit, mods[r.unit], r.root, key)
	}

	constructWires := map[string][]byte{}
	for name := range neededConstructs {
		con := constructs[name]
		u := loadSource(t, con.schema)
		unitKey := "con_" + name
		emitAndPatch(unitKey, u)

		wire := vtPointerEngineWire(t, u, con.root, con.json)
		constructWires[name] = wire
		key := "c_" + name
		inputs[key] = wire
		fmt.Fprintf(&script, "runConstruct(%q, %q, %q, %q);\n", unitKey, mods[unitKey], con.root, key)
	}

	writeFile(t, filepath.Join(dir, "main.nut"), []byte(script.String()))
	out := sqrun.Run(t, dir, "main.nut", inputs)

	gotInst := map[string][]string{}
	gotRep := map[string]string{}
	gotCon := map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		switch parts[0] {
		case "I":
			if len(parts) != 5 {
				t.Fatalf("malformed instance line %q", line)
			}
			gotInst[strings.TrimPrefix(parts[1], "i_")] = parts[2:]
		case "R":
			if len(parts) != 3 {
				t.Fatalf("malformed report line %q", line)
			}
			gotRep[strings.TrimPrefix(parts[1], "r_")] = parts[2]
		case "C":
			if len(parts) != 5 {
				t.Fatalf("malformed construct line %q", line)
			}
			gotCon[strings.TrimPrefix(parts[1], "c_")] = parts[2:]
		default:
			t.Fatalf("unexpected probe line %q", line)
		}
	}

	checkInst := func(name string) string {
		in := instByName[name]
		res, ok := gotInst[name]
		if !ok {
			return fmt.Sprintf("instance %s: no probe output", name)
		}
		verdict, mStr, hexStr := res[0], res[1], res[2]
		if in.goRefusal != "" {
			if verdict != "refused" && verdict != "save_null" {
				return fmt.Sprintf("instance %s (goRefusal=%s): got %v, want refused", name, in.goRefusal, res)
			}
			return ""
		}
		if verdict != "read" {
			return fmt.Sprintf("instance %s: strict read/save returned %s, want read", name, verdict)
		}
		have, err := hex.DecodeString(hexStr)
		if err != nil {
			return fmt.Sprintf("instance %s: bad hex: %v", name, err)
		}
		want := instWires[name]
		if fmt.Sprintf("%d", len(have)) != mStr {
			return fmt.Sprintf("instance %s: measure %s != len %d", name, mStr, len(have))
		}
		if !bytes.Equal(have, want) {
			return fmt.Sprintf("instance %s: saved bytes != wire (%d vs %d bytes)", name, len(have), len(want))
		}
		return ""
	}

	checkRep := func(name string) string {
		verdict, ok := gotRep[name]
		if !ok {
			return fmt.Sprintf("report %s: no probe output", name)
		}
		want := reportVerdict(pinnedReports[name])
		if verdict != want {
			return fmt.Sprintf("report %s: got verdict %q, reports.txt pins %q", name, verdict, want)
		}
		return ""
	}

	checkCon := func(name string) string {
		res, ok := gotCon[name]
		if !ok {
			return fmt.Sprintf("construct %s: no probe output", name)
		}
		verdict, mStr, hexStr := res[0], res[1], res[2]
		if verdict != "read" {
			return fmt.Sprintf("construct %s: got %s, want read", name, verdict)
		}
		have, err := hex.DecodeString(hexStr)
		if err != nil {
			return fmt.Sprintf("construct %s: bad hex: %v", name, err)
		}
		want := constructWires[name]
		if fmt.Sprintf("%d", len(have)) != mStr {
			return fmt.Sprintf("construct %s: measure %s != len %d", name, mStr, len(have))
		}
		if !bytes.Equal(have, want) {
			return fmt.Sprintf("construct %s: saved bytes != engine wire", name)
		}
		return ""
	}

	cellFailures := map[string][]string{}
	for _, cell := range vtPointerCellTable {
		var fails []string
		for _, c := range cell.cases {
			if _, ok := instByName[c]; ok {
				if why := checkInst(c); why != "" {
					fails = append(fails, why)
				}
			}
			if _, ok := repByName[c]; ok {
				if why := checkRep(c); why != "" {
					fails = append(fails, why)
				}
			}
		}
		for _, k := range cell.constructs {
			if why := checkCon(k); why != "" {
				fails = append(fails, why)
			}
		}
		cellFailures[cell.name] = fails
	}
	return vtPointerResults{cellFailures: cellFailures}
}

func TestSquirrelVtPointerCells(t *testing.T) {
	t.Parallel()
	loadSrc, saveSrc, refusalSrc := vtPointerSources(t)
	if f := vtPointerFindings(loadSrc, saveSrc, refusalSrc); len(f) > 0 {
		t.Fatalf("source gate failed: %v", f)
	}
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	res := runVtPointerCellsSuite(t, nil)
	for _, cell := range vtPointerCellTable {
		t.Run(cell.name, func(t *testing.T) {
			for _, fail := range res.cellFailures[cell.name] {
				t.Error(fail)
			}
		})
	}
}

func TestSquirrelVtPointerCellsNegativeControl(t *testing.T) {
	t.Parallel()
	loadSrc, saveSrc, refusalSrc := vtPointerSources(t)
	if f := vtPointerFindings(loadSrc, saveSrc, refusalSrc); len(f) > 0 {
		t.Fatalf("unplanted source gate already red: %v", f)
	}
	plants := []struct {
		name   string
		target string
		anchor string
	}{
		{"load ReadStrict delegation", "load", "return R.ReadStrict(T, rootName, bytes, report);"},
		{"load id-table distinctness", "load", "if (id in seen) { report.bodyDamaged = true; return retainClear(retain, report, \"wire_damaged\"); }"},
		{"save node deduplication", "save", "if (found >= 0) continue;"},
		{"refusal ReadStrict entry", "refusal", "_M.ReadStrict <- function(T, root, bytes, report)"},
	}
	for _, pl := range plants {
		l, s, r := loadSrc, saveSrc, refusalSrc
		switch pl.target {
		case "load":
			l = strings.Replace(l, pl.anchor, "// SABOTAGED", 1)
		case "save":
			s = strings.Replace(s, pl.anchor, "// SABOTAGED", 1)
		case "refusal":
			r = strings.Replace(r, pl.anchor, "// SABOTAGED", 1)
		}
		if f := vtPointerFindings(l, s, r); len(f) == 0 {
			t.Fatalf("NEGATIVE CONTROL FAILED: planting %s left source gate green", pl.name)
		}
	}

	slowtest.Gate(t, "the pinned Squirrel interpreter")

	t.Run("sabotage-load", func(t *testing.T) {
		const loadAnchor = "    table.append(id);\n"
		const refAnchor = "    if (n != 1) { report.bodyDamaged = true; report.reason = \"wire_damaged\"; return null; }\n"
		res := runVtPointerCellsSuite(t, map[string]func(string) string{
			"schema_table_load.nut": func(src string) string {
				if strings.Count(src, loadAnchor) != 1 {
					t.Fatalf("load anchor count = %d, want 1", strings.Count(src, loadAnchor))
				}
				return strings.Replace(src, loadAnchor, "    table.append(id ^ 1);\n", 1)
			},
			"schema_table_refusal.nut": func(src string) string {
				if strings.Count(src, refAnchor) != 1 {
					t.Fatalf("refusal anchor count = %d, want 1", strings.Count(src, refAnchor))
				}
				return strings.Replace(src, refAnchor, "    if (n == 1) { report.bodyDamaged = true; report.reason = \"wire_damaged\"; return null; }\n", 1)
			},
		})
		for _, cell := range vtPointerCellTable {
			if len(res.cellFailures[cell.name]) == 0 {
				t.Errorf("NEGATIVE CONTROL FAILED: cell %s stayed green when load/refusal runtime was sabotaged", cell.name)
			}
		}
	})

	t.Run("sabotage-save", func(t *testing.T) {
		const varAnchor = "  wfixed(w, ids.list.len(), 8);\n"
		const fixAnchor = "  putFixedU(out, rec, 8, c.lhash);\n"
		res := runVtPointerCellsSuite(t, map[string]func(string) string{
			"schema_table_save.nut": func(src string) string {
				if strings.Count(src, varAnchor) != 1 {
					t.Fatalf("save varAnchor count = %d, want 1", strings.Count(src, varAnchor))
				}
				if strings.Count(src, fixAnchor) != 1 {
					t.Fatalf("save fixAnchor count = %d, want 1", strings.Count(src, fixAnchor))
				}
				s := strings.Replace(src, varAnchor, "  wfixed(w, ids.list.len() + 1, 8);\n", 1)
				return strings.Replace(s, fixAnchor, "  putFixedU(out, rec, 8, c.lhash ^ 1);\n", 1)
			},
		})
		for _, cell := range vtPointerCellTable {
			if len(res.cellFailures[cell.name]) == 0 {
				t.Errorf("NEGATIVE CONTROL FAILED: cell %s stayed green when schema_table_save.nut was sabotaged", cell.name)
			}
		}
	})
}
