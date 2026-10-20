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
		cookEveryFamily{
			name:   "guarded-record-reference",
			schema: cookEveryGuardedRecordReferenceSchema,
			probe:  cookEveryGuardedRecordReferenceProbe,
			refuse: "invalid_value",
			mutate: cookEveryGuardedRecordReferenceMutate,
		},
		cookEveryFamily{
			name:   "guarded-pointer-array",
			schema: cookEveryGuardedPointerArraySchema,
			probe:  cookEveryGuardedPointerArrayProbe,
			mutate: cookEveryGuardedPointerArrayMutate,
		},
		cookEveryFamily{
			name:   "guarded-pointer-array-reference",
			schema: cookEveryGuardedPointerArrayReferenceSchema,
			probe:  cookEveryGuardedPointerArrayReferenceProbe,
			refuse: "invalid_value",
			mutate: cookEveryGuardedPointerArrayReferenceMutate,
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

const cookEveryGuardedRecordSchema = `table RefTarget { v int32 }
table Detail { id int32
  tag string(8)
  x *RefTarget }
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
 value.live.x=w.RefTarget()..v=5;
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
// what rides, not what the cook writes (§7.2, §7.6). The guard-set record's
// reference is numbered and cooks; the guard-clear record's is null, so its
// unnumbered target is never named and the cook still succeeds.
func cookEveryGuardedRecordMutate(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {
	t.Helper()
	guardedField(t, v, "on").Cell.B = true
	guardedField(t, v, "off").Cell.B = false
	live := guardedField(t, v, "live").Cell.Tab
	guardedField(t, live, "id").Cell.U = 7
	guardedText(guardedField(t, live, "tag"), "hi")
	target := m.New(m.Unit.Tables["RefTarget"])
	guardedField(t, target, "v").Cell.U = 5
	guardedField(t, live, "x").Cell.Node = target
	dead := guardedField(t, v, "dead").Cell.Tab
	guardedField(t, dead, "id").Cell.U = 9
	guardedText(guardedField(t, dead, "tag"), "xx")
}

// cookEveryGuardedRecordReferenceSchema is a GUARDED BY-VALUE RECORD whose
// storage reaches a POINTER (§2.2, §3.1, §7.6). With the guard clear the
// record's pointer is not numbered, so the reference leg refuses the region it
// would otherwise write; this writer must refuse it in the same name.
const cookEveryGuardedRecordReferenceSchema = `table RefTarget { v int32 }
table Detail { id int32
  x *RefTarget }
table Root {
  on bool
  if on { live Detail } }
`

// cookEveryGuardedRecordReferenceProbe holds the guard CLEAR and the guarded
// record's pointer NON-NULL: the target is unnumbered (§3.1), so both legs
// must refuse with the reference's invalid_value (§7.6).
const cookEveryGuardedRecordReferenceProbe = `import 'dart:io';
import 'dart:typed_data';
import 'ProbeWire.dart' as w;
import 'ProbeCook.dart' as c;
void main(){
 final value=w.Root()..on=false;
 value.live.x=w.RefTarget()..v=5;
 final measure=c.TableCookWriteReport();
 if(c.RootCook.cookMeasure(value,report:measure)>=0) throw StateError('cookMeasure accepted an unnumbered reference');
 if(measure.reason!='invalid_value') throw StateError('cookMeasure reason ${measure.reason}');
 for(final order in [Endian.little,Endian.big]){
  final report=c.TableCookWriteReport();
  if(c.RootCook.cook(value,Uint8List(64),order,report:report)) throw StateError('cook accepted an unnumbered reference');
  stdout.writeln('${order==Endian.little?'cook-le':'cook-be'} ${report.reason}');
 }
}
`

// cookEveryGuardedRecordReferenceMutate leaves the guard clear and puts a
// non-null target in the guarded record's pointer slot.
func cookEveryGuardedRecordReferenceMutate(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {
	t.Helper()
	guardedField(t, v, "on").Cell.B = false
	live := guardedField(t, v, "live").Cell.Tab
	guardedField(t, live, "id").Cell.U = 7
	target := m.New(m.Unit.Tables["RefTarget"])
	guardedField(t, target, "v").Cell.U = 5
	guardedField(t, live, "x").Cell.Node = target
}

// cookEveryGuardedPointerArraySchema is a GUARDED FIXED POINTER ARRAY (§2.1,
// §3.1, §7.6): the guard decides whether its slots' targets are numbered, while
// the cook writes every slot of the storage.
const cookEveryGuardedPointerArraySchema = `table RefTarget { v int32 }
table Root {
  on bool
  off bool
  if on { live [2]*RefTarget }
  if off { dead [2]*RefTarget } }
`

const cookEveryGuardedPointerArrayProbe = `import 'dart:io';
import 'dart:typed_data';
import 'ProbeWire.dart' as w;
import 'ProbeCook.dart' as c;
String hex(Uint8List b,int length)=>b.take(length).map((v)=>v.toRadixString(16).padLeft(2,'0')).join();
void main(){
 final value=w.Root()..on=true..off=false;
 value.live=[w.RefTarget()..v=5,null];
 final size=c.RootCook.cookMeasure(value);
 for(final order in [Endian.little,Endian.big]){
  final bytes=Uint8List(size)..fillRange(0,size,0xa5);
  if(!c.RootCook.cook(value,bytes,order)) throw StateError('cook refused');
  stdout.writeln('${order==Endian.little?'cook-le':'cook-be'} ${hex(bytes,size)}');
 }
}
`

// cookEveryGuardedPointerArrayMutate holds the guard-set array's first slot and
// leaves the guard-clear array's slots null, so both cook.
func cookEveryGuardedPointerArrayMutate(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {
	t.Helper()
	guardedField(t, v, "on").Cell.B = true
	guardedField(t, v, "off").Cell.B = false
	target := m.New(m.Unit.Tables["RefTarget"])
	guardedField(t, target, "v").Cell.U = 5
	guardedField(t, v, "live").Elems[0].Node = target
}

// cookEveryGuardedPointerArrayReferenceSchema holds a guarded pointer array
// whose guard is CLEAR and whose first slot is NON-NULL: the target is
// unnumbered (§3.1), so the reference leg refuses the region (§7.6).
const cookEveryGuardedPointerArrayReferenceSchema = `table RefTarget { v int32 }
table Root {
  on bool
  if on { arr [2]*RefTarget } }
`

const cookEveryGuardedPointerArrayReferenceProbe = `import 'dart:io';
import 'dart:typed_data';
import 'ProbeWire.dart' as w;
import 'ProbeCook.dart' as c;
void main(){
 final value=w.Root()..on=false;
 value.arr=[w.RefTarget()..v=5,null];
 final measure=c.TableCookWriteReport();
 if(c.RootCook.cookMeasure(value,report:measure)>=0) throw StateError('cookMeasure accepted an unnumbered reference');
 if(measure.reason!='invalid_value') throw StateError('cookMeasure reason ${measure.reason}');
 for(final order in [Endian.little,Endian.big]){
  final report=c.TableCookWriteReport();
  if(c.RootCook.cook(value,Uint8List(64),order,report:report)) throw StateError('cook accepted an unnumbered reference');
  stdout.writeln('${order==Endian.little?'cook-le':'cook-be'} ${report.reason}');
 }
}
`

func cookEveryGuardedPointerArrayReferenceMutate(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {
	t.Helper()
	guardedField(t, v, "on").Cell.B = false
	target := m.New(m.Unit.Tables["RefTarget"])
	guardedField(t, target, "v").Cell.U = 5
	guardedField(t, v, "arr").Elems[0].Node = target
}
