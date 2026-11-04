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
 if GraphLoad(aligned(1<<12),cyclic,&r)!=nil||!(r.Refusal.Error() == "wire_damaged" || r.Refusal.Error() == "malformed"){t.Fatalf("cycle: the region load handed back a cyclic graph %%+v",r)}
 r=TableReport{};retain:=TableRetain{Bytes:make([]byte,1024),Ids:make([]TableRetainID,64)}
 if GraphLoadRetain(aligned(1<<12),cyclic,&retain,&r)!=nil||!(r.Refusal.Error() == "wire_damaged" || r.Refusal.Error() == "malformed"){t.Fatalf("cycle: the retaining load handed back a cyclic graph %%+v",r)}
 r=TableReport{};var b GraphBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown()
 if GraphLoadBuilder(&b,cyclic,&r)||!(r.Refusal.Error() == "wire_damaged" || r.Refusal.Error() == "malformed"){t.Fatalf("cycle: the builder load did not refuse %%+v",r)}
 r=TableReport{};v:=GraphLoad(aligned(1<<12),shared,&r)
 if v==nil||(r.Refusal.Error() == "wire_damaged" || r.Refusal.Error() == "malformed"){t.Fatalf("share: the region load refused a shared closed node %%+v",r)}
 if GraphMeasure(v)!=int64(len(shared)){t.Fatalf("share: the region load did not keep the share")}
 r=TableReport{};if GraphLoadRetain(aligned(1<<12),shared,&retain,&r)==nil||(r.Refusal.Error() == "wire_damaged" || r.Refusal.Error() == "malformed"){t.Fatalf("share: the retaining load refused a shared closed node %%+v",r)}
 r=TableReport{};var c GraphBuilder;if !c.Init(){t.Fatal("init")};defer c.Shutdown()
 if !GraphLoadBuilder(&c,shared,&r)||(r.Refusal.Error() == "wire_damaged" || r.Refusal.Error() == "malformed"){t.Fatalf("share: the builder load refused a shared closed node %%+v",r)}
}
`, cyclic, shared)
	runGenerated(t, openNodeUnit, test)
}

// TestGoCycleWalkCoversStoredGraph covers the graph the loader actually
// placed, including guards, disconnected records and container slots.
func TestGoCycleWalkCoversStoredGraph(t *testing.T) {
	t.Parallel()
	const src = `package graphcheck
table Graph {
 a int32
 x float32
 on bool
 next *Graph
 also *Graph
 if on { side *Graph }
 pointers []*Graph
 mapping map[int32]*Graph
 pair [2]*Graph
}
`
	u := unitFrom(t, src)
	m := tabletext.NewModel(u)
	encode := func(text string) []byte {
		v := m.New(m.Lookup("Graph"))
		var r tabletext.Report
		if !m.Read(v, []byte(text), &r) {
			t.Fatalf("text %s: %+v", text, r)
		}
		b, e := tablewire.Encode(m, v)
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	patch := func(b []byte, from, to byte) {
		p := []byte{byte(ir.TableKindPointer), from}
		if bytes.Count(b, p) != 1 {
			t.Fatalf("pointer %d does not occur once", from)
		}
		b[bytes.Index(b, p)+1] = to
	}
	type vector struct {
		name string
		wire []byte
		good bool
	}
	var vectors []vector
	for _, tc := range []struct{ name, text string }{
		{"direct", `{"a":9,"next":{"a":8,"next":{"a":7}}}`},
		{"list", `{"pointers":[{"pointers":[{}]}]}`},
		{"map", `{"mapping":{"1":{"mapping":{"2":{}}}}}`},
		{"array", `{"pair":[{"pair":[{},null]},null]}`},
		{"guard", `{"next":{"on":true,"side":{}}}`},
	} {
		wire := encode(tc.text)
		if tc.name == "guard" {
			found := false
			for i := 0; i+1 < len(wire); i++ {
				if wire[i] != byte(ir.TableKindBool) || wire[i+1] != 1 {
					continue
				}
				probe := append([]byte(nil), wire...)
				probe[i+1] = 0
				v := m.New(m.Lookup("Graph"))
				var r tabletext.Report
				ok, e := tablewire.Decode(m, v, probe, &r)
				if !ok || e != nil || r.Malformed {
					continue
				}
				next, _ := v.FieldByKey("next")
				if next.Cell.Node == nil {
					continue
				}
				on, _ := next.Cell.Node.FieldByKey("on")
				side, _ := next.Cell.Node.FieldByKey("side")
				if !on.Cell.B && side.Cell.Node != nil {
					wire = probe
					found = true
					break
				}
			}
			if !found {
				t.Fatal("guard byte not found")
			}
		}
		vectors = append(vectors, vector{tc.name + " legal", append([]byte(nil), wire...), true})
		if tc.name == "list" || tc.name == "array" {
			count := byte(1)
			if tc.name == "array" {
				count = 2
			}
			p := []byte{byte(ir.TableKindPointer), count, 3}
			if bytes.Count(wire, p) != 1 {
				t.Fatalf("%s container pointer not unique", tc.name)
			}
			wire[bytes.Index(wire, p)+2] = 2
		} else {
			patch(wire, 3, 2)
		}
		vectors = append(vectors, vector{tc.name + " cycle", wire, false})
	}
	unreached := encode(`{"next":{"next":{}}}`)
	patch(unreached, 2, 0)
	vectors = append(vectors, vector{"unreached legal", append([]byte(nil), unreached...), true})
	patch(unreached, 3, 2)
	vectors = append(vectors, vector{"unreached cycle", unreached, false})
	// A long chain is not a Go stack depth; a diamond is not a path count.
	graph := func(n int, diamond bool) []byte {
		nodes := make([]*tabletext.Instance, n)
		for i := range nodes {
			nodes[i] = m.New(m.Lookup("Graph"))
		}
		for i := 0; i+1 < n; i++ {
			f, _ := nodes[i].FieldByKey("next")
			f.Cell.Node = nodes[i+1]
			if diamond && i+2 < n {
				f, _ = nodes[i].FieldByKey("also")
				f.Cell.Node = nodes[i+2]
			}
		}
		b, e := tablewire.Encode(m, nodes[0])
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	vectors = append(vectors, vector{"chain", graph(2048, false), true}, vector{"diamond", graph(82, true), true})
	finite := encode(`{"a":9,"next":{"x":2.5}}`)
	pattern := []byte{0, 0, 0x20, 0x40}
	if bytes.Count(finite, pattern) != 1 {
		t.Fatal("float pattern not unique")
	}
	finite = bytes.Replace(finite, pattern, []byte{0, 0, 0x80, 0x7f}, 1)
	var cases bytes.Buffer
	for _, v := range vectors {
		fmt.Fprintf(&cases, "{name:%q,wire:%#v,good:%v},\n", v.name, v.wire, v.good)
	}
	test := fmt.Sprintf(`package graphcheck
import("testing";"unsafe";"runtime/debug";"bytes")
func aligned(n int)[]byte{raw:=make([]byte,n+63);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;return raw[off:off+uintptr(n)]}
func TestStoredGraph(t *testing.T){
 old:=debug.SetMaxStack(128<<10);defer debug.SetMaxStack(old)
 for _,tc:=range []struct{name string;wire []byte;good bool}{%s}{
  size:=GraphLoadMeasure(tc.wire);if size<0{t.Fatalf("%%s measure",tc.name)}
  region:=aligned(int(size));var r TableReport
  v:=GraphLoad(region,tc.wire,&r)
  if (v!=nil)!=tc.good||(r.Refusal.Error() == "wire_damaged" || r.Refusal.Error() == "malformed")==tc.good{t.Fatalf("%%s region: %%+v",tc.name,r)}
  if !tc.good&&!bytes.Equal(region,make([]byte,len(region))){t.Fatalf("%%s region not discarded",tc.name)}
  if tc.good {
   rr:=TableReport{};if n:=testing.AllocsPerRun(10,func(){rr=TableReport{};if GraphLoad(region,tc.wire,&rr)==nil{panic("load")}});n!=0{t.Fatalf("%%s region allocated %%v",tc.name,n)}
   if tc.name=="direct legal"||tc.name=="list legal"||tc.name=="map legal"||tc.name=="array legal" {
    out:=make([]byte,len(tc.wire));if GraphSave(v,out)!=int64(len(out))||!bytes.Equal(out,tc.wire){t.Fatalf("%%s resolved references changed wire",tc.name)}
   }
  }
  retain:=TableRetain{Bytes:make([]byte,4096),Ids:make([]TableRetainID,1024)}
  r=TableReport{};v=GraphLoadRetain(region,tc.wire,&retain,&r)
  if (v!=nil)!=tc.good||(r.Refusal.Error() == "wire_damaged" || r.Refusal.Error() == "malformed")==tc.good{t.Fatalf("%%s retain: %%+v",tc.name,r)}
  if !tc.good&&(retain.Used!=0||retain.Count!=0){t.Fatalf("%%s retain not discarded",tc.name)}
  var b GraphBuilder;if !b.Init(){t.Fatal("init")}
  r=TableReport{};ok:=GraphLoadBuilder(&b,tc.wire,&r)
  if ok!=tc.good||(r.Refusal.Error() == "wire_damaged" || r.Refusal.Error() == "malformed")==tc.good{t.Fatalf("%%s builder: %%+v",tc.name,r)}
  b.Shutdown()
 }
}

func TestRefusedGraphDoesNotExposeIndices(t *testing.T){
 wire:=%#v;size:=GraphLoadMeasure(wire);if size<0{t.Fatal("measure")}
 region:=aligned(int(size));var r TableReport
 if GraphLoad(region,wire,&r)!=nil||!r.Refused(){t.Fatalf("non-finite region: %%+v",r)}
 if !bytes.Equal(region,make([]byte,len(region))){t.Fatal("non-finite region kept indices")}
 retain:=TableRetain{Bytes:make([]byte,4096),Ids:make([]TableRetainID,1024)}
 r=TableReport{};if GraphLoadRetain(region,wire,&retain,&r)!=nil||!r.Refused()||retain.Used!=0||retain.Count!=0{t.Fatalf("non-finite retain: %%+v",r)}
 var b GraphBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown()
 b.GetRoot().A=99;r=TableReport{}
 if GraphLoadBuilder(&b,wire,&r)||!r.Refused(){t.Fatalf("non-finite builder: %%+v",r)}
 if b.GetRoot().Next!=0||b.GetRoot().A!=0{t.Fatal("non-finite builder kept indices")}
}
`, cases.String(), finite)
	runGenerated(t, src, test)
}
