package tabletext_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// THE WRITE COUNTS EVERY CONTAINER THE READ DOES (docs/SPEC-TABLES.md §16.2,
// §16.14 W2): a chain of nodes, each holding one container of a kind the
// writer always emits, an EMPTY one at that, writes and reads back when the
// last node's container sits at depth 128 (127 nodes under the root), and the
// write refuses naming the cap when it would sit at 129 (128 nodes). Before
// the writer counted table values only, so it wrote the 128 chain and its own
// reader refused the text.
const depthWriteUnit = `package depthwrite

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
`

func TestWriteDepthCapCountsEveryContainer(t *testing.T) {
	m := checkModel(t, depthWriteUnit)
	for _, table := range []string{"ArrNode", "UnionNode", "MapNode", "KeyedNode"} {
		chain := func(nodes int) *tabletext.Instance {
			st := m.Lookup(table)
			root := m.New(st)
			cur := root
			for range nodes {
				next := m.New(st)
				field(t, cur, "next").Cell.Node = next
				cur = next
			}
			return root
		}
		text, err := m.Write(chain(127))
		if err != nil {
			t.Fatalf("%s: 127 nodes under the root, the last container at depth 128, were refused: %v", table, err)
		}
		var rep tabletext.Report
		if !m.Read(m.New(m.Lookup(table)), text, &rep) {
			t.Fatalf("%s: the 127-node text did not read back: %+v", table, rep)
		}
		if _, err := m.Write(chain(128)); err == nil || !strings.Contains(err.Error(), "depth cap") {
			t.Fatalf("%s: 128 nodes under the root put the last container at depth 129, and the writer did not refuse by the cap: %v", table, err)
		}
	}
}
