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
	"fmt"
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

// cookEnumAssertHookProblems checks every *Table.h in files for the cook
// enum refusal and the hook it needs in scope. Generate runs in process, so
// the headers need no build step; a set with no *Table.h at all is a problem
// in itself, never a pass that checked nothing.
func cookEnumAssertHookProblems(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		if strings.HasSuffix(name, "Table.h") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		all := make([]string, 0, len(files))
		for name := range files {
			all = append(all, name)
		}
		sort.Strings(all)
		return []string{fmt.Sprintf("0 *Table.h headers generated (files: %v); the check would pass having checked nothing", all)}
	}
	var problems []string
	for _, name := range names {
		src := string(files[name])
		use := strings.Index(src, "schema_assert( !(")
		if use < 0 {
			problems = append(problems, name+": no cook enum refusal emitted; the probe no longer exercises the hook")
			continue
		}
		def := strings.Index(src, "#define schema_assert assert")
		if def < 0 || def > use {
			problems = append(problems, fmt.Sprintf("%s: schema_assert is called before any #define schema_assert (define at %d, first use at %d)", name, def, use))
		}
		if !strings.Contains(src, "#include <assert.h>") {
			problems = append(problems, name+": schema_assert is called but <assert.h> is not included")
		}
	}
	return problems
}

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
	for _, p := range cookEnumAssertHookProblems(files) {
		t.Error(p)
	}
}

// The check above must not go green on a generated set with no *Table.h:
// zero headers checked is a red, by name.
func TestCCookEnumArmAssertHookRefusesNoHeaders(t *testing.T) {
	t.Parallel()
	for _, files := range []map[string][]byte{
		nil,
		{"ProbeTable.c": []byte("x"), "ProbeView.h": []byte("x"), "ProbeBlock.h": []byte("x")},
	} {
		problems := cookEnumAssertHookProblems(files)
		if len(problems) != 1 || !strings.HasPrefix(problems[0], "0 *Table.h headers generated") {
			t.Errorf("files %v: problems %q, want the one 0-headers refusal", files, problems)
		}
	}
}
