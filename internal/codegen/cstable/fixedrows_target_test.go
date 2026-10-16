package cstable

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestCsFixedRowsTargetCoversEveryRoadmapTest pins the single make target that
// runs every C# fixed-roadmap row and the held message form under the required
// toolchain. A row added later without being named in the tables-cs-fixed-rows
// -run pattern goes red here.
func TestCsFixedRowsTargetCoversEveryRoadmapTest(t *testing.T) {
	t.Parallel()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(cwd)))
	mkB, err := os.ReadFile(filepath.Join(root, "make", "cs.mk"))
	if err != nil {
		t.Fatalf("read make/cs.mk: %v", err)
	}
	recipe, ok := csMkTarget(string(mkB), "tables-cs-fixed-rows")
	if !ok {
		t.Fatalf("make/cs.mk has no tables-cs-fixed-rows recipe; the fixed-rows target must exist")
	}
	if !strings.Contains(recipe, "SCHEMA_REQUIRE_CORPUS=1") {
		t.Errorf("tables-cs-fixed-rows go test line does not set SCHEMA_REQUIRE_CORPUS=1")
	}
	run := csMkRunPattern(recipe)
	if run == nil {
		t.Fatalf("tables-cs-fixed-rows recipe has no -run pattern")
	}
	for _, name := range csRoadmapTestNames(t, root) {
		if !run.MatchString(name) {
			t.Errorf("-run pattern %q does not match roadmap test %s", run.String(), name)
		}
	}
}

// csMkTarget returns the recipe lines (tab-led lines) of the named target in a
// make fragment, or false when the target is absent.
func csMkTarget(mk, name string) (string, bool) {
	lines := strings.Split(mk, "\n")
	start := -1
	for i, ln := range lines {
		if strings.HasPrefix(ln, name+":") {
			start = i
			break
		}
	}
	if start < 0 {
		return "", false
	}
	var b strings.Builder
	for _, ln := range lines[start+1:] {
		if !strings.HasPrefix(ln, "\t") {
			break
		}
		b.WriteString(ln)
		b.WriteByte('\n')
	}
	return b.String(), true
}

// csMkRunPattern extracts the -run regex from a make recipe line.
func csMkRunPattern(recipe string) *regexp.Regexp {
	for _, ln := range strings.Split(recipe, "\n") {
		i := strings.Index(ln, "-run")
		if i < 0 {
			continue
		}
		rest := ln[i+len("-run"):]
		rest = strings.TrimLeft(rest, " \t=")
		if len(rest) >= 2 && rest[0] == '\'' {
			end := strings.IndexByte(rest[1:], '\'')
			if end < 0 {
				continue
			}
			pat := rest[1 : 1+end]
			return regexp.MustCompile(pat)
		}
	}
	return nil
}

// csRoadmapTestNames lists every top-level TestFixedRoadmap* and
// TestMessageFormHeldCSharp function in the package's _test.go files.
func csRoadmapTestNames(t *testing.T, root string) []string {
	t.Helper()
	pkgDir := filepath.Join(root, "internal", "codegen", "cstable")
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	re := regexp.MustCompile(`^func (TestFixedRoadmap\w+|TestMessageFormHeldCSharp)\(t \*testing.T\)`)
	var names []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, "_test.go") || name == "fixedrows_target_test.go" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(pkgDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, ln := range strings.Split(string(b), "\n") {
			if m := re.FindStringSubmatch(ln); m != nil {
				names = append(names, m[1])
			}
		}
	}
	return names
}
