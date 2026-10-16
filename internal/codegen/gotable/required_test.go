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

// TestGoTableRequiredRefusalReachesStrictCallers pins the reader finding on
// the first head: a missing required field is refused by NAME through the
// generated public APIs, at the root and inside a nested table. A child body
// refused for a missing required field refuses every body above it (the
// report is shared down the walk), so Load reports the record refused rather
// than a parent with a defaulted child, and ReadStrict returns the typed
// TableMissingRequired naming the field instead of a bare wire_damaged
// (docs/SPEC-TABLES.md §2.3).
func TestGoTableRequiredRefusalReachesStrictCallers(t *testing.T) {
	t.Parallel()
	// The Old* twins are the older layouts: a field id is the hash of the
	// field's name (§5), so a twin without a field writes exactly the record
	// an older writer of Root or Kid wrote before that field was required.
	const schema = `package probe
table Kid
{
    x int32 | required
    y int32
}
table OldKid
{
    y int32
}
table Root
{
    a int32 | required
    kid Kid
}
table OldRootNoA
{
    kid Kid
}
table OldRootOldKid
{
    a int32
    kid OldKid
}
`
	const body = `package probe
import ("errors"; "testing"; "unsafe")
func load(wire []byte) (*Root, TableReport) {
	var r TableReport
	size := RootLoadMeasure(wire)
	if size <= 0 { return nil, r }
	raw := make([]byte, size+16)
	off := (-uintptr(unsafe.Pointer(&raw[0]))) & 15
	got := RootLoad(raw[off:off+uintptr(size)], wire, &r)
	return got, r
}
func wantMissing(t *testing.T, what string, wire []byte, field string) {
	t.Helper()
	_, r := load(wire)
	if r.MissingRequired != 1 || r.Reason != "missing_required: "+field { t.Fatalf("%s: Load report %+v, want MissingRequired=1 reason missing_required: %s", what, r, field) }
	var m TableMissingRequired
	if err := r.Err(); !errors.As(err, &m) || m.Fields != field { t.Fatalf("%s: report.Err() = %v, want TableMissingRequired{%q}", what, err, field) }
	// The body walk itself refuses: a child refused for a missing required
	// field stops its parent, so the builder load answers false, not a
	// parent read whole around a defaulted child.
	var b RootBuilder
	if !b.Init() { t.Fatal("builder init") }
	var br TableReport
	ok := RootLoadBuilder(&b, wire, &br)
	b.Shutdown()
	if ok || br.MissingRequired != 1 { t.Fatalf("%s: LoadBuilder accepted the record (ok=%v report %+v)", what, ok, br) }
	v, err := RootReadStrict(wire)
	if v != nil || err == nil { t.Fatalf("%s: ReadStrict accepted the record: %+v", what, v) }
	if !errors.As(err, &m) || m.Fields != field || err.Error() != "missing_required: "+field { t.Fatalf("%s: ReadStrict error %v (%T), want missing_required: %s", what, err, err, field) }
}
func TestStrict(t *testing.T) {
	// CLEAN: required int32s riding as ZERO, root and child, read back strict.
	var v Root
	v.Kid.Y = 9
	n := RootMeasure(&v)
	buf := make([]byte, n)
	if RootSave(&v, buf) != n { t.Fatal("save short") }
	got, err := RootReadStrict(buf)
	if err != nil || got == nil || *got != v { t.Fatalf("clean strict read: %+v %v", got, err) }
	if _, r := load(buf); r.MissingRequired != 0 || r.Err() != nil { t.Fatalf("clean load report: %+v", r) }
	// ROOT: a record from before a was required.
	var old OldRootNoA
	old.Kid.X = 5
	n = OldRootNoAMeasure(&old)
	buf = make([]byte, n)
	OldRootNoASave(&old, buf)
	wantMissing(t, "root", buf, "a")
	// CHILD: the root is whole, the nested Kid is from before x was required.
	var oc OldRootOldKid
	oc.A = 3
	oc.Kid.Y = 2
	n = OldRootOldKidMeasure(&oc)
	buf = make([]byte, n)
	OldRootOldKidSave(&oc, buf)
	wantMissing(t, "child", buf, "x")
}
`
	runGenerated(t, schema, body)
}
