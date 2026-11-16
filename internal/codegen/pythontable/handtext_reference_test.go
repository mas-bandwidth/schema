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
    "github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
    "github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
    "github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
    "github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestPythonHandtextReferences compares the same source and typed output with
// generated Go and C++ readers, including the canonical JSON's final newline.
func TestPythonHandtextReferences(t *testing.T) {
    t.Parallel()
    slowtest.Gate(t, "generated Go and C++ readers and the pinned interpreters")
    const schema = `package probe
fixed table Sample {
 number int32
 title string(16)
}`
    unit := loadUnit(t, schema)
    const input = `(:number 2 :title "Hi")`
    goFiles, err := golang.Generate(unit); if err != nil { t.Fatal(err) }
    surfaces := gotable.DefaultSurfaces(); surfaces.TextForm = true
    tableFiles, err := gotable.GenerateLineageSurface(unit,nil,surfaces); if err != nil { t.Fatal(err) }
    maps.Copy(goFiles,tableFiles)
    runtime := slowtest.SerializeGo(t,"../../../")
    goFiles["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
    goFiles["reference_test.go"] = []byte(`package probe
import ("fmt"; "testing")
func TestReference(t *testing.T) {
 _, value, err := SampleLoadTextForm([]byte(` + "`" + input + "`" + `), TableTextFormDefaultLimits()); if err != nil { t.Fatal(err) }
 n := SampleToJSONMeasure(value); b := make([]byte,n); if SampleToJSON(value,b) != n {t.Fatal("json")}; fmt.Printf("ORACLE:%x\n",b)
}`)
    goDir := t.TempDir(); writeTree(t,goDir,goFiles)
    cmd := exec.Command("go", "test", "-count=1", "-v", "-run", "^TestReference$", "."); cmd.Dir = goDir
    output, err := cmd.CombinedOutput(); if err != nil { t.Fatalf("Go reference: %v\n%s", err, output) }
    var goHex string
    for _, line := range strings.Split(string(output),"\n") { if strings.HasPrefix(line,"ORACLE:") { goHex = strings.TrimPrefix(line,"ORACLE:") } }
    if goHex == "" { t.Fatalf("Go reference absent: %s",output) }

    cppFiles, err := cpp.Generate(unit); if err != nil { t.Fatal(err) }
    cppTables, err := cpptable.Generate(unit); if err != nil { t.Fatal(err) }; maps.Copy(cppFiles,cppTables)
    cppDir := t.TempDir(); writeTree(t,cppDir,cppFiles)
    source := `#include <cstdio>
#include <cstring>
#include <vector>
#include "CoreTable.h"
using namespace probe;
int main() {
 const char *text = R"doc(` + input + `)doc"; Sample v; const char *reason = nullptr;
 if (!SampleLoadTextForm(text, (int64_t)std::strlen(text), v, &reason)) { std::fprintf(stderr, "refused: %s\n", reason ? reason : "?"); return 1; }
 auto n = SampleToJsonMeasure(v); std::vector<char> out((size_t)n + 1);
 if (SampleToJson(v, out.data(), (int64_t)out.size()) != n) return 2;
 return std::fwrite(out.data(), 1, (size_t)n, stdout) == (size_t)n ? 0 : 3;
}`
    path := filepath.Join(cppDir,"main.cpp"); if err := os.WriteFile(path,[]byte(source),0600); err != nil {t.Fatal(err)}
    ser, err := filepath.Abs("../../../../serialize"); if err != nil {t.Fatal(err)}
    bin := filepath.Join(cppDir,"reference")
    cmd = exec.Command("c++","-std=c++17","-O1","-I"+cppDir,"-I"+ser,path,filepath.Join(cppDir,"CoreTable.cpp"),"-o",bin)
    if out, err := cmd.CombinedOutput(); err != nil {t.Fatalf("C++ compile: %v\n%s",err,out)}
    cppJSON, err := exec.Command(bin).CombinedOutput(); if err != nil {t.Fatalf("C++ reference: %v: %s",err,cppJSON)}
    if fmt.Sprintf("%x",cppJSON) != goHex {t.Fatalf("C++ %q disagrees with Go hex %s",cppJSON,goHex)}

    pyFiles, err := Generate(unit); if err != nil {t.Fatal(err)}
    pyDir := t.TempDir(); writeTree(t,pyDir,pyFiles)
    probe := pyrun.ImportPrelude + `from probe import Sample
v = Sample.from_text_form(b'(:number 2 :title "Hi")').value
print(v.to_json().encode().hex())
`
    for _, run := range []func(testing.TB,string,string,map[string][]byte) []byte{pyrun.Run,pyrun.RunFloor} {
        got := bytes.TrimSpace(run(t,pyDir,"main.py",map[string][]byte{"main.py":[]byte(probe)}))
        if string(got) != goHex {t.Fatalf("Python %s disagrees with Go %s and C++",got,goHex)}
    }
}
