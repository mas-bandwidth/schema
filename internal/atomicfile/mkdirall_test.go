package atomicfile

import (
	"errors"
	"path/filepath"
	"testing"
)

// A DIRECTORY THE TRANSACTION MADE IS ITS OWN: a transaction that ends
// without publishing removes the empty directories MkdirAll made for it, the
// ones it found are left, one that holds an entry is never removed, and a
// commit that publishes keeps them all.
func TestMkdirAllIsUndoneWithTheTransaction(t *testing.T) {
	t.Parallel()
	t.Run("closed without a commit", func(t *testing.T) {
		dir := tempDir(t)
		put(t, filepath.Join(dir, "there", "keep.txt"), "keep", 0o644)
		txn := New()
		for _, d := range []string{"there/new/deeper", "fresh/a", "fresh/b", "holds"} {
			if err := txn.MkdirAll(filepath.Join(dir, d)); err != nil {
				t.Fatal(err)
			}
		}
		// something else lands in one of them before the end
		put(t, filepath.Join(dir, "holds", "theirs.txt"), "theirs", 0o644)
		txn.Close()
		if got, want := names(t, dir), "holds/ holds/theirs.txt there/ there/keep.txt"; got != want {
			t.Errorf("after the transaction ended the directory holds %q; want %q", got, want)
		}
	})
	t.Run("a commit that fails", func(t *testing.T) {
		dir := tempDir(t)
		txn := New()
		out := filepath.Join(dir, "made", "out")
		if err := txn.MkdirAll(out); err != nil {
			t.Fatal(err)
		}
		txn.Write(filepath.Join(out, "a.txt"), []byte("a"), 0o644)
		txn.Write(filepath.Join(out, "b.txt"), []byte("b"), 0o644)
		fault := FailNthRename(out, 2)
		err := txn.Commit()
		fault.Remove()
		if !errors.Is(err, ErrInjected) {
			t.Fatalf("Commit = %v; want the injected failure", err)
		}
		if got := names(t, dir); got != "" {
			t.Errorf("after the failed commit the directory holds %q; want nothing", got)
		}
	})
	t.Run("a commit that publishes", func(t *testing.T) {
		dir := tempDir(t)
		txn := New()
		out := filepath.Join(dir, "made", "out")
		if err := txn.MkdirAll(out); err != nil {
			t.Fatal(err)
		}
		if err := txn.MkdirAll(filepath.Join(dir, "empty")); err != nil {
			t.Fatal(err)
		}
		txn.Write(filepath.Join(out, "a.txt"), []byte("a"), 0o644)
		if err := txn.Commit(); err != nil {
			t.Fatal(err)
		}
		txn.Close()
		if got, want := names(t, dir), "empty/ made/ made/out/ made/out/a.txt"; got != want {
			t.Errorf("after the commit the directory holds %q; want %q", got, want)
		}
		if err := txn.MkdirAll(filepath.Join(dir, "late")); err == nil {
			t.Error("MkdirAll on an ended transaction answered")
		}
	})
}
