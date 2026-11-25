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

// nameUnit is one manifest unit the target generates, with every top-level
// name its package modules write.
type nameUnit struct {
	key     string
	paths   []string
	pkg     string
	ns      string
	dir     string
	names   []string          // sorted
	claimed map[string]bool   // the names both surfaces claimed through Declare
	module  map[string]string // name -> the package module that writes it first
}

// scanNameUnits generates every unit of the conformance manifest the target
// does not refuse and collects the top-level names of its package modules. It
// fails a name two package modules both write (two declarations of one name
// in one namespace, whatever the claim maps say), a unit whose package modules
// declare nothing (the scan reading the wrong files), and a unit where a
// schema type under a fresh name does not generate (every refusal a probe
// sees would then prove nothing).
func scanNameUnits(t *testing.T) []nameUnit {
	t.Helper()
	units, _ := readManifest(t)
	runtimeFiles := goosetable.Runtime()
	maps.Copy(runtimeFiles, goose.Runtime())
	var keys []string
	for k := range units {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]*nameUnit, len(keys))
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.GOMAXPROCS(0))
	for i, key := range keys {
		paths, err := compiler.GatherPaths(prefix(units[key]))
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		dir := t.TempDir()
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, key string, paths []string, dir string) {
			defer wg.Done()
			defer func() { <-sem }()
			files, claimed, err := generateUnit(paths)
			if err != nil {
				return // a unit the target refuses writes nothing to claim
			}
			u, err := compiler.New().LoadWithoutEmitterRules(paths)
			if err != nil {
				t.Errorf("unit %s: %v", key, err)
				return
			}
			nu := &nameUnit{key: key, paths: paths, pkg: u.Package, ns: goose.Namespace(u), dir: dir, claimed: claimed, module: map[string]string{}}
			var fileNames []string
			for file := range files {
				if _, ok := runtimeFiles[file]; !ok {
					fileNames = append(fileNames, file)
				}
			}
			sort.Strings(fileNames)
			for _, file := range fileNames {
				for _, m := range topLevel.FindAllSubmatch(files[file], -1) {
					n := string(m[1])
					if first, ok := nu.module[n]; ok {
						t.Errorf("unit %s: %s and %s both declare %s in namespace %s: two declarations of one name", key, first, file, n, nu.ns)
						continue
					}
					nu.module[n] = file
					nu.names = append(nu.names, n)
				}
			}
			sort.Strings(nu.names)
			if len(nu.names) == 0 {
				t.Errorf("unit %s: the package modules declare nothing; the scan is reading the wrong files", key)
				return
			}
			// the probe itself must be able to pass: a schema type under a
			// name the target never writes generates, so a refusal below is
			// the claim and not a probe the loader refuses for its own reasons
			fresh := filepath.Join(dir, "ZzFresh")
			if err := os.MkdirAll(fresh, 0o755); err != nil {
				t.Error(err)
				return
			}
			freshProbe := filepath.Join(fresh, "ZzNameProbe.schema")
			if err := os.WriteFile(freshProbe, []byte(fmt.Sprintf("package %s\n\ntype ZzFreshProbeName\n{\n    x int32\n}\n", nu.pkg)), 0o644); err != nil {
				t.Error(err)
				return
			}
			if _, _, err := generateUnit(append(append([]string{}, paths...), freshProbe)); err != nil {
				t.Errorf("unit %s: the probe under a fresh name does not generate, so every refusal below would prove nothing: %v", key, err)
				return
			}
			out[i] = nu
		}(i, key, paths, dir)
	}
	wg.Wait()
	var res []nameUnit
	for _, nu := range out {
		if nu != nil {
			res = append(res, *nu)
		}
	}
	if t.Failed() {
		t.FailNow()
	}
	if len(res) == 0 {
		t.Fatal("no unit of the manifest generated: nothing to check")
	}
	return res
}

// probeNames adds, for every name of every unit that pick chooses, a schema
// file declaring a type of that name to the unit, and fails a unit that still
// generates: the target writes that name without claiming it, and a schema
// type of that name generates a second declaration. It answers how many
// names it probed.
func probeNames(t *testing.T, units []nameUnit, pick func(nameUnit, string) bool) int {
	t.Helper()
	var mu sync.Mutex
	probed := 0
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.GOMAXPROCS(0))
	for _, nu := range units {
		for _, n := range nu.names {
			if !pick(nu, n) {
				continue
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(nu nameUnit, n string) {
				defer wg.Done()
				defer func() { <-sem }()
				d := filepath.Join(nu.dir, n)
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Error(err)
					return
				}
				probe := filepath.Join(d, "ZzNameProbe.schema")
				text := fmt.Sprintf("package %s\n\ntype %s\n{\n    x int32\n}\n", nu.pkg, n)
				if err := os.WriteFile(probe, []byte(text), 0o644); err != nil {
					t.Error(err)
					return
				}
				if _, _, err := generateUnit(append(append([]string{}, nu.paths...), probe)); err == nil {
					t.Errorf("unit %s: the target writes %s into namespace %s without claiming it: a schema type of that name generates a second declaration", nu.key, n, nu.ns)
				}
				mu.Lock()
				probed++
				mu.Unlock()
			}(nu, n)
		}
	}
	wg.Wait()
	return probed
}

// TestGooseEveryPackageNameIsClaimed is the Goose form of docs/PORTING.md I6
// (claimed names, both ways), in its per-change form. The runtime is emitted
// into its own namespace, schema_rt, which a package cannot take
// (goose.CheckUnit); everything else the target writes lands in the
// PACKAGE's namespace beside the schema's own declarations, and the claim
// there is goose.Gen.Declare: every name is declared once per unit, and a
// second declaration of it refuses the unit by name. This holds that claim
// against the emitted text, for every unit of the conformance manifest:
//
//   - no two package modules write one name (scanNameUnits);
//   - EVERY top-level name of every package module is in the set the two
//     surfaces claimed through Declare (goose.GenerateClaims,
//     goosetable.GenerateClaims), or else a schema file that declares it is
//     added to the unit and the loader must refuse the unit (a name the
//     schema already declares or cannot spell);
//   - the claim REFUSES: for the first claimed name of every declaring module,
//     a schema file declaring that name is added to the unit and the unit
//     must refuse.
//
// A name the target writes WITHOUT declaring it is a unit that generates two
// declarations of one name; the test names it. It trusts the claim maps for
// the claimed names it does not probe; TestGooseEveryPackageNameRefusesEndToEnd
// (the nightly tier) probes every one of them end to end.
func TestGooseEveryPackageNameIsClaimed(t *testing.T) {
	t.Parallel()
	units := scanNameUnits(t)
	checked := 0
	for _, nu := range units {
		checked += len(nu.names)
	}
	sampled := map[string]bool{} // unit key + module
	probed := probeNames(t, units, func(nu nameUnit, n string) bool {
		if !nu.claimed[n] {
			return true
		}
		k := nu.key + "\x00" + nu.module[n]
		if sampled[k] {
			return false
		}
		sampled[k] = true
		return true
	})
	if checked == 0 || probed == 0 {
		t.Fatalf("no name was checked (%d) or probed (%d)", checked, probed)
	}
	t.Logf("%d package-scope names checked across the manifest's units against the claimed set, %d probed end to end, each refused when a schema declares it", checked, probed)
}

// TestGooseEveryPackageNameRefusesEndToEnd is docs/PORTING.md I6 for Goose
// with no claim map trusted: EVERY top-level name of every package module of
// every unit of the conformance manifest is declared in turn by a schema file
// added to the unit, and the unit must then refuse (by the loader, for a name
// the schema already declares or cannot spell, or by Declare). It is one
// whole load and generation per emitted name, some 38,000, so it is a
// nightly-tier row (test/registry/registry.jsonl); the per-change form is
// TestGooseEveryPackageNameIsClaimed.
func TestGooseEveryPackageNameRefusesEndToEnd(t *testing.T) {
	t.Parallel()
	units := scanNameUnits(t)
	checked := 0
	for _, nu := range units {
		checked += len(nu.names)
	}
	probed := probeNames(t, units, func(nameUnit, string) bool { return true })
	if checked == 0 || probed != checked {
		t.Fatalf("%d names written, %d probed: every written name is probed", checked, probed)
	}
	t.Logf("%d package-scope names across the manifest's units, each probed end to end and refused when a schema declares it", probed)
}

// TestGooseTableNamesMeetTheWirePartsNames: the table surface checks its
// names against EVERY name the type surface claims, the wire part's
// included (goose.DeclaredNames). Table ReadFoo's descriptor is
// ReadFooDescriptor, and so is the wire reader of a type FooDescriptor: the
// unit must refuse by that name, not write two declarations of it into one
// namespace. The same unit with the table under another name generates, so
// the refusal is the collision and nothing else.
func TestGooseTableNamesMeetTheWirePartsNames(t *testing.T) {
	t.Parallel()
	const tmpl = "package collide\n\ntype FooDescriptor\n{\n    x int32\n}\n\ntable %s\n{\n    v int32\n}\n"
	clean := unitFromSchema(t, "Collide.schema", fmt.Sprintf(tmpl, "ReadBar"))
	if _, err := goose.Generate(clean); err != nil {
		t.Fatalf("the control unit (table ReadBar) does not generate the type surface: %v", err)
	}
	if _, err := goosetable.Generate(clean); err != nil {
		t.Fatalf("the control unit (table ReadBar) does not generate the table surface, so the refusal below would prove nothing: %v", err)
	}
	u := unitFromSchema(t, "Collide.schema", fmt.Sprintf(tmpl, "ReadFoo"))
	types, err := goose.Generate(u)
	if err != nil {
		t.Fatalf("the type surface alone refuses the unit, so the table surface's check is not what is held: %v", err)
	}
	if !topLevelDeclares(types, "ReadFooDescriptor") {
		t.Fatal("the type surface no longer writes ReadFooDescriptor (the wire reader of FooDescriptor): the collision this test holds is gone; pick another")
	}
	_, err = goosetable.Generate(u)
	if err == nil {
		t.Fatal("goosetable generates table ReadFoo beside type FooDescriptor: ReadFooDescriptor is declared twice in namespace collide")
	}
	if !strings.Contains(err.Error(), "ReadFooDescriptor") {
		t.Fatalf("goosetable refuses the unit, but not by the name ReadFooDescriptor: %v", err)
	}
}

// topLevelDeclares reports whether any module of files declares name at its
// top level.
func topLevelDeclares(files map[string][]byte, name string) bool {
	for _, data := range files {
		for _, m := range topLevel.FindAllSubmatch(data, -1) {
			if string(m[1]) == name {
				return true
			}
		}
	}
	return false
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
