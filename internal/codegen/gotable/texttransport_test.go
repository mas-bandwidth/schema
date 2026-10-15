package gotable

// A table over a TEXT CHANNEL (docs/SPEC-TABLES.md §16.8): the two carriers
// the owner ruled for, both emitted, both refusing damaged or newer input BY
// NAME. base64 of the binary wire keeps the form byte, the framing check and
// every versioning guarantee and is opaque; the JSON text form is for a human
// to read or edit and carries what §16 says it carries.
//
// What is pinned here, in the generated unit the house helper compiles and
// runs in a t.TempDir():
//
//   - both helpers round-trip, and SaveText's answer is padded standard
//     base64 of exactly the bytes <Name>Save writes — the carrier keeps the
//     form byte rather than inventing a text of its own;
//   - a corrupted base64 body refuses text_not_base64;
//   - a truncated decoded wire refuses wire_damaged, the BINARY reader's own
//     name (§16.8 takes none away and adds none here);
//   - a newer form byte refuses unknown_form;
//   - a damaged JSON document refuses text_damaged, through
//     <Name>FromJsonReason, which decides exactly as <Name>FromJson does;
//   - every refusal leaves the caller's instance at its declared defaults and
//     says malformed in the report the caller handed in.

import "testing"

const textTransportSchema = `package probe
type Sample {
 s float32
}
fixed table Root {
 a float32
 b float64
 pair Sample
 many [..8]float32
}
`

const textTransportTest = `package probe

import (
	"encoding/base64"
	"testing"
)

func TestTextTransport(t *testing.T) {
	var v Root
	RootReset(&v)
	v.A = 1.5
	v.B = -2.25
	v.Pair.S = 0.5
	v.ManyCount = 3
	v.Many[0], v.Many[1], v.Many[2] = 1, 2, 3

	wire := make([]byte, RootMeasure(&v))
	if n := RootSave(&v, wire); n != int64(len(wire)) {
		t.Fatalf("Save wrote %d of %d", n, len(wire))
	}

	// THE base64 CARRIER: padded standard base64 OF THE WIRE, byte for byte.
	text, err := RootSaveText(&v)
	if err != nil {
		t.Fatalf("SaveText: %v", err)
	}
	if text != base64.StdEncoding.EncodeToString(wire) {
		t.Fatalf("SaveText is not padded standard base64 of the wire:\n%s", text)
	}
	var got Root
	var r TableReport
	if err := RootLoadText(&got, text, &r); err != nil {
		t.Fatalf("LoadText: %v", err)
	}
	if got.A != v.A || got.B != v.B || got.Pair.S != v.Pair.S ||
		got.ManyCount != 3 || got.Many[0] != 1 || got.Many[1] != 2 || got.Many[2] != 3 {
		t.Fatalf("the base64 carrier lost a value: %+v", got)
	}
	if r.Malformed || r.Unknown != 0 || r.KindMismatch != 0 || r.Verdict != TableOpenOk {
		t.Fatalf("a clean read reported %+v", r)
	}

	// A BODY THAT DOES NOT DECODE refuses by name (§16.8).
	got.A = 9
	r = TableReport{}
	if err := RootLoadText(&got, text[:len(text)-1]+"!", &r); err != TableRefuseTextNotBase64 {
		t.Fatalf("a corrupted base64 body: %v, want text_not_base64", err)
	}
	if !r.Malformed || got.A != 0 {
		t.Fatalf("a corrupted base64 body must report malformed and reset: %+v %+v", r, got)
	}

	// A DECODED WIRE THE BINARY READER REFUSES refuses under the BINARY
	// reader's own name: framing damage is wire_damaged, a form byte this
	// build does not read is unknown_form (§3, §3.4, §16.8).
	for _, tc := range []struct {
		wire []byte
		want TableRefuseReason
		what string
	}{
		{wire[:len(wire)/2], TableRefuseWireDamaged, "a truncated decoded wire"},
		{append([]byte{4}, wire[1:]...), TableRefuseUnknownForm, "a newer form byte"},
		{append([]byte{2}, wire[1:]...), TableRefuseUnknownForm, "the message form byte"},
	} {
		got.A = 9
		r = TableReport{}
		err := RootLoadText(&got, base64.StdEncoding.EncodeToString(tc.wire), &r)
		if err != tc.want {
			t.Fatalf("%s: %v, want %v", tc.what, err, tc.want)
		}
		if got.A != 0 {
			t.Fatalf("%s: the instance kept a value: %+v", tc.what, got)
		}
	}

	// THE JSON CARRIER: a human's form, round-tripped, and its damaged
	// document refused by name (§16.3's reader half, §16.8).
	jtext := make([]byte, RootToJsonMeasure(&v))
	if n := RootToJson(&v, jtext); n != int64(len(jtext)) {
		t.Fatalf("ToJson wrote %d of %d", n, len(jtext))
	}
	var j Root
	var jr TableReport
	if !RootFromJson(&j, jtext, &jr) {
		t.Fatalf("FromJson: %+v", jr)
	}
	if j.A != v.A || j.B != v.B || j.Pair.S != v.Pair.S || j.ManyCount != 3 || j.Many[2] != 3 {
		t.Fatalf("the JSON carrier lost a value: %+v", j)
	}
	for _, damaged := range []string{"", "{", "{\"a\":", "{\"a\" 1}", "{} rubbish"} {
		j.A = 9
		jr = TableReport{}
		if reason := RootFromJsonReason(&j, []byte(damaged), &jr); reason != TableRefuseTextDamaged {
			t.Fatalf("the damaged document %q: %v, want text_damaged", damaged, reason)
		}
		if !jr.Malformed || j.A != 0 {
			t.Fatalf("the damaged document %q must report malformed and reset: %+v %+v", damaged, jr, j)
		}
	}
}
`

func TestTableWireOverATextTransport(t *testing.T) {
	t.Parallel()
	runGenerated(t, textTransportSchema, textTransportTest)
}
