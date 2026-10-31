package squirreltable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

// TestJSONRefusesIllFormedAtBound pins docs/SPEC-TABLES.md §16.3 for the
// Squirrel text form on the corpus's str-illformed-at-bound tree: the 0xFF
// byte in the name field's string does not spell a code point, so FromJson
// refuses it by name before the field's 32-byte bound can clamp anything.
func TestJSONRefusesIllFormedAtBound(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")
	var paths []string
	for _, u := range manifestUnits(t, manifest) {
		if u.key == "tabledemo" {
			paths = u.paths
		}
	}
	if len(paths) == 0 {
		t.Fatalf("the manifest declares no unit tabledemo")
	}
	u := loadUnit(t, root, paths)
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "tabledemo"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(dir, "tabledemo", name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(dir, "tabledemo", name), data)
	}

	const illformed = "testdata/conformance/tables/json-hostile/str-illformed-at-bound/RootConfig.json"
	input, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(illformed)))
	if err != nil {
		t.Fatal(err)
	}
	main := fmt.Sprintf(`local inputs = dofile("inputs.nut", true);
local schema = dofile("tabledemo/schema_module.nut", true);
local T = schema.require(%q);
local J = schema.require("schema_table_json");
local r = { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0,
  malformed = false, refused = false };
J.FromJsonTolerant(T, "RootConfig", inputs["illformed"], r);
local why = ("reason" in r) ? ((r.reason == null) ? "-" : r.reason) : "-";
print((r.malformed ? "refused" : "read") + "|" + why + "\n");
`, squirreltable.ModuleName(u))
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(main))
	got := strings.TrimSpace(string(sqrun.Run(t, dir, "main.nut", map[string][]byte{"illformed": input})))
	const want = "refused|string_not_utf8: <root>"
	if got != want {
		t.Fatalf("FromJson of str-illformed-at-bound printed %q, want %q (§16.3 refuses ill-formed UTF-8 by name before any clamp)", got, want)
	}
}
