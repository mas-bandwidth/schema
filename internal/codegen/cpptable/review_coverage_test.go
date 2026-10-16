// Branches of the C++ table emitter's codecs that no test reached at base
// f28b1a58 (docs/CPP-REFERENCE-REVIEW.md, part 2). Each test emits the
// smallest unit that reaches the branch and pins what it writes.
package cpptable

import (
	"strings"
	"testing"
)

// TestTableIntLitInt64Min reaches tableIntLit's INT64_MIN guard
// (codecs.go:79) through a TABLE default. internal/codegen/cpp/expr_test.go
// says it pins "table struct NSDMIs", but its ExtremeRow is a `type` and it
// calls only the packet emitter, so the table path was never reached. A bare
// -9223372036854775808 is ill-formed C++ (the literal overflows before the
// minus applies), so the table NSDMI must spell it guarded.
func TestTableIntLitInt64Min(t *testing.T) {
	t.Parallel()
	out := specEmitAll(t, `package p
table Floor {
    x int64 = -9223372036854775808
}
`)
	if want := "int64_t x = ( -9223372036854775807ll - 1 );"; !strings.Contains(out, want) {
		t.Errorf("the table NSDMI must spell INT64_MIN guarded: want %q", want)
	}
	if strings.Contains(out, "= -9223372036854775808") {
		t.Error("the table emitter spelled INT64_MIN as a bare literal, which does not compile")
	}
}

// TestTableCStringLitEscapes reaches the table emitter's own cStringLit's
// backslash arm (codecs.go:2809), directly and through a table string
// default; the packet emitter's twin is pinned in internal/codegen/cpp.
func TestTableCStringLitEscapes(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		`a\b`:          `"a\\b"`,
		`say "x`:       `"say \"x"`,
		"tab\t7":       `"tab\0117"`,
		"\xe2\x82\xac": `"\342\202\254"`,
	} {
		if got := cStringLit([]byte(in)); got != want {
			t.Errorf("cStringLit(%q) = %s, want %s", in, got, want)
		}
	}
	out := specEmitAll(t, `package p
table Text {
    path string(8) = "a\b"
}
`)
	if want := `"a\\b"`; !strings.Contains(out, want) {
		t.Errorf("the table string default must escape its backslash: want %s", want)
	}
}
