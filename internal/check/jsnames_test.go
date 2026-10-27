package check_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

// The JavaScript backend's two claims (SPEC §4.6, §6.1): a field whose
// lowerCamelCase member would shadow an Object.prototype member, and a
// declaration named after a binding every generated JavaScript packet module
// declares at module scope.
func TestJavaScriptClaims(t *testing.T) {
	t.Parallel()
	compile := func(source string) []error {
		t.Helper()
		f, perrs := parser.Parse("Probe.schema", []byte(source))
		if len(perrs) > 0 {
			t.Fatalf("parse: %v", perrs[0])
		}
		_, cerrs := check.Unit([]check.SourceFile{{
			Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(source), AST: f,
		}})
		return cerrs
	}
	refused := func(source, needle string) {
		t.Helper()
		errs := compile(source)
		for _, err := range errs {
			if strings.Contains(err.Error(), needle) {
				return
			}
		}
		t.Errorf("%q was not refused naming %q; got %v", source, needle, errs)
	}

	refused("package probe\n\ntype T\n{\n    to_string uint8\n}\n", "toString")
	refused("package probe\n\ntype T\n{\n    constructor uint8\n}\n", "constructor")
	refused("package probe\n\nconst validText = 1\n", "validText")
	refused("package probe\n\nconst PRODUCTION = 1\n", "PRODUCTION")

	if errs := compile("package probe\n\ntype T\n{\n    to_text uint8\n    value uint8\n}\n"); len(errs) > 0 {
		t.Errorf("ordinary fields were refused: %v", errs)
	}
}
