package tabletext_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const writerDepthSchema = `package probe

union Pick
{
    n int32
}

flags Marks { A, B }

enum Side { Left, Right }

table MapNode
{
    next *MapNode
    bag  map[uint8]int32
}

table UnionNode
{
    next *UnionNode
    pick Pick
}

table ArrayNode
{
    next *ArrayNode
    nums [..2]int32
}

table KeyedNode
{
    next *KeyedNode
    sides [Side]int32
}

table FlagsNode
{
    next  *FlagsNode
    marks Marks
}

table Root
{
    maps   *MapNode
    unions *UnionNode
    arrays *ArrayNode
    keyed  *KeyedNode
    flags  *FlagsNode
}
`

// THE WRITER REFUSES EXACTLY WHAT THE READER REFUSES (docs/SPEC-TABLES.md
// §16.2): every array and object is one level of the 128 cap, a map's and a
// union's included. A pointer chain puts its last node at depth n, so the
// node's own container sits at n+1: at n = 127 the text is legal and reads
// back, and at n = 128 the writer refuses it rather than writing a text the
// reader refuses.
func TestWriterCountsEveryContainerAsTheReaderDoes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Probe.schema"), []byte(writerDepthSchema), 0o600); err != nil {
		t.Fatal(err)
	}
	m, u := load(t, dir)
	for _, c := range []struct{ field, node string }{
		{"maps", "MapNode"},
		{"unions", "UnionNode"},
		{"arrays", "ArrayNode"},
		{"keyed", "KeyedNode"},
		{"flags", "FlagsNode"},
	} {
		for _, n := range []int{127, 128} {
			root := m.New(u.Tables["Root"])
			var tail *tabletext.Instance
			for i := 0; i < n; i++ {
				node := m.New(m.Lookup(c.node))
				if tail == nil {
					field(t, root, c.field).Cell.Node = node
				} else {
					field(t, tail, "next").Cell.Node = node
				}
				tail = node
			}
			text, err := m.Write(root)
			if err == nil {
				var report tabletext.Report
				if !m.Read(m.New(u.Tables["Root"]), text, &report) {
					t.Errorf("%s, %d nodes: the writer wrote a text its reader refuses (%+v)", c.node, n, report)
				}
			}
			switch {
			case n == 127 && err != nil:
				t.Errorf("%s, %d nodes: the container sits at 128 and must be written: %v", c.node, n, err)
			case n == 128 && (err == nil || !strings.Contains(err.Error(), "depth cap")):
				t.Errorf("%s, %d nodes: the container sits at 129 and must be refused, got %v", c.node, n, err)
			}
		}
	}
}
