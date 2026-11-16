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
keep=TableRetain(bytearray(65536), bytearray(24*8192))
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
from probe import Refused, Root
from probe._schema.retain_known import KNOWN
assert tuple(sorted(KNOWN))==KNOWN and Root.descriptor.fields[0].field_id in KNOWN
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
from probe._schema.retain import TableRetain
keep=TableRetain(bytearray(256), bytearray(24*2))
assert keep.capture(1, ((3,2),), 0xabc, 13, memoryview(bytes([4,1,30,2,0])), ids)
used=keep.used
entries=list(keep.records())
assert len(entries)==1 and entries[0][:4] == (1,((3,2),),0xabc,13)
assert not keep.capture(1, (), 0xdef, 17, memoryview(bytes([0])), ids)
assert keep.used==used and len(list(keep.records()))==1
small=TableRetain(bytearray(22), bytearray())
assert not small.capture(1, (), 0xabc, 13, memoryview(bytes([4,1,30,2,0])), ids)
assert small.used==0 and list(small.records())==[]
keep.reset(); assert keep.used==0 and keep.id_used==0 and list(keep.records())==[]
from probe._schema.retain import emit_record
from probe._schema.save import Ctx, RetainCtx, Writer
keep=TableRetain(bytearray(128), bytearray(24*2))
assert keep.capture(1, (), 0xabc, 30, memoryview(bytes([1])), ids)
node,steps,field,kind,payload=next(keep.records())
ctx=RetainCtx(keep); output=bytearray(); writer=Writer(output)
assert emit_record(keep,field,kind,payload,frozenset(),ctx,writer)
assert bytes(output)==bytes([1,30,2]) and ctx.ids==[]
assert keep.id_used==2 and keep.retained_id(0xabc)==1 and keep.retained_id(ids[0])==2
keep.begin_save(); ctx=RetainCtx(keep); output=bytearray()
assert emit_record(keep,field,kind,payload,(ids[0],),ctx,Writer(output))
assert ctx.ids==[ids[0]] and keep.retained_id(0xabc)==1 and keep.retained_id(ids[0])==0
assert bytes(output)==bytes([1,30,2])
short=TableRetain(bytearray(128), bytearray(24))
assert short.capture(1, (), 0xabc, 30, memoryview(bytes([1])), ids)
_,_,field,kind,payload=next(short.records())
ctx=RetainCtx(short); output=bytearray(); writer=Writer(output)
assert not emit_record(short,field,kind,payload,frozenset(),ctx,writer)
assert ctx.ids==[] and ctx.refs=={} and short.id_used==0 and output==bytearray()
keep.begin_save(); ctx=RetainCtx(keep)
assert ctx.idref(0x100)==1
keep.add_retained_id(0xabc,2)
assert ctx.idref(0x200)==3
keep.add_retained_id(0xdef,4)
trailer=bytearray(); ctx.write_ids(Writer(trailer))
assert trailer==b''.join(v.to_bytes(8,'little') for v in (0x100,0xabc,0x200,0xdef,4))
bound=TableRetain(bytearray(128), bytearray(24*2)); ctx=RetainCtx(bound)
for i in range(4096): ctx.idref(i+1)
output=bytearray()
assert not emit_record(bound,0xabc000,4,memoryview(bytes([1,2,3,4])),frozenset(),ctx,Writer(output))
assert bound.id_used==0 and len(ctx.ids)==4096 and not output
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

func TestPythonRetainCapture(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters and the C++ retain corpus")
	root := filepath.Join("..", "..", "..")
	u := loadUnitPaths(t, filepath.Join(root, "test/tables/RT1.schema"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/retain_rt2.bin"))
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{"wire": wire}
	expected, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/retain_rt1_save.bin"))
	if err != nil {
		t.Fatal(err)
	}
	inputs["expected"] = expected
	for _, name := range []string{"retain_excluded_0", "retain_excluded_1", "retain_excluded_2", "retain_excluded_3", "retain_excluded_4", "retain_excluded_5", "retain_rt3"} {
		data, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		inputs[name] = data
	}
	script := pyrun.ImportPrelude + `from tblrt1 import Node, LoadReport
from tblrt1._schema.retain import TableRetain, capture_file
with open('wire','rb') as f: data=f.read()
report=LoadReport()
value=Node.load(data,report)
assert report.unknown==11, report.unknown
keep=TableRetain(bytearray(65536),bytearray(24*8192))
from tblrt1._schema.load import load_table
loaded_report=LoadReport()
loaded=load_table(Node,data,loaded_report,keep)
assert (loaded_report.unknown,loaded_report.retained,loaded_report.retain_lost)==(11,9,2)
assert keep.loaded_root is loaded and isinstance(keep.directory,list)
retained,lost=capture_file(Node,memoryview(data),keep)
assert (retained,lost)==(9,2), (retained,lost)
assert len(list(keep.records()))==9
def grouped(retain):
    result={}
    for node,path,identity,kind,payload in retain.records():
        result.setdefault((node,path),[]).append((identity,kind,bytes(payload)))
    return result
with open('expected','rb') as f: saved=f.read()
Node.load(saved)
again=TableRetain(bytearray(65536),bytearray(24*8192))
assert capture_file(Node,memoryview(saved),again)==(9,0)
assert grouped(again)==grouped(keep)
small=TableRetain(bytearray(), bytearray())
assert capture_file(Node,memoryview(data),small)==(0,11)
for name in ('retain_excluded_0','retain_excluded_1','retain_excluded_2','retain_excluded_3','retain_excluded_4','retain_excluded_5','retain_rt3'):
    with open(name,'rb') as f: sample=f.read()
    Node.load(sample)
    assert capture_file(Node,memoryview(sample),keep)==(0,1), name
print('captured')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		inputs["main.py"] = []byte(script)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
		if err != nil || strings.TrimSpace(out) != "captured" {
			t.Fatalf("capture: %v\n%s\n%s", err, out, stderr)
		}
	}
}

func TestPythonRetainPathIndex(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters and strict mypy")
	files, err := Generate(loadUnit(t, "package probe\ntable Root { n int32 }\n"))
	if err != nil {
		t.Fatal(err)
	}
	script := pyrun.ImportPrelude + `import sys
from probe._schema.retain import TableRetain
keep=TableRetain(bytearray(262144),bytearray())
steps=[0]
def trace(frame,event,arg):
    if event=='call' and frame.f_code.co_name in ('_bit','_query_bit'):
        steps[0]+=1
    return trace
sys.settrace(trace)
for index in range(1000):
    before=steps[0]
    assert keep.capture(1,((index,0),),1000+index,4,memoryview(bytes([1,2,3,4])),[])
    assert steps[0]-before <= 128
for index in range(1000):
    before=steps[0]
    row=list(keep.records_at(1,((index,0),)))
    assert len(row)==1 and row[0][0]==1000+index and bytes(row[0][2])==bytes([1,2,3,4])
    assert steps[0]-before <= 128
sys.settrace(None)
assert list(keep.records_at(1,((1001,0),)))==[]
assert keep.has_body(1,()) and keep.has_body(1,((7,0),))
assert not keep.has_body(2,()) and not keep.has_body(1,((1001,0),))
assert keep.capture(1,((7,0),(3,2)),9998,4,memoryview(bytes([1,2,3,4])),[])
assert keep.has_body(1,((7,0),)) and keep.has_body(1,((7,0),(3,2)))
assert not keep.has_body(1,((7,0),(3,3)))
assert keep.capture(1,((7,0),),9999,4,memoryview(bytes([9,8,7,6])),[])
assert [r[0] for r in keep.records_at(1,((7,0),))]==[1007,9999]
first=TableRetain(bytearray(1024),bytearray())
assert first.capture(1,((0,0),),7,4,memoryview(bytes([1,2,3,4])),[])
small=TableRetain(bytearray(first.used+35),bytearray())
assert small.capture(1,((0,0),),7,4,memoryview(bytes([1,2,3,4])),[])
before=small.used
assert not small.capture(1,((1,0),),8,4,memoryview(bytes([1,2,3,4])),[])
assert small.used==before and [r[0] for r in small.records_at(1,((0,0),))]==[7]
from probe._schema.save import Ctx
one,two=object(),object()
remap=TableRetain(bytearray(1024),bytearray())
for node,identity in ((1,11),(2,22),(3,33)):
    assert remap.capture(node,(),identity,4,memoryview(bytes([1,2,3,4])),[])
ctx=Ctx(); ctx.indices[id(one)]=3; ctx.indices[id(two)]=2
remap.begin_save(ctx,[one,two])
assert [r[0] for r in remap.records_at(1,())]==[11]
assert [r[0] for r in remap.records_at(2,())]==[33]
assert [r[0] for r in remap.records_at(3,())]==[22]
ctx=Ctx(); ctx.indices[id(one)]=2
remap.begin_save(ctx,[one,two])
assert [r[0] for r in remap.records_at(2,())]==[22]
assert [r[0] for r in remap.records_at(0,())]==[33]
remap.begin_save(Ctx(),[one,two])
assert [r[0] for r in remap.records_at(0,())]==[22,33]
ctx=Ctx(); ctx.indices[id(one)]=3; ctx.indices[id(two)]=2
remap.begin_save(ctx,[one,two])
assert [r[0] for r in remap.records_at(3,())]==[22]
assert [r[0] for r in remap.records_at(2,())]==[33]
ids=TableRetain(bytearray(),bytearray(24*1000))
for i in range(1000): ids.add_retained_id(i+10,i+1)
assert ids._id_height(ids.id_root) <= 20
for i in range(1000): assert ids.retained_id(i+10)==i+1
ids.truncate_ids(500)
for i in range(500): assert ids.retained_id(i+10)==i+1
assert ids.retained_id(510)==0 and ids.id_used==500
import random
order=list(range(1000)); random.Random(17).shuffle(order)
ids=TableRetain(bytearray(),bytearray(24*1000))
for i,key in enumerate(order): ids.add_retained_id(key+10,i+1)
assert ids._id_height(ids.id_root) <= 20
for i,key in enumerate(order): assert ids.retained_id(key+10)==i+1
ids.truncate_ids(500)
for i,key in enumerate(order): assert ids.retained_id(key+10)==(i+1 if i<500 else 0)
assert ids._id_height(ids.id_root) <= 20
print('path-index')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script)})
		if err != nil || strings.TrimSpace(out) != "path-index" {
			t.Fatalf("path index: %v\n%s\n%s", err, out, stderr)
		}
		if output, err := pyrun.Analyze(t, dir, "probe/_schema"); err != nil {
			t.Fatalf("mypy: %v\n%s", err, output)
		}
	}
}

func TestPythonRetainPathIndexNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	files, err := Generate(loadUnit(t, "package probe\ntable Root { n int32 }\n"))
	if err != nil {
		t.Fatal(err)
	}
	const path = "probe/_schema/retain.py"
	const anchor = "            self._set_word(data, last + 5, start + 1)\n"
	if strings.Count(string(files[path]), anchor) != 1 {
		t.Fatal("path index control anchor changed")
	}
	files[path] = []byte(strings.Replace(string(files[path]), anchor, "            pass\n", 1))
	script := pyrun.ImportPrelude + `from probe._schema.retain import TableRetain
keep=TableRetain(bytearray(1024),bytearray())
for identity in (7,8): assert keep.capture(1,((0,0),),identity,4,memoryview(bytes([1,2,3,4])),[])
if [r[0] for r in keep.records_at(1,((0,0),))] != [7,8]: raise SystemExit('retain_path_link_control')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script)})
		if err == nil || !strings.Contains(stderr, "retain_path_link_control") {
			t.Fatalf("path link sabotage not detected by name: %v\n%s", err, stderr)
		}
	}
}

func TestPythonRetainTail(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters and strict mypy")
	files, err := Generate(loadUnit(t, "package probe\ntable Root { n int32 }\n"))
	if err != nil {
		t.Fatal(err)
	}
	script := pyrun.ImportPrelude + `from probe._schema.retain import TableRetain
from probe._schema.save import Ctx, RetainCtx, Writer
keep=TableRetain(bytearray(1024),bytearray(24*2))
assert keep.capture(1,(),0xabc,30,memoryview(bytes([1])),[0x111])
assert keep.capture(1,((1,0),),0xdef,4,memoryview(bytes([1,2,3,4])),[])
def perform(output):
    keep.begin_save(); ctx=RetainCtx(keep); writer=Writer(output)
    lost=keep.write_tail(1,(),frozenset(),ctx,writer)
    lost+=keep.write_tail(1,((1,0),),frozenset(),ctx,writer)
    lost+=keep.unplaced()
    return writer.count,lost,ctx.ids
size,lost,ids=perform(None)
output=bytearray(); actual,again,ids2=perform(output)
assert size==actual==len(output)==3 and lost==again==1
assert ids==ids2==[] and bytes(output)==bytes([1,30,2])
keep.begin_save(); ctx=RetainCtx(keep); writer=Writer(bytearray())
assert keep.write_tail(1,(),frozenset(),ctx,writer)==0 and keep.unplaced()==1
print('tail')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script)})
		if err != nil || strings.TrimSpace(out) != "tail" {
			t.Fatalf("tail: %v\n%s\n%s", err, out, stderr)
		}
		if output, err := pyrun.Analyze(t, dir, "probe/_schema"); err != nil {
			t.Fatalf("mypy: %v\n%s", err, output)
		}
	}
}

func TestPythonRetainTailNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	files, err := Generate(loadUnit(t, "package probe\ntable Root { n int32 }\n"))
	if err != nil {
		t.Fatal(err)
	}
	const path = "probe/_schema/retain.py"
	const anchor = "            self.storage[record + 4] |= 0x80\n"
	if strings.Count(string(files[path]), anchor) != 1 {
		t.Fatal("placement control anchor changed")
	}
	files[path] = []byte(strings.Replace(string(files[path]), anchor, "            pass\n", 1))
	script := pyrun.ImportPrelude + `from probe._schema.retain import TableRetain
from probe._schema.save import Ctx, RetainCtx, Writer
keep=TableRetain(bytearray(128),bytearray(24*2))
assert keep.capture(1,(),0xabc,4,memoryview(bytes([1,2,3,4])),[])
keep.begin_save(); keep.write_tail(1,(),frozenset(),RetainCtx(keep),Writer(bytearray()))
if keep.unplaced()!=0: raise SystemExit('retain_placement_control')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script)})
		if err == nil || !strings.Contains(stderr, "retain_placement_control") {
			t.Fatalf("placement sabotage not detected by name: %v\n%s", err, stderr)
		}
	}
}

func TestPythonRetainNodeRemapNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	files, err := Generate(loadUnit(t, "package probe\ntable Root { n int32 }\n"))
	if err != nil {
		t.Fatal(err)
	}
	const path = "probe/_schema/retain.py"
	const anchor = "                        mapped = ctx.indices.get(id(obj), 0) if obj is not None else 0\n"
	if strings.Count(string(files[path]), anchor) != 1 {
		t.Fatal("node remap control anchor changed")
	}
	files[path] = []byte(strings.Replace(string(files[path]), anchor, "                        mapped = original\n", 1))
	script := pyrun.ImportPrelude + `from probe._schema.retain import TableRetain
from probe._schema.save import Ctx
one,two=object(),object()
keep=TableRetain(bytearray(1024),bytearray())
for node,identity in ((2,22),(3,33)):
    assert keep.capture(node,(),identity,4,memoryview(bytes([1,2,3,4])),[])
ctx=Ctx(); ctx.indices[id(one)]=3; ctx.indices[id(two)]=2
keep.begin_save(ctx,[one,two])
if [r[0] for r in keep.records_at(2,())] != [33]: raise SystemExit('retain_node_remap_control')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script)})
		if err == nil || !strings.Contains(stderr, "retain_node_remap_control") {
			t.Fatalf("node remap sabotage not detected by name: %v\n%s", err, stderr)
		}
	}
}

func TestPythonRetainUnionArrayCapture(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters and strict mypy")
	files, err := Generate(loadUnit(t, unionsSchema))
	if err != nil {
		t.Fatal(err)
	}
	w := &unionWire{}
	ghost := unionCat(w.ref("future_field"), []byte{4}, unionI32(73), []byte{0})
	first := w.arm("leaf", 13, ghost)
	second := w.arm("future_arm", 4, unionI32(12))
	trail := w.array("trail", 15, 2, first, second)
	nestedGhost := unionCat(w.ref("nested_future"), []byte{4}, unionI32(74), []byte{0})
	nestedLeaf := w.arm("leaf", 13, nestedGhost)
	innerPick := w.arm("pick", 15, nestedLeaf)
	wire := w.file(trail, w.union("inner", innerPick))
	script := pyrun.ImportPrelude + `from unions import Root, LoadReport
from unions._schema.retain import TableRetain, capture_file
with open('wire','rb') as f: wire=f.read()
report=LoadReport(); Root.load(wire,report)
assert report.unknown==3, report.unknown
retain=TableRetain(bytearray(4096),bytearray(240))
assert capture_file(Root,memoryview(wire),retain)==(2,1)
trail=Root.descriptor.field('trail'); inner=Root.descriptor.field('inner')
from unions import Pick, Inner
arm=next(a for a in Pick.descriptor.arms if a.name=='leaf')
inner_arm=next(a for a in Inner.descriptor.arms if a.name=='pick')
rows=list(retain.records())
assert len(rows)==2 and rows[0][0:2]==(1,((trail.ordinal,0),(trail.ordinal,arm.tag))), rows
assert rows[1][0:2]==(1,((inner.ordinal,inner_arm.tag),(inner.ordinal,arm.tag))), rows
print('union-array')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script), "wire": wire})
		if err != nil || strings.TrimSpace(out) != "union-array" {
			t.Fatalf("union array capture: %v\n%s\n%s", err, out, stderr)
		}
		if output, err := pyrun.Analyze(t, dir, "unions/_schema"); err != nil {
			t.Fatalf("mypy: %v\n%s", err, output)
		}
	}
}

func TestPythonRetainEnumArrayCapture(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	const schema = `package probe
enum Grade { A, B }
table Root { grades [2]Grade }
`
	u := loadUnit(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	w := &unionWire{}
	known, unknown := w.ref("A"), w.ref("new_grade")
	wire := w.file(w.array("grades", 30, 2, known, unknown))
	if verdict := unionEngineVerdict(t, u, wire); !strings.HasPrefix(verdict, "read 1 ") {
		t.Fatalf("engine rejected enum fixture: %s", verdict)
	}
	script := pyrun.ImportPrelude + `from probe import Root
from probe._schema.descriptor import FieldDesc
from probe._schema.retain import TableRetain, capture_file
with open('wire','rb') as f: wire=f.read()
# The older base refuses this codec; provide the descriptor the list-kind
# prerequisite generates so this isolated scan is tested against Go-validated wire.
field=Root.descriptor.field('grades')
field.codec='array'
field.element=FieldDesc('grades',0,30,'enum',variant_ids=field.variant_ids)
retain=TableRetain(bytearray(4096),bytearray(240))
assert capture_file(Root,memoryview(wire),retain)==(0,1)
print('enum-array')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script), "wire": wire})
		if err != nil || strings.TrimSpace(out) != "enum-array" {
			t.Fatalf("enum array capture: %v\n%s\n%s", err, out, stderr)
		}
	}
}

func TestPythonRetainRootBodyInternal(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters and strict mypy")
	files, err := Generate(loadUnit(t, "package probe\ntable Root { x int32 }\n"))
	if err != nil {
		t.Fatal(err)
	}
	w := &unionWire{}
	first := unionCat(w.ref("x"), []byte{4}, unionI32(4))
	unknown := unionCat(w.ref("future"), []byte{4}, unionI32(7))
	wire := w.file(first, unknown)
	script := pyrun.ImportPrelude + `from probe import Root, LoadReport
from probe._schema.retain import TableRetain
from probe._schema.load import load_table
from probe._schema.save import _encode_retain, Writer
with open('wire','rb') as f: wire=f.read()
keep=TableRetain(bytearray(1024),bytearray(24*8)); report=LoadReport()
value=load_table(Root,wire,report,keep)
assert (report.unknown,report.retained,report.retain_lost)==(1,1,0)
size,lost=_encode_retain(value,keep,Writer(None))
for _ in range(2):
    out=bytearray(); actual,lost2=_encode_retain(value,keep,Writer(out))
    assert out==wire and size==actual==len(wire) and lost==lost2==0
print('root-body')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script), "wire": wire})
		if err != nil || strings.TrimSpace(out) != "root-body" {
			t.Fatalf("root body: %v\n%s\n%s", err, out, stderr)
		}
		if output, err := pyrun.Analyze(t, dir, "probe/_schema"); err != nil {
			t.Fatalf("mypy: %v\n%s", err, output)
		}
	}
}

func TestPythonRetainNestedBodyInternal(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	const schema = `package probe
table Child { x int32 }
table Root {
 inner Child
 ptr *Child
}
`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	w := &unionWire{}
	ghost := unionCat(w.ref("future"), []byte{4}, unionI32(7))
	wire := w.file(w.table("inner", ghost))
	canonical := &unionWire{}
	canonical.ref("inner")
	canonicalGhost := unionCat(canonical.ref("future"), []byte{4}, unionI32(7))
	expected := canonical.file(canonical.table("inner", canonicalGhost))
	script := pyrun.ImportPrelude + `from probe import Root, LoadReport
from probe._schema.retain import TableRetain
from probe._schema.load import load_table
from probe._schema.save import _encode_retain, Writer
with open('wire','rb') as f: wire=f.read()
with open('expected','rb') as f: expected=f.read()
keep=TableRetain(bytearray(1024),bytearray(24*8)); report=LoadReport()
value=load_table(Root,wire,report,keep)
assert (report.unknown,report.retained,report.retain_lost)==(1,1,0)
out=bytearray(); size,lost=_encode_retain(value,keep,Writer(out))
assert out==expected and size==len(expected) and lost==0, (out,expected,lost)
print('nested-body')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script), "wire": wire, "expected": expected})
		if err != nil || strings.TrimSpace(out) != "nested-body" {
			t.Fatalf("nested body: %v\n%s\n%s", err, out, stderr)
		}
	}
}
