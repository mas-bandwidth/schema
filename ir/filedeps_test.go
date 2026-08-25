// Tests for FileDeps, the cross-file reference graph. Every expression a
// backend can render symbolically must appear as an edge here: the graph
// feeds the C++ #include emission. A missing edge is not a cosmetic
// omission — it silently produces mutual #includes in C++ and a missing
// `use crate::*` in Rust, and the include-cycle guard reads the same wrong
// graph, so nothing complains until the generated code fails to compile.
// IntMinExpr/IntMaxExpr were missing exactly this way.
package ir_test

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func loadFiles(t *testing.T, srcs map[string]string) *ir.Unit {
	t.Helper()
	var files []check.SourceFile
	for name, src := range srcs {
		f, perrs := parser.Parse(name, []byte(src))
		if len(perrs) > 0 {
			t.Fatalf("%s does not parse: %v", name, perrs[0])
		}
		base := name
		if n := len(base); n > 7 && base[n-7:] == ".schema" {
			base = base[:n-7]
		}
		files = append(files, check.SourceFile{
			Path: name, Name: name, Base: base, Bytes: []byte(src), AST: f,
		})
	}
	u, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		t.Fatalf("corpus does not check: %v", cerrs[0])
	}
	return u
}

// Each case declares a constant in Bounds.schema and references it from
// Use.schema through one expression-carrying position. Every one of them
// must produce the Use -> Bounds edge.
func TestFileDepsCoversEverySymbolicExpression(t *testing.T) {
	cases := []struct {
		name string
		use  string
	}{
		{"int range min/max", "package t\ntype T { hp int32 | min = -Lim, max = Lim }\n"},
		{"fixed range min/max", "package t\ntype T { p fixed(48, 16) | min = -Lim, max = Lim }\n"},
		{"array bound", "package t\ntype T { xs [Lim]uint8 }\n"},
		{"counted array bound", "package t\ntype T { xs [..Lim]uint8 }\n"},
		{"string size", "package t\ntype T { s string(Lim) }\n"},
		{"bytes size", "package t\ntype T { b bytes(Lim) }\n"},
		{"specified default", "package t\ntype T { n int32 = Lim | min = 0, max = 1000 }\n"},
		// the bound inside a nested expression, not at the top of the tree
		{"range bound in a binary expression", "package t\ntype T { hp int32 | min = 0, max = Lim * 2 + 1 }\n"},
		{"range bound under a unary minus", "package t\ntype T { hp int32 | min = -Lim, max = 0 }\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := loadFiles(t, map[string]string{
				"Bounds.schema": "package t\nconst Lim = 100\n",
				"Use.schema":    tc.use,
			})
			deps := ir.FileDeps(u)
			if !deps["Use"]["Bounds"] {
				t.Fatalf("Use -> Bounds edge missing: the expression naming Lim is not tracked, "+
					"so the include graph and dispatch-owner topo order are wrong (deps = %v)", deps)
			}
			// the edge is one-directional: Bounds names nothing in Use
			if deps["Bounds"]["Use"] {
				t.Errorf("spurious Bounds -> Use edge (deps = %v)", deps)
			}
		})
	}
}

// The regression proper: a range bound naming a cross-file constant, in the
// file-name order that once produced mutual includes. Aaa sorts first and
// references Bbb's constant, so without the edge the graph believes Aaa has
// no dependencies.
func TestFileDepsRangeBoundEdge(t *testing.T) {
	u := loadFiles(t, map[string]string{
		"Aaa.schema": "package t\ntype Ma { hp int32 | min = 0, max = Limit }\n",
		"Bbb.schema": "package t\nconst Limit = 100\ntype Mb { x uint8 }\n",
	})
	deps := ir.FileDeps(u)
	if !deps["Aaa"]["Bbb"] {
		t.Fatalf("Aaa -> Bbb edge missing for `max = Limit` (deps = %v)", deps)
	}
}
