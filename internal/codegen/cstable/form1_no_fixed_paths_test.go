package cstable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

func generateCS(t *testing.T, src string) map[string][]byte {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	out, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return out
}

// THE VARIABLE FORM CARRIES NO FIXED-TABLE PATHS (docs/SPEC-TABLES.md §3).
// Form byte 1 is the variable form. A table whose class is fixed encodes as
// form 3 and never as form 1, so this emitter has no reason to carry a second
// engine for a pointer-free table. What was here and is gone:
//
//   - the TYPED WIRE SURFACE — <X>CollectTyped, <X>BodySizeTyped,
//     <X>WriteBodyTyped and <X>SaveTyped, emitted only when the table was not
//     in ir.VariableTables, and the scalar-leaf / scalar-leaf-array predicates
//     that decided which fields it could specialise, and
//   - the ORDINAL-SLOT ACCELERATOR — Ids.OrdinalSlots, Ids.OrdinalOf,
//     Ids.RefAt, Ids.RefAtMiss and Writer.HeaderAt, plus the compile-time
//     ordinal the generated code passed them. Nothing but the typed surface
//     ever called them.
//
// Every table now rides TableWire.Save and the descriptor walk, which is what
// a variable table always rode and what produces the same bytes.
func TestNoFixedOnlyPathsInThisEmitter(t *testing.T) {
	gone := []string{
		"isCleanScalarLeaf",
		"isScalarLeafStruct",
		"isChildScalarArray",
		"needsFieldsArray",
		"needsElemOffset",
		"isPayloadPrefixed",
		"emitTypedWireSurface",
		"emitCollectTyped",
		"emitBodySizeTyped",
		"emitWriteBodyTyped",
		"emitSaveTyped",
		"knownOrdinal",
		"idOrdinal",
		"OrdinalSlots",
		"OrdinalOf",
		"RefAt",
		"HeaderAt",
		"SaveTyped",
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || name == "form1_no_fixed_paths_test.go" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatal(err)
		}
		for _, symbol := range gone {
			if strings.Contains(string(data), symbol) {
				t.Errorf("%s still names the removed form-1 fixed-table path %q", name, symbol)
			}
		}
	}
}

// A pointer-free table and a pointered one generate the SAME wire surface: one
// Measure and one Save, both through TableWire.Save.
func TestGeneratedCSharpCarriesNoTypedSurface(t *testing.T) {
	for name, data := range generateCS(t, `package probe
table Leaf {
 n int32
 ok bool
}
table Root {
 leaf Leaf
 leaves [..4]Leaf
}
table Pointered {
 target *Leaf
}
`) {
		src := string(data)
		for _, gone := range []string{"SaveTyped", "CollectTyped", "BodySizeTyped", "WriteBodyTyped", "HeaderAt", "RefAt", "OrdinalSlots", "OrdinalOf"} {
			if strings.Contains(src, gone) {
				t.Errorf("%s still carries %q", name, gone)
			}
		}
	}
}
