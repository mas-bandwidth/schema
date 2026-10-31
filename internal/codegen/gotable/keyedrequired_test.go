package gotable

import (
	"fmt"
	"testing"
)

// TestGoRequiredKeyedArrayRoundTrips covers an enum-keyed array with
// `| required` (docs/SPEC-TABLES.md §2.3, §2.4): in a variable table it
// round-trips through the table wire form, and in a fixed table the message
// form must compile (the element must not inherit Required, so the keyed count
// loop reads its index) and round-trip a batch.
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

	const fixedSchema = `package probe
enum State { A, B }
fixed table Root {
	k [State]int32 | required
	n int32
}
`
	runGenerated(t, fixedSchema, `package probe
import("testing")
func TestRequiredKeyedArrayMessageRoundTrips(t *testing.T){
	var v Root
	v.K[0]=5;v.N=3
	values:=[]*Root{&v}
	n:=RootMeasureMessages(values,nil)
	if n<2{t.Fatalf("measure %d",n)}
	out:=make([]byte,n)
	var wr TableReport
	if RootSaveMessages(values,out,&wr)!=n||wr!=(TableReport{}){t.Fatalf("save %+v",wr)}
	ann:=make([]byte,AnnounceMeasure());Announce(ann)
	var vocab TableVocabulary;vocab.Init(make([]TableMessageEntry,TableMessageEntriesHere))
	var ar TableReport
	if !AnnounceRead(&vocab,ann,&ar)||!vocab.Announced{t.Fatalf("announce %+v",ar)}
	var got [1]Root
	var report TableReport
	if count,ok:=RootLoadMessages(got[:],&vocab,out,&report);!ok||count!=1||got[0]!=v{t.Fatalf("msg roundtrip got %+v want %+v report %+v",got[0],v,report)}
}
`)
}
