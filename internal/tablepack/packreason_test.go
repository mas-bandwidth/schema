package tablepack_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
)

// A pack that refuses a text the read marked malformed carries that read's
// named Reason when it has one (docs/SPEC-TABLES.md §17.4): a timestamp that
// does not parse refuses as `timestamp_not_rfc3339: <field>` (§16.2), and the
// refusal names it instead of dropping it. A malformed text that names no rule
// keeps today's generic wording.
func TestPackErrorCarriesTheNamedReason(t *testing.T) {
	t.Parallel()

	const schema = "package probe\n\ntable Root\n{\n    t string(8)\n    at timestamp\n}\n"
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "Root.schema"), []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{filepath.Join(src, "Root.schema")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatalf("unit: %v", err)
	}

	// A root that reads as one text of the whole table is refused by name with
	// the reason the read hands back.
	t.Run("root-carries-reason", func(t *testing.T) {
		tree := t.TempDir()
		if err := os.WriteFile(filepath.Join(tree, "Root.json"), []byte(`{"at":"garbage"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		_, _, _, err := c.Pack(u, "Root", tree)
		if err == nil {
			t.Fatal("the root was accepted")
		}
		if !strings.Contains(err.Error(), "timestamp_not_rfc3339: at") {
			t.Fatalf("refusal drops the named reason:\n%s", err)
		}
	})

	// A field file that reads malformed with the same reason.
	t.Run("field-carries-reason", func(t *testing.T) {
		tree := t.TempDir()
		if err := os.WriteFile(filepath.Join(tree, "at.json"), []byte(`"garbage"`), 0o644); err != nil {
			t.Fatal(err)
		}
		_, _, _, err := c.Pack(u, "Root", tree)
		if err == nil {
			t.Fatal("the field was accepted")
		}
		if !strings.Contains(err.Error(), "timestamp_not_rfc3339: at") {
			t.Fatalf("refusal drops the named reason:\n%s", err)
		}
	})

	// A malformed text that names no rule keeps today's generic wording.
	t.Run("bare-brace-keeps-wording", func(t *testing.T) {
		tree := t.TempDir()
		if err := os.WriteFile(filepath.Join(tree, "Root.json"), []byte(`{`), 0o644); err != nil {
			t.Fatal(err)
		}
		_, _, _, err := c.Pack(u, "Root", tree)
		if err == nil {
			t.Fatal("the root was accepted")
		}
		if !strings.Contains(err.Error(), "not one JSON text for table Root") {
			t.Fatalf("generic wording changed:\n%s", err)
		}
	})
}
