package ci

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestConformanceLuauDependsOnEveryRuntimeFile holds build/conformance-luau to
// rebuilding when the runtime it emits changes. The rule once named only the
// manifest and make/luau.mk, so a planted bug in
// internal/codegen/luautable/runtime/load.luau left a stale build that every
// Luau gate then read as green.
//
// Text only: it reads make/luau.mk and make/luau-conformance.mk, no make run.
func TestConformanceLuauDependsOnEveryRuntimeFile(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	literal := map[string]bool{}
	var globs []string
	wildcard := regexp.MustCompile(`\$\(wildcard\s+([^)]+)\)`)
	found := false
	for _, name := range []string{"luau.mk", "luau-conformance.mk"} {
		data, err := os.ReadFile(filepath.Join(root, "make", name))
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(string(data), "\\\n", " ")
		for line := range strings.SplitSeq(text, "\n") {
			if !strings.HasPrefix(line, "build/conformance-luau:") {
				continue
			}
			found = true
			rest := strings.TrimPrefix(line, "build/conformance-luau:")
			for _, m := range wildcard.FindAllStringSubmatch(rest, -1) {
				globs = append(globs, m[1])
			}
			for _, tok := range strings.Fields(wildcard.ReplaceAllString(rest, " ")) {
				if !strings.Contains(tok, "$(") {
					literal[tok] = true
				}
			}
		}
	}
	if !found {
		t.Fatal("make/luau.mk and make/luau-conformance.mk define no build/conformance-luau rule")
	}

	dirs := []string{
		"internal/codegen/luautable/runtime",
		"internal/codegen/luau/runtime",
	}
	files := 0
	for _, dir := range dirs {
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			files++
			rel := dir + "/" + entry.Name()
			covered := literal[rel]
			for _, glob := range globs {
				if ok, err := path.Match(glob, rel); err == nil && ok {
					covered = true
					break
				}
			}
			if !covered {
				t.Errorf("%s changes build/conformance-luau's output but no prerequisite covers it (name it, or a $(wildcard) glob that matches it)", rel)
			}
		}
	}
	if files == 0 {
		t.Fatal("no runtime files found: this gate would pass over an empty set")
	}
}
