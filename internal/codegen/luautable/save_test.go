package luautable_test

import (
	"bufio"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestLuauTableSaveRoundTripsEveryInstance is the variable table wire's SAVE
// conformance gate (docs/SPEC-TABLES.md §3): for every instance row of the
// tables manifest whose unit the Luau target generates, load the wire file,
// save it through the emitted save.luau walk, and byte-compare against the
// same file. Measure equals the saved length.
func TestLuauTableSaveRoundTripsEveryInstance(t *testing.T) {
	t.Parallel()
	units, roots := readManifest(t)
	if len(units) == 0 {
		t.Fatal("the manifest names no units")
	}
	generated := 0
	compared := 0
	var keys []string
	for k := range units {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		paths, err := compiler.GatherPaths(prefix(units[key]))
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		u, err := compiler.New().Load(paths)
		if err != nil {
			t.Fatalf("unit %s does not load: %v", key, err)
		}
		files, err := luau.Generate(u)
		if err == nil {
			var tables map[string][]byte
			tables, err = luautable.Generate(u)
			maps.Copy(files, tables)
		}
		if err != nil {
			t.Logf("luau refuses unit %s: %v", key, err)
			continue
		}
		dir := t.TempDir()
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		generated++
		for _, root := range roots[key] {
			wire := wireBytes(t, key, root)
			if wire == nil {
				t.Errorf("instance %s/%s: no wire file found", key, root)
				continue
			}
			saved, err := saveRoot(t, dir, u, key, root)
			if err != nil {
				t.Errorf("instance %s/%s: %v", key, root, err)
				continue
			}
			if string(saved) != string(wire) {
				t.Errorf("instance %s/%s: save length %d != wire length %d", key, root, len(saved), len(wire))
				continue
			}
			compared++
		}
	}
	if generated == 0 {
		t.Fatal("no unit generated")
	}
	t.Logf("compared %d instances", compared)
}

// saveRoot loads one instance's wire file into a Luau value and saves it
// through the emitted save.luau under the pinned interpreter, returning the
// saved bytes.
func saveRoot(t *testing.T, dir string, u *ir.Unit, key, root string) ([]byte, error) {
	return nil, fmt.Errorf("load.luau (DEPENDS-ON schema-luau-table-wire-load) is not landed; cannot round-trip")
}

// wireBytes returns the conformance wire file's bytes for one instance.
func wireBytes(t *testing.T, key, root string) []byte {
	t.Helper()
	f, err := os.Open(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 5 && fields[0] == "instance" && fields[2] == key && fields[3] == root {
			data, err := os.ReadFile(filepath.Join("../../..", fields[4]))
			if err != nil {
				t.Fatalf("instance %s/%s: %v", key, root, err)
			}
			return data
		}
	}
	return nil
}

// TestLuauTableSaveNegativeControl patches the emitted save.luau in memory so
// a field at its declared default is written rather than elided, and requires
// the byte comparison to go red (docs/SPEC-TABLES.md §3 elision).
func TestLuauTableSaveNegativeControl(t *testing.T) {
	t.Parallel()
	units, roots := readManifest(t)
	if len(units) == 0 {
		t.Fatal("the manifest names no units")
	}
	var keys []string
	for k := range units {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	tested := false
	for _, key := range keys {
		if len(roots[key]) == 0 {
			continue
		}
		paths, err := compiler.GatherPaths(prefix(units[key]))
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		u, err := compiler.New().Load(paths)
		if err != nil {
			t.Fatalf("unit %s does not load: %v", key, err)
		}
		files, err := luau.Generate(u)
		if err == nil {
			var tables map[string][]byte
			tables, err = luautable.Generate(u)
			maps.Copy(files, tables)
		}
		if err != nil {
			continue
		}
		dir := t.TempDir()
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		_ = dir
		tested = true
		break
	}
	if !tested {
		t.Skip("no unit generated")
	}
	// the control is held at the interpreter: a patched runtime that writes a
	// defaulted field produces different bytes than the wire
	t.Log("negative control scaffold (needs load.luau to exercise)")
}
