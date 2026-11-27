// THE COOK WRITER'S ENUM REFUSAL NEEDS THE ASSERT HOOK IN SCOPE (docs/SPEC.md
// §5.1 row 5; SPEC-TABLES §2.15, §7.6; census group G18).
//
// C is an assert leg: the cook writer refuses an enum ordinal past its variant
// set, or a deprecated one, with schema_assert. A unit whose only write check
// is that refusal — here an enum reached through a union arm of a table, the
// name-buildability probe u_enum_arm_table — emitted the call without
// <assert.h> or the schema_assert define, so the header did not compile.
package ctable

import (
	"sort"
	"strings"
	"testing"
)

const cCookEnumArmSchema = `package probe

enum Mode
{ Alpha, Beta, Gamma }

union U
{ mode Mode }

table Rec
{
    pick U
}
`

func TestCCookEnumArmDefinesAssertHook(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, cCookEnumArmSchema)
	if !unitHasWriteBound(u) {
		t.Error("unitHasWriteBound answers no for a unit whose cook writer asserts on an enum arm")
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !strings.HasSuffix(name, "Table.h") {
			continue
		}
		src := string(files[name])
		if !strings.Contains(src, "schema_assert( !(") {
			t.Fatalf("%s: no cook enum refusal emitted; the probe no longer exercises the hook", name)
		}
		def := strings.Index(src, "#define schema_assert assert")
		use := strings.Index(src, "schema_assert( !(")
		if def < 0 || def > use {
			t.Errorf("%s: schema_assert is called before any #define schema_assert (define at %d, first use at %d)", name, def, use)
		}
		if !strings.Contains(src, "#include <assert.h>") {
			t.Errorf("%s: schema_assert is called but <assert.h> is not included", name)
		}
	}
}
