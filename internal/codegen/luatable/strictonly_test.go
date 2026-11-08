package luatable

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestStrictOnlyLuaNoTolerantEntry is the strict-only grep gate for the Lua
// leg (docs/plans/strict-only.md layer 4): the generator's output for
// tables/examples and a fixed-form probe schema, and the registered Lua names
// in internal/tablenames/lua.go, carry none of the retired tolerant surface —
// no Tolerant entry point, no clamped (renamed out_of_range), no kindMismatch
// or duplicate or blobShared counter, and no malformed binary verdict on the
// read report.
func TestStrictOnlyLuaNoTolerantEntry(t *testing.T) {
	t.Parallel()

	forbidden := []string{
		"Tolerant",
		"tolerant",
		"clamped",
		"Clamped",
		"kindMismatch",
		"blobShared",
		".duplicate",
		"duplicate =",
		".malformed",
		"malformed =",
	}
	check := func(where, text string) {
		for _, bad := range forbidden {
			if strings.Contains(text, bad) {
				t.Errorf("%s: retired strict-only name %q present", where, bad)
			}
		}
	}

	examplesDir := filepath.Join("..", "..", "..", "tables", "examples")
	entries, err := os.ReadDir(examplesDir)
	if err != nil {
		t.Fatalf("read %s: %v", examplesDir, err)
	}
	var paths []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".schema") {
			paths = append(paths, filepath.Join(examplesDir, entry.Name()))
		}
	}
	sort.Strings(paths)
	u := luarun.LoadUnit(t, paths...)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate lua tables/examples: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("generate lua tables/examples emitted nothing")
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		check("examples/"+name, string(files[name]))
	}

	const fixedSrc = `package strictonly
table Leaf { entries map[int32]int32
    values []int32
    name string(16)
    score int32 | min = 0, max = 100
    ratio float32 }
union Choice { leaf Leaf }
table Holder { inner Leaf
    maybe ?Leaf
    leaves []Leaf
    choice Choice
    head *Leaf }
fixed table Root { a uint16
    b int32 | min = 0, max = 10
    c float32 }
`
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "strictonly.schema")
	if err := os.WriteFile(schemaPath, []byte(fixedSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	fixedUnit := luarun.LoadUnit(t, schemaPath)
	fixedFiles, err := Generate(fixedUnit)
	if err != nil {
		t.Fatalf("generate lua fixed probe: %v", err)
	}
	fixedNames := make([]string, 0, len(fixedFiles))
	for name := range fixedFiles {
		fixedNames = append(fixedNames, name)
	}
	sort.Strings(fixedNames)
	for _, name := range fixedNames {
		check("fixed/"+name, string(fixedFiles[name]))
	}

	namesPath := filepath.Join("..", "..", "tablenames", "lua.go")
	data, err := os.ReadFile(namesPath)
	if err != nil {
		t.Fatalf("read %s: %v", namesPath, err)
	}
	check(namesPath, string(data))
}
