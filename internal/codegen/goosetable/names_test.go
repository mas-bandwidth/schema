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
// --lang goose` does, and answers the names they claimed in the unit's
// namespace (goose.Gen.Declare, both surfaces). It loads without the OTHER
// targets' name rules (compiler.LoadWithoutEmitterRules): Goose's own rule,
// goose.CheckUnit, runs in goose.Generate either way, so a probe refused here
// is refused by the loader's own rules or by Goose, never by another target's
// rule that would hide a name Goose writes unclaimed.
func generateUnit(paths []string) (map[string][]byte, map[string]bool, error) {
	u, err := compiler.New().LoadWithoutEmitterRules(paths)
	if err != nil {
		return nil, nil, fmt.Errorf("load: %w", err)
	}
	files, typeClaims, err := goose.GenerateClaims(u)
	if err != nil {
		return nil, nil, err
	}
	tables, tableClaims, err := goosetable.GenerateClaims(u)
	if err != nil {
		return nil, nil, err
	}
	maps.Copy(files, tables)
	claimed := map[string]bool{}
	for n := range typeClaims {
		claimed[n] = true
	}
	for n := range tableClaims {
		claimed[n] = true
	}
	return files, claimed, nil
}

// TestGooseEveryPackageNameIsClaimed is the Goose form of docs/PORTING.md I6
// (claimed names, both ways). The runtime is emitted into its own namespace,
// schema_rt, which a package cannot take (goose.CheckUnit); everything else
// the target writes lands in the PACKAGE's namespace beside the schema's own
// declarations, and the claim there is goose.Gen.Declare: every name is
// declared once per unit, and a second declaration of it refuses the unit by
// name. This holds that claim against the emitted text, for every unit of the
// conformance manifest, in two parts:
//
//   - EVERY top-level name of every package module is in the set the two
//     surfaces claimed through Declare (goose.GenerateClaims,
//     goosetable.GenerateClaims), or else a schema file that declares it is
//     added to the unit and the loader must refuse the unit (a name the
//     schema already declares or cannot spell);
//   - the claim REFUSES: for one claimed name of every declaring module, a
//     schema file declaring that name is added to the unit and the unit must
//     refuse.
//
// A name the target writes WITHOUT declaring it is a unit that generates two
// declarations of one name; the test names it. (Probing every claimed name
// end to end, one whole load and generation each, was some 40,000
// generations and over ten minutes on a bench slot; a claimed name refuses
// by the same Declare whichever name it is, which the second part holds
// module by module.)
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
	checked, probed := 0, 0
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for _, key := range keys {
		paths, err := compiler.GatherPaths(prefix(units[key]))
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		files, claimed, err := generateUnit(paths)
		if err != nil {
			continue // a unit the target refuses writes nothing to claim
		}
		u, err := compiler.New().LoadWithoutEmitterRules(paths)
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		seen := map[string]bool{}
		var names []string
		module := map[string]string{} // name -> the package module that writes it first
		var fileNames []string
		for file := range files {
			fileNames = append(fileNames, file)
		}
		sort.Strings(fileNames)
		for _, file := range fileNames {
			if _, ok := runtimeFiles[file]; ok {
				continue
			}
			for _, m := range topLevel.FindAllSubmatch(files[file], -1) {
				n := string(m[1])
				if !seen[n] {
					seen[n] = true
					names = append(names, n)
					module[n] = file
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
		if _, _, err := generateUnit(append(append([]string{}, paths...), freshProbe)); err != nil {
			t.Fatalf("unit %s: the probe under a fresh name does not generate, so every refusal below would prove nothing: %v", key, err)
		}
		// the probes: every unclaimed name, and the first claimed name of
		// each module that writes one
		var probes []string
		sampled := map[string]bool{}
		for _, n := range names {
			mu.Lock()
			checked++
			mu.Unlock()
			if !claimed[n] {
				probes = append(probes, n)
				continue
			}
			if !sampled[module[n]] {
				sampled[module[n]] = true
				probes = append(probes, n)
			}
		}
		for _, n := range probes {
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
				if _, _, err := generateUnit(append(append([]string{}, paths...), probe)); err == nil {
					t.Errorf("unit %s: the target writes %s into namespace %s without claiming it: a schema type of that name generates a second declaration", key, n, goose.Namespace(u))
				}
				mu.Lock()
				probed++
				mu.Unlock()
			}(key, n)
		}
	}
	wg.Wait()
	if checked == 0 || probed == 0 {
		t.Fatalf("no name was checked (%d) or probed (%d)", checked, probed)
	}
	t.Logf("%d package-scope names checked across the manifest's units against the claimed set, %d probed end to end, each refused when a schema declares it", checked, probed)
}

// TestGooseNamesScanSeesTheRuntime: the table runtime's modules declare into
// schema_rt and nothing else, so a package can never meet them; a runtime
// module that declared into another namespace would leave the claim's reach.
func TestGooseNamesScanSeesTheRuntime(t *testing.T) {
	t.Parallel()
	allRuntime := goosetable.Runtime()
	if len(allRuntime) == 0 {
		t.Fatal("goosetable.Runtime() is empty: the loop below would check nothing")
	}
	for file, data := range allRuntime {
		if !strings.HasSuffix(file, ".goose") {
			continue
		}
		if !strings.Contains(string(data), "\nnamespace schema_rt;") && !strings.HasPrefix(string(data), "namespace schema_rt;") {
			t.Errorf("runtime module %s does not declare into namespace schema_rt", file)
		}
	}
}
