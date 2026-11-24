package ci

// A *_test.py file that no CI target runs is a test that passes by not
// existing. nightly_readiness_test.py went red on main unseen for exactly that
// reason: nothing invoked it. This gate lists every *_test.py in the tree (the
// vendored third_party trees keep their own CI and naming) and requires each
// to be named by a make recipe whose target a workflow or a negative-control
// group names, or by a workflow run line directly.

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/provesran"
)

var yamlComment = regexp.MustCompile(`(^|\s)#.*$`)

// stripYAMLComment drops a trailing or whole-line YAML comment, so a command
// named only in a comment does not count as run.
func stripYAMLComment(line string) string { return yamlComment.ReplaceAllString(line, "") }

var recipeTarget = regexp.MustCompile(`^([^:=\s#][^:=]*):(?:[^=]|$)`)

// unrunPythonTests returns the repo-relative *_test.py files that nothing runs.
// makeFiles and workflowFiles map a name to its text; groupText is the text of
// make/negative-controls.d, which names targets a workflow reaches by matrix.
func unrunPythonTests(files []string, makeFiles, workflowFiles map[string]string, groupText string) []string {
	var wf strings.Builder
	for _, text := range workflowFiles {
		for line := range strings.SplitSeq(text, "\n") {
			wf.WriteString(stripYAMLComment(line))
			wf.WriteString("\n")
		}
	}
	reached := func(target string) bool {
		word := regexp.MustCompile(`(^|[^A-Za-z0-9_.-])` + regexp.QuoteMeta(target) + `($|[^A-Za-z0-9_.-])`)
		return word.MatchString(wf.String()) || word.MatchString(groupText)
	}
	var bad []string
	for _, file := range files {
		ok := false
		for line := range strings.SplitSeq(wf.String(), "\n") {
			if strings.Contains(line, file) {
				ok = true
			}
		}
		for _, text := range makeFiles {
			var current []string
			for line := range strings.SplitSeq(text, "\n") {
				if strings.HasPrefix(line, "\t") {
					if strings.Contains(line, file) && !strings.HasPrefix(strings.TrimSpace(line), "#") {
						for _, target := range current {
							if reached(target) {
								ok = true
							}
						}
					}
				} else if m := recipeTarget.FindStringSubmatch(line); m != nil {
					current = strings.Fields(m[1])
				} else if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "#") {
					current = nil
				}
			}
		}
		if !ok {
			bad = append(bad, file)
		}
	}
	sort.Strings(bad)
	return bad
}

func readAll(t *testing.T, pattern string) map[string]string {
	t.Helper()
	paths, err := provesran.Glob(pattern)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		out[p] = string(b)
	}
	return out
}

func TestEveryPythonTestFileIsRunByACITarget(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "build", "node_modules", "third_party":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), "_test.py") {
			rel, _ := filepath.Rel(root, path)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 2 {
		t.Fatalf("found %d *_test.py files; the walk is looking at the wrong tree", len(files))
	}
	makeFiles := readAll(t, filepath.Join(root, "Makefile"))
	maps.Copy(makeFiles, readAll(t, filepath.Join(root, "make", "*.mk")))
	workflowFiles := readAll(t, filepath.Join(root, ".github", "workflows", "*.yml"))
	var groups strings.Builder
	for _, v := range readAll(t, filepath.Join(root, "make", "negative-controls.d", "*.json")) {
		groups.WriteString(v)
	}
	for _, file := range unrunPythonTests(files, makeFiles, workflowFiles, groups.String()) {
		t.Errorf("%s is run by no CI target: name it in a make recipe whose target a workflow runs "+
			"(make python-tooling-tests), or in a workflow run line", file)
	}
}

// The reversed witness: the same check over a synthetic tree must flag a file
// no target runs, a file whose only target no workflow reaches, and a file
// named only in a comment, and must pass the wired cases.
func TestUnrunPythonTestWitness(t *testing.T) {
	t.Parallel()
	mk := map[string]string{"Makefile": "" +
		"wired:\n\tpython3 -I a/wired_test.py\n" +
		"orphan-target:\n\tpython3 -I a/orphan_test.py\n" +
		"commented:\n\t# python3 a/comment_test.py\n\ttrue\n" +
		"trailing-target:\n\tpython3 -I a/trailing_test.py\n"}
	wf := map[string]string{"ci.yml": "      - run: make wired\n      - run: python3 b/direct_test.py\n      # c/never_test.py\n" +
		"      - run: true # make trailing-target\n"}
	files := []string{"a/wired_test.py", "a/orphan_test.py", "a/comment_test.py", "b/direct_test.py", "c/never_test.py", "d/absent_test.py", "a/trailing_test.py"}
	got := unrunPythonTests(files, mk, wf, "")
	want := []string{"a/comment_test.py", "a/orphan_test.py", "a/trailing_test.py", "c/never_test.py", "d/absent_test.py"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("flagged %v, want %v", got, want)
	}
	if got := unrunPythonTests([]string{"a/orphan_test.py"}, mk, wf, `{"targets":["orphan-target"]}`); len(got) != 0 {
		t.Fatalf("a target named by a negative-control group must count, flagged %v", got)
	}
}
