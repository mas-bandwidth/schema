package darttable

// TestCookWriteEveryConstructDart's TYPE-RECORD families
// (dart-cook-write-type-records-t). A `type` reached by a variable table rides
// by value inside its holder's node (docs/SPEC-TABLES.md §3, §7.2), and a
// fixed record body past §3.4's 65536-byte ceiling has no writer at all: the
// reference leg refuses it by the fixed form's named reason
// (`layout_record_too_large`), never a new word.
//
// The shared test driver and the cookEveryFamily registry live in
// cookwrite_every_test.go, which dart-cook-write-arrays-t lands; this card
// only appends to cookEveryFamilies here.
import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

func init() {
	cookEveryFamilies = append(cookEveryFamilies,
		cookEveryFamily{
			name: "type-record",
			schema: `type Pos { x int32
 y int32 }
table Root { pos Pos }`,
			probe: cookEveryTypeRecordProbe,
			mutate: func(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {
				v.Fields[0].Cell.Tab.Fields[0].Cell.U = 7
				v.Fields[0].Cell.Tab.Fields[1].Cell.U = 9
			},
		},
		cookEveryFamily{
			name: "record-body-over-64k-refused",
			schema: `fixed table RenderFrame { data [65537]uint8 }
table Root {}`,
			probe: cookEveryEmptyRootProbe,
			mutate: func(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {
				fu := unitFrom(t, "package probe\nenum Tier { flash, pro, heavy, frontier }\n"+
					"fixed table RenderFrame { data [65537]uint8 }\ntable Root {}")
				files, err := Generate(fu)
				if err != nil {
					t.Fatal(err)
				}
				src := string(files["ProbeCook.dart"])
				if strings.Contains(src, "static int cookMeasure(RenderFrame") {
					t.Fatal("the Dart cook writer emitted a writer for RenderFrame's over-64k record body")
				}
				if !strings.Contains(src, "layout_record_too_large") {
					t.Fatalf("the Dart cook writer did not name the reference leg's reason layout_record_too_large for RenderFrame:\n%s", src)
				}
			},
		},
	)
}

const cookEveryTypeRecordProbe = `import 'dart:io';
import 'dart:typed_data';
import 'ProbeWire.dart' as w;
import 'ProbeCook.dart' as c;
String hex(Uint8List b,int length)=>b.take(length).map((v)=>v.toRadixString(16).padLeft(2,'0')).join();
void main(){
 final value=w.Root()..pos=(w.Pos()..x=7..y=9);
 final size=c.RootCook.cookMeasure(value);
 for(final order in [Endian.little,Endian.big]){
  final bytes=Uint8List(size)..fillRange(0,size,0xa5);
  if(!c.RootCook.cook(value,bytes,order)) throw StateError('cook refused');
  stdout.writeln('${order==Endian.little?'cook-le':'cook-be'} ${hex(bytes,size)}');
 }
}
`

const cookEveryEmptyRootProbe = `import 'dart:io';
import 'dart:typed_data';
import 'ProbeWire.dart' as w;
import 'ProbeCook.dart' as c;
String hex(Uint8List b,int length)=>b.take(length).map((v)=>v.toRadixString(16).padLeft(2,'0')).join();
void main(){
 final value=w.Root();
 final size=c.RootCook.cookMeasure(value);
 for(final order in [Endian.little,Endian.big]){
  final bytes=Uint8List(size)..fillRange(0,size,0xa5);
  if(!c.RootCook.cook(value,bytes,order)) throw StateError('cook refused');
  stdout.writeln('${order==Endian.little?'cook-le':'cook-be'} ${hex(bytes,size)}');
 }
}
`
