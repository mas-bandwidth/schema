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
table Root { value Sample }
`
const hostedFloatJSON = `{"value":{"f":1.5,"d":-2.25,"c":0.5,"a":[1.0,-3.25],"b":[2.0,4.5],"ca":[0.25,-0.5]}}`

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
assert v.value.a==[1.0,-3.25] and v.value.b==[2.0,4.5] and v.value.ca==[0.25,-0.5]
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
