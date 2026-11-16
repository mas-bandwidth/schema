package pythontable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
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
assert (report.unknown,report.retained,report.retain_lost)==(11,9,2)
saved=value.save_retain(keep, report)
assert saved==expected and report.retain_lost==2
assert value.measure_retain(keep)==len(saved)
assert value.save_retain(keep,report)==saved and report.retain_lost==2
try: value.save_retain(keep,None)
except ValueError as error: assert 'report' in str(error)
else: raise AssertionError('missing report was accepted')
short=TableRetain(bytearray(65536),bytearray()); second=LoadReport()
other=Node.load_retain(data,short,second)
assert (second.retained,second.retain_lost)==(9,2)
size=other.measure_retain(short); dropped=other.save_retain(short,second)
assert len(dropped)==size and second.retain_lost==11
print('retain-roundtrip')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script), "input": input, "expected": want})
		if err != nil || strings.TrimSpace(out) != "retain-roundtrip" {
			t.Fatalf("retain roundtrip: %v\n%s\n%s", err, out, stderr)
		}
		if output, err := pyrun.Analyze(t, dir, "tblrt1"); err != nil {
			t.Fatalf("mypy: %v\n%s", err, output)
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
	script := pyrun.ImportPrelude + `from probe._schema.retain import _resolve_payload, _measure_payload, _emit_payload, _Excluded, _Capacity
from probe import Refused, Root
from probe._schema.retain_known import KNOWN
assert tuple(sorted(KNOWN))==KNOWN and Root.descriptor.fields[0].field_id in KNOWN
ids=[0x111,0x222]
def u64(v): return v.to_bytes(8,'little')
def u32(v): return v.to_bytes(4,'little')
def resolved(kind, payload, cap=128):
    store=bytearray(cap)
    end=_resolve_payload(kind,memoryview(payload),ids,memoryview(store))
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
    end=_resolve_payload(kind,memoryview(payload),ids,memoryview(store))
    record=memoryview(store)[:end]
    refs={ids[0]:2,ids[1]:1}
    size=_measure_payload(kind,record,refs.__getitem__)
    output=bytearray(size)
    assert _emit_payload(kind,record,refs.__getitem__,memoryview(output)) == size
    assert bytes(output)==want, (kind,bytes(output),want)

for kind,payload in [(17,bytes([0])),(14,bytes([2,17,0])),(13,bytes([4,1,17,0,0]))]:
    try: resolved(kind,payload)
    except _Excluded: pass
    else: raise AssertionError('node index retained')
try: resolved(13,bytes([4,1,30,2,0]),8)
except _Capacity: pass
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
from probe._schema.retain import _emit_record
from probe._schema.save import Ctx, _RetainCtx, Writer
keep=TableRetain(bytearray(128), bytearray(24*2))
assert keep.capture(1, (), 0xabc, 30, memoryview(bytes([1])), ids)
node,steps,field,kind,payload=next(keep.records())
ctx=_RetainCtx(keep); output=bytearray(); writer=Writer(output)
assert _emit_record(keep,field,kind,payload,frozenset(),ctx,writer)
assert bytes(output)==bytes([1,30,2]) and ctx.ids==[]
assert keep.id_used==2 and keep.retained_id(0xabc)==1 and keep.retained_id(ids[0])==2
keep.begin_save(); ctx=_RetainCtx(keep); output=bytearray()
assert _emit_record(keep,field,kind,payload,(ids[0],),ctx,Writer(output))
assert ctx.ids==[ids[0]] and keep.retained_id(0xabc)==1 and keep.retained_id(ids[0])==0
assert bytes(output)==bytes([1,30,2])
short=TableRetain(bytearray(128), bytearray(24))
assert short.capture(1, (), 0xabc, 30, memoryview(bytes([1])), ids)
_,_,field,kind,payload=next(short.records())
ctx=_RetainCtx(short); output=bytearray(); writer=Writer(output)
assert not _emit_record(short,field,kind,payload,frozenset(),ctx,writer)
assert ctx.ids==[] and ctx.refs=={} and short.id_used==0 and output==bytearray()
keep.begin_save(); ctx=_RetainCtx(keep)
assert ctx.idref(0x100)==1
keep.add_retained_id(0xabc,2)
assert ctx.idref(0x200)==3
keep.add_retained_id(0xdef,4)
trailer=bytearray(); ctx.write_ids(Writer(trailer))
assert trailer==b''.join(v.to_bytes(8,'little') for v in (0x100,0xabc,0x200,0xdef,4))
bound=TableRetain(bytearray(128), bytearray(24*2)); ctx=_RetainCtx(bound)
for i in range(4096): ctx.idref(i+1)
output=bytearray()
assert not _emit_record(bound,0xabc000,4,memoryview(bytes([1,2,3,4])),frozenset(),ctx,Writer(output))
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
	const guard = "        if kind == 14 and element == 17:\n            raise _Excluded()\n"
	if strings.Count(string(files[path]), guard) != 1 {
		t.Fatal("node exclusion control anchor changed")
	}
	files[path] = []byte(strings.Replace(string(files[path]), guard, "", 1))
	script := pyrun.ImportPrelude + `from probe._schema.retain import _resolve_payload, _Excluded
try:
    _resolve_payload(14,memoryview(bytes([2,17,0])),[],memoryview(bytearray(64)))
except _Excluded:
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
from tblrt1._schema.retain import TableRetain, _capture_file
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
retained,lost=_capture_file(Node,memoryview(data),keep)
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
assert _capture_file(Node,memoryview(saved),again)==(9,0)
assert grouped(again)==grouped(keep)
small=TableRetain(bytearray(), bytearray())
assert _capture_file(Node,memoryview(data),small)==(0,11)
for name in ('retain_excluded_0','retain_excluded_1','retain_excluded_2','retain_excluded_3','retain_excluded_4','retain_excluded_5','retain_rt3'):
    with open(name,'rb') as f: sample=f.read()
    Node.load(sample)
    assert _capture_file(Node,memoryview(sample),keep)==(0,1), name
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
from probe._schema.save import Ctx, _RetainCtx, Writer
keep=TableRetain(bytearray(1024),bytearray(24*2))
assert keep.capture(1,(),0xabc,30,memoryview(bytes([1])),[0x111])
assert keep.capture(1,((1,0),),0xdef,4,memoryview(bytes([1,2,3,4])),[])
def perform(output):
    keep.begin_save(); ctx=_RetainCtx(keep); writer=Writer(output)
    lost=keep.write_tail(1,(),frozenset(),ctx,writer)
    lost+=keep.write_tail(1,((1,0),),frozenset(),ctx,writer)
    lost+=keep.unplaced()
    return writer.count,lost,ctx.ids
size,lost,ids=perform(None)
output=bytearray(); actual,again,ids2=perform(output)
assert size==actual==len(output)==3 and lost==again==1
assert ids==ids2==[] and bytes(output)==bytes([1,30,2])
keep.begin_save(); ctx=_RetainCtx(keep); writer=Writer(bytearray())
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
from probe._schema.save import Ctx, _RetainCtx, Writer
keep=TableRetain(bytearray(128),bytearray(24*2))
assert keep.capture(1,(),0xabc,4,memoryview(bytes([1,2,3,4])),[])
keep.begin_save(); keep.write_tail(1,(),frozenset(),_RetainCtx(keep),Writer(bytearray()))
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
	wire := w.file(trail, w.union("inner", innerPick), w.least())
	script := pyrun.ImportPrelude + `from unions import Root, LoadReport
from unions._schema.retain import TableRetain, _capture_file
with open('wire','rb') as f: wire=f.read()
report=LoadReport(); Root.load(wire,report)
assert report.unknown==3, report.unknown
retain=TableRetain(bytearray(4096),bytearray(240))
assert _capture_file(Root,memoryview(wire),retain)==(2,1)
trail=Root.descriptor.field('trail'); inner=Root.descriptor.field('inner')
from unions import Pick, Inner
arm=next(a for a in Pick.descriptor.arms if a.name=='leaf')
inner_arm=next(a for a in Inner.descriptor.arms if a.name=='pick')
rows=list(retain.records())
assert len(rows)==2 and rows[0][0:2]==(1,((trail.ordinal,0),(trail.ordinal,arm.tag))), rows
assert rows[1][0:2]==(1,((inner.ordinal,inner_arm.tag),(inner.ordinal,arm.tag))), rows
from unions._schema.load import load_table
from unions._schema.save import _encode_retain, Writer
value=load_table(Root,wire,report,retain)
out=bytearray(); size,lost=_encode_retain(value,retain,Writer(out))
assert size==len(out) and lost==0
back=LoadReport(); Root.load(out,back)
assert back.unknown==2
again=TableRetain(bytearray(4096),bytearray(240))
assert _capture_file(Root,memoryview(out),again)==(2,0)
assert {(node,path,identity,kind,bytes(payload)) for node,path,identity,kind,payload in again.records()}=={(node,path,identity,kind,bytes(payload)) for node,path,identity,kind,payload in rows}
value.trail[0]=Pick(); value.inner=Inner()
out=bytearray(); size,lost=_encode_retain(value,retain,Writer(out))
assert size==len(out) and lost==2
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
from probe._schema.retain import TableRetain, _capture_file
with open('wire','rb') as f: wire=f.read()
# The older base refuses this codec; provide the descriptor the list-kind
# prerequisite generates so this isolated scan is tested against Go-validated wire.
field=Root.descriptor.field('grades')
field.codec='array'
field.element=FieldDesc('grades',0,30,'enum',variant_ids=field.variant_ids)
retain=TableRetain(bytearray(4096),bytearray(240))
assert _capture_file(Root,memoryview(wire),retain)==(0,1)
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

func TestPythonRetainNodeBodyInternal(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	const schema = `package probe
table Child { x int32 }
table Root { ptr *Child }
`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	w := &unionWire{}
	ptr := unionCat(w.ref("ptr"), []byte{17, 2})
	nodeRef := w.refID(ir.TableNodeWireId)
	typeRef := w.ref("Child")
	ghost := unionCat(w.ref("future"), []byte{4}, unionI32(7), []byte{0})
	record := nodeRecord(typeRef, ghost)
	payload := unionCat(unionLeb(1), record)
	nodes := unionCat(nodeRef, []byte{12}, unionLeb(uint64(len(payload))), payload)
	wire := w.file(ptr, nodes)
	script := pyrun.ImportPrelude + `from probe import Root, LoadReport
from probe._schema.retain import TableRetain
from probe._schema.load import load_table
from probe._schema.save import _encode_retain, Writer
with open('wire','rb') as f: wire=f.read()
keep=TableRetain(bytearray(1024),bytearray(24*8)); report=LoadReport()
value=load_table(Root,wire,report,keep)
assert (report.unknown,report.retained,report.retain_lost)==(1,1,0)
out=bytearray(); size,lost=_encode_retain(value,keep,Writer(out))
assert out==wire and size==len(wire) and lost==0, (out,wire,lost)
value.ptr=None
out=bytearray(); size,lost=_encode_retain(value,keep,Writer(out))
assert lost==1 and size==len(out) and Root.load(out).ptr is None
print('node-body')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script), "wire": wire})
		if err != nil || strings.TrimSpace(out) != "node-body" {
			t.Fatalf("node body: %v\n%s\n%s", err, out, stderr)
		}
	}
}

func TestPythonRetainTableArrayInternal(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters and strict mypy")
	const schema = `package probe
table Child { x int32 }
table Root { children [..3]Child }
`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	w := &unionWire{}
	w.ref("children")
	ghost := unionCat(w.ref("future"), []byte{4}, unionI32(7), []byte{0})
	first := unionCat(unionLeb(uint64(len(ghost))), ghost)
	second := []byte{1, 0}
	wire := w.file(w.array("children", 13, 2, first, second))
	script := pyrun.ImportPrelude + `from probe import Root, LoadReport
from probe._schema.retain import TableRetain
from probe._schema.load import load_table
from probe._schema.save import _encode_retain, Writer
with open('wire','rb') as f: wire=f.read()
keep=TableRetain(bytearray(1024),bytearray(24*8)); report=LoadReport()
value=load_table(Root,wire,report,keep)
assert (report.unknown,report.retained,report.retain_lost)==(1,1,0)
out=bytearray(); size,lost=_encode_retain(value,keep,Writer(out))
assert out==wire and size==len(wire) and lost==0, (out,wire,lost)
value.children.pop(0)
out=bytearray(); size,lost=_encode_retain(value,keep,Writer(out))
assert lost==0 and size==len(out) and len(Root.load(out).children)==1
print('table-array')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script), "wire": wire})
		if err != nil || strings.TrimSpace(out) != "table-array" {
			t.Fatalf("table array: %v\n%s\n%s", err, out, stderr)
		}
		if output, err := pyrun.Analyze(t, dir, "probe/_schema"); err != nil {
			t.Fatalf("mypy: %v\n%s", err, output)
		}
	}
}

func TestPythonRetainKeyedAndMapInternal(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters and strict mypy")
	const schema = `package probe
enum Slot { A, B }
table Child { x int32 }
table Root {
 keyed [Slot]Child
 mapping map[int32]int32
}
`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	w := &unionWire{}
	keyedID := w.ref("keyed")
	keyID := w.ref("A")
	ghost := unionCat(w.ref("future"), []byte{4}, unionI32(7), []byte{0})
	keyedBody := unionCat([]byte{13, 1}, keyID, unionLeb(uint64(len(ghost))), ghost)
	keyed := unionCat(keyedID, []byte{16}, unionLeb(uint64(len(keyedBody))), keyedBody)
	w.ref("mapping")
	mapKey := unionCat(w.refID(ir.MapKeyWireId), []byte{4}, unionI32(3))
	mapGhost := unionCat(w.ref("map_future"), []byte{4}, unionI32(9))
	entry := unionCat(mapKey, mapGhost, []byte{0})
	mapping := w.array("mapping", 13, 1, unionCat(unionLeb(uint64(len(entry))), entry))
	wire := w.file(keyed, mapping)
	script := pyrun.ImportPrelude + `from probe import Root, LoadReport
from probe._schema.retain import TableRetain
from probe._schema.load import load_table
from probe._schema.save import _encode_retain, Writer
with open('wire','rb') as f: wire=f.read()
keep=TableRetain(bytearray(2048),bytearray(24*16)); report=LoadReport()
value=load_table(Root,wire,report,keep)
assert (report.unknown,report.retained,report.retain_lost)==(2,2,0)
out=bytearray(); size,lost=_encode_retain(value,keep,Writer(out))
assert out==wire and size==len(wire) and lost==0, (out,wire,lost)
value.mapping.clear()
out=bytearray(); size,lost=_encode_retain(value,keep,Writer(out))
assert lost==1 and size==len(out) and Root.load(out).mapping=={}
print('keyed-map')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script), "wire": wire})
		if err != nil || strings.TrimSpace(out) != "keyed-map" {
			t.Fatalf("keyed/map: %v\n%s\n%s", err, out, stderr)
		}
		if output, err := pyrun.Analyze(t, dir, "probe/_schema"); err != nil {
			t.Fatalf("mypy: %v\n%s", err, output)
		}
	}
}

func TestPythonRetainCrossVersionInternal(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters and the retained C++ file")
	root := filepath.Join("..", "..", "..")
	files, err := Generate(loadUnitPaths(t, filepath.Join(root, "test/tables/RT1.schema")))
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
	script := pyrun.ImportPrelude + `from tblrt1 import Node, LoadReport
from tblrt1._schema.retain import TableRetain
from tblrt1._schema.load import load_table
from tblrt1._schema.save import _encode_retain, Writer
with open('input','rb') as f: data=f.read()
with open('expected','rb') as f: expected=f.read()
keep=TableRetain(bytearray(65536), bytearray(24*8192)); report=LoadReport()
value=load_table(Node,data,report,keep)
assert (report.unknown,report.retained,report.retain_lost)==(11,9,2)
out=bytearray(); size,lost=_encode_retain(value,keep,Writer(out))
assert out==expected and size==len(expected) and lost==0, (out.hex(),expected.hex(),lost)
print('cross-version-internal')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script), "input": input, "expected": want})
		if err != nil || strings.TrimSpace(out) != "cross-version-internal" {
			t.Fatalf("cross-version internal: %v\n%s\n%s", err, out, stderr)
		}
	}
}

func TestPythonRetainNames(t *testing.T) {
	for _, name := range []string{"load_retain", "measure_retain", "save_retain"} {
		u := loadUnit(t, "package probe\ntable Root { n int32 }\n")
		u.Tables["Root"].Fields[0].Name = name
		if _, err := Generate(u); err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("field %s was not refused by name: %v", name, err)
		}
	}
	u := loadUnit(t, "package probe\ntable Root { n int32 }\n")
	u.Tables["TableRetain"] = u.Tables["Root"]
	if _, err := Generate(u); err == nil || !strings.Contains(err.Error(), "TableRetain") {
		t.Fatalf("runtime name was not refused: %v", err)
	}
}

func TestPythonRetainFixedRefusal(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters and strict mypy")
	files, err := Generate(loadUnit(t, "package probe\nfixed table Root { n int32 }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["probe/_schema/retain_known.py"]; ok {
		t.Fatal("fixed-only unit emitted a retention ID inventory")
	}
	script := pyrun.ImportPrelude + `from probe import Root, LoadReport
for name, call in (
    ('load_retain', lambda: Root.load_retain(b'',object())),
    ('measure_retain', lambda: Root().measure_retain(object())),
    ('save_retain', lambda: Root().save_retain(object(),LoadReport())),
):
    try: call()
    except ValueError as error: assert name in str(error), str(error)
    else: raise AssertionError(name+' accepted')
print('fixed-refusal')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script)})
		if err != nil || strings.TrimSpace(out) != "fixed-refusal" {
			t.Fatalf("fixed refusal: %v\n%s\n%s", err, out, stderr)
		}
		if output, err := pyrun.Analyze(t, dir, "probe"); err != nil {
			t.Fatalf("mypy: %v\n%s", err, output)
		}
	}
}

func TestPythonRetainDirectFrameBoundaries(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	files, err := Generate(loadUnit(t, "package probe\ntable Root { n int32 }\n"))
	if err != nil {
		t.Fatal(err)
	}
	script := pyrun.ImportPrelude + `from probe._schema.save import Writer
def leb(n):
    out=bytearray()
    while n>=128: out.append((n&127)|128); n >>= 7
    out.append(n); return bytes(out)
for n in (0,1,126,127,128,129,16383,16384):
    payload=bytes([91])*n
    output=bytearray(); w=Writer(output)
    a=w.begin_frame(); b=w.begin_frame(); w.raw(payload); w.finish_frame(b); w.finish_frame(a)
    expected=leb(len(leb(n))+n)+leb(n)+payload
    assert output==expected and w.count==len(expected), n
    count=Writer(None); a=count.begin_frame(); b=count.begin_frame(); count.raw(payload); count.finish_frame(b); count.finish_frame(a)
    assert count.count==len(expected), n
print('frames')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script)})
		if err != nil || strings.TrimSpace(out) != "frames" {
			t.Fatalf("direct frame: %v\n%s\n%s", err, out, stderr)
		}
	}
}

func TestPythonRetainPublicNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters and the retained C++ file")
	root := filepath.Join("..", "..", "..")
	unit := loadUnitPaths(t, filepath.Join(root, "test/tables/RT1.schema"))
	input, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/retain_rt2.bin"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/retain_rt1_save.bin"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, file, anchor, replacement, marker string }{
		{"load", "tblrt1/_schema/retain.py", "                if retain.capture(node, steps, identity, kind, raw, ids):\n", "                if False:\n", "retain_public_load_control"},
		{"save", "tblrt1/_schema/save.py", "    ctx.lost += ctx.retain.write_tail(ctx.node, parent_steps, ctx.known, ctx, writer)\n", "    pass\n", "retain_public_save_control"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files, err := Generate(unit)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(files[tc.file]), tc.anchor) != 1 {
				t.Fatal("public control anchor changed")
			}
			files[tc.file] = []byte(strings.Replace(string(files[tc.file]), tc.anchor, tc.replacement, 1))
			script := pyrun.ImportPrelude + `from tblrt1 import Node, TableRetain, LoadReport
with open('input','rb') as f: data=f.read()
with open('expected','rb') as f: expected=f.read()
r=LoadReport(); keep=TableRetain(bytearray(65536),bytearray(24*8192))
value=Node.load_retain(data,keep,r)
if r.retained != 9 or value.save_retain(keep,r) != expected: raise SystemExit('` + tc.marker + `')
`
			for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
				dir := t.TempDir()
				writeTree(t, dir, files)
				_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script), "input": input, "expected": want})
				if err == nil || !strings.Contains(stderr, tc.marker) {
					t.Fatalf("public %s sabotage not detected: %v\n%s", tc.name, err, stderr)
				}
			}
		})
	}
}

func TestPythonRetainManifestSave(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters and the retain manifest")
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(loadUnitPaths(t, filepath.Join(root, "test/tables/RT1.schema")))
	if err != nil {
		t.Fatal(err)
	}
	generated := t.TempDir()
	unitdir := filepath.Join(generated, "tblrt1")
	if err := os.MkdirAll(unitdir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTree(t, unitdir, files)
	cases := map[string]string{
		"retain_round_trip":   "retain_rt1_save.bin",
		"retain_ids_short":    "retain_rt1_save_id_short.bin",
		"retain_node_index":   "retain_excluded_2_save.bin",
		"retain_enum_variant": "retain_excluded_3_save.bin",
		"retain_union_arm":    "retain_excluded_4_save.bin",
		"retain_keyed_slot":   "retain_excluded_5_save.bin",
		"retain_node_record":  "retain_rt3_save.bin",
	}
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		outdir := t.TempDir()
		cmd := exec.Command(pin, "-W", "error", "-I", "-B", filepath.Join(root, "test/conformance/python/main.py"), filepath.Join(root, "testdata/conformance/tables/MANIFEST.txt"), "retain-save", outdir)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "PYTHON_CONFORMANCE_GENERATED="+generated)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("retain-save driver: %v\n%s", err, output)
		}
		for name, pinFile := range cases {
			got, err := os.ReadFile(filepath.Join(outdir, name))
			if err != nil {
				absent, _ := os.ReadFile(filepath.Join(outdir, name+".absent"))
				t.Fatalf("%s absent: %v %s", name, err, absent)
			}
			want, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables", pinFile))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) {
				t.Fatalf("%s differs from C++ pinned save: got %x want %x", name, got, want)
			}
		}
	}
}

func TestPythonRetainOutputAllocationShape(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	const schema = `package probe
table Child { n int32 }
table Root { child Child }
`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	script := pyrun.ImportPrelude + `import tracemalloc
from probe import Root, TableRetain, LoadReport
def leb(n):
    b=bytearray()
    while n>=128: b.append((n&127)|128); n >>= 7
    b.append(n); return bytes(b)
def input_file(n):
    field=Root.descriptor.field('child').field_id
    ghost=0x123456789abcdef0
    payload=bytes([91])*n
    body=bytes([2,31])+leb(n)+payload+bytes([0])
    return bytes([1,1,13])+leb(len(body))+body+bytes([0])+field.to_bytes(8,'little')+ghost.to_bytes(8,'little')+(2).to_bytes(8,'little')
def probe(n):
    wire=input_file(n)
    keep=TableRetain(bytearray(n+1024),bytearray(24*16)); report=LoadReport()
    value=Root.load_retain(wire,keep,report)
    assert (report.retained,report.retain_lost)==(1,0)
    tracemalloc.start()
    count=value.measure_retain(keep)
    _, measure_peak=tracemalloc.get_traced_memory(); tracemalloc.stop()
    tracemalloc.start()
    saved=value.save_retain(keep,report)
    _, save_peak=tracemalloc.get_traced_memory(); tracemalloc.stop()
    assert count==len(saved) and saved==wire
    return measure_peak,save_peak,count
small=probe(128); large=probe(131072)
assert large[0]-small[0] < 32768, (small,large)
assert large[1]-2*large[2] < 65536, large
print('allocation-shape')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script)})
		if err != nil || strings.TrimSpace(out) != "allocation-shape" {
			t.Fatalf("allocation shape: %v\n%s\n%s", err, out, stderr)
		}
	}
}

func TestPythonRetainTotalIDKnownLast(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	const schema = `package probe
table Child { n int32 }
table Root {
 child Child
 z int32
}
`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	script := pyrun.ImportPrelude + `from probe import Root, TableRetain, LoadReport
def leb(n):
    b=bytearray()
    while n>=128: b.append((n&127)|128); n >>= 7
    b.append(n); return bytes(b)
ids=[Root.descriptor.field('child').field_id]+list(range(1,4096))
body=bytearray()
for reference in range(2,4097):
    body+=leb(reference)+(bytes([30,1]) if reference==2 else bytes([4,1,0,0,0]))
body.append(0)
wire=bytes([1,1,13])+leb(len(body))+body+bytes([0])+b''.join(x.to_bytes(8,'little') for x in ids)+len(ids).to_bytes(8,'little')
keep=TableRetain(bytearray(500000),bytearray(24*4096)); report=LoadReport()
value=Root.load_retain(wire,keep,report)
assert report.retained==4095 and report.retain_lost==0
exact=value.save_retain(keep,report)
assert report.retain_lost==0 and int.from_bytes(exact[-8:],'little')==4096
assert Root.load(exact).z==0
past=exact[:-8]+(0xeffffffffffffffe).to_bytes(8,'little')+(4097).to_bytes(8,'little')
unchanged=LoadReport(); unchanged.unknown=42
try: Root.load_retain(past,keep,unchanged)
except ValueError: pass
else: raise AssertionError('4097-id input was accepted')
assert unchanged.unknown==42
value.z=1
saved=value.save_retain(keep,report)
assert report.retain_lost==1, report.retain_lost
assert Root.load(saved).z==1
assert int.from_bytes(saved[-8:],'little')==4096
print('known-last')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script)})
		if err != nil || strings.TrimSpace(out) != "known-last" {
			t.Fatalf("known-last ID limit: %v\n%s\n%s", err, out, stderr)
		}
	}
}

func TestPythonRetainTotalIDDroppedDefaultSlot(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	const schema = `package probe
enum Slot { A }
table Child { n int32 }
table Root {
 child Child
 keyed [Slot]Child
 z int32
}
`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	script := pyrun.ImportPrelude + `from probe import Root, TableRetain, LoadReport
def leb(n):
    b=bytearray()
    while n>=128: b.append((n&127)|128); n >>= 7
    b.append(n); return bytes(b)
child=Root.descriptor.field('child'); keyed=Root.descriptor.field('keyed')
ids=[child.field_id,keyed.field_id,keyed.keys[0]]+list(range(1,4094))
body=bytearray()
for ref in range(4,4097): body+=leb(ref)+bytes([4,1,0,0,0])
body.append(0)
child_field=bytes([1,13])+leb(len(body))+body
slot=leb(4096)+bytes([4,2,0,0,0,0])
keyed_payload=bytes([13,1,3])+leb(len(slot))+slot
keyed_field=bytes([2,16])+leb(len(keyed_payload))+keyed_payload
wire=bytes([1])+child_field+keyed_field+bytes([0])+b''.join(x.to_bytes(8,'little') for x in ids)+len(ids).to_bytes(8,'little')
keep=TableRetain(bytearray(600000),bytearray(24*4093)); report=LoadReport()
value=Root.load_retain(wire,keep,report)
assert report.retained==4094 and report.retain_lost==0
value.z=1
saved=value.save_retain(keep,report)
assert int.from_bytes(saved[-8:],'little')<=4096
assert Root.load(saved).z==1
assert report.retain_lost==1, report.retain_lost
print('dropped-default-slot')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script)})
		if err != nil || strings.TrimSpace(out) != "dropped-default-slot" {
			t.Fatalf("dropped default slot: %v\n%s\n%s", err, out, stderr)
		}
	}
}

func TestPythonRetainGuardedBody(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	const schema = `package probe
table Child { n int32 }
table Root {
 enabled bool
 if enabled { child Child }
}
`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	w := &unionWire{}
	enabled := unionCat(w.ref("enabled"), []byte{1, 1})
	w.ref("child")
	ghost := unionCat(w.ref("future"), []byte{4}, unionI32(7))
	wire := w.file(enabled, w.table("child", ghost))
	script := pyrun.ImportPrelude + `from probe import Root, TableRetain, LoadReport
with open('wire','rb') as f: wire=f.read()
retain=TableRetain(bytearray(1024),bytearray(24*8)); report=LoadReport()
value=Root.load_retain(wire,retain,report)
assert report.retained==1 and value.enabled
value.enabled=False
out=value.save_retain(retain,report)
assert report.retain_lost==1 and not Root.load(out).enabled
assert out==value.save(), (out,value.save())
print('guarded-body')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script), "wire": wire})
		if err != nil || strings.TrimSpace(out) != "guarded-body" {
			t.Fatalf("guarded retention: %v\n%s\n%s", err, out, stderr)
		}
	}
}
