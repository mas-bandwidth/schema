package lobstertable

import (
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
)

// Union payloads overlay their arm storage, so distinct lexical occurrences
// can have the same byte offset. Lobster still forbids reusing their locals.
func TestLobsterFixedNestedUnionGuardNames(t *testing.T) {
	u := fixedproof.ParseCheckUnit(t, `package probe
union Inner {
    value uint8
}
union Outer {
    nested Inner
    again Inner
}
fixed table Root {
    first Outer
    second Outer
    optional ?[2]Outer
}
`)
	files, err := lobster.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	seen := map[string]bool{}
	for line := range strings.SplitSeq(string(tables["probeTable.lobster"]), "\n") {
		words := strings.Fields(line)
		if len(words) < 2 || (words[0] != "let" && words[0] != "var") || (!strings.HasPrefix(words[1], "grd_at_") && !strings.HasPrefix(words[1], "q_")) {
			continue
		}
		if seen[words[1]] {
			t.Fatalf("duplicate union guard local %s", words[1])
		}
		seen[words[1]] = true
	}
	if len(seen) == 0 {
		t.Fatal("no union guard locals emitted")
	}
	out, _ := lobsterEvolutionRun(t, files, `import .probeTable
let a = probe.NewRoot()
a.First.Type = probe.OuterTypeNested
a.First.Nested.Type = probe.InnerTypeValue
a.First.Nested.Value = 7
a.Second.Type = probe.OuterTypeAgain
a.Second.Again.Type = probe.InnerTypeValue
a.Second.Again.Value = 9
let wire = probe.RootFixedSave([a])
let back:[probe.Root] = [probe.NewRoot()]
let plan:[schema_rt.TableFixedEntry] = []
for(4096): push(plan, schema_rt.TableFixedEntry {})
let report = schema_rt.TableReport {}
assert(probe.RootFixedLoad(back, wire, plan, report) == 1)
assert(back[0].First.Nested.Value == 7)
assert(back[0].Second.Again.Value == 9)
assert(probe.RootFixedSave(back) == wire)
print("PASS")
`, nil)
	if strings.TrimSpace(out) != "PASS" {
		t.Fatalf("native nested-union probe: %s", out)
	}
}
