// TestSpecPortingReadVerdictEveryBuild pins docs/PORTING.md's two rules for a
// porter: the READ verdict is the same in every build mode a language offers,
// and the write side keeps docs/rulings.json write-refusal's per-leg table; and
// freeing what a load accepted drops a pointer chain without recursion. The
// page must not claim a write refusal in every build in every leg.
package compiler

import (
	"os"
	"testing"
)

func TestSpecPortingReadVerdictEveryBuild(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../docs/PORTING.md")
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)

	wants := []string{
		"the same READ verdict in every build mode its language offers",
		"a release and a checked or debug build, an optimised and an overflow-checking one",
		"the port's gates run each mode the language offers",
		"Write-side checks follow `docs/rulings.json` write-refusal's per-leg table and are not changed by this rule",
		"`loading never crashes on input that loads` covers freeing what was loaded",
		"a leg whose values own their pointers drops a loaded pointer chain without recursion",
		"a chain the reader accepts never overflows the stack when it is freed",
	}
	stales := []string{
		"writers refuse in every build in every leg",
		"the write verdict is the same in every build",
	}
	for _, want := range wants {
		mustState(t, "docs/PORTING.md", page, want)
	}
	for _, stale := range stales {
		mustNotState(t, "docs/PORTING.md", page, stale)
	}
}
