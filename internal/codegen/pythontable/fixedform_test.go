package pythontable_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/pythontable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func pythonFixedTree(t *testing.T, files map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// Functional: both Python pins and a live C++ fixed-form oracle.
func TestPythonFixedForm(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned interpreters and the live C++ fixed-form oracle")
	tc := fixedproof.Cases()[0]
	u := fixedproof.ParseCheckUnit(t, tc.Schema)
	oracle, verify := fixedproof.CppOracle(t, tc, u)
	files, err := pythontable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := pythonFixedTree(t, files)
		probe := pyrun.ImportPrelude + `from probe import T
with open("oracle", "rb") as f:
    rows = T.fixed_load(f.read())
assert len(rows) == 2 and not rows[0].a and rows[0].b and rows[1].a and not rows[1].b
saved = T.fixed_save(rows)
assert len(saved) == T.fixed_measure(2)
with open("saved", "wb") as f:
    f.write(saved)
print("fixed-roundtrip")
`
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "oracle": oracle})
		if err != nil {
			t.Fatalf("fixed-form read/write: %v\n%s\n%s", err, out, stderr)
		}
		saved, err := os.ReadFile(filepath.Join(dir, "saved"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(saved, oracle) {
			t.Fatal("the Python fixed-form bytes differ from the live C++ oracle")
		}
		if err := verify(saved); err != nil {
			t.Fatal(err)
		}
	}
}

const pythonFixedMixedSchema = `package probe

enum E { one, two }

type Point { x int32 }

union Choice {
    point Point
    number int32
    empty
}

fixed table T {
    live bool
    big int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    f float64
    q fixed(16,16) | min = -100, max = 100
    s string(8)
    b bytes(5)
    v [2]int16
    a [..3]Point
    o ?Point
    e E
    z [E]int32
    u Choice
}
`

func pythonFixedMixed(t *testing.T) (*ir.Unit, map[string][]byte, []byte, func([]byte) error) {
	t.Helper()
	tc := fixedproof.RowCase{ID: "python-fixed-mixed", Schema: pythonFixedMixedSchema, Root: "T", Count: 2, CppSetup: `
 value[1].live = true;
 value[1].big = ((serialize::int128_t)1 << 100) + 7;
 value[1].f = -0.0;
 value[1].q = -98304;
 value[1].s_length = 3; memcpy(value[1].s, "one", 3);
 value[1].b_length = 2; value[1].b[0] = 0x42; value[1].b[1] = 0xA1;
 value[1].v[0] = -7; value[1].v[1] = 300;
 value[1].a_count = 1; value[1].a[0].x = 123;
 value[1].o_present = true; value[1].o.x = 99;
 value[1].e = E::two;
 `}
	u := fixedproof.ParseCheckUnit(t, tc.Schema)
	tc.Body = ir.TableFixedTypeBytes(u.Tables["T"])
	oracle, verify := fixedproof.CppOracle(t, tc, u)
	files, err := pythontable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return u, files, oracle, verify
}

func TestPythonFixedFormMixed(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned interpreters and the live C++ fixed-form oracle")
	u, files, oracle, verify := pythonFixedMixed(t)
	script := `from probe import T, Point, LoadReport, InvalidValue, Refused
import struct
with open('oracle','rb') as f:
    wire=f.read()
rows=T.fixed_load(wire)
assert len(rows)==2 and rows[1].big==(1<<100)+7
assert rows[1].q==-98304 and rows[1].s=='one' and rows[1].b==b'\x42\xA1'
assert rows[1].v==[-7,300] and rows[1].a[0].x==123 and rows[1].o.x==99 and rows[1].e==2
assert struct.pack('<d',rows[1].f)==b'\0\0\0\0\0\0\0\x80'
assert T.fixed_plan_max==1
assert T.fixed_measure(2)==len(wire)
saved=T.fixed_save(rows)
assert saved==wire
one=T.fixed_save([rows[1]])
assert T.load(one).save()==one
assert T.load(one).measure()==len(one)
with open('saved','wb') as f:
    f.write(saved)
for n in (0,-1,False):
    try: T.fixed_measure(n)
    except InvalidValue: pass
    else: raise AssertionError('invalid measurement')
for values in ([],[object()]):
    try: T.fixed_save(values)
    except InvalidValue: pass
    else: raise AssertionError('invalid batch')
for attr,value in [('live',1),('big',True),('big',1<<127),('f',float('inf')),('s','\ud800'),('s','a\0b'),('e',3),('v',[1]),('a',[Point(),Point(),Point(),Point()])]:
    item=T()
    setattr(item,attr,value)
    try: item.save()
    except InvalidValue: pass
    else: raise AssertionError('invalid field '+attr)
for cap in (0,1):
    try: T.fixed_load(wire,cap)
    except Refused: pass
    else: raise AssertionError('capacity')
try: T.fixed_load(wire,plan_capacity=0)
except Refused as err: assert err.reason=='plan_too_large'
else: raise AssertionError('plan capacity')
print('mixed-oracle')
`
	_, saved := pythonFixedRun(t, files, script, map[string][]byte{"oracle": oracle})
	if !bytes.Equal(saved, oracle) {
		t.Fatal("mixed byte oracle differs")
	}
	if err := verify(saved); err != nil {
		t.Fatal(err)
	}
	// Mutants touch both live and dead bytes and the last record. Every refusal
	// must leave a previously loaded object and a caller-owned report untouched.
	layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(u.Tables["T"]))
	start := 20 + len(layout) + 8
	offsets := map[string]int{}
	cursor := start
	for _, f := range u.Tables["T"].Fields {
		offsets[f.Name] = cursor
		cursor += int(ir.TableFixedFieldBytes(f))
	}
	inputs := map[string][]byte{"oracle": oracle}
	mutate := func(name string, change func([]byte)) { b := bytes.Clone(oracle); change(b); inputs[name] = b }
	mutate("bool", func(b []byte) { b[offsets["live"]] = 2 })
	mutate("dead-array", func(b []byte) { b[offsets["a"]+4] = 9 })
	mutate("dead-optional", func(b []byte) { b[offsets["o"]+1] = 9 })
	mutate("dead-union", func(b []byte) { b[offsets["u"]+1] = 9 })
	mutate("enum", func(b []byte) { b[offsets["e"]] = 3 })
	mutate("count", func(b []byte) { binary.LittleEndian.PutUint32(b[offsets["a"]:], 4) })
	mutate("text", func(b []byte) { binary.LittleEndian.PutUint32(b[offsets["s"]:], 1); b[offsets["s"]+4] = 0xff })
	mutate("float", func(b []byte) { binary.LittleEndian.PutUint64(b[offsets["f"]:], 0x7ff8000000000000) })
	mutate("reserved", func(b []byte) { b[1] = 1 })
	mutate("layout", func(b []byte) { b[20] ^= 1 })
	mutate("unknown", func(b []byte) { b[8] ^= 1 })
	mutate("last-hash", func(b []byte) { b[20+len(layout)+8+int(ir.TableFixedTypeBytes(u.Tables["T"]))] ^= 1 })
	script = `from probe import T, Refused, LoadReport
with open('oracle','rb') as f:
    witness=T.fixed_load(f.read())
before=T.fixed_save(witness)
report=LoadReport()
report.unknown=19
report.widened=23
for name in ['bool','dead-array','dead-optional','dead-union','enum','count','text','float','reserved','layout','unknown','last-hash']:
    with open(name,'rb') as f: wire=f.read()
    try: T.fixed_load(wire,report=report)
    except Refused: pass
    else: raise AssertionError('accepted '+name)
    assert report.unknown==19 and report.widened==23
    assert T.fixed_save(witness)==before
print('whole-batch-refusal')
`
	pythonFixedRun(t, files, script, inputs)
}

func TestPythonFixedFormStrict(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned analyzer")
	u := fixedproof.ParseCheckUnit(t, pythonFixedMixedSchema)
	files, err := pythontable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := pythonFixedTree(t, files)
	if out, err := pyrun.Analyze(t, dir, "probe"); err != nil {
		t.Fatalf("fixed-form strict analyzer: %v\n%s", err, out)
	}
}

func TestPythonFixedFormLineage(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned interpreters")
	old := fixedproof.ParseCheckUnit(t, "package probe\nfixed table T {\n a uint16 = 1\n b int32 = 10\n}\n")
	newer := fixedproof.ParseCheckUnit(t, "package probe\nfixed table T {\n a uint32 = 1\n b int64 = 10\n c int32 = 99\n}\n")
	entry, _ := fixedproof.LineageEntryOf(old, "T")
	body := binary.LittleEndian.AppendUint16(nil, 123)
	body = binary.LittleEndian.AppendUint32(body, uint32(0xffffffec))
	wire := fixedproof.MakeFixedFile(entry.Wire, entry.Layout, body, body)
	runner := pythonFixedProofRunner{}
	n, reason, fields, _, counters := runner.LoadLineage(t, newer, "T", map[string][]fixedproof.LineageEntry{"T": {entry}}, wire, 2)
	if n != 2 || fields["r0.a"] != "123" || fields["r0.b"] != "-20" || fields["r0.c"] != "99" || counters.Widened != 4 {
		t.Fatalf("lineage: %d %s %v %+v", n, reason, fields, counters)
	}
	entry.Retired = true
	if n, reason, _, _, _ := runner.LoadLineage(t, newer, "T", map[string][]fixedproof.LineageEntry{"T": {entry}}, wire, 2); n != -1 || reason != "layout_unsupported" {
		t.Fatalf("retirement: %d %s", n, reason)
	}
	if n, reason, _, _, _ := runner.LoadLineage(t, newer, "T", nil, wire, 2); n != -1 || reason != "layout_unknown" {
		t.Fatalf("unknown: %d %s", n, reason)
	}
}

func TestPythonFixedFormNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned interpreters and the live C++ fixed-form oracle")
	tc := fixedproof.Cases()[0]
	u := fixedproof.ParseCheckUnit(t, tc.Schema)
	oracle, _ := fixedproof.CppOracle(t, tc, u)
	base, err := pythontable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, control := range []struct{ name, anchor, planted, probe string }{
		{"file", "if source[0] != 3:", "if False:", `from probe import T, Refused
with open('oracle','rb') as f: wire=bytearray(f.read())
wire[0]=1
try: T.fixed_load(wire)
except Refused: pass
else: raise SystemExit('fixed_file_control')
`},
		{"writer", "records.append(root[\"prefix\"] + image)", "image[0] ^= 1\n        records.append(root[\"prefix\"] + image)", `from probe import T
with open('oracle','rb') as f: wire=f.read()
rows=T.fixed_load(wire)
if T.fixed_save(rows)!=wire: raise SystemExit('fixed_writer_control')
`},
	} {
		t.Run(control.name, func(t *testing.T) {
			t.Parallel()
			files := map[string][]byte{}
			for k, v := range base {
				files[k] = v
			}
			path := "probe/_schema/fixedform.py"
			source := string(files[path])
			if strings.Count(source, control.anchor) != 1 {
				t.Fatalf("control %s lost its runtime anchor", control.name)
			}
			files[path] = []byte(strings.Replace(source, control.anchor, control.planted, 1))
			for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
				dir := pythonFixedTree(t, files)
				_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"oracle": oracle, "main.py": []byte(pyrun.ImportPrelude + control.probe)})
				if err == nil || !strings.Contains(stderr, "fixed_"+control.name+"_control") {
					t.Fatalf("%s sabotage did not turn its named probe red: %v\n%s", control.name, err, stderr)
				}
			}
		})
	}
}

func TestPythonFixedFormBirthDefaults(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned interpreters and analyzer")
	u := fixedproof.ParseCheckUnit(t, "package probe\nfixed table T {\n a [2..3]int16\n f float64 = 1.0\n g float32 = 0.3 | min = 0.0, max = 1.0, resolution = 0.1\n q fixed(16,16) = 1.5 | min = 0, max = 10\n}\n")
	files, err := pythontable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	script := `from probe import T, FixedLoadReport, Refused
import struct
value=T()
assert value.a == [0,0] and type(value.f) is float and value.f == 1.0
assert value.g == struct.unpack('<f',struct.pack('<f',0.3))[0]
assert value.q == 98304
wire=value.save()
report=FixedLoadReport()
back=T.load(wire,report)
assert back.a == value.a and back.f == value.f and back.g == value.g and back.q == value.q
assert report.layout_hash == int.from_bytes(wire[8:16],'little')
report.layout_hash=123
try: T.load(wire[:-1],report)
except Refused: pass
else: raise AssertionError('truncated')
assert report.layout_hash==123
print('birth-defaults')
`
	pythonFixedRun(t, files, script, nil)
}

func TestPythonFixedFormWriterBounds(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned interpreters")
	source := `package probe
 type Inner { n int128 | min = -10, max = 10 }
 fixed table T {
  a int32 | min = 0, max = 10
  child Inner
  arr [1..3]uint8
  f float32 | min = 0.0, max = 1.0, resolution = 0.1
 }
 `
	old := fixedproof.ParseCheckUnit(t, source)
	newer := fixedproof.ParseCheckUnit(t, strings.ReplaceAll(strings.ReplaceAll(source, "max = 10", "max = 20"), "max = 1.0", "max = 2.0"))
	st := old.Tables["T"]
	layout, hash, size := fixedproof.RootFacts(st)
	body := make([]byte, size)
	binary.LittleEndian.PutUint32(body, 9)
	body[4] = 5
	binary.LittleEndian.PutUint32(body[20:], 1)
	body[24] = 7
	binary.LittleEndian.PutUint32(body[27:], 0x3f000000)
	wire := fixedproof.MakeFixedFile(hash, layout, body)
	entry := pythontable.FixedLineageEntry{Wire: hash, Layout: layout, Record: 8 + size, Writer: st}
	files, err := pythontable.GenerateLineage(newer, map[string][]pythontable.FixedLineageEntry{"T": {entry}})
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{"wire": wire}
	for _, c := range []struct {
		name   string
		change func([]byte)
	}{
		{"range", func(b []byte) { binary.LittleEndian.PutUint32(b, 11) }},
		{"wide", func(b []byte) { b[4] = 11 }},
		{"minimum", func(b []byte) { binary.LittleEndian.PutUint32(b[20:], 0) }},
		{"float", func(b []byte) { binary.LittleEndian.PutUint32(b[27:], 0x3fc00000) }},
	} {
		b := bytes.Clone(body)
		c.change(b)
		inputs[c.name] = fixedproof.MakeFixedFile(hash, layout, b)
	}
	script := `from probe import T, Refused, LoadReport
with open('wire','rb') as f: rows=T.fixed_load(f.read())
assert rows[0].a==9 and rows[0].child.n==5 and rows[0].arr==[7] and rows[0].f==0.5
report=LoadReport()
report.widened=91
for name in ['range','wide','minimum','float']:
    with open(name,'rb') as f: wire=f.read()
    try: T.fixed_load(wire,report=report)
    except Refused: pass
    else: raise AssertionError('writer bound lost: '+name)
    assert report.widened==91
print('writer-snapshot-bounds')
`
	pythonFixedRun(t, files, script, inputs)
	// Current lock lineage has no complete historical writer snapshot. Keep
	// this availability limitation visible; never use wider reader bounds as
	// if they proved the writer's narrower contract.
	entry.Writer = nil
	files, err = pythontable.GenerateLineage(newer, map[string][]pythontable.FixedLineageEntry{"T": {entry}})
	if err != nil {
		t.Fatal(err)
	}
	pythonFixedRun(t, files, `from probe import T, Refused
with open('wire','rb') as f: wire=f.read()
try: T.fixed_load(wire)
except Refused as err: assert err.reason=='layout_unsupported'
else: raise AssertionError('missing historical facts accepted')
print('missing-writer-metadata-hold')
`, map[string][]byte{"wire": wire})
}
