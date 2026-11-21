package ci

import (
	"io/fs"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// conformanceCompilerRuntimes are the compilers the Python and Ruby
// conformance trees are generated with, and the runtime directories each
// embeds and emits verbatim. SERIALIZE_PIN sits in each: a vendored
// serialize bump changes it and the files it pins, and either must rebuild
// the compiler and so build/conformance-<lang>, or a stale runtime can pass
// (review-owed/ci-tools/serialize-pin-input).
var conformanceCompilerRuntimes = map[string][]string{
	"build/schema-python": {"internal/codegen/python/runtime", "internal/codegen/pythontable/runtime"},
	"build/schema-ruby":   {"internal/codegen/ruby/runtime", "internal/codegen/rubytable/runtime"},
}

// makeDatabasePrereqs reads each target's prerequisites from make's printed
// database (`make -pq`), which merges every rule of a target.
func makeDatabasePrereqs(db string, targets []string) map[string][]string {
	out := map[string][]string{}
	for line := range strings.SplitSeq(db, "\n") {
		for _, t := range targets {
			if rest, ok := strings.CutPrefix(line, t+":"); ok && !strings.HasPrefix(rest, "=") {
				out[t] = append(out[t], strings.Fields(rest)...)
			}
		}
	}
	return out
}

// runtimeInputErrors names every runtime file a conformance compiler's rule
// does not take as a prerequisite.
func runtimeInputErrors(prereqs map[string][]string, files map[string][]string) []string {
	var errs []string
	var targets []string
	for t := range files {
		targets = append(targets, t)
	}
	slices.Sort(targets)
	for _, t := range targets {
		if len(prereqs[t]) == 0 {
			errs = append(errs, t+" has no prerequisites in make's database")
			continue
		}
		for _, f := range files[t] {
			if !slices.Contains(prereqs[t], f) {
				errs = append(errs, t+" does not rebuild when "+f+" changes: build/conformance-* would keep a stale runtime")
			}
		}
	}
	return errs
}

func TestConformanceCompilersRebuildOnTheirRuntime(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	files := map[string][]string{}
	var targets []string
	for target, dirs := range conformanceCompilerRuntimes {
		targets = append(targets, target)
		for _, d := range dirs {
			err := filepath.WalkDir(filepath.Join(root, d), func(p string, e fs.DirEntry, err error) error {
				if err != nil || e.IsDir() {
					return err
				}
				rel, err := filepath.Rel(root, p)
				files[target] = append(files[target], filepath.ToSlash(rel))
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		if !slices.Contains(files[target], dirs[0]+"/SERIALIZE_PIN") {
			t.Fatalf("%s has no SERIALIZE_PIN: this check would pass over the file it exists for", dirs[0])
		}
	}
	cmd := exec.Command("make", "-pq", "--no-print-directory", "build/schema-python", "build/schema-ruby")
	cmd.Dir = root
	out, _ := cmd.Output() // -q exits 1 when a target is out of date; the database is printed either way
	prereqs := makeDatabasePrereqs(string(out), targets)
	for _, e := range runtimeInputErrors(prereqs, files) {
		t.Error(e)
	}
	// the control: SERIALIZE_PIN dropped from each rule must be named
	for _, target := range targets {
		pin := conformanceCompilerRuntimes[target][0] + "/SERIALIZE_PIN"
		planted := map[string][]string{}
		for k, v := range prereqs {
			planted[k] = slices.DeleteFunc(slices.Clone(v), func(p string) bool { return k == target && p == pin })
		}
		if errs := runtimeInputErrors(planted, files); !strings.Contains(strings.Join(errs, "\n"), pin) {
			t.Errorf("NEGATIVE CONTROL FAILED: %s with no %s prerequisite left the check green", target, pin)
		}
	}
}
