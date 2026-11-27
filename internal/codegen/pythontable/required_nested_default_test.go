package pythontable

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/cpptable"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"

	"github.com/mas-bandwidth/schema/v3/internal/testproc"
)

const requiredNestedSchema = `package reqnested
enum Slot { A, B }
table Child { n int32 | required }
table GuardedChild { on bool
 if on { n int32 | required } }
table Plain { child Child }
table Fleet { slots [Slot]Child }
table Guarded { child GuardedChild }
table Envelope { child ?Child }
`

func TestPythonRequiredNestedDefaults(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "C++ and both pinned Python interpreters")
	u := loadUnit(t, requiredNestedSchema)
	model := tabletext.NewModel(u)
	encode := func(root, text string) []byte {
		t.Helper()
		v := model.New(model.Lookup(root))
		if text != "" {
			var rep tabletext.Report
			if !model.Read(v, []byte(text), &rep) || !rep.Silent() {
				t.Fatalf("%s input: %+v", root, rep)
			}
		}
		b, err := tablewire.EncodeVariable(model, v)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	expected := map[string][]byte{"plain": encode("Plain", ""), "keyed": encode("Fleet", ""), "guard_off": encode("Guarded", ""), "guard_on": encode("Guarded", `{"child":{"on":true,"n":0}}`), "optional_off": encode("Envelope", ""), "optional_on": encode("Envelope", `{"child":{"n":0}}`)}
	// The older schema has no required child and legitimately elides its parent.
	prior := loadUnit(t, strings.ReplaceAll(requiredNestedSchema, " | required", ""))
	old := tabletext.NewModel(prior)
	oldWire, err := tablewire.EncodeVariable(old, old.New(old.Lookup("Plain")))
	if err != nil {
		t.Fatal(err)
	}
	expected["old"] = oldWire
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	other, err := cpptable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, other)
	dir := t.TempDir()
	writeTree(t, dir, files)
	header := ""
	var sources []string
	for name := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if strings.HasSuffix(name, "Table.h") {
			header = name
		}
		if strings.HasSuffix(name, "Table.cpp") {
			sources = append(sources, filepath.Join(dir, name))
		}
	}
	if header == "" || len(sources) == 0 {
		t.Fatal("C++ table source missing")
	}
	code := fmt.Sprintf(`#include <cstdio>
#include <cstring>
#include <vector>
#include "%s"
using namespace reqnested;
template<class T> int save(T &v, int64_t (*measure)(const T&), int64_t (*write)(const T&, uint8_t*, int64_t)) { int64_t n=measure(v); if(n<=0)return 1; std::vector<unsigned char> b((size_t)n); if(write(v,b.data(),n)!=n)return 2; return std::fwrite(b.data(),1,b.size(),stdout)==b.size()?0:3; }
int main(int argc,char **argv) { if(argc!=2)return 4;
 if(!std::strcmp(argv[1],"plain")){PlainBuilder v;return save(v,PlainMeasure,PlainSave);}
 if(!std::strcmp(argv[1],"keyed")){FleetBuilder v;return save(v,FleetMeasure,FleetSave);}
 if(!std::strcmp(argv[1],"guard_off")){GuardedBuilder v;return save(v,GuardedMeasure,GuardedSave);}
 if(!std::strcmp(argv[1],"guard_on")){GuardedBuilder v;v.GetRoot()->child.on=true;return save(v,GuardedMeasure,GuardedSave);}
 if(!std::strcmp(argv[1],"optional_off")){EnvelopeBuilder v;return save(v,EnvelopeMeasure,EnvelopeSave);}
 if(!std::strcmp(argv[1],"optional_on")){EnvelopeBuilder v;v.GetRoot()->child_present=true;return save(v,EnvelopeMeasure,EnvelopeSave);}
 return 5; }
`, header)
	source := filepath.Join(dir, "probe.cpp")
	if err := os.WriteFile(source, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	ser, err := filepath.Abs("../../../../serialize")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args := append([]string{"-std=c++17", "-O1", "-I" + dir, "-I" + ser, source}, sources...)
	args = append(args, "-o", bin)
	if out, err := testproc.Command(t, "c++", args...).CombinedOutput(); err != nil {
		t.Fatalf("C++ compile: %v\n%s", err, out)
	}
	for _, name := range []string{"plain", "keyed", "guard_off", "guard_on", "optional_off", "optional_on"} {
		got, err := testproc.Command(t, bin, name).Output()
		if err != nil || !bytes.Equal(got, expected[name]) {
			t.Errorf("C++/%s: %v got %x want %x", name, err, got, expected[name])
		}
	}
	py, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	script := pyrun.ImportPrelude + `from reqnested import Plain, Fleet, Guarded, Envelope, Child
def wire(name):
    with open(name,"rb") as f: return f.read()
p=Plain(); assert p.save()==wire("plain"), "required_nested_plain"
k=Fleet(); assert k.save()==wire("keyed"), "required_nested_keyed"
g=Guarded(); assert g.save()==wire("guard_off"), "required_guard_off"
g.child.on=True; assert g.save()==wire("guard_on"), "required_guard_on"
o=Envelope(); assert o.save()==wire("optional_off"), "required_optional_absent"
o.child=Child(); assert o.save()==wire("optional_on"), "required_optional_present"
assert Plain.load(wire("old")).save()==wire("plain"), "prior_absence_accepted"
print("nested-required")
`
	expected["main.py"] = []byte(script)
	for _, pin := range []struct {
		name string
		path func(testing.TB) string
	}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
		d := t.TempDir()
		writeTree(t, d, py)
		out, stderr, err := pyrun.Try(t, pin.path(t), d, "main.py", expected)
		if err != nil || strings.TrimSpace(out) != "nested-required" {
			t.Errorf("%s: %v %s %s", pin.name, err, out, stderr)
		}
	}
	// Removing required-child awareness must fail the nested-byte assertion.
	mutant := maps.Clone(py)
	const path = "reqnested/_schema/descriptor.py"
	const anchor = "return all(not field.required and field.is_default(getattr(value, field.name)) for field in fields)"
	if strings.Count(string(mutant[path]), anchor) != 1 {
		t.Fatal("required-child control anchor missing")
	}
	mutant[path] = []byte(strings.Replace(string(mutant[path]), anchor, "return all(field.is_default(getattr(value, field.name)) for field in fields)", 1))
	for _, pin := range []struct {
		name string
		path func(testing.TB) string
	}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
		d := t.TempDir()
		writeTree(t, d, mutant)
		_, stderr, err := pyrun.Try(t, pin.path(t), d, "main.py", expected)
		if err == nil || !strings.Contains(stderr, "required_nested_plain") {
			t.Errorf("%s required-child control: %v %s", pin.name, err, stderr)
		}
		keyedInputs := maps.Clone(expected)
		keyedInputs["main.py"] = []byte(strings.Replace(script, `p=Plain(); assert p.save()==wire("plain"), "required_nested_plain"`, `p=Plain()`, 1))
		_, stderr, err = pyrun.Try(t, pin.path(t), d, "main.py", keyedInputs)
		if err == nil || !strings.Contains(stderr, "required_nested_keyed") {
			t.Errorf("%s keyed-child control: %v %s", pin.name, err, stderr)
		}
	}
	guardMutant := maps.Clone(py)
	const guard = "field for field in value.descriptor.fields if field.guard_holds(value)"
	if strings.Count(string(guardMutant[path]), guard) != 1 {
		t.Fatal("guard control anchor missing")
	}
	guardMutant[path] = []byte(strings.Replace(string(guardMutant[path]), guard, "field for field in value.descriptor.fields", 1))
	for _, pin := range []struct {
		name string
		path func(testing.TB) string
	}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
		d := t.TempDir()
		writeTree(t, d, guardMutant)
		_, stderr, err := pyrun.Try(t, pin.path(t), d, "main.py", expected)
		if err == nil || !strings.Contains(stderr, "required_guard_off") {
			t.Errorf("%s inactive-guard control: %v %s", pin.name, err, stderr)
		}
	}
}
