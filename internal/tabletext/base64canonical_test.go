package tabletext_test

import (
	"bytes"
	"encoding/base64"
	"testing"
)

// TestTextJSONBase64CanonicalRule pins docs/SPEC-TABLES.md §16.2's bytes(N)
// row and §16.8 in the tabletext reader itself (decodeBase64Bounded): a
// canonical body reads clean; a body that is base64 but not its exact
// encoding keeps its bytes and is reported base64_not_canonical, naming the
// field; anything else is not base64 and stays kind_mismatch with the field
// at its default.
func TestTextJSONBase64CanonicalRule(t *testing.T) {
	t.Parallel()
	edge := base64.StdEncoding.EncodeToString(make([]byte, 766)) // 1024 characters ending "AA=="
	for _, c := range []struct {
		name, body string
		want       []byte
		class      string // canonical | noncanonical | mismatch
	}{
		{"canonical_one", "AQ==", []byte{1}, "canonical"},
		{"canonical_three", "AQID", []byte{1, 2, 3}, "canonical"},
		{"canonical_empty", "", nil, "canonical"},
		{"escaped_solidus_canonical", `\/w==`, []byte{0xff}, "canonical"},
		{"nonzero_pad_bits", "AR==", []byte{1}, "noncanonical"},
		{"missing_equals", "AQ", []byte{1}, "noncanonical"},
		{"extra_equals", "AQ===", []byte{1}, "noncanonical"},
		{"equals_in_middle", "AQ==AQ==", []byte{1, 0, 0x10}, "noncanonical"},
		{"embedded_space", "AQ I=", []byte{1, 2}, "noncanonical"},
		{"escaped_newline", `AQ\n==`, []byte{1}, "noncanonical"},
		{"harness_Q=Q==", "Q=Q==", []byte{0x41}, "noncanonical"},
		{"harness_Q=Q=A=A=", "Q=Q=A=A=", []byte{0x41, 0, 0}, "noncanonical"},
		{"harness_QQ==Q", "QQ==Q", []byte{0x41, 0x04}, "noncanonical"},
		{"harness_invalid-20", "QQ AA", []byte{0x41, 0, 0}, "noncanonical"},
		{"equals_at_chunk_boundary", edge + "AAAA", make([]byte, 16), "noncanonical"},
		{"not_alphabet", "QQ*A", nil, "mismatch"},
		{"lone_symbol", "QQQQQ", nil, "mismatch"},
		{"only_equals", "====", nil, "mismatch"},
		{"only_space", " ", nil, "mismatch"},
	} {
		_, inst, r := read(t, "ProfileConfig", `{"icon":"`+c.body+`"}`)
		got := field(t, inst, "icon").Cell.Str
		switch c.class {
		case "canonical":
			if r.Reason != "" || r.KindMismatch != 0 || !bytes.Equal(got, c.want) {
				t.Fatalf("%s: Reason %q KindMismatch %d icon %x, want clean %x", c.name, r.Reason, r.KindMismatch, got, c.want)
			}
		case "noncanonical":
			if r.Reason != "base64_not_canonical: icon" || r.KindMismatch != 0 || !bytes.Equal(got, c.want) {
				t.Fatalf("%s: Reason %q KindMismatch %d icon %x, want base64_not_canonical with %x", c.name, r.Reason, r.KindMismatch, got, c.want)
			}
		case "mismatch":
			if r.KindMismatch != 1 || r.Reason != "" || len(got) != 0 {
				t.Fatalf("%s: Reason %q KindMismatch %d icon %x, want kind_mismatch and the default", c.name, r.Reason, r.KindMismatch, got)
			}
		}
	}
}
