package ci

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/testinputs"
)

// TestEveryInterpreterChildRegistersItsInputs holds every _test.go file of
// the module to internal/testinputs: a test that starts sh, bash, make, python
// or node (or a wrapper running one) calls testinputs.Files, Make or Build in
// the same function, or says on the start's line or the line above, in words,
// why the child reads no repository file. Without it, go test replays a
// cached PASS after only the script changed.
//
// IT LIVES HERE, NOT IN internal/testinputs, because ci-fast selects go test
// packages by the diff, and a PR that adds an unregistered child in some other
// package changes nothing under internal/testinputs. The ci-policy job runs
// this package on every pull request, whatever the diff (about 5 s, in its
// `rest` row). testinputs.Check is the reader, and
// TestEveryInterpreterChildRegistersItsInputsPlants there holds its verdicts.
func TestEveryInterpreterChildRegistersItsInputs(t *testing.T) {
	t.Parallel()
	root := testinputs.Root(t)
	files, err := testinputs.GoTestFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 100 {
		t.Fatalf("read %d _test.go files under %s; the walk is broken, and judging none proves nothing", len(files), root)
	}
	starts := 0
	for _, rel := range files {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		bad, err := testinputs.Check(rel, src)
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		for _, b := range bad {
			starts++
			t.Errorf("%s: its inputs are not registered; call testinputs.Files, Make or Build in %s on every repository file the child reads (testinputs.Root registers nothing), or put %q and a reason of at least %d words on the line above when it reads none", b, b.Func, "// "+testinputs.NoneDirective+" —", testinputs.MinReasonWords)
		}
	}
	if starts > 0 {
		t.Logf("%d unregistered interpreter start(s) in %d files", starts, len(files))
	}
}
