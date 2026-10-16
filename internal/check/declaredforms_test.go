// The `forms` table declaration's front end (docs/SPEC-TABLES.md §2.12,
// SPEC §4.2): a table line declares the forms it carries, a backend either
// honours them (Go) or refuses the construct by name (every other target).
// Every refusal the checker gives, and how each accepted spelling lands in the
// IR.
package check

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

func TestTableFormsDeclarationIsRead(t *testing.T) {
	t.Parallel()
	u := buildUnit(t, `package t

table Pong | forms = "wire,json"
{
    x int32
}

table Ball
{
    x int32
}
`)
	pong := u.Tables["Pong"]
	if !pong.DeclaredForms() {
		t.Fatal("Pong declares forms")
	}
	if len(pong.Forms) != 2 || pong.Forms[0] != "wire" || pong.Forms[1] != "json" {
		t.Fatalf("Pong forms = %v", pong.Forms)
	}
	if !pong.CarriesForm("wire") || !pong.CarriesForm("json") {
		t.Fatal("Pong carries the forms it declared")
	}
	if pong.CarriesForm("block") || pong.CarriesForm("cook") {
		t.Fatal("Pong does not carry the forms it left out")
	}
	ball := u.Tables["Ball"]
	if ball.DeclaredForms() {
		t.Fatal("Ball declares no forms")
	}
	for _, form := range []string{"wire", "json", "block", "cook"} {
		if !ball.CarriesForm(form) {
			t.Fatalf("an undeclared table carries every form, but Ball lacks %s", form)
		}
	}
}

func TestTableFormsRefusals(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		want string
		src  string
	}{
		{name: "a form outside the closed vocabulary", want: "names no form",
			src: "package t\ntable Tab | forms = \"wire,banana\"\n{\n    x int32\n}\n"},
		{name: "forms takes a quoted list", want: "forms takes a comma-separated quoted list",
			src: "package t\ntable Tab | forms = wire\n{\n    x int32\n}\n"},
		{name: "an empty forms list", want: "forms takes a comma-separated quoted list",
			src: "package t\ntable Tab | forms = \"\"\n{\n    x int32\n}\n"},
		{name: "forms written bare", want: "attribute forms requires a value",
			src: "package t\ntable Tab | forms\n{\n    x int32\n}\n"},
		{name: "forms on a type declaration", want: "is not an attribute a type declaration takes",
			src: "package t\ntype Tab | forms = \"wire\"\n{\n    x int32\n}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, perrs := parser.Parse("T.schema", []byte(tc.src))
			if len(perrs) > 0 {
				t.Fatalf("parse: %v", perrs[0])
			}
			_, errs := Unit([]SourceFile{{Path: "T.schema", Name: "T.schema", Base: "T", Bytes: []byte(tc.src), AST: f}})
			if len(errs) == 0 {
				t.Fatalf("compiled clean — want %q", tc.want)
			}
			for _, e := range errs {
				if strings.Contains(e.Error(), tc.want) {
					return
				}
			}
			t.Fatalf("no diagnostic contains %q; got: %v", tc.want, errs)
		})
	}
}
