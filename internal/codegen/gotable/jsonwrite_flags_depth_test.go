package gotable

import "testing"

// A flags value's text is an array of names, a container the read counts, so
// the write's depth cap counts it too (docs/SPEC-TABLES.md §16.14 W2; the
// oracle's FlagsNode row in internal/tabletext/depth_write_test.go): 127 nodes
// under the root put the last node's flags at depth 128, which writes and
// reads back; 128 nodes put it at depth 129, which the read refuses, so the
// write refuses it too (-1 from measure and write alike).
func TestGoJsonWriteDepthCountsFlags(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe

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
`, flagsDepthProbe)
}

const flagsDepthProbe = `package probe

import (
	"bytes"
	"testing"
)

func TestFlagsDepth(t *testing.T) {
	for _, c := range []struct {
		nodes  int
		writes bool
	}{{127, true}, {128, false}} {
		var b FlagsNodeBuilder
		if !b.Init() {
			t.Fatal("init")
		}
		cur := b.GetRoot()
		cur.Marks = 1
		for range c.nodes {
			cur = FlagsNodeEmplace(&b.Main, &cur.Next)
			if cur == nil {
				t.Fatal("emplace")
			}
			cur.Marks = 1
		}
		if !b.Lock() {
			t.Fatal("lock")
		}
		root := b.AsConst()
		n := FlagsNodeToJSONMeasure(root)
		text := make([]byte, 1<<20)
		w := FlagsNodeToJSON(root, text)
		if !c.writes {
			if n != -1 || w != -1 {
				t.Fatalf("%d nodes put the last flags array at depth 129, and the writer did not refuse: measure %d, write %d", c.nodes, n, w)
			}
			b.Shutdown()
			continue
		}
		if n <= 0 || w != n {
			t.Fatalf("%d nodes, the last flags array at depth 128, were refused: measure %d, write %d", c.nodes, n, w)
		}
		text = text[:w]
		if !bytes.Contains(text, []byte("\"Hot\"")) {
			t.Fatalf("the flags were not written: %s", text)
		}
		var back FlagsNodeBuilder
		if !back.Init() {
			t.Fatal("init")
		}
		var rep TableReport
		if !FlagsNodeFromJSON(&back, text, &rep) || rep.Err() != nil || !back.Lock() {
			t.Fatalf("the 127-node text did not read back: %+v", rep)
		}
		again := make([]byte, len(text))
		if FlagsNodeToJSON(back.AsConst(), again) != int64(len(text)) || !bytes.Equal(again, text) {
			t.Fatal("the 127-node text did not round-trip")
		}
		back.Shutdown()
		b.Shutdown()
	}
}
`
