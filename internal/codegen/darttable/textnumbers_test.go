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

func TestDartTextNumbersExactTokens(t *testing.T) {
	slowtest.Gate(t, "dart")
	// Canonical integers are independent expected values, not a normalization
	// computed by the implementation under test. Go's scalar reader currently
	// rounds fraction/exponent spellings via float64; use these exact canonical
	// spellings for its wire oracle, including the fraction/exponent cases.
	cases := []struct {
		token, canonical string
		unsigned         bool
		clamp, mismatch  int
	}{
		{"9223372036854775808", "9223372036854775808", true, 0, 0},
		{"9223372036854775809", "9223372036854775809", true, 0, 0},
		{"18446744073709551614", "18446744073709551614", true, 0, 0},
		{"18446744073709551615", "18446744073709551615", true, 0, 0},
		{"18446744073709551615.0", "18446744073709551615", true, 0, 0},
		{"184467440737095516150e-1", "18446744073709551615", true, 0, 0},
		{"1.8446744073709551615e19", "18446744073709551615", true, 0, 0},
		{"9007199254740993.0", "9007199254740993", true, 0, 0},
		{"18446744073709551616", "18446744073709551615", true, 1, 0},
		{"-1", "0", true, 1, 0},
		{"-0.0e999999999999999999999999", "0", true, 0, 0},
		{"9223372036854775807", "9223372036854775807", false, 0, 0},
		{"9223372036854775808", "9223372036854775807", false, 1, 0},
		{"-9223372036854775808", "-9223372036854775808", false, 0, 0},
		{"-9223372036854775809", "-9223372036854775808", false, 1, 0},
		{"1e999999999999999999999999999", "18446744073709551615", true, 1, 0},
		{"-1e999999999999999999999999999", "-9223372036854775808", false, 1, 0},
		{"1.0000000000000000000000000001", "0", false, 0, 1},
		{"18446744073709551615.0000000001", "0", true, 0, 1},
		{"1e-999999999999999999999999999", "0", false, 0, 1},
		{"12.5e1", "125", false, 0, 0},
		{"1200e-2", "12", false, 0, 0},
	}
	var rows []any
	for _, tc := range cases {
		typ := "int64"
		kind := 5
		if tc.unsigned {
			typ = "uint64"
			kind = 9
		}
		u := unitFrom(t, "package probe\ntable Root { value "+typ+" }\n")
		model := tabletext.NewModel(u)
		root := model.New(u.Tables["Root"])
		var report tabletext.Report
		if !model.Read(root, []byte(`{"value":`+tc.canonical+`}`), &report) || report.Malformed || report.Clamped != 0 || report.KindMismatch != 0 {
			t.Fatalf("canonical oracle %s: %+v", tc.canonical, report)
		}
		wire, err := tablewire.Encode(model, root)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, []any{tc.token, tc.unsigned, tc.clamp, tc.mismatch, hex.EncodeToString(wire), kind})
	}
	encoded, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	source := "import 'dart:convert';\nclass TableTextReport {int clamped=0,kindMismatch=0;" + textNumberMembers + "}\n" + fmt.Sprintf(textNumbersProbe, string(encoded), dartU64Literal(fieldId(unitFrom(t, "package probe\ntable Root { value uint64 }\n").Tables["Root"].Fields[0])))
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	// The same exactness assertion must fail when tokens pass through jsonDecode.
	red := exec.Command(dartBinary(t), "-DLEGACY_NUMBER=true", "probe.dart")
	red.Dir = dir
	out, err := red.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "wire oracle 9223372036854775809") {
		t.Fatalf("legacy precision negative control did not fail at adjacent uint64: %v\n%s", err, out)
	}
	t.Log("legacy jsonDecode RED: adjacent uint64 wire identity lost")
	green := exec.Command(dartBinary(t), "probe.dart")
	green.Dir = dir
	out, err = green.CombinedOutput()
	if err != nil {
		t.Fatalf("exact numeric token probe: %v\n%s", err, out)
	}
	t.Log(strings.TrimSpace(string(out)))
}

const textNumbersProbe = `const legacy=bool.fromEnvironment('LEGACY_NUMBER');
const valueId=%[2]s;
void check(bool ok,String why){if(!ok)throw StateError(why);}
String wire(int value,int kind){final out=<int>[1];void u64(int n){for(var i=0;i<8;i++){out.add((n>>(8*i))&255);}}if(value!=0){out.addAll([1,kind]);u64(value);}out.add(0);if(value!=0)u64(valueId);u64(value==0?0:1);return out.map((x)=>x.toRadixString(16).padLeft(2,'0')).join();}
void main(){
 final cases=jsonDecode(r'''%[1]s''') as List;
 for(final row in cases){
  final token=row[0] as String;final report=TableTextReport();
  final number=legacy?jsonDecode(token):TableTextReport.number(token);
  final value=TableTextReport.integerField(number,report,null,null,row[1] as bool,0);
  check(wire(value,row[5] as int)==row[4],'wire oracle '+token);
  check(report.clamped==row[2]&&report.kindMismatch==row[3],'report '+token);
 }
 for(final bad in ['','01','-01','+1','.1','1.','1e','1-2','5+','1.2.3','--3','NaN','Infinity',' 1','1 ','1\n']){var failed=false;try{TableTextReport.number(bad);}on FormatException{failed=true;}check(failed,'strict grammar '+bad);}
 check(!TableTextReport.isNumber('7')&&TableTextReport.integerValue('7')==null&&TableTextReport.floatValue('7')==null,'quoted string remains string');
 check(TableTextReport.isNumber(BigInt.one)&&TableTextReport.isNumber((number:'1.25')),'numeric classification');
 check(TableTextReport.integerValue(12.0)==BigInt.from(12)&&TableTextReport.integerValue(12.5)==null,'native integral/fraction');
 check(TableTextReport.integerValue(double.infinity)==null&&TableTextReport.integerValue(double.nan)==null,'nonfinite integer mismatch');
 check(TableTextReport.floatValue(TableTextReport.number('1.25'))==1.25,'fraction float conversion');
 for(final token in ['-0','-0.0','-0e4','-0.000e-99999999999999999999']){final value=TableTextReport.number(token);final report=TableTextReport();check(TableTextReport.floatValue(value)!.isNegative,'negative zero sign '+token);check(TableTextReport.integerValue(value)==BigInt.zero&&TableTextReport.integerField(value,report,null,null,true,42)==0&&report.clamped==0&&report.kindMismatch==0,'negative zero integer '+token);}
 check(TableTextReport.floatValue(TableTextReport.number('1e999999999999999999'))!.isInfinite,'huge exponent float nonfinite');
 check(TableTextReport.floatValue(TableTextReport.number('1e-999999999999999999'))==0.0,'tiny float underflow without integer rounding');
 final mismatch=TableTextReport();check(TableTextReport.integerField(TableTextReport.number('1.00001'),mismatch,null,null,false,42)==42&&mismatch.kindMismatch==1,'mismatch preserves fallback');
 final bound=TableTextReport();check(TableTextReport.integerField(TableTextReport.number('18446744073709551615'),bound,0,-2,true,0)==-2&&bound.clamped==1,'unsigned bounds use bitpatterns');
 final both=TableTextReport();check(TableTextReport.integerField(TableTextReport.number('1e999999999'),both,0,100,true,0)==100&&both.clamped==1,'one clamp event across storage and field bounds');
 print('exact numeric tokens GREEN: '+cases.length.toString()+' production Go wire oracles; grammar/fraction/exponent/native/bounds checks');
}
`
