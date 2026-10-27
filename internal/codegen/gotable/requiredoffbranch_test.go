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
	// The TEXT form owes the same rule (docs/SPEC-TABLES.md §2.3, §16.2):
	// with the branch off a default Current writes no req and its own text
	// reads back, while with the branch on a missing req is still refused.
	for _, options := range []TableJSONOptions{{}, {OmitDefaults: true}} {
		var jc Current
		text := make([]byte, CurrentToJSONMeasureWith(&jc, options)+1)
		if jn := CurrentToJSONWith(&jc, text, options); jn <= 0 { t.Fatalf("off-branch JSON write refused: options %+v", options) } else { text = text[:jn] }
		jb := &CurrentBuilder{}
		if !jb.Init() { t.Fatal("builder Init") }
		jr := TableReport{}
		ok := CurrentFromJSONTolerantWith(jb, text, &jr, options)
		if !jb.Lock() { t.Fatal("builder Lock") }
		if !ok { t.Fatalf("JSON off-branch round trip refused: options %+v text %s report %+v", options, text, jr) }
		if jr.MissingRequired != 0 { t.Fatalf("JSON off-branch demanded req: options %+v text %s report %+v", options, text, jr) }
	}
	var jlegacy Legacy
	jlegacy.Active = true
	jlegacy.X = 5
	jtext := make([]byte, LegacyToJSONMeasure(&jlegacy)+1)
	if jn := LegacyToJSON(&jlegacy, jtext); jn <= 0 { t.Fatal("legacy JSON write failed") } else { jtext = jtext[:jn] }
	jb := &CurrentBuilder{}
	if !jb.Init() { t.Fatal("builder Init") }
	jr := TableReport{}
	if CurrentFromJSONTolerantWith(jb, jtext, &jr, TableJSONOptions{}) { t.Fatalf("JSON on-branch missing read as present: %s", jtext) }
	if jr.MissingRequired != 1 || jr.Reason != "missing_required: req" { t.Fatalf("JSON on-branch missing: want MissingRequired=1 reason missing_required: req, got %+v text %s", jr, jtext) }
	sb := &CurrentBuilder{}
	if !sb.Init() { t.Fatal("builder Init") }
	jr = TableReport{}
	if CurrentFromJSONWith(sb, jtext, &jr, TableJSONOptions{}) { t.Fatalf("JSON strict read accepted a missing required field: %s", jtext) }
	if jr.MissingRequired != 1 { t.Fatalf("JSON strict on-branch missing: want MissingRequired=1, got %+v", jr) }
}
`
	runGenerated(t, schema, body)
	runTextFormUnit(t, schema, map[string]string{"requiredoffbranch_test.go": textformBody})
}

// textformBody pins the hand-editable TEXT form of the same rule
// (docs/SPEC-TABLES.md §2.3, §16.9, <T>LoadTextForm): with the branch off a
// default Current loads, while with the branch on a missing req is refused with
// text_missing_required.
const textformBody = `package probe
import ("errors";"testing")
func TestReqTextForm(t *testing.T){
	for text, want := range map[string]string{
		"()": "",
		"(:active false)": "",
		"(:active true)": "text_missing_required",
		"(:active true :req 3)": "",
	} {
		_, _, err := CurrentLoadTextForm([]byte(text), TableTextFormDefaultLimits())
		if want == "" {
			if err != nil { t.Errorf("%s: %v", text, err) }
			continue
		}
		var e *TableTextFormError
		if !errors.As(err, &e) || e.Name != want {
			t.Errorf("%s: %v, want %s", text, err, want)
		}
	}
}
`
