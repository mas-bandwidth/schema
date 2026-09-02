package tablepack_test

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// corpus loads the tabledemo unit the packed corpus declares its root in.
func corpus(t *testing.T) (*compiler.Compiler, *ir.Unit) {
	t.Helper()
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{"../../tables/examples"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return c, u
}

// trees are the packed corpus: the fixed-class collection shape, and the root
// that reaches the kinds it does not — a union, a flags mask, a guarded group,
// bytes, bits and every integer width.
var trees = []struct{ root, dir string }{
	{"PackConfig", "../../tables/pack/config"},
	{"RootConfig", "../../tables/pack/root"},
}

// The corpus trees pack, and they pack SILENTLY: nothing in them is unknown to
// the schema, nothing is the wrong shape, nothing is cut down (SPEC-TABLES.md
// §17.3).
func TestPackCorpusIsSilent(t *testing.T) {
	c, u := corpus(t)
	for _, tree := range trees {
		t.Run(tree.root, func(t *testing.T) {
			wire, _, report, err := c.Pack(u, tree.root, tree.dir)
			if err != nil {
				t.Fatal(err)
			}
			if !report.Silent() {
				t.Fatalf("the corpus tree should match the schema exactly: %+v", report)
			}
			if len(wire) == 0 {
				t.Fatal("pack produced no bytes")
			}
		})
	}
}

// `unpack` -> `pack` is byte-stable (SPEC-TABLES.md §17.2), and so is the
// second lap: the text form loses nothing the wire carried.
func TestUnpackThenPackIsByteStable(t *testing.T) {
	c, u := corpus(t)
	for _, tree := range trees {
		t.Run(tree.root, func(t *testing.T) { byteStable(t, c, u, tree.root, tree.dir) })
	}
}

func byteStable(t *testing.T, c *compiler.Compiler, u *ir.Unit, root, dir string) {
	t.Helper()
	first, _, _, err := c.Pack(u, root, dir)
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	report, err := c.Unpack(u, root, first, out)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Silent() {
		t.Fatalf("unpacking bytes this build wrote should report nothing: %+v", report)
	}
	second, _, _, err := c.Pack(u, root, out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("unpack -> pack moved bytes: %d then %d", len(first), len(second))
	}
	again := t.TempDir()
	if _, err := c.Unpack(u, root, second, again); err != nil {
		t.Fatal(err)
	}
	third, _, _, err := c.Pack(u, root, again)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, third) {
		t.Fatal("the second lap moved bytes")
	}
}

// The root may simply be one `<Root>.json` (SPEC-TABLES.md §17.1's last rule),
// and it packs to the same bytes the field tree does.
func TestRootAsOneFile(t *testing.T) {
	c, u := corpus(t)
	tree, _, _, err := c.Pack(u, "PackConfig", "../../tables/pack/config")
	if err != nil {
		t.Fatal(err)
	}
	// the whole instance as one text, written back as the single root file
	spread := t.TempDir()
	if _, err := c.Unpack(u, "PackConfig", tree, spread); err != nil {
		t.Fatal(err)
	}
	whole := t.TempDir()
	one := &bytes.Buffer{}
	one.WriteString("{")
	// assemble the one-file form out of the field files the unpack wrote, so
	// this test is over the DIRECTORY RULE and not over a second hand-written
	// corpus that could drift from the first
	entries, err := os.ReadDir(spread)
	if err != nil {
		t.Fatal(err)
	}
	first := true
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		text, err := os.ReadFile(filepath.Join(spread, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if !first {
			one.WriteString(",")
		}
		first = false
		one.WriteString("\n\"" + strings.TrimSuffix(e.Name(), ".json") + "\": ")
		one.Write(bytes.TrimSpace(text))
	}
	// the keyed fields live in directories; add them as objects keyed by name
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		slots, err := os.ReadDir(filepath.Join(spread, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		one.WriteString(",\n\"" + e.Name() + "\": {")
		firstSlot := true
		for _, s := range slots {
			text, err := os.ReadFile(filepath.Join(spread, e.Name(), s.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if !firstSlot {
				one.WriteString(",")
			}
			firstSlot = false
			one.WriteString("\n\"" + strings.TrimSuffix(s.Name(), ".json") + "\": ")
			one.Write(bytes.TrimSpace(text))
		}
		one.WriteString("}")
	}
	one.WriteString("\n}\n")
	if err := os.WriteFile(filepath.Join(whole, "PackConfig.json"), one.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _, report, err := c.Pack(u, "PackConfig", whole)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Silent() {
		t.Fatalf("the one-file root should match the schema exactly: %+v", report)
	}
	if !bytes.Equal(tree, got) {
		t.Fatal("the one-file root and the field tree are not the same instance")
	}
}

// A tree that does not mirror the table is REPORTED rather than guessed at
// (SPEC-TABLES.md §17.3), and each refusal names the file and the reason.
func TestHostileTreesAreRefused(t *testing.T) {
	cases := []struct {
		tree string
		says string
	}{
		{"unknown-dir", "names no field of table PackConfig"},
		{"unknown-file", "names no field of table PackConfig"},
		{"two-claims", "both claim field"},
		{"bad-variant", "is not a variant of enum ShipType"},
		{"none-slot", "None keys no record"},
		{"not-json", "not one JSON value"},
		{"root-and-fields", "is the whole root"},
		{"stray-file", "names no field"},
	}
	c, u := corpus(t)
	for _, tc := range cases {
		t.Run(tc.tree, func(t *testing.T) {
			_, _, _, err := c.Pack(u, "PackConfig", "../../tables/pack/hostile/"+tc.tree)
			if err == nil {
				t.Fatal("the tree was accepted")
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Fatalf("refusal does not name the reason %q:\n%s", tc.says, err)
			}
		})
	}
}

// `--root` naming something that is not a table is refused with the roots the
// unit does declare.
func TestUnknownRootIsRefused(t *testing.T) {
	c, u := corpus(t)
	_, _, _, err := c.Pack(u, "NotATable", "../../tables/pack/config")
	if err == nil || !strings.Contains(err.Error(), "names no table") {
		t.Fatalf("expected a refusal naming the roots, got %v", err)
	}
}

// The HOSTILE-VALUE corpus, over the same manifest the backend half of the gate
// reads (SPEC-TABLES.md §16.2, §16.3, §17.5): one tree per rule the text form
// states, each with the outcome the rule requires. Two clean trees prove the
// happy path and nothing else; this is where the rules bite.
func TestHostileValueCorpus(t *testing.T) {
	c, u := corpus(t)
	manifest, err := os.ReadFile("../../tables/pack/hostile-values/cases.txt")
	if err != nil {
		t.Fatal(err)
	}
	cases := 0
	for line := range strings.SplitSeq(string(manifest), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		row := strings.Fields(line)
		if len(row) < 3 {
			t.Fatalf("cases.txt: %q is neither blank, a comment, nor a case", line)
		}
		name, root, outcome := row[0], row[1], row[2]
		cases++
		t.Run(name, func(t *testing.T) {
			_, _, report, err := c.Pack(u, root, "../../tables/pack/hostile-values/"+name)
			if outcome == "refused" {
				if err == nil {
					t.Fatalf("the tree packed; the manifest says it is refused")
				}
				return
			}
			if outcome != "packs" || len(row) < 4 {
				t.Fatalf("cases.txt: %q names no outcome this gate knows", line)
			}
			if err != nil {
				t.Fatalf("the tree was refused; the manifest says it packs: %v", err)
			}
			got := fmt.Sprintf("%d,%d,%d,%d,%v",
				report.Unknown, report.KindMismatch, report.Clamped, report.Duplicate, report.Malformed)
			if got != row[3] {
				t.Fatalf("report %s, the manifest says %s", got, row[3])
			}
		})
	}
	if cases < 30 {
		t.Fatalf("the manifest carries %d cases; it is meant to cover every rule §16 states", cases)
	}
}

// §17.3: `unpack` -> `pack` is byte-stable into a directory that ALREADY holds
// a tree, which is the only directory the verb is ever pointed at. An absent
// `?T` and a guarded-out field write no file, so a stale one left standing
// would resurrect a value the newer bytes do not carry.
func TestUnpackPrunesWhatItDoesNotWrite(t *testing.T) {
	c, u := corpus(t)
	rich, _, _, err := c.Pack(u, "PackConfig", "../../tables/pack/config")
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	if _, err := c.Unpack(u, "PackConfig", rich, work); err != nil {
		t.Fatal(err)
	}
	// a stale spelling of a field that IS written, and one of a field that is
	// not: both name something the root owns, so both must go
	if err := os.MkdirAll(filepath.Join(work, "global"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "global", "tick_rate.json"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// a sparse instance: the ships all-default, so their gunner optionals are absent
	sparse := t.TempDir()
	if err := os.WriteFile(filepath.Join(sparse, "PackConfig.json"), []byte(`{ "version": 3 }`), 0o644); err != nil {
		t.Fatal(err)
	}
	thin, _, _, err := c.Pack(u, "PackConfig", sparse)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Unpack(u, "PackConfig", thin, work); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(work, "global")); !os.IsNotExist(err) {
		t.Fatal("a stale directory spelling of a written field survived the unpack")
	}
	again, _, _, err := c.Pack(u, "PackConfig", work)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(thin, again) {
		t.Fatalf("unpack -> pack into a populated tree moved bytes: %d then %d", len(thin), len(again))
	}
	// what the tree does not own is left exactly where it is, for pack to name
	if err := os.WriteFile(filepath.Join(work, "notes.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Unpack(u, "PackConfig", thin, work); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(work, "notes.txt")); err != nil {
		t.Fatal("unpack removed a file that names no field")
	}
}

// A hidden file that is not JSON is passed over and NAMED, so nothing a tree
// walk skips is invisible; a hidden `.json` file still names something and is
// refused if it names no field.
func TestHiddenEntries(t *testing.T) {
	c, u := corpus(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "PackConfig.json"), []byte(`{ "version": 3 }`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".DS_Store"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, skipped, _, err := c.Pack(u, "PackConfig", dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 1 || !strings.HasSuffix(skipped[0], ".DS_Store") {
		t.Fatalf("the walk did not name what it passed over: %v", skipped)
	}
	if err := os.WriteFile(filepath.Join(dir, ".version.json"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := c.Pack(u, "PackConfig", dir); err == nil {
		t.Fatal("a hidden .json file naming no field was swallowed")
	}
}

// A VARIABLE-LENGTH root is refused by name on BOTH verbs, and unpack refuses
// before it writes anything: its text form reads through a builder, a named
// follow-on (SPEC-TABLES.md §16.1, §15).
func TestVariableRootRefusedOnBothVerbs(t *testing.T) {
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{"../../tables/pointers"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	variable := ir.VariableTables(u)
	found := false
	for name := range u.Tables {
		if !variable[name] {
			continue
		}
		found = true
		out := t.TempDir()
		if _, _, _, err := c.Pack(u, name, out); err == nil {
			t.Fatalf("pack accepted the variable-length root %s", name)
		}
		if _, err := c.Unpack(u, name, nil, out); err == nil {
			t.Fatalf("unpack accepted the variable-length root %s", name)
		}
		entries, err := os.ReadDir(out)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("unpack wrote %d entries before refusing %s", len(entries), name)
		}
	}
	if !found {
		t.Skip("the pointers corpus declares no variable-length root")
	}
}

// A PINNED TEXT (SPEC-TABLES.md §16.5): what `unpack` writes for each corpus
// root, committed byte for byte. A round trip alone cannot see a vocabulary
// error — reader and writer share the name function, so a wrong spelling round
// trips perfectly — and it cannot see the pretty-print contract drift either.
// This is the file the THIRD golden of §17.1 compares against once a backend
// emits `ToJson`: the same texts, from the other implementation.
func TestUnpackMatchesThePinnedText(t *testing.T) {
	c, u := corpus(t)
	for _, tree := range trees {
		t.Run(tree.root, func(t *testing.T) {
			wire, _, _, err := c.Pack(u, tree.root, tree.dir)
			if err != nil {
				t.Fatal(err)
			}
			out := t.TempDir()
			if _, err := c.Unpack(u, tree.root, wire, out); err != nil {
				t.Fatal(err)
			}
			pinned := filepath.Join("../../tables/pack/pinned", tree.root)
			compareTrees(t, pinned, out)
		})
	}
}

// compareTrees asserts two trees hold the same files with the same bytes.
func compareTrees(t *testing.T, want, got string) {
	t.Helper()
	wantFiles := treeFiles(t, want)
	gotFiles := treeFiles(t, got)
	for rel, text := range wantFiles {
		other, ok := gotFiles[rel]
		if !ok {
			t.Fatalf("%s: unpack wrote no such file", rel)
		}
		if !bytes.Equal(text, other) {
			t.Fatalf("%s: unpack's text is not the pinned one\n--- pinned ---\n%s\n--- unpack ---\n%s", rel, text, other)
		}
		delete(gotFiles, rel)
	}
	for rel := range gotFiles {
		t.Fatalf("%s: unpack wrote a file the pinned tree does not carry", rel)
	}
}

func treeFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		text, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = text
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatalf("%s holds no files", root)
	}
	return out
}
