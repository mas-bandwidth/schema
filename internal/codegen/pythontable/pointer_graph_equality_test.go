package pythontable

import (
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
)

// Functional: the deadline bounds a deliberately nonterminating planted fault.
func TestPythonPointerGraphContainerEqualityFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters and strict mypy")
	u := loadNamedUnit(t, "PtrArr.schema", pointerArraysSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	if out, err := pyrun.Analyze(t, dir, "ptrarr"); err != nil {
		t.Fatalf("strict mypy: %v\n%s", err, out)
	}
	script := pyrun.ImportPrelude + `import signal
from ptrarr import Root, Node
from ptrarr._schema.graph import _graph_eq
class Deadline(Exception): pass
def interrupt(signum, frame): raise Deadline()
signal.signal(signal.SIGALRM, interrupt)
a=Root(); b=Root()
a.list_nodes.append(a.list_nodes); b.list_nodes.append(b.list_nodes)
signal.alarm(1)
try:
    assert a == b
    x=[]; y=[]; x.extend([x, 1]); y.extend([y, 2]); assert not _graph_eq(x, y)
    x={}; y={}; x['cycle']=x; y['cycle']=y; assert _graph_eq(x,y)
    x['value']=1; y['value']=2; assert not _graph_eq(x,y)
    p=Node(); q=Node(); p.children=[p]; q.children=[q]; assert p == q
    l=Node(); r=Node(); l.label=3; r.label=3
    a=Root(); b=Root(); a.list_nodes=[l,l]; b.list_nodes=[r,r]; assert a == b
    r.label=4; assert a != b
except Deadline:
    raise SystemExit('container_cycle_control')
finally:
    signal.alarm(0)
print('container-equality')
`
	const path = "ptrarr/_schema/graph.py"
	const guard = "        if type(x) is list or type(x) is dict:\n            pair = (id(x), id(y), id(field))\n            if pair in assumed:\n                continue\n            assumed.add(pair)\n"
	if strings.Count(string(files[path]), guard) != 1 {
		t.Fatal("container cycle control anchor changed")
	}
	for _, planted := range []bool{false, true} {
		selected := maps.Clone(files)
		if planted {
			selected[path] = []byte(strings.Replace(string(selected[path]), guard, "", 1))
		}
		for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
			dir := t.TempDir()
			writeTree(t, dir, selected)
			out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script)})
			if !planted && (err != nil || strings.TrimSpace(out) != "container-equality") {
				t.Fatalf("valid probe: %v\n%s\n%s", err, out, stderr)
			}
			if planted && (err == nil || !strings.Contains(stderr, "container_cycle_control")) {
				t.Fatalf("cycle control did not fail by name: %v\n%s\n%s", err, out, stderr)
			}
		}
	}
}

// Both scalar and array float fields retain bit equality inside graph walks.
func TestPythonPointerGraphFloatEquality(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, `package grapheq
fixed table Leaf {
 f float32
 d float64
 values [2]float32
}
table Root { leaf *Leaf }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	script := pyrun.ImportPrelude + `from grapheq import Root, Leaf
left=Root(); right=Root(); left.leaf=Leaf(); right.leaf=Leaf()
assert left == right, "equal"
right.leaf.f=-0.0
assert left != right, "float32"
right.leaf.f=0.0; right.leaf.d=-0.0
assert left != right, "float64"
right.leaf.d=0.0; right.leaf.values[1]=-0.0
assert left != right, "float array"
print("graph-floats")
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script)})
		if err != nil || strings.TrimSpace(out) != "graph-floats" {
			t.Fatalf("float graph equality: %v %s %s", err, out, stderr)
		}
	}
}

// A wide node visits descriptors once, rather than scanning every field for
// every slot. Replacing the linear lookup with a throwing control catches the
// formerly quadratic graph walk on both interpreters.
func TestPythonPointerGraphWideEquality(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	var schema strings.Builder
	schema.WriteString("package widegraph\ntable Root {\n")
	for i := range 128 {
		fmt.Fprintf(&schema, "f%d float32\n", i)
	}
	schema.WriteString("}\n")
	u := loadUnit(t, schema.String())
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	script := pyrun.ImportPrelude + `from widegraph import Root
from widegraph._schema.descriptor import TableDesc
def no_scan(self, name): raise AssertionError("repeated descriptor scan")
TableDesc.field=no_scan
left=Root(); right=Root()
assert left == right
right.f127=-0.0
assert left != right
print("wide-graph")
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script)})
		if err != nil || strings.TrimSpace(out) != "wide-graph" {
			t.Fatalf("wide graph: %v %s %s", err, out, stderr)
		}
	}
}
