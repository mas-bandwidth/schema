package tablewire_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestEngineHasNoTolerantPaths(t *testing.T) {
	t.Parallel()
	retired := map[string]bool{"textBoundary": true, "wideBoundary": true, "KeepTextTotal": true, "clampFloat": true, "Clamped": true, "KindMismatch": true, "Duplicate": true}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(source, func(node ast.Node) bool {
			if ident, ok := node.(*ast.Ident); ok && retired[ident.Name] {
				t.Errorf("%s retains tolerant identifier %s", name, ident.Name)
			}
			return true
		})
	}
	m := blobCatalogModel(t)
	loaded := m.New(m.Lookup("Catalog"))
	wire, err := os.ReadFile("../../testdata/wire/tables/blob_shared.bin")
	if err != nil {
		t.Fatal(err)
	}
	var report tabletext.Report
	ok, err := tablewire.Decode(m, loaded, wire, &report)
	if ok || err == nil {
		t.Fatalf("hostile shared-blob input returned ok=%v err=%v; want refusal", ok, err)
	}
	for _, key := range []string{"thumb", "alias"} {
		if blobCatalogField(t, loaded, key).Cell.Blob != nil {
			t.Errorf("refusal retained partial value in %s", key)
		}
	}
}
