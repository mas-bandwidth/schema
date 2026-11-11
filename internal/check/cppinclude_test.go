// A `cpp_include` value is one header path (SPEC §4.2 Native type mapping):
// the generators write it into a single `#include "<value>"` directive, so a
// value that could end the directive line or break the quoted path is refused
// by name at the checker.
package check

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/ast"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

func cppIncludeProbe(t *testing.T, value string) []error {
	t.Helper()
	files := []struct{ name, src string }{
		{"A.schema", "package t\ntype V | cpp_native = VMath, cpp_include = \"ok.h\" { x float64 }\n"},
		{"B.schema", "package t\ntype Body { p V }\n"},
	}
	var src []SourceFile
	for _, file := range files {
		f, perrs := parser.Parse(file.name, []byte(file.src))
		if len(perrs) > 0 {
			t.Fatalf("%s: parse failed: %v", file.name, perrs)
		}
		if file.name == "A.schema" {
			setCppInclude(t, f, value)
		}
		src = append(src, SourceFile{
			Path:  file.name,
			Name:  file.name,
			Base:  strings.TrimSuffix(file.name, ".schema"),
			Bytes: []byte(file.src),
			AST:   f,
		})
	}
	_, errs := Unit(src)
	return errs
}

func setCppInclude(t *testing.T, f *ast.File, value string) {
	t.Helper()
	for _, d := range f.Decls {
		td, ok := d.(*ast.TypeDecl)
		if !ok {
			continue
		}
		for i := range td.Attrs {
			if td.Attrs[i].Key != "cpp_include" {
				continue
			}
			lit, ok := td.Attrs[i].Value.(*ast.StringLit)
			if !ok {
				t.Fatalf("test schema has a non-string cpp_include: %T", td.Attrs[i].Value)
			}
			td.Attrs[i].Value = &ast.StringLit{Pos: lit.Pos, Value: value}
			return
		}
	}
	t.Fatal("test schema has no cpp_include attribute")
}

func TestCppIncludeIsOneHeaderPath(t *testing.T) {
	t.Parallel()
	refused := []struct{ name, value string }{
		{"carriage return", "core\rvector.h"},
		{"line feed", "core\nvector.h"},
		{"tab", "core\tvector.h"},
		{"double quote", `core"vector.h`},
		{"backslash", `core\vector.h`},
		{"empty", ""},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			errs := cppIncludeProbe(t, tc.value)
			for _, e := range errs {
				if strings.Contains(e.Error(), "cpp_include") {
					return
				}
			}
			t.Errorf("cpp_include %q was not refused by name; got %v", tc.value, errs)
		})
	}
	accepted := []struct{ name, value string }{
		{"plain", "core_vector.h"},
		{"separators", "lib/core-vector_2.h"},
		{"parent", "../shared/vec.h"},
		{"absolute", "/abs/path/vec.h"},
	}
	for _, tc := range accepted {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if errs := cppIncludeProbe(t, tc.value); len(errs) > 0 {
				t.Errorf("cpp_include %q is one header path yet was refused: %v", tc.value, errs)
			}
		})
	}
}
