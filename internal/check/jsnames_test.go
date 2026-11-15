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

// The JavaScript object-model names every object inherits: they are refused
// as every kind of schema name, not only as a field (SPEC §4.6, §6.1).
func TestJavaScriptObjectModelNamesRefused(t *testing.T) {
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

	for _, name := range []string{"__proto__", "__defineGetter__", "__defineSetter__", "__lookupGetter__", "__lookupSetter__"} {
		refused("package probe\n\ntype T\n{\n    "+name+" int32\n}\n", name)
		refused("package probe\n\ntable T\n{\n    "+name+" int32\n}\n", name)
	}
	refused("package probe\n\nenum E\n{\n    __proto__\n}\n", "__proto__")
	refused("package probe\n\nflags F\n{\n    __proto__\n}\n", "__proto__")
	refused("package probe\n\ntype P\n{\n    a int32\n}\n\nunion U\n{\n    __proto__ P\n}\n", "__proto__")

	for _, name := range []string{"valueOf", "hasOwnProperty"} {
		refused("package probe\n\nenum E\n{\n    "+name+"\n}\n", name)
		refused("package probe\n\ntype P\n{\n    a int32\n}\n\nunion U\n{\n    "+name+" P\n}\n", name)
	}

	if errs := compile("package probe\n\ntable T\n{\n    value int32\n    proto int32\n    kind int32\n}\n"); len(errs) > 0 {
		t.Errorf("ordinary names were refused: %v", errs)
	}
}
