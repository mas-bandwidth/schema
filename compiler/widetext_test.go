// Wide text and the TABLE emitter (docs/SPEC-TABLES.md §3, §11, schema#522):
// a `wstring(N)` inside a table closure rides kind 33, so the C++ table
// emitter carries it and the other eight refuse the unit by name.
package compiler

import (
	"os"
	"path/filepath"
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

table Root
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
	if !strings.Contains(text, "w.put8( 33 ); // title") {
		t.Errorf("the writer does not put kind 33 for the table's own field (docs/SPEC-TABLES.md §3)")
	}
	if !strings.Contains(text, "w.put8( 33 ); // name") {
		t.Errorf("the writer does not put kind 33 for the type's field (docs/SPEC-TABLES.md §3)")
	}
	if !strings.Contains(text, "TableUtf16Valid") {
		t.Errorf("the reader does not check the payload's content (docs/SPEC-TABLES.md §3, §4)")
	}
}

// TestWideTextInATableClosureIsRefusedByTheOtherEight is the same unit against
// a target that carries neither wire's half: the refusal names the field, so
// no port emits a member it never laid out (SPEC.md §4.12).
func TestWideTextInATableClosureIsRefusedByTheOtherEight(t *testing.T) {
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
	for _, target := range []string{"c", "cs", "go", "rust", "java", "js", "dart", "elixir"} {
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

func keysOf(files map[string][]byte) []string {
	out := make([]string, 0, len(files))
	for name := range files {
		out = append(out, name)
	}
	return out
}
