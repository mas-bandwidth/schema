package pythontable

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
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
	probe := pyrun.ImportPrelude + `from messageprobe._schema.message import MessageVocabulary, announce_read
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
