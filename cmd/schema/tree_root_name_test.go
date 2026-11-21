package main

import (
	"path/filepath"
	"testing"
)

// A JSON key named for its root needs the whole-root file. A table whose
// keys have no collision still unpacks into editable per-field files.
func TestRootNameTreeCorpus(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	corpus := filepath.Join("testdata", "tree_root_name")
	unit := filepath.Join(corpus, "Roots.schema")
	for _, root := range []string{"Player", "Plain"} {
		t.Run(root, func(t *testing.T) {
			work := t.TempDir()
			pinned := filepath.Join(corpus, root+".bin")
			saved := mustRead(t, pinned)
			packed := filepath.Join(work, "from-input.bin")
			run(t, bin, "pack", "--root", root, "--out", packed, filepath.Join(corpus, "input", root), unit)
			if mustRead(t, packed) != saved {
				t.Fatal("the authored input and the saved wire disagree")
			}

			tree := filepath.Join(work, "tree")
			run(t, bin, "unpack", "--root", root, "--in", pinned, tree, unit)
			compareTrees(t, root, filepath.Join(corpus, "expected", root), tree)
			repacked := filepath.Join(work, "from-tree.bin")
			run(t, bin, "pack", "--root", root, "--out", repacked, tree, unit)
			if mustRead(t, repacked) != saved {
				t.Fatal("unpacking and repacking changed the saved wire")
			}
		})
	}
}
