package gotable

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestGoUnitHasNoUnreferencedHelpers(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "..")
	units := []struct {
		name string
		unit *ir.Unit
	}{
		{name: "examples", unit: loadUnitPaths(t, root, []string{"tables/examples"})},
		{name: "pointers-maps-blobs", unit: unitFrom(t, `package probe
table Item { value int32 }
table Holder {
    item *Item
    items map[uint8]Item
    blob *bytes
    payload bytes(8)
		}`)},
		{name: "messages", unit: loadUnitPaths(t, root, []string{"tables/messages"})},
		{name: "fixed-only", unit: loadUnitPaths(t, root, []string{"tables/examples/Pack.schema"})},
	}

	for _, test := range units {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			files, err := Generate(test.unit)
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			var names []string
			for name := range files {
				if strings.HasSuffix(name, ".go") {
					names = append(names, name)
				}
			}
			sort.Strings(names)
			if len(names) == 0 {
				t.Fatal("Generate emitted no Go source")
			}

			fset := token.NewFileSet()
			parsed := make([]*ast.File, 0, len(names))
			declarations := map[string]*ast.Ident{}
			for _, name := range names {
				file, err := parser.ParseFile(fset, name, files[name], 0)
				if err != nil {
					t.Fatalf("parse %s: %v", name, err)
				}
				parsed = append(parsed, file)
				for _, decl := range file.Decls {
					switch decl := decl.(type) {
					case *ast.FuncDecl:
						if decl.Recv == nil && decl.Name.Name != "init" {
							addUnexportedDeclaration(declarations, decl.Name)
						}
					case *ast.GenDecl:
						for _, spec := range decl.Specs {
							switch spec := spec.(type) {
							case *ast.TypeSpec:
								addUnexportedDeclaration(declarations, spec.Name)
							case *ast.ValueSpec:
								for _, name := range spec.Names {
									addUnexportedDeclaration(declarations, name)
								}
							}
						}
					}
				}
			}

			referenced := map[string]bool{}
			for _, file := range parsed {
				nonReferences := map[*ast.Ident]bool{}
				ast.Inspect(file, func(node ast.Node) bool {
					switch node := node.(type) {
					case *ast.SelectorExpr:
						nonReferences[node.Sel] = true
					case *ast.KeyValueExpr:
						if key, ok := node.Key.(*ast.Ident); ok {
							nonReferences[key] = true
						}
					case *ast.Field:
						for _, name := range node.Names {
							nonReferences[name] = true
						}
					}
					return true
				})
				ast.Inspect(file, func(node ast.Node) bool {
					id, ok := node.(*ast.Ident)
					if !ok || declarations[id.Name] == nil || id == declarations[id.Name] {
						return true
					}
					if nonReferences[id] {
						return true
					}
					referenced[id.Name] = true
					return true
				})
			}

			var unused []string
			for name := range declarations {
				if !referenced[name] {
					unused = append(unused, name)
				}
			}
			sort.Strings(unused)
			if len(unused) != 0 {
				t.Errorf("%s has unreferenced unexported top-level declarations: %s", test.name, strings.Join(unused, ", "))
			}
		})
	}
}

func addUnexportedDeclaration(dst map[string]*ast.Ident, name *ast.Ident) {
	if name != nil && name.Name != "_" && !token.IsExported(name.Name) {
		dst[name.Name] = name
	}
}
