package jstable

import (
	"encoding/hex"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestJavaScriptTimestampConstruct(t *testing.T) {
	slowtest.Gate(t, "node (it executes strict RFC3339 and timestamp wire/cook)")
	t.Parallel()
	u := unitFrom(t, `package probe
 table Clock {
    at timestamp
 }
`)
	m := tabletext.NewModel(u)
	type row struct {
		Name     string `json:"name"`
		Input    string `json:"input"`
		Text     string `json:"text"`
		Wire     string `json:"wire"`
		Cook     string `json:"cook"`
		OK       bool   `json:"ok"`
		Mismatch int    `json:"mismatch"`
		Reason   string `json:"reason"`
	}
	inputs := []string{}
	for _, stamp := range []string{
		"1970-01-01T00:00:00Z", "1969-12-31T23:59:59.999999999Z",
		"2024-02-29T12:34:05Z", "2000-02-29T00:00:00Z",
		"2026-10-17t12:34:05z", "2026-10-17t12:34:05.5z",
		"2026-10-17T12:34:05.1234567899Z", "2026-10-17T12:34:05." + strings.Repeat("1234567890", 10) + "Z",
		"2026-10-17T18:04:05.123456789+05:30", "2026-10-17T05:34:05-07:00",
		"2026-10-18T12:33:05+23:59", "2026-10-16T12:35:05-23:59", "1970-01-01T00:00:00-00:00",
		"1677-09-21T00:12:43.145224192Z", "2262-04-11T23:47:16.854775807Z",
		"2026-10-17T12:34:05+24:00", "2026-10-17T12:34:05+05:60", "2026-10-17T12:34:05-24:00", "2026-10-17T12:34:05-05:60",
		"2016-12-31T23:59:60Z", "2016-12-31T23:59:60.5Z", "2026-00-17T12:34:05Z", "2026-13-17T12:34:05Z",
		"2026-10-00T12:34:05Z", "2026-10-32T12:34:05Z", "2026-02-30T12:34:05Z", "2026-04-31T12:34:05Z",
		"2025-02-29T12:34:05Z", "1900-02-29T12:34:05Z", "2026-10-17T24:00:00Z", "2026-10-17T12:60:05Z",
		"2026-10-17T12:34:05.Z", "2026-10-17T12:34:05.1.2Z", "2026-10-17T12:34:05,123Z",
		"2026-10-17T12:34:05", "2026-10-17T12:34:05+0530", "2026-10-17T12:34:05+05", "2026-10-17T12:34:05+05:30:00",
		"", "2026-10-17", "2026-10-17T12:34Z", " 2026-10-17T12:34:05Z", "2026-10-17T12:34:05Z ",
		"2026-10-17T12:34:05Z\n", "2026-10-17T12:34:05Zx", "x2026-10-17T12:34:05Z", "2026-10-17 12:34:05Z",
		"20x6-10-17T12:34:05Z", "2026-10-17T1x:34:05Z", "0000-01-01T00:00:00Z", "0099-01-01T00:00:00Z", "9999-12-31T23:59:59Z",
		"1677-09-21T00:12:43.145224191Z", "2262-04-11T23:47:16.854775808Z",
	} {
		inputs = append(inputs, `{"at":`+strconv.Quote(stamp)+`}`)
	}
	inputs = append(inputs, `{}`, `{"at":1}`, `{"at":"1970-01-01T00:00:00.000000001Z"}`)
	var cases []row
	for i, input := range inputs {
		value := m.New(u.Tables["Clock"])
		var rep tabletext.Report
		ok := m.Read(value, []byte(input), &rep)
		c := row{Name: strconv.Itoa(i), Input: input, OK: ok, Mismatch: rep.KindMismatch, Reason: rep.Reason}
		if ok {
			wire, err := tablewire.Encode(m, value)
			if err != nil {
				t.Fatal(err)
			}
			c.Wire = hex.EncodeToString(wire)
			cook, err := tablecook.Cook(m, value, tablecook.Options{})
			if err != nil {
				t.Fatal(err)
			}
			c.Cook = hex.EncodeToString(cook)
			txt, err := m.Write(value)
			if err != nil {
				t.Fatal(err)
			}
			c.Text = string(txt) + "\n"
		}
		cases = append(cases, c)
	}
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := runJSConstructProbe(t, files, runtimeHome(u)+"Variable.js", jsTimestampDriver, cases)
	for _, c := range cases {
		if !strings.Contains(out, c.Name+" OK\n") {
			t.Errorf("missing timestamp case%s\n%s", c.Name, out)
		}
	}
}

const jsTimestampDriver = `
import {readFileSync} from "node:fs";
import {pathToFileURL} from "node:url";
const cases=JSON.parse(readFileSync(process.argv[2],"utf8"));
const m=await import(pathToFileURL(process.argv[3]).href);
const hex=b=>b==null ? "null" : Buffer.from(b).toString("hex");
let failed=false;
for(const c of cases){
 try{
 const r=new m.TableVariableReport();const v=m.ClockFromJson(c.input,r);
 const ok=v!==null;
 if(ok!==c.ok || r.kindMismatch!==c.mismatch || (c.reason && r.reason!==c.reason)){
   throw new Error("JSON verdict "+JSON.stringify({ok,r,want:c}));
 }
 if(ok){
   if(hex(m.ClockSave(v))!==c.wire || m.ClockToJson(v)!==c.text){throw new Error("JSON/wire value mismatch");}
   const wr=new m.TableVariableReport();const back=m.ClockLoad(new Uint8Array(Buffer.from(c.wire,"hex")),wr);
   if(back===null || wr.malformed || wr.kindMismatch!==0 || hex(m.ClockSave(back))!==c.wire){throw new Error("kind36 wire read mismatch");}
   const n=m.ClockCookMeasure(v);const buf=new Uint8Array(n>0?n:0);
   if(n<=0 || !m.ClockCookFrom(v,buf,1) || hex(buf)!==c.cook){throw new Error("timestamp cook mismatch");}
 }
 console.log(c.name+" OK");
 }catch(e){failed=true;console.log(c.name+" DIFF "+e);}
}
// An integer and a timestamp have distinct wire identities; neither widens
// into the other even though both hold an eight-byte payload.
const stamp=cases.find(c=>c.ok && c.wire.startsWith("010124"));
if(stamp===undefined){throw new Error("no active kind36 witness");}
for(const kind of [5,9]){
 const bytes=new Uint8Array(Buffer.from(stamp.wire,"hex"));bytes[2]=kind;
 const report=new m.TableVariableReport();const back=m.ClockLoad(bytes,report);
 if(back===null || report.malformed || report.kindMismatch!==1 || back.at!==0n){failed=true;console.log("integer-to-timestamp mismatch");}
}
if(failed){process.exit(1);}
`

func TestJavaScriptTimestampFixedConstruct(t *testing.T) {
	slowtest.Gate(t, "node (it executes signed timestamp limits on fixed wire)")
	const src = `package probe
fixed table T {
    at timestamp
}
`
	const script = `
for(const stamp of [0n,1n,-1n,-9223372036854775808n,9223372036854775807n]){
 const value=new T();InitT(value);value.at=stamp;
 const wire=new Uint8Array(TMeasure(1));
 if(TSave([value],1,wire)!==wire.length){fail("fixed timestamp write");continue;}
 const back=[new T()];InitT(back[0]);const report=new TableFixedReport();
 const count=TLoad(back,1,wire,wire.length,TNewPlan(4096,4096),report);
 if(count!==1 || report.malformed || report.refused!==0 || report.clamped!==0 || back[0].at!==stamp){fail("fixed timestamp roundtrip "+show(back)+" "+reason(report));}
}
`
	if out, err := jsRunVersionProbe(t, jsNode(t), src, nil, 0, "T", script); err != nil {
		t.Fatalf("fixed timestamp: %v\n%s", err, out)
	}
}
