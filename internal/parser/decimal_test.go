package parser

import (
	"github.com/mas-bandwidth/schema/v2/internal/ast"
	"testing"
)

func TestLeadingZeroIntegersAreDecimal(t *testing.T) {
	for _, tc := range []struct {
		text string
		want int64
	}{{"08", 8}, {"010", 10}, {"0x10", 16}, {"0b10", 2}} {
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
