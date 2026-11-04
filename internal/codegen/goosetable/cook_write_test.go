package goosetable_test

// THE COOK'S WRITE SIDE against an independent layout (docs/SPEC-TABLES.md
// §7.6), the Go leg's TestCookWriterMatchesIndependentLayout for Goose: each
// case's value is read from text by the tool's model, written to a wire by
// the tool's encoder, and cooked by the tool's cooker (internal/tablecook) in
// both byte orders; the populated feature witnesses
// (testdata/conformance/feature-witnesses/full) are cooked from their
// committed wires the way `schema cook` cooks them. The Goose program loads
// the same wire through schema_rt::load_table and cooks it through the
// generated <Root>CookMeasure and <Root>Cook — the typed writer for a fixed
// root it carries, the region writer (runtime/cook_write.goose) for every
// other — and the bytes must be the tool's, the measure the file's length,
// and a capacity one byte short must write nothing. The conformance harness
// holds the same write against the C++ reference's cooks for every cook-write
// row of the shared corpus (test/conformance/goose/accel.goose);
// test/conformance/goose/negative-control-accel cook-numbering is its
// control.
//
// TestGooseCookRegionWriterBoundsCallerIndices holds the writer's refusal
// contract over a caller's malformed loaded value (a pointer past the node
// table, a root table past TableUnit's), and
// TestGooseCookRegionWriterNegativeControl breaks each clause these gates own.
//
// Unit tests here use no real time, no socket and no network: each run is
// `goose run` of a generated program in t.TempDir().

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goosetable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// cookWriteCases are the Go leg's cases (internal/codegen/gotable,
// cookwrite_test.go) — lists of 128-bit lanes in a node and in the root, a
// list of records holding lists, a fixed root whose closure holds unions,
// wstring, bytes and optional records, and a variable root with shared
// nodes, pointer lists, lists of records and union arms holding lists — and
// one more: blob nodes reached through a pointed-at node and the root, an
// optional scalar, and a map whose unbounded text keys ride in its extent.
var cookWriteCases = []struct {
	name, schema string
	texts        []string
}{
	{"wide-node-chain", `package probe
table Tail { wides []uint128 }
table Root { tail *Tail
 wides []uint128
 tags []uint64 }
`, []string{`{"tail":{"wides":[3]},"wides":[1],"tags":[2]}`}},
	{"wide-list", `package probe
table Root { items []uint128
 tail int32 }
`, []string{`{}`, `{"items":[1],"tail":7}`, `{"items":[1,340282366920938463463374607431768211455],"tail":9}`}},
	{"nested-wide-list", `package probe
table Child { tag uint8
 items []uint128 }
table Root { children []Child
 tail int32 }
`, []string{`{"children":[{"tag":3,"items":[1]},{"tag":5,"items":[2,3]}],"tail":7}`}},
	{"fixed", `package probe
enum Mode { First, Second }
fixed table Child { n int32 = 7 }
fixed table Rows { items [..3]Child }
union Value { amount uint128
 words wstring(8)
 rows Rows
 empty }
fixed table Root { flag bool
 mode Mode
 text string(9)
 data bytes(7)
 wide uint128
 real float64
 child ?Child
 values [..3]Value
 value Value }
`, []string{`{}`, `{"flag":true,"mode":"Second","text":"abc","data":"YWJj","wide":340282366920938463463374607431768211455,"real":1.25,"child":{"n":12},"values":[{"amount":123456789012345678901234567890},{"words":"hello"}],"value":{"rows":{"items":[{"n":8},{"n":9}]}}}`}},
	{"variable", `package probe
table Node { value int32
 next *Node }
table Nodes { items []*Node }
table Children { items []Child }
union Value { number uint128
 nodes Nodes
 children Children
 empty }
table Child { id uint8
 samples []uint16
 link *Node }
table Root { head *Node
 alias *Node
 small uint8
 wide uint128
 items []Child
 payload Value
 other [..2]Value }
`, []string{`{}`, `{"head":{"&node":1,"value":5,"next":{"value":6}},"alias":{"&node":1},"small":2,"wide":12345678901234567890,"items":[{"id":7,"samples":[10,20],"link":{"&node":1}}],"payload":{"children":{"items":[{"samples":[8,9]}]}},"other":[{"nodes":{"items":[{"&node":1}]}},{"children":{"items":[{"id":4}]}}]}`}},
	{"blobs-and-text-keys", `package probe
table Leaf { note *string
 data *bytes }
table Root { name string(12)
 leaf *Leaf
 thumb *bytes
 alias *bytes
 caption ?int32
 names map[string]string(8) }
`, []string{`{}`, `{"name":"root","leaf":{"note":"hi","data":"AQID"},"thumb":"BAUG","caption":4,"names":{"a":"x","bb":"yy"}}`}},
	{"wstring", `package probe
table Root { title wstring(8)
 label string(8) = "dflt"
 next *Root }
`, []string{`{}`, `{"title":"h\u00e9llo \ud83d\ude00","label":"x","next":{"title":"z"}}`}},
}

// cookUnitFiles generates a unit for Goose with `override` replacing runtime
// files, plus all.goose, which imports every generated module.
func cookUnitFiles(t *testing.T, paths []string, override map[string][]byte) (*ir.Unit, map[string][]byte) {
	t.Helper()
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	files, err := goose.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := goosetable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	maps.Copy(files, override)
	mods := make([]string, 0, len(files))
	for name := range files {
		mods = append(mods, strings.TrimSuffix(name, ".goose"))
	}
	sort.Strings(mods)
	var all strings.Builder
	for _, mod := range mods {
		fmt.Fprintf(&all, "import .%s;\n", mod)
	}
	files["all.goose"] = []byte(all.String())
	return u, files
}

// cookWire is one wire the program cooks as a root.
type cookWire struct {
	file, root string
}

// cookWriteProgram is the Goose side: for each wire, the load, then for
// each byte order one line — `ok <hex>` for the cook, or `bad <why>`. A root
// is cooked through its typed writer or its region writer, whichever the
// unit's generated cook module declares.
func cookWriteProgram(ns string, files map[string][]byte, wires []cookWire) string {
	var cooks strings.Builder
	for name, data := range files {
		if strings.HasSuffix(name, "Cook.goose") {
			cooks.Write(data)
		}
	}
	roots := map[string]bool{}
	for _, w := range wires {
		roots[w.root] = true
	}
	names := make([]string, 0, len(roots))
	for r := range roots {
		names = append(names, r)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("import os;\nimport .all;\n\n")
	b.WriteString(gooserun.HexProbe)
	b.WriteString("\nfn measure(r: const u8[:], data: const u8[:], vals: const schema_rt::LoadVal[:], nodes: const schema_rt::LoadNode[:], root: i64) -> i64 {\n")
	for _, r := range names {
		if strings.Contains(cooks.String(), "fn "+r+"CookMeasure(data: ") {
			fmt.Fprintf(&b, "    if r == %q { return %s::%sCookMeasure(data, vals, nodes, root); }\n", r, ns, r)
		} else {
			fmt.Fprintf(&b, "    if r == %q { return %s::%sCookMeasure(); }\n", r, ns, r)
		}
	}
	b.WriteString("    return -2;\n}\n\nfn cook(r: const u8[:], data: const u8[:], vals: const schema_rt::LoadVal[:], nodes: const schema_rt::LoadNode[:], root: i64, out: u8[:], big: bool) -> bool {\n")
	for _, r := range names {
		if strings.Contains(cooks.String(), "fn "+r+"CookMeasure(data: ") {
			fmt.Fprintf(&b, "    if r == %q { return %s::%sCook(data, vals, nodes, root, out, big); }\n", r, ns, r)
		} else {
			fmt.Fprintf(&b, "    if r == %q {\n        var v = %s::%s { .. };\n        %s::%sCookFromLoaded(v, data, vals, root);\n        return %s::%sCook(v, out, big);\n    }\n", r, ns, r, ns, r, ns, r)
		}
	}
	fmt.Fprintf(&b, `    return false;
}

fn one(file: const u8[:], r: const u8[:]) {
    var data: u8[>..] = [];
    if !read_file(file, data) { write_stdout("bad unreadable\n"); return; }
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var report = schema_rt::LoadReport { .. };
    let root = schema_rt::load_table(%s::TableUnit, r, data, vals, nodes, report);
    if root < 0 || report.malformed || report.refused { write_stdout("bad load\n"); write_stdout("bad load\n"); return; }
    var big = false;
    while true {
        let need = measure(r, data, vals, nodes, root);
        if need < 0 { write_stdout("bad measure\n"); }
        else {
            var out: u8[>..] = [];
            for i in need { out.push(0xa5); }
            var short_ok = !cook(r, data, vals, nodes, root, out[0..need - 1], big);
            for x in out { if x != 0xa5 { short_ok = false; } }
            if !short_ok { write_stdout("bad short-capacity\n"); }
            else if !cook(r, data, vals, nodes, root, out, big) { write_stdout("bad refused\n"); }
            else { write_stdout("ok "); hex(out); }
        }
        if big { break; }
        big = true;
    }
}

fn main() {
`, ns)
	for _, w := range wires {
		fmt.Fprintf(&b, "    one(%q, %q);\n", w.file, w.root)
	}
	b.WriteString("}\n")
	return b.String()
}

// toolCook is the tool's cook of one wire in one order: the wire decoded by
// the tool's engine into its model, then internal/tablecook, as `schema
// cook` does it.
func toolCook(t *testing.T, m *tabletext.Model, root string, wire []byte, big bool) []byte {
	t.Helper()
	inst := m.New(m.Lookup(root))
	var report tabletext.Report
	if _, err := tablewire.Decode(m, inst, wire, &report); err != nil || report.Refused {
		t.Fatalf("the tool does not decode the %s wire: %v %+v", root, err, report)
	}
	cooked, err := tablecook.Cook(m, inst, tablecook.Options{Big: big})
	if err != nil {
		t.Fatal(err)
	}
	return cooked
}

// cookDiffs runs the program and answers one finding per wire and order
// whose bytes are not the tool's.
func cookDiffs(t *testing.T, ns string, files map[string][]byte, wires []cookWire, want [][]byte) []string {
	t.Helper()
	files["main.goose"] = []byte(cookWriteProgram(ns, files, wires))
	out := gooserun.Run(t, t.TempDir(), "main.goose", files)
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != len(want) {
		t.Fatalf("the program printed %d lines, want %d:\n%s", len(lines), len(want), out)
	}
	var bad []string
	for i, line := range lines {
		name := fmt.Sprintf("%s %s", wires[i/2].file, map[bool]string{false: "little", true: "big"}[i%2 == 1])
		hexed, ok := strings.CutPrefix(line, "ok ")
		if !ok {
			bad = append(bad, name+": "+line)
			continue
		}
		got, err := hex.DecodeString(hexed)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want[i]) {
			at := 0
			for at < len(got) && at < len(want[i]) && got[at] == want[i][at] {
				at++
			}
			bad = append(bad, fmt.Sprintf("%s: %d bytes cooked, the tool cooks %d; first difference at byte %d", name, len(got), len(want[i]), at))
		}
	}
	return bad
}

// cookProbe runs one cookWriteCases entry with `override` in place and
// answers its findings.
func cookProbe(t *testing.T, name string, override map[string][]byte) []string {
	t.Helper()
	var tc struct {
		name, schema string
		texts        []string
	}
	for _, c := range cookWriteCases {
		if c.name == name {
			tc = c
		}
	}
	if tc.name == "" {
		t.Fatalf("no case %s", name)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(path, []byte(tc.schema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, files := cookUnitFiles(t, []string{path}, override)
	m := tabletext.NewModel(u)
	var wires []cookWire
	var want [][]byte
	for i, text := range tc.texts {
		v := m.New(m.Lookup("Root"))
		var report tabletext.Report
		if !m.Read(v, []byte(text), &report) {
			t.Fatalf("oracle text %d: %+v", i, report)
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatal(err)
		}
		file := fmt.Sprintf("w%d.bin", i)
		files[file] = wire
		wires = append(wires, cookWire{file, "Root"})
		for _, big := range []bool{false, true} {
			want = append(want, toolCook(t, m, "Root", wire, big))
		}
	}
	return cookDiffs(t, "probe", files, wires, want)
}

// featureWitnesses are the populated implementation witnesses
// (testdata/conformance/feature-witnesses/full): bounded and optional text
// lists under a text-list total, unbounded list text and map keys, an
// optional map, an optional variable closure, a timestamp, declared forms and
// CRC-32C integrity, and an array root.
var featureWitnesses = []cookWire{{"Root.bin", "Root"}, {"ArrayRoot.bin", "ArrayRoot"}}

// featureProbe cooks the feature witnesses with `override` in place and
// answers its findings.
func featureProbe(t *testing.T, override map[string][]byte) []string {
	t.Helper()
	dir := filepath.Join(root, "testdata", "conformance", "feature-witnesses", "full")
	u, files := cookUnitFiles(t, []string{filepath.Join(dir, "Features.schema")}, override)
	m := tabletext.NewModel(u)
	var want [][]byte
	for _, w := range featureWitnesses {
		wire, err := os.ReadFile(filepath.Join(dir, w.file))
		if err != nil {
			t.Fatal(err)
		}
		files[w.file] = wire
		for _, big := range []bool{false, true} {
			want = append(want, toolCook(t, m, w.root, wire, big))
		}
	}
	return cookDiffs(t, "featurewitnesses", files, featureWitnesses, want)
}

func TestGooseCookWriterMatchesIndependentLayout(t *testing.T) {
	slowtest.Gate(t, "goose")
	t.Parallel()
	for _, tc := range cookWriteCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, bad := range cookProbe(t, tc.name, nil) {
				t.Error(bad)
			}
		})
	}
	t.Run("feature-witnesses", func(t *testing.T) {
		t.Parallel()
		for _, bad := range featureProbe(t, nil) {
			t.Error(bad)
		}
	})
}

// TestGooseCookRegionWriterHonoursDeclaredForms: a pointered root that
// declares the cook among its forms (§2.12) gets the region writer and its
// layout; one that does not gets neither.
func TestGooseCookRegionWriterHonoursDeclaredForms(t *testing.T) {
	slowtest.Gate(t, "goose")
	t.Parallel()
	const src = `package formsdemo
table Cooked | forms = "wire,cook" {
    next *Cooked
}
table Uncooked | forms = "wire" {
    next *Uncooked
}
`
	u := unitFromSchema(t, "Forms.schema", src)
	files, err := goosetable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	cook := string(files["FormsCook.goose"])
	for _, want := range []string{"fn CookedCookMeasure(data: ", "fn CookedCook(data: "} {
		if !strings.Contains(cook, want) {
			t.Errorf("FormsCook.goose missing %q", want)
		}
	}
	if strings.Contains(cook, "UncookedCook") {
		t.Errorf("FormsCook.goose emitted a cook surface for Uncooked, which declares no cook")
	}
	home := string(files["formsdemo_cook.goose"])
	if !strings.Contains(home, "let CookedCookRegionTable:") || strings.Contains(home, "UncookedCookRegionTable") {
		t.Errorf("formsdemo_cook.goose region roots are not the cook-carrying tables:\n%s", home)
	}
}

// boundsProgram loads the `variable` case's populated wire and hands the
// region writer three malformed loaded values: a pointer slot naming the
// wire node nodes.len+2, a root table past TableUnit's, and a union tag whose
// high bit would become a negative index. Each must refuse with a poisoned buffer
// left untouched; it prints one line per check.
const boundsProgram = `import os;
import .all;

fn load(data: const u8[:], vals: schema_rt::LoadVal[>..]&, nodes: schema_rt::LoadNode[>..]&) -> i64 {
    var report = schema_rt::LoadReport { .. };
    let root = schema_rt::load_table(probe::TableUnit, "Root", data, vals, nodes, report);
    if root < 0 || report.malformed || report.refused { write_stdout("bad load\n"); exit(1); }
    return root;
}

fn untouched(out: const u8[:]) -> bool {
    for x in out { if x != 0xa5 { return false; } }
    return true;
}

fn verdict(name: const u8[:], measured: i64, cooked: bool, out: const u8[:]) {
    if measured == -1 && !cooked && untouched(out) { write_stdout(str(name, " refused\n")); return; }
    write_stdout(str(name, " accepted: measure ", measured, ", cook ", cooked, ", output untouched ", untouched(out), "\n"));
}

fn main() {
    var data: u8[>..] = [];
    if !read_file("w1.bin", data) { write_stdout("bad unreadable\n"); exit(1); }
    var out: u8[>..] = [];
    for i in 4096 { out.push(0xa5); }

    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    let root = load(data, vals, nodes);
    var edited = false;
    for v, i in vals {
        if !edited && v.shape == schema_rt::ShapePtr && v.state == schema_rt::ValPresent {
            vals[i].bits = schema_rt::Int128 { lo: (nodes.len + 2) as! u64, hi: 0 };
            edited = true;
        }
    }
    if !edited { write_stdout("bad no pointer slot\n"); exit(1); }
    verdict("pointer", probe::RootCookMeasure(data, vals, nodes, root), probe::RootCook(data, vals, nodes, root, out, false), out);

    var vals2: schema_rt::LoadVal[>..] = [];
    var nodes2: schema_rt::LoadNode[>..] = [];
    let root2 = load(data, vals2, nodes2);
    let tab = probe::TableUnit.tables.len;
    let measured = schema_rt::cook_region_measure(probe::TableUnit, probe::CookRegionLayout, probe::BuildVersion, tab, data, vals2, nodes2, root2);
    let cooked = schema_rt::cook_region_write(probe::TableUnit, probe::CookRegionLayout, probe::BuildVersion, tab, data, vals2, nodes2, root2, out, false, false) >= 0;
    verdict("table", measured, cooked, out);

    var vals3: schema_rt::LoadVal[>..] = [];
    var nodes3: schema_rt::LoadNode[>..] = [];
    let root3 = load(data, vals3, nodes3);
    var changed = false;
    for v, i in vals3 {
        if !changed && v.shape == schema_rt::ShapeUnion && v.state == schema_rt::ValPresent {
            vals3[i].bits = schema_rt::Int128 { lo: 0xFFFFFFFFFFFFFFFF, hi: 0 };
            changed = true;
        }
    }
    if !changed { write_stdout("bad no union slot\n"); exit(1); }
    verdict("union", probe::RootCookMeasure(data, vals3, nodes3, root3), probe::RootCook(data, vals3, nodes3, root3, out, false), out);
}
`

// cookBounds runs boundsProgram over the `variable` case with `override` in
// place and answers its stdout, and whether the program ran to its end.
func cookBounds(t *testing.T, override map[string][]byte) (string, bool) {
	t.Helper()
	var schema, text string
	for _, c := range cookWriteCases {
		if c.name == "variable" {
			schema, text = c.schema, c.texts[1]
		}
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(path, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, files := cookUnitFiles(t, []string{path}, override)
	m := tabletext.NewModel(u)
	v := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(v, []byte(text), &report) {
		t.Fatalf("oracle text: %+v", report)
	}
	wire, err := tablewire.Encode(m, v)
	if err != nil {
		t.Fatal(err)
	}
	files["w1.bin"] = wire
	files["main.goose"] = []byte(boundsProgram)
	goosePath, why := gooserun.Compiler()
	if goosePath == "" {
		t.Fatalf("the Goose compiler does not resolve: %s", why)
	}
	run := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(run, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := gooserun.Command(t, goosePath, "run", "main.goose")
	cmd.Dir = run
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	return stdout.String(), err == nil
}

// TestGooseCookRegionWriterBoundsCallerIndices: the region writer reads a
// caller's flat value table, so an index in it out of range is a refusal,
// never a read out of range.
func TestGooseCookRegionWriterBoundsCallerIndices(t *testing.T) {
	slowtest.Gate(t, "goose")
	t.Parallel()
	out, ok := cookBounds(t, nil)
	if want := "pointer refused\ntable refused\nunion refused\n"; !ok || out != want {
		t.Fatalf("the bounds program answered %q (ran to its end: %t), want %q", out, ok, want)
	}
}

// cookSabotage replaces lines of runtime/cook_write.goose, from/to pairs,
// failing if one is not there exactly once, so a control can never pass
// vacuously.
func cookSabotage(t *testing.T, pairs ...string) map[string][]byte {
	t.Helper()
	data := string(goosetable.Runtime()["cook_write.goose"])
	for i := 0; i+1 < len(pairs); i += 2 {
		if n := strings.Count(data, pairs[i]); n != 1 {
			t.Fatalf("sabotage: cook_write.goose holds %q %d times, want once", pairs[i], n)
		}
		data = strings.Replace(data, pairs[i], pairs[i+1], 1)
	}
	return map[string][]byte{"cook_write.goose": []byte(data)}
}

// TestGooseCookRegionWriterNegativeControl breaks one clause of the region
// writer per control and requires the gate that owns it to go red: the
// unbounded text blob's length and the bounded text's used length (the
// feature witnesses' text lists, list text and map keys), the wstring code
// unit order (the wstring case), and each bound (the bounds program, which
// then reads out of range and aborts instead of refusing; the root's table is
// bounded twice, and its control removes both).
func TestGooseCookRegionWriterNegativeControl(t *testing.T) {
	slowtest.Gate(t, "goose")
	t.Parallel()
	t.Run("unbounded-text-length", func(t *testing.T) {
		t.Parallel()
		bad := featureProbe(t, cookSabotage(t, "        cook_put_at(c, blob, n as! u64, 4);\n", "        cook_put_at(c, blob, (n - 1) as! u64, 4);\n"))
		if !strings.Contains(strings.Join(bad, "\n"), "Root.bin") {
			t.Fatalf("a wrong blob length left the feature witnesses green: %q", bad)
		}
	})
	t.Run("bounded-text-length", func(t *testing.T) {
		t.Parallel()
		bad := featureProbe(t, cookSabotage(t, "    cook_put_at(c, at + col.len_off, n as! u64, 4);\n", "    cook_put_at(c, at + col.len_off, 0, 4);\n"))
		if !strings.Contains(strings.Join(bad, "\n"), "Root.bin") {
			t.Fatalf("a zero text length left the feature witnesses green: %q", bad)
		}
	})
	t.Run("wstring-unit-order", func(t *testing.T) {
		t.Parallel()
		bad := cookProbe(t, "wstring", cookSabotage(t, "lo | (hi << 8)", "hi | (lo << 8)"))
		if len(bad) == 0 {
			t.Fatal("swapped code-unit bytes left the wstring case green")
		}
	})
	t.Run("union-tag-bound", func(t *testing.T) {
		t.Parallel()
		out, ok := cookBounds(t, cookSabotage(t, "    if tag < 0 { c.refused = true; return; }\n", ""))
		if ok && strings.Contains(out, "union refused") {
			t.Fatalf("without the union tag bound the writer still refused: %q", out)
		}
	})
	t.Run("pointer-bound", func(t *testing.T) {
		t.Parallel()
		out, ok := cookBounds(t, cookSabotage(t, "    if c.vals[s].bits.hi != 0 || wire > (c.nodes.len + 1) as! u64 { c.refused = true; return 0; }\n", ""))
		if ok && strings.Contains(out, "pointer refused") {
			t.Fatalf("without the pointer bound the writer still refused: %q", out)
		}
	})
	t.Run("table-bound", func(t *testing.T) {
		t.Parallel()
		// both places the root's table is bounded: the check before any walk,
		// and the walk's own bound on every table index it follows
		out, ok := cookBounds(t, cookSabotage(t,
			"    if c.root_tab < 0 || c.root_tab >= tables { return false; }\n", "",
			"    if tab < 0 || tab >= c.unit.tables.len { c.refused = true; return false; }\n", ""))
		if ok && strings.Contains(out, "table refused") {
			t.Fatalf("without the table bound the writer still refused: %q", out)
		}
	})
}
