package luautable_test

// The variable-table pointer cells (ROADMAP port/luau/vt: variable-class,
// union-table-arms, pointer-arrays, union-arrays, optional-arrays,
// union-arms-any). One subtest per row. Each corpus wire is read by the
// strict load (schema_load.luau load, which is ReadStrict: the whole value
// or a refusal) and, when the wire is readable, saved back through the
// emitted save. A report row's verdict is reports.txt. A Go-engine construct
// is the same round trip of a wire the engine wrote from JSON.
//
// The wire-alone refusals node_index_null, node_table_twice, and the inert
// bodies are luau-conformance-read-then-write's harness, not these rows.
// This file does not touch that harness.
//
// Functional: the pinned Luau interpreter. SCHEMA_SLOW=1, or slowtest.Gate skips.

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type luauVTCell struct {
	row        string
	cases      []string
	constructs []string
}

// The rows and their corpus are the Lua leg's
// (internal/codegen/luatable/vtcells_test.go), narrowed to these six cells.
var luauVTCells = []luauVTCell{
	{"variable-class", []string{"graph_tree", "graph_shared", "graph_empty", "graph_deep", "guard_false_no_edge", "guard_true_both_edges",
		"list_shared", "list_before_pointer"}, []string{"blobs_pointers"}},
	{"union-table-arms", []string{"m1_open", "m1_save", "m2_close", "m2_open", "m1_open_as_m2", "m1_save_as_m2", "m2_close_as_m1", "m2_open_as_m1",
		"message_arms", "message_arm_edits"}, []string{"unions_maps_keyed"}},
	{"pointer-arrays", []string{"stream_parts", "stream_chain", "parts_elem_kind", "parts_count_past_bound", "parts_index_out_of_range",
		"w1_fleet", "w2_fleet"}, []string{"blobs_pointers"}},
	{"union-arrays", []string{"message_batch", "message_arm_edits", "batch_elem_kind", "batch_unknown_arm"}, []string{"unions_maps_keyed"}},
	{"optional-arrays", []string{"message_trace", "message_save", "message_empty", "trace_elem_kind", "trace_count_past_bound", "marks_unknown_variant"}, nil},
	{"union-arms-any", []string{"stream_arm_first", "stream_arm_pointer", "stream_arm_tag", "a2_arm_enum_as_int", "a2_arm_pointer_as_int",
		"a2_arm_bytes_as_string", "a2_arm_length_off_width", "a1_arm_moved_width_as_a2", "a1_arm_one_width_as_a2", "a1_arm_as_string_as_a2",
		"a1_arm_as_body_as_a2", "a1_arm_body_as_a2", "a1_arm_unknown_as_a2", "nested_arm_trailing", "escape_arm"}, []string{"unions_maps_keyed"}},
}

type luauVTConstruct struct{ name, schema, root, json string }

var luauVTConstructs = []luauVTConstruct{
	{"blobs_pointers", "package probe\ntable Node\n{\n    name *string\n    data *bytes\n    next *Node\n    kids []*Node\n}\n",
		"Node", `{"name":"root","data":"AQID","next":{"name":"n1"},"kids":[{"name":"k1"},null,{"name":"k2"}]}`},
	{"unions_maps_keyed", "package probe\nenum Slot { head, body, feet }\ntable Item\n{\n    w uint16\n}\nunion Pick\n{\n    item Item\n    count uint32\n    label string(8)\n    empty\n}\ntable Kit\n{\n    gear [Slot]Item\n    pick Pick\n    picks []Pick\n    byid map[uint32]Item\n    flags_ok bool\n}\n",
		"Kit", `{"gear":{"head":{"w":1},"feet":{"w":3}},"pick":{"label":"x"},"picks":[{"count":4},{"empty":null},{"item":{"w":2}}],"byid":{"9":{"w":9},"2":{"w":2}},"flags_ok":true}`},
}

func luauVTProbe(g *generatedUnit, rootName string) string {
	var b strings.Builder
	b.WriteString("local inputs = require(\"./inputs\")\nlocal load = require(\"./schema_load\")\nlocal save = require(\"./schema_save\")\nlocal St = require(\"./schema_tables\")\n")
	var mods []string
	for name := range g.files {
		if strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)
	for _, m := range mods {
		fmt.Fprintf(&b, "require(\"./%s\")\n", m)
	}
	fmt.Fprintf(&b, "local report: any = {}\nlocal v = load.load(%q, inputs.wire, report)\n", rootName)
	b.WriteString("if v == nil then print(\"REFUSED\") return end\n")
	fmt.Fprintf(&b, "local d = St.lookup(%q)\n", rootName)
	b.WriteString("local out = save.save(d, v)\n")
	b.WriteString("local measure = save.measure(d, v)\n")
	b.WriteString("if out == nil or measure == nil then print(\"SAVENIL\") return end\n")
	b.WriteString("local parts = table.create(buffer.len(out))\n")
	b.WriteString("for i = 0, buffer.len(out) - 1 do\n")
	b.WriteString("\tparts[i + 1] = string.format(\"%02x\", buffer.readu8(out, i))\n")
	b.WriteString("end\n")
	b.WriteString("print(\"x\" .. table.concat(parts))\n")
	b.WriteString("print(measure)\n")
	return b.String()
}

// vtExec runs the emitted main and returns its stdout. A script failure is
// an error, not a skip and not a fatal: the negative control records it as
// the row going red, and the positive test records it as a finding.
func vtExec(t *testing.T, dir, main string, inputs map[string][]byte) (string, error) {
	t.Helper()
	bin, why := luaurun.Interpreter()
	if bin == "" {
		t.Fatalf("the Luau interpreter does not resolve: %s", why)
	}
	if err := os.WriteFile(filepath.Join(dir, "inputs.luau"), luaurun.InputsModule(inputs), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, main)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("luau %s: %v\n%s", main, err, stderr.Bytes())
	}
	return strings.TrimSpace(stdout.String()), nil
}

// vtRoundTrip is "REFUSED", "SAVENIL", or the saved bytes and measure.
func vtRoundTrip(t *testing.T, g *generatedUnit, rootName string, wire []byte) (string, []byte, int, error) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(luauVTProbe(g, rootName)), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := vtExec(t, g.dir, "main.luau", map[string][]byte{"wire": wire})
	if err != nil {
		return "", nil, 0, err
	}
	if out == "REFUSED" || out == "SAVENIL" {
		return out, nil, 0, nil
	}
	lines := strings.Split(out, "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "x") {
		return "", nil, 0, fmt.Errorf("unexpected output %q", out)
	}
	saved, err := hex.DecodeString(strings.TrimPrefix(lines[0], "x"))
	if err != nil {
		return "", nil, 0, err
	}
	measure, err := strconv.Atoi(lines[1])
	if err != nil {
		return "", nil, 0, err
	}
	return "", saved, measure, nil
}

func vtIndexRows(t *testing.T) map[string]reportRow {
	t.Helper()
	rows := map[string]reportRow{}
	for _, row := range manifestRows(t) {
		// A report line names the evolution verdict. It wins over an instance
		// of the same case name; these six cells do not share names that way.
		if _, ok := rows[row.caseName]; ok && row.kind != "report" {
			continue
		}
		rows[row.caseName] = row
	}
	return rows
}

type vtPatch struct{ file, from, to string }

func vtApplyPatch(t *testing.T, g *generatedUnit, p vtPatch) {
	t.Helper()
	path := filepath.Join(g.dir, p.file)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(src), p.from); n != 1 {
		t.Fatalf("%s carries the plant anchor %d times, want 1", p.file, n)
	}
	patched := strings.Replace(string(src), p.from, p.to, 1)
	if patched == string(src) {
		t.Fatal("the plant changed nothing")
	}
	if err := os.WriteFile(path, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
}

func vtUnit(t *testing.T, units map[string][]string, cache map[string]*generatedUnit, key string, patches []vtPatch) *generatedUnit {
	t.Helper()
	if g, ok := cache[key]; ok {
		return g
	}
	g := generateUnit(t, units, key)
	if g.err == nil {
		for _, p := range patches {
			vtApplyPatch(t, g, p)
		}
	}
	cache[key] = g
	return g
}

func vtSourceUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	p := filepath.Join(t.TempDir(), "probe.schema")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{p})
	if err != nil {
		t.Fatalf("the construct does not load: %v", err)
	}
	return u
}

func vtEmitUnit(t *testing.T, u *ir.Unit, patches []vtPatch) *generatedUnit {
	t.Helper()
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatalf("luau generate: %v", err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatalf("luautable generate: %v", err)
	}
	for name, data := range tables {
		files[name] = data
	}
	g := &generatedUnit{dir: t.TempDir(), files: files, loaded: true}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(g.dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range patches {
		vtApplyPatch(t, g, p)
	}
	return g
}

func vtEngineWire(t *testing.T, u *ir.Unit, rootName, text string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	st := u.Tables[rootName]
	if st == nil {
		t.Fatalf("no table %s", rootName)
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

func vtCellFailures(t *testing.T, c luauVTCell, patches []vtPatch) (fails []string, held int) {
	t.Helper()
	units, _ := readManifest(t)
	rows := vtIndexRows(t)
	expect := reportsExpect(t)
	cache := map[string]*generatedUnit{}
	for _, name := range c.cases {
		row, ok := rows[name]
		if !ok {
			t.Fatalf("%s names case %s, which the manifest does not carry", c.row, name)
		}
		if _, ok := units[row.unit]; !ok {
			t.Fatalf("%s case %s names unit %s, which the manifest does not declare", c.row, name, row.unit)
		}
		g := vtUnit(t, units, cache, row.unit, patches)
		if g.err != nil {
			fails = append(fails, fmt.Sprintf("%s: unit %s is refused: %v", name, row.unit, g.err))
			continue
		}
		wire, err := os.ReadFile(filepath.Join(root, row.wire))
		if err != nil {
			t.Fatal(err)
		}
		kind, saved, measure, err := vtRoundTrip(t, g, row.root, wire)
		if err != nil {
			fails = append(fails, fmt.Sprintf("%s (%s/%s): %v", name, row.unit, row.root, err))
			continue
		}
		held++
		switch {
		case row.kind == "report":
			want, ok := expect[name]
			if !ok {
				t.Fatalf("report %s: reports.txt names no such case", name)
			}
			got := "read"
			if kind == "REFUSED" {
				got = "refused"
			}
			if got != want {
				fails = append(fails, fmt.Sprintf("%s (%s/%s): Luau %s, the Go engine %s", name, row.unit, row.root, got, want))
			}
		case row.blobShared:
			if kind != "REFUSED" {
				fails = append(fails, fmt.Sprintf("%s (%s/%s): a wire-refused instance answered %s", name, row.unit, row.root, kind))
			}
		default:
			if kind == "REFUSED" || kind == "SAVENIL" {
				fails = append(fails, fmt.Sprintf("%s (%s/%s): the strict read answered %s", name, row.unit, row.root, kind))
				continue
			}
			if string(saved) != string(wire) || measure != len(wire) {
				fails = append(fails, fmt.Sprintf("%s (%s/%s): the save is not the wire (%d saved, measure %d, wire %d)", name, row.unit, row.root, len(saved), measure, len(wire)))
			}
		}
	}
	for _, name := range c.constructs {
		i := slices.IndexFunc(luauVTConstructs, func(cc luauVTConstruct) bool { return cc.name == name })
		if i < 0 {
			t.Fatalf("%s names construct %s, which luauVTConstructs does not carry", c.row, name)
		}
		cc := luauVTConstructs[i]
		u := vtSourceUnit(t, cc.schema)
		wire := vtEngineWire(t, u, cc.root, cc.json)
		g := vtEmitUnit(t, u, patches)
		kind, saved, measure, err := vtRoundTrip(t, g, cc.root, wire)
		if err != nil {
			fails = append(fails, fmt.Sprintf("construct %s: %v", name, err))
			continue
		}
		held++
		if kind != "" || string(saved) != string(wire) || measure != len(wire) {
			fails = append(fails, fmt.Sprintf("construct %s: Luau %s (%d saved, measure %d, wire %d)", name, kind, len(saved), measure, len(wire)))
		}
	}
	return fails, held
}

func TestLuauVtPointerCells(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "luau (it runs the emitted table runtime)")
	for _, c := range luauVTCells {
		t.Run(c.row, func(t *testing.T) {
			t.Parallel()
			fails, held := vtCellFailures(t, c, nil)
			for _, f := range fails {
				t.Error(f)
			}
			if held == 0 {
				t.Fatal("the row holds nothing")
			}
			t.Logf("%s: %d corpus rows and %d constructs held", c.row, len(c.cases), len(c.constructs))
		})
	}
}

// TestLuauVtPointerCellsNegativeControl plants one fault in the emitted
// save and one in the emitted load, on both the variable walk and the fixed
// reader the same rows also reach, then runs the real interpreter. Each row
// must go red. Matching an anchor without running the planted file is not
// the control.
func TestLuauVtPointerCellsNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "luau (it runs the emitted table runtime)")
	// Variable: shift every id-table entry, and write an id-table count one
	// past the entries. Fixed: refuse every form-3 read, and write form 1.
	// The anchors are bytes the emitter wrote into the temp dir.
	controls := []struct {
		name    string
		patches []vtPatch
	}{
		{"load-id-shift", []vtPatch{
			{file: "schema_load.luau",
				from: "\t\tseen[id] = true\n\t\tids[i + 1] = id\n",
				to:   "\t\tseen[id] = true\n\t\tids[i + 1] = id + integer.create(1)\n"},
			{file: "schema_fixed.luau",
				from: "if form ~= 3 then return no(\"newer_form\") end",
				to:   "if form == 3 then return no(\"newer_form\") end"},
		}},
		{"save-id-count", []vtPatch{
			{file: "schema_save.luau",
				from: "\tw_int(w, integer.create(#c.ids.list))\n",
				to:   "\tw_int(w, integer.create(#c.ids.list + 1))\n"},
			{file: "schema_fixed.luau",
				from: "buffer.writeu8(out, 0, 3)\n",
				to:   "buffer.writeu8(out, 0, 1)\n"},
		}},
	}
	for _, ctl := range controls {
		for _, c := range luauVTCells {
			t.Run(ctl.name+"/"+c.row, func(t *testing.T) {
				t.Parallel()
				fails, _ := vtCellFailures(t, c, ctl.patches)
				if len(fails) == 0 {
					t.Fatalf("%s: the %s row stayed green", ctl.name, c.row)
				}
				t.Logf("%s/%s: %d findings, first %s", ctl.name, c.row, len(fails), fails[0])
			})
		}
	}
}
