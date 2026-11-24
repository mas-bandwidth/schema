package check_test

import (
	"strings"
	"testing"

	_ "github.com/mas-bandwidth/schema/v2/compiler" // registers the emitters name rules
	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

// TestFieldNamedLikeItsOwnType holds the rule behind the name-buildability row
// e_kind (SPEC §4.6, Emitter name rules): a field named exactly like its own
// named type is refused for every target, naming C++ and Dart, which cannot
// spell it. A field and a type that differ by case are two names and pass,
// as does the same name on a field of another type.
func TestFieldNamedLikeItsOwnType(t *testing.T) {
	t.Parallel()
	run := func(t *testing.T, src string) []string {
		t.Helper()
		ast, perrs := parser.Parse("Probe.schema", []byte(src))
		if len(perrs) > 0 {
			t.Fatal(perrs)
		}
		_, errs := check.Unit([]check.SourceFile{{Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: ast}})
		var out []string
		for _, e := range errs {
			out = append(out, e.Error())
		}
		return out
	}
	const head = "package probe\n\nenum kind\n{ Alpha, Beta }\n\nenum Kind2\n{ Alpha, Beta }\n\n"
	for _, c := range []struct {
		name, src, want string
	}{
		{"enum field of a type", head + "type Inner\n{\n    kind kind\n}\n", "Inner.kind is a field named like its own type kind"},
		{"enum field of a table", head + "table Doc\n{\n    kind kind\n}\n", "Doc.kind is a field named like its own type kind"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			errs := strings.Join(run(t, c.src), "\n")
			for _, target := range []string{"the C++ target", "the Dart target"} {
				if !strings.Contains(errs, target) {
					t.Errorf("want a refusal naming %s, got:\n%s", target, errs)
				}
			}
			if !strings.Contains(errs, c.want) {
				t.Errorf("want %q, got:\n%s", c.want, errs)
			}
		})
	}
	for name, src := range map[string]string{
		"differing by case":    head + "type Inner\n{\n    kind Kind2\n}\n",
		"another field name":   head + "type Inner\n{\n    mode kind\n}\n",
		"a table field of another name": head + "table Doc\n{\n    mode kind\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if errs := run(t, src); len(errs) > 0 {
				t.Errorf("want it accepted, got:\n%s", strings.Join(errs, "\n"))
			}
		})
	}
}
