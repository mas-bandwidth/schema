package darttable

// THE WIDE SCALAR FAMILIES (docs/SPEC-TABLES.md §3, §7.2, §7.6, §15): kinds
// 18-29, the scalars the type wire brought — int128 and uint128, and the
// fixed/ufixed family at each storage width and signedness. Each rides as its
// RAW integer at the kind's width, so each is a family of its own here and
// each is cooked by this leg's variable writer and compared byte for byte with
// `schema cook`'s production writer (internal/tablecook) in both byte orders.
//
// WideBlob is the family that stays a REFUSAL: its record body is past
// §3.4's 65536-byte ceiling, so no conforming reader decodes it and this leg
// names that reason instead of emitting a writer. The family's probe checks
// the name and still cooks a wide scalar root, so the subtest is exercised on
// both sides of the rule.

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

func init() {
	cookEveryFamilies = append(cookEveryFamilies,
		cookEveryWideFamily("wide-i128", "int128 | min = -1267650600228229401496703205376, max = 1267650600228229401496703205376", "-123456789012345678901234567890"),
		cookEveryWideFamily("wide-u128", "uint128", "340282366920938463463374607431768211455"),
		cookEveryWideFamily("wide-fixed8", "fixed(4, 4) | min = -8, max = 7", "-5"),
		cookEveryWideFamily("wide-fixed16", "fixed(8, 8) | min = -128, max = 127", "4660"),
		cookEveryWideFamily("wide-fixed32", "fixed(16, 16) | min = -32768, max = 32767", "-123456789"),
		cookEveryWideFamily("wide-fixed64", "fixed(48, 16) | min = -140737488355328, max = 140737488355327", "-2000000000000000000"),
		cookEveryWideFamily("wide-fixed128", "fixed(112, 16) | min = -9223372036854775808, max = 9223372036854775807", "-123456789012345678901234567890"),
		cookEveryWideFamily("wide-ufixed8", "ufixed(4, 4) | min = 0, max = 15", "171"),
		cookEveryWideFamily("wide-ufixed16", "ufixed(8, 8) | min = 0, max = 255", "48879"),
		cookEveryWideFamily("wide-ufixed32", "ufixed(16, 16) | min = 0, max = 65535", "3735928559"),
		cookEveryWideFamily("wide-ufixed64", "ufixed(48, 16) | min = 0, max = 281474976710655", "211004897354834829"),
		cookEveryWideFamily("wide-ufixed128", "ufixed(112, 16) | min = 0, max = 9223372036854775807", "170141183460469231731687303715884117897"),
		cookEveryWideBlobFamily(),
	)
}

// cookEveryWideFamily is one kind's family: a Root holding a single wide
// scalar, the RAW decimal the probe and the model both place, and the cook
// comparison the shared driver runs.
func cookEveryWideFamily(name, decl, raw string) cookEveryFamily {
	value, ok := new(big.Int).SetString(raw, 10)
	if !ok {
		panic("cookwrite_every_widekinds_test: bad wide literal " + raw)
	}
	return cookEveryFamily{
		name:   name,
		schema: "table Root { v " + decl + " }",
		probe:  cookEveryWideProbe(raw),
		mutate: func(t *testing.T, _ *tabletext.Model, v *tabletext.Instance) {
			t.Helper()
			v.Fields[0].Cell.Wide = new(big.Int).Set(value)
		},
	}
}

// cookEveryWideBlobFamily pins the one wide-family refusal §3.4 names: a
// record body past 65536 bytes gets no Dart cook writer, and the generated
// library says the reason. Its probe also cooks a wide scalar Root so the
// shared driver's byte comparison still runs.
func cookEveryWideBlobFamily() cookEveryFamily {
	const blob = "fixed table WideBlob { payload bytes(66000) }"
	probe := `import 'dart:io';
import 'dart:typed_data';
import 'ProbeWire.dart' as w;
import 'ProbeCook.dart' as c;
String hex(Uint8List b,int length)=>b.take(length).map((v)=>v.toRadixString(16).padLeft(2,'0')).join();
void main(){
 final source=File('ProbeCook.dart').readAsStringSync();
 if(!source.contains('WideBlob has no fixed-root cook writer here')) throw StateError('WideBlob refusal is not named');
 if(!source.contains("record body is past §3.4's 65536-byte ceiling")) throw StateError('WideBlob refusal does not name the 64 KB ceiling');
 final value=w.Root()..v=BigInt.from(7);
 final size=c.RootCook.cookMeasure(value);
 for(final order in [Endian.little,Endian.big]){
  final bytes=Uint8List(size)..fillRange(0,size,0xa5);
  if(!c.RootCook.cook(value,bytes,order)) throw StateError('cook refused');
  stdout.writeln('${order==Endian.little?'cook-le':'cook-be'} ${hex(bytes,size)}');
 }
}
`
	return cookEveryFamily{
		name:   "wideblob-refused",
		schema: "table Root { v int128 | min = -1267650600228229401496703205376, max = 1267650600228229401496703205376 }\n" + blob,
		probe:  probe,
		mutate: func(t *testing.T, _ *tabletext.Model, v *tabletext.Instance) {
			t.Helper()
			v.Fields[0].Cell.Wide = big.NewInt(7)
		},
	}
}

// cookEveryWideProbe cooks a Root whose one wide field holds raw and prints
// both byte orders. cookMeasure is asked first and the buffer is exactly that
// size, so a writer that passes its measured extent throws instead of running
// into the slack.
func cookEveryWideProbe(raw string) string {
	return fmt.Sprintf(`import 'dart:io';
import 'dart:typed_data';
import 'ProbeWire.dart' as w;
import 'ProbeCook.dart' as c;
String hex(Uint8List b,int length)=>b.take(length).map((v)=>v.toRadixString(16).padLeft(2,'0')).join();
void main(){
 final value=w.Root()..v=BigInt.parse('%s');
 final size=c.RootCook.cookMeasure(value);
 for(final order in [Endian.little,Endian.big]){
  final bytes=Uint8List(size)..fillRange(0,size,0xa5);
  if(!c.RootCook.cook(value,bytes,order)) throw StateError('cook refused');
  stdout.writeln('${order==Endian.little?'cook-le':'cook-be'} ${hex(bytes,size)}');
 }
}
`, raw)
}
