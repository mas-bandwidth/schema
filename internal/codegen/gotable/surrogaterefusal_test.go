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

const loneSurrogateSchema = `package probe

fixed table Root
{
    s string(16)
    w wstring(16)
}
`

const loneSurrogateProbe = `package probe

import (
	"strings"
	"testing"
)

func TestLoneSurrogate(t *testing.T) {
	cases := []struct{ label, text string }{
		{"high alone", "{\"s\":\"\\ud800\"}"},
		{"low alone", "{\"s\":\"\\udfaa\"}"},
		{"high followed by plain", "{\"s\":\"\\ud800abc\"}"},
		{"high followed by raw utf8", "{\"s\":\"\\ud888ሴ\"}"},
		{"low then high", "{\"s\":\"\\udd1e\\ud834\"}"},
		{"two highs", "{\"s\":\"\\ud800\\ud800\\n\"}"},
		{"key low alone", "{\"\\udfaa\":0}"},
		{"wstring high alone", "{\"w\":\"\\ud800\"}"},
	}
	for _, tc := range cases {
		var v Root
		var report TableReport
		if RootFromJson(&v, []byte(tc.text), &report) {
			t.Fatalf("%s: FromJson accepted %s (%+v)", tc.label, tc.text, report)
		}
		if !report.Malformed || !strings.HasPrefix(report.Reason, "string_lone_surrogate") {
			t.Fatalf("%s: report %+v, want Malformed with Reason starting string_lone_surrogate", tc.label, report)
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
