package baseline_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestForeignBaselineRefusalNamesUpdateAndUpdateSalvages(t *testing.T) {
	t.Parallel()
	const foreignSrc = `package elsewhere
fixed table Other { value int32 = 1 }
`
	const ownSrc = `package fixture
fixed table Own { value int32 = 2 }
`
	makeUnit := func(src string) *ir.Unit {
		t.Helper()
		ast, errs := parser.Parse("Fixture.schema", []byte(src))
		if len(errs) != 0 {
			t.Fatalf("parse: %v", errs[0])
		}
		u, errs := check.Unit([]check.SourceFile{{Path: "Fixture.schema", Name: "Fixture.schema", Base: "Fixture", Bytes: []byte(src), AST: ast}})
		if len(errs) != 0 {
			t.Fatalf("check: %v", errs[0])
		}
		return u
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "Fixture.schema")
	if err := os.WriteFile(source, []byte(ownSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, baseline.FileName)
	foreign := baseline.Render(makeUnit(foreignSrc))
	foreign.History = []string{baseline.HistoryHeading, "### 2026-01-01 (UTC) — previous unit", "- retained foreign history"}
	if err := os.WriteFile(path, []byte(foreign.Text()), 0o644); err != nil {
		t.Fatal(err)
	}
	own := makeUnit(ownSrc)
	_, errs := baseline.Check(own, []string{source})
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "--update") {
		t.Fatalf("Check must refuse and name --update, got %v", errs)
	}
	if _, rewrote, err := baseline.UpdateAt(own, []string{source}, "repair foreign baseline", "1000.0"); err != nil || !rewrote {
		t.Fatalf("Update foreign baseline: rewrote=%t err=%v", rewrote, err)
	}
	gotBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := baseline.Parse(path, gotBytes)
	if err != nil {
		t.Fatal(err)
	}
	if got.Package != own.Package {
		t.Fatalf("package = %q, want %q", got.Package, own.Package)
	}
	text := string(gotBytes)
	for _, want := range []string{"retained foreign history", "repair foreign baseline", "previous projection could not be diffed"} {
		if !strings.Contains(text, want) {
			t.Errorf("salvaged baseline missing %q:\n%s", want, text)
		}
	}
	if _, errs := baseline.Check(own, []string{source}); len(errs) != 0 {
		t.Fatalf("Check after salvage: %v", errs)
	}
}
