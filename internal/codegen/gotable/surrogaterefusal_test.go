package gotable

import (
	"testing"
)

// TestGoJsonRefusesLoneSurrogate pins docs/SPEC-TABLES.md §16.3's ruling: a
// lone or out-of-order surrogate escape is REFUSED BY NAME at every string site
// — a field value, a key, a wstring — never silently replaced with U+FFFD.
// The read returns false with the report Malformed and Reason starting
// string_lone_surrogate; an astral character still reads clean.
func TestGoJsonRefusesLoneSurrogate(t *testing.T) {
	t.Parallel()
	runGenerated(t, loneSurrogateSchema, loneSurrogateProbe)
}

const longKeyFieldName = "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk"

const loneSurrogateSchema = `package probe

fixed table Root
{
    s string(16)
    w wstring(16)
    ` + longKeyFieldName + ` string(16)
}
`

const loneSurrogateProbe = `package probe

import (
	"strings"
	"testing"
)

func TestLoneSurrogate(t *testing.T) {
	// <key> is the decoded key as written (§16.3), not a table-qualified
	// field name: a known field's value is named by its key, as a skipped
	// value is.
	cases := []struct{ label, text, reason string }{
		{"high alone", "{\"s\":\"\\ud800\"}", "string_lone_surrogate: s"},
		{"low alone", "{\"s\":\"\\udfaa\"}", "string_lone_surrogate: s"},
		{"high followed by plain", "{\"s\":\"\\ud800abc\"}", "string_lone_surrogate: s"},
		{"high followed by raw utf8", "{\"s\":\"\\ud888ሴ\"}", "string_lone_surrogate: s"},
		{"low then high", "{\"s\":\"\\udd1e\\ud834\"}", "string_lone_surrogate: s"},
		{"two highs", "{\"s\":\"\\ud800\\ud800\\n\"}", "string_lone_surrogate: s"},
		{"key low alone", "{\"\\udfaa\":0}", "string_lone_surrogate: <root>"},
		{"skipped unknown value", "{\"unknown\":\"\\ud800\"}", "string_lone_surrogate: unknown"},
		{"skipped known wrong shape", "{\"s\":[\"\\ud800\"]}", "string_lone_surrogate: s"},
		{"bad key after good field", "{\"s\":\"a\",\"\\udfaa\":0}", "string_lone_surrogate: <root>"},
		{"bad key after skipped field", "{\"unknown\":1,\"\\udfaa\":0}", "string_lone_surrogate: <root>"},
		{"bad key in skipped object", "{\"unknown\":{\"a\":1,\"\\udfaa\":2}}", "string_lone_surrogate: unknown"},
		{"wstring high alone", "{\"w\":\"\\ud800\"}", "string_lone_surrogate: w"},
	}
	for _, tc := range cases {
		var v Root
		var report TableReport
		if RootFromJson(&v, []byte(tc.text), &report) {
			t.Fatalf("%s: FromJson accepted %s (%+v)", tc.label, tc.text, report)
		}
		if !report.Malformed || report.Reason != tc.reason {
			t.Fatalf("%s: report %+v, want Malformed with Reason %q", tc.label, report, tc.reason)
		}
	}
	// a long key inside a SKIPPED object is not a clamp: the key only names a
	// refusal (§16.3), and the engine does not count its bound either, so the
	// two readers must agree on valid JSON.
	long := strings.Repeat("k", 300)
	var lv Root
	var lr TableReport
	if !RootFromJson(&lv, []byte("{\"unknown\":{\""+long+"\":1}}"), &lr) || lr.Malformed || lr.Clamped != 0 {
		t.Fatalf("long key in a skipped object: %+v", lr)
	}
	// a COMPARED key that overflows the key buffer is not a shorter key
	// (§2.8, §16.3): it keeps its clamp, so two spellings sharing the field's
	// 255-byte prefix cannot alias one field.
	fieldKey := strings.Repeat("k", 255)
	var fv Root
	var fr TableReport
	if !RootFromJson(&fv, []byte("{\""+fieldKey+"\":\"a\"}"), &fr) || fr.Malformed || fr.Clamped != 0 {
		t.Fatalf("a key exactly at the field's bound: %+v", fr)
	}
	for _, suffix := range []string{"x", "xyz"} {
		var av Root
		var ar TableReport
		ok := RootFromJson(&av, []byte("{\""+fieldKey+suffix+"\":\"a\"}"), &ar)
		if ok || ar.Clamped != 1 {
			t.Fatalf("compared key over the key buffer %q: ok=%v %+v, want one clamp", suffix, ok, ar)
		}
	}
	// the astral character reads clean, as bytes and as code units
	var v Root
	var report TableReport
	if !RootFromJson(&v, []byte("{\"s\":\"𝄞\"}"), &report) || report != (TableReport{}) {
		t.Fatalf("astral string did not read clean: %+v", report)
	}
	if v.SLength != 4 || v.S[0] != 0xF0 || v.S[1] != 0x9D || v.S[2] != 0x84 || v.S[3] != 0x9E {
		t.Fatalf("astral string is %x len=%d", v.S[:v.SLength], v.SLength)
	}
	var w Root
	if !RootFromJson(&w, []byte("{\"w\":\"𝄞\"}"), &report) || report != (TableReport{}) {
		t.Fatalf("astral wstring did not read clean: %+v", report)
	}
	if w.WLength != 2 || w.W[0] != 0xD834 || w.W[1] != 0xDD1E {
		t.Fatalf("astral wstring units are %x len=%d", w.W[:w.WLength], w.WLength)
	}
}
`
