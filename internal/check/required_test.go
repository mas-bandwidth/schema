// The `| required` tag is READ into the ir on a variable-table field and
// REFUSED, by name, everywhere it has no meaning (docs/SPEC-TABLES.md §4.3):
// a packet `type` field (no table wire to be present on), an optional `?T`
// (the presence companion already says it, and the two contradict), a FIXED
// table (every fixed field always rides, §3.4), and a shape whose ride is a
// body or a count (a named follow-on, §15).
package check

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

func requiredCheckErrs(t *testing.T, src string) []string {
	t.Helper()
	f, perrs := parser.Parse("T.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs)
	}
	_, errs := Unit([]SourceFile{{Path: "T.schema", Name: "T.schema", Base: "T", Bytes: []byte(src), AST: f}})
	out := make([]string, 0, len(errs))
	for _, e := range errs {
		out = append(out, e.Error())
	}
	return out
}

func namesRequiredRefusal(errs []string, field string) bool {
	for _, e := range errs {
		if strings.Contains(e, "required") && strings.Contains(e, field) {
			return true
		}
	}
	return false
}

func TestRequiredTagIsReadAndRefusedWhereItHasNoMeaning(t *testing.T) {
	t.Parallel()

	// READ: the tag sets the flag on a variable table's field and on nothing
	// else — the same reading path `| deprecated` rides (§4.3).
	u := checkUnit(t, `package t

table Ship
{
    health int32 | required
    ammo   int32
}
`)
	st := u.Tables["Ship"]
	if st == nil {
		t.Fatal("the checked unit lost table Ship")
	}
	if !st.Fields[0].Required {
		t.Error("field health: `| required` was not read into ir.Field.Required (docs/SPEC-TABLES.md §4.3)")
	}
	if st.Fields[1].Required {
		t.Error("field ammo: Required set without the tag")
	}

	// REFUSED on a packet `type` field, by name.
	errs := requiredCheckErrs(t, `package t

type Vec3
{
    x float64 | required
    y float64
    z float64
}
`)
	if !namesRequiredRefusal(errs, "x") {
		t.Errorf("`| required` on a packet type field was not refused by name; errs = %v", errs)
	}

	// REFUSED on an optional `?T`: the two markers contradict (§2.3, §4.3).
	errs = requiredCheckErrs(t, `package t

table Inner
{
    a int32
}

table Ship
{
    settings ?Inner | required
}
`)
	if !namesRequiredRefusal(errs, "settings") {
		t.Errorf("`| required` on an optional ?T was not refused by name; errs = %v", errs)
	}

	// REFUSED in a FIXED table: every fixed field always rides (§3.4), so the
	// marker is meaningless there and the compiler says so by name.
	errs = requiredCheckErrs(t, `package t

fixed table Ship
{
    health int32 | required
}
`)
	if !namesRequiredRefusal(errs, "health") {
		t.Errorf("`| required` in a fixed table was not refused by name; errs = %v", errs)
	}
}
