package jstable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
)

func TestJavaScriptTimestampMessageConstruct(t *testing.T) {
	slowtest.Gate(t, "node (it executes timestamp vocabulary and message batches)")
	u := unitFrom(t, `package probe
 fixed table Clock { at timestamp }
 fixed table Older { other int32 }
 `)
	model := tabletext.NewModel(u)
	var values []*tabletext.Instance
	for _, stamp := range []int64{0, 1, -1, -9223372036854775808, 9223372036854775807} {
		value := model.New(u.Tables["Clock"])
		value.Fields[0].Cell.I = stamp
		values = append(values, value)
	}
	wire, err := tablewire.EncodeMessages(model, values)
	if err != nil {
		t.Fatal(err)
	}
	// A separate writer really declares the timestamp on Older. Its different
	// build makes the unknown field a lawful evolution case for this reader.
	writer := unitFrom(t, `package probe
 fixed table Clock { at timestamp }
 fixed table Older {
   other int32
   at timestamp
 }
 `)
	wm := tabletext.NewModel(writer)
	var oldValues []*tabletext.Instance
	for _, stamp := range []int64{0, 1, -1, -9223372036854775808, 9223372036854775807} {
		v := wm.New(writer.Tables["Older"])
		v.Fields[1].Cell.I = stamp
		oldValues = append(oldValues, v)
	}
	oldWire, err := tablewire.EncodeMessages(wm, oldValues)
	if err != nil {
		t.Fatal(err)
	}
	var vocab tablewire.Vocabulary
	var announced tabletext.Report
	if err := vocab.AnnounceRead(tablewire.Announce(writer), &announced); err != nil {
		t.Fatal(err)
	}
	oldDest := make([]*tabletext.Instance, 5)
	for i := range oldDest {
		oldDest[i] = model.New(u.Tables["Older"])
	}
	var oldReport tabletext.Report
	if n, ok, err := tablewire.DecodeMessages(model, oldDest, oldWire, &vocab, &oldReport); n != 5 || !ok || err != nil || oldReport.Unknown != 4 {
		t.Fatalf("writer evolution: n=%d ok=%v err=%v report=%+v", n, ok, err, oldReport)
	}
	var sameV tablewire.Vocabulary
	if err := sameV.AnnounceRead(tablewire.Announce(u), &announced); err != nil {
		t.Fatal(err)
	}
	if n, ok, err := tablewire.DecodeMessages(model, oldDest, wire, &sameV, &oldReport); n != 0 || ok || !tablewire.ReadRefusal(err) {
		t.Fatalf("own build unknown field must refuse: n=%d ok=%v err=%v", n, ok, err)
	}
	probe := messageProbeHeader(u, u.Tables["Clock"], "") + `
const {Older,OlderLoadMessages}=await import("./ProbeTable.js");
const wire=new Uint8Array(readFileSync("./batch.bin"));
const storage=Array.from({length:5},()=>new Clock());
const report={};
if(ClockLoadMessages(storage,v,wire,report)!==5 || report.Damaged || report.Verdict!==0){fail("timestamp message read "+JSON.stringify(report));}
const expected=[0n,1n,-1n,-9223372036854775808n,9223372036854775807n];
for(let i=0;i<5;i++){if(storage[i].at!==expected[i]){fail("timestamp message value "+i);}}
const n=ClockMeasureMessages(storage,{});const saved=new Uint8Array(n>0?n:0);
if(n!==wire.length || ClockSaveMessages(storage,saved,{})!==n || !bytesEq(saved,wire)){fail("timestamp message bytes");}
const old=Array.from({length:5},()=>new Older());const oldReport={};
const foreign=new TableVocabulary();foreign.Init(new Array(TableMessageEntriesHere));
if(!AnnounceRead(foreign,new Uint8Array(readFileSync("./old-ann.bin")),{})){fail("writer announcement");}
if(OlderLoadMessages(old,foreign,new Uint8Array(readFileSync("./old-batch.bin")),oldReport)!==5 || oldReport.Damaged || oldReport.Unknown!==4){fail("timestamp unknown payload skip "+JSON.stringify(oldReport));}
const forged={};
if(OlderLoadMessages(old,v,wire,forged)!==0 || !forged.Damaged){fail("same-build unknown accepted "+JSON.stringify(forged));}
` + probeTail()
	if out, err := jsRunMessageProbe(t, jsNode(t), u, map[string][]byte{"ann.bin": tablewire.Announce(u), "batch.bin": wire, "old-ann.bin": tablewire.Announce(writer), "old-batch.bin": oldWire}, probe); err != nil {
		t.Fatalf("timestamp messages: %v\n%s", err, out)
	}
}
