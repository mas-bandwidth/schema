package luau

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// unitFromSchemas writes one schema file per entry into a fresh directory and
// loads the unit, so a test names only the source it means.
func unitFromSchemas(t *testing.T, schemas map[string]string) *ir.Unit {
	t.Helper()
	dir := t.TempDir()
	var names []string
	for name := range schemas {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(schemas[name]), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return unitFromDir(t, dir)
}

// TestLuauGeneratedNamesAreCaseInsensitiveUnique pins docs/SPEC-TABLES.md §11's
// rule that two emitted file names equal ignoring case are refused by name,
// runtime modules included, and that a table-only schema file's module is
// <Base>Table.luau rather than an empty type-surface module (so tables/maps'
// Text.schema no longer claims schema_text.luau). The control at the end runs the
// same check with an exact comparison: that comparison does not see Text/text,
// which is what makes the positive assertions red if the production check is
// ever narrowed to exact case.
func TestLuauGeneratedNamesAreCaseInsensitiveUnique(t *testing.T) {
	t.Parallel()

	withType := func(name string) string {
		return "package p\n\ntype " + name + "\n{\n    x uint8\n}\n"
	}
	const tableOnly = "package p\n\ntable Text\n{\n    x uint8\n}\n"

	t.Run("Text beside the runtime's text", func(t *testing.T) {
		t.Parallel()
		u := unitFromSchemas(t, map[string]string{"Schema_text.schema": withType("TextProbe")})
		runtime := map[string][]byte{"schema_text.luau": []byte("-- runtime\n")}
		err := CheckBases(u, runtime)
		if err == nil {
			t.Fatal("Schema_text.luau beside schema_text.luau was accepted")
		}
		for _, want := range []string{"Schema_text.luau", "schema_text.luau", "Schema_text.schema"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("refusal does not name %s: %v", want, err)
			}
		}
	})

	t.Run("Foo and foo in one tree", func(t *testing.T) {
		t.Parallel()
		u := unitFromSchemas(t, map[string]string{
			"Foo.schema": withType("FooProbe"),
			"foo.schema": withType("FooTwo"),
		})
		_, err := Generate(u)
		if err == nil {
			t.Fatal("Foo.luau and foo.luau were both emitted")
		}
		for _, want := range []string{"Foo.luau", "foo.luau", "Foo.schema", "foo.schema"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("refusal does not name %s: %v", want, err)
			}
		}
	})

	t.Run("names differing by more than case", func(t *testing.T) {
		t.Parallel()
		u := unitFromSchemas(t, map[string]string{
			"Foo.schema": withType("FooProbe"),
			"Bar.schema": withType("BarProbe"),
		})
		first, err := Generate(u)
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		second, err := Generate(u)
		if err != nil {
			t.Fatalf("Generate again: %v", err)
		}
		if got, want := strings.Join(keysOf(first), ","), strings.Join(keysOf(second), ","); got != want {
			t.Errorf("the output file list changed on a repeat run:\nfirst  %s\nsecond %s", got, want)
		}
		for _, want := range []string{"Foo.luau", "Bar.luau"} {
			if _, ok := first[want]; !ok {
				t.Errorf("Generate omitted %s; emitted %v", want, keysOf(first))
			}
		}
	})

	t.Run("a table-only Text claims TextTable, not text", func(t *testing.T) {
		t.Parallel()
		u := unitFromSchemas(t, map[string]string{"Text.schema": tableOnly})
		runtime := map[string][]byte{"schema_text.luau": []byte("-- runtime\n")}
		if err := CheckBases(u, runtime); err != nil {
			t.Fatalf("a table-only Text.schema collides with schema_text.luau: %v", err)
		}
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if _, ok := files["Text.luau"]; ok {
			t.Errorf("the table-only Text.schema emitted the empty type module Text.luau; emitted %v", keysOf(files))
		}
	})

	// The control: an exact comparison is the planted narrow check. It must
	// not see Text/text, so a production check that used it would fail the
	// "Text beside the runtime's text" assertion above.
	t.Run("exact comparison does not catch the case pair", func(t *testing.T) {
		t.Parallel()
		u := unitFromSchemas(t, map[string]string{"Schema_text.schema": withType("TextProbe")})
		runtime := map[string][]byte{"schema_text.luau": []byte("-- runtime\n")}
		if _, _, collides := collidingNames(u, runtime, func(a, b string) bool { return a == b }); collides {
			t.Fatal("the exact-comparison control caught Text/text, so it is not a control")
		}
	})
}
