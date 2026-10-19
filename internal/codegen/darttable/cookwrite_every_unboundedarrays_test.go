package darttable

// Unbounded arrays in the variable cook writer (docs/SPEC-TABLES.md §2.9,
// §7.2, §7.6): a scalar list and a by-value record list ride in the holder's
// node extent, pre-order, measured before the write. The families below join
// cookEveryFamilies through init() so TestCookWriteEveryConstructDart pins
// them byte for byte against `schema cook` (internal/tablecook) in both byte
// orders, together with the families its siblings register.

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

func init() {
	cookEveryFamilies = append(cookEveryFamilies,
		cookEveryFamily{
			name:   "unbounded-array-of-scalars",
			schema: `table Root { vals []int32 }`,
			probe:  cookEveryUnboundedScalarsProbe,
			mutate: func(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {
				vals := &v.Fields[0]
				vals.Count = 3
				vals.Elems = make([]tabletext.Cell, 3)
				vals.Elems[0].U = 1
				vals.Elems[1].U = 2
				vals.Elems[2].U = 3
			},
		},
		cookEveryFamily{
			name: "unbounded-array-of-records",
			schema: `table Inner { v int32 }
table Root { inner []Inner }`,
			probe: cookEveryUnboundedRecordsProbe,
			mutate: func(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {
				inner := &v.Fields[0]
				inner.Count = 2
				inner.Elems = make([]tabletext.Cell, 2)
				inner.Elems[0].Tab = m.New(m.Unit.Tables["Inner"])
				inner.Elems[1].Tab = m.New(m.Unit.Tables["Inner"])
				inner.Elems[0].Tab.Fields[0].Cell.U = 10
				inner.Elems[1].Tab.Fields[0].Cell.U = uint64(0xFFFFFFFFFFFFFFFA)
			},
		},
	)
}

const cookEveryUnboundedScalarsProbe = `import 'dart:io';
import 'dart:typed_data';
import 'ProbeWire.dart' as w;
import 'ProbeCook.dart' as c;
String hex(Uint8List b,int length)=>b.take(length).map((v)=>v.toRadixString(16).padLeft(2,'0')).join();
void main(){
 final value=w.Root()..vals=[1,2,3];
 final size=c.RootCook.cookMeasure(value);
 for(final order in [Endian.little,Endian.big]){
  final bytes=Uint8List(size+4)..fillRange(0,size+4,0xa5);
  if(!c.RootCook.cook(value,bytes,order)) throw StateError('cook refused');
  for(var i=size;i<bytes.length;i++){
   if(bytes[i]!=0xa5) throw StateError('wrote past measure');
  }
  stdout.writeln('${order==Endian.little?'cook-le':'cook-be'} ${hex(bytes,size)}');
 }
}
`

const cookEveryUnboundedRecordsProbe = `import 'dart:io';
import 'dart:typed_data';
import 'ProbeWire.dart' as w;
import 'ProbeCook.dart' as c;
String hex(Uint8List b,int length)=>b.take(length).map((v)=>v.toRadixString(16).padLeft(2,'0')).join();
void main(){
 final value=w.Root()..inner=[w.Inner()..v=10,w.Inner()..v=-6];
 final size=c.RootCook.cookMeasure(value);
 for(final order in [Endian.little,Endian.big]){
  final bytes=Uint8List(size+4)..fillRange(0,size+4,0xa5);
  if(!c.RootCook.cook(value,bytes,order)) throw StateError('cook refused');
  for(var i=size;i<bytes.length;i++){
   if(bytes[i]!=0xa5) throw StateError('wrote past measure');
  }
  stdout.writeln('${order==Endian.little?'cook-le':'cook-be'} ${hex(bytes,size)}');
 }
}
`
