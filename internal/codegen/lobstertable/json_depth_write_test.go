package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// lobsterDepthWriteUnit is internal/tabletext/depth_write_test.go's
// depthWriteUnit, package and all.
const lobsterDepthWriteUnit = `package depthwrite

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

// TestLobsterJsonWriteDepthCountsEveryContainer holds json_write to the cap
// the read counts (docs/SPEC-TABLES.md §16.2, §16.14 W2): a chain of nodes,
// each holding one EMPTY container of a kind the writer always emits (an
// array, a union's object, a map's object, an enum-keyed array's object, a
// flags value's array), writes and reads back with 127 nodes under the root,
// the last container at depth 128, and json_write refuses 128 nodes, the
// last one's container at 129. The chains are built by the Go engine and
// handed to Lobster as wire.
func TestLobsterJsonWriteDepthCountsEveryContainer(t *testing.T) {
	t.Parallel()
	g, err := generateLoadUnit(t, []string{writeSchema(t, "DepthWrite", lobsterDepthWriteUnit)})
	if err != nil {
		t.Fatalf("the unit is refused: %v", err)
	}
	m := tabletext.NewModel(g.u)
	tables := []string{"ArrNode", "UnionNode", "MapNode", "KeyedNode", "FlagsNode"}
	inputs := map[string][]byte{}
	var p strings.Builder
	for _, mod := range g.mods {
		fmt.Fprintf(&p, "import .%s\n", mod)
	}
	p.WriteString("import .load\nimport .json\nimport .strict_and_refusals\n")
	p.WriteString(keyedRegistrations(g.u))
	var rows []string
	for _, table := range tables {
		for _, n := range []int{127, 128} {
			name := fmt.Sprintf("%s-%d", table, n)
			inputs[name] = lobsterDepthChainWire(t, m, table, n)
			i := len(rows)
			rows = append(rows, name)
			fmt.Fprintf(&p, "let d%d = read_file(%s)\nassert d%d\n", i, lobster.StringLit([]byte(name)), i)
			fmt.Fprintf(&p, "let v%d, w%d = schema_rt.load_strict(%s, d%d, schema_rt.LoadReport {})\n", i, i, lobster.StringLit([]byte(table)), i)
			fmt.Fprintf(&p, "if not v%d:\n    print(%s + \" load refused: \" + w%d)\nelse:\n", i, lobster.StringLit([]byte(name)), i)
			fmt.Fprintf(&p, "    let t%d, j%d = schema_rt.json_write(v%d)\n", i, i, i)
			fmt.Fprintf(&p, "    if j%d != \"\" or length(t%d) == 0:\n        print(%s + \" refused\")\n", i, i, lobster.StringLit([]byte(name)))
			fmt.Fprintf(&p, "    elif schema_rt.json_read(%s, t%d, schema_rt.LoadReport {}):\n        print(%s + \" wrote read\")\n", lobster.StringLit([]byte(table)), i, lobster.StringLit([]byte(name)))
			fmt.Fprintf(&p, "    else:\n        print(%s + \" wrote unread\")\n", lobster.StringLit([]byte(name)))
		}
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(p.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.Split(strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs))), "\n")
	if len(out) != len(rows) {
		t.Fatalf("the program printed %d lines for %d rows:\n%s", len(out), len(rows), strings.Join(out, "\n"))
	}
	for i, name := range rows {
		want := name + " wrote read"
		if strings.HasSuffix(name, "-128") {
			want = name + " refused"
		}
		if out[i] != want {
			t.Errorf("got %q, want %q", out[i], want)
		}
	}
}

// lobsterDepthChainWire is the engine's wire for a chain of n nodes of the
// table under the root, each linked by `next`, every other field its default.
func lobsterDepthChainWire(t *testing.T, m *tabletext.Model, table string, n int) []byte {
	t.Helper()
	st := m.Lookup(table)
	root := m.New(st)
	cur := root
	for range n {
		child := m.New(st)
		var next *tabletext.Field
		for j := range cur.Fields {
			if cur.Fields[j].Def.Name == "next" {
				next = &cur.Fields[j]
			}
		}
		if next == nil {
			t.Fatalf("%s has no next field", table)
		}
		next.Cell.Node = child
		cur = child
	}
	data, err := tablewire.Encode(m, root)
	if err != nil {
		t.Fatalf("%s: the engine could not save a chain of %d: %v", table, n, err)
	}
	return data
}
