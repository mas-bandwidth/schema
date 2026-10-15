package gotable

import (
	"testing"
)

// TestTableWireOverATextTransport is the card's gate (table-wire-over-text-
// transports): both text helpers — the base64 of the binary wire
// (SaveText/LoadText) and the JSON text form (FromJson/ToJson) — round-trip,
// and each reader refuses damaged or newer input by name. Docs in
// docs/SPEC-TABLES.md §16.6.
func TestTableWireOverATextTransport(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
fixed table Root {
	id uint32
	name string(8)
}
`, `package probe
import("testing";"encoding/base64")
func TestTableWireOverATextTransport(t *testing.T) {
	var v Root
	v.Id = 7
	copy(v.Name[:], "hi")
	v.NameLength = 2

	// BOTH HELPERS ROUND-TRIP.
	text, err := RootSaveText(&v)
	if err != nil { t.Fatalf("save text: %v", err) }
	got, err := RootLoadText(text, nil)
	if err != nil || got == nil || got.Id != 7 || string(got.Name[:got.NameLength]) != "hi" {
		t.Fatalf("base64 round trip: %+v %v", got, err)
	}
	buf := make([]byte, RootMeasure(&v))
	if n := RootSave(&v, buf); n != int64(len(buf)) { t.Fatalf("wire save: %d", n) }
	if text != base64.StdEncoding.EncodeToString(buf) {
		t.Fatalf("save text is not base64 of the wire")
	}
	jsonText := make([]byte, RootToJsonMeasure(&v))
	RootToJson(&v, jsonText)
	var j Root
	if !RootFromJson(&j, jsonText, nil) || j.Id != 7 || string(j.Name[:j.NameLength]) != "hi" {
		t.Fatalf("json round trip: %+v", j)
	}

	// A CORRUPTED BASE64 BODY REFUSES BY NAME.
	_, err = RootLoadText("!!!not-base64!!!", nil)
	if err == nil || err.Error() != "base64_damaged" {
		t.Fatalf("corrupt base64: %v", err)
	}

	// A TRUNCATED DECODED WIRE REFUSES BY NAME.
	wire, _ := base64.StdEncoding.DecodeString(text)
	_, err = RootLoadText(base64.StdEncoding.EncodeToString(wire[:4]), nil)
	if err == nil || err.Error() != "wire_damaged" {
		t.Fatalf("truncated wire: %v", err)
	}

	// A NEWER FORM BYTE REFUSES BY NAME.
	wire[0] = 2
	_, err = RootLoadText(base64.StdEncoding.EncodeToString(wire), nil)
	if err == nil || err.Error() != "unknown_form" {
		t.Fatalf("newer form: %v", err)
	}

	// A DAMAGED JSON DOCUMENT REFUSES BY NAME.
	var r TableReport
	if RootFromJson(&v, []byte("{ \"id\": 7 not json"), &r) || !r.Malformed {
		t.Fatalf("damaged json: %+v", r)
	}
}
`)
}
