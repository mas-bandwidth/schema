package tablepack_test

import (
	"bytes"
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
			wire, report, err := c.Pack(u, tree.root, tree.dir)
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
	first, _, err := c.Pack(u, root, dir)
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
	second, _, err := c.Pack(u, root, out)
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
	third, _, err := c.Pack(u, root, again)
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
	tree, _, err := c.Pack(u, "PackConfig", "../../tables/pack/config")
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
	got, report, err := c.Pack(u, "PackConfig", whole)
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
			_, _, err := c.Pack(u, "PackConfig", "../../tables/pack/hostile/"+tc.tree)
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
	_, _, err := c.Pack(u, "NotATable", "../../tables/pack/config")
	if err == nil || !strings.Contains(err.Error(), "names no table") {
		t.Fatalf("expected a refusal naming the roots, got %v", err)
	}
}
