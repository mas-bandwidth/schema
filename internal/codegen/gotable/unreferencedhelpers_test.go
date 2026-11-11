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
		{name: "lowercase-table-api", unit: unitFrom(t, "package probe\ntable lowercase { value int32 }\n")},
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
			api := schemaAPIEntries(goEscapeTables(test.unit), files)

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
							if !api[decl.Name.Name] {
								addUnexportedDeclaration(declarations, decl.Name)
							}
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

func TestGoPreservesLowercaseTableEntryPoints(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package probe
table Element { value int32 }
table lower {
    values []int32
    roster map[uint8]Element
}
`)
	surfaces := DefaultSurfaces()
	surfaces.TextForm = true
	assertCompiles(t, u, surfaces, `package probe
import "testing"
func TestLowercaseEntryPoints(t *testing.T) {
    _ = testing.Short()
    if lowerReset == nil || lowerTableType == nil || lowerMeasure == nil || lowerSave == nil || lowerLoad == nil { t.Fatal("table entry point missing") }
    if lowerValuesAdd == nil || lowerValuesErase == nil || lowerValuesEach == nil { t.Fatal("list entry point missing") }
    if lowerRosterInsert == nil || lowerRosterFind == nil || lowerRosterErase == nil || lowerRosterEach == nil || lowerRosterIndexMeasure == nil || lowerRosterIndex == nil || lowerRosterIndexFind == nil { t.Fatal("map entry point missing") }
    if lowerLoadTextForm == nil || lowerSaveTextForm == nil || lowerEditTextForm == nil { t.Fatal("textform entry point missing") }
    if lowerOpen == nil || lowerAt == nil { t.Fatal("cook entry point missing") }
}
`)
}

func TestGoPreservesSchemaRootedAPIWithPrivateSpellings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, upperSchemaRoot, privateSchemaRoot, upperGoRoot, privateGoRoot string
	}{
		{name: "lowercase", upperSchemaRoot: "Lower", privateSchemaRoot: "lower", upperGoRoot: "Lower", privateGoRoot: "lower"},
		{name: "escaped-keyword", upperSchemaRoot: "Function", privateSchemaRoot: "func", upperGoRoot: "Function", privateGoRoot: "func_"},
		{name: "initialism", upperSchemaRoot: "LowerId", privateSchemaRoot: "lowerId", upperGoRoot: "LowerID", privateGoRoot: "lowerID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := "table Element { value int32 }\ntable %s {\nitems []int32\nroster map[uint8]Element\n}\n"
			upper := unitFrom(t, "package probe\n"+strings.Replace(body, "%s", tc.upperSchemaRoot, 1))
			private := unitFrom(t, "package probe\n"+strings.Replace(body, "%s", tc.privateSchemaRoot, 1))
			surfaces := DefaultSurfaces()
			surfaces.TextForm = true
			want, err := GenerateLineageSurface(upper, nil, surfaces)
			if err != nil {
				t.Fatalf("generate uppercase-root unit: %v", err)
			}
			got, err := GenerateLineageSurface(private, nil, surfaces)
			if err != nil {
				t.Fatalf("generate private-root unit: %v", err)
			}
			upperPresent := map[string]bool{}
			for _, src := range want {
				file, err := parser.ParseFile(token.NewFileSet(), "generated.go", src, 0)
				if err != nil {
					t.Fatalf("parse uppercase-root output: %v", err)
				}
				for _, decl := range file.Decls {
					if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil {
						upperPresent["func:"+fn.Name.Name] = true
					}
				}
			}
			privatePresent := map[string]bool{}
			for _, src := range got {
				file, err := parser.ParseFile(token.NewFileSet(), "generated.go", src, 0)
				if err != nil {
					t.Fatalf("parse private-root output: %v", err)
				}
				for _, decl := range file.Decls {
					switch decl := decl.(type) {
					case *ast.FuncDecl:
						if decl.Recv == nil {
							privatePresent["func:"+decl.Name.Name] = true
						}
					case *ast.GenDecl:
						for _, spec := range decl.Specs {
							switch spec := spec.(type) {
							case *ast.TypeSpec:
								privatePresent["type:"+spec.Name.Name] = true
							case *ast.ValueSpec:
								for _, name := range spec.Names {
									privatePresent["value:"+name.Name] = true
								}
							}
						}
					}
				}
			}
			apiSuffixes := []string{
				"Reset", "TableType", "Measure", "Save", "Load", "Open", "At",
				"LoadTextForm", "SaveTextForm", "EditTextForm",
				"ItemsAdd", "ItemsErase", "ItemsEach",
				"RosterInsert", "RosterFind", "RosterErase", "RosterEach",
				"RosterIndexMeasure", "RosterIndex", "RosterIndexFind",
			}
			var missing []string
			for _, suffix := range apiSuffixes {
				from := tc.upperGoRoot + suffix
				to := tc.privateGoRoot + suffix
				if !upperPresent["func:"+from] {
					missing = append(missing, "uppercase fixture missing "+from)
				}
				if !privatePresent["func:"+to] {
					missing = append(missing, to)
				}
			}
			sort.Strings(missing)
			if len(missing) != 0 {
				t.Fatalf("private spelling dropped schema-rooted API declarations: %s", strings.Join(missing, ", "))
			}
		})
	}
}
