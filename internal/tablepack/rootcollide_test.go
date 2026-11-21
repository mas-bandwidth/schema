package tablepack_test

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
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
			c, u, first := packText(t, tc.schema, tc.text)
			out := t.TempDir()
			unpackRepack(t, c, u, first, out)
			listing := entries(t, out)
			// a second unpack into the tree the first one left is the same tree
			unpackRepack(t, c, u, first, out)
			if again := entries(t, out); again != listing {
				t.Fatalf("a repeated unpack changed the tree: %s then %s", listing, again)
			}
		})
	}
}

// An unpack into a destination that already holds an earlier tree — an
// earlier tool's or an earlier schema's — is an upgrade, and the names there
// may differ from the new names only in case: an old expanded `player.json`
// where the new whole root is `Player.json`, or an old `Score.json` where the
// new field file is `score.json`. Whatever the filesystem folds, the
// destination ends holding exactly the new tree, spelled as written, and it
// packs back to the new save's bytes.
func TestUnpackOverAnEarlierTreeIsTheNewTree(t *testing.T) {
	for _, tc := range []struct {
		name, schema, text string
		before             map[string]string
		want               string
	}{
		{
			"expanded player.json, then the whole root Player.json",
			"package probe\ntable Player { a int32 | json = \"player\"\n score int32 }\n",
			`{"player": 7, "score": 3}`,
			map[string]string{"player.json": "7\n", "score.json": "3\n"},
			"Player.json",
		},
		{
			"the whole root PLAYER.JSON, then expanded field files",
			"package probe\ntable Player { level int32\n score int32 }\n",
			`{"level": 7, "score": 3}`,
			map[string]string{"PLAYER.JSON": "{\"level\": 1, \"score\": 1}\n"},
			"level.json score.json",
		},
		{
			"Score.json, then score.json",
			"package probe\ntable Player { level int32\n score int32 }\n",
			`{"level": 7, "score": 3}`,
			map[string]string{"Score.json": "9\n", "level.json": "1\n"},
			"level.json score.json",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := t.TempDir()
			for name, data := range tc.before {
				if err := os.WriteFile(filepath.Join(out, name), []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			c, u, save := packText(t, tc.schema, tc.text)
			unpackRepack(t, c, u, save, out)
			if got := entries(t, out); got != tc.want {
				t.Fatalf("the destination holds %q, want %q", got, tc.want)
			}
		})
	}
}

// packText loads one schema and packs one root text of Player.
func packText(t *testing.T, schema, text string) (*compiler.Compiler, *ir.Unit, []byte) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(path, []byte(schema), 0o600); err != nil {
		t.Fatal(err)
	}
	c := compiler.New()
	u, err := c.Load([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	tree := t.TempDir()
	if err := os.WriteFile(filepath.Join(tree, "Player.json"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	wire, _, _, err := c.Pack(u, "Player", tree)
	if err != nil {
		t.Fatal(err)
	}
	return c, u, wire
}

// unpackRepack unpacks wire into out, expanded, and holds the tree to packing
// back to the same bytes.
func unpackRepack(t *testing.T, c *compiler.Compiler, u *ir.Unit, wire []byte, out string) {
	t.Helper()
	if _, err := c.Unpack(u, "Player", wire, out); err != nil {
		t.Fatalf("expanded unpack: %v", err)
	}
	again, _, _, err := c.Pack(u, "Player", out)
	if err != nil {
		t.Fatalf("repacking the expanded unpack: %v", err)
	}
	if !bytes.Equal(wire, again) {
		t.Fatalf("unpack -> pack moved bytes: %x then %x", wire, again)
	}
}

// entries lists a directory's names, sorted, as the filesystem spells them.
func entries(t *testing.T, dir string) string {
	t.Helper()
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(list))
	for i, e := range list {
		names[i] = e.Name()
	}
	sort.Strings(names)
	return strings.Join(names, " ")
}
