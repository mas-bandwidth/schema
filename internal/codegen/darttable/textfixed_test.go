package darttable

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestDartTextFixedExactQ(t *testing.T) {
	slowtest.Gate(t, "dart")
	var cases []any
	// Every expected raw value and spelling comes from independent exact Go
	// fixed-point tooling, never a native floating conversion or Dart roundtrip.
	for _, shape := range []struct {
		bits, frac int
		signed     bool
	}{{8, 0, true}, {8, 4, true}, {8, 4, false}, {16, 8, true}, {32, 16, true}, {64, 32, false}, {128, 64, true}, {128, 127, false}} {
		ib := shape.bits - shape.frac
		word := "ufixed"
		if shape.signed {
			word = "fixed"
		}
		exponent := ib
		if shape.signed {
			exponent--
		}
		if exponent > 63 {
			exponent = 63
		}
		lo := big.NewInt(0)
		hi := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(exponent)), big.NewInt(1))
		if shape.signed {
			lo.Neg(new(big.Int).Lsh(big.NewInt(1), uint(exponent)))
		}
		source := fmt.Sprintf("package probe\ntable Root {value %s(%d,%d) | min=%s,max=%s}\n", word, ib, shape.frac, lo, hi)
		u := unitFrom(t, source)
		f := u.Tables["Root"].Fields[0]
		kind := ir.TableWireScalarKind(f)
		rawLo, rawHi, _ := ir.TableRawRange(f)
		raws := []*big.Int{big.NewInt(0), big.NewInt(1), new(big.Int).Set(rawHi), new(big.Int).Set(rawLo)}
		if shape.frac > 0 && shape.bits > 8 {
			raws = append(raws, new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), uint(shape.frac)), big.NewInt(1)))
		}
		for _, raw := range raws {
			if raw.Cmp(rawLo) < 0 || raw.Cmp(rawHi) > 0 {
				continue
			}
			text := tabletext.FormatWide(raw, kind, shape.frac)
			parsed, exact, saturated := tabletext.ParseWide(text, kind, shape.frac)
			if !exact || saturated || parsed.Cmp(raw) != 0 {
				t.Fatalf("Go exact text oracle %s F%d", text, shape.frac)
			}
			model := tabletext.NewModel(u)
			root := model.New(u.Tables["Root"])
			root.Fields[0].Cell.Wide = new(big.Int).Set(raw)
			wire, err := tablewire.Encode(model, root)
			if err != nil {
				t.Fatal(err)
			}
			cases = append(cases, []any{text, shape.frac, raw.String(), rawLo.String(), rawHi.String(), hex.EncodeToString(wire), kind, ir.TableKindWidth(kind)})
		}
	}
	encoded, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	source := "import 'dart:convert';\nimport 'dart:typed_data';\nclass TableTextReport{int clamped=0,kindMismatch=0;" + textNumberMembers + textFixedMembers + "}\n" + fmt.Sprintf(textFixedProbe, string(encoded), dartU64Literal(fieldId(unitFrom(t, "package probe\ntable Root {value uint128}\n").Tables["Root"].Fields[0])))
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("exact Q probe: %v\n%s", err, out)
	}
	t.Log(strings.TrimSpace(string(out)))
}

const textFixedProbe = `const fieldId=%[2]s;
void check(bool ok,String why){if(!ok)throw StateError(why);}
String wire(BigInt raw,int width,int kind){final bytes=<int>[1];void put(BigInt value,int size){var v=value.toUnsigned(size*8);for(var i=0;i<size;i++){bytes.add((v&BigInt.from(255)).toInt());v>>=8;}}if(raw!=BigInt.zero){bytes.addAll([1,kind]);put(raw,width);}bytes.add(0);if(raw!=BigInt.zero)put(BigInt.from(fieldId),8);put(BigInt.from(raw==BigInt.zero?0:1),8);return bytes.map((x)=>x.toRadixString(16).padLeft(2,'0')).join();}
void main(){
 final cases=jsonDecode(r'''%[1]s''') as List;
 for(final row in cases){
  final text=row[0] as String,f=row[1] as int,raw=BigInt.parse(row[2] as String),lo=BigInt.parse(row[3] as String),hi=BigInt.parse(row[4] as String);
  final token=TableTextReport.number(text),report=TableTextReport();
  final got=TableTextReport.fixedField(token,report,f,lo,hi,BigInt.from(42));
  check(got==raw&&report.clamped==0&&report.kindMismatch==0,'Go exact Q read '+text);
  check(TableTextReport.fixedText(raw,f)==text,'Go exact Q text '+text);
  check(wire(got,row[7] as int,row[6] as int)==row[5],'Go scaled raw wire '+text);
 }
 for(final token in ['1.5','15e-1','150e-2'])check(TableTextReport.fixedValue(TableTextReport.number(token),16)==BigInt.from(98304),'integral exponent normalization '+token);
 for(final token in ['0.1','1.00000000000000000000000000000000000000000000000000000000000000001','1e-999999999999999999999999999']){final report=TableTextReport();check(TableTextReport.fixedField(TableTextReport.number(token),report,127,-(BigInt.one<<127),(BigInt.one<<127)-BigInt.one,BigInt.from(42))==BigInt.from(42)&&report.kindMismatch==1&&report.clamped==0,'fine/non-dyadic fallback '+token);}
 final hugeOdd='9'*1000+'.1';check(TableTextReport.fixedValue(TableTextReport.number(hugeOdd),127)==null,'huge odd denominator must mismatch before clamp');
 final hugeTiny='9'*1000+'.'+'0'*127+'1';check(TableTextReport.fixedValue(TableTextReport.number(hugeTiny),127)==null,'huge coefficient tiny tail mismatches before clamp');
 final hugeDyadic='9'*1000+'.5';final high=(BigInt.one<<127)-BigInt.one;final overflow=TableTextReport();check(TableTextReport.fixedField(TableTextReport.number(hugeDyadic),overflow,127,BigInt.zero,high,BigInt.zero)==high&&overflow.clamped==1&&overflow.kindMismatch==0,'huge exact dyadic clamps once');
 final exponent=TableTextReport();check(TableTextReport.fixedField(TableTextReport.number('1e99999999999999999999999'),exponent,127,BigInt.zero,high,BigInt.zero)==high&&exponent.clamped==1,'huge integral exponent clamps');
 check(TableTextReport.fixedValue(0.1,55)==BigInt.parse('3602879701896397'),'native double uses exact binary rational');
 check(TableTextReport.fixedValue(0.1,4)==null,'native double finer than Q');
 check(TableTextReport.fixedValue(double.minPositive,127)==null&&TableTextReport.fixedValue(double.infinity,127)==null&&TableTextReport.fixedValue(double.nan,127)==null,'native subnormal/nonfinite');
 check(TableTextReport.fixedValue(-1.5,16)==BigInt.from(-98304)&&TableTextReport.fixedValue(-0.0,127)==BigInt.zero,'native signed values');
 check(TableTextReport.fixedValue(BigInt.from(3),64)==BigInt.from(3)<<64,'BigInt whole units');
 check(TableTextReport.fixedValue('1.5',16)==null,'quoted string remains string');
 check(TableTextReport.fixedText(BigInt.from(3),0)=='3.0'&&TableTextReport.fixedText(BigInt.from(-1),2)=='-0.25','shortest whole-unit text');
 final negative=TableTextReport();check(TableTextReport.fixedField(TableTextReport.number('-1.5'),negative,16,BigInt.zero,high,BigInt.zero)==BigInt.zero&&negative.clamped==1,'negative unsigned clamp');
 print('fixed JSON Q GREEN: '+cases.length.toString()+' Go exact text/raw-wire cases; native binary-double, exponent, fraction, huge-coefficient checks');
}
`
