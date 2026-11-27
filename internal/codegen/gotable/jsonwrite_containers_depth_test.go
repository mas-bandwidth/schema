package gotable

import (
	"strings"
	"testing"
)

// THE WRITE COUNTS EVERY CONTAINER THE READ DOES (docs/SPEC-TABLES.md §16.3,
// §16.14 W2; the oracle's rows in internal/tabletext/depth_write_test.go): a
// chain of nodes each holding one EMPTY container of one kind (a union's {},
// an enum-keyed array's object, a map's {}, a counted array's [] and a
// list's []) writes and reads back when the last node's container sits at
// depth 128 (127 nodes under the root), and measure and write both refuse (-1)
// at depth 129 (128 nodes). Go refuses in every build. Flags are
// TestGoJsonWriteDepthCountsFlags.
func TestGoJsonWriteDepthCountsEveryContainer(t *testing.T) {
	t.Parallel()
	var probe strings.Builder
	probe.WriteString("package probe\n\nimport (\n\t\"bytes\"\n\t\"testing\"\n)\n")
	for _, node := range []string{"UnionNode", "KeyedNode", "MapNode", "ArrNode", "ListNode"} {
		probe.WriteString(strings.ReplaceAll(containerDepthProbe, "NODE", node))
	}
	runGenerated(t, `package probe

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

table UnionNode
{
    next *UnionNode
    pick Pick
}

table KeyedNode
{
    next  *KeyedNode
    sides [Side]int32
}

table MapNode
{
    next *MapNode
    by   map[uint8]int32
}

table ArrNode
{
    next *ArrNode
    vals [..4]int32
}

table ListNode
{
    next  *ListNode
    items []int32
}
`, probe.String())
}

const containerDepthProbe = `
func TestNODEDepth(t *testing.T) {
	for _, c := range []struct {
		nodes  int
		writes bool
	}{{127, true}, {128, false}} {
		var b NODEBuilder
		if !b.Init() {
			t.Fatal("init")
		}
		cur := b.GetRoot()
		for range c.nodes {
			cur = NODEEmplace(&b.Main, &cur.Next)
			if cur == nil {
				t.Fatal("emplace")
			}
		}
		if !b.Lock() {
			t.Fatal("lock")
		}
		root := b.AsConst()
		n := NODEToJSONMeasure(root)
		text := make([]byte, 1<<20)
		w := NODEToJSON(root, text)
		if !c.writes {
			if n != -1 || w != -1 {
				t.Fatalf("NODE: %d nodes put the last container at depth 129, and the writer did not refuse: measure %d, write %d", c.nodes, n, w)
			}
			b.Shutdown()
			continue
		}
		if n <= 0 || w != n {
			t.Fatalf("NODE: %d nodes, the last container at depth 128, were refused: measure %d, write %d", c.nodes, n, w)
		}
		text = text[:w]
		var back NODEBuilder
		if !back.Init() {
			t.Fatal("init")
		}
		var rep TableReport
		if !NODEFromJSON(&back, text, &rep) || rep.Err() != nil || !back.Lock() {
			t.Fatalf("NODE: the 127-node text did not read back: %+v", rep)
		}
		again := make([]byte, len(text))
		if NODEToJSON(back.AsConst(), again) != int64(len(text)) || !bytes.Equal(again, text) {
			t.Fatal("NODE: the 127-node text did not round-trip")
		}
		back.Shutdown()
		b.Shutdown()
	}
}
`
