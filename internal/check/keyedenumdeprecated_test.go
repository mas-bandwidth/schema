package check

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

func checkerErrors(t *testing.T, src string) []string {
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

func checkerErrorsFor(t *testing.T, src string, target string) []string {
	t.Helper()
	f, perrs := parser.Parse("T.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	_, cerrs := UnitForTargets([]SourceFile{{
		Path: "T.schema", Name: "T.schema", Base: "T", Bytes: []byte(src), AST: f,
	}}, []string{target})
	out := make([]string, 0, len(cerrs))
	for _, e := range cerrs {
		out = append(out, e.Error())
	}
	return out
}

const keyedDeprecatedHolder = `fixed table SomeTable
{
    x int32
}
`

func TestKeyedEnumWithADeprecatedVariantIsRefusedForEveryTarget(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		table string
		field string
		src   string
	}{
		{"variable-int32", "table Var", "ints", "ints [Tier]int32"},
		{"variable-table", "table Var", "tables", "tables [Tier]SomeTable"},
		{"fixed-int32", "fixed table Fix", "ints", "ints [Tier]int32"},
		{"fixed-table", "fixed table Fix", "tables", "tables [Tier]SomeTable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := retiredTier + keyedDeprecatedHolder + "\n" + tc.table + "\n{\n    " + tc.src + "\n}\n"
			errs := checkerErrors(t, src)
			if len(errs) == 0 {
				t.Fatalf("%s: an enum-keyed array whose enum has a deprecated variant must be refused", tc.src)
			}
			got := errs[0]
			for _, want := range []string{tc.field, "Tier", "Silver", "deprecated", "docs/SPEC-TABLES.md §2.15"} {
				if !strings.Contains(got, want) {
					t.Errorf("the refusal must name %q:\n%s", want, got)
				}
			}
			if !strings.Contains(got, "the key bound [Tier] names variant Silver, which is marked `| deprecated`") {
				t.Errorf("the refusal must open with the key-bound rule:\n%s", got)
			}

			for _, target := range []string{"Go", "C++"} {
				targetErrs := checkerErrorsFor(t, src, target)
				if len(targetErrs) == 0 {
					t.Fatalf("%s: target %s must return the checker's refusal", tc.src, target)
				}
				if targetErrs[0] != got {
					t.Fatalf("%s: target %s returned a different refusal:\n%s\nwant:\n%s", tc.src, target, targetErrs[0], got)
				}
			}
		})
	}
}

func TestDeprecatedEnumVariantDefaultingFormsAreAccepted(t *testing.T) {
	t.Parallel()

	src := retiredTier + `
table Witness
{
    e  Tier
    es [4]Tier
    fs [..4]Tier
    m  map[uint32]Tier
}
`
	if errs := checkerErrors(t, src); len(errs) != 0 {
		t.Fatalf("scalar, array and map-value uses of a deprecated variant must be accepted: %v", errs)
	}

	live := `package t

enum Live
{
    A
    B
    C
}

table Keyed
{
    slots [Live]int32
}
`
	if errs := checkerErrors(t, live); len(errs) != 0 {
		t.Fatalf("a keyed array over an enum with no deprecated variant must be accepted: %v", errs)
	}
}

func TestDeprecatedEnumTextListWitnessUsesExistingRefusals(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		table string
		want  string
	}{
		{"variable", "table Var", "is refused — a text list is COUNTED"},
		{"fixed", "fixed table Fix", "a text list is a VARIABLE-table construct"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := retiredTier + "\n" + tc.table + "\n{\n    texts [Tier]string(8)\n}\n"
			errs := checkerErrors(t, src)
			if len(errs) == 0 || !strings.Contains(errs[0], tc.want) {
				t.Fatalf("expected existing text-list diagnostic containing %q, got %v", tc.want, errs)
			}
		})
	}
}
