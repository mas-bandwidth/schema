// The two `was` rows' front end (docs/SPEC-TABLES.md §5, SPEC §4.2): string,
// bytes and flags defaults, and `was` on a table declaration. Every refusal
// the checker gives, and the resolution each accepted spelling lands in the IR.
package check

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestValueDefaultAndTableWasRefusals(t *testing.T) {
	cases := []struct {
		name string
		want string
		src  string
	}{
		// ---- string and bytes defaults (SPEC §4.2) ----
		{name: "a string default past the capacity", want: "past the field's capacity of 4",
			src: "package t\ntable Tab { s string(4) = \"hello\" }\n"},
		{name: "a bytes default past the capacity", want: "past the field's capacity of 2",
			src: "package t\ntable Tab { b bytes(2) = \"abc\" }\n"},
		{name: "a string default that is not UTF-8", want: "is not valid UTF-8",
			src: "package t\ntable Tab { s string(8) = \"\xff\" }\n"},
		{name: "a quoted default on an integer", want: "a quoted string is a string(N) or bytes(N) default",
			src: "package t\ntable Tab { n int32 = \"7\" }\n"},
		{name: "a wstring takes no default", want: "a wstring takes no specified default",
			src: "package t\ntype T { s wstring(8) = \"x\" }\n"},
		// ---- flags defaults ----
		{name: "a brace list on a scalar", want: "a brace list is a FLAGS default",
			src: "package t\ntable Tab { n int32 = { A } }\n"},
		{name: "a brace list on an enum", want: "a brace list is a FLAGS default",
			src: "package t\nenum E { A }\ntable Tab { e E = { A } }\n"},
		{name: "a flags default naming no variant", want: "Swim is not a variant of flags Caps",
			src: "package t\nflags Caps { Jump }\ntable Tab { c Caps = { Swim } }\n"},
		{name: "a flags default repeating a variant", want: "repeats in the default",
			src: "package t\nflags Caps { Jump }\ntable Tab { c Caps = { Jump, Jump } }\n"},
		{name: "an integer default on a flags field", want: "defaults cover bool, integer, float, enum, string, bytes and flags fields",
			src: "package t\nflags Caps { Jump }\ntable Tab { c Caps = 1 }\n"},
		// ---- was on a declaration (docs/SPEC-TABLES.md §5) ----
		{name: "was on a type declaration", want: "was on a type declaration is refused",
			src: "package t\ntype P | was = \"Q\"\n{\n    x int32\n}\n"},
		{name: "a table's was naming its own name", want: "names the table's own current name",
			src: "package t\ntable Tab | was = \"Tab\"\n{\n    x int32\n}\n"},
		{name: "a table's was with an empty string", want: "names nothing",
			src: "package t\ntable Tab | was = \"\"\n{\n    x int32\n}\n"},
		{name: "a table's was takes a quoted string", want: "was takes the table's old name as a quoted string",
			src: "package t\ntable Tab | was = Old\n{\n    x int32\n}\n"},
		{name: "a table's was written bare", want: "attribute was requires a value",
			src: "package t\ntable Tab | was\n{\n    x int32\n}\n"},
		{name: "a table's was colliding with a live table", want: "collide on table-wire type id",
			src: "package t\ntable Old { x int32 }\ntable Tab | was = \"Old\"\n{\n    y int32\n}\ntable Root\n{\n    a *Old\n    b *Tab\n}\n"},
		{name: "a table's was taking a held-back id is refused by name", want: "is not an attribute a table declaration takes",
			src: "package t\ntable Tab | json = \"x\"\n{\n    x int32\n}\n"},
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

const wasRowsSrc = `package t

flags Caps { Jump, Crouch, Fly }

type Badge
{
    label string(8) = "new"
    caps  Caps = {}
}

table Ship | pinned, was = "Vessel"
{
    name  string(32) = "untitled"
    tag   bytes(4) = "ab"
    caps  Caps = { Jump, Fly }
    badge Badge
}

table Fleet
{
    flagship *Ship
}
`

func TestValueDefaultsAndTableWasResolve(t *testing.T) {
	u := buildUnit(t, wasRowsSrc)
	ship := u.Tables["Ship"]
	if ship.WasName != "Vessel" || ship.WireName() != "Vessel" {
		t.Errorf("Ship rides under its was: WasName=%q WireName=%q", ship.WasName, ship.WireName())
	}
	if len(ship.Tags) != 1 || ship.Tags[0] != "pinned" {
		t.Errorf("a tag rides beside was: %v", ship.Tags)
	}
	if u.Tables["Fleet"].WireName() != "Fleet" {
		t.Error("a table with no was rides under its own name")
	}
	byName := map[string]*ir.Field{}
	for _, f := range ship.Fields {
		byName[f.Name] = f
	}
	if f := byName["name"]; !f.HasDefault || string(f.DefBytes) != "untitled" {
		t.Errorf("string default: %+v", f)
	}
	if f := byName["tag"]; !f.HasDefault || string(f.DefBytes) != "ab" {
		t.Errorf("bytes default: %+v", f)
	}
	if f := byName["caps"]; !f.HasDefault || f.DefInt == nil || f.DefInt.Int64() != 0b101 {
		t.Errorf("flags default is the mask the names spell: %+v", f)
	}
	badge := u.Structs["Badge"]
	if f := badge.Fields[1]; !f.HasDefault || f.DefInt == nil || f.DefInt.Sign() != 0 {
		t.Errorf("an empty brace list is the zero mask, declared: %+v", f)
	}
	if got := ir.ValueDefaultFields(u); strings.Join(got, " ") != "Badge.caps Badge.label Ship.caps Ship.name Ship.tag" {
		t.Errorf("ValueDefaultFields: %v", got)
	}
}
