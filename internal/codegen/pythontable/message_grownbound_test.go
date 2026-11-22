package pythontable

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A GROWN FIXED BOUND in the message form (docs/SPEC-TABLES.md §21.1, F-092):
// a peer that announced [1] wrote one element; this reader's [3] holds that
// element and then one fresh default element per new slot. A union slot is a
// union with no arm and a table slot a new table, never the field's default,
// so the value saves again (the F-092 widenbound_msg rows: the reader padded a
// union slot with a non-union and save_messages refused it).
const msgGrownBoundReader = `package msggrown
fixed table Inner {
 v int32 = 7
}
union Choice {
 count int32
 note string(8)
}
fixed table Root {
 picks [3]Choice
 inners [3]Inner
 counts [3]int16
}
`

func TestPythonMessageGrownBoundPadsOneDefaultPerSlot(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreters")
	reader := loadUnit(t, msgGrownBoundReader)
	writer := loadUnit(t, strings.ReplaceAll(msgGrownBoundReader, "[3]", "[1]"))
	wm := tabletext.NewModel(writer)
	inst := wm.New(wm.Lookup("Root"))
	var rep tabletext.Report
	if !wm.Read(inst, []byte(`{"picks":[{"count":5}],"inners":[{"v":1}],"counts":[6]}`), &rep) || rep.Refused {
		t.Fatalf("fixture: %+v", rep)
	}
	wire, err := tablewire.EncodeMessages(wm, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatal(err)
	}
	// The Go engine's read of the same bytes, saved again, is the value the
	// Python leg must save byte for byte.
	rm := tabletext.NewModel(reader)
	var vocab tablewire.Vocabulary
	var ar tabletext.Report
	if err := vocab.AnnounceRead(tablewire.Announce(writer), &ar); err != nil {
		t.Fatal(err)
	}
	got := []*tabletext.Instance{rm.New(rm.Lookup("Root"))}
	var report tabletext.Report
	if _, ok, err := tablewire.DecodeMessages(rm, got, wire, &vocab, &report); !ok || err != nil || report.Refused {
		t.Fatalf("the engine did not read across the grown bound: %v %+v", err, report)
	}
	want, err := tablewire.EncodeMessages(rm, got)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(reader)
	if err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + `from msggrown import Root, Choice, Inner, MessageVocabulary, announce_read, save_messages, load_messages
with open('peer','rb') as f: peer=f.read()
with open('wire','rb') as f: wire=f.read()
with open('want','rb') as f: want=f.read()
v=MessageVocabulary();announce_read(v,peer)
m=load_messages(Root,v,wire)[0]
assert len(m.picks)==3 and len(m.inners)==3 and len(m.counts)==3, 'slots'
assert m.picks[0].type!=0, 'kept'
for i in (1,2):
 assert isinstance(m.picks[i],Choice) and m.picks[i].type==0, ('union slot', i, m.picks[i])
 assert isinstance(m.inners[i],Inner) and m.inners[i].v==7, ('table slot', i, m.inners[i])
 assert m.counts[i]==0, ('int slot', i, m.counts[i])
assert m.picks[1] is not m.picks[2] and m.inners[1] is not m.inners[2], 'shared'
assert save_messages(Root,[m])==want, 'resave'
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe), "peer": tablewire.Announce(writer), "wire": wire, "want": want}
	dir := t.TempDir()
	writeTree(t, dir, files)
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
		if err != nil || string(bytes.TrimSpace([]byte(out))) != "ok" {
			t.Fatalf("grown bound: %v\n%s\n%s", err, out, stderr)
		}
	}
}
