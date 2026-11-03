package goosetable_test

// THE COOK'S WRITE SIDE against an independent layout (docs/SPEC-TABLES.md
// §7.6), the Go leg's TestCookWriterMatchesIndependentLayout for Goose: each
// case's value is read from text by the tool's model, written to a wire by
// the tool's encoder, and cooked by the tool's cooker (internal/tablecook) in
// both byte orders. The Goose program loads the same wire through
// schema_rt::load_table and cooks it through the generated <Root>CookMeasure
// and <Root>Cook — the typed writer for a fixed root it carries, the region
// writer (runtime/cook_write.goose) for every other — and the bytes must be
// the tool's, the measure the file's length, and a capacity one byte short
// must write nothing. The conformance harness holds the same write against
// the C++ reference's cooks for every cook-write row of the shared corpus
// (test/conformance/goose/accel.goose); test/conformance/goose/negative-control-accel
// cook-numbering is its control.
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
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
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
}

// cookWriteProgram is the Goose side: for each wire, the load, then for
// each byte order one line — `ok <hex>` for the cook, or `bad <why>`.
func cookWriteProgram(region bool, n int) string {
	var b strings.Builder
	b.WriteString("import os;\nimport .all;\n\n")
	b.WriteString(gooserun.HexProbe)
	b.WriteString(`
fn measure(data: const u8[:], vals: const schema_rt::LoadVal[:], nodes: const schema_rt::LoadNode[:], root: i64) -> i64 {
`)
	if region {
		b.WriteString("    return probe::RootCookMeasure(data, vals, nodes, root);\n")
	} else {
		b.WriteString("    return probe::RootCookMeasure();\n")
	}
	b.WriteString(`}

fn cook(data: const u8[:], vals: const schema_rt::LoadVal[:], nodes: const schema_rt::LoadNode[:], root: i64, out: u8[:], big: bool) -> bool {
`)
	if region {
		b.WriteString("    return probe::RootCook(data, vals, nodes, root, out, big);\n")
	} else {
		b.WriteString("    var v = probe::Root { .. };\n    probe::RootCookFromLoaded(v, data, vals, root);\n    return probe::RootCook(v, out, big);\n")
	}
	b.WriteString(`}

fn one(file: const u8[:]) {
    var data: u8[>..] = [];
    if !read_file(file, data) { write_stdout("bad unreadable\n"); return; }
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var report = schema_rt::LoadReport { .. };
    let root = schema_rt::load_table(probe::TableUnit, "Root", data, vals, nodes, report);
    if root < 0 || report.malformed || report.refused { write_stdout("bad load\n"); return; }
    var big = false;
    while true {
        let need = measure(data, vals, nodes, root);
        if need < 0 { write_stdout("bad measure\n"); }
        else {
            var file: u8[>..] = [];
            for i in need { file.push(0xa5); }
            var short_ok = !cook(data, vals, nodes, root, file[0..need - 1], big);
            for x in file { if x != 0xa5 { short_ok = false; } }
            if !short_ok { write_stdout("bad short-capacity\n"); }
            else if !cook(data, vals, nodes, root, file, big) { write_stdout("bad refused\n"); }
            else { write_stdout("ok "); hex(file); }
        }
        if big { break; }
        big = true;
    }
}

fn main() {
`)
	for i := range n {
		fmt.Fprintf(&b, "    one(\"w%d.bin\");\n", i)
	}
	b.WriteString("}\n")
	return b.String()
}

func TestGooseCookWriterMatchesIndependentLayout(t *testing.T) {
	t.Parallel()
	for _, tc := range cookWriteCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "Probe.schema")
			if err := os.WriteFile(path, []byte(tc.schema), 0o644); err != nil {
				t.Fatal(err)
			}
			u, err := compiler.New().Load([]string{path})
			if err != nil {
				t.Fatal(err)
			}
			m := tabletext.NewModel(u)
			files, err := goose.Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			tables, err := goosetable.Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			maps.Copy(files, tables)
			region := strings.Contains(string(files["ProbeCook.goose"]), "fn RootCookMeasure(data: ")
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
				files[fmt.Sprintf("w%d.bin", i)] = wire
				for _, big := range []bool{false, true} {
					cooked, err := tablecook.Cook(m, v, tablecook.Options{Big: big})
					if err != nil {
						t.Fatal(err)
					}
					want = append(want, cooked)
				}
			}
			files["main.goose"] = []byte(cookWriteProgram(region, len(tc.texts)))
			out := gooserun.Run(t, t.TempDir(), "main.goose", files)
			lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
			if len(lines) != len(want) {
				t.Fatalf("the program printed %d lines, want %d:\n%s", len(lines), len(want), out)
			}
			for i, line := range lines {
				name := fmt.Sprintf("text %d, %s", i/2, map[bool]string{false: "little", true: "big"}[i%2 == 1])
				hexed, ok := strings.CutPrefix(line, "ok ")
				if !ok {
					t.Errorf("%s: %s", name, line)
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
					t.Errorf("%s: %d bytes cooked, the tool cooks %d; first difference at byte %d", name, len(got), len(want[i]), at)
				}
			}
		})
	}
}
