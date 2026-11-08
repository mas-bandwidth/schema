package luatable

// THE VARIABLE-TABLE CELLS on the Lua leg (ROADMAP.md, Variable tables;
// docs/SPEC-TABLES.md §2, §3, §3.1, §4), ported from
// internal/codegen/mootable/vtcells_test.go to the one read: one subtest per
// roadmap row, each over the shared corpus rows and Go-engine constructs that
// exercise that row's feature. The whole-manifest gates
// (TestLuaTableSaveRoundTripsEveryInstance, TestLuaTableLoadMatchesTheReports)
// answer for the manifest as a whole; this file answers for each row:
//
//	instance   the wire the C++ reference or the Go engine wrote loads through
//	           the strict Lua read and saves back byte for byte, its measure
//	           its length; a wire-refused instance is held to the refusal
//	report     the strict read's verdict (read or refused) is reports.txt's,
//	           the Go engine's own verdict for that wire
//	construct  a Go-engine construct: the engine's wire of the JSON text reads
//	           through the strict Lua read and saves back byte for byte
//
// TestLuaVariableTableCellsNegativeControl plants one defect in the save and
// one in the load and requires every row's subtest to go red under each.

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type luaVTCell struct {
	row        string
	cases      []string
	constructs []string
}

// The rows and their corpus are moo's (internal/codegen/mootable/vtcells_test.go);
// wide_blob is the manifest's previous_form_wide_blob since the strict spec
// reads the fixed corpus as form 3.
var luaVTCells = []luaVTCell{
	{"fixed-class", []string{"root_full", "root_default", "profile_elide", "loadout_full", "previous_form_wide_blob", "archive",
		"v1_cfg", "v1_seams", "v2_cfg", "v2_seams", "chain_value", "chain_value_empty",
		"v1_cfg_as_v2", "v1_seams_as_v2", "v2_cfg_as_v1", "v2_seams_as_v1", "form_zero", "form_six", "form_ff", "message_as_file"}, nil},
	{"optional-fields", []string{"chain_optional", "chain_optional_empty", "p1_as_p3", "p3_as_p1", "p1_empty_as_p3", "p3_empty_as_p1",
		"p2_as_p3", "p2_empty_as_p3", "p3_as_p2", "optional_absent_no_edge", "optional_present_edge"},
		[]string{"optional_variable", "optional_map"}},
	{"enum-keyed-arrays", []string{"keyed_config", "keyed_default", "r1_cfg", "r2_cfg", "r1_cfg_as_r2", "r2_cfg_as_r1", "k1_as_k2", "k2_as_k1"},
		[]string{"unions_maps_keyed"}},
	{"variable-class", []string{"graph_tree", "graph_shared", "graph_empty", "graph_deep", "guard_false_no_edge", "guard_true_both_edges",
		"list_shared", "list_before_pointer"}, []string{"blobs_pointers"}},
	{"wide-scalars", []string{"scalars_full", "scalars_default", "scalars_edges", "scalars_full_as_2", "scalars_edges_as_2", "field_scalar_widen"},
		[]string{"wide_ints"}},
	{"union-table-arms", []string{"m1_open", "m1_save", "m2_close", "m2_open", "m1_open_as_m2", "m1_save_as_m2", "m2_close_as_m1", "m2_open_as_m1",
		"message_arms", "message_arm_edits"}, []string{"unions_maps_keyed"}},
	{"pointer-arrays", []string{"stream_parts", "stream_chain", "parts_elem_kind", "parts_count_past_bound", "parts_index_out_of_range",
		"w1_fleet", "w2_fleet"}, []string{"blobs_pointers"}},
	{"union-arrays", []string{"message_batch", "message_arm_edits", "batch_elem_kind", "batch_unknown_arm"}, []string{"unions_maps_keyed"}},
	{"optional-arrays", []string{"message_trace", "message_save", "message_empty", "trace_elem_kind", "trace_count_past_bound", "marks_unknown_variant"}, nil},
	{"blobs", []string{"blob_small", "blob_empty", "blob_large", "blob_str8", "blob_str16", "blob_equal_bytes", "blob_shared", "blob_shared_string"},
		[]string{"blobs_pointers"}},
	{"maps", []string{"map_cells", "map_chunks", "map_crews", "map_depth", "map_docs", "map_empty", "map_full", "map_pairs", "map_slots",
		"map_spans", "map_text", "map_trails", "map_runs", "map_key_widen", "map_depth_root_kind", "list_of_maps"},
		[]string{"unions_maps_keyed", "optional_map"}},
	{"union-arms-any", []string{"stream_arm_first", "stream_arm_pointer", "stream_arm_tag", "a2_arm_enum_as_int", "a2_arm_pointer_as_int",
		"a2_arm_bytes_as_string", "a2_arm_length_off_width", "a1_arm_moved_width_as_a2", "a1_arm_one_width_as_a2", "a1_arm_as_string_as_a2",
		"a1_arm_as_body_as_a2", "a1_arm_body_as_a2", "a1_arm_unknown_as_a2", "nested_arm_trailing", "escape_arm"}, []string{"unions_maps_keyed"}},
	{"defaults-and-table-rename", []string{"w1_fleet", "w1_fleet_default", "w2_fleet", "w1_fleet_as_w2", "w2_fleet_as_w1", "w1_fleet_default_as_w2"},
		[]string{"defaults_at", "defaults_off"}},
	{"renames", []string{"r1_cfg", "r1_ping", "r2_cfg", "r1_cfg_as_r2", "r1_ping_as_r2", "r2_cfg_as_r1"}, nil},
	{"id-table-wire", []string{"vocab_wide", "vocab_low", "peer_a", "peer_b", "vocab9_wide", "vocab9_low", "login_full", "match_full",
		"escape_unnamed_field", "escape_named_field", "escape_element", "nopayload_named_field"}, nil},
	{"wstring", []string{"wide_empty", "wide_title", "wide_sites", "wide_narrow_arm", "wide_stamp", "widelist_wide"}, []string{"wide_text"}},
	{"unbounded-arrays", []string{"list_tables", "list_scalars", "list_empty", "list_erased", "list_mixed", "list_shared", "list_nested",
		"list_of_maps", "list_migrates", "list_scalar_widen"}, []string{"blobs_pointers"}},
}

type luaVTConstruct struct{ name, schema, root, json string }

// The constructs are moo's constructCases (internal/codegen/mootable/construct_test.go).
var luaVTConstructs = []luaVTConstruct{
	{"defaults_at", "package probe\ntable D\n{\n    s string(8) = \"hi\"\n    b bytes(4) = \"ab\"\n    n int32 = 5 | min = 0, max = 9\n}\n",
		"D", `{"s":"hi","b":"YWI=","n":5}`},
	{"defaults_off", "package probe\ntable D\n{\n    s string(8) = \"hi\"\n    b bytes(4) = \"ab\"\n    n int32 = 5 | min = 0, max = 9\n}\n",
		"D", `{"s":"","b":"","n":0}`},
	{"optional_variable", "package probe\ntable Inner\n{\n    text *string\n    n int32\n}\ntable Outer\n{\n    inner ?Inner\n    tail int32\n}\n",
		"Outer", `{"inner":{"text":"hi","n":4},"tail":5}`},
	{"optional_map", "package probe\ntable M\n{\n    id int32\n    tags ?map[string(8)]int32\n}\n",
		"M", `{"id":1,"tags":{}}`},
	{"wide_ints", "package probe\ntable W\n{\n    a int64\n    b uint64\n    c int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727\n    d uint128\n    e fixed(48, 16) | min = -100000, max = 100000\n    f ufixed(8, 8) | min = 0, max = 200\n    g fixed(60, 4) | min = -1000000000000, max = 1000000000000\n    h bits(40)\n}\n",
		"W", `{"a":-9007199254740993,"b":18446744073709551615,"c":-170141183460469231731687303715884105728,"d":340282366920938463463374607431768211455,"e":-12345.5,"f":3.25,"g":-1.5,"h":1099511627775}`},
	{"wide_text", "package probe\ntable T\n{\n    w wstring(8)\n    v wstring(4)\n}\n",
		"T", `{"w":"wïde😀","v":"ab"}`},
	{"blobs_pointers", "package probe\ntable Node\n{\n    name *string\n    data *bytes\n    next *Node\n    kids []*Node\n}\n",
		"Node", `{"name":"root","data":"AQID","next":{"name":"n1"},"kids":[{"name":"k1"},null,{"name":"k2"}]}`},
	{"unions_maps_keyed", "package probe\nenum Slot { head, body, feet }\ntable Item\n{\n    w uint16\n}\nunion Pick\n{\n    item Item\n    count uint32\n    label string(8)\n    empty\n}\ntable Kit\n{\n    gear [Slot]Item\n    pick Pick\n    picks []Pick\n    byid map[uint32]Item\n    flags_ok bool\n}\n",
		"Kit", `{"gear":{"head":{"w":1},"feet":{"w":3}},"pick":{"label":"x"},"picks":[{"count":4},{"empty":null},{"item":{"w":2}}],"byid":{"9":{"w":9},"2":{"w":2}},"flags_ok":true}`},
}

// luaVTEngineWire is the Go engine's form-1 wire of the JSON text at root.
func luaVTEngineWire(t *testing.T, u *ir.Unit, root, text string) []byte {
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

// luaVTRoundTrip is the strict read of the wire at root and its save, as
// "<len>:<same|diff>:<measure>", or "refused".
func luaVTRoundTrip(module, root string, fixed bool) string {
	if fixed {
		return `local T = require("` + module + `")
local inputs = require("inputs")
local ok, v = pcall(T.ReadFixed, "` + root + `", inputs.wire)
if not ok or v == nil then print("refused") return end
local saved = T.FixedSave("` + root + `", { v })
print(#saved .. ":" .. (saved == inputs.wire and "same" or "diff") .. ":" .. T.FixedMeasure("` + root + `", 1))
`
	}
	return `local T = require("` + module + `")
local load = require("load")
local save = require("save")
local inputs = require("inputs")
local v = load.Load(T, "` + root + `", inputs.wire, {})
if v == nil then print("refused") return end
local ok, saved = pcall(save.Save, T, "` + root + `", v)
if not ok or saved == nil then print("savenil " .. tostring(saved)) return end
print(#saved .. ":" .. (saved == inputs.wire and "same" or "diff") .. ":" .. tostring(save.Measure(T, "` + root + `", v)))
`
}

type luaVTRow struct {
	kind, unit, root, wire string
	refusal                bool
}

func luaVTManifest(t *testing.T, repo string) (map[string][]string, map[string]luaVTRow, map[string]string) {
	t.Helper()
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	refused := manifestWireRefusalRows(t, manifest)
	rows := map[string]luaVTRow{}
	for _, kind := range []string{"instance", "report"} {
		for _, r := range manifestRows(t, manifest, kind) {
			rows[r.caseName] = luaVTRow{kind: kind, unit: r.unit, root: r.root, wire: r.wire, refusal: refused[r.caseName]}
		}
	}
	return manifestUnits(t, manifest), rows, reportsExpect(t, filepath.Join(repo, "testdata", "conformance", "tables", "reports.txt"))
}

// luaVTRun generates u (its emitted files edited by mutate when set), runs the
// round-trip probe over wire at root and answers its line.
func luaVTRun(t *testing.T, u *ir.Unit, root string, wire []byte, mutate func(*testing.T, map[string][]byte)) (string, error) {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		return "", err
	}
	files = maps.Clone(files)
	if mutate != nil {
		mutate(t, files)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	st := u.Tables[root]
	probe := luaVTRoundTrip(ModuleName(u), root, st != nil && st.FixedDeclared)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(luarun.Run(t, dir, "main.lua", map[string][]byte{"wire": wire}))), nil
}

func luaVTCellFailures(t *testing.T, c luaVTCell, mutate func(*testing.T, map[string][]byte)) (fails []string, held int) {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	units, rows, expect := luaVTManifest(t, repo)
	unitCache := map[string]*ir.Unit{}
	for _, name := range c.cases {
		row, ok := rows[name]
		if !ok {
			t.Fatalf("%s names case %s, which the manifest does not carry", c.row, name)
		}
		u, ok := unitCache[row.unit]
		if !ok {
			var paths []string
			for _, p := range units[row.unit] {
				paths = append(paths, filepath.Join(repo, p))
			}
			u = luarun.LoadUnit(t, paths...)
			unitCache[row.unit] = u
		}
		wire, err := os.ReadFile(filepath.Join(repo, row.wire))
		if err != nil {
			t.Fatal(err)
		}
		out, err := luaVTRun(t, u, row.root, wire, mutate)
		if err != nil {
			fails = append(fails, fmt.Sprintf("%s: unit %s is refused: %v", name, row.unit, err))
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
			if out == "refused" {
				got = "refused"
			}
			if got != want {
				fails = append(fails, fmt.Sprintf("%s (%s/%s): Lua %s, the Go engine %s", name, row.unit, row.root, got, want))
			}
		case row.refusal:
			if out != "refused" {
				fails = append(fails, fmt.Sprintf("%s (%s/%s): the engine refuses this instance and Lua answered %s", name, row.unit, row.root, out))
			}
		default:
			p := strings.SplitN(out, ":", 3)
			if len(p) != 3 || p[1] != "same" || p[0] != p[2] {
				fails = append(fails, fmt.Sprintf("%s (%s/%s): the save is not the wire: %s", name, row.unit, row.root, out))
			}
		}
	}
	for _, name := range c.constructs {
		i := slices.IndexFunc(luaVTConstructs, func(cc luaVTConstruct) bool { return cc.name == name })
		if i < 0 {
			t.Fatalf("%s names construct %s, which luaVTConstructs does not carry", c.row, name)
		}
		cc := luaVTConstructs[i]
		u := luaEvoSource(t, cc.schema)
		wire := luaVTEngineWire(t, u, cc.root, cc.json)
		out, err := luaVTRun(t, u, cc.root, wire, mutate)
		if err != nil {
			fails = append(fails, fmt.Sprintf("construct %s: the Lua backend refuses it: %v", name, err))
			continue
		}
		held++
		if want := fmt.Sprintf("%d:same:%d", len(wire), len(wire)); out != want {
			fails = append(fails, fmt.Sprintf("construct %s: Lua %s, want %s", name, out, want))
		}
	}
	return fails, held
}

// Functional (luaBinary): run with SCHEMA_SLOW=1.
func TestLuaVariableTableCells(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lua (it runs the emitted table runtime)")
	for _, c := range luaVTCells {
		t.Run(c.row, func(t *testing.T) {
			t.Parallel()
			fails, held := luaVTCellFailures(t, c, nil)
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

// TestLuaVariableTableCellsNegativeControl plants, one at a time, a defect in
// the emitted saves (the id table's count one too many, the fixed record hash
// one bit off, so no saved file is the reference's) and in the emitted reads
// (the id table read in reverse, the fixed record-hash test inverted), and
// requires every row's subtest to go red
// under each: no row's gate is green by holding nothing.
//
// Functional (luaBinary): run with SCHEMA_SLOW=1.
func TestLuaVariableTableCellsNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lua (it runs the emitted table runtime)")
	plant := func(file, from, to string) func(*testing.T, map[string][]byte) {
		return func(t *testing.T, files map[string][]byte) {
			src := string(files[file])
			if strings.Count(src, from) != 1 {
				t.Fatalf("%s carries %q %d times, want 1: the control would be vacuous", file, from, strings.Count(src, from))
			}
			files[file] = []byte(strings.Replace(src, from, to, 1))
		}
	}
	both := func(a, b func(*testing.T, map[string][]byte)) func(*testing.T, map[string][]byte) {
		return func(t *testing.T, files map[string][]byte) { a(t, files); b(t, files) }
	}
	for _, ctl := range []struct {
		name   string
		mutate func(*testing.T, map[string][]byte)
	}{
		// the variable save's id-table count one too many, and the fixed
		// save's record hash one bit off: no saved file is the reference's
		{"save-id-count", both(plant("save.lua", "  w_u64(idx, #ids.list)\n", "  w_u64(idx, #ids.list + 1)\n"),
			plant("fixed.lua", "    fixed.hashPrefix = pack_uint(fixed.hash, 8)\n", "    fixed.hashPrefix = pack_uint(fixed.hash ~ 1, 8)\n"))},
		// the variable load's id table read in reverse, and the fixed read's
		// record-hash test inverted: every reference names another field and
		// every fixed record is refused
		{"load-id-order", both(plant("load.lua", "    seen[id] = true\n    table_ids[i + 1] = id\n  end\n  -- roots end", "    seen[id] = true\n    table_ids[count - i] = id\n  end\n  -- roots end"),
			plant("fixed.lua", `  if string.unpack("<I8", buf, at) ~= hash then error("no_layout") end`, `  if string.unpack("<I8", buf, at) == hash then error("no_layout") end`))},
	} {
		for _, c := range luaVTCells {
			t.Run(ctl.name+"/"+c.row, func(t *testing.T) {
				t.Parallel()
				fails, _ := luaVTCellFailures(t, c, ctl.mutate)
				if len(fails) == 0 {
					t.Fatalf("%s: the %s row stayed green", ctl.name, c.row)
				}
				t.Logf("%s: %d findings, first %s", c.row, len(fails), fails[0])
			})
		}
	}
}
