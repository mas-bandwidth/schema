package pythontable

import (
 "bytes"
 "fmt"
 "maps"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "testing"

 "github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
 "github.com/mas-bandwidth/schema/v2/internal/codegen/cpptable"
 "github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
 "github.com/mas-bandwidth/schema/v2/internal/slowtest"
 "github.com/mas-bandwidth/schema/v2/internal/tabletext"
 "github.com/mas-bandwidth/schema/v2/internal/tablewire"
 "github.com/mas-bandwidth/schema/v2/ir"
)

const requiredForm1Schema = `package req
fixed table Leaf { n int32 }
table Child { n int32 | required }
enum Slot { A, B }
union Choice { x int32 }
table Root {
 number int32 | required
 flag bool | required
 text string(8) | required
 data bytes(8) | required
 items []int32 | required
 pointer_array []*Leaf | required
 pointer *Leaf | required
 blob *string | required
 nested Child | required
 choice Choice | required
 keyed [Slot]int32 | required
 lookup map[string]int32 | required
}
`

// TestPythonForm1Required compares default-valued required fields with both
// live C++ and Go writers, and tests missing required fields under both pins.
func TestPythonForm1Required(t *testing.T) {
 t.Parallel(); slowtest.Gate(t,"C++ and both pinned Python interpreters")
 u:=loadUnit(t,requiredForm1Schema); model:=tabletext.NewModel(u); value:=model.New(model.Lookup("Root")); wire,err:=tablewire.EncodeVariable(model,value); if err!=nil {t.Fatal(err)}
 files,err:=cpp.Generate(u);if err!=nil {t.Fatal(err)}; tables,err:=cpptable.Generate(u);if err!=nil {t.Fatal(err)};maps.Copy(files,tables)
 dir:=t.TempDir();writeTree(t,dir,files);header:="";var sources []string
 for name:=range files {if strings.Contains(name,"/"){continue};if strings.HasSuffix(name,"Table.h"){header=name};if strings.HasSuffix(name,"Table.cpp"){sources=append(sources,filepath.Join(dir,name))}}
 if header=="" || len(sources)==0 {t.Fatal("C++ table source missing")}
 source:=fmt.Sprintf(`#include <cstdio>
#include <vector>
#include "%s"
using namespace req;
int main(){ RootBuilder root; int64_t n=RootMeasure(root); if(n<=0)return 1; std::vector<uint8_t> data((size_t)n); if(RootSave(root,data.data(),n)!=n)return 2; return std::fwrite(data.data(),1,data.size(),stdout)==data.size()?0:3; }
`,header)
 path:=filepath.Join(dir,"probe.cpp");if err:=os.WriteFile(path,[]byte(source),0600);err!=nil {t.Fatal(err)};ser,err:=filepath.Abs("../../../../serialize");if err!=nil {t.Fatal(err)};bin:=filepath.Join(dir,"probe")
 args:=append([]string{"-std=c++17","-O1","-I"+dir,"-I"+ser,path},sources...);args=append(args,"-o",bin);if out,err:=exec.Command("c++",args...).CombinedOutput();err!=nil {t.Fatalf("C++ compile: %v\n%s",err,out)};cppWire,err:=exec.Command(bin).Output();if err!=nil {t.Fatalf("C++ save: %v",err)};if !bytes.Equal(wire,cppWire){t.Fatalf("Go/C++ required bytes differ: %x vs %x",wire,cppWire)}
 py,err:=Generate(u);if err!=nil {t.Fatal(err)}; pyDir:=t.TempDir();writeTree(t,pyDir,py)
 for _,path:=range []string{"req", "req/_schema"} {if out,err:=pyrun.Analyze(t,pyDir,path);err!=nil {t.Fatalf("mypy %s: %v\n%s",path,err,out)}}
 probe:=pyrun.ImportPrelude+`from req import Root, Refused
with open("wire", "rb") as f: expected=f.read()
value=Root()
assert value.save() == expected, "required_writer"
assert value.measure() == len(expected), "required_measure"
loaded=Root.load(expected)
assert loaded.items == [] and loaded.pointer_array == [] and loaded.pointer is None and loaded.blob is None, "required_values"
try:
    Root.load(bytes([1,0])+bytes(8))
except Refused as err:
    assert err.reason == "missing_required", err.reason
else:
    raise AssertionError("missing_required_not_refused")
print("required_ok")
`
 for _,pin:=range []struct{name string; path func(testing.TB)string}{{"current",pyrun.Interpreter},{"floor",pyrun.Floor}} {
  out,stderr,err:=pyrun.Try(t,pin.path(t),pyDir,"main.py",map[string][]byte{"main.py":[]byte(probe),"wire":wire});if err!=nil || out!="required_ok\n" {t.Errorf("%s: %v %s %s",pin.name,err,out,stderr)}
 }
}

func TestPythonForm1RequiredMissing(t *testing.T) {
 t.Parallel(); slowtest.Gate(t,"both pinned Python interpreters")
 u:=loadUnit(t,requiredForm1Schema); files,err:=Generate(u);if err!=nil {t.Fatal(err)};dir:=t.TempDir();writeTree(t,dir,files)
 probe:=pyrun.ImportPrelude+`from req import Root, Refused
try: Root.load(bytes([1,0])+bytes(8))
except Refused as err:
    assert err.reason == "missing_required", err.reason
    assert "number" in str(err) and "items" in str(err)
else: raise AssertionError("missing_required_not_refused")
print("missing_ok")
`
 for _,p:=range []struct{name string;path func(testing.TB)string}{{"current",pyrun.Interpreter},{"floor",pyrun.Floor}} {
  out,stderr,err:=pyrun.Try(t,p.path(t),dir,"main.py",map[string][]byte{"main.py":[]byte(probe)});if err!=nil || out!="missing_ok\n" {t.Errorf("%s: %v %s %s",p.name,err,out,stderr)}
 }
}

func TestPythonForm1RequiredScalarAndNested(t *testing.T) {
 t.Parallel(); slowtest.Gate(t,"both pinned Python interpreters")
 scalar:=loadUnit(t,`package reqscalar
table Simple { count int32 | required }`)
 m:=tabletext.NewModel(scalar); wire,err:=tablewire.EncodeVariable(m,m.New(m.Lookup("Simple")));if err!=nil {t.Fatal(err)}
 files,err:=Generate(scalar);if err!=nil {t.Fatal(err)};dir:=t.TempDir();writeTree(t,dir,files)
 scalarProbe:=pyrun.ImportPrelude+`from reqscalar import Simple, Refused
with open("wire", "rb") as f: wire = f.read()
v=Simple()
assert v.save() == wire, "scalar_required_write"
assert Simple.load(wire).count == 0, "scalar_required_read"
try: Simple.load(bytes([1,0])+bytes(8))
except Refused as err: assert err.reason == "missing_required" and "count" in str(err)
else: raise AssertionError("scalar_missing_required")
print("ok")
`
 for _,p:=range []struct{name string;path func(testing.TB)string}{{"current",pyrun.Interpreter},{"floor",pyrun.Floor}} {out,stderr,err:=pyrun.Try(t,p.path(t),dir,"main.py",map[string][]byte{"main.py":[]byte(scalarProbe),"wire":wire});if err!=nil||out!="ok\n"{t.Errorf("scalar/%s: %v %s %s",p.name,err,out,stderr)}}
 old:=loadUnit(t,`package reqnested
table Child { n int32
other int32 }
table Root { child Child }`)
 newer:=loadUnit(t,`package reqnested
table Child { n int32 | required
other int32 }
table Root { child Child }`)
 oldModel:=tabletext.NewModel(old);oldValue:=oldModel.New(oldModel.Lookup("Root"));var rep tabletext.Report
 if !oldModel.Read(oldValue,[]byte(`{"child":{"other":7}}`),&rep){t.Fatalf("old input refused: %+v",rep)}
 nestedWire,err:=tablewire.EncodeVariable(oldModel,oldValue);if err!=nil{t.Fatal(err)}
 nestedFiles,err:=Generate(newer);if err!=nil{t.Fatal(err)};nestedDir:=t.TempDir();writeTree(t,nestedDir,nestedFiles)
 nestedProbe:=pyrun.ImportPrelude+`from reqnested import Root, Refused
assert Root.load(Root().save()).child.n == 0, "absent_default_child"
with open("wire", "rb") as f: wire = f.read()
try: Root.load(wire)
except Refused as err: assert err.reason == "missing_required" and "n" in str(err)
else: raise AssertionError("nested_missing_required")
print("ok")
`
 for _,p:=range []struct{name string;path func(testing.TB)string}{{"current",pyrun.Interpreter},{"floor",pyrun.Floor}} {out,stderr,err:=pyrun.Try(t,p.path(t),nestedDir,"main.py",map[string][]byte{"main.py":[]byte(nestedProbe),"wire":nestedWire});if err!=nil||out!="ok\n"{t.Errorf("nested/%s: %v %s %s",p.name,err,out,stderr)}}
}

func TestPythonForm1RequiredNegativeControls(t *testing.T) {
 t.Parallel(); slowtest.Gate(t,"both pinned Python interpreters")
 u:=loadUnit(t,requiredForm1Schema); m:=tabletext.NewModel(u); wire,err:=tablewire.EncodeVariable(m,m.New(m.Lookup("Root")));if err!=nil {t.Fatal(err)};files,err:=Generate(u);if err!=nil{t.Fatal(err)}
 probe:=pyrun.ImportPrelude+`from req import Root, Refused
with open("wire", "rb") as f: wire = f.read()
try: actual = Root().save()
except Exception as err: raise AssertionError("required_write") from err
assert actual == wire, "required_write"
try: Root.load(bytes([1,0])+bytes(8))
except Refused as err: assert err.reason == "missing_required", "required_missing"
else: raise AssertionError("required_missing")
print("ok")
`
 controls:=[]struct{name,path,from,to,want string}{
  {"emit", "req/_schema/save.py", "if not field.required and field.is_default(current):", "if field.is_default(current):", "required_write"},
  {"pointer", "req/_schema/kind_pointer.py", "index = 0 if value is None else ctx.indices.get(id(value))", "index = None if value is None else ctx.indices.get(id(value))", "required_write"},
  {"blob", "req/_schema/kind_blob.py", "if value is None:\n            w.varint(ctx.idref(fd.field_id))", "if False:\n            w.varint(ctx.idref(fd.field_id))", "required_write"},
  {"keyed", "req/_schema/kind_keyed.py", "if present == 0 and not fd.required:", "if present == 0:", "required_write"},
  {"missing", "req/_schema/load.py", "    if missing:\n        raise Refused(\"missing_required\", \", \".join(missing))", "    if False:\n        raise Refused(\"missing_required\", \", \".join(missing))", "required_missing"},
 }
 for _,c:=range controls {t.Run(c.name,func(t *testing.T){
  copyFiles:=maps.Clone(files);if strings.Count(string(copyFiles[c.path]),c.from)!=1 {t.Fatalf("%s control anchor moved",c.name)};copyFiles[c.path]=[]byte(strings.Replace(string(copyFiles[c.path]),c.from,c.to,1));dir:=t.TempDir();writeTree(t,dir,copyFiles)
  for _,p:=range []struct{name string;path func(testing.TB)string}{{"current",pyrun.Interpreter},{"floor",pyrun.Floor}} {
   _,stderr,err:=pyrun.Try(t,p.path(t),dir,"main.py",map[string][]byte{"main.py":[]byte(probe),"wire":wire});if err==nil || !strings.Contains(stderr,"AssertionError: "+c.want){t.Errorf("%s/%s did not fail by %s: %v %s",c.name,p.name,c.want,err,stderr)}
  }
 })}
}

func TestPythonForm1RequiredRemoval(t *testing.T) {
 t.Parallel(); slowtest.Gate(t,"both pinned Python interpreters")
 old:=loadUnit(t,`package reqevo
table Root { values []int32 | required }`)
 newer:=loadUnit(t,`package reqevo
table Root { values []int32 }`)
 encode:=func(model *tabletext.Model) []byte {wire,err:=tablewire.EncodeVariable(model,model.New(model.Lookup("Root")));if err!=nil{t.Fatal(err)};return wire}
 prior:=encode(tabletext.NewModel(old)); canonical:=encode(tabletext.NewModel(newer)); if bytes.Equal(prior,canonical){t.Fatal("old required field did not ride")}
 files,err:=Generate(newer);if err!=nil{t.Fatal(err)};dir:=t.TempDir();writeTree(t,dir,files)
 probe:=pyrun.ImportPrelude+`from reqevo import Root
with open("prior", "rb") as f: prior = f.read()
with open("canonical", "rb") as f: canonical = f.read()
v = Root.load(prior)
assert v.values == [], "required_removal_value"
assert v.save() == canonical, "required_removal_resave"
print("ok")
`
 for _,p:=range []struct{name string;path func(testing.TB)string}{{"current",pyrun.Interpreter},{"floor",pyrun.Floor}} {out,stderr,err:=pyrun.Try(t,p.path(t),dir,"main.py",map[string][]byte{"main.py":[]byte(probe),"prior":prior,"canonical":canonical});if err!=nil||out!="ok\n"{t.Errorf("%s: %v %s %s",p.name,err,out,stderr)}}
}

func TestPythonForm1RequiredFastControls(t *testing.T) {
 t.Parallel(); slowtest.Gate(t,"both pinned Python interpreters")
 u:=loadUnit(t,`package reqscalar
table Simple { count int32 | required }`);m:=tabletext.NewModel(u);wire,err:=tablewire.EncodeVariable(m,m.New(m.Lookup("Simple")));if err!=nil{t.Fatal(err)};files,err:=Generate(u);if err!=nil{t.Fatal(err)}
 probe:=pyrun.ImportPrelude+`from reqscalar import Simple, Refused
with open("wire", "rb") as f: wire=f.read()
assert Simple().save() == wire, "required_fast_write"
try: Simple.load(bytes([1,0])+bytes(8))
except Refused as err: assert err.reason == "missing_required", "required_fast_missing"
else: raise AssertionError("required_fast_missing")
print("ok")
`
 controls:=[]struct{name,from,to,want string}{
  {"write", "if fields[0].required or not fields[0].is_default(current):", "if not fields[0].is_default(current):", "required_fast_write"},
  {"missing", "    if missing:\n        raise Refused(\"missing_required\", \", \".join(missing))", "    if False:\n        raise Refused(\"missing_required\", \", \".join(missing))", "required_fast_missing"},
 }
 for _,c:=range controls {t.Run(c.name,func(t *testing.T){copyFiles:=maps.Clone(files);path:="reqscalar/Core.py";if strings.Count(string(copyFiles[path]),c.from)!=1{t.Fatalf("%s anchor moved",c.name)};copyFiles[path]=[]byte(strings.Replace(string(copyFiles[path]),c.from,c.to,1));dir:=t.TempDir();writeTree(t,dir,copyFiles)
  for _,p:=range []struct{name string;path func(testing.TB)string}{{"current",pyrun.Interpreter},{"floor",pyrun.Floor}} {_,stderr,err:=pyrun.Try(t,p.path(t),dir,"main.py",map[string][]byte{"main.py":[]byte(probe),"wire":wire});if err==nil || !strings.Contains(stderr,"AssertionError: "+c.want){t.Errorf("%s/%s did not fail by %s: %v %s",c.name,p.name,c.want,err,stderr)}}
 })}
}

func TestPythonForm1RequiredRange(t *testing.T) {
 t.Parallel(); slowtest.Gate(t,"both pinned Python interpreters")
 u:=loadUnit(t,`package reqrange
table Root { count int32 | min = 1, max = 3, required }`);m:=tabletext.NewModel(u);v:=m.New(m.Lookup("Root"));var rep tabletext.Report;if !m.Read(v,[]byte(`{"count":1}`),&rep){t.Fatalf("Go JSON: %+v",rep)};wire,err:=tablewire.EncodeVariable(m,v);if err!=nil{t.Fatal(err)}
 files,err:=Generate(u);if err!=nil{t.Fatal(err)};dir:=t.TempDir();writeTree(t,dir,files)
 probe:=pyrun.ImportPrelude+`from reqrange import Root, InvalidValue
with open("wire", "rb") as f: wire=f.read()
v=Root()
try: v.save()
except InvalidValue as err: assert "count" in str(err)
else: raise AssertionError("required_range_default")
v.count=1
assert v.save()==wire and Root.load(wire).count==1, "required_range_set"
print("ok")
`
 for _,p:=range []struct{name string;path func(testing.TB)string}{{"current",pyrun.Interpreter},{"floor",pyrun.Floor}} {out,stderr,err:=pyrun.Try(t,p.path(t),dir,"main.py",map[string][]byte{"main.py":[]byte(probe),"wire":wire});if err!=nil||out!="ok\n"{t.Errorf("%s: %v %s %s",p.name,err,out,stderr)}}
}

func TestPythonForm1RequiredGuards(t *testing.T) {
 t.Parallel(); slowtest.Gate(t,"both pinned Python interpreters")
 newer:=loadUnit(t,`package reqguard
table Root { active bool
if active { amount int32 | required } else { fallback int32 | required } }`)
 older:=loadUnit(t,`package reqguard
table Root { active bool
if active { amount int32 } else { fallback int32 } }`)
 encode:=func(u *ir.Unit,text string) []byte {m:=tabletext.NewModel(u);v:=m.New(m.Lookup("Root"));var rep tabletext.Report;if !m.Read(v,[]byte(text),&rep){t.Fatalf("guard input: %+v",rep)};wire,err:=tablewire.EncodeVariable(m,v);if err!=nil{t.Fatal(err)};return wire}
 off:=encode(newer,`{"fallback":0}`);on:=encode(newer,`{"active":true,"amount":0}`);missingOff:=encode(older,`{}`);missingOn:=encode(older,`{"active":true}`)
 files,err:=Generate(newer);if err!=nil{t.Fatal(err)};dir:=t.TempDir();writeTree(t,dir,files)
 probe:=pyrun.ImportPrelude+`from reqguard import Root, Refused

def get(name):
    with open(name, "rb") as f: return f.read()
v=Root()
assert v.save()==get("off") and Root.load(get("off")).active is False, "guard_off"
v.active=True
assert v.save()==get("on") and Root.load(get("on")).active is True, "guard_on"
for name, field in (("missing_off","fallback"),("missing_on","amount")):
    try: Root.load(get(name))
    except Refused as err: assert err.reason == "missing_required" and field in str(err), name
    else: raise AssertionError(name)
print("ok")
`
 for _,p:=range []struct{name string;path func(testing.TB)string}{{"current",pyrun.Interpreter},{"floor",pyrun.Floor}} {out,stderr,err:=pyrun.Try(t,p.path(t),dir,"main.py",map[string][]byte{"main.py":[]byte(probe),"off":off,"on":on,"missing_off":missingOff,"missing_on":missingOn});if err!=nil||out!="ok\n"{t.Errorf("%s: %v %s %s",p.name,err,out,stderr)}}
}

func TestPythonForm1RequiredGuardControls(t *testing.T) {
 t.Parallel(); slowtest.Gate(t,"both pinned Python interpreters")
 u:=loadUnit(t,`package reqguard
table Root { active bool
if active { amount int32 | required } else { fallback int32 | required } }`)
 m:=tabletext.NewModel(u);v:=m.New(m.Lookup("Root"));var rep tabletext.Report;if !m.Read(v,[]byte(`{"active":true,"amount":0}`),&rep){t.Fatalf("guard input: %+v",rep)};wire,err:=tablewire.EncodeVariable(m,v);if err!=nil{t.Fatal(err)};files,err:=Generate(u);if err!=nil{t.Fatal(err)}
 probe:=pyrun.ImportPrelude+`from reqguard import Root
with open("wire", "rb") as f: wire=f.read()
v=Root();v.active=True
try: out=v.save(); loaded=Root.load(wire)
except Exception as err: raise AssertionError("required_guard") from err
assert out==wire and loaded.active is True, "required_guard"
print("ok")
`
 controls:=[]struct{name,path,from,to string}{
  {"fast_write","reqguard/Core.py","if f1.required or not f1.is_default(current):","if not f1.is_default(current):"},
  {"inactive_missing","reqguard/_schema/load.py","field.required and field.guard_holds(obj) and field.field_id not in seen","field.required and field.field_id not in seen"},
 }
 for _,c:=range controls {t.Run(c.name,func(t *testing.T){fs:=maps.Clone(files);if strings.Count(string(fs[c.path]),c.from)!=1{t.Fatalf("%s anchor moved",c.name)};fs[c.path]=[]byte(strings.Replace(string(fs[c.path]),c.from,c.to,1));dir:=t.TempDir();writeTree(t,dir,fs)
  for _,p:=range []struct{name string;path func(testing.TB)string}{{"current",pyrun.Interpreter},{"floor",pyrun.Floor}} {_,stderr,err:=pyrun.Try(t,p.path(t),dir,"main.py",map[string][]byte{"main.py":[]byte(probe),"wire":wire});if err==nil || !strings.Contains(stderr,"AssertionError: required_guard"){t.Errorf("%s/%s did not fail on required guard: %v %s",c.name,p.name,err,stderr)}}
 })}
}
