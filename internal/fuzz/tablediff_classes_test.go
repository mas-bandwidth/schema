package fuzz_test

// tablediff_classes_test.go holds the random-schema differential's own
// classifier to account (card schema-go-fuzz-random-table-differential): an
// expected class may file a divergence line only when that line is exactly the
// owed difference and nothing else, every class names a card that exists, and
// the campaign runs only where make fuzz-tables-diff asks for it. These are
// pure unit tests: no toolchain, no gate.

import (
	"os"
	"strings"
	"testing"
)

// verifiedClassCards is every card id an expected class may name, each looked
// up with `nova-sprint card <id>`. A class naming anything else is owed by
// nobody.
var verifiedClassCards = map[string]string{}

func classUnit() *genUnit {
	return &genUnit{Root: &genTable{Name: "Root", Fields: []*genField{
		{Name: "t", Kind: gTimestamp},
		{Name: "b", Kind: gBool},
		{Name: "i", Kind: gInt, Bits: 64},
		{Name: "f", Kind: gFloat64},
		{Name: "s", Kind: gString, StrN: 40},
		{Name: "m", Kind: gMap, MapKey: &genField{Kind: gString, StrN: 40}, Elem: &genField{Name: "v", Kind: gInt, Bits: 32}},
	}}}
}

// maskedBy returns the id of the first expected class that files line, or "".
func maskedBy(u *genUnit, line string) string {
	for _, c := range expectedClasses {
		if c.match(u, line) {
			return c.id
		}
	}
	return ""
}

func TestExpectedClassesNameRealCards(t *testing.T) {
	t.Parallel()
	for _, c := range expectedClasses {
		if _, ok := verifiedClassCards[c.id]; !ok {
			t.Errorf("expected class %q names no card found by nova-sprint card", c.id)
		}
	}
	// A class may match only a line kind the probe records: the probe never
	// writes "wrongvalue", so a class matching it is dead.
	if strings.Contains(tableDifferentialProbe, `"wrongvalue`) {
		t.Fatalf("the probe records wrongvalue lines now; update this test")
	}
	u := classUnit()
	if id := maskedBy(u, "wrongvalue 3"); id != "" {
		t.Errorf("class %s matches a wrongvalue line the probe never records", id)
	}
}

func TestTableDiffRunsOnlyWhenOptedIn(t *testing.T) {
	t.Parallel()
	env := func(kv ...string) func(string) string {
		m := map[string]string{}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return func(k string) string { return m[k] }
	}
	if tableDiffOptedIn(env("SCHEMA_SLOW", "1")) {
		t.Errorf("SCHEMA_SLOW=1 alone (ci-full) runs the table differential")
	}
	if tableDiffOptedIn(env("SCHEMA_REQUIRE_CORPUS", "1")) {
		t.Errorf("SCHEMA_REQUIRE_CORPUS alone runs the table differential")
	}
	if !tableDiffOptedIn(env("SCHEMA_SLOW", "1", "SCHEMA_TABLE_DIFF", "1")) {
		t.Errorf("SCHEMA_TABLE_DIFF=1 does not run the table differential")
	}
}

// TestCiFullKeepsFuzzAndOnlyMakeOptsIn reads the workflow and make files: ci-full
// runs ./internal/fuzz (crasher corpus, property and oracle tests), and the one
// line that sets SCHEMA_TABLE_DIFF=1 anywhere is the fuzz-tables-diff recipe.
func TestCiFullKeepsFuzzAndOnlyMakeOptsIn(t *testing.T) {
	t.Parallel()
	ci, err := os.ReadFile("../../.github/workflows/ci-full.yml")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for line := range strings.SplitSeq(string(ci), "\n") {
		if strings.Contains(line, "run: go test $(go list ./...") && strings.Contains(line, "grep -v '/internal/codegen/'") {
			found = true
			if strings.Contains(line, "/internal/fuzz") {
				t.Errorf("ci-full's unit-rest go test drops ./internal/fuzz: %s", strings.TrimSpace(line))
			}
		}
	}
	if !found {
		t.Errorf("ci-full.yml has no unit-rest go test over the non-emitter packages")
	}
	files := []string{"../../.github/workflows/ci-full.yml", "../../.github/workflows/ci-fast.yml", "../../.github/workflows/certify.yml", "../../Makefile"}
	mk, err := os.ReadDir("../../make")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range mk {
		if strings.HasSuffix(e.Name(), ".mk") {
			files = append(files, "../../make/"+e.Name())
		}
	}
	setters := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatal(err)
		}
		inTarget := false
		for line := range strings.SplitSeq(string(b), "\n") {
			if !strings.HasPrefix(line, "\t") {
				inTarget = strings.HasPrefix(line, "fuzz-tables-diff:")
			}
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue // a comment sets nothing
			}
			if strings.Contains(line, "SCHEMA_TABLE_DIFF:") || strings.Contains(line, "SCHEMA_TABLE_DIFF=1") {
				if f != "../../make/go.mk" || !inTarget {
					t.Errorf("%s sets SCHEMA_TABLE_DIFF outside make fuzz-tables-diff: %s", f, strings.TrimSpace(line))
					continue
				}
				setters++
			}
		}
	}
	if setters != 2 {
		t.Errorf("make fuzz-tables-diff sets SCHEMA_TABLE_DIFF=1 on %d recipe lines, want 2 (the seeds and the negative control)", setters)
	}
}
