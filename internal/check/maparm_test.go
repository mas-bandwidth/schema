// A MAP IS NOT A UNION ARM, and a table that holds it is the repair
// (docs/SPEC-TABLES.md §2.6, §2.8, §11): the refusal is the one `[]T` already
// takes at an arm, and it names the union, the arm and the table wrapper.
// A table arm whose table declares the same map still passes.
package check

import (
	"strings"
	"testing"
)

func TestMapAsUnionArmIsRefusedByName(t *testing.T) {
	t.Parallel()
	const want = "a map is not an arm"
	errs := runUnit(t, map[string]string{
		"Bad.schema": "package t\nunion U { m map[uint8]int32 }\ntable Root { u U }\n",
	})
	var got string
	for _, e := range errs {
		if strings.Contains(e.Error(), want) {
			got = e.Error()
			break
		}
	}
	if got == "" {
		t.Fatalf("the refusal went missing: %v", errs)
	}
	for _, part := range []string{"union U: arm m", "table"} {
		if !strings.Contains(got, part) {
			t.Errorf("the refusal does not name %q: %s", part, got)
		}
	}

	// the repair: an arm names a table that holds the map
	good := "package t\ntable Holder { m map[uint8]int32 }\nunion U { m Holder }\ntable Root { u U }\n"
	if errs := runUnit(t, map[string]string{"Good.schema": good}); len(errs) > 0 {
		t.Fatalf("a table arm holding the map was refused: %v", errs)
	}
}
