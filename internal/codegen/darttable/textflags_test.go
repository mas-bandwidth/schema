package darttable

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestDartTextFlagsGoOracle(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")
	names := make([]string, 64)
	for i := range names {
		names[i] = fmt.Sprintf("Flag%02d", i)
	}
	source := "package probe\nflags Mask {" + strings.Join(names, ",") + "}\ntable Root {value Mask = {Flag01}\nafter int32}\n"
	u := unitFrom(t, source)
	model := tabletext.NewModel(u)
	inputs := []string{
		`[]`, `["Flag00"]`, `["Flag63"]`, `["Flag63","Flag00"]`,
		`["Flag00","Flag00","Flag63","Flag63"]`,
		`["Flag63","Unknown","Unknown","Flag00"]`,
		`["Flag63",7,null,{},[],true,"Flag00"]`,
		`null`, `7`, `"Flag63"`, `{}`, `true`,
	}
	all, _ := json.Marshal(names)
	inputs = append(inputs, string(all))
	var cases []any
	for _, input := range inputs {
		root := model.New(u.Tables["Root"])
		var report tabletext.Report
		document := `{"value":` + input + `,"after":77}`
		if !model.Read(root, []byte(document), &report) || report.Malformed {
			t.Fatalf("Go flags read %s: %+v", input, report)
		}
		text, err := model.Write(root)
		if err != nil {
			t.Fatal(err)
		}
		wire, err := tablewire.Encode(model, root)
		if err != nil {
			t.Fatal(err)
		}
		selected := []string{}
		for bit, name := range names {
			if root.Fields[0].Cell.U&(uint64(1)<<uint(bit)) != 0 {
				selected = append(selected, name)
			}
		}
		cases = append(cases, []any{input, report.Unknown, report.KindMismatch, report.Duplicate, fmt.Sprint(int64(root.Fields[0].Cell.U)), string(text), hex.EncodeToString(wire), selected})
	}
	// A repeated FIELD is counted by the parent; this helper sees its accepted
	// replacement and must never count repeated names as duplicate properties.
	root := model.New(u.Tables["Root"])
	var duplicate tabletext.Report
	if !model.Read(root, []byte(`{"value":["Flag63"],"value":["Flag00","Flag00"],"after":77}`), &duplicate) || duplicate.Malformed || duplicate.Duplicate != 1 || root.Fields[0].Cell.U != 1 {
		t.Fatalf("Go field duplicate contract %+v", duplicate)
	}
	// The writer refuses bits not named by the declaration, including bit 63.
	short := unitFrom(t, "package probe\nflags Small {Low,High}\ntable Root {value Small}\n")
	sm := tabletext.NewModel(short)
	invalid := sm.New(short.Tables["Root"])
	invalid.Fields[0].Cell.U = uint64(1) << 63
	if _, err := sm.Write(invalid); err == nil {
		t.Fatal("Go flags text writer must refuse unnamed bit63")
	}
	encoded, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	declared, err := json.Marshal(names)
	if err != nil {
		t.Fatal(err)
	}
	// The helper uses the existing JSON sink/string escaping API. These minimal
	// peers provide that unchanged contract without altering production dispatch.
	dart := "import 'dart:convert';\nclass TableTextReport{int unknown=0,kindMismatch=0,duplicate=0;" + textFlagsMembers + "}\nclass _JOut{final b=StringBuffer();void raw(String s)=>b.write(s);void line(int depth){b.write('\\n');b.write('  '*depth);}@override String toString()=>b.toString();}\nString _jsonString(String s)=>jsonEncode(s);\n" + fmt.Sprintf(textFlagsProbe, string(encoded), string(declared), dartU64Literal(fieldId(u.Tables["Root"].Fields[0])), dartU64Literal(fieldId(u.Tables["Root"].Fields[1])))
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(dart), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("flags probe: %v\n%s", err, out)
	}
	t.Log(strings.TrimSpace(string(out)))
}

const textFlagsProbe = `const valueId=%[3]s,afterId=%[4]s;
void check(bool ok,String why){if(!ok)throw StateError(why);}
String text(int mask,List<String> names){final out=_JOut();out.raw('{');out.line(1);out.raw('"value": ');TableTextReport.flagsText(out,mask,names,1);out.raw(',');out.line(1);out.raw('"after": 77');out.line(0);out.raw('}');return out.toString();}
String wire(int mask){final out=<int>[1],ids=<int>[];void u64(int n){for(var i=0;i<8;i++)out.add((n>>(i*8))&255);}if(mask!=2){ids.add(valueId);out.addAll([ids.length,9]);u64(mask);}ids.add(afterId);out.addAll([ids.length,4,77,0,0,0,0]);for(final id in ids)u64(id);u64(ids.length);return out.map((b)=>b.toRadixString(16).padLeft(2,'0')).join();}
void main(){
 final names=(jsonDecode(r'''%[2]s''') as List).cast<String>(),cases=jsonDecode(r'''%[1]s''') as List;
 for(final row in cases){final report=TableTextReport(),value=jsonDecode(row[0] as String);final mask=TableTextReport.flagsField(value,report,names,2);check(mask.toString()==row[4],'nativeuint64 mask '+row[0]);check(report.unknown==row[1]&&report.kindMismatch==row[2]&&report.duplicate==row[3],'Go flags reports '+row[0]);check(text(mask,names)==row[5],'Go flags exact JSON '+row[0]);check(wire(mask)==row[6],'Go flags wire '+row[0]);check(jsonEncode(TableTextReport.flagsNames(mask,names))==jsonEncode(row[7]),'declared output order '+row[0]);}
 final repeat=TableTextReport();final first=TableTextReport.flagsField(['Flag63'],repeat,names,2);final second=TableTextReport.flagsField(['Flag00','Flag00'],repeat,names,first);check(second==1&&repeat.duplicate==0,'parent owns repeated field counting');
 final fallback=TableTextReport();check(TableTextReport.flagsField('Flag00',fallback,names,-9223372036854775808)==-9223372036854775808&&fallback.kindMismatch==1,'wrongshape preserves highbit fallback');
 final out=_JOut();var refused=false;try{TableTextReport.flagsText(out,-9223372036854775808,['Low','High'],0);}on StateError{refused=true;}check(refused&&out.toString().isEmpty,'unnamed bit63 refuses before output');
 check(TableTextReport.flagsNames(-1,names).length==64&&TableTextReport.flagsNames(-9223372036854775808,names).single=='Flag63','allbits and bit63 identities');
 print('flags JSON GREEN: '+cases.length.toString()+' exact Go JSON/wire/report cases; repeated names, highbit/maxmask, wrongshape fallback, unnamed-bit refusal');
}
`
