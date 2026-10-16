// The `| required` tag (docs/SPEC-TABLES.md §2.3): a table field reads it,
// it is refused where it has no meaning — a packet `type` field and an
// optional `?T` — and it is carried by the field's IR.
package check

import (
	"strings"
	"testing"
)

func TestRequiredTagIsReadAndRefusedWhereItHasNoMeaning(t *testing.T) {
	t.Parallel()
	t.Run("read on a table field", func(t *testing.T) {
		u := checkUnit(t, `package t
table Ship
{
    name string(32) | required
    score int32
}
`)
		st, ok := u.Tables["Ship"]
		if !ok {
			t.Fatal("table Ship missing")
		}
		if len(st.Fields) != 2 {
			t.Fatalf("want 2 fields, got %d", len(st.Fields))
		}
		if !st.Fields[0].Required {
			t.Fatal("field name: want Required=true")
		}
		if st.Fields[1].Required {
			t.Fatal("field score: want Required=false")
		}
	})
	t.Run("refused on a packet type field", func(t *testing.T) {
		errs := runUnit(t, map[string]string{"T.schema": "package t\ntype P\n{\n    x int32 | required\n}\n"})
		if len(errs) == 0 {
			t.Fatal("compiled clean — | required on a `type` field was accepted")
		}
		var got bool
		for _, e := range errs {
			if strings.Contains(e.Error(), "refused on a `type` field") {
				got = true
			}
		}
		if !got {
			t.Fatalf("the refusal does not name the `type` refusal: %v", errs)
		}
	})
	t.Run("refused on an optional ?T", func(t *testing.T) {
		errs := runUnit(t, map[string]string{"T.schema": "package t\ntable Ship\n{\n    tier ?int32 | required\n}\n"})
		if len(errs) == 0 {
			t.Fatal("compiled clean — | required on an optional field was accepted")
		}
		var got bool
		for _, e := range errs {
			if strings.Contains(e.Error(), "refused with ?") {
				got = true
			}
		}
		if !got {
			t.Fatalf("the refusal does not name the optional refusal: %v", errs)
		}
	})
	t.Run("refused on a fixed table by name", func(t *testing.T) {
		errs := runUnit(t, map[string]string{"T.schema": "package t\nfixed table F\n{\n    a int32 | required\n}\n"})
		var got bool
		for _, e := range errs {
			if strings.Contains(e.Error(), "fixed table F: field a is `| required`") {
				got = true
			}
		}
		if !got {
			t.Fatalf("a fixed table must refuse required by name: %v", errs)
		}
	})
}
