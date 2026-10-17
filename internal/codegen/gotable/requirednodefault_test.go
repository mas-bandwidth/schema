// A `| required` field whose range excludes zero takes no default (docs/SPEC-TABLES.md
// §2.3, SPEC §4.6): the field compiles with a declared default, its fresh value is the
// storage zero — which is outside the range, so Save refuses it — and once set it saves
// and round-trips, while a record that never carried the field is missing_required on
// a strict read in the binary, base64 and JSON forms alike.
package gotable

import (
	"sort"
	"strings"
	"testing"
)

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
table Other
{
    ver uint8
}
table Inner
{
    id uint8 | required
}
table Outer
{
    id uint8
    inner Inner
}
`
	const body = `package probe
import ("errors"; "strings"; "testing")
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
	// CROSS-TABLE: a required field is per field per table. Other's ver is
	// NOT required, and it shares only a NAME with Exact's required ver, so
	// an empty Other reads clean (§2.3).
	var ob OtherBuilder
	if !ob.Init() { t.Fatal("other builder init") }
	defer ob.Shutdown()
	var orep TableReport
	if !OtherFromJson(&ob, []byte("{}"), &orep) || orep.Err() != nil { t.Fatalf("Other {} wrongly refused (name collision): %+v", orep) }
	// NESTED: a required field inside a nested table is enforced at ITS
	// object, never at the enclosing one.
	var out OuterBuilder
	if !out.Init() { t.Fatal("outer builder init") }
	defer out.Shutdown()
	var nrep TableReport
	if OuterFromJson(&out, []byte("{\"inner\":{}}"), &nrep) { t.Fatal("nested inner without id accepted") }
	if !errors.As(nrep.Err(), &m) || !strings.Contains(m.Fields, "id") { t.Fatalf("nested missing error %v (%T), want TableMissingRequired naming id", nrep.Err(), nrep.Err()) }
	nrep = TableReport{}
	if !OuterFromJson(&out, []byte("{\"inner\":{\"id\":5}}"), &nrep) || nrep.Err() != nil { t.Fatalf("nested inner with id refused: %+v", nrep) }
}
`
	runGenerated(t, schema, body)
}

// TestGoRequiredGeneratedDeterministic pins the text form's generated output to
// byte-identical bytes across runs. The required-field registry is a Go map
// literal, so its entries must be emitted in sorted table order or the
// generated unit is non-deterministic (docs/SPEC-TABLES.md §2.3).
func TestGoRequiredGeneratedDeterministic(t *testing.T) {
	t.Parallel()
	const schema = `package probe
table A
{
    ver uint8 | min = 7, max = 7, required
}
table B
{
    id uint8 | required
}
table C
{
    flag uint8 | min = 1, max = 3, required
}
`
	var ref string
	var keys []string
	for i := 0; i < 20; i++ {
		out := generate(t, schema)
		if i == 0 {
			for k := range out {
				keys = append(keys, k)
			}
			sort.Strings(keys)
		}
		var b strings.Builder
		for _, k := range keys {
			b.Write(out[k])
		}
		got := b.String()
		if i == 0 {
			ref = got
		} else if got != ref {
			t.Fatalf("run %d: generated output differs from the first run", i)
		}
	}
}
