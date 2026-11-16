package pythontable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestPythonRetainRoundTrip uses the shared cross-version C++-pinned files.
func TestPythonRetainRoundTrip(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters and the retained C++ file")
	root := filepath.Join("..", "..", "..")
	u := loadUnitPaths(t, filepath.Join(root, "test/tables/RT1.schema"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	input, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/retain_rt2.bin"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/retain_rt1_save.bin"))
	if err != nil {
		t.Fatal(err)
	}
	script := pyrun.ImportPrelude + `from tblrt1 import Node, TableRetain, LoadReport
with open('input','rb') as f: data=f.read()
with open('expected','rb') as f: expected=f.read()
keep=TableRetain(bytearray(65536), 8192)
report=LoadReport()
value=Node.load_retain(data, keep, report)
assert report.unknown > 0 and report.retained > 0
saved=value.save_retain(keep, report)
assert saved==expected
assert value.measure_retain(keep)==len(saved)
print('retain-roundtrip')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script), "input": input, "expected": want})
		if err != nil || strings.TrimSpace(out) != "retain-roundtrip" {
			t.Fatalf("retain roundtrip: %v\n%s\n%s", err, out, stderr)
		}
	}
}

func TestPythonRetainResolvedPayload(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters and strict mypy")
	u := loadUnit(t, "package probe\ntable Root { n int32 }\n")
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	if out, err := pyrun.Analyze(t, dir, "probe/_schema"); err != nil {
		t.Fatalf("mypy: %v\n%s", err, out)
	}
	script := pyrun.ImportPrelude + `from probe._schema.retain import resolve_payload, measure_payload, emit_payload, Excluded, Capacity
from probe import Refused
ids=[0x111,0x222]
def u64(v): return v.to_bytes(8,'little')
def u32(v): return v.to_bytes(4,'little')
def resolved(kind, payload, cap=128):
    store=bytearray(cap)
    end=resolve_payload(kind,memoryview(payload),ids,memoryview(store))
    return bytes(store[:end])
assert resolved(13,bytes([4,1,30,2,0])) == u32(25)+u32(0)+u64(ids[0])+bytes([30])+u64(ids[1])+u64(0)
assert resolved(14,bytes([4,30,2,1,2])) == u32(18)+u32(0)+bytes([30,2])+u64(ids[0])+u64(ids[1])
assert resolved(16,bytes([8,4,1,1,4,1,2,3,4])) == u32(22)+u32(0)+bytes([4,1])+u64(ids[0])+u32(4)+u32(0)+bytes([1,2,3,4])
assert resolved(15,bytes([1,13,1,0])) == u64(ids[0])+bytes([13])+u32(8)+u32(0)+u64(0)
assert resolved(12,bytes([3,97,98,99])) == bytes([3,97,98,99])
# Re-interning swaps the two references while keeping the values' ids intact.
for kind, payload, want in [
    (13, bytes([4,1,30,2,0]), bytes([4,2,30,1,0])),
    (14, bytes([4,30,2,1,2]), bytes([4,30,2,2,1])),
    (16, bytes([8,4,1,1,4,1,2,3,4]), bytes([8,4,1,2,4,1,2,3,4])),
    (15, bytes([1,13,1,0]), bytes([2,13,1,0])),
]:
    store=bytearray(128)
    end=resolve_payload(kind,memoryview(payload),ids,memoryview(store))
    record=memoryview(store)[:end]
    refs={ids[0]:2,ids[1]:1}
    size=measure_payload(kind,record,refs.__getitem__)
    output=bytearray(size)
    assert emit_payload(kind,record,refs.__getitem__,memoryview(output)) == size
    assert bytes(output)==want, (kind,bytes(output),want)

for kind,payload in [(17,bytes([0])),(14,bytes([2,17,0])),(13,bytes([4,1,17,0,0]))]:
    try: resolved(kind,payload)
    except Excluded: pass
    else: raise AssertionError('node index retained')
try: resolved(13,bytes([4,1,30,2,0]),8)
except Capacity: pass
else: raise AssertionError('capacity ignored')
try: resolved(30,bytes([3]))
except Refused: pass
else: raise AssertionError('invalid id reference accepted')
print('resolved')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script)})
		if err != nil || strings.TrimSpace(out) != "resolved" {
			t.Fatalf("resolve: %v\n%s\n%s", err, out, stderr)
		}
	}
}

func TestPythonRetainResolverNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	files, err := Generate(loadUnit(t, "package probe\ntable Root { n int32 }\n"))
	if err != nil {
		t.Fatal(err)
	}
	const path = "probe/_schema/retain.py"
	const guard = "        if kind == 14 and element == 17:\n            raise Excluded()\n"
	if strings.Count(string(files[path]), guard) != 1 {
		t.Fatal("node exclusion control anchor changed")
	}
	files[path] = []byte(strings.Replace(string(files[path]), guard, "", 1))
	script := pyrun.ImportPrelude + `from probe._schema.retain import resolve_payload, Excluded
try:
    resolve_payload(14,memoryview(bytes([2,17,0])),[],memoryview(bytearray(64)))
except Excluded:
    pass
else:
    raise SystemExit('retain_node_exclusion_control')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script)})
		if err == nil || !strings.Contains(stderr, "retain_node_exclusion_control") {
			t.Fatalf("node exclusion sabotage not detected by name: %v\n%s", err, stderr)
		}
	}
}
