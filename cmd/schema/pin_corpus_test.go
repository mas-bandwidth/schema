package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/modulemove"
	"github.com/mas-bandwidth/schema/v2/internal/modulemove/pincorpus"
)

// The committed corpus (internal/modulemove/pincorpus, testdata/pin) holds
// the version spellings and adopter trees every review finding on pin turned
// on. The tests here run it through the command's own code; none builds a
// binary but the one that says so, and none reads a clock or a network.

// pinCorpusOwnModule is the /v2 module path the refusals name, assembled so
// the module-path move program leaves it alone.
const pinCorpusOwnModule = pinModuleBase + "/v" + "2"

// TestPinCorpusSpellings runs every spelling of the corpus through pin's
// decision, under the case's boundary: a refused one carries the case's
// reason and names the version, an accepted one takes the module path with
// the case's major suffix.
func TestPinCorpusSpellings(t *testing.T) {
	t.Parallel()
	cases, err := pincorpus.Spellings()
	if err != nil {
		t.Fatal(err)
	}
	ran := 0
	for _, c := range cases {
		path, err := pinTarget(c.Version, c.Boundary, pinCorpusOwnModule)
		ran++
		switch {
		case c.Verdict == "accept" && (err != nil || path != pinModuleBase+c.Path):
			t.Errorf("%s (%s): pinTarget(%s, boundary %q) = %q, %v; want the base path plus %q", c.ID, c.Finding, c.Version, c.Boundary, path, err, c.Path)
		case c.Verdict == "refuse" && err == nil:
			t.Errorf("%s (%s): pinTarget(%s, boundary %q) = %q; want a refusal", c.ID, c.Finding, c.Version, c.Boundary, path)
		case c.Verdict == "refuse" && (!strings.Contains(err.Error(), c.Version) || !strings.Contains(err.Error(), c.Reason)):
			t.Errorf("%s (%s): the refusal of %s does not name the version and say %q: %v", c.ID, c.Finding, c.Version, c.Reason, err)
		}
	}
	if ran != len(cases) || ran == 0 {
		t.Errorf("ran %d of %d spellings", ran, len(cases))
	}
}

// TestPinCorpusSpellingsThroughTheCommand is the same through the built
// command, as the reviewer ran it, for every case whose boundary is the one
// this build carries: a refusal exits 1 with the file byte for byte as it
// was and nothing beside it, an acceptance rewrites the directive under the
// case's module path.
func TestPinCorpusSpellingsThroughTheCommand(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	cases, err := pincorpus.Spellings()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	ran := 0
	for _, c := range cases {
		if c.Boundary != modulemove.FirstV3ModuleTag {
			continue
		}
		ran++
		p := writePinPkg(t, root, c.ID, pinPkg1)
		if c.Verdict == "accept" {
			run(t, bin, "pin", c.Version, p)
			want := strings.ReplaceAll(pinPkg1, pinDirective+"v2.5.0", pinDirectiveBase+c.Path+"/cmd/schema@"+c.Version)
			if got := mustRead(t, filepath.Join(p, "generate.go")); got != want {
				t.Errorf("%s (%s): pin %s wrote\n%q\nwant\n%q", c.ID, c.Finding, c.Version, got, want)
			}
			continue
		}
		msg, status := runFail(t, bin, "pin", c.Version, p)
		if status != 1 {
			t.Fatalf("%s: pin %s exit %d, want 1\n%s", c.ID, c.Version, status, msg)
		}
		if !strings.Contains(msg, c.Version) || !strings.Contains(msg, c.Reason) {
			t.Errorf("%s (%s): the refusal of %s does not name the version and say %q: %q", c.ID, c.Finding, c.Version, c.Reason, msg)
		}
		if got := mustRead(t, filepath.Join(p, "generate.go")); got != pinPkg1 {
			t.Errorf("%s (%s): the refusal of %s changed the file:\n%q", c.ID, c.Finding, c.Version, got)
		}
		if entries, err := os.ReadDir(p); err != nil || len(entries) != 1 {
			t.Errorf("%s: the refusal left the package directory holding %v (err %v)", c.ID, entries, err)
		}
	}
	if ran == 0 {
		t.Errorf("no corpus spelling has this build's boundary %q", modulemove.FirstV3ModuleTag)
	}
}

// pinCorpusLine is the corpus's stand-in for a pin directive.
var pinCorpusLine = regexp.MustCompile(`@PIN (\S+) (\S+)@`)

// pinCorpusContent is a corpus entry's lines as file content.
func pinCorpusContent(lines []string) string {
	text := strings.Join(lines, "\n") + "\n"
	return pinCorpusLine.ReplaceAllStringFunc(text, func(m string) string {
		parts := pinCorpusLine.FindStringSubmatch(m)
		suffix := parts[1]
		if suffix == "-" {
			suffix = ""
		}
		return pinDirectiveHead + pinModuleBase + suffix + pinCommandTail + parts[2]
	})
}

// pinOld is the time every file of a corpus tree was last written, long
// before any test runs, so a file that was rewritten shows.
var pinOld = time.Date(2020, 2, 3, 4, 5, 6, 0, time.UTC)

var errPinCorpus = errors.New("permission denied (injected by the corpus case)")

// TestPinCorpusTrees builds every adopter tree of the corpus in a temporary
// directory (regular files, symbolic links, hard links), runs the case's pin
// through the command's own code with the case's file operations made to
// fail, and holds the tree afterwards to the case: the files it lists hold
// the new content, and EVERY other entry is the entry it was (the same file,
// the same bytes, mode and time last written, a link still a link to the
// same place), with nothing staged or kept left beside them.
func TestPinCorpusTrees(t *testing.T) {
	t.Parallel()
	cases, err := pincorpus.Trees()
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("the corpus holds no tree case")
	}
	for _, c := range cases {
		t.Run(c.ID, func(t *testing.T) {
			t.Parallel()
			runPinCorpusTree(t, c)
		})
	}
}

func runPinCorpusTree(t *testing.T, c pincorpus.TreeCase) {
	// resolved, so a path compares equal to the one pin follows it to where
	// the temporary directory itself sits behind a symbolic link
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	at := func(rel string) string { return filepath.Join(root, filepath.FromSlash(rel)) }

	// build the layout
	before := map[string]os.FileInfo{}
	orig := map[string]string{}
	for _, e := range c.Layout {
		if err := os.MkdirAll(filepath.Dir(at(e.Path)), 0o755); err != nil {
			t.Fatal(err)
		}
		switch {
		case e.Symlink != "":
			err = os.Symlink(e.Symlink, at(e.Path))
		case e.Hardlink != "":
			err = os.Link(at(e.Hardlink), at(e.Path))
		default:
			if err = os.WriteFile(at(e.Path), []byte(pinCorpusContent(e.Lines)), 0o644); err == nil {
				err = os.Chtimes(at(e.Path), pinOld, pinOld)
			}
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range c.Layout {
		if before[e.Path], err = os.Lstat(at(e.Path)); err != nil {
			t.Fatal(err)
		}
		if e.Symlink == "" {
			orig[e.Path] = mustRead(t, at(e.Path))
		}
	}

	// the case's failures
	renames := make([]int, len(c.Fail))
	f := pinFiles{
		createTemp: func(dir, pattern string) (*os.File, error) {
			for _, rule := range c.Fail {
				if rule.StageIn != "" && dir == at(rule.StageIn) {
					return nil, errPinCorpus
				}
			}
			return os.CreateTemp(dir, pattern)
		},
		rename: func(oldpath, newpath string) error {
			for i, rule := range c.Fail {
				if rule.Onto == "" || newpath != at(rule.Onto) {
					continue
				}
				renames[i]++
				if renames[i] >= rule.From && (rule.Until == 0 || renames[i] <= rule.Until) {
					return errPinCorpus
				}
			}
			return os.Rename(oldpath, newpath)
		},
	}

	dirs := make([]string, len(c.Dirs))
	for i, d := range c.Dirs {
		dirs[i] = at(d)
	}
	changed, err := runPin(f, c.Version, dirs, false, c.Boundary, pinCorpusOwnModule)
	switch {
	case c.Error == "" && err != nil:
		t.Fatalf("pin %s = %v; want it to succeed", c.Version, err)
	case c.Error == "" && len(changed) == 0:
		t.Errorf("pin %s reports no file moved", c.Version)
	case c.Error != "" && err == nil:
		t.Fatalf("pin %s succeeded (%v); want a failure saying %q", c.Version, changed, c.Error)
	case c.Error != "" && !strings.Contains(err.Error(), c.Error):
		t.Errorf("pin %s = %v; want it to say %q", c.Version, err, c.Error)
	}
	if err != nil && len(c.HalfMoved) == 0 && strings.Contains(err.Error(), "HALF MOVED") {
		t.Errorf("pin %s is half moved, and the case expects the tree as it was: %v", c.Version, err)
	}
	if len(c.HalfMoved) != 0 && strings.Contains(err.Error(), "nothing was changed") {
		t.Errorf("a half-moved pin claims nothing was changed: %v", err)
	}

	// the tree afterwards
	after := map[string]pincorpus.Entry{}
	for _, e := range c.After {
		if _, ok := before[e.Path]; !ok {
			t.Fatalf("the case expects new content at %s, which its layout does not hold", e.Path)
		}
		after[e.Path] = e
	}
	layout := map[string]pincorpus.Entry{}
	for _, e := range c.Layout {
		layout[e.Path] = e
		if slices.Contains(c.HalfMoved, e.Path) {
			if !strings.Contains(err.Error(), at(e.Path)) {
				t.Errorf("the error does not name %s, which could not be put back: %v", e.Path, err)
			}
			continue
		}
		if want, ok := after[e.Path]; ok {
			if got := mustRead(t, at(e.Path)); got != pinCorpusContent(want.Lines) {
				t.Errorf("%s holds\n%q\nwant\n%q", e.Path, got, pinCorpusContent(want.Lines))
			}
			continue
		}
		info, lerr := os.Lstat(at(e.Path))
		if lerr != nil {
			t.Errorf("%s is gone: %v", e.Path, lerr)
			continue
		}
		if e.Symlink != "" {
			if info.Mode()&os.ModeSymlink == 0 {
				t.Errorf("%s is no longer a symbolic link (mode %v)", e.Path, info.Mode())
			} else if got, _ := os.Readlink(at(e.Path)); got != e.Symlink {
				t.Errorf("%s links to %q; want %q", e.Path, got, e.Symlink)
			}
			continue
		}
		if !os.SameFile(before[e.Path], info) {
			t.Errorf("%s is a different file from the one it was (a copy of its bytes is not the entry)", e.Path)
		}
		if !info.ModTime().Equal(pinOld) {
			t.Errorf("%s was last written %v; want %v, the time it had", e.Path, info.ModTime(), pinOld)
		}
		if info.Mode() != before[e.Path].Mode() {
			t.Errorf("%s has mode %v; want %v", e.Path, info.Mode(), before[e.Path].Mode())
		}
		if got := mustRead(t, at(e.Path)); got != orig[e.Path] {
			t.Errorf("%s changed:\n%q", e.Path, got)
		}
		if _, moved := after[e.Hardlink]; e.Hardlink != "" && !moved && !slices.Contains(c.HalfMoved, e.Hardlink) {
			other, oerr := os.Stat(at(e.Hardlink))
			if oerr != nil || !os.SameFile(info, other) {
				t.Errorf("%s no longer shares a file with its hard link %s (err %v)", e.Path, e.Hardlink, oerr)
			}
		}
	}

	// nothing beside the layout, but the kept original of a half-moved file
	kept := 0
	werr := filepath.WalkDir(root, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil || d.IsDir() {
			return werr
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if _, ok := layout[rel]; ok {
			return nil
		}
		home := filepath.ToSlash(filepath.Join(filepath.Dir(rel), "generate.go"))
		if !slices.Contains(c.HalfMoved, home) {
			t.Errorf("%s is left in the tree: nothing staged or kept may stay", rel)
			return nil
		}
		kept++
		if !strings.Contains(err.Error(), path) {
			t.Errorf("the error does not name %s, where the original of %s is kept: %v", rel, home, err)
		}
		if got := mustRead(t, path); got != orig[home] {
			t.Errorf("the kept original %s holds %q", rel, got)
		}
		return nil
	})
	if werr != nil {
		t.Fatal(werr)
	}
	if kept != len(c.HalfMoved) {
		t.Errorf("%d originals are kept beside their files; want %d, one for each half-moved file", kept, len(c.HalfMoved))
	}
}
