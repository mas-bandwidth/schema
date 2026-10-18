// A `| required` field inside an `if` guard applies only while the guard
// holds (docs/SPEC-TABLES.md §2.3): with the branch off the field is absent
// from the wire and from defaults and a read does not demand it; with the
// branch on it must ride and a strict read refuses a record missing it with
// missing_required.
package gotable

import (
	"testing"
)

func TestGoRequiredInOffBranch(t *testing.T) {
	t.Parallel()
	const schema = `package probe
table Current
{
    active bool
    if active
    {
        req int32 | required
    }
}
table Legacy
{
    active bool
    if active
    {
        x int32
    }
}
`
	const body = `package probe
import ("testing"; "unsafe"; "errors")
func regFor(wire []byte, report *TableReport) *Current {
	size := CurrentLoadMeasure(wire)
	raw := make([]byte, size+16)
	off := (-uintptr(unsafe.Pointer(&raw[0]))) & 15
	region := raw[off : off+uintptr(size)]
	return CurrentLoad(region, wire, report)
}
func TestReq(t *testing.T){
	buf := make([]byte, 256)
	// branch OFF: a Current with active=false saves no req; it round-trips at
	// defaults with no refusal.
	var c Current
	n := CurrentSave(&c, buf)
	if n <= 0 { t.Fatal("off-branch save failed") }
	var r TableReport
	got := regFor(buf[:n], &r)
	if got == nil { t.Fatalf("off-branch round trip refused: %+v", r) }
	if r.MissingRequired != 0 { t.Fatalf("off-branch demanded req: %+v", r) }
	// branch ON, present: reads.
	c.Active = true
	c.Req = 7
	n = CurrentSave(&c, buf)
	r = TableReport{}
	got = regFor(buf[:n], &r)
	if got == nil || got.Req != 7 { t.Fatalf("on-branch present: got %v report %+v", got, r) }
	// branch ON, missing: a Legacy (older) record with active=true lacks req, so
	// the tolerant read reports the miss and the strict read refuses by name.
	var lg Legacy
	lg.Active = true
	lg.X = 5
	n = LegacySave(&lg, buf)
	r = TableReport{}
	if got = regFor(buf[:n], &r); got != nil { t.Fatalf("on-branch missing read as present") }
	if r.MissingRequired != 1 { t.Fatalf("want MissingRequired=1, got %+v", r) }
	if r.Reason != "missing_required: req" { t.Fatalf("want reason %q, got %q", "missing_required: req", r.Reason) }
	if _, err := CurrentReadStrict(buf[:n]); err == nil { t.Fatal("strict read accepted a missing required field") } else {
		var m TableMissingRequired
		if !errors.As(err, &m) || m.Fields != "req" { t.Fatalf("strict read error %v %T", err, err) }
	}
}
`
	runGenerated(t, schema, body)
}
