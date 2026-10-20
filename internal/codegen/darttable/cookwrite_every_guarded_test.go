package darttable

// THE COOK-WRITE CONSTRUCT FAMILIES this card adds (docs/SPEC-TABLES.md §7.2,
// §7.6): GUARDED REFERENCES and GUARDED RECORDS, a field under a guard (the
// guard's presence bit and the guarded slot, §2). A guard is a fact of the
// WIRE (§2.2, §16.2): it decides what rides, while the region the cook writes
// is the storage, every slot of it, whatever the guard holds (§7.2). So a
// guarded reference whose target a false guard leaves unnumbered (§3.1) cooks
// as null, and a guarded record cooks as its own storage even when its guard is
// clear. Each family's value holds one guarded field with its guard SET and one
// with it CLEAR; both byte orders are compared with tablecook.Cook.
//
// The driver and cookEveryFamilies registry live in cookwrite_every_test.go;
// this card only appends its families here, never another card's.
import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

func init() {
	cookEveryFamilies = append(cookEveryFamilies,
		cookEveryFamily{
			name:   "guarded-reference",
			schema: cookEveryGuardedReferenceSchema,
			probe:  cookEveryGuardedReferenceProbe,
			mutate: cookEveryGuardedReferenceMutate,
		},
		cookEveryFamily{
			name:   "guarded-record",
			schema: cookEveryGuardedRecordSchema,
			probe:  cookEveryGuardedRecordProbe,
			mutate: cookEveryGuardedRecordMutate,
		},
	)
}

const cookEveryGuardedReferenceSchema = `table RefTarget { v int32 }
table Root {
  on bool
  off bool
  if on { live *RefTarget }
  if off { dead *RefTarget } }
`

const cookEveryGuardedReferenceProbe = `import 'dart:io';
import 'dart:typed_data';
import 'ProbeWire.dart' as w;
import 'ProbeCook.dart' as c;
String hex(Uint8List b,int length)=>b.take(length).map((v)=>v.toRadixString(16).padLeft(2,'0')).join();
void main(){
 final target=w.RefTarget()..v=5;
 final value=w.Root()..on=true..off=false..live=target;
 final size=c.RootCook.cookMeasure(value);
 for(final order in [Endian.little,Endian.big]){
  final bytes=Uint8List(size)..fillRange(0,size,0xa5);
  if(!c.RootCook.cook(value,bytes,order)) throw StateError('cook refused');
  stdout.writeln('${order==Endian.little?'cook-le':'cook-be'} ${hex(bytes,size)}');
 }
}
`

const cookEveryGuardedRecordSchema = `table Detail { id int32
  tag string(8) }
table Root {
  on bool
  off bool
  if on { live Detail }
  if off { dead Detail } }
`

const cookEveryGuardedRecordProbe = `import 'dart:io';
import 'dart:typed_data';
import 'ProbeWire.dart' as w;
import 'ProbeCook.dart' as c;
String hex(Uint8List b,int length)=>b.take(length).map((v)=>v.toRadixString(16).padLeft(2,'0')).join();
void main(){
 final value=w.Root()..on=true..off=false;
 value.live.id=7;value.live.tag='hi';
 value.dead.id=9;value.dead.tag='xx';
 final size=c.RootCook.cookMeasure(value);
 for(final order in [Endian.little,Endian.big]){
  final bytes=Uint8List(size)..fillRange(0,size,0xa5);
  if(!c.RootCook.cook(value,bytes,order)) throw StateError('cook refused');
  stdout.writeln('${order==Endian.little?'cook-le':'cook-be'} ${hex(bytes,size)}');
 }
}
`

// guardedField returns the field NAME of v, or fails the subtest.
func guardedField(t *testing.T, v *tabletext.Instance, name string) *tabletext.Field {
	t.Helper()
	for i := range v.Fields {
		if v.Fields[i].Def.Name == name {
			return &v.Fields[i]
		}
	}
	t.Fatalf("%s has no field %s", v.Def.Name, name)
	return nil
}

// guardedText sets a string(N) slot's inline bytes and count.
func guardedText(f *tabletext.Field, s string) {
	f.Cell.Str = []byte(s)
	f.Count = len(s)
}

// cookEveryGuardedReferenceMutate holds the guard-set reference with a target
// and the guard-clear reference without one (its target is unnumbered, §3.1).
func cookEveryGuardedReferenceMutate(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {
	t.Helper()
	guardedField(t, v, "on").Cell.B = true
	guardedField(t, v, "off").Cell.B = false
	target := m.New(m.Unit.Tables["RefTarget"])
	guardedField(t, target, "v").Cell.U = 5
	guardedField(t, v, "live").Cell.Node = target
}

// cookEveryGuardedRecordMutate fills BOTH records' storage: the guard decides
// what rides, not what the cook writes (§7.2, §7.6).
func cookEveryGuardedRecordMutate(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {
	t.Helper()
	guardedField(t, v, "on").Cell.B = true
	guardedField(t, v, "off").Cell.B = false
	live := guardedField(t, v, "live").Cell.Tab
	guardedField(t, live, "id").Cell.U = 7
	guardedText(guardedField(t, live, "tag"), "hi")
	dead := guardedField(t, v, "dead").Cell.Tab
	guardedField(t, dead, "id").Cell.U = 9
	guardedText(guardedField(t, dead, "tag"), "xx")
}

// The two REFUSAL families below pin the other half of a guard's rule: a
// clear guard leaves every target it covers unnumbered (§3.1), so a guarded
// by-value record that still holds a reference and a guarded array whose live
// slot still holds one must refuse exactly as the reference leg does (§7.6).
func init() {
	cookEveryFamilies = append(cookEveryFamilies,
		cookEveryFamily{
			name:   "guarded-record-reference-refused",
			schema: cookEveryGuardedRecordRefSchema,
			probe:  cookEveryGuardedRecordRefProbe,
			refuse: "invalid_value",
			mutate: cookEveryGuardedRecordRefMutate,
		},
		cookEveryFamily{
			name:   "guarded-pointer-array-fixed-refused",
			schema: cookEveryGuardedPointerArraySchema,
			probe:  cookEveryGuardedPointerArrayProbe,
			refuse: "invalid_value",
			mutate: cookEveryGuardedPointerArrayFixedMutate,
		},
		cookEveryFamily{
			name:   "guarded-pointer-array-counted-refused",
			schema: cookEveryGuardedPointerArrayCountedSchema,
			probe:  cookEveryGuardedPointerArrayCountedProbe,
			refuse: "invalid_value",
			mutate: cookEveryGuardedPointerArrayCountedMutate,
		},
	)
}

const cookEveryGuardedRecordRefSchema = `table RefNode { v int32 }
table RefDetail { x *RefNode }
table Root {
  on bool
  if on { rec RefDetail } }
`

// cookEveryGuardedRecordRefProbe holds the guard clear and the by-value
// record's reference non-null: the numbering never reaches it (§3.1), so the
// cook refuses as the reference leg does (§7.6).
const cookEveryGuardedRecordRefProbe = `import 'dart:io';
import 'dart:typed_data';
import 'ProbeWire.dart' as w;
import 'ProbeCook.dart' as c;
void main(){
 final value=w.Root()..on=false;
 value.rec.x=w.RefNode()..v=1;
 final bytes=Uint8List(4096);
 for(final order in [Endian.little,Endian.big]){
  final report=c.TableCookWriteReport();
  final ok=c.RootCook.cook(value,bytes,order,report:report);
  stdout.writeln('${order==Endian.little?'cook-le':'cook-be'} ${ok?'ok':report.reason}');
 }
}
`

func cookEveryGuardedRecordRefMutate(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {
	t.Helper()
	guardedField(t, v, "on").Cell.B = false
	node := m.New(m.Unit.Tables["RefNode"])
	guardedField(t, node, "v").Cell.U = 1
	guardedField(t, guardedField(t, v, "rec").Cell.Tab, "x").Cell.Node = node
}

const cookEveryGuardedPointerArraySchema = `table RefNode { v int32 }
table Root {
  on bool
  if on { arr [4]*RefNode } }
`

const cookEveryGuardedPointerArrayProbe = `import 'dart:io';
import 'dart:typed_data';
import 'ProbeWire.dart' as w;
import 'ProbeCook.dart' as c;
void main(){
 final value=w.Root()..on=false;
 value.arr[0]=w.RefNode()..v=1;
 final bytes=Uint8List(4096);
 for(final order in [Endian.little,Endian.big]){
  final report=c.TableCookWriteReport();
  final ok=c.RootCook.cook(value,bytes,order,report:report);
  stdout.writeln('${order==Endian.little?'cook-le':'cook-be'} ${ok?'ok':report.reason}');
 }
}
`

const cookEveryGuardedPointerArrayCountedSchema = `table RefNode { v int32 }
table Root {
  on bool
  if on { arr [..4]*RefNode } }
`

const cookEveryGuardedPointerArrayCountedProbe = `import 'dart:io';
import 'dart:typed_data';
import 'ProbeWire.dart' as w;
import 'ProbeCook.dart' as c;
void main(){
 final value=w.Root()..on=false;
 value.arr=[w.RefNode()..v=1];
 final bytes=Uint8List(4096);
 for(final order in [Endian.little,Endian.big]){
  final report=c.TableCookWriteReport();
  final ok=c.RootCook.cook(value,bytes,order,report:report);
  stdout.writeln('${order==Endian.little?'cook-le':'cook-be'} ${ok?'ok':report.reason}');
 }
}
`

func cookEveryGuardedPointerArrayFixedMutate(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {
	t.Helper()
	guardedField(t, v, "on").Cell.B = false
	node := m.New(m.Unit.Tables["RefNode"])
	guardedField(t, node, "v").Cell.U = 1
	guardedField(t, v, "arr").Elems[0].Node = node
}

func cookEveryGuardedPointerArrayCountedMutate(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {
	t.Helper()
	guardedField(t, v, "on").Cell.B = false
	node := m.New(m.Unit.Tables["RefNode"])
	guardedField(t, node, "v").Cell.U = 1
	arr := guardedField(t, v, "arr")
	arr.Count = 1
	arr.Elems[0].Node = node
}
