// SPEC §4.6 refuses Rust keywords as schema basenames for every target.
// Rust also defends its own module names when given an already built IR.
package compiler

import (
	"strings"
	"testing"
)

func TestRustRefusesAKeywordModuleName(t *testing.T) {
	t.Parallel()
	const src = "package p\ntype Ship { hp int32 }\n"
	for _, base := range []string{"type", "Type", "Box", "Match"} {
		if _, err := loadBaseErr(t, base, src); err == nil ||
			!strings.Contains(err.Error(), base+".schema") ||
			!strings.Contains(err.Error(), "reserved word in ") || !strings.Contains(err.Error(), "Rust") {
			t.Errorf("%s.schema: want the checker's module-name refusal naming Rust, got %v", base, err)
		}
		_, err := New().Generate(unitRebasedTo(t, base, src), "rust", Options{})
		if err == nil || !strings.Contains(err.Error(), "reserved word in Rust") {
			t.Errorf("%s.schema: want the Rust module-name refusal, got %v", base, err)
		}
	}
	files, err := New().Generate(loadBase(t, "Ships", src), "rust", Options{})
	if err != nil {
		t.Fatalf("Ships.schema: %v", err)
	}
	if !strings.Contains(string(files["lib.rs"]), "mod ships;") {
		t.Errorf("Ships.schema: lib.rs lacks mod ships;")
	}
}
