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
)

// TestPythonCards checks every answer from the nine-surface manifest with both
// pinned interpreters, using a fresh generated package and the facade itself.
func TestPythonCards(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters and mypy (cards conformance)")
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile(filepath.Join(root, "tables/cards/Card.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, string(schema))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	facade, err := os.ReadFile(filepath.Join(root, "tables/cards/python/facade.py"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "facade.py"), facade, 0644); err != nil {
		t.Fatal(err)
	}
	if out, err := pyrun.Analyze(t, dir, "cards", "facade.py"); err != nil {
		t.Fatalf("generated package and facade mypy: %v\n%s", err, out)
	}
	if out, err := pyrun.Analyze(t, dir, "cards/_schema"); err != nil {
		t.Fatalf("runtime mypy: %v\n%s", err, out)
	}
	manifest, err := os.ReadFile(filepath.Join(root, "tables/cards/conformance/MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	surfaces := []string{"load", "save", "render", "parse", "bounds", "upgrade", "edit", "taskhash", "view"}
	for _, pin := range []struct {
		name string
		run  func(testing.TB, string, string, map[string][]byte) []byte
	}{{"current", pyrun.Run}, {"floor", pyrun.RunFloor}} {
		for _, surface := range surfaces {
			outdir := filepath.Join(dir, pin.name, surface)
			if err := os.MkdirAll(outdir, 0755); err != nil {
				t.Fatal(err)
			}
			script := fmt.Sprintf(`import os, runpy, sys
os.chdir(%q)
os.environ["CARDS_PYTHON_GENERATED"]=%q
sys.argv=["driver.py",%q,%q,%q]
runpy.run_path("tables/cards/python/driver.py",run_name="__main__")
`, root, dir, filepath.Join(root, "tables/cards/conformance/MANIFEST.txt"), surface, outdir)
			pin.run(t, dir, "main.py", map[string][]byte{"main.py": []byte(script)})
			for line := range strings.SplitSeq(string(manifest), "\n") {
				parts := strings.Fields(line)
				if len(parts) < 3 || parts[0] != surface {
					continue
				}
				suffixes := []string{".wire"}
				switch surface {
				case "load":
					suffixes = []string{".json", ".report"}
				case "render":
					suffixes = []string{".txt"}
				case "taskhash":
					suffixes = []string{".hex"}
				case "view":
					suffixes = []string{".txt"}
				case "upgrade":
					suffixes = []string{".wire", ".report"}
				}
				name := parts[1]
				if surface == "view" {
					name = "view"
				}
				for _, suffix := range suffixes {
					expected := filepath.Join(root, "tables/cards/conformance/expected", name+suffix)
					if _, err := os.Stat(expected); os.IsNotExist(err) {
						suffix = ".refused"
						expected = filepath.Join(root, "tables/cards/conformance/expected", name+suffix)
					}
					want, err := os.ReadFile(expected)
					if err != nil {
						t.Fatalf("%s/%s expected: %v", surface, name, err)
					}
					got, err := os.ReadFile(filepath.Join(outdir, name+suffix))
					if err != nil {
						t.Errorf("%s/%s/%s: %v", pin.name, surface, name, err)
						continue
					}
					if string(got) != string(want) {
						t.Errorf("%s/%s/%s%s differs", pin.name, surface, name, suffix)
					}
					if suffix == ".refused" {
						break
					}
				}
			}
		}
	}
}

// TestPythonCardsCppIdentity holds the populated card to independent Go and
// generated C++ writers, in addition to the Python manifest reader/writer.
func TestPythonCardsCppIdentity(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "generated C++ cards writer")
	root := filepath.Join("..", "..", "..", "tables", "cards")
	source, err := os.ReadFile(filepath.Join(root, "Card.schema"))
	if err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(filepath.Join(root, "conformance", "cases", "card-populated.json"))
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := os.ReadFile(filepath.Join(root, "conformance", "expected", "card-populated.wire"))
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, string(source))
	model := tabletext.NewModel(u)
	v := model.New(model.Lookup("Card"))
	var report tabletext.Report
	if !model.Read(v, text, &report) || !report.Silent() {
		t.Fatalf("Go JSON read: %+v", report)
	}
	goWire, err := tablewire.Encode(model, v)
	if err != nil {
		t.Fatal(err)
	}
	cppWire := cppWideWire(t, u, wideVTConstruct{root: "Card", json: string(text)})
	if !bytes.Equal(goWire, pinned) || !bytes.Equal(cppWire, pinned) {
		t.Fatalf("populated card wire differs: Go=%x C++=%x pin=%x", goWire, cppWire, pinned)
	}
}

func TestPythonCardsCppJSON(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "generated C++ cards JSON")
	root := filepath.Join("..", "..", "..", "tables", "cards")
	src, err := os.ReadFile(filepath.Join(root, "Card.schema"))
	if err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(filepath.Join(root, "conformance", "cases", "card-populated.json"))
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile(filepath.Join(root, "conformance", "expected", "card-populated.json"))
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, string(src))
	model := tabletext.NewModel(u)
	value := model.New(model.Lookup("Card"))
	var report tabletext.Report
	if !model.Read(value, text, &report) || !report.Silent() {
		t.Fatalf("Go JSON read: %+v", report)
	}
	goJSON, err := model.Write(value)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSuffix(goJSON, []byte("\n")), bytes.TrimSuffix(expected, []byte("\n"))) {
		t.Fatalf("Go JSON differs from card pin:\n%s\nexpected:\n%s", goJSON, expected)
	}
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := cpptable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	writeTree(t, dir, files)
	program := `#include "CoreTable.h"
#include <cstdio>
#include <cstring>
#include <vector>
using namespace cards;
int main(int argc, char **argv) {
 if (argc != 2) return 1;
 FILE *f=std::fopen(argv[1],"rb"); if (!f) return 2;
 std::fseek(f,0,SEEK_END); auto n=std::ftell(f); std::rewind(f); std::vector<char> text((size_t)n);
 if (std::fread(text.data(),1,text.size(),f)!=text.size()) return 3; std::fclose(f);
 CardBuilder b; TableReport report{};
 if (!CardFromJson(b,text.data(),n,&report) || !b.Lock()) return 4;
 const Card *v=b.AsConst(); auto size=CardToJsonMeasure(v); if (size<0) return 5;
 std::vector<char> out((size_t)size); if (CardToJson(v,out.data(),size)!=size) return 6;
 return std::fwrite(out.data(),1,out.size(),stdout)==out.size()?0:7;
}`
	path := filepath.Join(dir, "probe.cpp")
	if err := os.WriteFile(path, []byte(program), 0600); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(dir, "input.json")
	if err := os.WriteFile(input, text, 0600); err != nil {
		t.Fatal(err)
	}
	runtime, err := filepath.Abs("../../../../serialize")
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c++17", "-O1", "-I" + dir, "-I" + runtime, path}
	for name := range files {
		if !strings.Contains(name, "/") && strings.HasSuffix(name, ".cpp") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	bin := filepath.Join(dir, "probe")
	args = append(args, "-o", bin)
	if out, err := exec.Command("c++", args...).CombinedOutput(); err != nil {
		t.Fatalf("C++ build: %v\n%s", err, out)
	}
	got, err := exec.Command(bin, input).Output()
	if err != nil {
		t.Fatalf("C++ JSON: %v", err)
	}
	if !bytes.Equal(bytes.TrimSuffix(got, []byte("\n")), bytes.TrimSuffix(expected, []byte("\n"))) {
		t.Fatalf("C++ JSON differs from pin:\n%s", got)
	}
}

func TestPythonCardsFacade(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters (card facade)")
	root := filepath.Join("..", "..", "..", "tables", "cards")
	schema, err := os.ReadFile(filepath.Join(root, "Card.schema"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(loadUnit(t, string(schema)))
	if err != nil {
		t.Fatal(err)
	}
	facade, err := os.ReadFile(filepath.Join(root, "python", "facade.py"))
	if err != nil {
		t.Fatal(err)
	}
	future, err := os.ReadFile(filepath.Join(root, "conformance", "cases", "future-note.wire"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + cardsFacadeProbe
	inputs := map[string][]byte{"main.py": []byte(probe), "facade.py": facade, "future.wire": future}
	for _, out := range []string{string(pyrun.Run(t, dir, "main.py", inputs)), string(pyrun.RunFloor(t, dir, "main.py", inputs))} {
		if out != "facade checks ok\n" {
			t.Fatalf("facade probe: %s", out)
		}
	}
}

const cardsFacadeProbe = `import facade
from cards import BlobRef
brief="RESULT: x sha=0123456789ab tier: pro\nPATHS: a, b\nbody\u2028PATHS: still prose"
r=facade.parse(brief)
assert r.root.body=="body\u2028PATHS: still prose", "facade body"
original=r.wire
old_hash=facade.task_hash(r)
r.root.body=BlobRef("body\u2028PATHS: still prose")
assert facade.task_hash(r)==old_hash
r.root.body="body\u2028PATHS: still prose"
for field,value in (("tier","bad"),("holder","x"*33),("paths","x"*193)):
    try: facade.edit(r,field,value)
    except facade.CardError: pass
    else: raise AssertionError("invalid edit accepted")
    assert r.wire==original and r.root.body=="body\u2028PATHS: still prose"
for bad in ("", "RESULT: x sha=no tier: pro\n", "RESULT: x sha=0123456789ab tier: pro\nprose\nPATHS: a\n", "RESULT: x sha=0123456789ab tier: pro\n\x00"):
    try: facade.parse(bad)
    except facade.CardError: pass
    else: raise AssertionError("bad brief accepted")
facade.edit(r,"paths","x,y")
assert [x.s for x in r.root.paths]==["x","y"] and r.root.body=="body\u2028PATHS: still prose"
facade.set_paths(r,["x,y"]); assert [x.s for x in r.root.paths]==["x,y"]
with open("future.wire","rb") as f: future=f.read()
try: facade.read_strict(future)
except facade.CardError as e: assert e.reason=="unknown-present"
else: raise AssertionError("strict unknown accepted")
print("facade checks ok")
`

// TestPythonCardsFacadeNegativeControl corrupts a facade copy and requires
// the same probe to detect the damaged body on both pinned interpreters.
func TestPythonCardsFacadeNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters (card facade control)")
	root := filepath.Join("..", "..", "..", "tables", "cards")
	schema, err := os.ReadFile(filepath.Join(root, "Card.schema"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(loadUnit(t, string(schema)))
	if err != nil {
		t.Fatal(err)
	}
	future, err := os.ReadFile(filepath.Join(root, "conformance", "cases", "future-note.wire"))
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join(root, "python", "facade.py"))
	if err != nil {
		t.Fatal(err)
	}
	anchor := "root.body = text[body_at:] or None"
	if strings.Count(string(source), anchor) != 1 {
		t.Fatal("body control anchor absent or ambiguous")
	}
	mutant := strings.Replace(string(source), anchor, "root.body = None", 1)
	dir := t.TempDir()
	writeTree(t, dir, files)
	inputs := map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + cardsFacadeProbe), "facade.py": []byte(mutant), "future.wire": future}
	for _, py := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		_, stderr, err := pyrun.Try(t, py, dir, "main.py", inputs)
		if err == nil || !strings.Contains(stderr, "facade body") {
			t.Errorf("body control did not fail by name: %v\n%s", err, stderr)
		}
	}
}
