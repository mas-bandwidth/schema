// A `| required` field whose range excludes zero takes no default (docs/SPEC-TABLES.md
// §2.3, SPEC §4.6): the field compiles with a declared default, its fresh value is the
// storage zero — which is outside the range, so Save refuses it — and once set it saves
// and round-trips, while a record that never carried the field is missing_required on
// a strict read in the binary, base64 and JSON forms alike.
package gotable

import "testing"

func TestGoRequiredExactFieldHasNoDefault(t *testing.T) {
	t.Parallel()
	const schema = `package probe
table Exact
{
    ver uint8 | min = 7, max = 7, required
}
table OldExact
{
}
`
	const body = `package probe
import ("errors"; "testing")
func TestReq(t *testing.T){
	// a FRESH record holds ver at storage zero, which is outside [7,7]:
	// Save is refused (-1).
	var v Exact
	if n := ExactSave(&v, make([]byte, 256)); n != -1 { t.Fatalf("fresh save = %d, want -1 (range excludes zero)", n) }
	// set ver=7: saves and round-trips.
	v.Ver = 7
	n := ExactMeasure(&v)
	buf := make([]byte, n)
	if ExactSave(&v, buf) != n { t.Fatal("save short") }
	got, err := ExactReadStrict(buf)
	if err != nil || got == nil || *got != v { t.Fatalf("clean strict read: %+v %v", got, err) }
	// a wire WITHOUT the field (an older record) is missing_required on ver.
	var old OldExact
	n = OldExactMeasure(&old)
	buf = make([]byte, n)
	OldExactSave(&old, buf)
	var m TableMissingRequired
	if _, err := ExactReadStrict(buf); err == nil || !errors.As(err, &m) || m.Fields != "ver" { t.Fatalf("ReadStrict missing field error %v (%T), want TableMissingRequired{ver}", err, err) }
	// the JSON form refuses the same absence (SPEC-TABLES §2.3, §16): an
	// object without ver is missing_required, not a silent zero outside [7,7].
	var jb ExactBuilder
	if !jb.Init() { t.Fatal("builder init") }
	defer jb.Shutdown()
	var rep TableReport
	if ExactFromJson(&jb, []byte("{\"hp\":5}"), &rep) { t.Fatal("JSON without ver was accepted") }
	if !errors.As(rep.Err(), &m) || m.Fields != "ver" { t.Fatalf("JSON missing field error %v (%T), want TableMissingRequired{ver}", rep.Err(), rep.Err()) }
	rep = TableReport{}
	if !ExactFromJson(&jb, []byte("{\"ver\":7}"), &rep) || rep.Err() != nil { t.Fatalf("JSON with ver refused: %+v", rep) }
}
`
	runGenerated(t, schema, body)
}
