// A KEYED [Enum]T is refused by the shared checker as soon as its enum has ANY
// deprecated variant, for every target, whether or not the key names that
// variant (docs/SPEC-TABLES.md §2.15). Deprecated-slot defaulting covers the
// SCALAR enum field and the ARRAY and MAP forms whose elements or map keys are
// enum values; the keyed array is not one of them.
package check

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

// checkerErrors runs the package's own check entry point with no target, so a
// refusal here is the shared checker's and precedes any emitter.
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

// checkerErrorsFor runs the same checker narrowed to one target language; the
// key-bound refusal is target-independent, so the answer must not move.
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

	// Silver is deprecated and NOT named by any field default or const, so the
	// key bound is the only use the checker can be refusing. `[E]string(8)` also
	// spells a text list, but the key-bound rule speaks first, so even that
	// spelling carries the §2.15 diagnostic (docs/SPEC-TABLES.md §2.13, §2.15).
	cases := []struct {
		name  string
		table string
		field string
		src   string
	}{
		{"variable-int32", "table Var", "ints", "ints [Tier]int32"},
		{"variable-string", "table Var", "texts", "texts [Tier]string(8)"},
		{"variable-table", "table Var", "tables", "tables [Tier]SomeTable"},
		{"fixed-int32", "fixed table Fix", "ints", "ints [Tier]int32"},
		{"fixed-string", "fixed table Fix", "texts", "texts [Tier]string(8)"},
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

			// The same refusal comes out of the checker for each target: it runs
			// before any emitter, so go and cpp cannot disagree.
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

	// The same enum as a SCALAR field, an ARRAY element and a MAP value: the
	// defaulting forms, which the checker accepts. A map key is a defaulting
	// form too, but it is covered by the existing map rules; the witness here
	// is the element/value set the READ rule names.
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

	// REVERSED WITNESS: the SAME keyed spelling over an enum with no deprecated
	// variant is accepted, so the refusals above are the marker's rule and not
	// the keyed form's.
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
