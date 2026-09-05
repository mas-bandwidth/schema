// The second `was` row's front end (docs/SPEC-TABLES.md §5): `was` on an enum
// variant, on a union arm and on a field of a `type` a table reaches. Every
// refusal the checker gives, and the resolution each accepted spelling lands
// in the IR.
package check

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestWasRowRefusals(t *testing.T) {
	cases := []struct {
		name string
		want string
		src  string
	}{
		{name: "a variant's was naming its own name", want: "names the variant's own current name",
			src: "package t\nenum E\n{\n    A | was = \"A\"\n}\ntable Tab { e E }\n"},
		{name: "a variant's was with an empty string", want: "names nothing",
			src: "package t\nenum E\n{\n    A | was = \"\"\n}\ntable Tab { e E }\n"},
		{name: "a variant's was takes a quoted string", want: "was takes the variant's old name as a quoted string",
			src: "package t\nenum E\n{\n    A | was = B\n}\ntable Tab { e E }\n"},
		{name: "a variant's was written bare", want: "attribute was requires a value",
			src: "package t\nenum E\n{\n    A | was\n}\ntable Tab { e E }\n"},
		{name: "a variant takes no other valued key", want: "is not an attribute a variant takes",
			src: "package t\nenum E\n{\n    A | max = 3\n}\ntable Tab { e E }\n"},
		{name: "a variant's was colliding with a live variant", want: "collide on table-wire id",
			src: "package t\nenum E\n{\n    A,\n    B | was = \"A\"\n}\ntable Tab { e E }\n"},
		{name: "a variant's was outside a table closure", want: "no table reaches E",
			src: "package t\nenum E\n{\n    A | was = \"Z\"\n}\ntype P { e E }\n"},
		{name: "a flags variant takes no was", want: "takes no was",
			src: "package t\nflags F\n{\n    A | was = \"Z\"\n}\ntable Tab { f F }\n"},
		{name: "an arm's was naming its own name", want: "names the field's own current name",
			src: "package t\ntype A { x int32 }\nunion U\n{\n    a A | was = \"a\"\n}\ntable Tab { u U }\n"},
		{name: "a payload-free arm's was naming its own name", want: "names the variant's own current name",
			src: "package t\nunion U\n{\n    a | was = \"a\"\n}\ntable Tab { u U }\n"},
		{name: "an arm's was colliding with a live arm", want: "collide on table-wire id",
			src: "package t\ntype A { x int32 }\nunion U\n{\n    a A\n    b A | was = \"a\"\n}\ntable Tab { u U }\n"},
		{name: "an arm's was outside a table closure", want: "no table reaches U",
			src: "package t\ntype A { x int32 }\nunion U\n{\n    a A | was = \"z\"\n}\ntype P { u U }\n"},
		{name: "a type field's was outside a table closure", want: "no table reaches P",
			src: "package t\ntype P { speed float32 | was = \"velocity\" }\n"},
		{name: "a type field's was colliding inside the type", want: "collide on table-wire id",
			src: "package t\ntype P\n{\n    a int32\n    b int32 | was = \"a\"\n}\ntable Tab { p P }\n"},
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

const wasRowsRenamedSrc = `package t

enum Grade
{
    Bronze,
    Argent | was = "Silver"
    Gold
}

type Buff
{
    mult float32 = 1.0 | was = "multiplier"
}

type Ward
{
    charge float32 = 0.0
}

union Effect
{
    shield Ward | was = "ward"
    pong | was = "ping"
}

table Cfg
{
    grade  Grade
    effect Effect
    buff   Buff
}
`

func TestWasRowsResolve(t *testing.T) {
	u := buildUnit(t, wasRowsRenamedSrc)
	g := u.Enums["Grade"]
	if g.VariantWireName(0) != "Bronze" || g.VariantWireName(1) != "Silver" || g.VariantWireName(2) != "Gold" {
		t.Errorf("variant wire names: %v / %v", g.Variants, g.Was)
	}
	if g.VariantWireNameOf("Argent") != "Silver" || g.VariantWireNameOf("Gold") != "Gold" {
		t.Error("VariantWireNameOf")
	}
	un := u.TableUnions["Effect"]
	if un == nil {
		un = u.Unions["Effect"]
	}
	if un.Variants[0].WireName() != "ward" || un.Variants[0].WasName != "ward" || un.Variants[0].F.WasName != "ward" {
		t.Errorf("arm wire name: %+v", un.Variants[0])
	}
	if !un.Variants[1].Void() || un.Variants[1].WireName() != "ping" {
		t.Errorf("payload-free arm wire name: %+v", un.Variants[1])
	}
	if f := u.Structs["Buff"].Fields[0]; ir.TableFieldWireName(f) != "multiplier" || ir.TableFieldWireId(f) != ir.TableWireId("multiplier") {
		t.Errorf("type field wire name: %+v", f)
	}
	if got := strings.Join(ir.WasRows(u), " "); got != "Buff.mult Effect.pong Effect.shield Grade.Argent" {
		t.Errorf("WasRows: %q", got)
	}
}
