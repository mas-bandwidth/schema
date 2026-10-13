package cstable

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

// TestGenerateCsUnionsDoesNotPanic pins the v1.3 blocker: CsUnions.schema
// declares two payload-free union arms (`empty` in Leaf, `signal` in Choice),
// and the IR gives a payload-free arm a nil field. The C# fixed-form type walk
// must check v.F before reading v.F.Type, exactly as the C++ emitter does
// (internal/codegen/cpptable/fixedform.go:280-282) — a payload-free arm names no
// type (docs/SPEC-TABLES.md §2.6), so the arm's tag is its whole storage.
func TestGenerateCsUnionsDoesNotPanic(t *testing.T) {
	t.Parallel()
	path := filepath.Join("..", "..", "..", "test", "tables", "CsUnions.schema")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	f, perrs := parser.Parse("CsUnions.schema", src)
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "CsUnions.schema", Name: "CsUnions.schema", Base: "CsUnions", Bytes: src, AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	body, ok := files["CsUnionsTable.cs"]
	if !ok {
		names := make([]string, 0, len(files))
		for name := range files {
			names = append(names, name)
		}
		sort.Strings(names)
		t.Fatalf("no CsUnionsTable.cs emitted; got %v", names)
	}
	if len(body) == 0 {
		t.Fatal("CsUnionsTable.cs is empty")
	}
	if !strings.Contains(string(body), "RootFixedWriteBody") {
		t.Errorf("CsUnionsTable.cs holds no fixed write body for Root")
	}
}
