package pythontable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const pythonMapBenchSchema = `package mapbench

table Node
{
    value int32
}

table Root
{
    values map[uint32]int32
    heads  map[uint16]*Node
}
`

// TestPythonMapShapeBench measures the map hot path (docs/PORT-PYTHON.md, THE
// TABLE FRAME, Maps bench) under both pinned interpreters: the save and the
// load of a 64-entry scalar map and a 16-entry pointer map. The save is timed
// in two shapes: the runtime's, which writes each entry body straight from the
// key and the value (kept), and a prototype that builds an entry object per
// key and walks it through write_body. A map holder is variable, so the load
// is the descriptor walk alone. It is a functional test; the numbers it logs
// are the record in docs/PORT-PYTHON.md and it asserts only that both save
// shapes write the same bytes and the load reads them back.
//
// Functional (python pins): run with SCHEMA_SLOW=1.
func TestPythonMapShapeBench(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreters (it times the emitted table runtime)")
	files, err := Generate(loadUnit(t, pythonMapBenchSchema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `import time
from mapbench import Node, Root
from mapbench._schema import kind_map
from mapbench._schema.save import Writer, write_body

scalar = Root()
scalar.values = {(i * 7919) % 100003: i - 32 for i in range(64)}
pointer = Root()
nodes = [Node() for _ in range(16)]
for i, node in enumerate(nodes):
    node.value = i
pointer.heads = {(i * 31) % 997: nodes[i] for i in range(16)}

direct_write = kind_map._Map.write

def walk_write(self, w, fd, value, ctx):
    entry_cls = kind_map._entry(fd)
    key_field, value_field = kind_map._entry_fields(fd)
    field_ref = ctx.idref(fd.field_id)
    keys = kind_map._sorted_keys(fd, value)
    body = bytearray()
    body_writer = Writer(body)
    body_writer.byte(13)
    body_writer.varint(len(keys))
    for key in keys:
        entry = entry_cls()
        setattr(entry, key_field.name, key)
        setattr(entry, value_field.name, value[key])
        entry_body = bytearray()
        write_body(Writer(entry_body), entry, ctx, False)
        body_writer.varint(len(entry_body))
        body_writer.raw(entry_body)
    w.varint(field_ref)
    w.byte(fd.kind)
    w.varint(len(body))
    w.raw(body)

scalar_data = scalar.save()
pointer_data = pointer.save()
kind_map._Map.write = walk_write
if scalar.save() != scalar_data or pointer.save() != pointer_data:
    raise SystemExit("the entry-object walk differs from the direct entry writer")
kind_map._Map.write = direct_write
if Root.load(scalar_data) != scalar or Root.load(pointer_data).save() != pointer_data:
    raise SystemExit("the load does not read the save back")

N = 2000
ROUNDS = 5

def run(fn):
    best = None
    for _ in range(ROUNDS):
        start = time.perf_counter()
        for _ in range(N):
            fn()
        value = (time.perf_counter() - start) * 1e9 / N
        if best is None or value < best:
            best = value
    return best

def save_shapes(label, value):
    kind_map._Map.write = direct_write
    value.save()
    direct = run(value.save)
    kind_map._Map.write = walk_write
    value.save()
    walk = run(value.save)
    kind_map._Map.write = direct_write
    print(label + "_save_direct", round(direct, 1))
    print(label + "_save_walk", round(walk, 1))

save_shapes("scalar", scalar)
save_shapes("pointer", pointer)
Root.load(scalar_data)
print("scalar_load", round(run(lambda: Root.load(scalar_data)), 1))
Root.load(pointer_data)
print("pointer_load", round(run(lambda: Root.load(pointer_data)), 1))
print("OK")
`
	keys := []string{"scalar_save_direct", "scalar_save_walk", "pointer_save_direct", "pointer_save_walk", "scalar_load", "pointer_load"}
	for name, pin := range map[string]string{
		"python-3.15.2":  pyrun.Interpreter(t),
		"python-3.11.18": pyrun.Floor(t),
	} {
		stdout, stderr, runErr := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if runErr != nil || !strings.Contains(stdout, "OK\n") {
			t.Fatalf("%s did not finish: %v\n%s%s", name, runErr, stdout, stderr)
		}
		for _, key := range keys {
			value := timedValue(t, stdout, key)
			if value <= 0 {
				t.Fatalf("%s %s did not report a positive time", name, key)
			}
			t.Logf("%s %s %.1f ns/op", name, key, value)
		}
	}
}
