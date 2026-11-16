package compiler

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const hostedFloatSchema = `package probe
type Sample {
 f float32
 d float64
 c float32 | min = -1, max = 1, resolution = 0.25
 a [2]float32
 b [..2]float64
 ca [..2]float32 | min = -1, max = 1, resolution = 0.25
}
union Choice { x float32 }
table Root { value Sample
 pick Choice
}
`
const hostedFloatJSON = `{"value":{"f":1.5,"d":-2.25,"c":0.5,"a":[1.0,-3.25],"b":[2.0,4.5],"ca":[0.25,-0.5]},"pick":{"x":1.75}}`

func TestPythonHostedFloatFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "pinned Python, mypy and C++ hosted float wire")
	u := unitFromSource(t, hostedFloatSchema)
	model := tabletext.NewModel(u)
	value := model.New(u.Tables["Root"])
	var report tabletext.Report
	if !model.Read(value, []byte(hostedFloatJSON), &report) || report.Refused || report.Unknown != 0 {
		t.Fatalf("JSON: %+v", report)
	}
	wire, err := tablewire.Encode(model, value)
	if err != nil {
		t.Fatal(err)
	}
	cpp, err := New().Generate(u, "cpp", Options{})
	if err != nil {
		t.Fatal(err)
	}
	cdir := t.TempDir()
	writeHostedFloatFiles(t, cdir, cpp)
	source := fmt.Sprintf(`#include "ProbeTable.h"
#include <cstring>
using namespace probe;
int main() {
 RootBuilder b; TableReport r{}; const char *s=R"json(%s)json";
 if (!RootFromJson(b,s,strlen(s),&r)) return 1;
 const unsigned char expected[]={%s}; unsigned char out[sizeof(expected)];
 if (RootMeasure(b) != sizeof(expected) || RootSave(b,out,sizeof(out)) != sizeof(expected)) return 2;
 return memcmp(expected,out,sizeof(expected)) != 0;
}`, hostedFloatJSON, cppWire(wire))
	issue715CompileRun(t, cdir, "c++", ".cpp", source)
	files, err := New().Generate(u, "python", Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeHostedFloatFiles(t, dir, files)
	probe := pyrun.ImportPrelude + hostedFloatProbe
	inputs := map[string][]byte{"main.py": []byte(probe), "wire": wire}
	for _, got := range []string{string(pyrun.Run(t, dir, "main.py", inputs)), string(pyrun.RunFloor(t, dir, "main.py", inputs))} {
		if got != "hosted floats ok\n" {
			t.Fatalf("probe: %s", got)
		}
	}
	if out, err := pyrun.Analyze(t, dir, "probe"); err != nil {
		t.Fatalf("mypy: %v\n%s", err, out)
	}
}

const hostedFloatProbe = `import struct
from probe import Root, Refused, InvalidValue
with open("wire", "rb") as f: data=f.read()
v=Root.load(data)
assert (v.value.f,v.value.d,v.value.c)==(1.5,-2.25,0.5)
assert v.value.a==[1.0,-3.25] and v.value.b==[2.0,4.5] and v.value.ca==[0.25,-0.5] and v.pick.x==1.75
assert v.save()==data
bad=data.replace(struct.pack("<d", -2.25), struct.pack("<Q", 0x7ff8000000000000), 1)
assert bad != data
try: Root.load(bad)
except Refused: pass
else: raise AssertionError("nonfinite read accepted")
bad_range=data.replace(struct.pack("<f",0.5),struct.pack("<f",2.0),1)
assert bad_range != data
try: Root.load(bad_range)
except Refused: pass
else: raise AssertionError("compressed read accepted")
try: Root.load(data[:-1])
except Refused: pass
else: raise AssertionError("truncated read accepted")
v.value.f=float("nan")
try: v.save()
except InvalidValue: pass
else: raise AssertionError("nonfinite write accepted")
v=Root.load(data)
v.value.c=2.0
try: v.save()
except InvalidValue: pass
else: raise AssertionError("compressed range accepted")
print("hosted floats ok")
`

func writeHostedFloatFiles(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for name, data := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPythonHostedFloatNegativeControls(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "pinned Python hosted float negative controls")
	u := unitFromSource(t, hostedFloatSchema)
	model := tabletext.NewModel(u)
	value := model.New(u.Tables["Root"])
	var report tabletext.Report
	if !model.Read(value, []byte(hostedFloatJSON), &report) || report.Refused {
		t.Fatalf("JSON: %+v", report)
	}
	wire, err := tablewire.Encode(model, value)
	if err != nil {
		t.Fatal(err)
	}
	files, err := New().Generate(u, "python", Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, old, replacement string }{
		{"read-nonfinite", "if not math.isfinite(value):\n            raise Refused", "if False:\n            raise Refused"},
		{"write-nonfinite", "if not math.isfinite(value):\n            raise InvalidValue", "if False:\n            raise InvalidValue"},
		{"write-range", "if fd.float_range and (stored < fd.fmin or stored > fd.fmax):", "if False:"},
		{"read-range", "if value < fd.fmin or value > fd.fmax:", "if False:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cloned := maps.Clone(files)
			key := "probe/_schema/kind_float.py"
			text := string(cloned[key])
			if strings.Count(text, tc.old) != 1 {
				t.Fatalf("%s anchor missing or ambiguous", tc.name)
			}
			cloned[key] = []byte(strings.Replace(text, tc.old, tc.replacement, 1))
			dir := t.TempDir()
			writeHostedFloatFiles(t, dir, cloned)
			inputs := map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + hostedFloatProbe), "wire": wire}
			for _, py := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
				_, stderr, err := pyrun.Try(t, py, dir, "main.py", inputs)
				if err == nil || !strings.Contains(stderr, "accepted") {
					t.Errorf("%s control did not fail by name: %v %s", tc.name, err, stderr)
				}
			}
		})
	}
}

const optionalFloatSchema = `package probe
table Root {
 f ?float32
 d ?float64
 c ?float32 | min = -1, max = 1, resolution = 0.25
 a ?[2]float32
 b ?[..2]float64
 ca ?[..2]float32 | min = -1, max = 1, resolution = 0.25
}
`
const optionalFloatJSON = `{"f":-1.25,"d":2.5,"c":0.5,"a":[1.0,-2.0],"b":[3.0],"ca":[0.25,-0.5]}`

func TestPythonOptionalFloatFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "pinned Python, mypy and C++ optional floats")
	u := unitFromSource(t, optionalFloatSchema)
	model := tabletext.NewModel(u)
	value := model.New(u.Tables["Root"])
	var report tabletext.Report
	if !model.Read(value, []byte(optionalFloatJSON), &report) || report.Refused || report.Unknown != 0 {
		t.Fatalf("JSON: %+v", report)
	}
	wire, err := tablewire.Encode(model, value)
	if err != nil {
		t.Fatal(err)
	}
	cpp, err := New().Generate(u, "cpp", Options{})
	if err != nil {
		t.Fatal(err)
	}
	cdir := t.TempDir()
	writeHostedFloatFiles(t, cdir, cpp)
	src := fmt.Sprintf(`#include "ProbeTable.h"
#include <cstring>
using namespace probe;
int main() { RootBuilder b; TableReport r{}; const char *s=R"json(%s)json";
 if (!RootFromJson(b,s,strlen(s),&r)) return 1;
 const unsigned char expected[]={%s}; unsigned char out[sizeof(expected)];
 if (RootMeasure(b)!=sizeof(expected) || RootSave(b,out,sizeof(out))!=sizeof(expected)) return 2;
 return memcmp(expected,out,sizeof(expected)) != 0;
}`, optionalFloatJSON, cppWire(wire))
	issue715CompileRun(t, cdir, "c++", ".cpp", src)
	files, err := New().Generate(u, "python", Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeHostedFloatFiles(t, dir, files)
	probe := pyrun.ImportPrelude + optionalFloatProbe
	inputs := map[string][]byte{"main.py": []byte(probe), "wire": wire}
	for _, got := range []string{string(pyrun.Run(t, dir, "main.py", inputs)), string(pyrun.RunFloor(t, dir, "main.py", inputs))} {
		if got != "optional floats ok\n" {
			t.Fatalf("probe: %s", got)
		}
	}
	if out, err := pyrun.Analyze(t, dir, "probe"); err != nil {
		t.Fatalf("mypy: %v\n%s", err, out)
	}
}

const optionalFloatProbe = `from probe import Root, Refused, InvalidValue
with open("wire", "rb") as fh: data=fh.read()
v=Root.load(data)
assert (v.f,v.d,v.c)==(-1.25,2.5,0.5) and v.a==[1.0,-2.0] and v.b==[3.0] and v.ca==[0.25,-0.5] and v.save()==data
r=Root(); r.f=-1.25; r.d=2.5; r.c=0.5; r.a=[1.0,-2.0]; r.b=[3.0]; r.ca=[0.25,-0.5]
assert r.save()==data
r.f=None; r.d=None; r.c=None; r.a=None; r.b=None; r.ca=None
assert Root.load(r.save()).f is None
r.f=0.0; r.d=0.0; r.c=0.0; r.b=[]; r.ca=[]
v=Root.load(r.save()); assert (v.f,v.d,v.c)==(0.0,0.0,0.0) and v.b==[] and v.ca==[] and v.a is None
r.ca=[2.0]
try: r.save()
except InvalidValue: pass
else: raise AssertionError("optional array range accepted")
r.ca=[]; r.a=[float("nan"),0.0]
try: r.save()
except InvalidValue: pass
else: raise AssertionError("optional array nonfinite accepted")
r.a=None
r.c=2.0
try: r.save()
except InvalidValue: pass
else: raise AssertionError("optional range accepted")
r.c=0.5; r.f=float("nan")
try: r.save()
except InvalidValue: pass
else: raise AssertionError("optional nonfinite accepted")
print("optional floats ok")
`

// Each control puts the previous optional-width bug back into one operation.
func TestPythonOptionalFloatNegativeControls(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "pinned Python optional float negative controls")
	u := unitFromSource(t, optionalFloatSchema)
	model := tabletext.NewModel(u)
	value := model.New(u.Tables["Root"])
	var report tabletext.Report
	if !model.Read(value, []byte(optionalFloatJSON), &report) || report.Refused {
		t.Fatalf("JSON: %+v", report)
	}
	wire, err := tablewire.Encode(model, value)
	if err != nil {
		t.Fatal(err)
	}
	files, err := New().Generate(u, "python", Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, old, replacement string }{
		{"pack", "if fd.kind == 10:\n            return struct.pack", "if fd.codec == \"float32\":\n            return struct.pack"},
		{"unpack", "if fd.kind == 10:\n        return float(struct.unpack", "if fd.codec == \"float32\":\n        return float(struct.unpack"},
		{"width", "r.take(4 if fd.kind == 10 else 8)", "r.take(4 if fd.codec == \"float32\" else 8)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cloned := maps.Clone(files)
			key := "probe/_schema/kind_float.py"
			text := string(cloned[key])
			if strings.Count(text, tc.old) != 1 {
				t.Fatalf("%s anchor absent or ambiguous", tc.name)
			}
			cloned[key] = []byte(strings.Replace(text, tc.old, tc.replacement, 1))
			dir := t.TempDir()
			writeHostedFloatFiles(t, dir, cloned)
			inputs := map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + optionalFloatProbe), "wire": wire}
			for _, py := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
				_, stderr, err := pyrun.Try(t, py, dir, "main.py", inputs)
				if err == nil {
					t.Errorf("%s control accepted: %s", tc.name, stderr)
				}
			}
		})
	}
	t.Run("array-write", func(t *testing.T) {
		cloned := maps.Clone(files)
		key := "probe/_schema/kind_optional.py"
		old := "cast(kinds._ElementKind, kinds._REGISTRY[elem.codec]).write_element(w, elem, item, ctx)"
		text := string(cloned[key])
		if strings.Count(text, old) != 1 {
			t.Fatal("array-write anchor absent or ambiguous")
		}
		cloned[key] = []byte(strings.Replace(text, old, "pass", 1))
		dir := t.TempDir()
		writeHostedFloatFiles(t, dir, cloned)
		inputs := map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + optionalFloatProbe), "wire": wire}
		for _, py := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
			_, stderr, err := pyrun.Try(t, py, dir, "main.py", inputs)
			if err == nil || !strings.Contains(stderr, "AssertionError") {
				t.Errorf("array-write control: %v %s", err, stderr)
			}
		}
	})

}
