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
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// topLevel is a declaration at a module's top level: the name it puts into
// the module's namespace.
var topLevel = regexp.MustCompile(`(?m)^(?:pub\s+)?(?:recursive\s+)?(?:fn|let|var|struct|enum|union|const|type)\s+([A-Za-z_][A-Za-z0-9_]*)`)

// generateUnit runs both Goose surfaces over one unit, as `schema generate
// --lang goose` does.
func generateUnit(paths []string) (map[string][]byte, error) {
	files, _, err := generateClaimed(paths)
	return files, err
}

// generateClaimed is generateUnit, and also every name the two surfaces
// claimed in the unit's namespace (goose.Gen.Declare): a name in it is one a
// second declaration refuses by name.
func generateClaimed(paths []string) (map[string][]byte, map[string]string, error) {
	u, err := compiler.New().Load(paths)
	if err != nil {
		return nil, nil, fmt.Errorf("load: %w", err)
	}
	files, claimed, err := goose.GenerateClaimed(u)
	if err != nil {
		return nil, nil, err
	}
	tables, tableClaimed, err := goosetable.GenerateClaimed(u)
	if err != nil {
		return nil, nil, err
	}
	maps.Copy(files, tables)
	maps.Copy(claimed, tableClaimed)
	return files, claimed, nil
}

// TestGooseEveryPackageNameIsClaimed is the Goose form of docs/PORTING.md I6
// (claimed names, both ways). The runtime is emitted into its own namespace,
// schema_rt, which a package cannot take (goose.CheckUnit); everything else
// the target writes lands in the PACKAGE's namespace beside the schema's own
// declarations, and the claim there is goose.Gen.Declare: every name is
// declared once per unit, and a second declaration of it refuses the unit by
// name. This holds that claim against the emitted text, for every unit of the
// conformance manifest, from ONE generation per unit: every top-level name of
// every package module must be in the namespace the generation claimed. A
// name the target writes WITHOUT claiming it is a unit that generates two
// declarations of one name; the test names it.
//
// Two probes per unit tie the claim to the refusal, so the check is not a
// claim taken on faith: a schema type under a name the target never writes
// generates, and one under a name the target claimed for itself is refused.
// A written name outside the claim is still probed the old way, a schema
// type of that name added to the unit, and passes only if the unit refuses
// it (the loader refuses a name the schema cannot spell).
func TestGooseEveryPackageNameIsClaimed(t *testing.T) {
	t.Parallel()
	// One generation per unit and two probes, not one generation per emitted
	// name: the per-name sweep ran past 18 minutes on a loaded bench
	// (2026-11-24). It stays on the slow lane: every unit of the manifest.
	slowtest.Gate(t, "no toolchain, but the whole-manifest name sweep (one generation per unit, two probes)")
	units, _ := readManifest(t)
	runtimeFiles := goosetable.Runtime()
	maps.Copy(runtimeFiles, goose.Runtime())
	var keys []string
	for k := range units {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var mu sync.Mutex
	checked, probedOutside, tied := 0, 0, 0
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for _, key := range keys {
		paths, err := compiler.GatherPaths(prefix(units[key]))
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		dir := t.TempDir()
		wg.Add(1)
		sem <- struct{}{}
		go func(key string, paths []string, dir string) {
			defer wg.Done()
			defer func() { <-sem }()
			c, outside, tie := checkUnitClaims(t, key, paths, dir, runtimeFiles)
			mu.Lock()
			checked += c
			probedOutside += outside
			tied += tie
			mu.Unlock()
		}(key, paths, dir)
	}
	wg.Wait()
	if checked == 0 {
		t.Fatal("no name was checked")
	}
	if tied == 0 {
		t.Fatal("no unit tied its claim to a refusal: the claim map would be taken on faith")
	}
	t.Logf("%d package-scope names checked across the manifest's units against one generation each; %d units tied the claim to a refusal; %d written names outside the claim probed", checked, tied, probedOutside)
}

// checkUnitClaims holds one unit's written names to its claimed namespace and
// answers how many names it checked, how many outside the claim it probed,
// and 1 when a claimed-name probe tied the claim to a refusal.
func checkUnitClaims(t *testing.T, key string, paths []string, dir string, runtimeFiles map[string][]byte) (checked, outside, tied int) {
	files, claimed, err := generateClaimed(paths)
	if err != nil {
		return 0, 0, 0 // a unit the target refuses writes nothing to claim
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Errorf("unit %s: %v", key, err)
		return 0, 0, 0
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
		return 0, 0, 0
	}
	probe := func(name string) error {
		d := filepath.Join(dir, "probe_"+name)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(d, "ZzNameProbe.schema")
		text := fmt.Sprintf("package %s\n\ntype %s\n{\n    x int32\n}\n", u.Package, name)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := generateUnit(append(append([]string{}, paths...), p))
		return err
	}
	// the probe itself must be able to pass: a schema type under a name the
	// target never writes generates, so a refusal below is the claim and not
	// a probe the loader refuses for its own reasons
	if err := probe("ZzFreshProbeName"); err != nil {
		t.Errorf("unit %s: the probe under a fresh name does not generate, so every refusal below would prove nothing: %v", key, err)
		return 0, 0, 0
	}
	var surfaceClaim string
	for _, n := range names {
		checked++
		if _, ok := claimed[n]; ok {
			if surfaceClaim == "" && u.DeclFile[n] == "" && u.Tables[n] == nil && u.TableUnions[n] == nil {
				surfaceClaim = n
			}
			continue
		}
		outside++
		if probe(n) == nil {
			t.Errorf("unit %s: the target writes %s into namespace %s without claiming it: a schema type of that name generates a second declaration", key, n, goose.Namespace(u))
		}
	}
	if surfaceClaim != "" {
		if probe(surfaceClaim) == nil {
			t.Errorf("unit %s: %s is in the claimed namespace, yet a schema type of that name generates: the claim map does not refuse", key, surfaceClaim)
		} else {
			tied = 1
		}
	}
	return checked, outside, tied
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
