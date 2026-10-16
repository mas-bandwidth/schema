// The Go backend honours a table's `forms` declaration (docs/SPEC-TABLES.md
// §2.12): a table carries exactly the forms it names, and a unit whose tables
// declare a form gets that form's files and no other. The default — no
// declaration — is every form, today's behaviour.
package gotable

import (
	"sort"
	"strings"
	"testing"
)

func keysOf(files map[string][]byte) []string {
	out := make([]string, 0, len(files))
	for k := range files {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestGoTableEmitsOnlyItsDeclaredForms(t *testing.T) {
	t.Parallel()
	defaultSchema := `package probe
table Pong { x int32 }
`
	def := generate(t, defaultSchema)
	if !hasFormFile(def, "Cook.go") || !hasFormFile(def, "TableJson.go") {
		t.Fatalf("an undeclared table emits every form — got %v", keysOf(def))
	}

	decl := generate(t, "package probe\ntable Pong | forms = \"wire\"\n{\n    x int32\n}\n")
	for _, name := range keysOf(decl) {
		if strings.HasSuffix(name, "Block.go") ||
			strings.HasSuffix(name, "Cook.go") ||
			strings.HasSuffix(name, "TableJson.go") {
			t.Errorf("%s emitted although Pong declares only wire", name)
		}
	}
	if !hasFormFile(decl, "Table.go") {
		t.Fatalf("the wire form is missing — got %v", keysOf(decl))
	}
}

func hasFormFile(files map[string][]byte, suffix string) bool {
	for name := range files {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// TestDeclaredFormUnitsCompile proves a unit whose tables declare forms still
// compiles: only the declared forms' files are emitted, and the wire table file
// and view neither reference an omitted form's symbols nor drop a core type.
func TestDeclaredFormUnitsCompile(t *testing.T) {
	t.Parallel()
	const probe = `package probe
import "testing"
func TestNoop(t *testing.T) {}
`
	for _, dec := range []string{
		"wire", "json", "cook", "block",
		"wire,cook", "wire,json", "json,cook",
	} {
		dec := dec
		t.Run(dec, func(t *testing.T) {
			t.Parallel()
			runGenerated(t, `package probe
table Pong | forms = "`+dec+`"
{
    x int32
}
`, probe)
		})
	}
}
