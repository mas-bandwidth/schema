// Package pincorpus is the committed corpus of `schema pin` and the release
// gate beside it: every spelling, adopter tree and release history a review
// finding turned on, with the verdict each must get, as data the tests read
// instead of literals of their own. It lives in testdata/pin:
//
//	versions/*.json  version spellings: the verdict, and the module path's
//	                 major suffix when accepted
//	trees/*.json     adopter project trees: a layout, and cases that pin it
//	                 (some with a file operation made to fail), each with the
//	                 tree expected afterwards
//	releases/*.json  release histories: the tags already cut and the module
//	                 path each declares, a go.mod, a boundary and a version,
//	                 with the gate's verdict
//
// One command runs the whole corpus, with no network, no clock and no built
// binary:
//
//	go test -count=1 -run PinCorpus ./internal/modulemove/... ./cmd/schema ./tools/releasetag
//
// cmd/schema runs the spellings and the trees through the pin command's own
// code, internal/modulemove the spellings and the histories through the
// rules, tools/releasetag the histories through the tagger and a fake git.
// This package's own test fails when a kind is empty, a file is of no kind or
// a file is not read by its loader. A module path is never written whole
// here, only its major suffix ("/v2"), so the module-path move program leaves
// the corpus alone.
package pincorpus

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"slices"
)

//go:embed testdata/pin
var files embed.FS

// Root is the corpus directory inside this package.
const Root = "testdata/pin"

// Spelling is one version spelling and what pin must do with it.
type Spelling struct {
	ID      string `json:"id"`
	Finding string `json:"finding"`
	Note    string `json:"note"`
	Version string `json:"version"`
	// Boundary is the first v3 tag whose go.mod names /v3; "" is none yet.
	Boundary string `json:"boundary"`
	// Verdict is "accept" or "refuse".
	Verdict string `json:"verdict"`
	// Path is the major suffix of the module path an accepted version is
	// pinned under: "" for v0 and v1, "/vN" from v2 on.
	Path string `json:"path"`
	// Reason is a phrase the refusal must carry.
	Reason string `json:"reason"`
	File   string `json:"-"`
}

// Entry is one directory entry of an adopter tree: a regular file holding
// Lines, a symbolic link to Symlink, or a hard link to the entry at Hardlink.
// A line may hold "@PIN <suffix> <version>@", which stands for the pin
// directive at that version under the module path with that major suffix
// ("-" for none).
type Entry struct {
	Path     string   `json:"path"`
	Lines    []string `json:"lines"`
	Symlink  string   `json:"symlink"`
	Hardlink string   `json:"hardlink"`
}

// Fail makes file operations of a pin fail. StageIn fails every temporary
// created in that directory. Otherwise the Nth rename onto Onto fails for
// From <= N <= Until (Until 0: every one from From on).
type Fail struct {
	StageIn string `json:"stage_in"`
	Onto    string `json:"onto"`
	From    int    `json:"from"`
	Until   int    `json:"until"`
}

// TreeCase is one pin of a layout.
type TreeCase struct {
	ID       string   `json:"id"`
	Finding  string   `json:"finding"`
	Note     string   `json:"note"`
	Version  string   `json:"version"`
	Boundary string   `json:"boundary"`
	Dirs     []string `json:"dirs"`
	Fail     []Fail   `json:"fail"`
	// Error is a phrase the failure must carry; "" means the pin succeeds.
	Error string `json:"error"`
	// After lists the files that hold new content afterwards. Every entry
	// of the layout not listed must be the entry it was: same file, same
	// bytes, same mode, same time last written, a link still a link.
	After []Entry `json:"after"`
	// HalfMoved lists the files whose original could not be put back: each
	// is named in the error with where its original is kept.
	HalfMoved []string `json:"half_moved"`
	Layout    []Entry  `json:"-"`
	File      string   `json:"-"`
}

type treeFile struct {
	Layout []Entry    `json:"layout"`
	Cases  []TreeCase `json:"cases"`
}

// ReleaseCase is one question to a gate of internal/modulemove.
type ReleaseCase struct {
	ID      string `json:"id"`
	Finding string `json:"finding"`
	Note    string `json:"note"`
	// Gate is "release" (ReleaseGate), "boundary" (BoundaryGate) or "tree"
	// (TreeGate).
	Gate    string `json:"gate"`
	Version string `json:"version"`
	// GoMod is the major suffix of the module path the go.mod being
	// released declares ("/v2"), "base" for the unversioned path, or
	// "missing" for a go.mod with no module line.
	GoMod    string `json:"gomod"`
	Boundary string `json:"boundary"`
	// Tags maps each release tag already cut to the major suffix its own
	// go.mod declares.
	Tags map[string]string `json:"tags"`
	// Verdict is "accept", "module_mismatch", "boundary_covers",
	// "boundary_stale" or "no_module".
	Verdict string `json:"verdict"`
	// Tagger marks a case tools/releasetag also drives through Release.
	Tagger bool   `json:"tagger"`
	File   string `json:"-"`
}

// Kinds are the corpus's directories.
var Kinds = []string{"versions", "trees", "releases"}

// Files lists every file of the corpus, by walking the folder.
func Files() ([]string, error) {
	var out []string
	err := fs.WalkDir(files, Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			out = append(out, p)
		}
		return nil
	})
	slices.Sort(out)
	return out, err
}

// load decodes every file of one kind, strictly, into one value per file.
func load[T any](kind string, each func(file string, v T) error) error {
	dir := path.Join(Root, kind)
	entries, err := files.ReadDir(dir)
	if err != nil {
		return err
	}
	n := 0
	for _, e := range entries {
		file := path.Join(dir, e.Name())
		if e.IsDir() || path.Ext(file) != ".json" {
			return fmt.Errorf("pincorpus: %s is not a case file", file)
		}
		src, err := files.ReadFile(file)
		if err != nil {
			return err
		}
		dec := json.NewDecoder(bytes.NewReader(src))
		dec.DisallowUnknownFields()
		var v T
		if err := dec.Decode(&v); err != nil {
			return fmt.Errorf("pincorpus: %s: %w", file, err)
		}
		if err := each(file, v); err != nil {
			return fmt.Errorf("pincorpus: %s: %w", file, err)
		}
		n++
	}
	if n == 0 {
		return fmt.Errorf("pincorpus: %s holds no case file", dir)
	}
	return nil
}

func oneOf(what, got string, want ...string) error {
	if slices.Contains(want, got) {
		return nil
	}
	return fmt.Errorf("%s is %q; want one of %q", what, got, want)
}

// Spellings is every case of versions/*.json.
func Spellings() ([]Spelling, error) {
	var out []Spelling
	err := load("versions", func(file string, cases []Spelling) error {
		if len(cases) == 0 {
			return fmt.Errorf("no cases")
		}
		for _, c := range cases {
			c.File = file
			if c.ID == "" || c.Version == "" {
				return fmt.Errorf("a case with no id or no version")
			}
			if err := oneOf(c.ID+": verdict", c.Verdict, "accept", "refuse"); err != nil {
				return err
			}
			if (c.Verdict == "refuse") != (c.Reason != "") || (c.Verdict == "refuse" && c.Path != "") {
				return fmt.Errorf("%s: a refusal carries a reason and no path, an acceptance a path and no reason", c.ID)
			}
			out = append(out, c)
		}
		return nil
	})
	return out, unique(err, len(out), func(i int) string { return out[i].ID })
}

// Trees is every case of trees/*.json, each with its file's layout.
func Trees() ([]TreeCase, error) {
	var out []TreeCase
	err := load("trees", func(file string, f treeFile) error {
		if len(f.Layout) == 0 || len(f.Cases) == 0 {
			return fmt.Errorf("no layout or no cases")
		}
		for _, c := range f.Cases {
			c.File, c.Layout = file, f.Layout
			if c.ID == "" || c.Version == "" || len(c.Dirs) == 0 {
				return fmt.Errorf("a case with no id, no version or no directories")
			}
			if c.Error == "" && (len(c.After) == 0 || len(c.HalfMoved) != 0) {
				return fmt.Errorf("%s: a pin that succeeds lists the files it moved, and none half moved", c.ID)
			}
			out = append(out, c)
		}
		return nil
	})
	return out, unique(err, len(out), func(i int) string { return out[i].ID })
}

// Releases is every case of releases/*.json.
func Releases() ([]ReleaseCase, error) {
	var out []ReleaseCase
	err := load("releases", func(file string, cases []ReleaseCase) error {
		if len(cases) == 0 {
			return fmt.Errorf("no cases")
		}
		for _, c := range cases {
			c.File = file
			if c.ID == "" {
				return fmt.Errorf("a case with no id")
			}
			if err := oneOf(c.ID+": gate", c.Gate, "release", "boundary", "tree"); err != nil {
				return err
			}
			if err := oneOf(c.ID+": verdict", c.Verdict, "accept", "module_mismatch", "boundary_covers", "boundary_stale", "no_module"); err != nil {
				return err
			}
			out = append(out, c)
		}
		return nil
	})
	return out, unique(err, len(out), func(i int) string { return out[i].ID })
}

func unique(err error, n int, id func(int) string) error {
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for i := range n {
		if seen[id(i)] {
			return fmt.Errorf("pincorpus: case id %s is used twice", id(i))
		}
		seen[id(i)] = true
	}
	return nil
}
