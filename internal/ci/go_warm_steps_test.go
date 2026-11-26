package ci

// A WARM STEP THAT CANNOT LIST ITS PACKAGES FAILS. The warm steps of
// build/conformance-go (make/go.mk) take a quarter of `go list -deps` and
// compile it. A pipe from go list into awk takes its status from awk, so a go
// list that failed (a generated module missing) left the quarter empty, which
// the recipe takes for a quarter with nothing to do: the step touched its stamp
// having warmed nothing. The list is read on its own, and a failure of it fails
// the recipe.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// checkWarmRecipeReadsTheListAlone: in the recipe lines of src that run go
// list -deps, the list is assigned on its own under && (so its status stops the
// recipe) and never piped on.
func checkWarmRecipeReadsTheListAlone(src string) []string {
	var bad []string
	n := 0
	for i, line := range strings.Split(src, "\n") {
		if !strings.Contains(line, "go list -deps") || !strings.HasPrefix(line, "\t") {
			continue
		}
		n++
		_, after, _ := strings.Cut(line, "go list -deps")
		listCmd, _, _ := strings.Cut(after, ") &&")
		if strings.Contains(listCmd, "|") {
			bad = append(bad, "make/go.mk:"+strconv.Itoa(i+1)+": go list -deps is piped on, so its failure is lost: read it into a variable under &&")
		}
		if !strings.Contains(line, "=$$(go list -deps") {
			bad = append(bad, "make/go.mk:"+strconv.Itoa(i+1)+": go list -deps is not assigned to a variable under &&")
		}
	}
	if n == 0 {
		bad = append(bad, "no recipe line runs go list -deps")
	}
	return bad
}

func TestWarmStepsFailWhenGoListFails(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "make", "go.mk"))
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range checkWarmRecipeReadsTheListAlone(string(b)) {
		t.Error(msg)
	}
	// the fixture pair: the piped form is caught, the assigned form passes
	piped := "\tcd x && pkgs=$$(go list -deps -f 'f' . | awk 'p') && { go build $$pkgs; }\n"
	if got := checkWarmRecipeReadsTheListAlone(piped); len(got) == 0 {
		t.Errorf("the piped go list was not caught")
	}
	good := "\tcd x && all=$$(go list -deps -f 'f' .) && pkgs=$$(printf '%s\\n' \"$$all\" | awk 'p') && { go build $$pkgs; }\n"
	if got := checkWarmRecipeReadsTheListAlone(good); len(got) != 0 {
		t.Errorf("the assigned go list was refused: %v", got)
	}
}
