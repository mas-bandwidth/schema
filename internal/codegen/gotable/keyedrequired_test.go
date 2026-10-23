package gotable

import (
	"fmt"
	"testing"
)

// TestGoRequiredKeyedArrayRoundTrips covers a variable table holding an
// enum-keyed array with `| required` (docs/SPEC-TABLES.md §2.3, §2.4). The
// message form must compile (the element must not inherit Required, so the
// keyed count loop reads its index) and round-trip through both the message
// form and the table wire form, agreeing with the wire writer's per-element
// elision.
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
	if !RootFromJson(&b,[]byte(%q),&report)||report!=(TableReport{}){t.Fatalf("json %%+v",report)}
	v:=b.GetRoot()

	w:=TableMessageWriter{TableBitWriter:TableBitWriter{Measuring:true}}
	if !RootSaveMessageBody(&w,v){t.Fatal("msg save")}
	w.Align();if w.Overflow{t.Fatal("overflow")}
	size:=w.Bits/8
	out:=make([]byte,2+size);out[0]=2;out[1]=0
	w2:=TableMessageWriter{TableBitWriter:TableBitWriter{Buffer:out[2:]}}
	if !RootSaveMessageBody(&w2,v){t.Fatal("msg save2")}
	w2.Align();if w2.Overflow{t.Fatal("overflow2")}
	out=out[:2+w2.Bits/8]

	ann:=make([]byte,AnnounceMeasure());Announce(ann)
	var vocab TableVocabulary;vocab.Init(make([]TableMessageEntry,TableMessageEntriesHere))
	var ar TableReport
	if !AnnounceRead(&vocab,ann,&ar)||!vocab.Announced{t.Fatalf("announce %%+v",ar)}
	r,count:=tableMessageBatchOpen(&vocab,out,&report)
	if count!=1{t.Fatalf("batch %%d %%+v",count,report)}
	var decoded Root
	if !RootLoadMessageBody(&r,&decoded)||decoded!=*v{t.Fatalf("msg roundtrip got %%+v want %%+v report %%+v",decoded,*v,r.Report)}

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
