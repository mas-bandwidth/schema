package pythontable

import (
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
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
	const guard = "        if type(x) is list or type(x) is dict:\n            pair = (id(x), id(y))\n            if pair in assumed:\n                continue\n            assumed.add(pair)\n"
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
