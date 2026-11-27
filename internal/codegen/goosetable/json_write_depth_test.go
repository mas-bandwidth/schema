package goosetable_test

// THE WRITE COUNTS EVERY CONTAINER THE READ DOES (docs/SPEC-TABLES.md §16.2,
// §16.14 W2), held in both Goose text writers: the variable class's
// (runtime/json_value.goose, through json_save_table) and the typed
// accessors' (runtime/schema_json.goose, through <T>ToJson).

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goosetable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// gooseWriteDepthSchema is internal/tabletext's depthWriteUnit: a pointer
// chain whose every node holds one container the writer always emits, an
// EMPTY one at that: an array, a union, a map, an enum-keyed array and a
// flags value. A pointer makes each table the variable class's.
const gooseWriteDepthSchema = `package depthwrite

enum Side
{
    Left
    Right
}

union Pick
{
    a int32
    b int32
}

table ArrNode
{
    next *ArrNode
    vals [..4]int32
}

table UnionNode
{
    next *UnionNode
    pick Pick
}

table MapNode
{
    next *MapNode
    by   map[uint8]int32
}

table KeyedNode
{
    next  *KeyedNode
    sides [Side]int32
}

flags Mark
{
    Hot
    Cold
}

table FlagsNode
{
    next  *FlagsNode
    marks Mark
}
`

var gooseWriteDepthTables = []string{"ArrNode", "UnionNode", "MapNode", "KeyedNode", "FlagsNode"}

// gooseDepthUnit loads src and writes its type and table surfaces into a new
// directory, answering the unit, the directory and every module's name.
func gooseDepthUnit(t *testing.T, src string) (*ir.Unit, string, []string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "unit.schema")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("the unit does not load: %v", err)
	}
	files, err := goose.Generate(u)
	if err != nil {
		t.Fatalf("goose: %v", err)
	}
	tables, err := goosetable.Generate(u)
	if err != nil {
		t.Fatalf("goosetable: %v", err)
	}
	var mods []string
	for _, set := range []map[string][]byte{files, tables} {
		for name, data := range set {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
			if m, ok := strings.CutSuffix(name, ".goose"); ok {
				mods = append(mods, m)
			}
		}
	}
	sort.Strings(mods)
	return u, dir, mods
}

func gooseWriteDepthWant(names []string) string {
	var rows []string
	for _, name := range names {
		rows = append(rows, name+" 127 write=1 read=1", name+" 128 write=0 read=0")
	}
	return strings.Join(rows, "\n")
}

// TestGooseJsonWriteDepthCapVariable: 127 nodes under the root put the last
// node's container at depth 128, which json_save_table writes and
// json_load_table reads back; 128 nodes put it at 129, which the writer
// refuses, because its own reader refuses that text. The chain is built and
// saved by the Go engine, and loaded by the leg's own wire read.
func TestGooseJsonWriteDepthCapVariable(t *testing.T) {
	t.Parallel()
	u, dir, mods := gooseDepthUnit(t, gooseWriteDepthSchema)
	m := tabletext.NewModel(u)
	inputs := map[string][]byte{}
	for _, name := range gooseWriteDepthTables {
		st := m.Lookup(name)
		if st == nil {
			t.Fatalf("the unit declares no %s", name)
		}
		for _, n := range []int{127, 128} {
			root := m.New(st)
			cur := root
			for range n {
				next := m.New(st)
				found := false
				for i := range cur.Fields {
					if cur.Fields[i].Def.Name == "next" {
						cur.Fields[i].Cell.Node = next
						found = true
					}
				}
				if !found {
					t.Fatalf("%s has no field next", name)
				}
				cur = next
			}
			wire, err := tablewire.Encode(m, root)
			if err != nil {
				t.Fatalf("%s %d: the engine could not save the chain: %v", name, n, err)
			}
			inputs[fmt.Sprintf("%s_%d.bin", name, n)] = wire
		}
	}
	ns := goose.Namespace(u)
	var b strings.Builder
	b.WriteString("import os;\n")
	for _, mod := range mods {
		fmt.Fprintf(&b, "import .%s;\n", mod)
	}
	fmt.Fprintf(&b, `
fn row(root: const u8[:], n: const u8[:]) {
    var data: u8[>..] = [];
    if !read_file(str(root, "_", n, ".bin"), data) {
        write_stderr(str("probe: cannot read ", root, " ", n, "\n"));
        exit(1);
    }
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var report = schema_rt::LoadReport { .. };
    let at = schema_rt::load_table(%[1]s::TableUnit, root, data, vals, nodes, report);
    if at < 0 || report.refused || report.damaged {
        write_stderr(str("probe: the wire did not load: ", root, " ", n, "\n"));
        exit(2);
    }
    var text: u8[>..] = [];
    let written = schema_rt::json_save_table(%[1]s::TableUnit, %[1]s::TextUnit, root, data, vals, nodes, at, text);
    var read = 0;
    if written >= 0 {
        var data2: u8[>..] = [];
        var vals2: schema_rt::LoadVal[>..] = [];
        var nodes2: schema_rt::LoadNode[>..] = [];
        var jr = schema_rt::json_report();
        if schema_rt::json_load_table(%[1]s::TableUnit, %[1]s::TextUnit, root, text, data2, vals2, nodes2, jr) >= 0 && !jr.damaged {
            read = 1;
        }
    }
    print(root, " ", n, " write=", if written >= 0 { 1 } else { 0 }, " read=", read);
}

fn main() {
`, ns)
	for _, name := range gooseWriteDepthTables {
		fmt.Fprintf(&b, "    row(%q, \"127\");\n    row(%q, \"128\");\n", name, name)
	}
	b.WriteString("}\n")
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimRight(string(gooserun.Run(t, dir, "main.goose", inputs)), "\n")
	if want := gooseWriteDepthWant(gooseWriteDepthTables); got != want {
		t.Fatalf("the variable text writer's depth cap:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// typedCapSchema is one table the typed accessors carry (json_form.go), one
// field per container its writer emits through runtime/schema_json.goose:
// an array, a union, an enum-keyed array and a flags value.
const typedCapSchema = `package typedcap

enum Side
{
    Left
    Right
}

union Pick
{
    a int32
    b int32
}

flags Mark
{
    Hot
    Cold
}

table R
{
    vals  [..4]int32
    pick  Pick
    sides [Side]int32
    marks Mark
}
`

// TestGooseJsonWriteDepthCapTyped holds the cap in the typed writer. A typed
// table cannot nest deep enough to reach depth 129 in a Goose program (the
// compiler holds a recursive generic to 16 specializations in progress, and
// every nested table's write is one), so the probe calls the table's own
// emitted field writer, _RJsonWrite, at the field depth the record walk hands
// it: each container, the union's None and the flags value's [] included,
// writes at depth 128 and is refused at 129.
func TestGooseJsonWriteDepthCapTyped(t *testing.T) {
	t.Parallel()
	u, dir, mods := gooseDepthUnit(t, typedCapSchema)
	ns := goose.Namespace(u)
	var b strings.Builder
	b.WriteString("import os;\n")
	for _, mod := range mods {
		fmt.Fprintf(&b, "import .%s;\n", mod)
	}
	fields := []string{"vals", "pick", "sides", "marks"}
	b.WriteString("fn main() {\n")
	fmt.Fprintf(&b, "    let v = %s::NewR();\n", ns)
	for ord, name := range fields {
		for _, depth := range []int{128, 129} {
			fmt.Fprintf(&b, "    {\n        var out: u8[>..] = [];\n        let ok = %s::_RJsonWrite(out, v, %d, schema_rt::JsonOpWrite, %d);\n", ns, ord, depth)
			fmt.Fprintf(&b, "        print(%q, \" %d write=\", if ok { 1 } else { 0 });\n    }\n", name, depth)
		}
	}
	b.WriteString("}\n")
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimRight(string(gooserun.Run(t, dir, "main.goose", nil)), "\n")
	var rows []string
	for _, name := range fields {
		rows = append(rows, name+" 128 write=1", name+" 129 write=0")
	}
	if want := strings.Join(rows, "\n"); got != want {
		t.Fatalf("the typed text writer's depth cap:\ngot:\n%s\nwant:\n%s", got, want)
	}
}
