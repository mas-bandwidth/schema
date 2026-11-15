package gotable

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/goname"
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

	// each unit both ways a unit's runtime can live: emitted into it, and
	// imported from the shared package with the unit naming it through shims
	type runtimeCase struct {
		name    string
		unit    *ir.Unit
		runtime Runtime
	}
	var cases []runtimeCase
	for _, test := range units {
		cases = append(cases,
			runtimeCase{name: test.name + "/standalone", unit: test.unit, runtime: RuntimeStandalone},
			runtimeCase{name: test.name + "/shared", unit: test.unit, runtime: RuntimeShared})
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			files, err := GenerateRuntime(test.unit, nil, test.runtime)
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
			parsed := make(map[string]*ast.File, len(names))
			declarations := map[string][]*ast.Ident{}
			methods := map[string]bool{}
			var blankShims []string
			for _, name := range names {
				file, err := parser.ParseFile(fset, name, files[name], 0)
				if err != nil {
					t.Fatalf("parse %s: %v", name, err)
				}
				parsed[name] = file
				for _, decl := range file.Decls {
					if shim := blankRuntimeShim(decl); shim != "" {
						blankShims = append(blankShims, fmt.Sprintf("%s (%s)", shim, fset.Position(decl.Pos())))
					}
					switch decl := decl.(type) {
					case *ast.FuncDecl:
						if decl.Recv != nil {
							methods[decl.Name.Name] = true
						}
						addDeclaration(declarations, decl.Name)
					case *ast.GenDecl:
						for _, spec := range decl.Specs {
							switch spec := spec.(type) {
							case *ast.TypeSpec:
								addDeclaration(declarations, spec.Name)
							case *ast.ValueSpec:
								for _, name := range spec.Names {
									addDeclaration(declarations, name)
								}
							}
						}
					}
				}
			}

			roots := map[string]bool{"init": true}
			for name := range declarations {
				if token.IsExported(name) {
					roots[name] = true
				}
			}
			for name := range api {
				roots[name] = true
			}
			referenced := reachableFromRoots(names, parsed, declarations, methods, roots)

			var unused []string
			for name := range declarations {
				if name == "init" || !isPrivate(name) || referenced[name] {
					continue
				}
				unused = append(unused, name)
			}
			sort.Strings(unused)
			if len(unused) != 0 {
				t.Errorf("%s has unexported declarations unreachable from the unit's API: %s", test.name, strings.Join(unused, ", "))
			}
			if len(blankShims) != 0 {
				t.Errorf("%s carries shims it does not use, as blank constants: %s", test.name, strings.Join(blankShims, ", "))
			}
		})
	}
}

// reachableFromRoots keeps the declarations a unit's API can reach: an
// exported top-level name, an init function, or a schema-rooted entry point,
// followed through every name those declarations use. A private cluster that
// only calls itself does not stay.
func reachableFromRoots(names []string, parsed map[string]*ast.File, declarations map[string][]*ast.Ident, methods map[string]bool, roots map[string]bool) map[string]bool {
	declarationIDs := map[*ast.Ident]bool{}
	for _, ids := range declarations {
		for _, id := range ids {
			declarationIDs[id] = true
		}
	}
	edges := map[string]map[string]bool{}
	addEdge := func(from, to string) {
		if edges[from] == nil {
			edges[from] = map[string]bool{}
		}
		edges[from][to] = true
	}
	for _, name := range names {
		file := parsed[name]
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
				for _, id := range node.Names {
					nonReferences[id] = true
				}
			case *ast.ImportSpec:
				if node.Name != nil {
					nonReferences[node.Name] = true
				}
			}
			return true
		})
		collect := func(from string, nodes ...ast.Node) {
			for _, node := range nodes {
				if node == nil || reflect.ValueOf(node).IsNil() {
					continue
				}
				ast.Inspect(node, func(node ast.Node) bool {
					switch node := node.(type) {
					case *ast.SelectorExpr:
						if methods[node.Sel.Name] {
							addEdge(from, node.Sel.Name)
						}
						return true
					}
					id, ok := node.(*ast.Ident)
					if !ok || nonReferences[id] || declarationIDs[id] || declarations[id.Name] == nil {
						return true
					}
					addEdge(from, id.Name)
					return true
				})
			}
		}
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				collect(decl.Name.Name, decl.Recv, decl.Type, decl.Body)
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						collect(spec.Name.Name, spec.Type)
					case *ast.ValueSpec:
						if len(spec.Values) == len(spec.Names) {
							for i, id := range spec.Names {
								collect(id.Name, spec.Type, spec.Values[i])
							}
						} else {
							for _, id := range spec.Names {
								collect(id.Name, spec.Type)
								for _, value := range spec.Values {
									collect(id.Name, value)
								}
							}
						}
					}
				}
			}
		}
	}
	referenced := map[string]bool{}
	var queue []string
	for name := range roots {
		if referenced[name] {
			continue
		}
		referenced[name] = true
		queue = append(queue, name)
	}
	for len(queue) != 0 {
		name := queue[0]
		queue = queue[1:]
		for ref := range edges[name] {
			if referenced[ref] {
				continue
			}
			referenced[ref] = true
			queue = append(queue, ref)
		}
	}
	return referenced
}

// blankRuntimeShim names a const declaration that binds only the blank name
// to a bare runtime name: a shim the unit does not use, left in as a blank
// rather than left out. A blank constant over any other expression is a
// compile-time check and is not a shim.
func blankRuntimeShim(decl ast.Decl) string {
	gen, ok := decl.(*ast.GenDecl)
	if !ok || gen.Tok != token.CONST {
		return ""
	}
	for _, spec := range gen.Specs {
		value, ok := spec.(*ast.ValueSpec)
		if !ok || len(value.Names) != 1 || value.Names[0].Name != "_" || len(value.Values) != 1 {
			continue
		}
		if sel, ok := value.Values[0].(*ast.SelectorExpr); ok {
			if pkg, ok := sel.X.(*ast.Ident); ok {
				return pkg.Name + "." + sel.Sel.Name
			}
		}
	}
	return ""
}

func TestGoPreservesLowercaseTableEntryPoints(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package probe
table Element { value int32 }
table lower {
    items []int32
    object_id []int32
    roster map[uint8]Element
    bounded [..8]string(8)
    words []string(8)
    maybe []?string(8)
}
`)
	surfaces := DefaultSurfaces()
	surfaces.TextForm = true
	assertCompiles(t, u, surfaces, `package probe
import "testing"
func TestLowercaseEntryPoints(t *testing.T) {
    t.Parallel()
    _ = lowerReset
    _ = lowerTableType
    _ = lowerMeasure
    _ = lowerSave
    _ = lowerLoad
    _ = lowerItemsAdd
    _ = lowerItemsErase
    _ = lowerItemsEach
    _ = lowerObjectIDAdd
    _ = lowerObjectIDErase
    _ = lowerObjectIDEach
    _ = lowerBoundedTextAt
    _ = lowerBoundedAppend
    _ = lowerWordsTextAt
    _ = lowerWordsAppend
    _ = lowerMaybeTextAt
    _ = lowerMaybeAppend
    _ = lowerMaybeAppendAbsent
    _ = lowerRosterInsert
    _ = lowerRosterFind
    _ = lowerRosterErase
    _ = lowerRosterEach
    _ = lowerRosterIndexMeasure
    _ = lowerRosterIndex
    _ = lowerRosterIndexFind
    _ = lowerLoadTextForm
    _ = lowerSaveTextForm
    _ = lowerEditTextForm
    _ = lowerOpen
    _ = lowerAt
}
`)
	t.Run("block lowercase root", func(t *testing.T) {
		t.Parallel()
		u := unitFrom(t, `package probe
fixed table blocklower | forms = "wire,block" {
    values [4]int32
}
`)
		assertCompiles(t, u, DefaultSurfaces(), `package probe
import "testing"
func TestBlockEntryPoints(t *testing.T) {
    t.Parallel()
    _ = blocklowerBlockOpen
    _ = blocklowerBlockBegin
    _ = blocklowerBlockBytes
}
`)
	})
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
			body := "table Element { value int32 }\ntable %s | forms = \"wire,json,cook,block\" {\nitems []int32\nobject_id []int32\nroster map[uint8]Element\nbounded [..8]string(8)\nwords []string(8)\nmaybe []?string(8)\n}\n"
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
			rootPairs := [][2]string{{tc.upperGoRoot, tc.privateGoRoot}}
			upperTable := upper.Tables[tc.upperSchemaRoot]
			privateTable := private.Tables[tc.privateSchemaRoot]
			if upperTable == nil || privateTable == nil || len(upperTable.Fields) != len(privateTable.Fields) {
				t.Fatal("paired schemas have different table fields")
			}
			for i, field := range upperTable.Fields {
				upperFieldRoot := goname.Initialisms(tc.upperGoRoot + ir.GoExportName(field.Name))
				privateFieldRoot := goname.Initialisms(tc.privateGoRoot + ir.GoExportName(privateTable.Fields[i].Name))
				rootPairs = append(rootPairs, [2]string{upperFieldRoot, privateFieldRoot})
			}
			upperSynthetic := syntheticTableRoots(upper)
			privateSynthetic := syntheticTableRoots(private)
			upperDeclarations := rootedTopLevelDeclarations(t, want, rootPairs, upperSynthetic)
			privateDeclarations := rootedTopLevelDeclarations(t, got, [][2]string{{tc.privateGoRoot, tc.privateGoRoot}}, privateSynthetic)
			var missing []string
			for key, name := range upperDeclarations {
				if _, ok := privateDeclarations[key]; !ok {
					missing = append(missing, name)
				}
			}
			sort.Strings(missing)
			if len(missing) != 0 {
				t.Fatalf("private spelling dropped schema-rooted API declarations: %s", strings.Join(missing, ", "))
			}
		})
	}
	t.Run("fixed lowercase root", func(t *testing.T) {
		t.Parallel()
		upper := unitFrom(t, "package probe\nfixed table FixedLower { value int32 }\n")
		private := unitFrom(t, "package probe\nfixed table fixedlower { value int32 }\n")
		want, err := Generate(upper)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Generate(private)
		if err != nil {
			t.Fatal(err)
		}
		upperDeclarations := rootedTopLevelDeclarations(t, want, [][2]string{{"FixedLower", "fixedlower"}}, syntheticTableRoots(upper))
		privateDeclarations := rootedTopLevelDeclarations(t, got, [][2]string{{"fixedlower", "fixedlower"}}, syntheticTableRoots(private))
		var missing []string
		for key, name := range upperDeclarations {
			if _, ok := privateDeclarations[key]; !ok {
				missing = append(missing, name)
			}
		}
		sort.Strings(missing)
		if len(missing) != 0 {
			t.Fatalf("fixed table private spelling dropped declarations: %s", strings.Join(missing, ", "))
		}
	})
}

func syntheticTableRoots(u *ir.Unit) []string {
	var roots []string
	for _, st := range goEscapeTables(u).Tables {
		if st.MapEntryOf != "" || st.TextElem {
			roots = append(roots, goname.Initialisms(st.Name))
		}
	}
	return roots
}

func rootedTopLevelDeclarations(t *testing.T, files map[string][]byte, roots [][2]string, synthetic []string) map[string]string {
	t.Helper()
	declarations := map[string]string{}
	for _, src := range files {
		file, err := parser.ParseFile(token.NewFileSet(), "generated.go", src, 0)
		if err != nil {
			t.Fatalf("parse generated output: %v", err)
		}
		add := func(kind, name string) {
			for _, root := range synthetic {
				if strings.HasPrefix(name, root) {
					return
				}
			}
			for _, pair := range roots {
				from, to := pair[0], pair[1]
				if !strings.HasPrefix(name, from) {
					continue
				}
				if kind == "func" && strings.HasSuffix(name, "MeasureBodyRetain") {
					// retain.go emits this recursive retained-child measurement
					// worker; the table's retain entry points are checked below.
					return
				}
				target := to + strings.TrimPrefix(name, from)
				key := kind + ":" + target
				declarations[key] = target
				return
			}
		}
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				if decl.Recv == nil {
					add("func", decl.Name.Name)
				}
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						add("type", spec.Name.Name)
					case *ast.ValueSpec:
						kind := "var"
						if decl.Tok == token.CONST {
							kind = "const"
						}
						for _, name := range spec.Names {
							add(kind, name.Name)
						}
					}
				}
			}
		}
	}
	return declarations
}
