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

// THE BINDING SITE (SPEC §4.6, §6.1). A declaration becomes a module-scope
// binding of its own name in the generated JavaScript and a declaration of it
// in the .d.ts. Eight names cannot be one: an ES module refuses eval and
// arguments as a binding, a binding named undefined takes the value the
// generated modules compare against, and TypeScript refuses a declaration
// named after a predefined type (unknown, never, symbol, number, bigint,
// undefined). Each is refused as every kind of declaration. A FIELD of the
// same name is a property and binds nothing, so it is kept.
func TestJavaScriptUnbindableNamesRefusedAsDeclarations(t *testing.T) {
	t.Parallel()
	compile := func(source string) []error {
		t.Helper()
		f, perrs := parser.Parse("Probe.schema", []byte(source))
		if len(perrs) > 0 {
			t.Fatalf("parse %q: %v", source, perrs[0])
		}
		_, cerrs := check.Unit([]check.SourceFile{{
			Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(source), AST: f,
		}})
		return cerrs
	}
	forms := func(name string) map[string]string {
		return map[string]string{
			"type":  "package probe\n\ntype " + name + "\n{\n    value uint32\n}\n\ntable Entry\n{\n    part " + name + "\n}\n",
			"table": "package probe\n\ntable " + name + "\n{\n    value uint32\n}\n",
			"enum":  "package probe\n\nenum " + name + "\n{\n    a\n    b\n}\n",
			"flags": "package probe\n\nflags " + name + "\n{\n    a\n    b\n}\n",
			"union": "package probe\n\ntype P\n{\n    a int32\n}\n\nunion " + name + "\n{\n    p P\n}\n",
			"const": "package probe\n\nconst " + name + " = 1\n",
		}
	}
	// the controls: every form compiles under an ordinary name, and under
	// two words TypeScript reads as keywords only where it expects one
	for _, name := range []string{"Clean", "intrinsic", "declare"} {
		for kind, source := range forms(name) {
			if errs := compile(source); len(errs) > 0 {
				t.Fatalf("the control %s named %s was refused: %v", kind, name, errs)
			}
		}
	}
	for _, name := range []string{"eval", "arguments", "undefined", "unknown", "never", "symbol", "number", "bigint"} {
		for kind, source := range forms(name) {
			errs := compile(source)
			named := false
			for _, err := range errs {
				if strings.Contains(err.Error(), name) && strings.Contains(err.Error(), "JavaScript") {
					named = true
				}
			}
			if !named {
				t.Errorf("%s %s was not refused naming the JavaScript binding; got %v", kind, name, errs)
			}
		}
		// the same word as a field is a property, never a binding
		field := "package probe\n\ntype Clean\n{\n    value uint32\n}\n\ntable Entry\n{\n    part Clean\n    " + name + " uint32\n}\n"
		if errs := compile(field); len(errs) > 0 {
			t.Errorf("a field named %s was refused: %v", name, errs)
		}
	}
}
