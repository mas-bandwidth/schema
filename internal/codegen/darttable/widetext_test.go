package darttable

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestDartWideTextWireOracle(t *testing.T) {
	slowtest.Gate(t, "dart")
	producer := unitFrom(t, "package probe\ntable Root {value wstring(64)\nafter int32}\n")
	pm := tabletext.NewModel(producer)
	valueID, afterID := fieldId(producer.Tables["Root"].Fields[0]), fieldId(producer.Tables["Root"].Fields[1])
	var rows []any
	for _, input := range []string{"", "A", "Ω漢", "💡", "A💡Z", "💡Z💡", "A𐐷漢"} {
		units := utf16.Encode([]rune(input))
		payload := make([]byte, len(units)*2)
		for i, unit := range units {
			binary.LittleEndian.PutUint16(payload[i*2:], unit)
		}
		root := pm.New(producer.Tables["Root"])
		root.Fields[0].Cell.Units = units
		root.Fields[0].Count = len(units)
		root.Fields[1].Cell.U = 77
		root.Fields[1].Cell.I = 77
		wire, err := tablewire.Encode(pm, root)
		if err != nil {
			t.Fatal(err)
		}
		for _, bound := range []int{2, 3, 4, 5, 6, 7} {
			u := unitFrom(t, fmt.Sprintf("package probe\ntable Root {value wstring(%d)\nafter int32}\n", bound))
			model := tabletext.NewModel(u)
			back := model.New(u.Tables["Root"])
			var report tabletext.Report
			if ok, err := tablewire.Decode(model, back, wire, &report); !ok || err != nil || report.Malformed || back.Fields[1].Cell.U != 77 {
				t.Fatalf("valid wide Go decode %v %+v", err, report)
			}
			text, err := model.Write(back)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err := json.Unmarshal(text, &fields); err != nil {
				t.Fatal(err)
			}
			expected, _ := fields["value"].(string)
			rows = append(rows, []any{input, hex.EncodeToString(payload), bound, expected, report.Clamped, hex.EncodeToString(wire)})
		}
	}
	var badRows []any
	for _, payload := range [][]byte{{0, 0xd8}, {0, 0xdc}, {0, 0xd8, 65, 0}, {0, 0}, {65}, {65, 0, 0, 0xdc}, {65, 0, 66, 0, 0, 0xdc}, {0, 0xdc, 0, 0xd8}} {
		wire := wideTextForgedWire(payload, valueID, afterID)
		u := unitFrom(t, "package probe\ntable Root {value wstring(2)\nafter int32}\n")
		model := tabletext.NewModel(u)
		back := model.New(u.Tables["Root"])
		var report tabletext.Report
		if ok, err := tablewire.Decode(model, back, wire, &report); !ok || err != nil || !report.Malformed || report.Clamped != 0 || back.Fields[1].Cell.U != 77 || len(back.Fields[0].Cell.Units) != 0 {
			t.Fatalf("invalid wide Go decode %x %v %+v", payload, err, report)
		}
		badRows = append(badRows, hex.EncodeToString(payload))
	}
	validJSON, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	badJSON, err := json.Marshal(badRows)
	if err != nil {
		t.Fatal(err)
	}
	source := "import 'dart:convert';\nclass Report{bool malformed=false;int clamped=0;}\nclass Writer{final bytes=<int>[];void putByte(int value){bytes.add(value&255);}" + wideTextWriterMembers + "}\nclass Reader{final List<int> data;final Report report;int pos=0;late int limit;Reader(this.data,this.report){limit=data.length;}bool has(int n)=>n>=0&&pos+n<=limit;int readByte()=>data[pos++];" + wideTextReaderMembers + "}\n" + fmt.Sprintf(wideTextProbe, string(validJSON), string(badJSON), dartU64Literal(valueID), dartU64Literal(afterID))
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("wide text probe: %v\n%s", err, out)
	}
	t.Log(strings.TrimSpace(string(out)))
}

func wideTextForgedWire(payload []byte, valueID, afterID uint64) []byte {
	// A framed malformed field remains within L; the following scalar must live.
	wire := []byte{1, 1, 33, byte(len(payload))}
	wire = append(wire, payload...)
	wire = append(wire, 2, 4, 77, 0, 0, 0, 0)
	for _, value := range []uint64{valueID, afterID, 2} {
		var out [8]byte
		binary.LittleEndian.PutUint64(out[:], value)
		wire = append(wire, out[:]...)
	}
	return wire
}

const wideTextProbe = `const valueId=%[3]s,afterId=%[4]s;
void check(bool ok,String why){if(!ok)throw StateError(why);}
String hex(List<int> bytes)=>bytes.map((b)=>b.toRadixString(16).padLeft(2,'0')).join();
List<int> unhex(String text)=>[for(var i=0;i<text.length;i+=2)int.parse(text.substring(i,i+2),radix:16)];
String file(String text){final w=Writer()..putByte(1);final ids=<int>[];void leb(int n){while(n>=128){w.putByte((n&127)|128);n>>=7;}w.putByte(n);}void u64(int n){for(var i=0;i<8;i++)w.putByte(n>>(8*i));}if(text.isNotEmpty){ids.add(valueId);leb(ids.length);w.putByte(33);leb(text.length*2);w.putWide(text);}ids.add(afterId);leb(ids.length);w.putByte(4);w.putByte(77);w.putByte(0);w.putByte(0);w.putByte(0);w.putByte(0);for(final id in ids)u64(id);u64(ids.length);return hex(w.bytes);}
void main(){
 final rows=jsonDecode(r'''%[1]s''') as List;
 for(final row in rows){final input=row[0] as String,payload=unhex(row[1] as String),bound=row[2] as int;final writer=Writer();writer.putWide(input);check(hex(writer.bytes)==row[1],'Go UTF16LE payload '+input);check(file(input)==row[5],'Go full wide wire '+input);final report=Report();final reader=Reader([165,...payload,90],report)..pos=1..limit=payload.length+1;final got=reader.readWide(payload.length,bound);check(got==row[3]&&!report.malformed&&report.clamped==row[4]&&reader.pos==payload.length+1&&reader.data.last==90,'Go text/boundary/report '+input);}
 final bad=jsonDecode(r'''%[2]s''') as List;
 for(final spelling in bad){final payload=unhex(spelling as String),report=Report();final reader=Reader([...payload,77,0,0,0],report);check(reader.readWide(payload.length,2)==null&&report.malformed&&report.clamped==0&&reader.pos==payload.length&&reader.readByte()==77,'framed invalid consumes L and preserves parent '+spelling);}
 for(final payload in [<int>[],<int>[65],<int>[65,0]]){final report=Report();final reader=Reader(payload,report);check(reader.readWide(payload.length+1,1)==null&&report.malformed&&report.clamped==0&&reader.pos==0,'truncated framing leaves cursor');}
 final hugeReport=Report();final huge=Reader([165,90],hugeReport)..pos=1;check(huge.readWide(9223372036854775807,2)==null&&hugeReport.malformed&&huge.pos==1,'large length cannot wrap framing check');
 for(final units in [[0],[0xd800],[0xdc00],[0xd800,65]]){final text=String.fromCharCodes(units),writer=Writer();check(!Writer.validWide(text),'invalid writer classifier');var failed=false;try{writer.putWide(text);}on AssertionError{failed=true;}check(failed&&writer.bytes.isEmpty,'writer asserts before bytes');}
 final exact=Report();check(Reader([0x3d,0xd8,0xa1,0xdc],exact).readWide(4,2)=='💡'&&exact.clamped==0,'exact nonBMP unit bound');
 print('wide UTF16 GREEN: '+rows.length.toString()+' Go wire/text/boundary cases, '+bad.length.toString()+' Go malformed-parent controls, truncation and checked writer guards');
}
`
