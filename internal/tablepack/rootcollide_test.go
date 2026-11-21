package tablepack_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
)

// A field whose JSON key is the root's own name would write `<Root>.json` as a
// field file in the expanded tree, and pack reads `<Root>.json` as the whole
// root (docs/SPEC-TABLES.md §17.2). Unpack writes the whole root on that
// collision, so the expanded tree it leaves still packs back to the same bytes.
func TestUnpackFieldKeyNamedLikeRootStaysReversible(t *testing.T) {
	dir := t.TempDir()
	schema := filepath.Join(dir, "Probe.schema")
	src := "package probe\ntable Player { level int32 | json = \"Player\"\n score int32 }\n"
	if err := os.WriteFile(schema, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	c := compiler.New()
	u, err := c.Load([]string{schema})
	if err != nil {
		t.Fatal(err)
	}
	tree := t.TempDir()
	if err := os.WriteFile(filepath.Join(tree, "Player.json"), []byte(`{"Player": 7, "score": 3}`), 0o600); err != nil {
		t.Fatal(err)
	}
	first, _, _, err := c.Pack(u, "Player", tree)
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if _, err := c.Unpack(u, "Player", first, out); err != nil {
		t.Fatal(err)
	}
	second, _, _, err := c.Pack(u, "Player", out)
	if err != nil {
		t.Fatalf("repacking the expanded unpack: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("unpack -> pack moved bytes: %x then %x", first, second)
	}
}
