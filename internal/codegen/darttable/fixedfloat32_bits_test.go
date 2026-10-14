package darttable

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// The fixed-form scalar is the IEEE-754 pattern, with no canonicalisation
// (FIXED-FORM-ALGORITHM §3, §7 item 1). The input is forged from raw bits,
// independently of either generated conversion; the whole file must resave
// identically and the public scalar must retain the sign/payload too.
func TestFixedDartFloat32ScalarBitsSurviveLoadSave(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Dart SDK")
	u := unitFrom(t, "package probe\nfixed table Cell { value float32\n tail uint32 }\nfixed table _float32Cell { n uint8 }\n")
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "fixed_float32_probe.dart"), []byte(fixedFloat32BitsProbe), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "fixed_float32_probe.dart")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixed float32 bits: %v\n%s", err, out)
	}
}

const fixedFloat32BitsProbe = `import 'dart:typed_data';
import 'ProbeFixed.dart' as t;
void check(bool ok,String why){if(!ok)throw StateError(why);}
int raw(double value){
 final bits=(ByteData(8)..setFloat64(0,value,Endian.little)).getUint64(0,Endian.little);
 if((bits&0x7ff0000000000000)==0x7ff0000000000000&&(bits&0xfffffffffffff)!=0){
  return ((bits>>>63)<<31)|0x7f800000|((bits>>>29)&0x7fffff);
 }
 return (ByteData(4)..setFloat32(0,value,Endian.little)).getUint32(0,Endian.little);
}
void main(){
 final patterns=[0x7f800001,0xff800001,0x7f8abcde,0xffa5a5a5,0x7fc0dead,0xffc0dead,0x80000000,0,1,0x807fffff,0x00800000,0x3f800000,0x7f800000,0xff800000];
 for(final bits in patterns){
  final fresh=t.Cell()..value=1.0..tail=0xaabbccdd;
  final original=Uint8List(t.cellFixedMeasure(1));check(t.cellFixedSave([fresh],1,original)==original.length,'control save');
  final at=original.length-t.cellFixedBodyBytes;
  ByteData.sublistView(original).setUint32(at,bits,Endian.little);
  final values=[t.Cell()];final report=t.TableFixedReport();
  check(t.cellFixedLoad(values,1,original,original.length,t.cellFixedNewPlan(),report)==1,'input refused $bits');
  check(report.refused==0&&!report.malformed&&report.clamped==0,'input damaged $bits');
  check(raw(values[0].value)==bits,'public scalar changed ${bits.toRadixString(16)} to ${raw(values[0].value).toRadixString(16)}');
  check(values[0].tail==0xaabbccdd,'neighbor changed');
  final saved=Uint8List(original.length);check(t.cellFixedSave(values,1,saved)==saved.length,'resave refused');
  for(var i=0;i<saved.length;i++)check(saved[i]==original[i],'resave changed pattern=${bits.toRadixString(16)} byte=$i got=${saved[i]} expected=${original[i]}');
 }
}
`
