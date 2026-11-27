package tablepack_test

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/compiler"
	"github.com/mas-bandwidth/schema/v3/ir"
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

// An unpack into a destination that already holds an earlier tree is an
// upgrade, and an old name may differ from a new one only in case: an old
// expanded `player.json` where the new whole root is `Player.json`, an old
// `Score.json` where the new field file is `score.json`. Where the filesystem
// folds case the old entry IS the new name: the destination ends holding the
// new tree, spelled as written, and it packs back to the new save's bytes.
// Where it does not, an entry that only folds to a field's name names no field
// (docs/SPEC-TABLES.md §17.3) and stays where it is.
func TestUnpackOverAnEarlierTreeIsTheNewTree(t *testing.T) {
	for _, tc := range []struct {
		name, schema, text string
		before             map[string]string
		want, wantExact    string // the tree on a folding / an exact filesystem
	}{
		{
			"expanded player.json, then the whole root Player.json",
			"package probe\ntable Player { a int32 | json = \"player\"\n score int32 }\n",
			`{"player": 7, "score": 3}`,
			map[string]string{"player.json": "7\n", "score.json": "3\n"},
			"Player.json", "Player.json",
		},
		{
			"Score.json, then score.json",
			"package probe\ntable Player { level int32\n score int32 }\n",
			`{"level": 7, "score": 3}`,
			map[string]string{"Score.json": "9\n", "level.json": "1\n"},
			"level.json score.json", "Score.json level.json score.json",
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
			if _, err := c.Unpack(u, "Player", save, out); err != nil {
				t.Fatalf("expanded unpack: %v", err)
			}
			// which filesystem this is shows in the names it kept
			switch got := entries(t, out); got {
			case tc.want:
				unpackRepack(t, c, u, save, out)
			case tc.wantExact:
				// the unowned entry stays, and pack names it rather than merge it
				if _, _, _, err := c.Pack(u, "Player", out); err == nil {
					t.Fatal("pack merged an entry that names no field")
				}
			default:
				t.Fatalf("the destination holds %q, want %q (folding) or %q (exact)", got, tc.want, tc.wantExact)
			}
		})
	}
}

// An entry that stands where the unpack writes and names no field the schema
// owns at that level is never deleted (docs/SPEC-TABLES.md §17.3): the unpack
// is refused by naming it, before anything is written, at each of the four
// levels — the whole root's file, a field's file, an enum-keyed array's
// directory, and a variant's file inside it.
func TestUnpackNeverDeletesAnEntryNamingNoField(t *testing.T) {
	const keyed = "package probe\nenum Team { Red, Blue }\ntable Player { level int32\n scores [Team]int32 }\n"
	for _, tc := range []struct {
		name, schema, text string
		dirs               []string // directories made first
		files              map[string]string
		blocker            string
	}{
		{
			"the whole root's file",
			"package probe\ntable Player { level int32 | json = \"Player\"\n score int32 }\n",
			`{"Player": 7, "score": 3}`,
			[]string{"Player.json"}, map[string]string{"Player.json/notes.txt": "mine\n"},
			"Player.json",
		},
		{
			"a field's file",
			"package probe\ntable Player { level int32\n score int32 }\n",
			`{"level": 7, "score": 3}`,
			[]string{"level.json"}, map[string]string{"level.json/notes.txt": "mine\n"},
			"level.json",
		},
		{
			"an enum-keyed array's directory",
			keyed,
			`{"level": 7, "scores": {"Red": 1, "Blue": 2}}`,
			nil, map[string]string{"scores": "mine\n"},
			"scores",
		},
		{
			"a variant's file",
			keyed,
			`{"level": 7, "scores": {"Red": 1, "Blue": 2}}`,
			[]string{"scores", "scores/Red.json"}, map[string]string{"scores/Red.json/notes.txt": "mine\n"},
			"Red.json",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := t.TempDir()
			for _, d := range tc.dirs {
				if err := os.MkdirAll(filepath.Join(out, d), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			for name, data := range tc.files {
				if err := os.WriteFile(filepath.Join(out, name), []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			before := entries(t, out)
			c, u, save := packText(t, tc.schema, tc.text)
			_, err := c.Unpack(u, "Player", save, out)
			if err == nil || !strings.Contains(err.Error(), tc.blocker+" is in the way") {
				t.Fatalf("unpack must refuse by naming %s: %v", tc.blocker, err)
			}
			for name, data := range tc.files {
				got, err := os.ReadFile(filepath.Join(out, name))
				if err != nil || string(got) != data {
					t.Fatalf("%s did not survive the refused unpack: %q %v", name, got, err)
				}
			}
			if after := entries(t, out); after != before {
				t.Fatalf("a refused unpack wrote: %q then %q", before, after)
			}
		})
	}
}

// An existing keyed directory spelled in another case (`Scores/` where the
// field writes `scores/`) may hold an entry no schema owns beside the variant
// files. Where the filesystem folds case, that directory IS the field's: it is
// renamed to the written spelling with every child in it, and the variants are
// written into it. Where it does not, it is another entry and stays as it is.
// The unrelated child survives either way.
func TestUnpackKeepsTheChildrenOfAKeyedDirectorySpelledOtherwise(t *testing.T) {
	const schema = "package probe\nenum Team { Red, Blue }\ntable Player { level int32\n scores [Team]int32 }\n"
	out := t.TempDir()
	if err := os.MkdirAll(filepath.Join(out, "Scores"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"Scores/notes.txt": "mine\n", "Scores/Red.json": "9\n"} {
		if err := os.WriteFile(filepath.Join(out, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	c, u, save := packText(t, schema, `{"level": 7, "scores": {"Red": 1, "Blue": 2}}`)
	if _, err := c.Unpack(u, "Player", save, out); err != nil {
		t.Fatalf("expanded unpack: %v", err)
	}
	// which filesystem this is shows in the names it kept: the entries are
	// read back by the names the filesystem lists
	listed, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	var keeper, keyed string // the directory holding notes.txt, and the field's
	switch got := entries(t, out); got {
	case "level.json scores": // folding: one directory, renamed
		keeper, keyed = listed[1].Name(), listed[1].Name()
	case "Scores level.json scores": // exact: two directories
		keeper, keyed = listed[0].Name(), listed[2].Name()
	default:
		t.Fatalf("the destination holds %q", got)
	}
	wantKeyed := "Blue.json Red.json"
	if keeper == keyed {
		wantKeyed += " notes.txt"
	}
	if got := entries(t, filepath.Join(out, keyed)); got != wantKeyed {
		t.Fatalf("%s/ holds %q, want %q", keyed, got, wantKeyed)
	}
	if got, err := os.ReadFile(filepath.Join(out, keeper, "notes.txt")); err != nil || string(got) != "mine\n" {
		t.Fatalf("the unrelated child did not survive: %q %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(out, keyed, "Red.json")); err != nil || strings.TrimSpace(string(got)) != "1" {
		t.Fatalf("scores/Red.json holds %q (%v), want the save's 1", got, err)
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
