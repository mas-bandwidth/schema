package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// W-ROSTER (docs/SPEC-CONFORMANCE.md §2): a change to the wire-fuzz corpus,
// the harness or the absent-root ledger runs the per-PR fuzz row of EVERY leg, so a
// root added to the corpus without its drivers goes red on the PR that adds it
// and not in the nightly. The shared_broad path rule is that selection.
func TestVerifyLegsCorpusRosterIsShared(t *testing.T) {
	re := sharedBroadRule(t)
	for _, path := range []string{
		"testdata/conformance/tables/MANIFEST.txt",
		"test/conformance/harness/wirefuzz.go",
		"test/conformance/wire-absent/c-plain.tsv",
		"test/tables/surfaces/Surfaces.schema",
		"testdata/wire/tables/fuzz-vectors/INDEX.txt",
		"testdata/wire/tables/fuzz-vectors/stream_count_past_body.bin",
		"testdata/conformance/feature-witnesses/full/Features.schema",
	} {
		if !re.MatchString(path) {
			t.Errorf("a change to %s does not select every verify leg (shared_broad rule %q)", path, re.String())
		}
	}
}

// The selection is derived from what the wire fuzzer actually reads, not
// from a list kept beside it: every path the conformance manifest names on a
// line that feeds the roster (unit, instance, report, message, connection),
// the vector index and every vector it names. A path the shared_broad rule
// misses is a corpus input whose change would not run every leg.
func TestVerifyLegsCorpusRosterCoversEveryInput(t *testing.T) {
	re := sharedBroadRule(t)
	inputs := wireFuzzCorpusInputs(t, "../..")
	if len(inputs) < 100 {
		t.Fatalf("found %d wire-fuzz corpus inputs; the manifest or vector index was not read", len(inputs))
	}
	for _, path := range inputs {
		if !re.MatchString(path) {
			t.Errorf("a change to %s, a wire-fuzz corpus input, does not select every verify leg (shared_broad rule %q)", path, re.String())
		}
	}
}

// sharedBroadRule is the shared_broad grep of ci-fast.yml's plan step.
func sharedBroadRule(t *testing.T) *regexp.Regexp {
	t.Helper()
	data, err := os.ReadFile("../../.github/workflows/ci-fast.yml")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "shared_broad=no" && i+1 < len(lines) {
			m := regexp.MustCompile(`grep -qE \x27([^\x27]+)\x27`).FindStringSubmatch(lines[i+1])
			if m == nil {
				t.Fatalf("the line after shared_broad=no has no grep -qE rule: %q", lines[i+1])
			}
			re, err := regexp.Compile(m[1])
			if err != nil {
				t.Fatalf("shared_broad rule %q: %v", m[1], err)
			}
			return re
		}
	}
	t.Fatal("ci-fast.yml has no shared_broad=no plan step")
	return nil
}

// wireFuzzCorpusInputs lists the repository paths the wire fuzzer's roster
// is built from (test/conformance/harness: buildWireFuzzRoster and
// readWireVectors), relative to root: the manifest, the unit schemas, the
// seed wires, the vector index and its vectors. A directory a unit line
// names stands for every file under it.
func wireFuzzCorpusInputs(t *testing.T, root string) []string {
	t.Helper()
	const manifest = "testdata/conformance/tables/MANIFEST.txt"
	const index = "testdata/wire/tables/fuzz-vectors/INDEX.txt"
	inputs := []string{manifest, index}
	add := func(p string) {
		info, err := os.Stat(filepath.Join(root, p))
		if err != nil {
			t.Errorf("a wire-fuzz corpus input does not exist: %s", p)
			return
		}
		if !info.IsDir() {
			inputs = append(inputs, p)
			return
		}
		err = filepath.WalkDir(filepath.Join(root, p), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				rel, _ := filepath.Rel(root, path)
				inputs = append(inputs, filepath.ToSlash(rel))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	feeds := map[string]bool{"unit": true, "instance": true, "report": true, "message": true, "connection": true}
	for _, file := range []string{manifest, index} {
		data, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			f := strings.Fields(line)
			if len(f) == 0 || strings.HasPrefix(f[0], "#") {
				continue
			}
			if file == manifest && !feeds[f[0]] {
				continue
			}
			for _, w := range f[1:] {
				if strings.Contains(w, "/") {
					add(w)
				}
			}
		}
	}
	return inputs
}
