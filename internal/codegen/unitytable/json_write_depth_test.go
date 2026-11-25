package unitytable_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
)

// unityJSONWriteDepthUnit is internal/tabletext's depthWriteUnit: a chain of
// nodes, each holding one container of a kind the writer always emits, an
// EMPTY one at that.
const unityJSONWriteDepthUnit = `package depthwrite

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

var unityJSONWriteDepthTables = []string{"ArrNode", "UnionNode", "MapNode", "KeyedNode", "FlagsNode"}

// THE WRITE COUNTS EVERY CONTAINER THE READ DOES (docs/SPEC-TABLES.md §16.2,
// §16.14 W2), in the generated C# (unity): 127 nodes under the root put the
// last node's empty container at depth 128, and the text writes and reads
// back; 128 nodes put it at 129, and ToJson refuses (a negative return, every
// build). The Go oracle's twin is internal/tabletext's
// TestWriteDepthCapCountsEveryContainer.
func TestUnityJSONWriteDepthCapCountsEveryContainer(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString(`using System;
using Depthwrite;

public static class ProbeMain
{
    public static int Main()
    {
        int bad = 0;
`)
	for _, table := range unityJSONWriteDepthTables {
		fmt.Fprintf(&b, `        {
            Func<int, %[1]s> chain = nodes =>
            {
                var root = new %[1]s(); var cur = root;
                for (int i = 0; i < nodes; i++) { var next = new %[1]s(); cur.Next = next; cur = next; }
                return root;
            };
            int before = bad;
            var lawful = chain(127);
            long m = Schema.%[1]sToJsonMeasure(lawful);
            byte[] text = m < 0 ? null : new byte[m];
            long n = text == null ? -1 : Schema.%[1]sToJson(lawful, text);
            if (n < 0) { Console.WriteLine("ROW %[1]s: FAILED 127 nodes were refused"); bad++; }
            else
            {
                var report = new Schema.TableReport();
                if (!Schema.%[1]sFromJson(new %[1]s(), text.AsSpan(0, (int)n), ref report)) { Console.WriteLine("ROW %[1]s: FAILED the 127-node text did not read back"); bad++; }
            }
            var past = chain(128);
            long pm = Schema.%[1]sToJsonMeasure(past);
            if (pm >= 0 && Schema.%[1]sToJson(past, new byte[pm]) >= 0) { Console.WriteLine("ROW %[1]s: FAILED 128 nodes were written"); bad++; }
            if (bad == before) { Console.WriteLine("ROW %[1]s: ok"); }
        }
`, table)
	}
	b.WriteString(`        return bad == 0 ? 0 : 1;
    }
}
`)
	files, err := unityGenerateFiles(fixedproof.ParseCheckUnit(t, unityJSONWriteDepthUnit), nil)
	if err != nil {
		t.Fatal(err)
	}
	out, code, _, runErr := runUnityProbe(t, files, b.String(), nil)
	if runErr != nil {
		t.Fatalf("probe build/exec error: %v\n%s", runErr, out)
	}
	for _, table := range unityJSONWriteDepthTables {
		if !strings.Contains(out, "ROW "+table+": ok") {
			t.Errorf("%s is not held (exit %d):\n%s", table, code, out)
		}
	}
	if code != 0 {
		t.Errorf("probe exited %d:\n%s", code, out)
	}
}
