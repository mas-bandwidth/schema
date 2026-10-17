package gotable

import "testing"

// TestGoTextZeroByteTakesOneRuleInEveryForm pins docs/SPEC-TABLES.md §16.3's
// ruling: a JSON string the read would place in text storage that decodes to
// U+0000 refuses the read, with the report Malformed and Reason
// text_holds_zero naming the innermost field, so no Go read lands a value a Go
// writer refuses or a wire Go's strict reader refuses (§2.13, §4.2). Each row
// plants \u0000 at the front, the middle and the end of a string(8), a
// *string, a []string(8) element, a [..4]?string(8) element and a
// map[string(8)]int32 key, then a wstring(8) and a nested table's string(8):
// the read is refused, and whatever value it left measures, saves, reads
// back through ReadStrict and writes the same JSON text. A \u0001 control in
// the same place reads clean and round-trips, and a zero the read only SKIPS
// (an unknown key's value, an object key naming no field) is not the event.
// Red at BASE: FromJson accepted every zero row silently, and Save or
// ReadStrict then refused what the read produced.
func TestGoTextZeroByteTakesOneRuleInEveryForm(t *testing.T) {
	t.Parallel()
	runGenerated(t, textZeroSchema, textZeroProbe)
}

const textZeroSchema = `package probe

type Inner
{
    n string(8)
}

table Root
{
    s *string
    t string(8)
    l []string(8)
    o [..4]?string(8)
    m map[string(8)]int32
    w wstring(8)
    i Inner
}
`

const textZeroProbe = `package probe

import (
	"bytes"
	"strings"
	"testing"
)

func toJson(t *testing.T, what string, v *Root) []byte {
	t.Helper()
	n := RootToJsonMeasure(v)
	if n < 0 {
		t.Fatalf("%s: ToJsonMeasure refused", what)
	}
	buf := make([]byte, n)
	if RootToJson(v, buf) != n {
		t.Fatalf("%s: ToJson", what)
	}
	return buf
}

func save(t *testing.T, what string, b *RootBuilder) []byte {
	t.Helper()
	v := b.GetRoot()
	n := RootMeasure(v, &b.Arena)
	if n < 0 {
		t.Fatalf("%s: Measure refused the value a read produced", what)
	}
	wire := make([]byte, n)
	if RootSave(v, wire, &b.Arena) != n {
		t.Fatalf("%s: Save", what)
	}
	return wire
}

// carry saves what a read left, reads it back strictly, and holds the text:
// the strict value's ToJson reads clean, saves the same wire and writes the
// same text again. It answers that text.
func carry(t *testing.T, what string, b *RootBuilder) []byte {
	t.Helper()
	wire := save(t, what, b)
	strict, err := RootReadStrict(wire)
	if err != nil {
		t.Fatalf("%s: ReadStrict refused the wire Save wrote: %v", what, err)
	}
	text := toJson(t, what+" strict", strict)
	var again RootBuilder
	if !again.Init() {
		t.Fatal("init")
	}
	defer again.Shutdown()
	var report TableReport
	if !RootFromJson(&again, text, &report) || report != (TableReport{}) {
		t.Fatalf("%s: the strict value's text did not read clean (%+v)\n%s", what, report, text)
	}
	if !bytes.Equal(save(t, what+" again", &again), wire) {
		t.Fatalf("%s: the text moved the wire", what)
	}
	back, err := RootReadStrict(wire)
	if err != nil || !bytes.Equal(toJson(t, what+" back", back), text) {
		t.Fatalf("%s: the text moved through the wire (%v)", what, err)
	}
	return text
}

func TestTextZero(t *testing.T) {
	fields := []struct{ name, text string }{
		{"s", "{\"s\":\"%s\"}"},
		{"t", "{\"t\":\"%s\"}"},
		{"l", "{\"l\":[\"ok\",\"%s\"]}"},
		{"o", "{\"o\":[null,\"%s\"]}"},
		{"m", "{\"m\":{\"ok\":1,\"%s\":2}}"},
		{"w", "{\"w\":\"%s\"}"},
		{"i", "{\"i\":{\"n\":\"%s\"}}"},
	}
	where := map[string]string{"s": "Root.s", "t": "Root.t", "l": "Root.l", "o": "Root.o", "m": "Root.m", "w": "Root.w", "i": "Inner.n"}
	for _, f := range fields {
		for _, at := range []string{"\\u0000ab", "a\\u0000b", "ab\\u0000"} {
			text := strings.Replace(f.text, "%s", at, 1)
			what := f.name + " " + text
			var b RootBuilder
			if !b.Init() {
				t.Fatal("init")
			}
			var report TableReport
			if RootFromJson(&b, []byte(text), &report) {
				t.Fatalf("%s: FromJson accepted a zero in text (%+v)", what, report)
			}
			if !report.Malformed || report.Reason != "text_holds_zero: "+where[f.name] ||
				report.Clamped != 0 || report.KindMismatch != 0 || report.Unknown != 0 {
				t.Fatalf("%s: report %+v", what, report)
			}
			carry(t, what, &b)
			b.Shutdown()
			// the control: a \u0001 in the same place is ordinary text
			control := strings.Replace(text, "\\u0000", "\\u0001", 1)
			var c RootBuilder
			c.Init()
			report = TableReport{}
			if !RootFromJson(&c, []byte(control), &report) || report != (TableReport{}) {
				t.Fatalf("%s: the control did not read clean (%+v)", control, report)
			}
			if got := carry(t, control, &c); !bytes.Contains(got, []byte(strings.Replace(at, "\\u0000", "\\u0001", 1))) {
				t.Fatalf("%s: the control text was not carried\n%s", control, got)
			}
			c.Shutdown()
		}
	}
	// a zero the read only skips stores nothing and is not the event
	for _, text := range []string{"{\"zz\":\"a\\u0000b\"}", "{\"t\\u0000\":\"x\"}"} {
		var b RootBuilder
		b.Init()
		var report TableReport
		if !RootFromJson(&b, []byte(text), &report) || report.Malformed || report.Unknown != 1 {
			t.Fatalf("%s: a skipped zero %+v", text, report)
		}
		carry(t, text, &b)
		b.Shutdown()
	}
}
`
