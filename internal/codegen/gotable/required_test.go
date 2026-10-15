// The `| required` field marker (docs/SPEC-TABLES.md §2.3): a required field
// rides the wire even at zero, and a record from an older layout that lacks it
// is refused with missing_required naming the field.
package gotable

import "testing"

func TestGoTableRequiredFieldRefusesWhenMissing(t *testing.T) {
	t.Parallel()
	const schema = `package probe
table T
{
    a int32
    b int32 | required
}
`
	const body = `package probe
import ("testing"; "unsafe")
func le8(v uint64) []byte { b:=make([]byte,8); for i:=0;i<8;i++ { b[i]=byte(v>>(8*i)) }; return b }
func regFor(wire []byte, report *TableReport) ([]byte, *T) {
	size := TLoadMeasure(wire)
	raw := make([]byte, size+16)
	off := (-uintptr(unsafe.Pointer(&raw[0]))) & 15
	region := raw[off : off+uintptr(size)]
	return region, TLoad(region, wire, report)
}
func TestReq(t *testing.T){
	// a required int32 riding as ZERO rides the wire and reads back
	var v T
	v.A = 7
	v.B = 0
	n := TSave(&v, make([]byte, 256))
	buf := make([]byte, n)
	if TSave(&v, buf) != n { t.Fatal("save short") }
	var r TableReport
	_, got := regFor(buf[:n], &r)
	if got == nil || *got != v { t.Fatalf("roundtrip: %+v %+v", got, r) }
	if r.MissingRequired != 0 || r != (TableReport{}) { t.Fatalf("roundtrip report: %+v", r) }
	// an OLDER layout (written before b was required) lacks b and refuses
	old := []byte{1, 1, 4, 7, 0, 0, 0, 0}
	old = append(old, le8(TTableFields[0].Id)...)
	old = append(old, 1, 0, 0, 0, 0, 0, 0, 0)
	r = TableReport{}
	_, _ = regFor(old, &r)
	if r.MissingRequired != 1 { t.Fatalf("want MissingRequired=1, got %+v", r) }
	if r.Reason != "missing_required: b" { t.Fatalf("want reason %q, got %q", "missing_required: b", r.Reason) }
}
`
	runGenerated(t, schema, body)
}
