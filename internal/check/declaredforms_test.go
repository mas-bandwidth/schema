// `forms = "wire,json"` on a table declaration (docs/SPEC-TABLES.md §2.11,
// SPEC §4.2): the accepted spellings land in the IR in the vocabulary's order,
// and each refused one is refused by name.
package check

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

func TestTableFormsDeclarationIsRead(t *testing.T) {
	t.Parallel()
	unit := func(t *testing.T, src string) (forms map[string][]string, errs []string) {
		t.Helper()
		f, perrs := parser.Parse("T.schema", []byte(src))
		if len(perrs) > 0 {
			t.Fatalf("parse: %v", perrs[0])
		}
		u, cerrs := Unit([]SourceFile{{Path: "T.schema", Name: "T.schema", Base: "T", Bytes: []byte(src), AST: f}})
		for _, e := range cerrs {
			errs = append(errs, e.Error())
		}
		forms = map[string][]string{}
		if u != nil {
			for name, st := range u.Tables {
				// read by name so a tree without the field fails this test rather than its build
				field := reflect.ValueOf(st).Elem().FieldByName("Forms")
				if !field.IsValid() {
					t.Fatalf("ir.Struct has no Forms field")
				}
				forms[name], _ = field.Interface().([]string)
			}
		}
		return forms, errs
	}
	accepted := []struct {
		name, qual string
		want       []string // nil is the default: every form
	}{
		{"no declaration is every form", "", nil},
		{"wire and json", ` | forms = "wire,json"`, []string{"wire", "json"}},
		{"written in another order", ` | forms = "json,wire"`, []string{"wire", "json"}},
		{"wire alone", ` | forms = "wire"`, []string{"wire"}},
		{"beside a tag", ` | forms = "wire", shipped`, []string{"wire"}},
		{"beside was", ` | was = "Old", forms = "wire"`, []string{"wire"}},
	}
	for _, c := range accepted {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			forms, errs := unit(t, "package t\ntable Tab"+c.qual+"\n{\n    x int32\n}\n")
			if len(errs) > 0 {
				t.Fatalf("refused: %v", errs)
			}
			if strings.Join(forms["Tab"], ",") != strings.Join(c.want, ",") || (c.want == nil) != (forms["Tab"] == nil) {
				t.Fatalf("Forms = %#v, want %#v", forms["Tab"], c.want)
			}
		})
	}
	refused := []struct{ name, src, want string }{
		{"a bare list is a value then a tag", "package t\ntable Tab | forms = wire, json\n{\n    x int32\n}\n", "forms takes a quoted string"},
		{"a bare key", "package t\ntable Tab | forms\n{\n    x int32\n}\n", "attribute forms requires a value"},
		{"an empty value", "package t\ntable Tab | forms = \"\"\n{\n    x int32\n}\n", "names no form"},
		{"an unknown form", "package t\ntable Tab | forms = \"wire,xml\"\n{\n    x int32\n}\n", `unknown form "xml"`},
		{"json without wire", "package t\ntable Tab | forms = \"json\"\n{\n    x int32\n}\n", "does not list wire"},
		{"a repeated form", "package t\ntable Tab | forms = \"wire,wire\"\n{\n    x int32\n}\n", `form "wire" repeated`},
		{"a space after the comma", "package t\ntable Tab | forms = \"wire, json\"\n{\n    x int32\n}\n", `unknown form " json"`},
		{"cook is not declarable yet", "package t\ntable Tab | forms = \"wire,cook\"\n{\n    x int32\n}\n", "not a form a table can declare"},
		{"block is not declarable", "package t\ntable Tab | forms = \"wire,block\"\n{\n    x int32\n}\n", "not a form a table can declare"},
		{"repeated key", "package t\ntable Tab | forms = \"wire\", forms = \"wire\"\n{\n    x int32\n}\n", "attribute forms repeated"},
		{"on a type", "package t\ntype Ty | forms = \"wire\"\n{\n    x int32\n}\n", "is not an attribute a type declaration takes"},
		{"on a field", "package t\ntable Tab\n{\n    x int32 | forms = \"wire\"\n}\n", "is not an attribute"},
	}
	for _, c := range refused {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, errs := unit(t, c.src)
			for _, e := range errs {
				if strings.Contains(e, c.want) {
					return
				}
			}
			t.Fatalf("no diagnostic contains %q; got %v", c.want, errs)
		})
	}
}
