package ci

// TestNoTestFileIsExcludedByABuildConstraint is issue #1221's gate.
//
// THE CLASS IT CLOSES. A Go file whose name ends in `_<GOOS>.go` or
// `_<GOARCH>.go` carries an IMPLICIT build constraint, and the rule applies to
// test files too: `fixedversioning_darwin_test.go` is a darwin-only test and
// `fixedbounds_js_test.go` is a GOARCH=wasm one. On any other host the
// toolchain does not compile them, does not run them, and SAYS NOTHING — `go
// test ./...` reports `ok` over a file it never opened. A suite that never
// runs is indistinguishable from a suite that passes, which is the one failure
// this repo's gates exist to make impossible. The tokens that bite here are
// ordinary words: `js`, `android`, `plan9`, `wasm`, `arm`, `386`, `s390x`.
//
// The toolchain already knows: `go list` reports a file excluded by a build
// constraint under `IgnoredGoFiles`, whether the constraint was written in a
// `//go:build` line or implied by the name. So the gate asks it, over every
// package under internal/ and test/, and refuses any ignored file whose name
// ends `_test.go`.
//
// A test file that is DELIBERATELY constrained to one platform is not
// unrepresentable — it is just not silent any more. Give it a name that does
// not end in a GOOS or GOARCH token and write the `//go:build` line you mean,
// and this gate will not see it, because a file ignored for a constraint you
// wrote is still ignored: THAT IS THE POINT. If this repo ever wants one, the
// gate grows an explicit allow list with the reason beside each entry, which is
// a line a reviewer can argue with.
//
// THE GATE IS HOST-DEPENDENT ON PURPOSE. `_linux_test.go` is ignored on the
// author's mac and compiled on the CI runner; `_darwin_test.go` is the other
// way round. Both hosts run this gate, so between them they see every such
// file, and neither can be the one host where the silence hides.

import (
	"encoding/json"
	"io"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ignoredScanRoots are the package patterns the gate covers: the compiler and
// its nine code-generation legs, and the Go harnesses under test/.
var ignoredScanRoots = []string{"./internal/...", "./test/..."}

// goListPackage is the sliver of `go list -json` this gate reads.
type goListPackage struct {
	ImportPath      string
	Dir             string
	IgnoredGoFiles  []string
	InvalidGoFiles  []string
	IgnoredOtherFil []string `json:"IgnoredOtherFiles"`
}

func TestNoTestFileIsExcludedByABuildConstraint(t *testing.T) {
	root := repoRoot(t)

	args := append([]string{"list", "-json"}, ignoredScanRoots...)
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("go list -json %s: %v\n%s", strings.Join(ignoredScanRoots, " "), err, stderr)
	}

	dec := json.NewDecoder(strings.NewReader(string(out)))
	var offenders []string
	packages := 0
	for {
		var pkg goListPackage
		if err := dec.Decode(&pkg); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("decode go list -json: %v", err)
		}
		packages++
		for _, name := range pkg.IgnoredGoFiles {
			if !strings.HasSuffix(name, "_test.go") {
				continue
			}
			rel, relErr := filepath.Rel(root, filepath.Join(pkg.Dir, name))
			if relErr != nil {
				rel = filepath.Join(pkg.Dir, name)
			}
			offenders = append(offenders, rel)
		}
	}

	// A gate that scanned nothing is a gate that passes for the wrong reason.
	if packages == 0 {
		t.Fatalf("go list reported no packages under %s: this gate is looking at the wrong tree",
			strings.Join(ignoredScanRoots, " "))
	}
	t.Logf("scanned %d packages under %s", packages, strings.Join(ignoredScanRoots, " "))

	sort.Strings(offenders)
	for _, path := range offenders {
		t.Errorf("%s is a test file the toolchain EXCLUDES on this host and never compiles: "+
			"go list reports it under IgnoredGoFiles, so `go test ./...` prints ok over a test "+
			"that did not run (issue #1221). A name ending in a GOOS or GOARCH token — js, "+
			"android, plan9, wasm, arm, 386, s390x, darwin, linux, windows — carries an implicit "+
			"build constraint. Rename it so the token is not last, or write the //go:build line "+
			"you mean and say here why the file is platform-bound.", path)
	}
}
