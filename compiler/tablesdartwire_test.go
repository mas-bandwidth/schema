// The Dart table-wire tests, in the language's own file
// (docs/CONTRIBUTING.md, "Adding a language"): schema#514.
package compiler

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestDartEmitsIdTableWire is schema#514's red-first gate: the Dart target
// emits the ID-TABLE WIRE (docs/SPEC-TABLES.md §3) — the form byte read FIRST,
// sixty-four-bit identity for every vocabulary, canonical LEB128, and the
// trailing id table a reader finds from the END.
//
// The identity is asserted against ir.TableWireId, the target-independent hash
// the compiler's own engine and the C++ reference share, so a Dart emitter
// that folds or rebinds the id fails here on a byte and not on a comment.
func TestDartEmitsIdTableWire(t *testing.T) {
	files, err := New().Generate(unitFromSource(t, tableSrc), "dart", Options{})
	if err != nil {
		t.Fatalf("--lang dart: %v", err)
	}
	wire, ok := files["ProbeTable.dart"]
	if !ok {
		t.Fatalf("--lang dart emitted no ProbeTable.dart for a unit with tables; got %d files", len(files))
	}
	text := string(wire)

	for _, want := range []string{
		"const int tableForm = 1;",
		"const int tableReservedId = 0xFFFFFFFFFFFFFFFF;",
		"final class TableWireFieldInfo",
		"final class TableWireInfo",
		"final class TableIds",
		"int tableFnv1a64(String name)",
		"int tableLebBytes(int value)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("ProbeTable.dart is missing %q — the id-table wire's %s", want, want)
		}
	}

	// Identity at sixty-four bits, with no fold and no rebound (§5): the
	// table's own name id and every field's id are ir.TableWireId's answer.
	ids := map[string]uint64{
		"Config": ir.TableWireId("Config"),
		"scale":  ir.TableWireId("scale"),
		"label":  ir.TableWireId("label"),
		"grade":  ir.TableWireId("grade"),
		"points": ir.TableWireId("points"),
	}
	for name, id := range ids {
		want := fmt.Sprintf("0x%016X", id)
		if !strings.Contains(text, want) {
			t.Errorf("ProbeTable.dart is missing %s's sixty-four-bit id %s — got no %s",
				name, want, want)
		}
	}
}
