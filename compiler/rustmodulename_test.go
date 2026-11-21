// A SCHEMA FILE WHOSE BASENAME LOWERS TO A RUST KEYWORD IS REFUSED BY THE RUST
// TARGET BY NAME (FRONTEND-4, SPEC §4.6): lib.rs declares `mod <base
// lowercased>;`, and `mod type;` or `mod box;` does not parse. Every other
// target generates the same unit, because none emits the basename bare.
package compiler

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func unitNamed(t *testing.T, base, src string) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse(base+".schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: base + ".schema", Name: base + ".schema", Base: base, Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

func TestRustRefusesAKeywordModuleName(t *testing.T) {
	t.Parallel()
	const src = "package p\ntype A { a int32 }\n"
	for _, base := range []string{"type", "Type", "Box", "Match"} {
		_, err := New().Generate(unitNamed(t, base, src), "rust", Options{})
		if err == nil || !strings.Contains(err.Error(), "reserved word in Rust") {
			t.Errorf("%s.schema: want the Rust module-name refusal, got %v", base, err)
		}
		if _, err := New().Generate(unitNamed(t, base, src), "go", Options{}); err != nil {
			t.Errorf("%s.schema: go refused a basename only Rust cannot carry: %v", base, err)
		}
	}
	files, err := New().Generate(unitNamed(t, "Ships", src), "rust", Options{})
	if err != nil {
		t.Fatalf("Ships.schema: %v", err)
	}
	if !strings.Contains(string(files["lib.rs"]), "mod ships;") {
		t.Errorf("Ships.schema: lib.rs lacks mod ships;")
	}
}
