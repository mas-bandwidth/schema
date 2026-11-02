package jstable

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestJavaScriptIntegrityConstruct(t *testing.T) {
	slowtest.Gate(t, "node (it executes CRC-32C on file, fixed, and message forms)")
	u := unitFrom(t, `package probe
 fixed table Reading | integrity="crc32c" {
 x int32
 y int16
 label string(8)
 }
 fixed table PlainReading {
 x int32
 y int16
 label string(8)
 }
 table Leaf | integrity="crc32c" { x int32 }
 table Root | integrity="crc32c" { n int32
 child *Leaf
 }
 table PlainRoot { n int32
 child *Leaf
 }
 `)
	model := tabletext.NewModel(u)
	seal := func(wire []byte) string {
		return hex.EncodeToString(binary.LittleEndian.AppendUint32(wire, crc32.Checksum(wire, crc32.MakeTable(crc32.Castagnoli))))
	}
	var variable []string
	for _, input := range []string{`{}`, `{"n":7,"child":{"x":9}}`} {
		value := model.New(u.Tables["Root"])
		var rep tabletext.Report
		if !model.Read(value, []byte(input), &rep) {
			t.Fatal(rep)
		}
		wire, err := tablewire.Encode(model, value)
		if err != nil {
			t.Fatal(err)
		}
		// Encode already appends the declared file CRC. Only the message
		// oracle below still needs its explicit checksum adapter.
		variable = append(variable, hex.EncodeToString(wire))
	}
	var values []*tabletext.Instance
	for i, label := range []string{"ok", "", "eight ch"} {
		v := model.New(u.Tables["Reading"])
		var rep tabletext.Report
		if !model.Read(v, []byte(fmt.Sprintf(`{"x":%d,"y":%d,"label":%q}`, 1000+i, -7*i, label)), &rep) {
			t.Fatal(rep)
		}
		values = append(values, v)
	}
	message, err := tablewire.EncodeMessages(model, values)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := js.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packet)
	spec := struct {
		Variable     []string `json:"variable"`
		Fixed        string   `json:"fixed"`
		PlainFixed   string   `json:"plainFixed"`
		Message      string   `json:"message"`
		Announcement string   `json:"announcement"`
	}{variable, jsIntegrityFixedGolden, jsIntegrityPlainFixedGolden, seal(message), hex.EncodeToString(ir.TableAnnouncement(u))}
	out := runJSConstructProbe(t, files, runtimeHome(u)+"Variable.js", jsIntegrityDriver, spec)
	if !strings.Contains(out, "INTEGRITY OK\n") {
		t.Fatalf("missing native integrity proof: %s", out)
	}
}

// Independent existing generated-Go pins from gotable/integrityfixed_test.go.
const jsIntegrityFixedGolden = "03000000000000006938893b0968270148000000040000007b8c6cd3fc19b9b80d1200000003000000071702864cf563af040400000000000000541502864cf463af0302000000000000003d62cb8fecfcf7390c0c000000000000006938893b09682701e80300000000020000006f6b0000000000006938893b09682701e9030000f9ff0000000000000000000000006938893b09682701ea030000f2ff0800000065696768742063682854cdb3"
const jsIntegrityPlainFixedGolden = "030000000000000064f3fa8daeaf844448000000040000000deeaf527dc1c0970d1200000003000000071702864cf563af040400000000000000541502864cf463af0302000000000000003d62cb8fecfcf7390c0c0000000000000064f3fa8daeaf8444e80300000000020000006f6b00000000000064f3fa8daeaf8444e9030000f9ff00000000000000000000000064f3fa8daeaf8444ea030000f2ff080000006569676874206368"

const jsIntegrityDriver = `
import {readFileSync} from "node:fs";
import {pathToFileURL} from "node:url";
const spec=JSON.parse(readFileSync(process.argv[2],"utf8"));const base=pathToFileURL(process.argv[3]);
const v=await import(base.href);const f=await import(new URL("ProbeTable.js",base).href);
const bytes=s=>new Uint8Array(Buffer.from(s,"hex"));const hex=b=>b===null?"null":Buffer.from(b).toString("hex");
const expect=(ok,msg)=>{if(!ok){throw new Error(msg);}};

for(const wireHex of spec.variable){
 const wire=bytes(wireHex),report=new v.TableVariableReport();const value=v.RootLoad(wire,report);
 expect(value!==null && !report.malformed && report.reason==="","variable integrity load");
 expect(hex(v.RootSave(value))===wireHex && v.RootMeasure(value)===wire.length,"variable integrity writer");
 expect(v.RootLoadMeasure(wire)>0,"integrity region measure");
 const text=v.RootToJson(value);expect(hex(v.RootSave(v.RootFromJson(text,new v.TableVariableReport())))===wireHex,"integrity JSON bridge");
 const retained=new v.TableVariableRetain(new Uint8Array(4096),new Array(64));const rr=new v.TableVariableReport();
 const back=v.RootLoadRetain(wire,retained,rr);expect(back!==null && hex(v.RootSaveRetain(back,retained,rr))===wireHex && v.RootMeasureRetain(back,retained)===wire.length,"integrity retain bridge");
 for(let i=1;i<wire.length;i++){
  const bad=wire.slice();bad[i]^=1;const r=new v.TableVariableReport();
  expect(v.RootLoad(bad,r)===null && r.malformed && r.reason==="integrity_mismatch" && r.unknown===0 && r.kindMismatch===0,"variable damaged byte "+i);
  expect(v.RootLoadMeasure(bad)===-1,"damaged integrity measure");
 }
 const plain=wire.subarray(0,wire.length-4);const r=new v.TableVariableReport();expect(v.RootLoad(plain,r)===null,"unsealed file accepted");
 expect(v.PlainRootLoad(wire,new v.TableVariableReport())===null,"old reader accepted sealed file");
}
const records=["ok","","eight ch"].map((s,i)=>{const x=new f.Reading();f.InitReading(x);x.x=1000+i;x.y=-7*i;x.label.set(Buffer.from(s));x.labelLength=s.length;return x;});
const fixed=bytes(spec.fixed);const output=new Uint8Array(f.ReadingFixedMeasure(3));
expect(f.ReadingFixedSave(records,3,output)===fixed.length && hex(output)===spec.fixed,"fixed sealed Go pin");
expect(f.ReadingFixedMeasure(3)===f.PlainReadingFixedMeasure(3)+4,"fixed trailer measure");
expect(f.ReadingFixedSave(records,3,new Uint8Array(fixed.length-1))===-1,"fixed short writer");
const plainRecords=records.map(x=>Object.assign(new f.PlainReading(),x));const plainOut=new Uint8Array(f.PlainReadingFixedMeasure(3));
expect(f.PlainReadingFixedSave(plainRecords,3,plainOut)===plainOut.length && hex(plainOut)===spec.plainFixed,"plain fixed pin moved");
function loadFixed(wire){const dst=Array.from({length:3},()=>{const x=new f.Reading();f.InitReading(x);x.x=777;return x;});const report=new f.TableFixedReport();const n=f.ReadingFixedLoad(dst,3,wire,wire.length,f.ReadingFixedNewPlan(4096,4096),report);return {n,dst,report};}
const good=loadFixed(fixed);expect(good.n===3 && !good.report.malformed && good.dst[2].x===1002,"fixed integrity load");
for(let i=1;i<fixed.length;i++){
 const bad=fixed.slice();bad[i]^=1;const result=loadFixed(bad);
 expect(result.n===-1 && result.report.malformed && f.TableFixedRefusalName(result.report.refused)==="integrity_mismatch" && result.dst.every(x=>x.x===777),"fixed damaged byte "+i);
}
const vocab=new f.TableVocabulary();vocab.Init(new Array(f.TableMessageEntriesHere));const ar={};expect(f.AnnounceRead(vocab,bytes(spec.announcement),ar),"integrity announcement");
const message=bytes(spec.message);const msgOut=new Uint8Array(f.ReadingMeasureMessages(records,{}));
const wrote=f.ReadingSaveMessages(records,msgOut,{});expect(wrote===message.length && hex(msgOut)===spec.message,"message sealed pin got="+hex(msgOut)+" wrote="+wrote+" want="+spec.message);
expect(f.ReadingSaveMessages(records,new Uint8Array(message.length-1),{})===-1,"message short writer");
function loadMessage(wire){const dst=Array.from({length:3},()=>{const x=new f.Reading();f.InitReading(x);x.x=777;return x;});const report={};const n=f.ReadingLoadMessages(dst,vocab,wire,report);return {n,dst,report};}
expect(loadMessage(message).n===3,"message integrity load");
for(let i=1;i<message.length;i++){
 const bad=message.slice();bad[i]^=1;const result=loadMessage(bad);
 expect(result.n===-1 && result.report.Malformed && result.report.Reason==="integrity_mismatch" && result.report.Verdict===2 && result.dst.every(x=>x.x===777),"message damaged byte "+i);
}
// The form byte's diagnostic has precedence over CRC, including short inputs.
const vr=new v.TableVariableReport();v.RootLoad(new Uint8Array([2]),vr);expect(vr.reason==="message_form_as_file","variable form precedence");
expect(f.TableFixedRefusalName(loadFixed(new Uint8Array([1])).report.refused)==="previous_form","fixed form precedence");
expect(loadMessage(new Uint8Array([1])).report.Reason==="newer_form","message form precedence");
expect(loadFixed(new Uint8Array([3])).report.refused===f.TableFixedRefusal.IntegrityMismatch,"short fixed trailer");
expect(loadMessage(new Uint8Array([2])).report.Reason==="integrity_mismatch","short message trailer");
expect(v.TableIntegritySum(Buffer.from("123456789"),9)===0xe3069283,"standard CRC-32C check value");
console.log("INTEGRITY OK");
`
