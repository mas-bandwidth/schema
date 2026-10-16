// The `| required` field marker (docs/SPEC-TABLES.md §2.3): a required field
// rides the wire even at zero, and a record from an older layout that lacks it
// is refused with missing_required naming the missing field only.
package gotable

import (
	"strings"
	"testing"
)

func TestGoTableRequiredFieldRefusesWhenMissing(t *testing.T) {
	t.Parallel()
	const schema = `package probe
table T
{
    a int32
    b int32 | required
    c int32 | required
}
`
	// THE EMITTED SOURCE, in process: the writer drops default elision for
	// the required fields only, and the reader names a missing required
	// field. This half runs without SCHEMA_SLOW; the compiled half below is
	// the behavioural proof.
	t.Run("emitted", func(t *testing.T) {
		t.Parallel()
		files, err := Generate(unitFrom(t, schema))
		if err != nil {
			t.Fatal(err)
		}
		var src strings.Builder
		for _, data := range files {
			src.Write(data)
		}
		s := src.String()
		if !strings.Contains(s, "value.A != 0") {
			t.Fatalf("control: the plain field a no longer elides at its default, so the probe below proves nothing")
		}
		for _, m := range []string{"value.B != 0", "value.C != 0"} {
			if strings.Contains(s, m) {
				t.Errorf("a required field still elides at its default: the source carries %q", m)
			}
		}
		for _, m := range []string{"MissingRequired++", `"missing_required: "`, `missingReq += "b"`, `missingReq += "c"`} {
			if !strings.Contains(s, m) {
				t.Errorf("the reader does not refuse a missing required field: the source lacks %q", m)
			}
		}
	})
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
	// required int32s riding as ZERO ride the wire and read back
	var v T
	v.A = 7
	n := TSave(&v, make([]byte, 256))
	buf := make([]byte, n)
	if TSave(&v, buf) != n { t.Fatal("save short") }
	var r TableReport
	_, got := regFor(buf[:n], &r)
	if got == nil || *got != v { t.Fatalf("roundtrip: %+v %+v", got, r) }
	if r != (TableReport{}) { t.Fatalf("roundtrip report: %+v", r) }
	// an OLDER layout (written before b and c were required) carries only a
	// and c: only b is missing, and only b is named. The bytes are what the
	// same table without the markers saves for a=7, c=4 (b elided at zero).
	old := []byte{1, 1, 4, 7, 0, 0, 0, 2, 4, 4, 0, 0, 0, 0}
	old = append(old, le8(TTableFields[0].Id)...)
	old = append(old, le8(TTableFields[2].Id)...)
	old = append(old, 2, 0, 0, 0, 0, 0, 0, 0)
	r = TableReport{}
	_, _ = regFor(old, &r)
	if r.MissingRequired != 1 { t.Fatalf("want MissingRequired=1, got %+v", r) }
	if r.Reason != "missing_required: b" { t.Fatalf("want reason %q, got %q", "missing_required: b", r.Reason) }
}
`
	t.Run("compiled", func(t *testing.T) {
		t.Parallel()
		runGenerated(t, schema, body)
	})
}
