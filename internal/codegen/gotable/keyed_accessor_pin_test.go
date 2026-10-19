package gotable

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// TestTableKeyedEmitsBothBounds checks the emitted accessor's structure.
// Runtime behavior is exercised in test/go-tables/keyed_test.go; this structural
// witness is not a substitute for that runtime pin.
func TestTableKeyedEmitsBothBounds(t *testing.T) {
	t.Parallel()
	if err := checkTableKeyedSource(tableKeyedAccessor); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]string{
		"no lower bound": strings.Replace(tableKeyedAccessor, "key <= 0", "key < 0", 1),
		"no upper bound": strings.Replace(tableKeyedAccessor, "key > len(slots)", "key < len(slots)", 1),
		"wrong shift":    strings.Replace(tableKeyedAccessor, "slots[key-1]", "slots[key]", 1),
	} {
		if err := checkTableKeyedSource(bad); err == nil {
			t.Errorf("%s survived the accessor pin", name)
		}
	}
}

func checkTableKeyedSource(source string) error {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "keyed.go", "package probe\n"+source, 0)
	if err != nil {
		return err
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "TableKeyed" {
			continue
		}
		if len(fn.Body.List) != 2 {
			return fmt.Errorf("TableKeyed has %d statements, want guard and slot return", len(fn.Body.List))
		}
		guard, ok := fn.Body.List[0].(*ast.IfStmt)
		if !ok || compact(expression(fset, guard.Cond)) != "key<=0||key>len(slots)" {
			return fmt.Errorf("TableKeyed does not guard both invalid key ranges")
		}
		if len(guard.Body.List) != 1 || !strings.HasPrefix(expression(fset, guard.Body.List[0]), "panic(") {
			return fmt.Errorf("TableKeyed guard does not refuse by panic")
		}
		ret, ok := fn.Body.List[1].(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 || compact(expression(fset, ret.Results[0])) != "&slots[key-1]" {
			return fmt.Errorf("TableKeyed does not return the named slot at key minus one")
		}
		return nil
	}
	return fmt.Errorf("generated TableKeyed function missing")
}

func expression(fset *token.FileSet, node ast.Node) string {
	var out bytes.Buffer
	if err := format.Node(&out, fset, node); err != nil {
		return ""
	}
	return out.String()
}

func compact(s string) string { return strings.Join(strings.Fields(s), "") }
