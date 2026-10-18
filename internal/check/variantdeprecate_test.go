// A deprecated ENUM VARIANT is retired in place (docs/SPEC-TABLES.md §2.15):
// a field default or an enum-keyed array that names one is a NEW USE and is
// refused by name; a live variant in the same position is accepted.
package check

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

func variantCheckErrors(t *testing.T, src string) []string {
	t.Helper()
	f, perrs := parser.Parse("T.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	_, cerrs := Unit([]SourceFile{{
		Path: "T.schema", Name: "T.schema", Base: "T", Bytes: []byte(src), AST: f,
	}})
	out := make([]string, 0, len(cerrs))
	for _, e := range cerrs {
		out = append(out, e.Error())
	}
	return out
}

const retiredTier = `package t

enum Tier
{
    Bronze
    Silver | deprecated
    Gold
}
`

func TestVariantDeprecatedDefaultRefused(t *testing.T) {
	t.Parallel()
	src := retiredTier + `
table Tab
{
    tier Tier = Silver
}
`
	errs := variantCheckErrors(t, src)
	if len(errs) == 0 {
		t.Fatal("a field default naming a deprecated variant must be refused")
	}
	got := errs[0]
	for _, want := range []string{"tier", "Tier", "Silver", "deprecated", "docs/SPEC-TABLES.md §2.15"} {
		if !strings.Contains(got, want) {
			t.Errorf("the refusal must name %q:\n%s", want, got)
		}
	}

	// REVERSED WITNESS: the same default on a variant that was never
	// deprecated is accepted, so the refusal above is the rule and not the
	// default production.
	live := `package t

enum Tier
{
    Bronze
    Silver
    Gold
}

table Tab
{
    tier Tier = Silver
}
`
	if errs := variantCheckErrors(t, live); len(errs) != 0 {
		t.Fatalf("a default naming a live variant must be accepted: %v", errs)
	}
}

func TestVariantDeprecatedKeyBoundRefused(t *testing.T) {
	t.Parallel()
	src := retiredTier + `
table Tab
{
    slots [Tier]int32
}
`
	errs := variantCheckErrors(t, src)
	if len(errs) == 0 {
		t.Fatal("an enum-keyed array naming a deprecated variant must be refused")
	}
	got := errs[0]
	for _, want := range []string{"slots", "Tier", "Silver", "deprecated", "docs/SPEC-TABLES.md §2.15"} {
		if !strings.Contains(got, want) {
			t.Errorf("the refusal must name %q:\n%s", want, got)
		}
	}
}
