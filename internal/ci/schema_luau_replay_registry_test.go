package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/provesran"
)

// luauReplayRegistryProblems holds a Luau replay corpus to its INPUTS list
// both ways, the rule make/schema-luau-nightly-fuzz.mk's generator enforces
// before every replay: each registered input has its .bin and its .bin.meta,
// each .bin and .bin.meta on disk is registered, and the list is not empty.
// A missing directory or a missing list is a problem, never an empty corpus.
func luauReplayRegistryProblems(dir string) []string {
	data, err := os.ReadFile(filepath.Join(dir, "INPUTS"))
	if err != nil {
		return []string{fmt.Sprintf("no INPUTS list: %v", err)}
	}
	registered := map[string]bool{}
	var problems []string
	// a last line with no newline is the line a shell `read` loop drops
	if len(data) > 0 && data[len(data)-1] != '\n' {
		problems = append(problems, "INPUTS does not end in a newline, so its last line would not be read")
	}
	for name := range strings.SplitSeq(strings.TrimRight(string(data), "\n"), "\n") {
		if name == "" {
			problems = append(problems, "INPUTS holds an empty line")
			continue
		}
		if registered[name] {
			problems = append(problems, "INPUTS registers "+name+" twice")
		}
		registered[name] = true
		for _, file := range []string{name, name + ".meta"} {
			if _, err := os.Stat(filepath.Join(dir, file)); err != nil {
				problems = append(problems, "registered input "+name+": "+file+" is missing")
			}
		}
	}
	if len(registered) == 0 {
		problems = append(problems, "INPUTS registers no input")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return append(problems, err.Error())
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".bin") && !strings.HasSuffix(name, ".bin.meta") {
			continue
		}
		if !registered[strings.TrimSuffix(name, ".meta")] {
			problems = append(problems, name+" is not registered in INPUTS")
		}
	}
	sort.Strings(problems)
	return problems
}

// TestLuauReplayCorpusIsRegistered: the saved corpus pairs with its list, the
// make generator carries the same refusals, and each broken copy is named.
// The first two controls are the reviewer's: with sidecars removed the replay
// reported 1 and 0 of 3 inputs and exited 0.
func TestLuauReplayCorpusIsRegistered(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	corpus := filepath.Join(root, "testdata", "luau-nightly-fuzz")
	for _, p := range luauReplayRegistryProblems(corpus) {
		t.Errorf("testdata/luau-nightly-fuzz: %s", p)
	}

	mk, err := os.ReadFile(filepath.Join(root, "make", "schema-luau-nightly-fuzz.mk"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`fail "the directory does not exist"`,
		`fail "no INPUTS file registers its saved inputs"`,
		`fail "registered input $$name is missing"`,
		`fail "registered input $$name has no $$name.meta"`,
		`fail "INPUTS registers no input"`,
		`is not registered in INPUTS"`,
		`luau-nightly-fuzz-replay: luau-nightly-fuzz-replay-controls `,
		`grep -q "^REPLAY-DONE|inputs=$$n|"`,
		`fail "INPUTS does not end in a newline, so its last line would not be read"`,
		`[ $$refused -eq 8 ]`,
		`the healthy fixture is accepted with 3 inputs`,
	} {
		if !strings.Contains(string(mk), want) {
			t.Errorf("make/schema-luau-nightly-fuzz.mk no longer holds %q: the replay set is not held to its list", want)
		}
	}

	copyCorpus := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		entries, err := os.ReadDir(corpus)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			data, err := os.ReadFile(filepath.Join(corpus, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, e.Name()), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	remove := func(t *testing.T, dir, pattern string, keep int) {
		t.Helper()
		matches, err := provesran.Glob(filepath.Join(dir, pattern))
		if err != nil || len(matches) <= keep {
			t.Fatalf("%s matches %d files (err %v), want more than %d", pattern, len(matches), err, keep)
		}
		for _, m := range matches[keep:] {
			if err := os.Remove(m); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, c := range []struct {
		name   string
		mutate func(t *testing.T, dir string)
		want   string
	}{
		{"one sidecar kept", func(t *testing.T, dir string) { remove(t, dir, "*.bin.meta", 1) }, ".bin.meta is missing"},
		{"no sidecars", func(t *testing.T, dir string) { remove(t, dir, "*.bin.meta", 0) }, ".bin.meta is missing"},
		{"a binary removed", func(t *testing.T, dir string) { remove(t, dir, "*.bin", 2) }, ".bin is missing"},
		{"every binary and sidecar removed", func(t *testing.T, dir string) {
			remove(t, dir, "*.bin", 0)
			remove(t, dir, "*.bin.meta", 0)
		}, "is missing"},
		{"an unregistered binary", func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, "zz-unregistered.bin"), []byte{1}, 0o644); err != nil {
				t.Fatal(err)
			}
		}, "zz-unregistered.bin is not registered in INPUTS"},
		{"an unregistered sidecar", func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, "zz-orphan.bin.meta"), []byte("kind=load\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, "zz-orphan.bin.meta is not registered in INPUTS"},
		{"an empty list", func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, "INPUTS"), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}, "INPUTS registers no input"},
		{"a list with no final newline", func(t *testing.T, dir string) {
			path := filepath.Join(dir, "INPUTS")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(strings.TrimRight(string(data), "\n")), 0o644); err != nil {
				t.Fatal(err)
			}
		}, "INPUTS does not end in a newline"},
		{"no list", func(t *testing.T, dir string) { remove(t, dir, "INPUTS", 0) }, "no INPUTS list"},
		{"no directory", func(t *testing.T, dir string) {
			if err := os.RemoveAll(dir); err != nil {
				t.Fatal(err)
			}
		}, "no INPUTS list"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := copyCorpus(t)
			if problems := luauReplayRegistryProblems(dir); len(problems) != 0 {
				t.Fatalf("the unbroken copy has problems: %q", problems)
			}
			c.mutate(t, dir)
			problems := strings.Join(luauReplayRegistryProblems(dir), "\n")
			if !strings.Contains(problems, c.want) {
				t.Fatalf("problems %q, want one holding %q", problems, c.want)
			}
		})
	}
}
