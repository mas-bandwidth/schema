package compiler

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

// TestWstringKind33Retired is the refusal: kind 33 is retired, and every
// spelling of `wstring` is refused by the one diagnostic that names the fix.
func TestWstringKind33Retired(t *testing.T) {
	cases := []string{
		"package wide\ntype Text { name wstring(4) }\n",
		"package wide\ntable Root { title wstring(6) }\n",
		"package wide\ntable Root { m map[wstring(8)]uint32 }\n",
		"package wide\ntable Root { w ?wstring(8) }\n",
		"package wide\ntable Root { w *wstring }\n",
		"package wide\ntable Root { w wstring }\n",
	}
	for _, src := range cases {
		_, errs := parser.Parse("Probe.schema", []byte(src))
		if len(errs) == 0 {
			t.Errorf("wstring was accepted: %s", src)
			continue
		}
		msg := errs[0].Error()
		if !strings.Contains(msg, parser.WStringRefusal[:strings.Index(parser.WStringRefusal, "%s")]) {
			t.Errorf("refusal is not the named retirement error: %v", msg)
		}
	}
}
