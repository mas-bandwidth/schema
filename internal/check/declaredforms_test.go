// The `forms` table declaration's front end (docs/SPEC-TABLES.md §2.12,
// SPEC §4.2): a table line declares the forms it carries, a backend either
// honours them (Go) or refuses the construct by name (every other target).
// Every refusal the checker gives, and how each accepted spelling lands in the
// IR.
package check

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestTableFormsDeclarationIsRead(t *testing.T) {
	t.Parallel()
	u := buildUnit(t, `package t

table Pong | forms = "wire,json"
{
    x int32
    m map[string(8)]int32
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
	entry := u.Tables["PongMEntry"]
	if entry == nil || !entry.DeclaredForms() || !reflect.DeepEqual(entry.Forms, pong.Forms) {
		t.Fatalf("synthesized map entry PongMEntry must inherit Pong forms, got %+v", entry)
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
		{name: "an empty form element in commas", want: "contains an empty form name",
			src: "package t\ntable Tab | forms = \" , \"\n{\n    x int32\n}\n"},
		{name: "a trailing comma in forms", want: "contains an empty form name",
			src: "package t\ntable Tab | forms = \"wire,\"\n{\n    x int32\n}\n"},
		{name: "a repeated form name", want: `repeats form "wire"`,
			src: "package t\ntable Tab | forms = \"wire,wire\"\n{\n    x int32\n}\n"},
		{name: "forms omitting wire (json only)", want: "must include wire",
			src: "package t\ntable Tab | forms = \"json\"\n{\n    x int32\n}\n"},
		{name: "forms omitting wire (cook only)", want: "must include wire",
			src: "package t\ntable Tab | forms = \"cook\"\n{\n    x int32\n}\n"},
		{name: "forms omitting wire (block only)", want: "must include wire",
			src: "package t\nfixed table Tab | forms = \"block\"\n{\n    x int32\n}\n"},
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

func TestDeclaredFormsLeaveTheWireAlone(t *testing.T) {
	t.Parallel()
	// The declaration is a property of what a table CARRIES, not of the wire:
	// adding it must not move the wire name, the table id, the vocabulary or the
	// build version (docs/SPEC-TABLES.md §3.1, §5). A real `was` rename
	// composes with forms: the wire name stays the OLD name and forms still
	// leaves it alone.
	cases := []struct {
		name      string
		baseTail  string // the table line without a forms declaration
		formsTail string // the same table line with forms added
		wantWas   string
	}{
		{name: "undeclared", baseTail: "", formsTail: ` | forms = "wire,json"`, wantWas: ""},
		{name: "after a was rename", baseTail: ` | was = "OldPong"`, formsTail: ` | was = "OldPong", forms = "wire,json"`, wantWas: "OldPong"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := buildUnit(t, "package t\ntable Pong"+tc.baseTail+"\n{\n    x int32\n}\n")
			with := buildUnit(t, "package t\ntable Pong"+tc.formsTail+"\n{\n    x int32\n}\n")
			bt := base.Tables["Pong"]
			wt := with.Tables["Pong"]
			if wt.WasName != tc.wantWas {
				t.Errorf("WasName = %q, want %q", wt.WasName, tc.wantWas)
			}
			if !wt.DeclaredForms() || len(wt.Forms) != 2 {
				t.Errorf("forms = %v, want [wire json]", wt.Forms)
			}
			if wt.WireName() != bt.WireName() {
				t.Errorf("declaring forms moved the wire name %q -> %q", bt.WireName(), wt.WireName())
			}
			if !reflect.DeepEqual(ir.TableWireIds(with), ir.TableWireIds(base)) {
				t.Error("declaring forms moved the table id set")
			}
			if !bytes.Equal(ir.TableVocabularyBytes(ir.TableVocabulary(with)), ir.TableVocabularyBytes(ir.TableVocabulary(base))) {
				t.Error("declaring forms moved the vocabulary")
			}
			if ir.BuildVersion(with) != ir.BuildVersion(base) {
				t.Error("declaring forms moved the build version")
			}
		})
	}
}
