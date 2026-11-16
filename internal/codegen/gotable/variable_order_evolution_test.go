package gotable

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestGoVariableFieldReordering(t *testing.T) {
	t.Parallel()
	oldSchema := `package probe
 table Root { a int32
 b int32 }
`
	newSchema := `package probe
 table Root { b int32
 a int32 }
`
	old := kindsUnit{t: t, u: unitFrom(t, oldSchema)}
	old.m = tabletext.NewModel(old.u)
	wire := old.encode("Root", `{"a":7,"b":9}`)
	a, b := ir.TableFieldWireId(old.field("Root", "a")), ir.TableFieldWireId(old.field("Root", "b"))
	field := func(w *formOne, ref uint64, v uint32) {
		w.body = append(leb(w.body, ref), byte(ir.TableKindI32))
		w.body = binary.LittleEndian.AppendUint32(w.body, v)
	}
	duplicate := &formOne{ids: []uint64{a, b}}
	field(duplicate, 1, 7)
	field(duplicate, 2, 9)
	field(duplicate, 1, 7)
	firstUse := &formOne{ids: []uint64{a, b}}
	field(firstUse, 2, 9)
	field(firstUse, 1, 7)
	unused := &formOne{ids: []uint64{a, b, 0x123456789abcdef0}}
	field(unused, 1, 7)
	field(unused, 2, 9)
	duplicateID := &formOne{ids: []uint64{a, a}}
	field(duplicateID, 1, 7)
	field(duplicateID, 2, 9)
	sourceTemplate := `package probe
import ("testing";"unsafe")
func region(n int64) []byte { raw:=make([]byte,n+64);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;return raw[off:off+uintptr(n)] }
func TestReordering(t *testing.T) {
 wire:=[]byte(%q)
 n:=RootLoadMeasure(wire); if n<0 { t.Fatalf("measure refused writer's file: %%d",n) }
 var report TableReport; v:=RootLoad(region(n),wire,&report)
 if v==nil || report.Refused() || v.A!=7 || v.B!=9 { t.Fatalf("region: value=%%+v report=%%+v",v,report) }
 var b RootBuilder; if !b.Init(){t.Fatal("init")}; defer b.Shutdown()
 if !RootLoadBuilder(&b,wire,&report) || report.Refused() || b.GetRoot().A!=7 || b.GetRoot().B!=9 {t.Fatalf("builder report=%%+v",report)}
 strict,err:=RootReadStrict(wire);if err!=nil || strict.A!=7 || strict.B!=9 {t.Fatalf("strict: %%+v %%v",strict,err)}
 retain:=TableRetain{Bytes:make([]byte,65536),Ids:make([]TableRetainID,64)}
 v=RootLoadRetain(region(n),wire,&retain,&report);if v==nil||report.Refused()||v.A!=7||v.B!=9 {t.Fatalf("retain: %%+v %%+v",v,report)}
 for _,c:=range []struct{name string;wire []byte}{ {"duplicate field",[]byte(%q)}, {"out of first use",[]byte(%q)}, {"unused ID",[]byte(%q)}, {"duplicate ID",[]byte(%q)} } {
   if _,err:=RootReadStrict(c.wire);err==nil {t.Errorf("%%s accepted",c.name)}
 }
}`
	source := fmt.Sprintf(sourceTemplate, string(wire), string(duplicate.done()), string(firstUse.done()), string(unused.done()), string(duplicateID.done()))
	runGenerated(t, newSchema, source)
	newer := kindsUnit{t: t, u: unitFrom(t, newSchema)}
	newer.m = tabletext.NewModel(newer.u)
	reverse := newer.encode("Root", `{"a":7,"b":9}`)
	reverseSource := fmt.Sprintf(sourceTemplate, string(reverse), string(duplicate.done()), string(firstUse.done()), string(unused.done()), string(duplicateID.done()))
	runGenerated(t, oldSchema, reverseSource)
}
