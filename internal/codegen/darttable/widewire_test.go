package darttable

import (
	"encoding/hex"
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

func TestDartWideWireDefaultsAndBounds(t *testing.T) {
	u := unitFrom(t, `package probe
table Root { scaled fixed(16,16) = 1.5 | min=-32768,max=32767
 bounded fixed(16,16) | min=-2,max=3
 wide fixed(64,64) = 1.5 | min=-9223372036854775808,max=9223372036854775807
 high ufixed(32,32) = 4294967295.0 | min=0,max=4294967295 }
`)
	fields := u.Tables["Root"].Fields
	if fields[0].DefInt.String() != "98304" || wideWireDefault(fields[0]) != "98304" {
		t.Fatal("fixed default must be IR's raw scaled integer")
	}
	if wideWireDefault(fields[2]) != "BigInt.parse('27670116110564327424')" || wideWireStorageType(fields[2]) != "BigInt" {
		t.Fatal("128-bit fixed default must preserve all scaled bits")
	}
	if wideWireDefault(fields[3]) != "-4294967296" || wideWireStorageType(fields[3]) != "int" {
		t.Fatal("unsigned64 raw default uses native signed bitpattern")
	}
	lo, hi, ok := wideWireRawBounds(fields[1])
	if !ok || lo != "BigInt.parse('-131072')" || hi != "BigInt.parse('196608')" {
		t.Fatalf("raw fixed bounds %s %s %v", lo, hi, ok)
	}
	if _, _, ok := wideWireRawBounds(unitFrom(t, "package probe\ntable Root {value uint128}\n").Tables["Root"].Fields[0]); ok {
		t.Fatal("unbounded field must not invent declared bounds")
	}
	if got := wideWireRawValue(fields[3], "v"); got != "BigInt.from(v).toUnsigned(64)" {
		t.Fatal(got)
	}
	if wideWireRawValue(fields[2], "v") != "v" || wideWireStorageValue(fields[2], "raw") != "raw" {
		t.Fatal("128-bit storage stays BigInt")
	}
}

func TestDartWideWireRawOracle(t *testing.T) {
	var checks strings.Builder
	count := 0
	for _, spelling := range []string{"int128", "uint128", "fixed(4,4)", "ufixed(4,4)", "fixed(8,8)", "ufixed(8,8)", "fixed(16,16)", "ufixed(16,16)", "fixed(32,32)", "ufixed(32,32)", "fixed(64,64)", "ufixed(64,64)"} {
		definition := spelling
		if spelling == "int128" {
			definition += " | min=-170141183460469231731687303715884105728,max=170141183460469231731687303715884105727"
		} else if strings.Contains(spelling, "fixed") {
			var ib, fb int
			fmt.Sscanf(strings.TrimPrefix(spelling, "u"), "fixed(%d,%d)", &ib, &fb)
			power := ib
			if !strings.HasPrefix(spelling, "u") {
				power--
			}
			if power > 63 {
				power = 63
			}
			low := big.NewInt(0)
			high := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(power)), big.NewInt(1))
			if !strings.HasPrefix(spelling, "u") {
				low.Neg(new(big.Int).Lsh(big.NewInt(1), uint(power)))
			}
			definition += fmt.Sprintf(" | min=%s,max=%s", low, high)
		}
		u := unitFrom(t, "package probe\ntable Root { value "+definition+" }\n")
		f := u.Tables["Root"].Fields[0]
		kind := ir.TableWireScalarKind(f)
		width := ir.TableKindWidth(kind)
		max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
		min := big.NewInt(0)
		if f.Type.Signed {
			max.Rsh(max, 1)
			min.Neg(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width-1)))
		}
		if low, high, ok := ir.TableRawRange(f); ok {
			min.Set(low)
			max.Set(high)
		}
		values := []*big.Int{big.NewInt(0), big.NewInt(1), new(big.Int).Set(max), new(big.Int).Set(min)}
		if width == 16 {
			mixed, _ := new(big.Int).SetString("11121314151617180102030405060708", 16)
			values = append(values, mixed)
		}
		for _, raw := range values {
			model := tabletext.NewModel(u)
			root := model.New(u.Tables["Root"])
			root.Fields[0].Cell.Wide = new(big.Int).Set(raw)
			file, err := tablewire.Encode(model, root)
			if err != nil {
				t.Fatal(err)
			}
			payload := tabletext.WideBytes(raw, kind)
			fmt.Fprintf(&checks, " { final raw=BigInt.parse('%s');final %s stored=%s;final back=%s;check(back==raw,'storage %s %s');\n", raw.String(), wideWireStorageType(f), wideWireStorageValue(f, "raw"), wideWireRawValue(f, "stored"), spelling, raw.String())
			fmt.Fprintf(&checks, " final w=Writer();w.putBigInt(back,%d);check(hex(w.bytes)==%q,'Go raw bytes %s');\n", width, hex.EncodeToString(payload), spelling)
			fmt.Fprintf(&checks, " final input=unhex(%q);final report=Report();final r=Reader([165,...input,90],report)..pos=1..limit=%d;check(r.readBigInt(%d,%v)==raw&&!report.malformed&&r.pos==%d&&r.data.first==165&&r.data.last==90,'Go raw read %s');\n", hex.EncodeToString(payload), width+1, width, f.Type.Signed, width+1, spelling)
			fmt.Fprintf(&checks, " final badReport=Report();final bad=Reader([165,...input,90],badReport)..pos=1..limit=%d;check(bad.readBigInt(%d,%v)==null&&badReport.malformed&&bad.pos==1,'truncated raw %s');\n", width, width, f.Type.Signed, spelling)
			fmt.Fprintf(&checks, " check(file(back,%d,%d)==%q,'Go whole wire %s');}\n", width, kind, hex.EncodeToString(file), spelling)
			count++
		}
	}
	t.Run("runtime-production-oracle", func(t *testing.T) {
		slowtest.Gate(t, "dart")
		dir := t.TempDir()
		source := "import 'dart:typed_data';\nclass Report{bool malformed=false;}\nclass Writer{final bytes=<int>[];void putByte(int b){bytes.add(b&255);}" + wideWireWriterMembers + "}\nclass Reader{final List<int> data;final Report report;int pos=0;late int limit;Reader(this.data,this.report){limit=data.length;}bool has(int n)=>n>=0&&pos+n<=limit;int readByte()=>data[pos++];" + wideWireReaderMembers + "}\n" + fmt.Sprintf(wideWireProbe, dartU64Literal(fieldId(unitFrom(t, "package probe\ntable Root {value uint128}\n").Tables["Root"].Fields[0])), checks.String(), count)
		if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("wide raw probe: %v\n%s", err, out)
		}
		t.Log(strings.TrimSpace(string(out)))
	})
}

const wideWireProbe = `const fieldId=%[1]s;
void check(bool ok,String why){if(!ok)throw StateError(why);}
String hex(List<int> bytes)=>bytes.map((b)=>b.toRadixString(16).padLeft(2,'0')).join();
List<int> unhex(String value)=>[for(var i=0;i<value.length;i+=2)int.parse(value.substring(i,i+2),radix:16)];
String file(BigInt raw,int width,int kind){final w=Writer()..putByte(1);if(raw!=BigInt.zero){w.putByte(1);w.putByte(kind);w.putBigInt(raw,width);}w.putByte(0);if(raw!=BigInt.zero)w.putBigInt(BigInt.from(fieldId).toUnsigned(64),8);w.putBigInt(BigInt.from(raw==BigInt.zero?0:1),8);return hex(w.bytes);}
void main(){
%[2]s
 print('wide raw GREEN: %[3]d Go payload/read/full-wire cases, widths in bytes 1/2/4/8/16; truncated reads preserve cursor');
}
`
