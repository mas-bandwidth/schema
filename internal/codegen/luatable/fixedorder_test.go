package luatable

import (
	"bytes"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestFixedRootsGenerateOneFile holds the fixed form's header to one emission
// per unit. The unit's tables are a map, so a walk over it that is not sorted
// writes the roots in a different order from run to run: the same compiler
// and the same schema would generate different files. Twenty generations make
// an unsorted walk all but certain to show.
func TestFixedRootsGenerateOneFile(t *testing.T) {
	t.Parallel()
	u := luarun.LoadUnit(t, "../../../test/tables/V2.schema")
	if n := len(fixedRoots(u)); n < 2 {
		t.Fatalf("test/tables/V2.schema has %d fixed root(s); the order needs two", n)
	}
	first, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for range 20 {
		again, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		if len(first) == 0 {
			t.Fatal("first is empty: the loop below would check nothing")
		}
		for name, body := range first {
			if !bytes.Equal(body, again[name]) {
				t.Fatalf("%s differs between two generations of one unit", name)
			}
		}
	}
	roots := fixedRoots(u)
	if len(roots) == 0 {
		t.Fatal("roots is empty: the loop below would check nothing")
	}
	for i := 1; i < len(roots); i++ {
		if roots[i-1].Name >= roots[i].Name {
			t.Errorf("fixed roots out of name order: %s before %s", roots[i-1].Name, roots[i].Name)
		}
	}
}
