package parser

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/ast"
)

// TestLeadingZeroIntegersAreRefused: issue #710 stopped `08` failing as a
// malformed octal and read it as decimal; a leading-zero decimal is now
// refused by name instead (SPEC §4.1), because the backends that render a
// literal in its source spelling hand `010` to C, C++ and Go, which read it
// as octal. Hex and binary keep their values.
func TestLeadingZeroIntegersAreRefused(t *testing.T) {
	for _, text := range []string{"08", "010", "00"} {
		_, errs := Parse("T.schema", []byte("package p\nconst X = "+text+"\n"))
		if len(errs) == 0 || !strings.Contains(errs[0].Error(), "leading zero") {
			t.Fatalf("%s: want a leading-zero refusal, got %v", text, errs)
		}
	}
	for _, tc := range []struct {
		text string
		want int64
	}{{"0", 0}, {"8", 8}, {"0x10", 16}, {"0b10", 2}} {
		f, errs := Parse("T.schema", []byte("package p\nconst X = "+tc.text+"\n"))
		if len(errs) > 0 {
			t.Fatalf("%s: %v", tc.text, errs)
		}
		c := f.Decls[0].(*ast.ConstDecl)
		if v := c.Expr.(*ast.IntLit).Value.Int64(); v != tc.want {
			t.Fatalf("%s = %d, want %d", tc.text, v, tc.want)
		}
	}
}
