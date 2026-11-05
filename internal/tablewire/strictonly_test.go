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
	writer := listModel(t, "package strictpartial\ntable Partial { a int32\n b int32 }")
	reader := listModel(t, "package strictpartial\ntable Partial { a int32\n b int32 | min = 0, max = 1 }")
	valid := place(t, writer, "Partial", `{"a":7,"b":3}`)
	wire, err = tablewire.Encode(writer, valid)
	if err != nil {
		t.Fatal(err)
	}
	partial := reader.New(reader.Lookup("Partial"))
	report = tabletext.Report{}
	if ok, err := tablewire.Decode(reader, partial, wire, &report); ok || err == nil {
		t.Fatalf("out-of-range field accepted: ok=%v err=%v", ok, err)
	}
	for _, field := range partial.Fields {
		if field.Cell.I != 0 {
			t.Errorf("refusal retained partial %s=%d", field.Def.Name, field.Cell.I)
		}
	}

}
