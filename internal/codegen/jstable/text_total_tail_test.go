package jstable

import (
	"encoding/json"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

func TestJavaScriptTextTotalTailConstruct(t *testing.T) {
	slowtest.Gate(t, "native Go and Node (discarded total tails)")
	u := unitFrom(t, `package probe
 table Wide { a [..4]string(16)
 b [..4]?string(16)
 c []string(16)
 d []?string(16)
 }
 table Totals { a [..3]string(5) | total=5
 b [..3]?string(5) | total=5
 c []string(5) | total=5
 d []?string(5) | total=5
 }
 `)
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tf, err := gotable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tf)
	dir := t.TempDir()
	runtime := slowtest.SerializeGo(t, "../../../")
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	files["oracle_test.go"] = []byte(jsTextTotalTailGo)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := testproc.Command(t, "go", "test", "-count=1", "-timeout=0", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Go tail oracle: %v\n%s", err, out)
	}
	data, err := os.ReadFile(filepath.Join(dir, "oracle.json"))
	if err != nil {
		t.Fatal(err)
	}
	var spec any
	if err = json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	jsfiles, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := runJSConstructProbe(t, jsfiles, runtimeHome(u)+"Variable.js", jsTextTotalTailDriver, spec)
	if !strings.Contains(out, "TOTAL TAIL OK\n") {
		t.Fatal(out)
	}
}

const jsTextTotalTailGo = `package probe
import("bytes";"encoding/hex";"encoding/json";"fmt";"os";"testing";"strings")
func TestTail(t *testing.T){
 type row struct{Input,Wire,Text string;WireRead,JSONRead bool}
 var rows []row
 for _,field:=range []string{"a","b","c","d"}{
  for _,tail:=range []string{"FIT","toolong","BAD","COUNT","BODY"}{
   if tail=="BODY"&&field!="b"&&field!="d"{continue}
   textTail:=tail;if tail=="BODY"{textTail="BAD"}
   input:=fmt.Sprintf("{%q:[%q,%q,%q]}",field,"abcde","xyz",textTail)
   if tail=="FIT"{input=fmt.Sprintf("{%q:[%q,%q]}",field,"abc","de")}
   if tail=="COUNT"{input=fmt.Sprintf("{%q:[%q,%q,%q,%q]}",field,"ééé","a","z","tail")}
   var source WideBuilder;source.Init();var r TableReport
   if !WideFromJSON(&source,[]byte(input),&r){t.Fatal(r)}
   size:=WideMeasure(source.GetRoot(),&source.Arena);wire:=make([]byte,size);if WideSave(source.GetRoot(),wire,&source.Arena)!=size{t.Fatal("save")};source.Shutdown()
   if tail=="BAD"{at:=bytes.Index(wire,[]byte("BAD"));if at<0{t.Fatal("marker")};wire[at]=0;input=fmt.Sprintf("{%q:[%q,%q,%q]}",field,"abcde","xyz","\x00AD")}
   if tail=="BODY"{at:=bytes.Index(wire,[]byte("BAD"));if at<0{t.Fatal("marker")};wire[at+3]=255}
   input=strings.ReplaceAll(input,"\\x00","\\u0000")
   var loaded TotalsBuilder;if !loaded.Init(){t.Fatal("load builder")};r=TableReport{};wireRead:=TotalsLoadBuilder(&loaded,wire,&r);var text []byte
   if wireRead { if !loaded.Lock(){t.Fatal("load lock")};v:=loaded.AsConst();n:=TotalsToJSONMeasure(v);text=make([]byte,n);if TotalsToJSON(v,text)!=n{t.Fatal("text")} };loaded.Shutdown()
   var dest TotalsBuilder;dest.Init();r=TableReport{};jsonRead:=TotalsFromJSON(&dest,[]byte(input),&r);dest.Shutdown()
   if tail=="FIT" && (!wireRead || !jsonRead){t.Fatalf("valid control %s: wire=%v JSON=%v",field,wireRead,jsonRead)}
   if tail!="FIT" && (wireRead || jsonRead){t.Fatalf("strict refusal %s %s: wire=%v JSON=%v",field,tail,wireRead,jsonRead)}
   rows=append(rows,row{input,hex.EncodeToString(wire),string(text),wireRead,jsonRead})
  }
 }
 out,err:=json.Marshal(rows);if err!=nil{t.Fatal(err)};if err=os.WriteFile("oracle.json",out,0600);err!=nil{t.Fatal(err)}
}
`
const jsTextTotalTailDriver = `
import{readFileSync}from"node:fs";import{pathToFileURL}from"node:url";
const spec=JSON.parse(readFileSync(process.argv[2],"utf8")),m=await import(pathToFileURL(process.argv[3]).href);
for(const c of spec){for(const mode of ["wire","json"]){
 const r=new m.TableVariableReport(),v=mode==="wire"?m.TotalsLoad(new Uint8Array(Buffer.from(c.Wire,"hex")),r):m.TotalsFromJson(c.Input,r);
 const want=mode==="wire"?c.WireRead:c.JSONRead;
 if((v!==null)!==want)throw new Error(mode+" verdict "+JSON.stringify(r));
 if(v!==null&&(r.damaged||m.TotalsToJson(v).trim()!==c.Text.trim()))throw new Error(mode+" valid text "+JSON.stringify(r));
}}
console.log("TOTAL TAIL OK");
`
