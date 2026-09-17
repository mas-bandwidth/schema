package tabletext

import (
	"bytes"
	"testing"
)

// A JSON `\u0000` ESCAPE IN A STRING BODY IS REFUSED BY NAME (docs/SPEC-TABLES.md
// §3, §16.3). An interior zero byte is DAMAGE on the table wire — a kind 12
// payload carries no zero byte and a kind 33 payload no zero unit — so the text
// READ refuses the escape rather than build storage the wire cannot carry (§5).
// U+0000 IS a code point and JSON has an escape for it, so the U+FFFD rule for
// what is not a code point does not reach it: this is a refusal, not a
// replacement, and it is one rule at the point the defect enters.
//
// The scan functions are reached through the same cursor an ordinary read uses;
// this file is in the package (not the exported test package) so the unexported
// reader is the thing under test, with no schema load between the text and the
// rule.
func TestZeroEscapeIsRefused(t *testing.T) {
	cases := []string{
		`"a\u0000b"`, // interior
		`"\u0000ab"`, // at the front
		`"ab\u0000"`, // at the end
		`"\u0000"`,   // alone
	}
	for _, text := range cases {
		in := &reader{text: []byte(text), report: &Report{}}
		got, _, ok := in.scanString(-1)
		if ok || !in.bad {
			t.Fatalf("scanString(%s) accepted a zero escape: unit=% x ok=%v bad=%v", text, got, ok, in.bad)
		}
	}
	// The same rule on the WIDE scan: a zero unit is damage there too.
	in := &reader{text: []byte(`"A\u0000B"`), report: &Report{}}
	if _, _, ok := in.scanWString(-1); ok || !in.bad {
		t.Fatalf("scanWString accepted a zero escape: ok=%v bad=%v", ok, in.bad)
	}
}

// The control that keeps the refusal from swallowing the rest of the grammar: an
// ordinary escape and a surrogate pair still scan, because neither is a zero.
func TestZeroEscapeRefusalIsNotOverbroad(t *testing.T) {
	in := &reader{text: []byte(`"\u0041\uD83D\uDE00"`), report: &Report{}}
	got, _, ok := in.scanString(-1)
	if !ok || in.bad || !bytes.Equal(got, []byte("A\xf0\x9f\x98\x80")) {
		t.Fatalf("an ordinary escape or a surrogate pair was refused: unit=% x ok=%v bad=%v", got, ok, in.bad)
	}
}
