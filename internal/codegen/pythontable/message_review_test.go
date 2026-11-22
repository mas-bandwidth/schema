package pythontable

import (
	"bytes"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// messageWire encodes one root instance of the writer schema, read from JSON,
// in the message form, and returns the writer's announcement beside it.
func messageWire(t *testing.T, writerSrc, root, doc string) (announcement, wire []byte) {
	t.Helper()
	writer := loadUnit(t, writerSrc)
	wm := tabletext.NewModel(writer)
	instance := wm.New(wm.Lookup(root))
	var rep tabletext.Report
	if !wm.Read(instance, []byte(doc), &rep) || rep.Refused {
		t.Fatalf("fixture %s: %+v", doc, rep)
	}
	wire, err := tablewire.EncodeMessages(wm, []*tabletext.Instance{instance})
	if err != nil {
		t.Fatal(err)
	}
	return tablewire.Announce(writer), wire
}

// runMessageProbe generates the reader schema and runs probe under both pinned
// interpreters; the probe prints "ok" when every property holds.
func runMessageProbe(t *testing.T, readerSrc, probe string, inputs map[string][]byte) {
	t.Helper()
	files, err := Generate(loadUnit(t, readerSrc))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	inputs["main.py"] = []byte(pyrun.ImportPrelude + probe)
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
		if err != nil || string(bytes.TrimSpace([]byte(out))) != "ok" {
			t.Fatalf("%s: %v\n%s\n%s", pin, err, out, stderr)
		}
	}
}

// PYTHON-1: a bytes(N) field read by name from a peer whose announced element
// kind is not uint8 (kind 6) refuses kind_mismatch, as an array field's
// element kind does, and never escapes as a Python exception that is not
// Refused. The announced minimum is added to the count.
func TestPythonMessageBytesElementKind(t *testing.T) {
	t.Parallel()
	const reader = `package msgbyteskind
fixed table Root { data bytes(4) }
`
	inputs := map[string][]byte{}
	peers := []struct{ name, src, doc string }{
		{"u16", "package msgbyteskind\nfixed table Root { data [..4]uint16 }\n", `{"data":[300,7]}`},
		{"i8", "package msgbyteskind\nfixed table Root { data [..4]int8 }\n", `{"data":[-1,7]}`},
		{"f32", "package msgbyteskind\nfixed table Root { data [..4]float32 }\n", `{"data":[1.5]}`},
		{"bool", "package msgbyteskind\nfixed table Root { data [..4]bool }\n", `{"data":[true,false]}`},
		{"table", "package msgbyteskind\nfixed table Inner { v int32 }\nfixed table Root { data [..4]Inner }\n", `{"data":[{"v":3}]}`},
	}
	for _, peer := range peers {
		announcement, wire := messageWire(t, peer.src, "Root", peer.doc)
		inputs[peer.name+".peer"] = announcement
		inputs[peer.name+".wire"] = wire
	}
	announcement, wire := messageWire(t, "package msgbyteskind\nfixed table Root { data [2..4]uint8 }\n", "Root", `{"data":[1,2,3]}`)
	inputs["min.peer"] = announcement
	inputs["min.wire"] = wire
	runMessageProbe(t, reader, `from msgbyteskind import Root, MessageVocabulary, announce_read, load_messages, Refused
def read(name):
 with open(name+'.peer','rb') as f: peer=f.read()
 with open(name+'.wire','rb') as f: wire=f.read()
 v=MessageVocabulary();announce_read(v,peer)
 return load_messages(Root,v,wire)
for name in ('u16','i8','f32','bool','table'):
 try: read(name)
 except Refused as e: assert e.reason=='kind_mismatch',('bytes_element_kind_'+name,e.reason)
 except Exception as e: raise AssertionError('bytes_element_kind_'+name+'_escaped') from e
 else: raise AssertionError('bytes_element_kind_'+name+'_landed')
assert read('min')[0].data==bytes([1,2,3]),'bytes_announced_min'
print('ok')
`, inputs)
}

// PYTHON-2: a fixed array of unions read from a peer with fewer slots is
// padded with empty unions (type 0), as the variable and JSON reads and the
// constructor pad it, never with None.
func TestPythonMessageFixedUnionArrayPad(t *testing.T) {
	t.Parallel()
	schema := func(n string) string {
		return "package msgunionpad\nfixed table Inner { v int32 }\nunion Choice { count int32\n nested Inner }\nfixed table Root { items [" + n + "]Choice }\n"
	}
	announcement, wire := messageWire(t, schema("2"), "Root", `{"items":[{"count":5},{"nested":{"v":3}}]}`)
	inputs := map[string][]byte{"peer": announcement, "wire": wire}
	runMessageProbe(t, schema("4"), `from msgunionpad import Root, Choice, MessageVocabulary, announce_read, load_messages
with open('peer','rb') as f: peer=f.read()
with open('wire','rb') as f: wire=f.read()
v=MessageVocabulary();announce_read(v,peer)
items=load_messages(Root,v,wire)[0].items
assert len(items)==4,'union_pad_count'
assert items[0].type!=0 and items[0].count==5 and items[1].nested.v==3,'union_pad_landed'
for i in (2,3):
 assert isinstance(items[i],Choice) and items[i].type==0,('union_pad_empty',i,items[i])
assert items[2] is not items[3],'union_pad_distinct'
assert items[2:]==Root().items[2:],'union_pad_matches_birth'
print('ok')
`, inputs)
}
