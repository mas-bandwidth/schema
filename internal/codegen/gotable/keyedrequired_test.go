package gotable

import (
	"fmt"
	"testing"
)

// TestGoRequiredKeyedArrayRoundTrips covers a variable table holding an
// enum-keyed array with `| required` (docs/SPEC-TABLES.md §2.3, §2.4): it
// round-trips through the table wire form, agreeing with the wire writer's
// per-element elision. A fixed table refuses `| required`, so the message form
// never meets one.
func TestGoRequiredKeyedArrayRoundTrips(t *testing.T) {
	t.Parallel()
	const schema = `package probe
enum State { A, B }
table Root {
	k [State]int32 | required
	n int32
}
`
	const text = `{"k":{"A":5},"n":3}`
	gen := fmt.Sprintf(`package probe
import("testing")
func TestRequiredKeyedArrayRoundTrips(t *testing.T){
	var b RootBuilder
	if !b.Init(){t.Fatal("init")};defer b.Shutdown()
	var report TableReport
	if !RootFromJSON(&b,[]byte(%q),&report)||report!=(TableReport{}){t.Fatalf("json %%+v",report)}
	v:=b.GetRoot()

	buf:=make([]byte,RootMeasure(v))
	if RootSave(v,buf)!=int64(len(buf)){t.Fatal("wire save")}
	var g RootBuilder
	if !g.Init(){t.Fatal("init2")};defer g.Shutdown()
	report=TableReport{}
	if !RootLoadBuilderTolerant(&g,buf,&report)||report!=(TableReport{})||*g.GetRoot()!=*v{t.Fatalf("wire got %%+v want %%+v %%+v",*g.GetRoot(),*v,report)}
}
`, text)
	runGenerated(t, schema, gen)
}
