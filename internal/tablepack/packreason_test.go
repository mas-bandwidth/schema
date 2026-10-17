package tablepack_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// modelFromSchema compiles one schema source into a tabletext model, the shape
// a pack reads a tree against.
func modelFromSchema(t *testing.T, schema string) *tabletext.Model {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "probe.schema"), []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return tabletext.NewModel(u)
}

// A pack refusal that the read marked malformed carries the report's named
// reason when it has one (docs/SPEC-TABLES.md §16.2), and keeps today's
// wording when no rule names the refusal (§17.3).
func TestPackErrorCarriesTheNamedReason(t *testing.T) {
	t.Parallel()
	m := modelFromSchema(t, "package probe\n\ntable Root {\n t string(8)\n at timestamp\n}\n")

	t.Run("timestamp carries its named reason", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "Root.json"), []byte(`{"at":"garbage"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		_, _, _, err := tablepack.Pack(m, "Root", dir)
		if err == nil {
			t.Fatal("the tree packed; the malformed timestamp should refuse it")
		}
		if !strings.Contains(err.Error(), "timestamp_not_rfc3339: at") {
			t.Fatalf("the refusal does not carry the read's named reason:\n%s", err)
		}
	})

	t.Run("a text with no named reason keeps today's wording", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "Root.json"), []byte(`{`), 0o644); err != nil {
			t.Fatal(err)
		}
		_, _, _, err := tablepack.Pack(m, "Root", dir)
		if err == nil {
			t.Fatal("the tree packed; the malformed text should refuse it")
		}
		if !strings.Contains(err.Error(), "not one JSON text for table") {
			t.Fatalf("expected today's wording for an unnamed refusal:\n%s", err)
		}
	})
}
