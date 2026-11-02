package jstable

import (
	"encoding/hex"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestJavaScriptFormsConstruct(t *testing.T) {
	slowtest.Gate(t, "node (it executes declared form exports and closure helpers)")
	for _, mixed := range []bool{false, true} {
		src := `package probe
fixed table Hidden | forms="wire" { n int32 }
table WireOnly | forms="wire" { n int32
 tags map[uint8]int32
}
`
		if mixed {
			src += `
fixed table Visible { n int32 }
table JsonOnly | forms="wire,json" { n int32 }
table CookOnly | forms="wire,cook" { n int32
 child Hidden
}
table All { n int32
 child WireOnly
}
`
		}
		u := unitFrom(t, src)
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		packet, err := js.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		maps.Copy(files, packet)
		type row struct {
			Name  string `json:"name"`
			JSON  bool   `json:"json"`
			Cook  bool   `json:"cook"`
			Block bool   `json:"block"`
			Wire  string `json:"wire"`
			Text  string `json:"text"`
		}
		var rows []row
		model := tabletext.NewModel(u)
		for _, name := range []string{"Hidden", "WireOnly", "Visible", "JsonOnly", "CookOnly", "All"} {
			st := u.Tables[name]
			if st == nil {
				continue
			}
			value := model.New(st)
			var report tabletext.Report
			if !model.Read(value, []byte(`{"n":7}`), &report) {
				t.Fatal(report)
			}
			wire, err := tablewire.Encode(model, value)
			if err != nil {
				t.Fatal(err)
			}
			txt, err := model.Write(value)
			if err != nil {
				t.Fatal(err)
			}
			rows = append(rows, row{name, st.CarriesForm("json"), st.CarriesForm("cook"), name == "Visible", hex.EncodeToString(wire), string(txt) + "\n"})
		}
		var modules []string
		for name := range files {
			if !mixed && (strings.HasSuffix(name, "Block.js") || strings.HasSuffix(name, "Cook.js")) {
				t.Errorf("wire-only emitted %s", name)
			}
			modules = append(modules, name)
		}
		slices.Sort(modules)
		spec := struct {
			Modules []string `json:"modules"`
			Rows    []row    `json:"rows"`
			Mixed   bool     `json:"mixed"`
		}{modules, rows, mixed}
		out := runJSConstructProbe(t, files, runtimeHome(u)+"Variable.js", jsFormsDriver, spec)
		if !strings.Contains(out, "FORMS OK\n") {
			t.Fatalf("missing native form proof: %s", out)
		}
	}
}

const jsFormsDriver = `
import {readFileSync} from "node:fs";
import {pathToFileURL} from "node:url";
const spec=JSON.parse(readFileSync(process.argv[2],"utf8"));
const base=pathToFileURL(process.argv[3]);
const v=await import(base.href);
const all=[];
for(const file of spec.modules){all.push(await import(new URL(file,base).href));}
const has=name=>all.some(m=>Object.hasOwn(m,name));
const expect=(ok,msg)=>{if(!ok){throw new Error(msg);}};
const hex=b=>b===null?"null":Buffer.from(b).toString("hex");
for(const c of spec.rows){
 expect(has(c.name+"Save"),"wire missing "+c.name);
 for(const suffix of ["ToJson","FromJson"]){expect(has(c.name+suffix)===c.json,"JSON export "+c.name+suffix);}
 for(const suffix of ["Cook","CookMeasure","CookMeasureReason","CookFrom"]){expect(has(c.name+suffix)===c.cook,"cook export "+c.name+suffix);}
 expect(has(c.name+"Block")===c.block,"block export "+c.name);
 const value=new v[c.name]();value.n=7;
 expect(hex(v[c.name+"Save"](value))===c.wire,"declared forms moved wire "+c.name);
 const report=new v.TableVariableReport();expect(v[c.name+"Load"](new Uint8Array(Buffer.from(c.wire,"hex")),report)!==null && !report.malformed,"wire load "+c.name);
 if(c.json){expect(v[c.name+"ToJson"](value)===c.text,"JSON closure "+c.name);expect(v[c.name+"FromJson"](c.text,new v.TableVariableReport())!==null,"JSON read "+c.name);}
 if(c.cook){const n=v[c.name+"CookMeasure"](value);const bytes=new Uint8Array(n>0?n:0);expect(n>0 && v[c.name+"CookFrom"](value,bytes,1),"cook write "+c.name);const reader=all.find(m=>m[c.name+"Cook"]!==undefined)[c.name+"Cook"];expect(reader.Open(bytes)!==null,"cook read "+c.name);}
}
// Hidden is retained as a layout dependency of CookOnly; generic public cook
// methods must not re-open the explicitly omitted root surface.
if(spec.mixed){expect(v.TableCookWrite.variableMeasure("Hidden",new v.Hidden())===-1,"generic cook bypass");}
console.log("FORMS OK");
`
