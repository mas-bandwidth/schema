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
// the schema, nothing is the wrong shape, nothing is cut down (docs/SPEC-TABLES.md
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

// `unpack` -> `pack` is byte-stable (docs/SPEC-TABLES.md §17.2), and so is the
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

// The root may simply be one `<Root>.json` (docs/SPEC-TABLES.md §17.1's last rule),
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
// (docs/SPEC-TABLES.md §17.3), and each refusal names the file and the reason.
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
// reads (docs/SPEC-TABLES.md §16.2, §16.3, §17.5): one tree per rule the text form
// states, each with the outcome the rule requires. Two clean trees prove the
// happy path and nothing else; this is where the rules bite.
//
// The manifest is the CONFORMANCE HARNESS's, and the trees live beside it: the
// battery was always data, so it moved there whole rather than keeping a
// registry of its own, and the harness's `json-hostile` surface reads these very
// rows. One corpus, one set of expectations.
//
//	json-hostile <case> <unit> <root> <tree> <verdict>
func TestHostileValueCorpus(t *testing.T) {
	const manifestPath = "../../testdata/conformance/tables/MANIFEST.txt"
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	// the units the rows name, loaded from the manifest's own `unit` lines
	c := compiler.New()
	units := map[string]*ir.Unit{}
	for line := range strings.SplitSeq(string(manifest), "\n") {
		row := strings.Fields(line)
		if len(row) < 3 || row[0] != "unit" {
			continue
		}
		args := make([]string, 0, len(row)-2)
		for _, p := range row[2:] {
			args = append(args, "../../"+p)
		}
		paths, err := compiler.GatherPaths(args)
		if err != nil {
			t.Fatal(err)
		}
		u, err := c.Load(paths)
		if err != nil {
			t.Fatalf("unit %s: %v", row[1], err)
		}
		units[row[1]] = u
	}
	cases := 0
	for line := range strings.SplitSeq(string(manifest), "\n") {
		row := strings.Fields(line)
		if len(row) == 0 || row[0] != "json-hostile" {
			continue
		}
		if len(row) != 6 {
			t.Fatalf("%s: %q is not a json-hostile row", manifestPath, line)
		}
		name, unit, root, tree, verdict := row[1], row[2], row[3], row[4], row[5]
		u := units[unit]
		if u == nil {
			t.Fatalf("%s: the manifest names no unit %s", name, unit)
		}
		cases++
		t.Run(name, func(t *testing.T) {
			_, _, report, err := c.Pack(u, root, "../../"+tree)
			if verdict == "refused" {
				if err == nil {
					t.Fatalf("the tree packed; the manifest says it is refused")
				}
				return
			}
			if err != nil {
				t.Fatalf("the tree was refused; the manifest says it packs: %v", err)
			}
			// the VERDICT rides beside the counters (docs/SPEC-TABLES.md §3),
			// and a text read never refuses: only a form byte does
			got := fmt.Sprintf("%d,%d,%d,%d,%d,%v,read",
				report.Unknown, report.KindMismatch, report.Widened, report.Clamped, report.Duplicate, report.Malformed)
			if got != verdict {
				t.Fatalf("report %s, the manifest says %s", got, verdict)
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

// pointers loads the graphdemo unit, whose roots derive the VARIABLE mode.
func pointers(t *testing.T) (*compiler.Compiler, *ir.Unit) {
	t.Helper()
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{"../../tables/pointers"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return c, u
}

// A VARIABLE-LENGTH root is ONE text (docs/SPEC-TABLES.md §16.7, §17.2): its
// shared nodes are named by labels a text owns, so `unpack` writes `<Root>.json`
// whichever shape is asked for, and `pack` reads that file — and refuses a
// tree of fields by name, before a file is read. The pinned variable instances
// are the corpus: unpack -> pack is byte-identical to the wire each came from,
// which is what proves a text COMPLETE (a text that lost a field or an identity
// cannot pack to the bytes it came from).
func TestVariableRootIsOneText(t *testing.T) {
	c, u := pointers(t)
	for _, name := range []string{"graph_tree", "graph_shared", "graph_empty"} {
		t.Run(name, func(t *testing.T) {
			wire, err := os.ReadFile("../../testdata/wire/tables/" + name + ".bin")
			if err != nil {
				t.Fatal(err)
			}
			out := t.TempDir()
			// the EXPANDED shape is asked for and the one-file shape is what
			// a variable root writes
			report, err := c.Unpack(u, "Scene", wire, out)
			if err != nil {
				t.Fatal(err)
			}
			if !report.Silent() {
				t.Fatalf("the pinned wire did not read clean: %+v", report)
			}
			entries, err := os.ReadDir(out)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != "Scene.json" {
				t.Fatalf("a variable root unpacks as one Scene.json; got %d entries", len(entries))
			}
			back, _, report, err := c.Pack(u, "Scene", out)
			if err != nil {
				t.Fatal(err)
			}
			if !report.Silent() {
				t.Fatalf("the engine's own text did not read clean: %+v", report)
			}
			if !bytes.Equal(back, wire) {
				t.Fatalf("unpack -> pack moved bytes: %d back against %d pinned", len(back), len(wire))
			}
		})
	}

	// a tree of fields under a variable root is refused by name, and nothing
	// beside the refusal comes back
	t.Run("tree of fields refused", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "name.json"), []byte(`"split"`), 0o644); err != nil {
			t.Fatal(err)
		}
		_, _, _, err := c.Pack(u, "Scene", dir)
		if err == nil {
			t.Fatal("pack accepted a tree of fields under a variable root")
		}
		if !strings.Contains(err.Error(), "VARIABLE-LENGTH") || !strings.Contains(err.Error(), "Scene.json") {
			t.Fatalf("the refusal does not name the class and the one file it packs from: %v", err)
		}
	})
}

// A chain nests in the text as deep as it is long (docs/SPEC-TABLES.md §16.7),
// and the text form's depth cap bounds it: the corpus's 260-node chain has a
// wire and no text, in this engine as in the reference, so `unpack` refuses it
// by depth before a file is written.
func TestVariableRootPastTheDepthCapHasNoText(t *testing.T) {
	c, u := pointers(t)
	wire, err := os.ReadFile("../../testdata/wire/tables/graph_deep.bin")
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	_, err = c.Unpack(u, "Scene", wire, out)
	if err == nil {
		t.Fatal("unpack wrote a text past the depth cap")
	}
	if !strings.Contains(err.Error(), "depth cap") {
		t.Fatalf("the refusal does not name the cap: %v", err)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("unpack wrote %d entries before refusing", len(entries))
	}
}

// A PINNED TEXT (docs/SPEC-TABLES.md §16.5): what `unpack` writes for each corpus
// root, committed byte for byte. A round trip alone cannot see a vocabulary
// error — reader and writer share the name function, so a wrong spelling round
// trips perfectly — and it cannot see the pretty-print contract drift either.
//
// §17.1's third golden covers the WHOLE-root text against the backend's
// `ToJson` (make tables-pack); this covers the shape that has no `ToJson` to
// compare to, the EXPANDED tree of per-field files, where a field's own text
// sits at depth 0 rather than nested inside the root's.
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

// §17.2's last rule as an output shape: `unpack --one-file` writes one
// `<Root>.json`, and it is the same instance through the same writer, so it
// packs to the bytes the expanded tree does. It is also the text §17.1's third
// golden hands the backend's `ToJson`.
func TestUnpackOneFile(t *testing.T) {
	c, u := corpus(t)
	for _, tree := range trees {
		t.Run(tree.root, func(t *testing.T) {
			wire, _, _, err := c.Pack(u, tree.root, tree.dir)
			if err != nil {
				t.Fatal(err)
			}
			out := t.TempDir()
			if _, err := c.UnpackOneFile(u, tree.root, wire, out); err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(out)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != tree.root+".json" {
				t.Fatalf("--one-file wrote %d entries", len(entries))
			}
			again, _, report, err := c.Pack(u, tree.root, out)
			if err != nil {
				t.Fatal(err)
			}
			if !report.Silent() {
				t.Fatalf("the one-file form should read back clean: %+v", report)
			}
			if !bytes.Equal(wire, again) {
				t.Fatal("the one-file form is not the same instance")
			}
		})
	}
}

// §17.3: the prune covers the ROOT'S WHOLE SHAPE, not just the shape being
// written, so unpacking either form over the other leaves a tree `pack`
// accepts. Without it the tool writes a tree its own sibling verb refuses
// ("a root is one file or one tree of fields, never both").
func TestUnpackPrunesAcrossShapes(t *testing.T) {
	c, u := corpus(t)
	for _, tree := range trees {
		t.Run(tree.root, func(t *testing.T) {
			wire, _, _, err := c.Pack(u, tree.root, tree.dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, order := range []struct {
				name  string
				first func(*ir.Unit, string, []byte, string) (compiler.TableReport, error)
				then  func(*ir.Unit, string, []byte, string) (compiler.TableReport, error)
			}{
				{"expanded then one-file", c.Unpack, c.UnpackOneFile},
				{"one-file then expanded", c.UnpackOneFile, c.Unpack},
			} {
				t.Run(order.name, func(t *testing.T) {
					work := t.TempDir()
					if _, err := order.first(u, tree.root, wire, work); err != nil {
						t.Fatal(err)
					}
					if _, err := order.then(u, tree.root, wire, work); err != nil {
						t.Fatal(err)
					}
					again, _, report, err := c.Pack(u, tree.root, work)
					if err != nil {
						t.Fatalf("the tree the second unpack left is one pack refuses:\n%v", err)
					}
					if !report.Silent() {
						t.Fatalf("expected silence, got %+v", report)
					}
					if !bytes.Equal(wire, again) {
						t.Fatal("switching shapes moved bytes")
					}
				})
			}
		})
	}
}
