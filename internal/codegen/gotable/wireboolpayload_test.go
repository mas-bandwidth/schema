package gotable

import (
	"os"
	"testing"
)

func TestGoWireBoolPayloadNot0Or1(t *testing.T) {
	t.Parallel()
	schema, err := os.ReadFile("../../../tables/examples/Guarded.schema")
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, string(schema), `package tabledemo
import("testing";"encoding/hex";"bytes";"errors";"unsafe")
func TestGoWireBoolPayloadNot0Or1(t *testing.T){
t.Parallel()
wire,_:=hex.DecodeString("010101fe020a0000c03f0301010404e8030000006f0c6f030b798065400e20a08a498122bdcfaa557f0e7f245050a215c09abcb70400000000000000")
clean,_:=hex.DecodeString("01010101020a0000c03f0301010404e8030000006f0c6f030b798065400e20a08a498122bdcfaa557f0e7f245050a215c09abcb70400000000000000")
region:=func(w []byte)[]byte{size:=PatrolLoadMeasure(w);if size<0{t.Fatalf("measure")};raw:=make([]byte,size+64);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;return raw[off:off+uintptr(size)]}
var r TableReport
v:=PatrolLoadTolerant(region(wire), wire, &r)
if v==nil{t.Fatalf("tolerant refused: %+v", r)}
if !v.Active{t.Fatal("active not true")}
if !r.Malformed{t.Fatal("no malformed reported")}
if r.Reason!="malformed: Active"{t.Fatalf("Reason=%q want %q", r.Reason, "malformed: Active")}
if _,err:=PatrolReadStrict(wire);!errors.Is(err, TableRefuseMalformed){t.Fatalf("strict did not refuse: %v", err)}
want,err:=PatrolReadStrict(clean)
if err!=nil{t.Fatalf("clean strict: %v", err)}
if !want.Active{t.Fatal("clean active not true")}
n:=PatrolMeasure(want);buf:=make([]byte,n)
if n==0||PatrolSave(want,buf)!=n{t.Fatalf("clean save failed")}
if !bytes.Equal(buf, clean){t.Fatalf("re-encode differs")}
for _,b:=range []byte{0x00,0x01}{flip:=append([]byte(nil),clean...);flip[3]=b;val,err:=PatrolReadStrict(flip);if err!=nil{t.Fatalf("bool payload 0x%02x reads clean: %v", b, err)};if val.Active!=(b!=0){t.Fatalf("bool payload 0x%02x wrong value", b)}}
}
`)
}
