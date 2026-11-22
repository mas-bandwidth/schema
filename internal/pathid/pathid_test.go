package pathid

import (
	"os"
	"path/filepath"
	"testing"
)

func tree(t *testing.T) (root, in, out string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	in = filepath.Join(root, "tree")
	out = filepath.Join(root, "elsewhere")
	for _, d := range []string{filepath.Join(in, "nested"), out} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(in, "level.json"), []byte("7"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, in, out
}

// Inside goes by identity: every spelling that reaches the tree is inside it,
// whether the entry exists or not, and nothing else is.
func TestInside(t *testing.T) {
	root, in, out := tree(t)
	if err := os.Symlink("tree", filepath.Join(root, "alias")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(in, "level.json"), filepath.Join(out, "points-in")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path string
		want bool
	}{
		{filepath.Join(in, "level.json"), true},
		{filepath.Join(in, "fresh.bin"), true},
		{filepath.Join(in, "not-yet", "deeper", "fresh.bin"), true},
		{filepath.Join(root, "alias", "level.json"), true},
		{filepath.Join(root, "alias", "fresh.bin"), true},
		{filepath.Join(in, "nested", "..", "level.json"), true},
		{filepath.Join(out, "points-in"), true},
		{in, true},
		{filepath.Join(out, "fresh.bin"), false},
		{filepath.Join(out, "not-yet", "fresh.bin"), false},
		{filepath.Join(root, "tree-sibling.bin"), false},
		{root, false},
	} {
		if got := Inside(tc.path, info); got != tc.want {
			t.Errorf("Inside(%s) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestOverlap(t *testing.T) {
	root, in, out := tree(t)
	if err := os.Symlink("tree", filepath.Join(root, "alias")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	nested := filepath.Join(in, "nested")
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{in, in, true},
		{in, filepath.Join(root, "alias"), true},
		{in, nested, true},
		{nested, in, true},
		{filepath.Join(root, "alias", "nested"), in, true},
		{in, out, false},
		{nested, out, false},
	} {
		got, err := Overlap(tc.a, tc.b)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("Overlap(%s, %s) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
	if _, err := Overlap(filepath.Join(root, "missing"), in); err == nil {
		t.Error("Overlap of a directory that does not exist answered")
	}
}
