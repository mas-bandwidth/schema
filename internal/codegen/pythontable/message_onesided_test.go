package pythontable

import (
	"bytes"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"strings"
	"testing"
)

func TestPythonMessageOneSided(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "generated C++ and pinned Python interpreters")
	reader := loadUnit(t, "package msgoneside\nfixed table Root { low int32 = 5 | min = 5\n high uint16 | max = 9 }\n")
	writer := loadUnit(t, "package msgoneside\nfixed table Root { low int32 = 5\n high uint16 }\n")
	wire := func(uModel *tabletext.Model, text string) []byte {
		inst := uModel.New(uModel.Lookup("Root"))
		var rep tabletext.Report
		if !uModel.Read(inst, []byte(text), &rep) || rep.Refused {
			t.Fatalf("fixture: %+v", rep)
		}
		out, err := tablewire.EncodeMessages(uModel, []*tabletext.Instance{inst})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	rm, wm := tabletext.NewModel(reader), tabletext.NewModel(writer)
	good := wire(rm, `{"low":6,"high":9}`)
	if cpp := messageCppWire(t, reader, `Root v[1]{}; RootReset(v[0]); v[0].low=6; v[0].high=9;`, 1); !bytes.Equal(good, cpp) {
		t.Fatalf("C++ vs Go: %x vs %x", cpp, good)
	}
	low := wire(wm, `{"low":4,"high":0}`)
	high := wire(wm, `{"low":5,"high":10}`)
	var vocab tablewire.Vocabulary
	var ar tabletext.Report
	if err := vocab.AnnounceRead(tablewire.Announce(writer), &ar); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{low, high} {
		var report tabletext.Report
		_, ok, _ := tablewire.DecodeMessages(rm, []*tabletext.Instance{rm.New(rm.Lookup("Root"))}, bad, &vocab, &report)
		if ok || !report.Refused {
			t.Fatalf("engine accepted out of range: %+v", report)
		}
	}
	files, err := Generate(reader)
	if err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + `from msgoneside import Root, MessageVocabulary, ANNOUNCEMENT, announce_read, save_messages, load_messages, InvalidValue, Refused
with open('good','rb') as f: good=f.read()
with open('peer','rb') as f: peer=f.read()
v=MessageVocabulary();announce_read(v,ANNOUNCEMENT)
x=Root();assert (x.low,x.high)==(5,0)
x.low=6;x.high=9
assert save_messages(Root,[x])==good
r=load_messages(Root,v,good)[0];assert (r.low,r.high)==(6,9)
from msgoneside._schema._message_unit import _local, FIELD_KEYS
from msgoneside._schema.message import BitWriter
slot=_local.slots[FIELD_KEYS[('Root','high')]]; shape=_local.entries[slot-1].shape
w=BitWriter();w.put(2,8);w.put(0,8);w.put(slot,_local.ref_bits)
w.put(10-shape.base,shape.bits);w.put(0,_local.ref_bits);forged=w.finish()
try:load_messages(Root,v,forged)
except Refused as e:assert e.reason=='out_of_range'
else:raise AssertionError('one_sided_read')
for field,value in (('low',4),('high',10)):
 x=Root();setattr(x,field,value)
 try:save_messages(Root,[x])
 except InvalidValue:pass
 else:raise AssertionError('one_sided_write')
peer_v=MessageVocabulary();announce_read(peer_v,peer)
for name in ('low','high'):
 with open(name,'rb') as f: bad=f.read()
 try:load_messages(Root,peer_v,bad)
 except Refused as e:assert e.reason=='out_of_range'
 else:raise AssertionError('one_sided_read')
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe), "peer": tablewire.Announce(writer), "good": good, "low": low, "high": high}
	dir := t.TempDir()
	writeTree(t, dir, files)
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
		if err != nil || string(bytes.TrimSpace([]byte(out))) != "ok" {
			t.Fatalf("onesided: %v\n%s\n%s", err, out, stderr)
		}
	}
	if out, err := pyrun.Analyze(t, dir, "msgoneside"); err != nil {
		t.Fatalf("strict mypy: %v\n%s", err, out)
	}
	for _, fault := range []struct{ old, replacement, expect string }{{`if type(value) is int and (value < field.lo or value > field.hi):`, `if False:`, `one_sided_read`}, {`_validate(value, getattr(value, "descriptor"), "", set())`, `pass`, `one_sided_write`}} {
		broken := make(map[string][]byte, len(files))
		found := false
		for name, src := range files {
			broken[name] = src
			if strings.HasSuffix(name, "_schema/message.py") && strings.Contains(string(src), fault.old) {
				mutated := strings.Replace(string(src), fault.old, fault.replacement, 1)
				if fault.expect == "one_sided_read" {
					const nested = `if type(value) is int and fd.codec != "bytes" and (value < fd.lo or value > fd.hi):`
					if strings.Count(mutated, nested) != 1 {
						t.Fatal("nested range control anchor absent")
					}
					mutated = strings.Replace(mutated, nested, "if False:", 1)
				}
				broken[name] = []byte(mutated)
				found = true
			}
		}
		if !found {
			t.Fatalf("fault anchor missing: %s", fault.old)
		}
		d := t.TempDir()
		writeTree(t, d, broken)
		for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
			out, stderr, err := pyrun.Try(t, pin, d, "main.py", inputs)
			if err == nil || !strings.Contains(out+stderr, fault.expect) {
				t.Fatalf("control %s: %v\n%s\n%s", fault.expect, err, out, stderr)
			}
		}
	}
}
