// A table that declares `forms = "..."` (docs/SPEC-TABLES.md §2.12) gets only
// the C++ surfaces of the forms it names; a form no table in the unit carries
// emits no surface at all. The default — a table with no `forms` declaration —
// is every form, today's behaviour.
//
// The expected answers are the ones the Go oracle pins for the same schema text
// (internal/codegen/gotable/declaredforms_test.go, TestGoTableEmitsOnlyItsDeclaredForms
// and TestSurfaceIntersectsDeclaredForms), transcribed into the C++ probe.
package compiler

import (
	"sort"
	"strings"
	"testing"
)

// cppKeysOf returns the generated file names in sorted order.
func cppKeysOf(files map[string][]byte) []string {
	out := make([]string, 0, len(files))
	for k := range files {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// cppHasFormFile reports whether a generated file set holds a file whose name
// ends with suffix, mirroring gotable's hasFormFile.
func cppHasFormFile(files map[string][]byte, suffix string) bool {
	for name := range files {
		if len(name) >= len(suffix) && name[len(name)-len(suffix):] == suffix {
			return true
		}
	}
	return false
}

// TestCPPTableEmitsOnlyItsDeclaredForms holds the C++ table emitter to §2.12:
// only the surfaces a table declares are emitted, and an undeclared unit emits
// every form (the Go oracle's default and wire-only expectations, transcribed).
func TestCPPTableEmitsOnlyItsDeclaredForms(t *testing.T) {
	t.Parallel()
	defaultSchema := `package probe
table Pong { x int32 }
fixed table Fox { x int32 }
`
	def, err := New().Generate(unitFromSource(t, defaultSchema), "cpp", nil)
	if err != nil {
		t.Fatalf("generate default unit: %v", err)
	}
	if !cppHasFormFile(def, "Table.h") || !cppHasFormFile(def, "Block.h") || !cppHasFormFile(def, "View.h") {
		t.Fatalf("an undeclared unit emits every form — got %v", cppKeysOf(def))
	}

	// A table that declares only wire must not carry the cook, block, message or
	// view surfaces (docs/SPEC-TABLES.md §2.12; Go oracle pins the same shape:
	// gotable's TestGoTableEmitsOnlyItsDeclaredForms forbids the PongBlock /
	// PongCook / PongFromJson / PongToJson members).
	decl, err := New().Generate(unitFromSource(t, "package probe\ntable Pong | forms = \"wire\"\n{\n    x int32\n}\n"), "cpp", nil)
	if err != nil {
		t.Fatalf("generate wire-only unit: %v", err)
	}
	if !cppHasFormFile(decl, "Table.h") {
		t.Fatalf("the wire form is missing — got %v", cppKeysOf(decl))
	}
	for name, data := range decl {
		src := string(data)
		for _, forbidden := range []string{
			"PongBlock", "PongBlockOpen",
			"PongCook", "PongOpen", "PongAt",
			"PongFromJson", "PongToJson",
		} {
			if strings.Contains(src, forbidden) {
				t.Errorf("%s leaked omitted form member %q although Pong declares only wire", name, forbidden)
			}
		}
	}

	// Multi-table unit: Pong and FoxWire declare only wire; Ping and FoxAll are
	// undeclared (all forms). This is the Go oracle's mixed case
	// (internal/codegen/gotable/declaredforms_test.go,
	// TestGoTableEmitsOnlyItsDeclaredForms), transcribed to the C++ spellings.
	mixed, err := New().Generate(unitFromSource(t, `package probe
table Pong | forms = "wire"
{
    x int32
}
table Ping
{
    x int32
}
fixed table FoxWire | forms = "wire"
{
    x int32
}
fixed table FoxAll
{
    x int32
}
`), "cpp", nil)
	if err != nil {
		t.Fatalf("generate mixed unit: %v", err)
	}
	mixedSrc := ""
	for _, name := range cppKeysOf(mixed) {
		mixedSrc += string(mixed[name]) + "\n"
	}
	for _, want := range []string{
		"PingCook", "PingOpen", "PingFromJson", "PingToJson",
		"FoxAllBlock", "FoxAllCook", "FoxAllOpen",
		"FoxAllFromJson", "FoxAllToJson",
	} {
		if !strings.Contains(mixedSrc, want) {
			t.Errorf("mixed unit is missing expected member %q for an undeclared table", want)
		}
	}
	for _, forbidden := range []string{
		"PongCook", "PongOpen", "PongAt",
		"PongFromJson", "PongToJson",
		"FoxWireBlock", "FoxWireBlockOpen",
		"FoxWireCook", "FoxWireOpen", "FoxWireAt",
		"FoxWireFromJson", "FoxWireToJson",
	} {
		if strings.Contains(mixedSrc, forbidden) {
			t.Errorf("mixed unit leaked omitted form member %q for a wire-only table", forbidden)
		}
	}
}
