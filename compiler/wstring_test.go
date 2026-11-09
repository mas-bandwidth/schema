package compiler

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

// TestWstringKind33Retired is the refusal: kind 33 is retired, and every
// spelling of `wstring` is refused by the one diagnostic that names the fix.
func TestWstringKind33Retired(t *testing.T) {
	const want = "wstring is not a schema type: text is string, which is UTF-8"
	cases := []string{
		"package wide\ntype Text { name wstring(4) }\n",
		"package wide\ntable Root { title wstring(6) }\n",
		"package wide\ntable Root { m map[wstring(8)]uint32 }\n",
		"package wide\ntable Root { w ?wstring(8) }\n",
		"package wide\ntable Root { w *wstring }\n",
		"package wide\ntable Root { w wstring }\n",
	}
	for _, src := range cases {
		f, perrs := parser.Parse("Probe.schema", []byte(src))
		if len(perrs) > 0 {
			t.Fatalf("parse %q: %v", src, perrs[0])
		}
		_, errs := check.Unit([]check.SourceFile{{
			Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
		}})
		if len(errs) != 1 {
			t.Errorf("%q: want one refusal, got %d: %v", src, len(errs), errs)
			continue
		}
		if msg := errs[0].Error(); !strings.Contains(msg, want) {
			t.Errorf("%q: refusal is not the named retirement error: %v", src, msg)
		}
	}
}
