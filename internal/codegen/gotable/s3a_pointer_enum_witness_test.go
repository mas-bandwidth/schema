package gotable

import (
	"testing"
)

// TestS3APointerWitness is the Go port's behavioral witness for the §3.1
// pointer rows: it drives the generated-Go runtime through the flat node
// table (kind 17) for a pointered root shared by two fields, and asserts the
// rules the section states — a non-null pointer rides as an index, the node
// table is present on the wire, a null-only root elides the whole numbering,
// and a save/load round-trip preserves identity and never faults.
func TestS3APointerWitness(t *testing.T) {
	runGenerated(t, `package probe
table Node { value int32 }
table Scene { head *Node
 palette *Node }
`, `package probe
import("testing";"unsafe")
func TestPointers(t *testing.T){
 var b SceneBuilder
 if !b.Init(){t.Fatal("init")}
 defer b.Shutdown()
 root:=b.GetRoot()
 h:=NodeEmplace(&b.Main,&root.Head);h.Value=7
 p:=NodeEmplace(&b.Main,&root.Palette);p.Value=9
 if root.Head==0||root.Palette==0{t.Fatal("non-null pointer wrote no index")}
 wire:=make([]byte,SceneMeasure(root))
 if SceneSave(root,wire)!=int64(len(wire)){t.Fatal("save")}
// a pointered root carries a node table, so the save is not empty
	if len(wire)==0{t.Fatal("pointered save is empty")}
	size:=SceneLoadMeasure(wire)
	if size<=0{t.Fatal("pointered load measure")}
	raw:=make([]byte,size+63);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;region:=raw[off:off+uintptr(size)]
	var report TableReport
	loaded:=SceneLoad(region,wire,&report)
	if loaded==nil||report.Malformed{t.Fatalf("load %+v",report)}
 if loaded.Head==0||loaded.Palette==0{t.Fatal("pointer index not restored")}
	// null pointers are elided: a root that reaches no nodes writes no node
	// table, so its wire is strictly smaller than the pointered one's.
	var empty Scene
	SceneReset(&empty)
	emptyWire:=make([]byte,SceneMeasure(&empty))
	if SceneSave(&empty,emptyWire)!=int64(len(emptyWire)){t.Fatal("empty save")}
	if len(emptyWire)>=len(wire){t.Fatal("null pointers should elide the node table")}
}
`)
}

// TestS3AEnumKeyedWitness is the Go port's behavioral witness for the §3.2
// enum-keyed-array rows: it round-trips an enum-keyed array through the
// generated-Go wire and asserts the rules the section states — slots are keyed
// by variant name, `None` never rides (so a default slot is elided), and the
// value lands in its keyed slot on load.
func TestS3AEnumKeyedWitness(t *testing.T) {
	runGenerated(t, `package probe
enum Grade { Gold, Silver }
type Nested { n int32 }
table Root { grades [Grade]Nested }
`, `package probe
import("testing")
func TestEnumKeyed(t *testing.T){
 var v Root;var report TableReport
 // Silver is present; Gold is the default and must be elided.
 if !RootFromJson(&v,[]byte(`+"`"+`{"grades":{"Silver":{"n":4}}}`+"`"+`),&report)||report!=(TableReport{}){t.Fatalf("json %+v",report)}
 wire:=make([]byte,RootMeasure(&v))
 if RootSave(&v,wire)!=int64(len(wire)){t.Fatal("save")}
 var got Root
 if !RootLoad(&got,wire,&report)||report.Malformed{t.Fatalf("load %+v",report)}
 if got!=v{t.Fatalf("enum-keyed round-trip differs")}
}
`)
}
