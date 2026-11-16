package pythontable

import (
	"bytes"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpptable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPythonMessageCRC(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, "package messagecrc\nfixed table Root | integrity = \"crc32c\" { value int32 }\nfixed table Plain { value int32 }\n")
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
	plain := m.New(m.Lookup("Plain"))
	var plainReport tabletext.Report
	if !m.Read(plain, []byte(`{"value":7}`), &plainReport) || plainReport.Refused {
		t.Fatalf("plain fixture: %+v", plainReport)
	}
	plainWire, err := tablewire.EncodeMessages(m, []*tabletext.Instance{plain})
	if err != nil {
		t.Fatal(err)
	}
	cppWire := messageCRCCpp(t, u)
	if !bytes.Equal(wire, cppWire) {
		t.Fatalf("C++ vs Go: %x vs %x", cppWire, wire)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + `from messagecrc import Root, Plain, MessageVocabulary, ANNOUNCEMENT, announce_read, save_messages, measure_messages, load_messages, Refused, LoadReport
with open('wire','rb') as f: wire=f.read()
with open('plain','rb') as f: plain=f.read()
v=MessageVocabulary();announce_read(v,ANNOUNCEMENT)
p=Plain();p.value=7
assert save_messages(Plain,[p])==plain and measure_messages(Plain,[p])==len(plain)
assert load_messages(Plain,v,plain)[0].value==7
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
	inputs := map[string][]byte{"main.py": []byte(probe), "wire": wire, "plain": plainWire}
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

func messageCRCCpp(t *testing.T, u *ir.Unit) []byte {
	t.Helper()
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := cpptable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	d := t.TempDir()
	writeTree(t, d, files)
	header := ""
	var sources []string
	for name := range files {
		if !strings.Contains(name, "/") && strings.HasSuffix(name, "Table.h") {
			header = name
		}
		if !strings.Contains(name, "/") && strings.HasSuffix(name, "Table.cpp") {
			sources = append(sources, filepath.Join(d, name))
		}
	}
	if header == "" {
		t.Fatal("missing header")
	}
	ser, err := filepath.Abs("../../../../serialize")
	if err != nil {
		t.Fatal(err)
	}
	code := fmt.Sprintf(`#include <cstdio>
#include <cstdint>
#include <vector>
#include "%s"
using namespace messagecrc;
int main() {
 Root v[3]{}; for (auto &x : v) RootReset(x);
 v[0].value=0; v[1].value=7; v[2].value=-2;
 TableReport report{}; auto n=RootMeasureMessages(v,3,&report);
 if (n<=0) return 2; std::vector<uint8_t> b((size_t)n);
 if (RootSaveMessages(v,3,b.data(),n,&report)!=n) return 3;
 return std::fwrite(b.data(),1,b.size(),stdout)==b.size()?0:4;
}
`, header)
	path := filepath.Join(d, "probe.cpp")
	if err = os.WriteFile(path, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(d, "probe")
	args := append([]string{"-std=c++17", "-O1", "-I" + d, "-I" + ser, path}, sources...)
	args = append(args, "-o", bin)
	if out, err := exec.Command("c++", args...).CombinedOutput(); err != nil {
		t.Fatalf("compile C++ CRC: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run C++ CRC: %v", err)
	}
	return out
}
