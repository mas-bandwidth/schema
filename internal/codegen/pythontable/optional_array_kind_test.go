package pythontable

import (
	"bytes"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
)

func TestPythonOptionalArrayClosedElementKind(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, `package optkind
table Root { x ?[..2]int32 }
`)
	model := tabletext.NewModel(u)
	value := model.New(model.Lookup("Root"))
	var report tabletext.Report
	if !model.Read(value, []byte(`{"x":[]}`), &report) {
		t.Fatalf("writer input: %+v", report)
	}
	wire, err := tablewire.EncodeVariable(model, value)
	if err != nil {
		t.Fatal(err)
	}
	marker := []byte{14, 2, 4, 0}
	if bytes.Count(wire, marker) != 1 {
		t.Fatalf("array header not unique: %x", wire)
	}
	pos := bytes.Index(wire, marker) + 2
	damaged := bytes.Clone(wire)
	damaged[pos] = 36
	foreign := bytes.Clone(wire)
	foreign[pos] = 1
	for _, tc := range []struct {
		name string
		wire []byte
		want string
	}{{"closed", damaged, "wire_damaged"}, {"foreign", foreign, "kind_mismatch"}} {
		var r tabletext.Report
		ok, err := tablewire.DecodeVariable(model, model.New(model.Lookup("Root")), tc.wire, &r)
		if ok || !r.Refused || !strings.Contains(r.Reason, tc.want) {
			t.Errorf("Go %s: %v %+v", tc.name, err, r)
		}
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	script := pyrun.ImportPrelude + `from optkind import Root, Refused
from optkind._schema.kind_optional import _read_array
from optkind._schema.load import Reader, Ctx
fd=Root.descriptor.field("x")
def direct(kind):
    data=memoryview(bytes((2,kind,0)))
    return _read_array(Reader(data,0,len(data),[]),fd,Ctx([]))
def refusal(label,fn,want):
    try: fn()
    except Refused as e: assert e.reason==want, (label,e.reason)
    else: raise AssertionError(label+" accepted")
assert direct(4)==[], "valid direct"
refusal("closed_direct",lambda: direct(36),"wire_damaged")
refusal("foreign_direct",lambda: direct(1),"kind_mismatch")
for name, want in (("closed","wire_damaged"),("foreign","kind_mismatch")):
    with open(name,"rb") as f: data=f.read()
    refusal(name,lambda: Root.load(data),want)
print("optional-kind")
`
	inputs := map[string][]byte{"main.py": []byte(script), "closed": damaged, "foreign": foreign}
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
		if err != nil || strings.TrimSpace(out) != "optional-kind" {
			t.Errorf("%s: %v %s %s", pin, err, out, stderr)
		}
	}
	const path = "optkind/_schema/kind_optional.py"
	const anchor = `_ELEMENT_KINDS = frozenset(list(range(1, 14)) + [15, 17] + list(range(18, 30)) + [30])`
	if strings.Count(string(files[path]), anchor) != 1 {
		t.Fatal("closed-kind control anchor missing")
	}
	mutated := maps.Clone(files)
	mutated[path] = []byte(strings.Replace(string(files[path]), anchor, strings.Replace(anchor, "[30])", "[30, 36])", 1), 1))
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, mutated)
		_, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
		if err == nil || !strings.Contains(stderr, "closed_direct") {
			t.Errorf("%s closed-kind control: %v %s", pin, err, stderr)
		}
	}
}
