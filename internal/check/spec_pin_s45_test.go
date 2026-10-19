// Per-row witnesses for docs/SPEC-TABLES.md §5 (identity: the name hash,
// `was`, and the collision refusal). Each subtest is named for the row it
// pins, so a row's pin names a test that asserts that sentence and not a
// neighbouring refusal.
package check

import (
	"strings"
	"testing"
)

func TestSpecPinSectionFiveRows(t *testing.T) {
	t.Parallel()
	cases := []struct {
		row  string
		name string
		want string
		src  string
	}{
		{"T5-4", "was naming the field's own current name",
			"names the field's own current name",
			"package t\ntable Tab { speed float32 | was = \"speed\" }\n"},
		{"T5-4", "was outside a table body",
			"was is a table-wire concept",
			"package t\ntype P { speed float32 | was = \"velocity\" }\n"},
		{"T5-5", "two fields of one table colliding on the wire id",
			"collide on table-wire id",
			"package t\ntable Tab {\n    a int32 | was = \"b\"\n    b int32\n}\n"},
		{"T5-6", "a collision names both fields",
			"collide on table-wire id",
			"package t\ntable Tab {\n    a int32 | was = \"b\"\n    b int32\n}\n"},
		{"T5-7", "a table's was naming its own name",
			"names the table's own current name",
			"package t\ntable Tab | was = \"Tab\"\n{\n    x int32\n}\n"},
		{"T5-7", "a table's was with an empty string",
			"names nothing",
			"package t\ntable Tab | was = \"\"\n{\n    x int32\n}\n"},
		{"T5-7", "a table's was colliding with a live table",
			"collide on table-wire type id",
			"package t\ntable Old { x int32 }\ntable Tab | was = \"Old\"\n{\n    y int32\n}\ntable Root\n{\n    a *Old\n    b *Tab\n}\n"},
		{"T5-7", "was on a type declaration",
			"was on a type declaration is refused",
			"package t\ntype P | was = \"Q\"\n{\n    x int32\n}\n"},
		{"T5-10", "a variant's was naming its own name",
			"names the variant's own current name",
			"package t\nenum E\n{\n    A | was = \"A\"\n}\ntable Tab { e E }\n"},
		{"T5-10", "a variant's was outside a table closure",
			"no table reaches E",
			"package t\nenum E\n{\n    A | was = \"Z\"\n}\ntype P { e E }\n"},
		{"T5-10", "an arm's was colliding with a live arm",
			"collide on table-wire id",
			"package t\ntype A { x int32 }\nunion U\n{\n    a A\n    b A | was = \"a\"\n}\ntable Tab { u U }\n"},
		{"T5-11", "a flags variant takes no was",
			"takes no was",
			"package t\nflags F\n{\n    A | was = \"Z\"\n}\ntable Tab { f F }\n"},
		{"T5-12", "a type field's was outside a table closure",
			"no table reaches P",
			"package t\ntype P { speed float32 | was = \"velocity\" }\n"},
		{"T5-12", "a type field's was colliding inside the type",
			"collide on table-wire id",
			"package t\ntype P\n{\n    a int32\n    b int32 | was = \"a\"\n}\ntable Tab { p P }\n"},
		{"T5-13", "enum headroom has no name to ride under",
			"headroom value has no NAME",
			"package t\nenum E | max = 8\n{ A, B }\ntable Tab { e E }\n"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.row+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			errs := runUnit(t, map[string]string{"T.schema": tc.src})
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

// TestSpecPinSectionFiveRenameKeepsIdentity is T5-3's accepting side: a rename
// is only safe when it is declared, and the declared `was` keeps the OLD name's
// hash on the wire.
func TestSpecPinSectionFiveRenameKeepsIdentity(t *testing.T) {
	t.Parallel()
	u := buildUnit(t, "package t\ntable Tab { speed float32 | was = \"velocity\" }\n")
	f := u.Tables["Tab"].Fields[0]
	if f.WasName != "velocity" {
		t.Fatalf("the declared rename did not record its old name: %+v", f)
	}
}
