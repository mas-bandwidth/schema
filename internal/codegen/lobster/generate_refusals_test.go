package lobster

import (
	"slices"
	"strings"
	"testing"
)

// TestGenerateRefusesAnImportCycleBetweenFiles: two schema files whose types
// use each other's declarations are two Lobster modules that import each
// other, and a Lobster import cycle declares a type after its use, so Generate
// refuses the unit and names the cycle. The types themselves are not recursive:
// the cycle is the FILES', which is the case only this refusal catches. The
// control is the same three types with C moved beside B, which generates.
func TestGenerateRefusesAnImportCycleBetweenFiles(t *testing.T) {
	t.Parallel()
	control := t.TempDir()
	writeFile(t, control, "a.schema", "package cyc\ntype A { n int32 }\n")
	writeFile(t, control, "b.schema", "package cyc\ntype B { a A }\ntype C { b B }\n")
	if _, err := Generate(unitFromDir(t, control)); err != nil {
		t.Fatalf("control: files that import one way only were refused: %v", err)
	}
	dir := t.TempDir()
	writeFile(t, dir, "a.schema", "package cyc\ntype A { n int32 }\ntype C { b B }\n")
	writeFile(t, dir, "b.schema", "package cyc\ntype B { a A }\n")
	files, err := Generate(unitFromDir(t, dir))
	if err == nil {
		t.Fatalf("a.schema and b.schema import each other and the unit generated %d modules instead of a refusal", len(files))
	}
	if !strings.Contains(err.Error(), "cycle") || !strings.Contains(err.Error(), "a -> b -> a") {
		t.Fatalf("the refusal does not name the cycle a -> b -> a: %v", err)
	}
}

// TestForNamesNeedsTheClosingParen: forNames reads the loop variables bound
// after a for(...)'s closing paren. A line whose paren does not close on it
// binds nothing on that line, and an unclosed line is never read as if the
// whole line were the binding list (which would name `for` itself a local and
// mark the keyword). Controls: the closed spellings bind their names.
func TestForNamesNeedsTheClosingParen(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		line string
		want []string
	}{
		{"for(xs) x, i:", []string{"x", "i"}},
		{"    for(f(xs)) e:", []string{"e"}},
		{"for(xs", nil},
		{"    for(f(xs), g(", nil},
	} {
		if got := forNames(tc.line); !slices.Equal(got, tc.want) {
			t.Errorf("forNames(%q) = %q, want %q", tc.line, got, tc.want)
		}
	}
}
