package luautable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
)

func TestStrictOnlyLuauNoTolerantEntry(t *testing.T) {
	t.Parallel()
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "tables", "examples", "*.schema"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no tables/examples/*.schema found")
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("load tables/examples: %v", err)
	}
	files, err := luautable.Generate(u)
	if err != nil {
		t.Fatalf("luautable.Generate(examples): %v", err)
	}

	const fixedSrc = `package p

fixed table Rec
{
    x int32 | min = 0, max = 10
    y uint16
}
`
	tmp := t.TempDir()
	fixedPath := filepath.Join(tmp, "probe.schema")
	if err := os.WriteFile(fixedPath, []byte(fixedSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	uf, err := compiler.New().Load([]string{fixedPath})
	if err != nil {
		t.Fatalf("load fixed probe: %v", err)
	}
	fixedFiles, err := luautable.Generate(uf)
	if err != nil {
		t.Fatalf("luautable.Generate(fixed): %v", err)
	}
	for k, v := range fixedFiles {
		files["fixed/"+k] = v
	}
	for k, v := range luautable.Runtime() {
		files["runtime/"+k] = v
	}
	tn, err := os.ReadFile(filepath.Join("..", "..", "tablenames", "luau.go"))
	if err != nil {
		t.Fatal(err)
	}
	files["internal/tablenames/luau.go"] = tn

	for name, data := range files {
		text := string(data)
		for _, needle := range []string{"Tolerant", "tolerant"} {
			if strings.Contains(text, needle) {
				t.Errorf("%s still contains %q", name, needle)
			}
		}
	}
}
