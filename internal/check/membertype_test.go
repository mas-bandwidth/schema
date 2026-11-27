package check_test

import (
	"strings"
	"testing"

	_ "github.com/mas-bandwidth/schema/v3/compiler" // registers the emitters name rules
	"github.com/mas-bandwidth/schema/v3/internal/check"
	"github.com/mas-bandwidth/schema/v3/internal/parser"
)

// TestMemberNamedLikeAType holds the rule behind the name-buildability rows
// e_kind, e_kind_arm, e_kind_case and e_kind_sibling (SPEC §4.6, Emitter name
// rules): a member (a field of a type or a table, or a union arm) spelled like
// a named type a member of the same declaration is declared with is refused
// for every target. C++ compares the name as written and Dart its lowerCamel
// spelling, so `Kind kind` names Dart alone. A field and a type that differ by
// case in every target (`kind Kind`) are two names and pass, as does the same
// name on a field of another type.
func TestMemberNamedLikeAType(t *testing.T) {
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
	const both, dartOnly = "C++ Dart", "Dart"
	for _, c := range []struct {
		name, src, targets, want string
	}{
		{"enum field of a type", head + "type Inner\n{\n    kind kind\n}\n", both,
			"type Inner: member kind is named like its own type kind"},
		{"enum field of a table", head + "table Doc\n{\n    kind kind\n}\n", both,
			"table Doc: member kind is named like its own type kind"},
		{"union arm of a table", head + "union U\n{ kind kind }\n\ntable Rec\n{\n    pick U\n}\n", both,
			"union U: member kind is named like its own type kind"},
		{"packet union arm", "package probe\n\ntype inner\n{\n    x int32\n}\n\nunion P\n{ inner inner }\n\ntype Holder\n{\n    pick P\n}\n", both,
			"union P: member inner is named like its own type inner"},
		{"differing by case, Dart spells it lowerCamel", head + "type Inner\n{\n    Kind kind\n}\n", dartOnly,
			"type Inner: member Kind is named like its own type kind"},
		{"named like a sibling's type", head + "type Inner\n{\n    mode kind\n    kind Kind2\n}\n", both,
			"type Inner: member kind is named like the type kind another member of Inner is declared with"},
		{"named like a later sibling's type", head + "table Doc\n{\n    kind Kind2\n    mode kind\n}\n", both,
			"table Doc: member kind is named like the type kind another member of Doc is declared with"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := run(t, c.src)
			errs := strings.Join(got, "\n")
			for _, target := range []string{"C++", "Dart"} {
				named := strings.Contains(errs, "the "+target+" target")
				if want := strings.Contains(c.targets, target); named != want {
					t.Errorf("naming the %s target: got %v, want %v:\n%s", target, named, want, errs)
				}
			}
			if !strings.Contains(errs, c.want) {
				t.Errorf("want %q, got:\n%s", c.want, errs)
			}
			for _, e := range got {
				if n := strings.Count(e, "(SPEC §4.6)"); n != 1 {
					t.Errorf("want (SPEC §4.6) once, got %d times: %s", n, e)
				}
			}
		})
	}
	for name, src := range map[string]string{
		"differing by case":             "package probe\n\nenum Kind\n{ Alpha, Beta }\n\ntype Inner\n{\n    kind Kind\n}\n",
		"differing by case in a table":  "package probe\n\nenum Kind\n{ Alpha, Beta }\n\ntable Doc\n{\n    kind Kind\n}\n",
		"another field name":            head + "type Inner\n{\n    mode kind\n}\n",
		"a table field of another name": head + "table Doc\n{\n    mode kind\n}\n",
		"a union arm of another name":   head + "union U\n{ mode kind }\n\ntable Rec\n{\n    pick U\n}\n",
		"a type used by another owner":  head + "type Inner\n{\n    mode kind\n}\n\ntype Outer\n{\n    kind Kind2\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if errs := run(t, src); len(errs) > 0 {
				t.Errorf("want it accepted, got:\n%s", strings.Join(errs, "\n"))
			}
		})
	}
}
