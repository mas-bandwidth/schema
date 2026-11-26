package ci

// THE DART ANALYZE TREE HOLDS WHAT THE READERS IMPORT. `make
// schema-dart-nightly-fuzz-analyze` copied each generated <Base>Wire.dart
// reader alone into a scratch tree and ran dart analyze over it; the readers
// import their enums from the unit's packet library (`import 'VarEvo1.dart'
// show Tier;`), so the tree did not resolve and the gate was red on every
// unit with an enum ("Target of URI doesn't exist: 'VarEvo1.dart'"). The tree
// is now make/dart-analyze-tree.sh: each reader and what it imports by a
// relative path, transitively, and nothing else. This test runs the script
// over a generated tree it writes to its TempDir: no Dart, no network.

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const dartAnalyzeTreeScript = "make/dart-analyze-tree.sh"

// writeDartGenTree writes files (path relative to dir: contents).
func writeDartGenTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for p, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// listDartTree is every file under dir, relative and slash-separated, sorted.
func listDartTree(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, err := filepath.Rel(dir, p)
			if err != nil {
				return err
			}
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

// runAnalyzeTree runs a copy of the script over gen, into a fresh tree, and
// returns the exit status, the output and the tree's files.
func runAnalyzeTree(t *testing.T, script, gen string) (int, string, []string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "dart-analyze-tree.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "analyze")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	// testinputs:none — runs a copy of the script over a tree in its own directory
	cmd := exec.Command("sh", path, gen, out)
	cmd.Dir = dir
	b, err := cmd.CombinedOutput()
	c := exitCode(err)
	if c < 0 {
		t.Fatalf("sh did not run: %v", err)
	}
	return c, string(b), listDartTree(t, out)
}

// analyzeTreeVerdict is what is wrong with a script's trees: "" when none.
func analyzeTreeVerdict(t *testing.T, script string) []string {
	t.Helper()
	var wrong []string
	// two units: an enum import on one line and one across lines, a packet
	// library importing Schema.dart, a dart: import, and libraries no reader
	// imports (one importing a file the corpus never wrote, as tables/scalars'
	// Fixed library imports Int128.dart)
	gen := t.TempDir()
	writeDartGenTree(t, gen, map[string]string{
		"u1/AWire.dart":   "import 'dart:convert';\nimport 'dart:typed_data';\n\nimport 'P.dart' show Tier;\n",
		"u1/P.dart":       "import 'Schema.dart';\n",
		"u1/Schema.dart":  "// runtime\n",
		"u1/Unused.dart":  "import 'Schema.dart';\n",
		"u1/AFixed.dart":  "import 'Int128.dart';\n",
		"u2/BWire.dart":   "import 'dart:convert';\nimport 'Q.dart'\n    show\n        Alpha,\n        Beta;\n",
		"u2/Q.dart":       "import 'package:meta/meta.dart';\n",
		"u2/QBlock.dart":  "import 'Q.dart';\n",
		"u2/Schema.dart":  "// runtime\n",
		"u2/notes/x.dart": "// not a unit file\n",
	})
	want := []string{"u1/AWire.dart", "u1/P.dart", "u1/Schema.dart", "u2/BWire.dart", "u2/Q.dart"}
	if c, out, got := runAnalyzeTree(t, script, gen); c != 0 {
		wrong = append(wrong, "a generated tree whose imports are all there exits "+strconv.Itoa(c)+": "+out)
	} else if !slices.Equal(got, want) {
		wrong = append(wrong, "the tree holds "+strings.Join(got, " ")+", want the readers and what they import: "+strings.Join(want, " "))
	}
	// a reader importing a library the corpus did not generate: red, named
	missing := t.TempDir()
	writeDartGenTree(t, missing, map[string]string{
		"u3/CWire.dart": "import 'P.dart' show Tier;\n",
		"u3/P.dart":     "import 'Gone.dart';\n",
	})
	if c, out, _ := runAnalyzeTree(t, script, missing); c == 0 || !strings.Contains(out, "Gone.dart") {
		wrong = append(wrong, "a reader importing a library the corpus did not generate: exit "+strconv.Itoa(c)+", want non-zero naming Gone.dart: "+out)
	}
	// no reader at all: red
	if c, _, _ := runAnalyzeTree(t, script, t.TempDir()); c == 0 {
		wrong = append(wrong, "a generated tree with no reader exits 0")
	}
	return wrong
}

func TestTheDartAnalyzeTreeHoldsWhatTheReadersImport(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	b, err := os.ReadFile(filepath.Join(root, dartAnalyzeTreeScript))
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range analyzeTreeVerdict(t, string(b)) {
		t.Errorf("%s: %s", dartAnalyzeTreeScript, w)
	}
	// the gate runs the script over the corpus before dart analyze
	mk, err := os.ReadFile(filepath.Join(root, "make", "schema-dart-nightly-fuzz.mk"))
	if err != nil {
		t.Fatal(err)
	}
	const call = "\t@sh make/dart-analyze-tree.sh $(DART_NIGHTLY_FUZZ_BUILD)/gen $(DART_NIGHTLY_FUZZ_BUILD)/analyze\n\t@$(DART) analyze $(DART_NIGHTLY_FUZZ_BUILD)/analyze\n"
	if !strings.Contains(string(mk), call) {
		t.Errorf("schema-dart-nightly-fuzz-analyze does not build its tree with %s before dart analyze", dartAnalyzeTreeScript)
	}
	// the plant: the tree of the readers alone, as the recipe built it
	readersOnly := `#!/bin/sh
set -eu
for f in "$1"/*/*Wire.dart; do
	unit=$(basename "$(dirname "$f")")
	mkdir -p "$2/$unit"
	cp "$f" "$2/$unit/"
done
`
	if len(analyzeTreeVerdict(t, readersOnly)) == 0 {
		t.Error("planted (the readers alone, without what they import): the test is not red")
	}
}
