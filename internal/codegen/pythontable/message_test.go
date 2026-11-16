package pythontable

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const messageSchema = `package messageprobe
fixed table Probe {
    enabled bool
    number int32 | min = -4, max = 10
}
`

// TestPythonMessage compares a batch against the engine's own form-2 bytes.
func TestPythonMessage(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, messageSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Probe"))
	for i := range inst.Fields {
		switch inst.Fields[i].Def.Name {
		case "enabled":
			inst.Fields[i].Cell.B = true
		case "number":
			inst.Fields[i].Cell.I = 7
		}
	}
	want, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst, inst})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from messageprobe import Probe, MessageVocabulary, ANNOUNCEMENT, announce_read, save_messages, measure_messages, load_messages
v = MessageVocabulary()
announce_read(v, ANNOUNCEMENT)
p = Probe()
p.enabled = True
p.number = 7
wire = save_messages(Probe, [p, p])
assert measure_messages(Probe, [p, p]) == len(wire)
values = load_messages(Probe, v, wire)
assert len(values) == 2 and all(x.enabled and x.number == 7 for x in values)
assert len(load_messages(Probe, v, bytearray(wire))) == 2
assert len(load_messages(Probe, v, memoryview(wire))) == 2
print(wire.hex())
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	got := bytes.TrimSpace(pyrun.Run(t, dir, "main.py", inputs))
	gotFloor := bytes.TrimSpace(pyrun.RunFloor(t, dir, "main.py", inputs))
	if string(got) != string(gotFloor) || string(got) != fmt.Sprintf("%x", want) {
		t.Fatalf("message goldens: got %s / %s, want %x", got, gotFloor, want)
	}
	if out, err := pyrun.Analyze(t, dir, "messageprobe"); err != nil {
		t.Fatalf("strict mypy: %v\n%s", err, out)
	}
}

// TestPythonMessageBits holds unaligned widths and padding against a bitwise oracle.
func TestPythonMessageBits(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, messageSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from messageprobe._schema.message import BitReader, BitWriter
from messageprobe import Refused
for offset in range(8):
 for width in range(129):
  value = ((1 << width)-1) ^ (((1 << width)-1)//3) if width else 0
  w = BitWriter(); w.put(0, offset); w.put(value, width)
  result = w.finish()
  expected = bytearray((offset+width+7)//8)
  for i in range(width):
   expected[(offset+i)//8] |= ((value >> i)&1) << ((offset+i)%8)
  assert result == expected, (offset,width)
  r = BitReader(result); assert r.get(offset)==0 and r.get(width)==value
  r.finish()
  if (offset+width)%8:
   broken=bytearray(result); broken[-1] |= 128
   r=BitReader(bytes(broken)); r.get(offset); r.get(width)
   try: r.finish()
   except Refused as exc: assert exc.reason=='wire_damaged'
   else: raise AssertionError('message_padding_control')
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
		if err != nil || string(bytes.TrimSpace([]byte(out))) != "ok" {
			t.Fatalf("bit oracle: %v\n%s\n%s", err, out, stderr)
		}
	}
	if out, err := pyrun.Analyze(t, dir, "messageprobe"); err != nil {
		t.Fatalf("strict mypy: %v\n%s", err, out)
	}
}

// TestPythonMessageAnnouncement reads the engine's real vocabulary and tests terminal refusal.
func TestPythonMessageAnnouncement(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, messageSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from messageprobe._schema.message import MessageVocabulary, BuildVersionMismatch, announce_read
from messageprobe import Refused
with open('announcement','rb') as f: data=f.read()
v=MessageVocabulary()
announce_read(v,data)
assert v.announced and v.entries and v.ref_bits == max(1,len(v.entries).bit_length())
try: announce_read(v,data)
except Refused as exc: assert exc.reason=='second_announcement'
else: raise AssertionError('message_second_announcement_control')
bad=MessageVocabulary()
try: announce_read(bad,data[:-1])
except Refused: pass
else: raise AssertionError('message_announcement_damage_control')
try: announce_read(bad,data)
except Refused as exc: assert exc.reason=='second_announcement'
else: raise AssertionError('message_failed_announcement_terminal')
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe), "announcement": tablewire.Announce(u)}
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
		if err != nil || string(bytes.TrimSpace([]byte(out))) != "ok" {
			t.Fatalf("announcement: %v\n%s\n%s", err, out, stderr)
		}
	}
	if out, err := pyrun.Analyze(t, dir, "messageprobe"); err != nil {
		t.Fatalf("strict mypy: %v\n%s", err, out)
	}
}

// TestPythonMessageRangeInformation shows the shape does not encode the exact maximum.
func TestPythonMessageRangeInformation(t *testing.T) {
	t.Parallel()
	const schema = "package msgbounds\nfixed table Probe { value uint8 | min = 0, max = %s }\n"
	a := loadUnit(t, fmt.Sprintf(schema, "5"))
	b := loadUnit(t, fmt.Sprintf(schema, "7"))
	va, vb := tablewire.MessageVocabulary(a), tablewire.MessageVocabulary(b)
	if len(va) != 1 || len(vb) != 1 || !bytes.Equal(va[0].Encode(nil), vb[0].Encode(nil)) {
		t.Fatal("the two valid writers should announce one identical field shape")
	}
	m := tabletext.NewModel(b)
	inst := m.New(m.Lookup("Probe"))
	inst.Fields[0].Cell.U = 6
	wire, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(b)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from msgbounds import Probe, MessageVocabulary, announce_read, load_messages, save_messages
with open('narrow','rb') as f: announcement=f.read()
with open('wide-wire','rb') as f: wire=f.read()
v=MessageVocabulary(); announce_read(v,announcement)
assert load_messages(Probe,v,wire)[0].value==6
p=Probe();p.value=6
assert save_messages(Probe,[p])==wire
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe), "narrow": tablewire.Announce(a), "wide-wire": wire}
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
		if err != nil || string(bytes.TrimSpace([]byte(out))) != "ok" {
			t.Fatalf("shape ambiguity: %v\n%s\n%s", err, out, stderr)
		}
	}
	narrowFiles, err := Generate(a)
	if err != nil {
		t.Fatal(err)
	}
	narrowDir := t.TempDir()
	writeTree(t, narrowDir, narrowFiles)
	narrowProbe := pyrun.ImportPrelude + `from msgbounds import Probe, MessageVocabulary, announce_read, load_messages, Refused
with open('narrow','rb') as f: announcement=f.read()
with open('wide-wire','rb') as f: wire=f.read()
v=MessageVocabulary();announce_read(v,announcement)
try: load_messages(Probe,v,wire)
except Refused as e: assert e.reason=='out_of_range'
else: raise AssertionError('message_same_build_bound')
print('ok')
`
	inputs["main.py"] = []byte(narrowProbe)
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, narrowDir, "main.py", inputs)
		if err != nil || string(bytes.TrimSpace([]byte(out))) != "ok" {
			t.Fatalf("same-build bound: %v\n%s\n%s", err, out, stderr)
		}
	}
	other := loadUnit(t, "package msgbounds\nfixed table Probe { value uint8 = 1 | min = 0, max = 15 }\n")
	om := tabletext.NewModel(other)
	ov := om.New(om.Lookup("Probe"))
	ov.Fields[0].Cell.U = 0
	otherWire, err := tablewire.EncodeMessages(om, []*tabletext.Instance{ov})
	if err != nil {
		t.Fatal(err)
	}
	forged := append([]byte(nil), tablewire.Announce(other)...)
	copy(forged[3:11], tablewire.Announce(a)[3:11])
	forgedProbe := pyrun.ImportPrelude + `from msgbounds import Probe, MessageVocabulary, announce_read, load_messages, Refused
with open('forged','rb') as f: announcement=f.read()
with open('other-wire','rb') as f: wire=f.read()
v=MessageVocabulary();announce_read(v,announcement)
assert load_messages(Probe,v,wire)[0].value==0
print('ok')
`
	forgedInputs := map[string][]byte{"main.py": []byte(forgedProbe), "forged": forged, "other-wire": otherWire}
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, narrowDir, "main.py", forgedInputs)
		if err != nil || string(bytes.TrimSpace([]byte(out))) != "ok" {
			t.Fatalf("same-build distinct shape: %v\n%s\n%s", err, out, stderr)
		}
	}
}

// TestPythonMessageNestedArray compares nested and fixed-array bodies to the engine.
func TestPythonMessageNestedArray(t *testing.T) {
	t.Parallel()
	const src = `package messagearray
fixed table Child { value int32 }
fixed table Root { nested Child
 counts [3]int32
}
`
	u := loadUnit(t, src)
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(inst, []byte(`{"nested":{"value":7},"counts":[1,2,3]}`), &report) || report.Refused {
		t.Fatalf("engine JSON fixture: %+v", report)
	}
	want, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from messagearray import Root, MessageVocabulary, ANNOUNCEMENT, announce_read, save_messages, load_messages
v=MessageVocabulary();announce_read(v,ANNOUNCEMENT)
p=Root();p.nested.value=7;p.counts=[1,2,3]
w=save_messages(Root,[p])
a=load_messages(Root,v,w)[0]
assert a.nested.value==7 and a.counts==[1,2,3]
print(w.hex())
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
		if err != nil || string(bytes.TrimSpace([]byte(out))) != fmt.Sprintf("%x", want) {
			t.Fatalf("nested array: %v\n%s\n%s\nwant %x", err, out, stderr, want)
		}
	}
}

// TestPythonMessageEvolution uses independently generated peer declarations.
func TestPythonMessageEvolution(t *testing.T) {
	t.Parallel()
	old := loadUnit(t, "package evolve\nfixed table Probe { known bool }\n")
	newer := loadUnit(t, "package evolve\nfixed table Probe { known bool\n extra int32 }\n")
	wrong := loadUnit(t, "package evolve\nfixed table Probe { known int32 }\n")
	m := tabletext.NewModel(newer)
	inst := m.New(m.Lookup("Probe"))
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == "known" {
			inst.Fields[i].Cell.B = true
		}
		if inst.Fields[i].Def.Name == "extra" {
			inst.Fields[i].Cell.I = 17
		}
	}
	wire, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(old)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from evolve import Probe, MessageVocabulary, announce_read, load_messages, Refused, LoadReport
with open('peer','rb') as f: peer=f.read()
with open('wire','rb') as f: wire=f.read()
with open('wrong','rb') as f: wrong=f.read()
v=MessageVocabulary();announce_read(v,peer)
report=LoadReport()
assert load_messages(Probe,v,wire,report=report)[0].known and report.unknown==1
w=MessageVocabulary()
try: announce_read(w,wrong)
except Refused as e: assert e.reason=='kind_mismatch'
else: raise AssertionError('message_kind_control')
try: announce_read(w,peer)
except Refused as e: assert e.reason=='second_announcement'
else: raise AssertionError('message_kind_terminal')
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe), "peer": tablewire.Announce(newer), "wrong": tablewire.Announce(wrong), "wire": wire}
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
		if err != nil || string(bytes.TrimSpace([]byte(out))) != "ok" {
			t.Fatalf("evolution: %v\n%s\n%s", err, out, stderr)
		}
	}
}

// TestPythonMessageNegativeControl plants a padding fault in the emitted runtime.
func TestPythonMessageNegativeControl(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, messageSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	name := "messageprobe/_schema/message.py"
	const original = "if self.get(padding):"
	if strings.Count(string(files[name]), original) != 1 {
		t.Fatal("message padding control lost its anchor")
	}
	files[name] = []byte(strings.Replace(string(files[name]), original, "if self.get(padding) and False:", 1))
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from messageprobe import Refused
from messageprobe._schema.message import BitReader
r=BitReader(b'\xff');r.get(3)
try:r.finish()
except Refused:print('ok')
else:raise AssertionError('message_padding_control')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err == nil || !strings.Contains(stderr, "message_padding_control") {
			t.Fatalf("padding sabotage did not fail by name: %v\n%s", err, stderr)
		}
	}
}

// TestPythonMessageFixedKinds compares strings, bytes, floats and arrays to the engine.
func TestPythonMessageFixedKinds(t *testing.T) {
	t.Parallel()
	const schema = `package msgkinds
fixed table Probe {
 name string(16)
 data bytes(8)
 f32 float32
 f64 float64
 quant float32 | min = -1.0, max = 1.0, resolution = 0.25
 numbers [..3]int16
}
`
	u := loadUnit(t, schema)
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Probe"))
	var report tabletext.Report
	if !m.Read(inst, []byte(`{"name":"hello","data":"AQID","f32":1.5,"f64":-2.25,"quant":0.5,"numbers":[-1,7]}`), &report) || report.Refused {
		t.Fatalf("engine JSON fixture: %+v", report)
	}
	want, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from msgkinds import Probe, MessageVocabulary, ANNOUNCEMENT, announce_read, save_messages, load_messages, InvalidValue
v=MessageVocabulary();announce_read(v,ANNOUNCEMENT)
p=Probe();p.name='hello';p.data=b'\x01\x02\x03';p.f32=1.5;p.f64=-2.25;p.quant=0.5;p.numbers=[-1,7]
w=save_messages(Probe,[p]); q=load_messages(Probe,v,w)[0]
assert (q.name,q.data,q.f32,q.f64,q.quant,q.numbers)==('hello',b'\x01\x02\x03',1.5,-2.25,0.5,[-1,7])
for name,value in (('name','x'*17),('name','a\0b'),('quant',1.5),('f64',float('nan')),('numbers',[1,2,3,4])):
 bad=Probe();setattr(bad,name,value)
 try:save_messages(Probe,[bad])
 except InvalidValue:pass
 else:raise AssertionError('message_fixed_kind_write_refusal')
print(w.hex())
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err != nil || string(bytes.TrimSpace([]byte(out))) != fmt.Sprintf("%x", want) {
			t.Fatalf("fixed kinds: %v\n%s\n%s\nwant %x", err, out, stderr, want)
		}
	}
}

// TestPythonMessageUnionEnum compares name references with the engine.
func TestPythonMessageUnionEnum(t *testing.T) {
	t.Parallel()
	const schema = `package msgunion
enum Mode { Idle, Run }
union Choice { number int32
 toggle bool
 ping
}
fixed table Probe { mode Mode
 choice Choice
}
`
	u := loadUnit(t, schema)
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Probe"))
	var report tabletext.Report
	if !m.Read(inst, []byte(`{"mode":"Run","choice":{"number":7}}`), &report) || report.Refused {
		t.Fatalf("engine JSON fixture: %+v", report)
	}
	want, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from msgunion import Probe, MessageVocabulary, ANNOUNCEMENT, announce_read, save_messages, load_messages
v=MessageVocabulary();announce_read(v,ANNOUNCEMENT)
p=Probe();p.mode=2;p.choice.type=1;p.choice.number=7
w=save_messages(Probe,[p]);q=load_messages(Probe,v,w)[0]
assert q.mode==2 and q.choice.type==1 and q.choice.number==7
print(w.hex())
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err != nil || string(bytes.TrimSpace([]byte(out))) != fmt.Sprintf("%x", want) {
			t.Fatalf("union enum: %v\n%s\n%s\nwant %x", err, out, stderr, want)
		}
	}
}

// TestPythonMessageKeyedOptional preserves keyed names and optional presence.
func TestPythonMessageKeyedOptional(t *testing.T) {
	t.Parallel()
	const schema = `package msgkeyed
enum Slot { head, body, feet }
fixed table Probe { items [Slot]int32
 present ?int32
}
`
	u := loadUnit(t, schema)
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Probe"))
	var report tabletext.Report
	if !m.Read(inst, []byte(`{"items":{"head":7,"feet":9},"present":0}`), &report) || report.Refused {
		t.Fatalf("engine JSON fixture: %+v", report)
	}
	want, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from msgkeyed import Probe, MessageVocabulary, ANNOUNCEMENT, announce_read, save_messages, load_messages
v=MessageVocabulary();announce_read(v,ANNOUNCEMENT)
p=Probe();p.items[1]=7;p.items[3]=9;p.present=0
w=save_messages(Probe,[p]);q=load_messages(Probe,v,w)[0]
assert q.items[1]==7 and q.items[3]==9 and q.present==0
print(w.hex())
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err != nil || string(bytes.TrimSpace([]byte(out))) != fmt.Sprintf("%x", want) {
			t.Fatalf("keyed optional: %v\n%s\n%s\nwant %x", err, out, stderr, want)
		}
	}
}

// TestPythonMessageDamage holds batch atomicity, order and zero-padding rules.
func TestPythonMessageDamage(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, messageSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from messageprobe import Probe, MessageVocabulary, ANNOUNCEMENT, announce_read, save_messages, load_messages, Refused, LoadReport, InvalidValue
from messageprobe._schema._message_unit import FIELD_KEYS, _local
from messageprobe._schema.message import BitWriter
v=MessageVocabulary();announce_read(v,ANNOUNCEMENT)
slots={e.key:i+1 for i,e in enumerate(_local.entries)}
a=slots[FIELD_KEYS[('Probe','enabled')]];b=slots[FIELD_KEYS[('Probe','number')]]
def batch(parts):
 w=BitWriter();w.put(2,8);w.put(0,8)
 for value,width in parts:w.put(value,width)
 return w.finish()
def refuse(wire,reason):
 report=LoadReport();report.unknown=73
 try:load_messages(Probe,v,wire,report=report)
 except Refused as e:assert e.reason==reason,(e.reason,reason)
 else:raise AssertionError('message_damage_control')
 assert report.unknown==73
r=v.ref_bits
refuse(batch([(a,r),(1,1),(a,r),(1,1),(0,r)]),'wire_damaged')
refuse(batch([(a,r),(0,1),(0,r)]),'wire_damaged')
refuse(batch([(b,r),(11,4),(a,r),(1,1),(0,r)]),'wire_damaged')
p=Probe();p.enabled=True;p.number=7;wire=save_messages(Probe,[p])
refuse(wire+b'\0','wire_damaged')
try:load_messages(Probe,v,wire,capacity=0)
except Refused as e:assert e.reason=='batch_too_large'
else:raise AssertionError('message_capacity_control')
for values in ([],[p]*257):
 try:save_messages(Probe,values)
 except InvalidValue:pass
 else:raise AssertionError('message_batch_count_control')
p.number=11
try:save_messages(Probe,[p])
except InvalidValue:pass
else:raise AssertionError('message_range_control')
print('ok')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err != nil || string(bytes.TrimSpace([]byte(out))) != "ok" {
			t.Fatalf("message damage: %v\n%s\n%s", err, out, stderr)
		}
	}
}

// TestPythonMessageHostileAnnouncement checks framing, widths and terminal state.
func TestPythonMessageHostileAnnouncement(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, messageSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from messageprobe._schema.message import MessageVocabulary, BuildVersionMismatch, announce_read
from messageprobe import Refused

def leb(n):
 out=bytearray()
 while n>=128:out.append((n&127)|128);n>>=7
 out.append(n);return bytes(out)
def entry(ident,kind,shape=b''):return ident.to_bytes(8,'little')+bytes([kind])+shape
def announce(raw,extra=False):
 payload=b'\x06'+leb(len(raw))+raw
 body=b'\x01\x09'+(123).to_bytes(8,'little')+b'\x02\x0e'+leb(len(payload))+payload
 ids=[0xfffffffffffffffe,0xfffffffffffffffd]
 if extra:body+=b'\x03\x01\x01';ids.append(987)
 return b'\x01'+body+b'\0'+b''.join(x.to_bytes(8,'little') for x in ids)+len(ids).to_bytes(8,'little')
good=entry(17,4,b'\x01\x04\x07')
v=MessageVocabulary();announce_read(v,announce(good,True));assert v.unknown==1 and v.build_version==123
canonical=announce(good)
unused=canonical[:-8]+(987).to_bytes(8,'little')+(3).to_bytes(8,'little')
reordered=bytearray(canonical);reordered[1],reordered[11]=2,1
reordered[-24:-16],reordered[-16:-8]=canonical[-16:-8],canonical[-24:-16]
for bad in (unused,reordered):
 try:announce_read(MessageVocabulary(),bad)
 except Refused as e:assert e.reason=='wire_damaged'
 else:raise AssertionError('message_announcement_id_order')
def graph_announcement(point):
 payload=b'\x06'+leb(len(good))+good
 body=b'\x01\x09'+(123).to_bytes(8,'little')+b'\x02\x0e'+leb(len(payload))+payload
 if point:body+=b'\x03\x11\x02'
 node=b'\x01\x05\x01\x00'
 body+=b'\x04\x0c'+leb(len(node))+node+b'\x00'
 ids=[0xfffffffffffffffe,0xfffffffffffffffd,987,0xffffffffffffffff,654]
 return b'\x01'+body+b''.join(x.to_bytes(8,'little') for x in ids)+len(ids).to_bytes(8,'little')
v=MessageVocabulary();announce_read(v,graph_announcement(True));assert v.announced
try:announce_read(MessageVocabulary(),graph_announcement(False))
except Refused as e:assert e.reason=='wire_damaged'
else:raise AssertionError('message_announcement_graph')
for form,reason in ((2,'message_form_as_file'),(3,'newer_form')):
 try:announce_read(MessageVocabulary(),bytes([form]))
 except Refused as e:assert e.reason==reason
 else:raise AssertionError('message_announcement_form')
for raw in (entry(17,4,b'\x01\x21\x00'),entry(17,99),good+good,entry(0xfffffffffffffffe,0)):
 v=MessageVocabulary()
 try:announce_read(v,announce(raw))
 except Refused as e:assert e.reason=='wire_damaged'
 else:raise AssertionError('message_announcement_hostile')
 try:announce_read(v,announce(good))
 except Refused as e:assert e.reason=='second_announcement'
 else:raise AssertionError('message_announcement_retry')
for kwargs in ({'max_bytes':1},{'max_entries':0}):
 v=MessageVocabulary()
 try:announce_read(v,announce(good),**kwargs)
 except Refused as e:assert e.reason=='vocabulary_too_large'
 else:raise AssertionError('message_vocabulary_bound')
try:announce_read(MessageVocabulary(),announce(good+b'\x01'),max_entries=1)
except Refused as e:assert e.reason=='vocabulary_too_large'
else:raise AssertionError('message_capacity_before_entry')
bad=announce(good,True).replace(b'\x03\x01\x01',b'\x03\x01\x02',1)
try:announce_read(MessageVocabulary(),bad,max_bytes=1)
except Refused as e:assert e.reason=='wire_damaged'
else:raise AssertionError('message_announcement_damage_priority')
v=MessageVocabulary(own_version=456,strict=True)
try:announce_read(v,announce(good))
except BuildVersionMismatch as e:assert e.reason=='build_version_mismatch' and e.peer==123 and e.own==456 and v.build_version==123
else:raise AssertionError('message_strict_version')
print('ok')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err != nil || string(bytes.TrimSpace([]byte(out))) != "ok" {
			t.Fatalf("hostile announcement: %v\n%s\n%s", err, out, stderr)
		}
	}
}

// TestPythonMessageWide holds 128-bit, fixed-point and flag bits to engine bytes.
func TestPythonMessageWide(t *testing.T) {
	t.Parallel()
	const schema = `package msgwide
flags Cap { A, B, C }
fixed table Probe {
 wide int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
 huge uint128
 price fixed(48,16) | min = -100000, max = 100000
 flags Cap
}
`
	u := loadUnit(t, schema)
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Probe"))
	var report tabletext.Report
	if !m.Read(inst, []byte(`{"wide":"-1267650600228229401496703205376","huge":"1267650600228229401496703205376","price":"3.25","flags":["A","C"]}`), &report) || report.Refused {
		t.Fatalf("engine JSON fixture: %+v", report)
	}
	want, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from msgwide import Probe, MessageVocabulary, ANNOUNCEMENT, announce_read, save_messages, load_messages, InvalidValue
v=MessageVocabulary();announce_read(v,ANNOUNCEMENT)
p=Probe();p.wide=-(1<<100);p.huge=1<<100;p.price=212992;p.flags=5
w=save_messages(Probe,[p]);q=load_messages(Probe,v,w)[0]
assert (q.wide,q.huge,q.price,q.flags)==(-(1<<100),1<<100,212992,5)
bad=Probe();bad.flags=8
try:save_messages(Probe,[bad])
except InvalidValue:pass
else:raise AssertionError('message_mask_width_refusal')
print(w.hex())
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err != nil || string(bytes.TrimSpace([]byte(out))) != fmt.Sprintf("%x", want) {
			t.Fatalf("wide values: %v\n%s\n%s\nwant %x", err, out, stderr, want)
		}
	}
}

// TestPythonMessageWidening uses a smaller writer kind and counts one widening.
func TestPythonMessageWidening(t *testing.T) {
	t.Parallel()
	writer := loadUnit(t, "package msgwiden\nfixed table Probe { value int8 }\n")
	reader := loadUnit(t, "package msgwiden\nfixed table Probe { value int32 }\n")
	m := tabletext.NewModel(writer)
	inst := m.New(m.Lookup("Probe"))
	inst.Fields[0].Cell.I = -7
	wire, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(reader)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from msgwiden import Probe, MessageVocabulary, announce_read, load_messages, LoadReport
with open('peer','rb') as f: peer=f.read()
with open('wire','rb') as f: wire=f.read()
v=MessageVocabulary();announce_read(v,peer);r=LoadReport()
assert load_messages(Probe,v,wire,report=r)[0].value==-7 and r.widened==1
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe), "peer": tablewire.Announce(writer), "wire": wire}
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
		if err != nil || string(bytes.TrimSpace([]byte(out))) != "ok" {
			t.Fatalf("widening: %v\n%s\n%s", err, out, stderr)
		}
	}
}

// TestPythonMessageShapeBenchFunctional compares two valid single-body writer shapes.
func TestPythonMessageShapeBenchFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "timed Python message writer shapes")
	u := loadUnit(t, messageSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `import timeit
from messageprobe import Probe, save_messages
from messageprobe._schema._message_unit import _local, FIELD_KEYS, ROOTS
from messageprobe._schema.message import BitWriter, _write_scalar, save_messages as walk_save
from messageprobe._schema.save import _validate
p=Probe();p.enabled=True;p.number=7
slots={entry.key:i+1 for i,entry in enumerate(_local.entries)}
a=slots[FIELD_KEYS[('Probe','enabled')]];b=slots[FIELD_KEYS[('Probe','number')]]
sa=_local.entries[a-1].shape;sb=_local.entries[b-1].shape;bits=_local.ref_bits
fa,fb=Probe.descriptor.fields
def direct():
 _validate(p,p.descriptor,'',set())
 w=BitWriter();w.put(2,8);w.put(0,8)
 if not fa.is_default(p.enabled):w.put(a,bits);_write_scalar(w,sa,p.enabled)
 if not fb.is_default(p.number):w.put(b,bits);_write_scalar(w,sb,p.number)
 w.put(0,bits);return w.finish()
assert direct()==save_messages(Probe,[p])==walk_save(Probe,[p],_local,FIELD_KEYS,ROOTS)
n=3000
walk=min(timeit.repeat(lambda:walk_save(Probe,[p],_local,FIELD_KEYS,ROOTS),number=n,repeat=5))*1e9/n
generated=min(timeit.repeat(lambda:save_messages(Probe,[p]),number=n,repeat=5))*1e9/n
straight=min(timeit.repeat(direct,number=n,repeat=5))*1e9/n
print('descriptor_ns=%.0f generated_ns=%.0f straight_ns=%.0f' % (walk,generated,straight))
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err != nil {
			t.Fatalf("shape bench %s: %v\n%s\n%s", pin, err, out, stderr)
		}
		t.Logf("%s %s", pin, strings.TrimSpace(out))
	}
}

// TestPythonMessagePublicNameCollision preserves a schema's existing class.
func TestPythonMessagePublicNameCollision(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, "package msgcollision\nfixed table MessageVocabulary { value int32 }\n")
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `import msgcollision
from msgcollision import MessageVocabulary
from msgcollision._schema import _message_unit as message
assert MessageVocabulary is msgcollision.MessageVocabulary
v=message.MessageVocabulary();message.announce_read(v,message.ANNOUNCEMENT)
p=MessageVocabulary();p.value=7
assert message.load_messages(MessageVocabulary,v,message.save_messages(MessageVocabulary,[p]))[0].value==7
print('ok')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err != nil || string(bytes.TrimSpace([]byte(out))) != "ok" {
			t.Fatalf("message export collision: %v\n%s\n%s", err, out, stderr)
		}
	}
	if out, err := pyrun.Analyze(t, dir, "msgcollision"); err != nil {
		t.Fatalf("strict mypy: %v\n%s", err, out)
	}
}

// TestPythonMessageArrayEvolution checks counted minima and fixed-array padding.
func TestPythonMessageArrayEvolution(t *testing.T) {
	t.Parallel()
	cases := []struct{ writer, reader, value, expected string }{
		{"[..4]int32", "[2..4]int32", `[7]`, "refused"},
		{"[1]int32", "[3]int32", `[7]`, "[7, 0, 0]"},
	}
	for _, tc := range cases {
		writer := loadUnit(t, fmt.Sprintf("package msgarrayevo\nfixed table Probe { items %s }\n", tc.writer))
		reader := loadUnit(t, fmt.Sprintf("package msgarrayevo\nfixed table Probe { items %s }\n", tc.reader))
		t.Logf("writer %s version %016x reader %s version %016x", tc.writer, ir.BuildVersion(writer), tc.reader, ir.BuildVersion(reader))
		m := tabletext.NewModel(writer)
		inst := m.New(m.Lookup("Probe"))
		var report tabletext.Report
		if !m.Read(inst, []byte(`{"items":`+tc.value+`}`), &report) || report.Refused {
			t.Fatalf("engine JSON fixture: %+v", report)
		}
		wire, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
		if err != nil {
			t.Fatal(err)
		}
		files, err := Generate(reader)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		writeTree(t, dir, files)
		probe := pyrun.ImportPrelude + `from msgarrayevo import Probe, MessageVocabulary, announce_read, load_messages, Refused
with open('peer','rb') as f: peer=f.read()
with open('wire','rb') as f: wire=f.read()
v=MessageVocabulary();announce_read(v,peer)
try:print(load_messages(Probe,v,wire)[0].items)
except Refused as e:
 assert e.reason=='count_over_bound';print('refused')
`
		inputs := map[string][]byte{"main.py": []byte(probe), "peer": tablewire.Announce(writer), "wire": wire}
		for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
			out, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
			if err != nil || string(bytes.TrimSpace([]byte(out))) != tc.expected {
				t.Fatalf("array evolution %s to %s: %v\n%s\n%s", tc.writer, tc.reader, err, out, stderr)
			}
		}
		if tc.writer == "[..4]int32" {
			// Form-5 projection gave both declarations this same version.
			// Recreate that historical peer even after form 6 fixes the hash.
			const legacy = uint64(0x83c01f94234296fd)
			unit := "msgarrayevo/_schema/_message_unit.py"
			before := fmt.Sprintf("BUILD_VERSION = %d\n", ir.BuildVersion(reader))
			if strings.Count(string(files[unit]), before) != 1 {
				t.Fatal("historical build-version fixture lost its anchor")
			}
			files[unit] = []byte(strings.Replace(string(files[unit]), before, fmt.Sprintf("BUILD_VERSION = %d\n", legacy), 1))
			legacyDir := t.TempDir()
			writeTree(t, legacyDir, files)
			peer := append([]byte(nil), tablewire.Announce(writer)...)
			binary.LittleEndian.PutUint64(peer[3:11], legacy)
			inputs["peer"] = peer
			for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
				out, stderr, err := pyrun.Try(t, pin, legacyDir, "main.py", inputs)
				if err != nil || string(bytes.TrimSpace([]byte(out))) != tc.expected {
					t.Fatalf("historical equal-build array shape: %v\n%s\n%s", err, out, stderr)
				}
			}
		}
	}
}

// TestPythonMessagePacketValue carries a hosted packet value by value.
func TestPythonMessagePacketValue(t *testing.T) {
	t.Parallel()
	const schema = `package msgpacket
type Point { x int16
 y uint16
}
fixed table Probe { point Point }
`
	u := loadUnit(t, schema)
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Probe"))
	var report tabletext.Report
	if !m.Read(inst, []byte(`{"point":{"x":-3,"y":7}}`), &report) || report.Refused {
		t.Fatalf("engine JSON fixture: %+v", report)
	}
	want, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from msgpacket import Probe, MessageVocabulary, ANNOUNCEMENT, announce_read, save_messages, load_messages
v=MessageVocabulary();announce_read(v,ANNOUNCEMENT)
p=Probe();p.point.x=-3;p.point.y=7
w=save_messages(Probe,[p]);q=load_messages(Probe,v,w)[0]
assert q.point.x==-3 and q.point.y==7
print(w.hex())
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err != nil || string(bytes.TrimSpace([]byte(out))) != fmt.Sprintf("%x", want) {
			t.Fatalf("packet value: %v\n%s\n%s\nwant %x", err, out, stderr, want)
		}
	}
}

// TestPythonMessageHostilePayload checks malformed announced-shape payloads.
func TestPythonMessageHostilePayload(t *testing.T) {
	t.Parallel()
	const schema = `package msghostile
fixed table Probe {
 q float32 | min = -1.0, max = 1.0, resolution = 0.25
 f float32
 s string(4)
 list [..2]int32
}
`
	u := loadUnit(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from msghostile import Probe, MessageVocabulary, ANNOUNCEMENT, announce_read, load_messages, Refused
from msghostile._schema._message_unit import FIELD_KEYS, _local
from msghostile._schema.message import BitWriter
v=MessageVocabulary();announce_read(v,ANNOUNCEMENT)
slots={e.key:i+1 for i,e in enumerate(_local.entries)}
def writer(name):
 w=BitWriter();w.put(2,8);w.put(0,8);w.put(slots[FIELD_KEYS[('Probe',name)]],v.ref_bits);return w
def refuse(w,reason):
 w.put(0,v.ref_bits)
 try:load_messages(Probe,v,w.finish())
 except Refused as e:assert e.reason==reason,(e.reason,reason)
 else:raise AssertionError('message_hostile_payload_control')
w=writer('q');w.put(15,4);refuse(w,'wire_damaged')
w=writer('f');w.put(0x7fc00000,32);refuse(w,'non_finite_float')
w=writer('s');w.put(1,3);w.align();w.put(0xff,8);refuse(w,'wire_damaged')
w=writer('list');w.put(3,2);refuse(w,'wire_damaged')
w=writer('f');w.put(0x7fc00000,32);w.put(slots[FIELD_KEYS[('Probe','list')]],v.ref_bits);w.put(3,2);refuse(w,'wire_damaged')
print('ok')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err != nil || string(bytes.TrimSpace([]byte(out))) != "ok" {
			t.Fatalf("hostile payload: %v\n%s\n%s", err, out, stderr)
		}
	}
}

// TestPythonMessageWriteNegativeControl removes the real writer preflight.
func TestPythonMessageWriteNegativeControl(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, messageSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	name := "messageprobe/_schema/message.py"
	const anchor = `_validate(value, getattr(value, "descriptor"), "", set())`
	if strings.Count(string(files[name]), anchor) != 1 {
		t.Fatal("message write control lost its anchor")
	}
	files[name] = []byte(strings.Replace(string(files[name]), anchor, "pass  # removed preflight", 1))
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from messageprobe import Probe, save_messages, InvalidValue
p=Probe();p.number=11
try:save_messages(Probe,[p])
except InvalidValue:print('ok')
else:raise AssertionError('message_write_preflight_control')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err == nil || !strings.Contains(stderr, "message_write_preflight_control") {
			t.Fatalf("write preflight sabotage did not fail by name: %v\n%s", err, stderr)
		}
	}
}

// TestPythonMessageRootIdentity refuses a class from another unit with the same name.
func TestPythonMessageRootIdentity(t *testing.T) {
	t.Parallel()
	a := loadUnit(t, "package msgroota\nfixed table Probe { value int32 }\n")
	b := loadUnit(t, "package msgrootb\nfixed table Probe { value int32 }\n")
	files, err := Generate(a)
	if err != nil {
		t.Fatal(err)
	}
	other, err := Generate(b)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, other)
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `import msgroota as a, msgrootb as b
v=a.MessageVocabulary();a.announce_read(v,a.ANNOUNCEMENT)
try:a.save_messages(b.Probe,[b.Probe()])
except a.InvalidValue:pass
else:raise AssertionError('message_cross_unit_writer')
try:a.load_messages(b.Probe,v,b'\x02\x00\x00')
except a.Refused as e:assert e.reason=='invalid_root'
else:raise AssertionError('message_cross_unit_reader')
print('ok')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err != nil || string(bytes.TrimSpace([]byte(out))) != "ok" {
			t.Fatalf("root identity: %v\n%s\n%s", err, out, stderr)
		}
	}
}

// TestPythonMessageZeroWidthSkip holds an unknown zero-width run to bounded work.
func TestPythonMessageZeroWidthSkip(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, "package msgskip\nfixed table Probe { known bool }\n")
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from msgskip import Probe, MessageVocabulary, announce_read, load_messages, LoadReport
from msgskip._schema.message import BitWriter

def leb(n):
 b=bytearray()
 while n>=128:b.append((n&127)|128);n>>=7
 b.append(n);return bytes(b)
# Unknown kind-14 array, [0,2^32-1], element u8 ranged [0,0].
entry=(1234).to_bytes(8,'little')+b'\x0e'+leb(0)+leb(0xffffffff)+b'\x06\x01\x00\x00'
payload=b'\x06'+leb(len(entry))+entry
body=b'\x01\x09'+(999).to_bytes(8,'little')+b'\x02\x0e'+leb(len(payload))+payload+b'\x00'
announce=b'\x01'+body+(0xfffffffffffffffe).to_bytes(8,'little')+(0xfffffffffffffffd).to_bytes(8,'little')+(2).to_bytes(8,'little')
v=MessageVocabulary();announce_read(v,announce)
w=BitWriter();w.put(2,8);w.put(0,8);w.put(1,v.ref_bits);w.put(0xffffffff,32);w.align();w.put(0,v.ref_bits)
r=LoadReport();out=load_messages(Probe,v,w.finish(),report=r)
assert len(out)==1 and out[0].known is False and r.unknown==1
print('ok')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err != nil || string(bytes.TrimSpace([]byte(out))) != "ok" {
			t.Fatalf("zero-width skip: %v\n%s\n%s", err, out, stderr)
		}
	}
}

// TestPythonMessageUnionArray holds the distinction between absent fields and None elements.
func TestPythonMessageUnionArray(t *testing.T) {
	t.Parallel()
	const schema = `package msgunionarray
union Choice { number int32
 ping
}
fixed table Probe { trail [2]Choice }
`
	u := loadUnit(t, schema)
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Probe"))
	var report tabletext.Report
	if !m.Read(inst, []byte(`{"trail":[{},{"ping":null}]}`), &report) || report.Refused {
		t.Fatalf("engine JSON fixture: %+v", report)
	}
	want, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from msgunionarray import Probe, MessageVocabulary, ANNOUNCEMENT, announce_read, save_messages, load_messages
v=MessageVocabulary();announce_read(v,ANNOUNCEMENT)
p=Probe();p.trail[1].type=2
w=save_messages(Probe,[p]);q=load_messages(Probe,v,w)[0]
assert q.trail[0].type==0 and q.trail[1].type==2
print(w.hex())
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err != nil || string(bytes.TrimSpace([]byte(out))) != fmt.Sprintf("%x", want) {
			t.Fatalf("union array: %v\n%s\n%s\nwant %x", err, out, stderr, want)
		}
	}
}

// TestPythonMessageMixedKinds holds a mixed fixed closure to the engine's bytes.
func TestPythonMessageMixedKinds(t *testing.T) {
	t.Parallel()
	const schema = `package msgmixed
enum Mode { Idle, Run, Jump }
flags Cap { Trace, Verbose, Extra }
fixed table Leaf { n int32
 tag string(8)
}
union Choice { number int32
 leaf Leaf
 ping
}
fixed table Root {
 live bool
 small int8
 wide uint64
 bits12 bits(12)
 ranged int16 | min = -100, max = 100
 f32 float32
 f64 float64
 cf32 float32 | min = -1.0, max = 1.0, resolution = 0.25
 name string(16)
 data bytes(8)
 mode Mode
 cap Cap
 leaf Leaf
 fixed_f32 [3]float32
 fixed_i32 [2]int32
 fixed_leaf [2]Leaf
 fixed_mode [2]Mode
 fixed_cap [2]Cap
 counted_int [..4]int32
 counted_f64 [1..3]float64
 counted_leaf [..2]Leaf
 counted_mode [..2]Mode
 counted_cap [..3]Cap
 selected Choice
 choices [2]Choice
 keyed [Mode]int32
 present ?int32
 x int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
 y uint128
 price fixed(48,16) | min = -100000, max = 100000
}
`
	const input = `{"live":true,"small":-7,"wide":"18446744073709551615","bits12":4095,"ranged":99,"f32":1.5,"f64":-2.25,"cf32":0.5,"name":"héllo","data":"AQID","mode":"Run","cap":["Trace","Extra"],"leaf":{"n":7,"tag":"inner"},"fixed_f32":[1.0,2.5,-3.25],"fixed_i32":[11,12],"fixed_leaf":[{"n":1,"tag":"x"},{"n":2,"tag":"y"}],"fixed_mode":["Idle","Jump"],"fixed_cap":[["Trace"],[]],"counted_int":[-1,0,2147483647,5],"counted_f64":[1.0,2.0],"counted_leaf":[{"n":3,"tag":"z"}],"counted_mode":["Run","Idle"],"counted_cap":[["Verbose"],[]],"selected":{"number":7},"choices":[{},{"ping":null}],"keyed":{"Idle":7,"Jump":9},"present":0,"x":"-1267650600228229401496703205376","y":"1267650600228229401496703205376","price":"3.25"}`
	u := loadUnit(t, schema)
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(inst, []byte(input), &report) || report.Refused {
		t.Fatalf("engine JSON fixture: %+v", report)
	}
	file, err := tablewire.EncodeVariable(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst, inst})
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from msgmixed import Root, MessageVocabulary, ANNOUNCEMENT, announce_read, save_messages, load_messages, measure_messages
from msgmixed._schema.load import load_table
with open('file','rb') as f: value=load_table(Root,f.read())
with open('wire','rb') as f: expected=f.read()
v=MessageVocabulary();announce_read(v,ANNOUNCEMENT)
encoded=save_messages(Root,[value,value])
assert encoded==expected and measure_messages(Root,[value,value])==len(encoded)
decoded=load_messages(Root,v,expected)
assert len(decoded)==2 and save_messages(Root,decoded)==expected
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe), "file": file, "wire": wire}
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
		if err != nil || string(bytes.TrimSpace([]byte(out))) != "ok" {
			t.Fatalf("mixed kinds: %v\n%s\n%s", err, out, stderr)
		}
	}
	if out, err := pyrun.Analyze(t, dir, "msgmixed"); err != nil {
		t.Fatalf("mixed kinds strict mypy: %v\n%s", err, out)
	}
}
