package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGoModulesResolveTheSharedRuntime holds the module wiring that makes every
// main module compiling a default-generated (shared-runtime) Go table unit
// resolve tables/goruntime from this checkout. The Go target's home file
// imports `github.com/mas-bandwidth/schema/v2/tables/goruntime` because shared
// is the default (docs/SPEC-TABLES.md §8.4); a module that compiles such a unit
// must therefore require schema/v2 and replace it with the repository root.
// Text only: no go command, no build, no network.
func TestGoModulesResolveTheSharedRuntime(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	rootMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read repo root go.mod: %v", err)
	}
	if !strings.Contains(string(rootMod), "module github.com/mas-bandwidth/schema/v2") {
		t.Fatal("repo root go.mod does not name github.com/mas-bandwidth/schema/v2")
	}

	checkModule := func(name, dir, mod string) {
		if !strings.Contains(mod, "github.com/mas-bandwidth/schema/v2 v2.0.0") {
			t.Errorf("%s: no require github.com/mas-bandwidth/schema/v2 v2.0.0 (docs/SPEC-TABLES.md §8.4)", name)
		}
		foundReplace := false
		for _, line := range strings.Split(mod, "\n") {
			rest, cut := strings.CutPrefix(strings.TrimSpace(line), "replace github.com/mas-bandwidth/schema/v2 => ")
			if !cut {
				continue
			}
			rel := strings.Trim(rest, `"`)
			resolved := filepath.Clean(filepath.Join(dir, rel))
			if resolved != filepath.Clean(root) {
				t.Errorf("%s: replace %q resolves to %q, want the repository root %q", name, rel, resolved, root)
			}
			foundReplace = true
		}
		if !foundReplace {
			t.Errorf("%s: no replace for github.com/mas-bandwidth/schema/v2 (docs/SPEC-TABLES.md §8.4)", name)
		}
	}

	// test/conformance/go compiles every default-generated unit.
	cf := filepath.Join(root, "test", "conformance", "go", "go.mod")
	cfData, err := os.ReadFile(cf)
	if err != nil {
		t.Fatalf("read %s: %v", cf, err)
	}
	checkModule("test/conformance/go/go.mod", filepath.Dir(cf), string(cfData))

	// tables/cards/example imports the generated `cards` package.
	ex := filepath.Join(root, "tables", "cards", "example", "go.mod")
	exData, err := os.ReadFile(ex)
	if err != nil {
		t.Fatalf("read %s: %v", ex, err)
	}
	checkModule("tables/cards/example/go.mod", filepath.Dir(ex), string(exData))

	// make/go.mk writes the cards module's go.mod (build/tables-generated-go/
	// cards), which runs `go test` and compiles the generated `cards` package.
	mkData, err := os.ReadFile(filepath.Join(root, "make", "go.mk"))
	if err != nil {
		t.Fatalf("read make/go.mk: %v", err)
	}
	cardsMod, cardsDir := cardsGovernModFromMake(string(mkData), root)
	checkModule("make/go.mk cards go.mod", cardsDir, cardsMod)
}

// cardsGovernModFromMake extracts the go.mod the make/go.mk printf writes for
// the cards module and the directory it lands in.
func cardsGovernModFromMake(mk, root string) (string, string) {
	const dest = "build/tables-generated-go/cards/go.mod"
	dir := filepath.Join(root, "build", "tables-generated-go", "cards")
	for _, line := range strings.Split(mk, "\n") {
		if !strings.Contains(line, dest) || !strings.Contains(line, "printf ") {
			continue
		}
		start := strings.Index(line, "'")
		if start < 0 {
			return "", dir
		}
		end := strings.LastIndex(line, "'")
		if end <= start {
			return "", dir
		}
		format := line[start+1 : end]
		decoded := strings.NewReplacer(`\n`, "\n", `\t`, "\t", `\\`, "\\").Replace(format)
		return decoded, dir
	}
	return "", dir
}
