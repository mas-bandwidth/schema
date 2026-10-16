// The `forms` table declaration is carried by Go first (docs/SPEC-TABLES.md
// §2.12): every other target refuses the construct BY NAME, centrally, in
// Generate. Go honours it, and a unit with no declaration generates as before.
package compiler

import (
	"strings"
	"testing"
)

func TestDeclaredFormsRefusedByOtherTargets(t *testing.T) {
	t.Parallel()
	u := unitFromSource(t, "package probe\ntable Pong | forms = \"wire\"\n{\n    x int32\n}\n")
	c := New()
	for _, lang := range []string{"c", "cpp", "cs", "dart", "elixir", "java", "javascript", "rust"} {
		_, err := c.Generate(u, lang, nil)
		if err == nil {
			t.Errorf("target %s accepted a forms declaration", lang)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, "forms") || !strings.Contains(msg, "Go carries it first") {
			t.Errorf("target %s refusal does not name the construct and Go: %s", lang, msg)
		}
	}
	if _, err := c.Generate(u, "go", nil); err != nil {
		t.Fatalf("Go honours the declaration: %v", err)
	}
}
