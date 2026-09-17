package compiler

import (
	"os"
	"path/filepath"
	"testing"
)

// §16.2 (#610): the interpreter's 64-bit ceiling is NOT a count apart from the
// field's own domain for the 64-bit integer kinds. A magnitude past what
// sixty-four bits hold clamps ONCE, folded into the field's own domain clamp —
// a caller reading `clamped` wants to know a value was not the writer's, not
// how many rules it crossed.
//
// #607's one checked numeric interpretation moved this observable: an int64
// field given a magnitude past sixty-four bits counted two clamps where it had
// counted one (the interpreter's ceiling, then the field's domain). This holds
// the folded count, one event per field, on the tool's own walk, which the
// generated C walk and the C++ reference mirror.
func TestIssue610InterpreterCeilingIsOneClamp(t *testing.T) {
	const src = `package p

fixed table Root
{
    signed_box   int64
    unsigned_box uint64
    narrow_int   int32
    ranged       int64 | min = 0, max = 1000
}
`
	cases := []struct {
		name  string
		field string
		value string
	}{
		{"int64 past the 64-bit ceiling", "signed_box", "99999999999999999999999999"},
		{"int64 past INT64_MAX within 64 bits", "signed_box", "9223372036854775808"},
		{"int32 past the 64-bit ceiling", "narrow_int", "99999999999999999999999999"},
		{"ranged int64 past the 64-bit ceiling", "ranged", "99999999999999999999999999"},
		// a magnitude past 64 bits in an unsigned 64-bit field counts the one
		// clamp the ceiling owes: the guard that the fold did not DROP a count.
		{"uint64 past the 64-bit ceiling", "unsigned_box", "99999999999999999999999999"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := unitFromSource(t, src)
			dir := t.TempDir()
			text := `{ "` + tc.field + `": ` + tc.value + ` }`
			if err := os.WriteFile(filepath.Join(dir, "Root.json"), []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
			_, _, report, err := New().Pack(u, "Root", dir)
			if err != nil {
				t.Fatal(err)
			}
			if report.Clamped != 1 || report.KindMismatch != 0 || report.Malformed {
				t.Fatalf("a magnitude past the field's domain owes ONE clamp, got %+v", report)
			}
		})
	}
}
