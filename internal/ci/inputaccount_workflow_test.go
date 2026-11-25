package ci

import (
	"regexp"
	"strings"
	"testing"
)

// The accounting consumer also runs Dart in JIT mode and needs the generated
// source trees imported by the driver.
func TestDartConformanceHandoffIncludesGeneratedImports(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	driver := readCI(t, root, "test/conformance/dart/main.dart")
	helper := readCI(t, root, "make/conformance-handoff.py")
	_, paths, ok := strings.Cut(helper, "\nDART_PATHS = tuple(\n")
	if !ok {
		t.Fatal("handoff helper has no DART_PATHS manifest")
	}
	paths, _, ok = strings.Cut(paths, "\n)\n")
	if !ok {
		t.Fatal("handoff helper has no end to its DART_PATHS manifest")
	}
	imports := regexp.MustCompile(`(?m)^import '\.\./\.\./\.\./build/([^/]+)/`).FindAllStringSubmatch(driver, -1)
	if len(imports) == 0 {
		t.Fatal("Dart conformance driver has no generated imports")
	}
	checked := make(map[string]bool)
	for _, match := range imports {
		dir := match[1]
		if !checked[dir] && !strings.Contains(paths, `"`+dir+`"`) {
			t.Errorf("Dart handoff manifest is missing imported build directory %s", dir)
		}
		checked[dir] = true
	}
}
