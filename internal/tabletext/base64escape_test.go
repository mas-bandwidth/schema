package tabletext_test

import (
	"bytes"
	"testing"
)

// TestTextBase64BodyDecodesEscapesFirst pins docs/SPEC-TABLES.md §16.2: a
// bytes field is a JSON string whose decoded value is base64, so JSON escapes
// inside the string are undone before base64 decoding, and an escape that
// decodes to a character outside the base64 alphabet stays kind_mismatch.
func TestTextBase64BodyDecodesEscapesFirst(t *testing.T) {
	t.Parallel()

	_, inst, r := read(t, "ProfileConfig", `{ "icon": "\/w==" }`)
	if !r.Silent() {
		t.Fatalf("escaped solidus: expected silence, got %+v", r)
	}
	if got := field(t, inst, "icon").Cell.Str; !bytes.Equal(got, []byte{0xff}) {
		t.Fatalf("escaped solidus: got %x, want ff", got)
	}

	_, inst, r = read(t, "ProfileConfig", `{ "icon": "\u0041Q==" }`)
	if !r.Silent() {
		t.Fatalf("unicode escape: expected silence, got %+v", r)
	}
	if got := field(t, inst, "icon").Cell.Str; !bytes.Equal(got, []byte{0x01}) {
		t.Fatalf("unicode escape: got %x, want 01", got)
	}

	_, inst, r = read(t, "ProfileConfig", `{ "icon": "\n" }`)
	if r.Malformed || r.KindMismatch != 1 {
		t.Fatalf("escaped newline: expected 1 kind_mismatch, got %+v", r)
	}
	if got := field(t, inst, "icon").Cell.Str; len(got) != 0 {
		t.Fatalf("escaped newline: expected default empty bytes, got %x", got)
	}
}
