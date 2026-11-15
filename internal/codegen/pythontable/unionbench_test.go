package pythontable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const pythonUnionBenchSchema = `package unionbench

table Leaf
{
    value int32
    flag  bool
}

union Pick
{
    leaf   Leaf
    number int32
    next   *Leaf
}

table Root
{
    picks [..32]Pick
    one   Pick
}
`

// TestPythonUnionShapeBench measures the union hot path (docs/PORT-PYTHON.md,
// THE TABLE FRAME, Unions bench) under both pinned interpreters: the save and
// the load of a 32-element array of unions, a third of each arm kind (a table,
// a scalar and a pointer), and one union field. Each is timed in two shapes.
// The load reads an arm's payload in place out of the reader that framed it
// (kept), or copies a length kind's payload into a new buffer behind a fresh
// length prefix. The save writes a table arm's field line straight into the
// caller's writer (kept), or builds every arm in a temporary buffer and
// splits its header. It is a functional test; the numbers it logs are the
// record in docs/PORT-PYTHON.md and it asserts only that both shapes of each
// direction agree on the bytes and the value.
//
// Functional (python pins): run with SCHEMA_SLOW=1.
func TestPythonUnionShapeBench(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreters (it times the emitted table runtime)")
	files, err := Generate(loadUnit(t, pythonUnionBenchSchema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `import time
from unionbench import Leaf, Pick, Root
from unionbench._schema import kinds, kind_union
from unionbench._schema.load import Reader

def arm(name):
    for i, a in enumerate(Pick.descriptor.arms, 1):
        if a.name == name:
            return i
    raise SystemExit("arm missing")

root = Root()
for i in range(32):
    p = Pick()
    if i % 3 == 0:
        p.type = arm("leaf")
        p.leaf.value = i
        p.leaf.flag = i % 2 == 0
    elif i % 3 == 1:
        p.type = arm("number")
        p.number = i * 1000
    else:
        p.type = arm("next")
        p.next = Leaf()
        p.next.value = i
    root.picks.append(p)
root.one.type = arm("leaf")
root.one.leaf.value = 77

def varint_bytes(value):
    out = bytearray()
    while True:
        byte = value & 0x7F
        value >>= 7
        if value:
            out.append(byte | 0x80)
        else:
            out.append(byte)
            return bytes(out)

def copy_read(r, framed, start, f, ctx):
    payload = r.data[start:r.pos]
    if f.kind in kind_union._LENGTH_KINDS:
        data = memoryview(varint_bytes(len(payload)) + bytes(payload))
    else:
        data = payload
    sub = Reader(data, 0, len(data), ctx.ids)
    if f.codec == "union":
        value = kind_union._read_union_payload(sub, kind_union._union_class(f.target), ctx)
    else:
        value = kinds._REGISTRY[f.codec].read(sub, f, ctx)
    if sub.pos != sub.end:
        raise SystemExit("bytes left")
    return value

def split_write(w, a, value, ctx):
    if a.field is None:
        w.varint(ctx.idref(a.arm_id))
        w.byte(a.kind)
        w.varint(0)
        return
    f = a.field
    ctx.idref(a.arm_id)
    body = bytearray()
    kinds._REGISTRY[f.codec].write(kind_union._writer(body), f, value, ctx)
    if f.kind in kind_union._LENGTH_KINDS:
        w.raw(body)
        return
    end = kind_union._header_end(body)
    payload = body[end:]
    w.raw(body[:end])
    w.varint(len(payload))
    w.raw(payload)

in_place_read = kind_union._read_arm_payload
direct_write = kind_union._write_arm_header

data = root.save()
kind_union._write_arm_header = split_write
if root.save() != data:
    raise SystemExit("the split write differs from the direct write")
kind_union._write_arm_header = direct_write
kind_union._read_arm_payload = copy_read
if Root.load(data) != root:
    raise SystemExit("the copying read differs")
kind_union._read_arm_payload = in_place_read
if Root.load(data) != root:
    raise SystemExit("the in-place read differs")

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

def timed(label, patch, name, fn):
    setattr(kind_union, name, patch)
    fn()
    print(label, round(run(fn), 1))

timed("save_direct", direct_write, "_write_arm_header", root.save)
timed("save_split", split_write, "_write_arm_header", root.save)
kind_union._write_arm_header = direct_write
timed("load_in_place", in_place_read, "_read_arm_payload", lambda: Root.load(data))
timed("load_copy", copy_read, "_read_arm_payload", lambda: Root.load(data))
kind_union._read_arm_payload = in_place_read
print("OK")
`
	keys := []string{"save_direct", "save_split", "load_in_place", "load_copy"}
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
