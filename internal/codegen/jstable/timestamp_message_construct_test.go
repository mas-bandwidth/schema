package jstable

import (
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
	"testing"
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
	probe := messageProbeHeader(u, u.Tables["Clock"], "") + `
const {Older,OlderLoadMessages}=await import("./ProbeTable.js");
const wire=new Uint8Array(readFileSync("./batch.bin"));
const storage=Array.from({length:5},()=>new Clock());
const report={};
if(ClockLoadMessages(storage,v,wire,report)!==5 || report.Malformed || report.Verdict!==0){fail("timestamp message read "+JSON.stringify(report));}
const expected=[0n,1n,-1n,-9223372036854775808n,9223372036854775807n];
for(let i=0;i<5;i++){if(storage[i].at!==expected[i]){fail("timestamp message value "+i);}}
const n=ClockMeasureMessages(storage,{});const saved=new Uint8Array(n>0?n:0);
if(n!==wire.length || ClockSaveMessages(storage,saved,{})!==n || !bytesEq(saved,wire)){fail("timestamp message bytes");}
const old=Array.from({length:5},()=>new Older());const oldReport={};
if(OlderLoadMessages(old,v,wire,oldReport)!==5 || oldReport.Malformed || oldReport.Unknown!==4){fail("timestamp unknown payload skip "+JSON.stringify(oldReport));}
` + probeTail()
	if out, err := jsRunMessageProbe(t, jsNode(t), u, map[string][]byte{"ann.bin": ir.TableAnnouncement(u), "batch.bin": wire}, probe); err != nil {
		t.Fatalf("timestamp messages: %v\n%s", err, out)
	}
}
