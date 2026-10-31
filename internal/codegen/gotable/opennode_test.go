package gotable

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const openNodeUnit = `package opennode

table Graph
{
    a    int32 = 0
    next *Graph
    also *Graph
}
`

// TestPointerNamingAnOpenNodeIsRefusedGo is the Go leg's gate for
// docs/SPEC-TABLES.md §3.1 and §4: no writer produces a cycle, so a pointer
// naming a node whose descent is still open is damage and the whole load is
// refused, by the region load, the retaining load and the builder load alike,
// with no allocation added to the region and retaining loads (their own
// gates). The cyclic wire is a clean chain root -> 2 -> 3 with node 2's
// pointer turned back at node 2 itself. The legal twin shares a CLOSED node
// (root.next and root.also both name node 2), which every load must still
// read. Red if a load hands back the cyclic graph or refuses the share.
func TestPointerNamingAnOpenNodeIsRefusedGo(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, openNodeUnit)
	m := tabletext.NewModel(u)
	encode := func(text string) []byte {
		inst := m.New(m.Lookup("Graph"))
		var r tabletext.Report
		if !m.Read(inst, []byte(text), &r) {
			t.Fatalf("the text did not place: %s", text)
		}
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatal(err)
		}
		return wire
	}
	cyclic := encode(`{"a":5,"next":{"a":6,"next":{"a":7}}}`)
	at := bytes.Index(cyclic, []byte{byte(ir.TableKindPointer), 3})
	if at < 0 || bytes.Count(cyclic, []byte{byte(ir.TableKindPointer), 3}) != 1 {
		t.Fatal("node 2's pointer at node 3 is not where this test expects it")
	}
	cyclic[at+1] = 2
	var report tabletext.Report
	if ok, err := tablewire.Decode(m, m.New(m.Lookup("Graph")), cyclic, &report); ok || err != nil || !report.Malformed {
		t.Fatalf("the engine does not refuse the cycle: ok=%v err=%v %+v", ok, err, report)
	}
	shared := encode(`{"a":5,"next":{"&node":1,"a":6},"also":{"&node":1}}`)
	test := fmt.Sprintf(`package opennode
import("testing";"unsafe")
func aligned(n int)[]byte{raw:=make([]byte,n+63);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;return raw[off:off+uintptr(n)]}
func TestOpenNode(t *testing.T){
 cyclic:=%#v;shared:=%#v
 var r TableReport
 if GraphLoadTolerant(aligned(1<<12),cyclic,&r)!=nil||!r.Malformed{t.Fatalf("cycle: the region load handed back a cyclic graph %%+v",r)}
 r=TableReport{};retain:=TableRetain{Bytes:make([]byte,1024),Ids:make([]TableRetainID,64)}
 if GraphLoadRetain(aligned(1<<12),cyclic,&retain,&r)!=nil||!r.Malformed{t.Fatalf("cycle: the retaining load handed back a cyclic graph %%+v",r)}
 r=TableReport{};var b GraphBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown()
 if GraphLoadBuilderTolerant(&b,cyclic,&r)||!r.Malformed{t.Fatalf("cycle: the builder load did not refuse %%+v",r)}
 r=TableReport{};v:=GraphLoadTolerant(aligned(1<<12),shared,&r)
 if v==nil||r.Malformed{t.Fatalf("share: the region load refused a shared closed node %%+v",r)}
 if GraphMeasure(v)!=int64(len(shared)){t.Fatalf("share: the region load did not keep the share")}
 r=TableReport{};if GraphLoadRetain(aligned(1<<12),shared,&retain,&r)==nil||r.Malformed{t.Fatalf("share: the retaining load refused a shared closed node %%+v",r)}
 r=TableReport{};var c GraphBuilder;if !c.Init(){t.Fatal("init")};defer c.Shutdown()
 if !GraphLoadBuilderTolerant(&c,shared,&r)||r.Malformed{t.Fatalf("share: the builder load refused a shared closed node %%+v",r)}
}
`, cyclic, shared)
	runGenerated(t, openNodeUnit, test)
}
