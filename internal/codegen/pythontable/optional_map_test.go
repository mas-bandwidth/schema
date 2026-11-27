package pythontable

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
)

const optionalMapSchema = `package optmap
 table Leaf { value int32 }
 table Root {
   tags ?map[string(8)]int32
   nodes ?map[uint8]*Leaf
 }
`

const optionalMapProbe = `import optmap
wire = open('wire', 'rb').read()
mode = open('mode').read()
r = optmap.Root()
if mode == 'empty': r.tags = {}
if mode == 'populated': r.tags = {'b': 2, 'a': 1}
if mode == 'nodes':
    leaf = optmap.Leaf()
    leaf.value = 7
    r.nodes = {2: leaf}
assert r.save() == wire, 'python map bytes'
out = optmap.Root.load(wire)
assert out == r, 'python map round trip'
assert out.save() == wire, 'python map resave'
assert out.measure() == len(wire)
print('ok')
`

func TestPythonOptionalMapFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Go and C++ wire oracles and both pinned Python interpreters")
	for _, tc := range []struct{ name, json string }{
		{"absent", `{}`},
		{"empty", `{"tags":{}}`},
		{"populated", `{"tags":{"a":1,"b":2}}`},
		{"nodes", `{"nodes":{"2":{"value":7}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u := loadUnit(t, optionalMapSchema)
			wire := wideEngineWire(t, u, "Root", tc.json)
			cpp := cppWideWire(t, u, wideVTConstruct{name: tc.name, schema: optionalMapSchema, root: "Root", json: tc.json})
			if !bytes.Equal(wire, cpp) {
				t.Fatalf("Go and C++ wires differ: %x / %x", wire, cpp)
			}
			got := pyKeyedRun(t, u, optionalMapProbe, map[string][]byte{"wire": wire, "mode": []byte(tc.name)}, nil)
			if strings.TrimSpace(got) != "ok" {
				t.Fatalf("probe: %s", got)
			}
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			writeTree(t, dir, files)
			if out, err := pyrun.Analyze(t, dir, "optmap"); err != nil {
				t.Fatalf("mypy: %v\n%s", err, out)
			}
		})
	}
}

const optionalMapRefusalsProbe = `import optmap
from optmap._schema import InvalidValue
for name, value in [('tags', {'toolongkey': 1}), ('tags', {'ok': True}), ('tags', {'ok': 1 << 31}), ('tags', []), ('nodes', {256: None}), ('nodes', {1: object()})]:
    r = optmap.Root()
    setattr(r, name, value)
    try:
        r.save()
    except InvalidValue as e:
        assert name in str(e), 'map refusal path'
        assert getattr(r, name) is value, 'map value changed'
    else:
        raise AssertionError('invalid map accepted')
print('ok')
`

func TestPythonOptionalMapRefusalsFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, optionalMapSchema)
	if got := pyKeyedRun(t, u, optionalMapRefusalsProbe, nil, nil); strings.TrimSpace(got) != "ok" {
		t.Fatal(got)
	}
}

func TestPythonOptionalMapNegativeControlFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, optionalMapSchema)
	emptyWire := wideEngineWire(t, u, "Root", `{"tags":{}}`)
	nodesWire := wideEngineWire(t, u, "Root", `{"nodes":{"2":{"value":7}}}`)
	cases := []struct {
		name, path, from, to, mode string
		wire                       []byte
	}{
		{"presence", "optmap/_schema/descriptor.py", "        if self.pointer or self.optional:\n            return value is None", "        if self.pointer or self.optional:\n            return value is None or value == {}", "empty", emptyWire},
		{"graph", "optmap/_schema/save.py", "                yield from _collect_map_edges(field, current, (path, field.name, _NO_SUB), ctx, blobs)", "                pass", "nodes", nodesWire},
		{"validation", "optmap/_schema/save.py", "                yield from _validate_map_entries(field, current, where)", "                pass", "validation", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			src := string(files[tc.path])
			if !strings.Contains(src, tc.from) {
				t.Fatal("control anchor absent")
			}
			files[tc.path] = []byte(strings.Replace(src, tc.from, tc.to, 1))
			dir := t.TempDir()
			writeTree(t, dir, files)
			probe := pyrun.ImportPrelude + `import optmap
wire = open('wire','rb').read()
mode = open('mode').read()
r=optmap.Root()
if mode == 'empty': r.tags = {}
elif mode == 'validation':
    from optmap._schema import InvalidValue
    r.tags={'toolongkey': 1}
    try:
        r.save()
    except InvalidValue:
        raise SystemExit(0)
    raise AssertionError('optional map validation drift')
else:
    v=optmap.Leaf(); v.value=7; r.nodes={2:v}
try:
    actual=r.save()
except Exception as e:
    raise AssertionError('optional map wire drift') from e
assert actual == wire, 'optional map wire drift'
`
			inputs := map[string][]byte{"main.py": []byte(probe), "wire": tc.wire, "mode": []byte(tc.mode)}
			for _, bin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
				_, stderr, err := pyrun.Try(t, bin, dir, "main.py", inputs)
				reason := "optional map wire drift"
				if tc.mode == "validation" {
					reason = "optional map validation drift"
				}
				if err == nil || !strings.Contains(stderr, reason) {
					t.Fatalf("%s control escaped: %v\n%s", tc.name, err, stderr)
				}
			}
		})
	}
}
