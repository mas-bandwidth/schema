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

// TestPythonHandtextReferencesFunctional compares the same source and typed output with
// generated Go and C++ readers, including the canonical JSON's final newline.
func TestPythonHandtextReferencesFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "generated Go and C++ readers and the pinned interpreters")
	const schema = `package probe
fixed table Sample {
 number int32
 title string(16)
}`
	unit := loadUnit(t, schema)
	const input = `(:number 2 :title "Hi")`
	goFiles, err := golang.Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	surfaces := gotable.DefaultSurfaces()
	surfaces.TextForm = true
	tableFiles, err := gotable.GenerateLineageSurface(unit, nil, surfaces)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(goFiles, tableFiles)
	runtime := slowtest.SerializeGo(t, "../../../")
	goFiles["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	goFiles["reference_test.go"] = []byte(`package probe
import ("fmt"; "testing")
func TestReference(t *testing.T) {
 _, value, err := SampleLoadTextForm([]byte(` + "`" + input + "`" + `), TableTextFormDefaultLimits()); if err != nil { t.Fatal(err) }
 n := SampleToJSONMeasure(value); b := make([]byte,n); if SampleToJSON(value,b) != n {t.Fatal("json")}; fmt.Printf("ORACLE:%x\n",b)
}`)
	goDir := t.TempDir()
	writeTree(t, goDir, goFiles)
	cmd := exec.Command("go", "test", "-count=1", "-v", "-run", "^TestReference$", ".")
	cmd.Dir = goDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Go reference: %v\n%s", err, output)
	}
	var goHex string
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "ORACLE:") {
			goHex = strings.TrimPrefix(line, "ORACLE:")
		}
	}
	if goHex == "" {
		t.Fatalf("Go reference absent: %s", output)
	}

	cppFiles, err := cpp.Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	cppTables, err := cpptable.Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(cppFiles, cppTables)
	cppDir := t.TempDir()
	writeTree(t, cppDir, cppFiles)
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
	path := filepath.Join(cppDir, "main.cpp")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	ser, err := filepath.Abs("../../../../serialize")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(cppDir, "reference")
	cmd = exec.Command("c++", "-std=c++17", "-O1", "-I"+cppDir, "-I"+ser, path, filepath.Join(cppDir, "CoreTable.cpp"), "-o", bin)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("C++ compile: %v\n%s", err, out)
	}
	cppJSON, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("C++ reference: %v: %s", err, cppJSON)
	}
	if fmt.Sprintf("%x", cppJSON) != goHex {
		t.Fatalf("C++ %q disagrees with Go hex %s", cppJSON, goHex)
	}

	pyFiles, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	pyDir := t.TempDir()
	writeTree(t, pyDir, pyFiles)
	probe := pyrun.ImportPrelude + `from probe import Sample
v = Sample.from_text_form(b'(:number 2 :title "Hi")').value
print(v.to_json().encode().hex())
`
	for _, run := range []func(testing.TB, string, string, map[string][]byte) []byte{pyrun.Run, pyrun.RunFloor} {
		got := bytes.TrimSpace(run(t, pyDir, "main.py", map[string][]byte{"main.py": []byte(probe)}))
		if string(got) != goHex {
			t.Fatalf("Python %s disagrees with Go %s and C++", got, goHex)
		}
	}
}

// TestPythonHandtextGoCorpusFunctional compares every positive contract value
// and canonical JSON byte with the generated Go reader of the same input.
func TestPythonHandtextGoCorpusFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "generated Go reader and pinned Python interpreters")
	for _, fixed := range []bool{false, true} {
		t.Run(fmt.Sprintf("fixed=%t", fixed), func(t *testing.T) {
			t.Parallel()
			schema := handtextSchema
			if fixed {
				schema = strings.Replace(schema, "table Document {", "fixed table Document {", 1)
			}
			unit := loadUnit(t, schema)
			files, err := golang.Generate(unit)
			if err != nil {
				t.Fatal(err)
			}
			surfaces := gotable.DefaultSurfaces()
			surfaces.TextForm = true
			tables, err := gotable.GenerateLineageSurface(unit, nil, surfaces)
			if err != nil {
				t.Fatal(err)
			}
			maps.Copy(files, tables)
			runtime := slowtest.SerializeGo(t, "../../../")
			files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
			vectors, err := filepath.Abs("../../../test/conformance/textform")
			if err != nil {
				t.Fatal(err)
			}
			files["oracle_test.go"] = []byte(fmt.Sprintf(`package probe
import ("fmt"; "os"; "path/filepath"; "testing")
func TestOracle(t *testing.T) {
 for _, stem := range []string{"roadmap", "layout", "kinds", "variable"} {
  data, err := os.ReadFile(filepath.Join(%q, stem+".input.sexp")); if err != nil { t.Fatal(err) }
  if stem == "variable" {
   _, v, err := GraphLoadTextForm(data, TableTextFormDefaultLimits()); if err != nil { t.Fatal(stem, err) }
   n := GraphToJSONMeasure(v); if n < 0 {t.Fatal(stem,"measure")}; b := make([]byte,n); if GraphToJSON(v,b) != n {t.Fatal(stem,"write")}; fmt.Printf("ORACLE:%%s:%%x\n",stem,b)
  } else {
   _, v, err := DocumentLoadTextForm(data, TableTextFormDefaultLimits()); if err != nil { t.Fatal(stem, err) }
   n := DocumentToJSONMeasure(v); if n < 0 {t.Fatal(stem,"measure")}; b := make([]byte,n); if DocumentToJSON(v,b) != n {t.Fatal(stem,"write")}; fmt.Printf("ORACLE:%%s:%%x\n",stem,b)
  }
 }
}`, vectors))
			dir := t.TempDir()
			writeTree(t, dir, files)
			cmd := exec.Command("go", "test", "-count=1", "-v", "-run", "^TestOracle$", ".")
			cmd.Dir = dir
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("Go oracle: %v\n%s", err, output)
			}
			var expected []string
			for _, line := range strings.Split(string(output), "\n") {
				if strings.HasPrefix(line, "ORACLE:") {
					expected = append(expected, line)
				}
			}
			if len(expected) != 4 {
				t.Fatalf("Go oracle returned %d rows: %s", len(expected), output)
			}
			pyFiles, err := Generate(unit)
			if err != nil {
				t.Fatal(err)
			}
			pyDir := t.TempDir()
			writeTree(t, pyDir, pyFiles)
			inputs := map[string][]byte{}
			for _, stem := range []string{"roadmap", "layout", "kinds", "variable"} {
				b, err := os.ReadFile(filepath.Join(vectors, stem+".input.sexp"))
				if err != nil {
					t.Fatal(err)
				}
				inputs[stem] = b
			}
			inputs["main.py"] = []byte(pyrun.ImportPrelude + `from pathlib import Path
from probe import Document, Graph
for stem in ('roadmap','layout','kinds','variable'):
    doc = (Graph if stem == 'variable' else Document).from_text_form(Path(stem).read_bytes())
    print('ORACLE:' + stem + ':' + doc.value.to_json().encode().hex())
`)
			for _, run := range []func(testing.TB, string, string, map[string][]byte) []byte{pyrun.Run, pyrun.RunFloor} {
				got := strings.TrimSpace(string(run(t, pyDir, "main.py", inputs)))
				if got != strings.Join(expected, "\n") {
					t.Fatalf("Python does not match Go:\ngot %s\nwant %s", got, strings.Join(expected, "\n"))
				}
			}
		})
	}
}

// TestPythonHandtextCppCorpusFunctional compares all four positive documents
// directly against generated C++ for fixed and variable Document roots.
func TestPythonHandtextCppCorpusFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "generated C++ reader and pinned Python interpreters")
	for _, fixed := range []bool{false, true} {
		t.Run(fmt.Sprintf("fixed=%t", fixed), func(t *testing.T) {
			t.Parallel()
			schema := handtextSchema
			if fixed {
				schema = strings.Replace(schema, "table Document {", "fixed table Document {", 1)
			}
			unit := loadUnit(t, schema)
			files, err := cpp.Generate(unit)
			if err != nil {
				t.Fatal(err)
			}
			tables, err := cpptable.Generate(unit)
			if err != nil {
				t.Fatal(err)
			}
			maps.Copy(files, tables)
			dir := t.TempDir()
			writeTree(t, dir, files)
			docRead := `DocumentBuilder b; if (!DocumentLoadTextForm(text.data(), (int64_t)text.size(), b, &reason) || !b.Lock()) return 3; const Document *v = b.AsConst();`
			if fixed {
				docRead = `Document v; if (!DocumentLoadTextForm(text.data(), (int64_t)text.size(), v, &reason)) {std::fprintf(stderr,"%s",reason ? reason : "?"); return 3;}`
			}
			source := `#include <cstdio>
#include <fstream>
#include <iterator>
#include <string>
#include <vector>
#include "CoreTable.h"
using namespace probe;
int main(int argc, char **argv) {
 if (argc != 3) return 1;
 std::ifstream f(argv[2], std::ios::binary); std::string text((std::istreambuf_iterator<char>(f)), {});
 const char *reason = nullptr;
 if (std::string(argv[1]) == "variable") {
  GraphBuilder b; if (!GraphLoadTextForm(text.data(), (int64_t)text.size(), b, &reason)) {std::fprintf(stderr,"%s",reason ? reason : "?"); return 2;} if (!b.Lock()) {std::fprintf(stderr,"lock"); return 2;}
  const Graph *v = b.AsConst(); auto n = GraphToJsonMeasure(v); std::vector<char> out((size_t)n + 1);
  if (GraphToJson(v, out.data(), (int64_t)out.size()) != n) return 4; std::fwrite(out.data(), 1, (size_t)n, stdout);
 } else {` + docRead + `auto n = DocumentToJsonMeasure(v); std::vector<char> out((size_t)n + 1); if (DocumentToJson(v, out.data(), (int64_t)out.size()) != n) return 4; std::fwrite(out.data(), 1, (size_t)n, stdout); }
 return 0;
}`
			path := filepath.Join(dir, "main.cpp")
			if err := os.WriteFile(path, []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			ser, err := filepath.Abs("../../../../serialize")
			if err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(dir, "oracle")
			cmd := exec.Command("c++", "-std=c++17", "-O1", "-I"+dir, "-I"+ser, path, filepath.Join(dir, "CoreTable.cpp"), "-o", bin)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("compile: %v\n%s", err, out)
			}
			vectors, err := filepath.Abs("../../../test/conformance/textform")
			if err != nil {
				t.Fatal(err)
			}
			var expected []string
			for _, stem := range []string{"roadmap", "layout", "kinds", "variable"} {
				out, err := exec.Command(bin, stem, filepath.Join(vectors, stem+".input.sexp")).CombinedOutput()
				if err != nil {
					t.Fatalf("C++ %s: %v: %s", stem, err, out)
				}
				expected = append(expected, fmt.Sprintf("ORACLE:%s:%x", stem, out))
			}
			pyFiles, err := Generate(unit)
			if err != nil {
				t.Fatal(err)
			}
			pyDir := t.TempDir()
			writeTree(t, pyDir, pyFiles)
			inputs := map[string][]byte{}
			for _, stem := range []string{"roadmap", "layout", "kinds", "variable"} {
				b, err := os.ReadFile(filepath.Join(vectors, stem+".input.sexp"))
				if err != nil {
					t.Fatal(err)
				}
				inputs[stem] = b
			}
			inputs["main.py"] = []byte(pyrun.ImportPrelude + `from pathlib import Path
from probe import Document, Graph
for stem in ('roadmap','layout','kinds','variable'):
    doc = (Graph if stem == 'variable' else Document).from_text_form(Path(stem).read_bytes())
    print('ORACLE:' + stem + ':' + doc.value.to_json().encode().hex())
`)
			for _, run := range []func(testing.TB, string, string, map[string][]byte) []byte{pyrun.Run, pyrun.RunFloor} {
				got := strings.TrimSpace(string(run(t, pyDir, "main.py", inputs)))
				if got != strings.Join(expected, "\n") {
					t.Fatalf("Python does not match C++: got %s want %s", got, strings.Join(expected, "\n"))
				}
			}
		})
	}
}

func TestPythonHandtextUnboundedReferencesFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "generated Go and C++ readers and pinned Python interpreters")
	const schema = `package probe
table Tags {
 rules []string
 first [..4]?string
 capped [..2]string
 byname map[string]int32
}`
	unit := loadUnit(t, schema)
	input, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "conformance", "textform", "unbounded.input.sexp"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := golang.Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	surfaces := gotable.DefaultSurfaces()
	surfaces.TextForm = true
	tables, err := gotable.GenerateLineageSurface(unit, nil, surfaces)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	runtime := slowtest.SerializeGo(t, "../../../")
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	files["input.sexp"] = input
	files["oracle_test.go"] = []byte(`package probe
import ("fmt"; "os"; "testing")
func TestOracle(t *testing.T) {
 data, err := os.ReadFile("input.sexp"); if err != nil {t.Fatal(err)}
 _, v, err := TagsLoadTextForm(data,TableTextFormDefaultLimits()); if err != nil {t.Fatal(err)}
 n := TagsToJSONMeasure(v); if n < 0 {t.Fatal("measure")}; b := make([]byte,n); if TagsToJSON(v,b) != n {t.Fatal("write")}; fmt.Printf("ORACLE:%x\n",b)
}`)
	dir := t.TempDir()
	writeTree(t, dir, files)
	cmd := exec.Command("go", "test", "-count=1", "-v", "-run", "^TestOracle$", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Go oracle: %v\n%s", err, out)
	}
	var expected string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "ORACLE:") {
			expected = strings.TrimPrefix(line, "ORACLE:")
		}
	}
	if expected == "" {
		t.Fatalf("Go oracle absent: %s", out)
	}
	cppFiles, err := cpp.Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	cppTables, err := cpptable.Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(cppFiles, cppTables)
	cppDir := t.TempDir()
	writeTree(t, cppDir, cppFiles)
	source := `#include <cstdio>
#include <fstream>
#include <iterator>
#include <string>
#include <vector>
#include "CoreTable.h"
using namespace probe;
int main(int argc, char **argv) {
 if (argc != 2) return 1;
 std::ifstream f(argv[1],std::ios::binary); std::string text((std::istreambuf_iterator<char>(f)),{});
 TagsBuilder builder; const char *reason = nullptr;
 if (!TagsLoadTextForm(text.data(),(int64_t)text.size(),builder,&reason)) {std::fprintf(stderr,"%s",reason ? reason : "?"); return 2;}
 if (!builder.Lock()) return 3; const Tags *v = builder.AsConst(); auto n = TagsToJsonMeasure(v);
 if (n < 0) return 4; std::vector<char> out((size_t)n+1); if (TagsToJson(v,out.data(),(int64_t)out.size()) != n) return 5;
 return std::fwrite(out.data(),1,(size_t)n,stdout) == (size_t)n ? 0 : 6;
}`
	path := filepath.Join(cppDir, "main.cpp")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	cppInput := filepath.Join(cppDir, "input.sexp")
	if err := os.WriteFile(cppInput, input, 0600); err != nil {
		t.Fatal(err)
	}
	ser, err := filepath.Abs("../../../../serialize")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(cppDir, "oracle")
	cmd = exec.Command("c++", "-std=c++17", "-O1", "-I"+cppDir, "-I"+ser, path, filepath.Join(cppDir, "CoreTable.cpp"), "-o", bin)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("C++ compile: %v\n%s", err, out)
	}
	cppOut, err := exec.Command(bin, cppInput).CombinedOutput()
	if err != nil {
		t.Fatalf("C++ oracle: %v\n%s", err, cppOut)
	}
	if got := fmt.Sprintf("%x", cppOut); got != expected {
		t.Fatal("C++ unbounded value differs from Go")
	}
	pyFiles, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	pyDir := t.TempDir()
	writeTree(t, pyDir, pyFiles)
	probe := pyrun.ImportPrelude + `from pathlib import Path
from probe import Tags
print(Tags.from_text_form(Path('input.sexp').read_bytes()).value.to_json().encode().hex())
`
	inputs := map[string][]byte{"main.py": []byte(probe), "input.sexp": input}
	for _, run := range []func(testing.TB, string, string, map[string][]byte) []byte{pyrun.Run, pyrun.RunFloor} {
		if got := strings.TrimSpace(string(run(t, pyDir, "main.py", inputs))); got != expected {
			t.Fatal("Python unbounded value differs from Go and C++")
		}
	}
}
