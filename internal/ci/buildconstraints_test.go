package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoGoFileIsIgnoredByItsNameAlone refuses any committed .go file the
// toolchain declines to compile because of its filename alone — a name ending
// in a GOOS or GOARCH token like _arm, _386 or _js — while permitting one the
// author ignored on purpose with an explicit //go:build or // +build line.
//
// The class it closes (issue #1221): a file named
// fixedversioning_union_unselected_arm_test.go ends in _arm, so every non-arm
// build files it under IgnoredGoFiles and never compiles it, while the row
// gate prints a green `ok ... [no tests to run]`. An ignored test is green
// under every negative control, including the ones that break the runtime it
// was written for. The toolchain's own word for it is IgnoredGoFiles, so the
// gate asks `go list` for that list and reads each file's leading lines.
func TestNoGoFileIsIgnoredByItsNameAlone(t *testing.T) {
	root := repoRoot(t)

	// Text, not JSON: `go list -json` prints a concatenated stream of objects,
	// and `-e` keeps one broken package from killing the whole listing.
	cmd := exec.Command("go", "list", "-e", "-f",
		"{{.Dir}}|{{range .IgnoredGoFiles}}{{.}} {{end}}", "./...")
	cmd.Dir = root
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -e ./... failed: %v\n%s", err, stderr.String())
	}

	type pkg struct {
		dir     string
		ignored []string
	}
	var packages []pkg
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		dir, rest, ok := strings.Cut(line, "|")
		if !ok {
			continue
		}
		packages = append(packages, pkg{dir: dir, ignored: strings.Fields(rest)})
	}
	if len(packages) == 0 {
		t.Fatal("go list ./... found no packages — this gate would pass over an empty set")
	}

	for _, p := range packages {
		for _, name := range p.ignored {
			path := filepath.Join(p.dir, name)
			rel, err := filepath.Rel(root, path)
			if err != nil {
				rel = path
			}
			if hasExplicitBuildConstraint(t, path) {
				t.Logf("allowing %s: it carries an explicit //go:build or // +build line", rel)
				continue
			}
			t.Errorf("%s is ignored by the toolchain because of its name alone — rename it, or add an explicit //go:build line if you meant it", rel)
		}
	}
}

// hasExplicitBuildConstraint reports whether the file's leading lines — the
// ones above the package clause — carry an explicit build constraint, which is
// the signal that the author meant to exclude it. It stops at the first line
// that begins `package `, and does not parse the constraint itself.
func hasExplicitBuildConstraint(t *testing.T, path string) bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "package ") {
			return false
		}
		if strings.HasPrefix(line, "//go:build") || strings.HasPrefix(line, "// +build") {
			return true
		}
	}
	return false
}
