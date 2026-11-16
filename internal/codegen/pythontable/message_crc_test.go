package pythontable

import (
	"bytes"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"strings"
	"testing"
)

func TestPythonMessageCRC(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, "package messagecrc\nfixed table Root | integrity = \"crc32c\" { value int32 }\n")
	m := tabletext.NewModel(u)
	values := make([]*tabletext.Instance, 3)
	for i, text := range []string{`{"value":0}`, `{"value":7}`, `{"value":-2}`} {
		values[i] = m.New(m.Lookup("Root"))
		var report tabletext.Report
		if !m.Read(values[i], []byte(text), &report) || report.Refused {
			t.Fatalf("fixture: %+v", report)
		}
	}
	wire, err := tablewire.EncodeMessages(m, values)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + `from messagecrc import Root, MessageVocabulary, ANNOUNCEMENT, announce_read, save_messages, measure_messages, load_messages, Refused, LoadReport
with open('wire','rb') as f: wire=f.read()
v=MessageVocabulary();announce_read(v,ANNOUNCEMENT)
values=[]
for n in (0,7,-2):
 x=Root();x.value=n;values.append(x)
assert save_messages(Root,values)==wire and measure_messages(Root,values)==len(wire), 'message_crc_write'
assert [x.value for x in load_messages(Root,v,wire)]==[0,7,-2]
report=LoadReport();report.unknown=77
try:load_messages(Root,v,wire[:-1]+bytes([wire[-1]^1]),report=report)
except Refused as e:assert e.reason=='integrity_mismatch' and report.unknown==77
else:raise AssertionError('message_crc_read')
try:load_messages(Root,v,bytes([3]))
except Refused as e:assert e.reason=='newer_form'
else:raise AssertionError('message_crc_form')
print('ok')
`
	dir := t.TempDir()
	writeTree(t, dir, files)
	inputs := map[string][]byte{"main.py": []byte(probe), "wire": wire}
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
		if err != nil || string(bytes.TrimSpace([]byte(out))) != "ok" {
			t.Fatalf("CRC: %v\n%s\n%s", err, out, stderr)
		}
	}
	if out, err := pyrun.Analyze(t, dir, "messagecrc"); err != nil {
		t.Fatalf("mypy: %v\n%s", err, out)
	}
	for _, fault := range []struct{ old, replacement, expect string }{{`if int.from_bytes(view[-4:], "little") != crc32c(body):`, `if False:`, `message_crc_read`}, {`return data + crc32c(data).to_bytes(4, "little")`, `return data`, `message_crc_write`}} {
		broken := make(map[string][]byte, len(files))
		found := false
		for name, src := range files {
			broken[name] = src
			if strings.HasSuffix(name, "_schema/integrity.py") && strings.Contains(string(src), fault.old) {
				broken[name] = []byte(strings.Replace(string(src), fault.old, fault.replacement, 1))
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
