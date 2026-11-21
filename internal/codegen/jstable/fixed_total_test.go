package jstable

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
)

const jsFixedTotalSchema = `package probe
 enum Kind { A, B }
 union Pick { number int32 | min = -3, max = 3
 text string(4) }
 fixed table Row {
 lead int32 = 7
 value int32 | min = 0, max = 10
 items [..2]int16 | min = -2, max = 2
 opt ?int32 | min = 0, max = 10
 kind Kind
 pick Pick
 real float32
 data bytes(3)
 tail uint8
 }
`

// A bad second record must not land the first record or partially write the
// second. Exercise both the in-place identity and shared image paths.
func TestJavaScriptFixedBatchRefusalUntouched(t *testing.T) {
	slowtest.Gate(t, "node (fixed whole-batch CHECK before LAND)")
	t.Parallel()
	files := generatedTree(unitFrom(t, jsFixedTotalSchema))
	for _, shared := range []bool{false, true} {
		t.Run(map[bool]string{false: "identity", true: "shared"}[shared], func(t *testing.T) {
			current := maps.Clone(files)
			if shared {
				editJsFixedTotal(t, current, "const identity = entries === RowFixedIdentity && holeN === 0;", "const identity = false;")
			}
			out, err := runJsFixedTotal(t, current)
			if err != nil || strings.TrimSpace(string(out)) != "ok" {
				t.Fatalf("whole-batch refusal: %v\n%s", err, out)
			}
		})
	}
}

func TestJavaScriptFixedBatchCheckNegativeControl(t *testing.T) {
	slowtest.Gate(t, "node (real generated CHECK phase sabotage)")
	t.Parallel()
	files := generatedTree(unitFrom(t, jsFixedTotalSchema))
	editJsFixedTotal(t, files, "for (let phase = 0; phase < 2; phase++)", "for (let phase = 1; phase < 2; phase++)")
	out, err := runJsFixedTotal(t, files)
	if err == nil || !strings.Contains(string(out), "FAIL: scalar-second") {
		t.Fatalf("skipping real CHECK did not break the scalar oracle: %v\n%s", err, out)
	}
}

func editJsFixedTotal(t *testing.T, files map[string][]byte, from, to string) {
	t.Helper()
	hits := 0
	for name, data := range files {
		count := strings.Count(string(data), from)
		if count == 0 {
			continue
		}
		hits += count
		files[name] = []byte(strings.ReplaceAll(string(data), from, to))
	}
	if hits != 1 {
		t.Fatalf("generated mutation needs one hook, got %d", hits)
	}
}

func runJsFixedTotal(t *testing.T, files map[string][]byte) ([]byte, error) {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.mjs"), []byte(jsFixedTotalProbe), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(jsNode(t), "probe.mjs")
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

const jsFixedTotalProbe = `import * as m from "./ProbeTable.js";
let failures=0;
const fail=(s)=>{console.log("FAIL: "+s);failures++};
const state=(v)=>JSON.stringify(v,(_,x)=>ArrayBuffer.isView(x)?Array.from(x):x);
function poison(v){
 if(ArrayBuffer.isView(v)){v.fill(90);return}
 for(const key of Object.keys(v)){
  const x=v[key];
  if(typeof x==="number")v[key]=90;
  else if(typeof x==="boolean")v[key]=!x;
  else if(x!==null&&typeof x==="object")poison(x);
 }
}
function clean(text=false){
 const values=[new m.Row(),new m.Row()];
 for(let i=0;i<2;i++){
  const v=values[i];v.lead=11+i;v.value=5;v.itemsCount=1;v.items[0]=1;
  v.optPresent=true;v.opt=3;v.kind=1;v.pick.type=text?2:1;v.pick.number=2;
  if(text){v.pick.textLength=1;v.pick.text[0]=97}
  v.real=1.5;v.dataLength=1;v.data[0]=3;v.tail=101+i;
 }
 const bytes=new Uint8Array(m.RowFixedMeasure(2));
 if(m.RowFixedSave(values,2,bytes)!==bytes.length)throw new Error("lawful save");
 return bytes;
}
const base=clean(),textBase=clean(true),first=20+m.RowFixedLayoutBytes+8,second=first+m.RowFixedRecordBytes;
const R=m.TableFixedRefusal;
const cases=[
 ["scalar-second",R.OutOfRange,(b,d)=>d.setInt32(second+4,11,true)],
 ["element-second",R.OutOfRange,(b,d)=>d.setInt16(second+12,3,true)],
 ["count-second",R.CountOverBound,(b,d)=>d.setInt32(second+8,-1,true)],
 ["counted-tail",R.WireDamaged,(b,d)=>d.setInt16(second+14,1,true)],
 ["optional-value",R.OutOfRange,(b,d)=>d.setInt32(second+17,11,true)],
 ["optional-flag",R.WireDamaged,(b)=>{b[second+16]=2}],
 ["optional-dead",R.WireDamaged,(b)=>{b[second+16]=0}],
 ["enum-second",R.OutOfRange,(b)=>{b[second+21]=3}],
 ["union-second",R.OutOfRange,(b)=>{b[second+22]=3}],
 ["union-value",R.OutOfRange,(b,d)=>d.setInt32(second+23,4,true)],
 ["union-dead",R.WireDamaged,(b)=>{b[second+22]=0}],
 ["float-second",R.NonFiniteFloat,(b,d)=>d.setUint32(second+31,0x7fc00000,true)],
 ["bytes-length",R.LengthOverBound,(b,d)=>d.setInt32(second+35,4,true)],
 ["bytes-tail",R.WireDamaged,(b)=>{b[second+40]=1}],
 ["hash-second",R.NoLayout,(b)=>{b[second-8]^=1}],
 ["text-second",R.TextIllFormed,(b)=>{b[second+27]=0xff},true],
];
for(const [name,why,mutate,text] of cases){
 const bytes=Uint8Array.from(text?textBase:base);mutate(bytes,new DataView(bytes.buffer));
 const back=[new m.Row(),new m.Row()];back.forEach(poison);const before=state(back);
 const report=new m.TableFixedReport();
 const n=m.RowFixedLoad(back,2,bytes,bytes.length,m.RowFixedNewPlan(),report);
 if(n!==-1||report.refused!==why||state(back)!==before||report.oor!==0||report.widened!==0)fail(name+" n="+n+" why="+report.refused+" untouched="+(state(back)===before));
}
for(const bytes of [base,textBase]){
 const back=[new m.Row(),new m.Row()],r=new m.TableFixedReport();
 const n=m.RowFixedLoad(back,2,bytes,bytes.length,m.RowFixedNewPlan(),r);
 if(n!==2||r.refused!==0||r.damaged||back[0].lead!==11||back[1].lead!==12||back[1].tail!==102)fail("lawful bounds and neighbors");
 const saved=new Uint8Array(bytes.length);
 if(m.RowFixedSave(back,2,saved)!==bytes.length||!saved.every((x,i)=>x===bytes[i]))fail("lawful byte identity");
}
if(failures)process.exit(1);
console.log("ok");
`
