// Wide text and the TABLE emitter (docs/SPEC-TABLES.md §3, §11, schema#522):
// a `wstring(N)` inside a table closure rides kind 33, so the C++ table
// emitter carries it and the other eight refuse the unit by name.
package compiler

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// wideTextInATableClosure is the smallest unit that reaches the case: the
// wide text sits in a `type`, and a table holds the type by value.
const wideTextInATableClosure = `package wide

type Text
{
    name wstring(4)
}

fixed table Root
{
    t     Text
    title wstring(6)
}
`

// TestWideTextInATableClosureReachesTheTableEmitter loads the unit the way the
// CLI does and asks the C++ target for it: the front end accepts it and the
// generated Table header carries kind 33. The C++ target is the one carrier of
// wide text on either wire, so it is the one target whose table emitter is
// handed the kind.
func TestWideTextInATableClosureReachesTheTableEmitter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Wide.schema")
	if err := os.WriteFile(path, []byte(wideTextInATableClosure), 0o644); err != nil {
		t.Fatal(err)
	}
	c := New()
	u, err := c.Load([]string{path})
	if err != nil {
		t.Fatalf("Load refused a wstring(N) inside a table closure: %v", err)
	}
	files, err := c.Generate(u, "cpp", Options{})
	if err != nil {
		t.Fatalf("the C++ target refused the unit: %v", err)
	}
	header, ok := files["WideTable.h"]
	if !ok {
		t.Fatalf("no Table header was generated: %v", keysOf(files))
	}
	text := string(header)
	if !strings.Contains(text, "char16_t title[6 + 1]") {
		t.Errorf("the record does not carry wide text's storage (docs/SPEC-TABLES.md §7.2)")
	}
	// the reference and the kind ride as ONE header call (TableWriter::header)
	if !strings.Contains(text, "), 33 ); // title") {
		t.Errorf("the writer does not put kind 33 for the table's own field (docs/SPEC-TABLES.md §3)")
	}
	if !strings.Contains(text, "), 33 ); // name") {
		t.Errorf("the writer does not put kind 33 for the type's field (docs/SPEC-TABLES.md §3)")
	}
	if !strings.Contains(text, "TableUtf16Valid") {
		t.Errorf("the reader does not check the payload's content (docs/SPEC-TABLES.md §3, §4)")
	}
}

// TestTableWideTextTargetsCarryEveryLeg is the RED-FIRST unit for the four
// legs this card lands: rust, java, js and elixir each carry a `wstring(N)`
// table field through their fixed form (SPEC §4.12), so all four are in
// tableWideTextTargets, named by tableWideTextNames, and a unit with a
// wstring(N) table field is no longer refused for them.
func TestTableWideTextTargetsCarryEveryLeg(t *testing.T) {
	t.Parallel()
	for _, leg := range []string{"rust", "java", "js", "elixir"} {
		if !tableWideTextTargets[leg] {
			t.Errorf("%s does not carry wide text in a table closure (SPEC §4.12)", leg)
		}
	}
	for _, name := range []string{"Rust", "Java", "JavaScript", "Elixir"} {
		if !slices.Contains(tableWideTextNames, name) {
			t.Errorf("tableWideTextNames does not name %s", name)
		}
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "Wide.schema")
	if err := os.WriteFile(path, []byte(wideTextInATableClosure), 0o644); err != nil {
		t.Fatal(err)
	}
	c := New()
	u, err := c.Load([]string{path})
	if err != nil {
		t.Fatalf("Load refused a wstring(N) inside a table closure: %v", err)
	}
	for _, leg := range []string{"rust", "java", "js", "elixir"} {
		if _, err := c.Generate(u, leg, Options{}); err != nil {
			t.Errorf("%s still refuses a wstring(N) table field it carries: %v", leg, err)
		}
	}
}

// TestWideTextInATableClosureRefusesMissingCarriers pins the named refusal a
// target without the construct still gives. Every current target carries it
// (c, cpp, cs, go, dart, rust, java, js, elixir), so the list is empty and the
// test is the guard against a future target being added without a table codec:
// it refuses by field name (SPEC.md §4.12).
func TestWideTextInATableClosureRefusesMissingCarriers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "Wide.schema")
	if err := os.WriteFile(path, []byte(wideTextInATableClosure), 0o644); err != nil {
		t.Fatal(err)
	}
	c := New()
	u, err := c.Load([]string{path})
	if err != nil {
		t.Fatalf("Load refused a wstring(N) inside a table closure: %v", err)
	}
	// No target lacks the table codec today; a new one added to the registry
	// without one goes here and the refusal must name the field.
	for _, target := range []string{} {
		_, err := c.Generate(u, target, Options{})
		if err == nil {
			t.Errorf("%s took a wstring(N) it does not carry", target)
			continue
		}
		if !strings.Contains(err.Error(), "Text.name") {
			t.Errorf("%s's refusal does not name the field: %v", target, err)
		}
	}
}

// TestWideTextInATableClosureRidesTheDartFixedForm is the other half of the
// rule above: Dart takes the same unit, emits the FIXED form for it, and emits
// NEITHER accelerator — a block row and a cooked node have no kind 33 in this
// backend, and the refusal is scoped to them by name rather than taken out on
// a form that lays the construct out fine (docs/SPEC-TABLES.md §3.4, §15).
func TestWideTextInATableClosureRidesTheDartFixedForm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Wide.schema")
	if err := os.WriteFile(path, []byte(wideTextInATableClosure), 0o644); err != nil {
		t.Fatal(err)
	}
	c := New()
	u, err := c.Load([]string{path})
	if err != nil {
		t.Fatalf("Load refused a wstring(N) inside a table closure: %v", err)
	}
	files, err := c.Generate(u, "dart", Options{})
	if err != nil {
		t.Fatalf("dart refused a wstring(N) its fixed form carries: %v", err)
	}
	var fixed, accelerator []string
	for _, name := range keysOf(files) {
		switch {
		case strings.HasSuffix(name, "Fixed.dart"):
			fixed = append(fixed, name)
		case strings.HasSuffix(name, "Block.dart"), strings.HasSuffix(name, "Cook.dart"):
			accelerator = append(accelerator, name)
		}
	}
	if len(fixed) == 0 {
		t.Errorf("dart emitted no fixed form for a wide-text table: %v", keysOf(files))
	}
	if len(accelerator) != 0 {
		t.Errorf("dart emitted an accelerator for a wide-text table: %v", accelerator)
	}
}

func keysOf(files map[string][]byte) []string {
	out := make([]string, 0, len(files))
	for name := range files {
		out = append(out, name)
	}
	return out
}
