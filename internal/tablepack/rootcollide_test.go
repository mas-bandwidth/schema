package tablepack_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
)

// The expanded tree's entries are computed whole before anything is written:
// a field's `<key>.json`, an enum-keyed array's `<key>/`, and `<Root>.json`,
// which pack reads as the whole root (docs/SPEC-TABLES.md §17.2). When two of
// them name one entry, unpack writes the whole root instead — never a tree
// that stops halfway or does not pack back — and the result packs back to the
// same bytes.
func TestUnpackTreeNameOverlapStaysReversible(t *testing.T) {
	cases := []struct{ name, schema, text string }{
		{
			"a field key names the root",
			"package probe\ntable Player { level int32 | json = \"Player\"\n score int32 }\n",
			`{"Player": 7, "score": 3}`,
		},
		{
			"a keyed directory takes a field's file name",
			"package probe\nenum Team { Red, Blue }\ntable Player { health int32\n scores [Team]int32 | json = \"health.json\" }\n",
			`{"health": 7, "health.json": {"Red": 1, "Blue": 2}}`,
		},
		{
			"a keyed directory takes the root's file name",
			"package probe\nenum Team { Red, Blue }\ntable Player { level int32\n scores [Team]int32 | json = \"Player.json\" }\n",
			`{"level": 7, "Player.json": {"Red": 1, "Blue": 2}}`,
		},
		{
			// two keys one normalizing filesystem stores as one path: é as
			// U+00E9, and e followed by U+0301
			"two Unicode spellings of one name",
			"package probe\ntable Player { a int32 | json = \"Café\"\n b int32 | json = \"Café\" }\n",
			"{\"Café\": 7, \"Café\": 9}",
		},
		{
			"a key differing only in case",
			"package probe\ntable Player { a int32 | json = \"score\"\n b int32 | json = \"Score\" }\n",
			`{"score": 7, "Score": 9}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			schema := filepath.Join(dir, "Probe.schema")
			if err := os.WriteFile(schema, []byte(tc.schema), 0o600); err != nil {
				t.Fatal(err)
			}
			c := compiler.New()
			u, err := c.Load([]string{schema})
			if err != nil {
				t.Fatal(err)
			}
			tree := t.TempDir()
			if err := os.WriteFile(filepath.Join(tree, "Player.json"), []byte(tc.text), 0o600); err != nil {
				t.Fatal(err)
			}
			first, _, _, err := c.Pack(u, "Player", tree)
			if err != nil {
				t.Fatal(err)
			}
			out := t.TempDir()
			if _, err := c.Unpack(u, "Player", first, out); err != nil {
				t.Fatalf("expanded unpack: %v", err)
			}
			second, _, _, err := c.Pack(u, "Player", out)
			if err != nil {
				t.Fatalf("repacking the expanded unpack: %v", err)
			}
			if !bytes.Equal(first, second) {
				t.Fatalf("unpack -> pack moved bytes: %x then %x", first, second)
			}
		})
	}
}
