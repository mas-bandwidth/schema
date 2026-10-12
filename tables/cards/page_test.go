package cards

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The supported customer surface, named explicitly. Adding a supported facade
// call means adding it here and documenting it in docs/CARDS.md; this list does
// NOT claim automatic coverage of every generated symbol (docs/CARDS.md).
var facadeNames = []string{
	"Record", "Report",
	"MaxBodyBytes", "MaxListEntries",
	"Load", "FromJSON", "ToJSON",
	"Parse", "Render", "Upgrade", "ReadStrict",
	"SetNeeds", "SetPaths", "SetTier", "SetPriority", "SetColumn", "SetHolder",
	"TaskHash",
}

// facadeMethods is the explicit method surface; Record.Wire returns a defensive
// copy of the owned bytes (docs/CARDS.md).
var facadeMethods = []struct{ recv, name string }{
	{"Record", "Wire"},
}

// facadeSources are the named handwritten files that own the facade. Only these
// are scanned; the generated module is not.
var facadeSources = []string{"cards.go", "parse.go", "render.go", "edit.go", "upgrade.go"}

// TestCardsPageNamesFacade holds docs/CARDS.md against the settled facade: each
// documented name is an exported declaration in a named handwritten source file,
// and the documented declaration block matches Card.schema byte for byte. The
// inputs are passed from the make wiring; missing inputs fail rather than walk
// ancestors.
func TestCardsPageNamesFacade(t *testing.T) {
	t.Parallel()

	doc := mustReadEnv(t, "CARDS_DOC_PATH")
	decl := mustReadEnv(t, "CARDS_DECL_PATH")
	root := os.Getenv("CARDS_SOURCE_ROOT")
	if root == "" {
		t.Fatal("CARDS_SOURCE_ROOT is not set")
	}
	src := filepath.Join(root, "tables", "cards")

	declared, methods := parseFacade(t, src)
	for _, name := range facadeNames {
		if !declared[name] {
			t.Errorf("docs/CARDS.md names %q but no named handwritten source declares it", name)
		}
	}
	for _, m := range facadeMethods {
		if !methods[m.recv+"."+m.name] {
			t.Errorf("docs/CARDS.md names %s.%s but no named handwritten source declares it", m.recv, m.name)
		}
	}
	for _, name := range facadeNames {
		if !strings.Contains(doc, name) {
			t.Errorf("facade %q has no entry in docs/CARDS.md", name)
		}
	}
	for _, m := range facadeMethods {
		if !strings.Contains(doc, m.name) {
			t.Errorf("facade method %q has no entry in docs/CARDS.md", m.name)
		}
	}

	block := fencedBlock(doc, "// Card.schema")
	if block == "" {
		t.Fatal("docs/CARDS.md has no fenced declaration block")
	}
	if strings.TrimRight(block, "\n") != strings.TrimRight(decl, "\n") {
		t.Error("the declaration block in docs/CARDS.md does not match Card.schema byte for byte")
	}
}

func mustReadEnv(t *testing.T, key string) string {
	t.Helper()
	path := os.Getenv(key)
	if path == "" {
		t.Fatalf("%s is not set", key)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s=%s: %v", key, path, err)
	}
	return string(b)
}

// parseFacade collects exported top-level declarations and exported methods on
// Record from the named source files, without reading the generated module.
func parseFacade(t *testing.T, dir string) (map[string]bool, map[string]bool) {
	t.Helper()
	names := map[string]bool{}
	methods := map[string]bool{}
	fset := token.NewFileSet()
	for _, file := range facadeSources {
		path := filepath.Join(dir, file)
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					if d.Name.IsExported() {
						names[d.Name.Name] = true
					}
					continue
				}
				if star, ok := d.Recv.List[0].Type.(*ast.StarExpr); ok {
					if id, ok := star.X.(*ast.Ident); ok && id.Name == "Record" {
						methods["Record."+d.Name.Name] = true
					}
				}
			case *ast.GenDecl:
				for _, s := range d.Specs {
					switch s := s.(type) {
					case *ast.TypeSpec:
						if s.Name.IsExported() {
							names[s.Name.Name] = true
						}
					case *ast.ValueSpec:
						for _, n := range s.Names {
							if n.IsExported() {
								names[n.Name] = true
							}
						}
					}
				}
			}
		}
	}
	return names, methods
}

// fencedBlock returns the body of the first ``` fence, dropping an optional
// language tag on its first line, whose body starts with prefix.
func fencedBlock(doc, prefix string) string {
	parts := strings.Split(doc, "```")
	for i := 1; i < len(parts); i += 2 {
		body := parts[i]
		if j := strings.IndexByte(body, '\n'); j >= 0 {
			body = body[j+1:]
		}
		if strings.HasPrefix(body, prefix) {
			return body
		}
	}
	return ""
}
