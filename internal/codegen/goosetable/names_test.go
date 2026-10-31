package goosetable_test

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goosetable"
)

// topLevel is a declaration at a module's top level: the name it puts into
// the module's namespace.
var topLevel = regexp.MustCompile(`(?m)^(?:pub\s+)?(?:recursive\s+)?(?:fn|let|var|struct|enum|union|const|type)\s+([A-Za-z_][A-Za-z0-9_]*)`)

// generateUnit runs both Goose surfaces over one unit, as `schema generate
// --lang goose` does.
func generateUnit(paths []string) (map[string][]byte, error) {
	u, err := compiler.New().Load(paths)
	if err != nil {
		return nil, fmt.Errorf("load: %w", err)
	}
	files, err := goose.Generate(u)
	if err != nil {
		return nil, err
	}
	tables, err := goosetable.Generate(u)
	if err != nil {
		return nil, err
	}
	maps.Copy(files, tables)
	return files, nil
}

// TestGooseEveryPackageNameIsClaimed is the Goose form of docs/PORTING.md I6
// (claimed names, both ways). The runtime is emitted into its own namespace,
// schema_rt, which a package cannot take (goose.CheckUnit); everything else
// the target writes lands in the PACKAGE's namespace beside the schema's own
// declarations, and the claim there is goose.Gen.Declare: every name is
// declared once per unit, and a second declaration of it refuses the unit by
// name. This holds that claim against the emitted text, for every unit of the
// conformance manifest: every top-level name of every package module is
// declared in turn by a schema file added to the unit, and the unit must then
// refuse (by the loader, for a name the schema already declares or cannot
// spell, or by Declare). A name the target writes WITHOUT declaring it is a
// unit that generates two declarations of one name; the test names it.
func TestGooseEveryPackageNameIsClaimed(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	runtimeFiles := goosetable.Runtime()
	maps.Copy(runtimeFiles, goose.Runtime())
	var keys []string
	for k := range units {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var mu sync.Mutex
	checked := 0
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for _, key := range keys {
		paths, err := compiler.GatherPaths(prefix(units[key]))
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		files, err := generateUnit(paths)
		if err != nil {
			continue // a unit the target refuses writes nothing to claim
		}
		u, err := compiler.New().Load(paths)
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		seen := map[string]bool{}
		var names []string
		for file, data := range files {
			if _, ok := runtimeFiles[file]; ok {
				continue
			}
			for _, m := range topLevel.FindAllSubmatch(data, -1) {
				n := string(m[1])
				if !seen[n] {
					seen[n] = true
					names = append(names, n)
				}
			}
		}
		sort.Strings(names)
		if len(names) == 0 {
			t.Errorf("unit %s: the package modules declare nothing; the scan is reading the wrong files", key)
			continue
		}
		dir := t.TempDir()
		// the probe itself must be able to pass: a schema type under a name
		// the target never writes generates, so a refusal below is the claim
		// and not a probe the loader refuses for its own reasons
		fresh := filepath.Join(dir, "ZzFresh")
		if err := os.MkdirAll(fresh, 0o755); err != nil {
			t.Fatal(err)
		}
		freshProbe := filepath.Join(fresh, "ZzNameProbe.schema")
		if err := os.WriteFile(freshProbe, []byte(fmt.Sprintf("package %s\n\ntype ZzFreshProbeName\n{\n    x int32\n}\n", u.Package)), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := generateUnit(append(append([]string{}, paths...), freshProbe)); err != nil {
			t.Fatalf("unit %s: the probe under a fresh name does not generate, so every refusal below would prove nothing: %v", key, err)
		}
		for _, n := range names {
			wg.Add(1)
			sem <- struct{}{}
			go func(key, n string) {
				defer wg.Done()
				defer func() { <-sem }()
				d := filepath.Join(dir, n)
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Error(err)
					return
				}
				probe := filepath.Join(d, "ZzNameProbe.schema")
				text := fmt.Sprintf("package %s\n\ntype %s\n{\n    x int32\n}\n", u.Package, n)
				if err := os.WriteFile(probe, []byte(text), 0o644); err != nil {
					t.Error(err)
					return
				}
				if _, err := generateUnit(append(append([]string{}, paths...), probe)); err == nil {
					t.Errorf("unit %s: the target writes %s into namespace %s without claiming it: a schema type of that name generates a second declaration", key, n, goose.Namespace(u))
				}
				mu.Lock()
				checked++
				mu.Unlock()
			}(key, n)
		}
	}
	wg.Wait()
	if checked == 0 {
		t.Fatal("no name was checked")
	}
	t.Logf("%d package-scope names checked across the manifest's units, each refused when a schema declares it", checked)
}

// TestGooseNamesScanSeesTheRuntime: the table runtime's modules declare into
// schema_rt and nothing else, so a package can never meet them; a runtime
// module that declared into another namespace would leave the claim's reach.
func TestGooseNamesScanSeesTheRuntime(t *testing.T) {
	t.Parallel()
	for file, data := range goosetable.Runtime() {
		if !strings.HasSuffix(file, ".goose") {
			continue
		}
		if !strings.Contains(string(data), "\nnamespace schema_rt;") && !strings.HasPrefix(string(data), "namespace schema_rt;") {
			t.Errorf("runtime module %s does not declare into namespace schema_rt", file)
		}
	}
}
