package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/provesran"
)

// echoIntoGrep is a condition decided by a pipeline whose left side can die of
// SIGPIPE: `echo "$var" | grep -q ...` or `echo "$var" | grep ... >/dev/null`.
// On a value larger than the pipe buffer grep stops reading at its first match
// (-q always; GNU grep with stdout on /dev/null too), echo is killed writing
// the rest, `set -o pipefail` fails the pipeline, and the `if` or `||` takes
// the branch for NO MATCH. PR 4445's 3011-file diff touched go.mod, Makefile,
// compiler/ and ir/ and ci-fast.yml plan said `broad=no` (run 44914934707).
// A here-string, `grep -q ... <<<"$var"`, has no writer to kill.
var echoIntoGrep = regexp.MustCompile(`echo\s+(?:-[neE]+\s+)?"?\$\{?[A-Za-z_][A-Za-z0-9_]*\}?"?\s*\|\s*grep((?:\s+-[-A-Za-z]+)*)(.*)`)

// grepQuiet is a quiet flag among grep's leading options; grepToNull is
// output thrown away later on the line.
var (
	grepQuiet  = regexp.MustCompile(`(?:^|\s)(?:-[A-Za-z]*q[A-Za-z]*|--quiet|--silent)(?:\s|$)`)
	grepToNull = regexp.MustCompile(`>\s*/dev/null`)
)

// echoIntoGrepErrors names every line of the given files that decides on echo
// into a quiet grep. Comment lines are prose and are skipped.
func echoIntoGrepErrors(files map[string]string) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var errs []string
	for _, name := range names {
		for i, line := range strings.Split(files[name], "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			for _, m := range echoIntoGrep.FindAllStringSubmatch(line, -1) {
				if grepQuiet.MatchString(m[1]) || grepToNull.MatchString(m[2]) {
					errs = append(errs, fmt.Sprintf("%s:%d decides on echo into a quiet grep, which a value larger than the pipe buffer turns false under pipefail; use grep -q ... <<<\"$var\": %s", name, i+1, strings.TrimSpace(line)))
				}
			}
		}
	}
	return errs
}

// workflowAndActionFiles reads every workflow and composite action, keyed by
// its path under .github.
func workflowAndActionFiles(t *testing.T) map[string]string {
	t.Helper()
	root := repoRoot(t)
	out := map[string]string{}
	for name, text := range workflows(t, root) {
		out["workflows/"+name] = text
	}
	actions, err := provesran.Glob(filepath.Join(root, ".github", "actions", "*", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range actions {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(filepath.Join(root, ".github"), path)
		out[filepath.ToSlash(rel)] = string(data)
	}
	return out
}

// TestNoWorkflowDecidesOnEchoIntoGrep holds every workflow and action to the
// here-string form for a grep that decides a condition.
func TestNoWorkflowDecidesOnEchoIntoGrep(t *testing.T) {
	t.Parallel()
	files := workflowAndActionFiles(t)
	if errs := echoIntoGrepErrors(files); len(errs) > 0 {
		for _, e := range errs {
			t.Error(e)
		}
		t.FailNow()
	}
	full := files["workflows/ci-full.yml"]
	for _, tc := range []struct {
		name, file, old, new string
	}{
		{"ci-full pr-select C# rule back to echo | grep -E >/dev/null", "workflows/ci-full.yml",
			`|| grep -qE '^(internal/codegen/cstable/|internal/codegen/csharp/|generated/cs/|test/cs-tables/|make/cs\.mk$|make/cs-[^/]*\.mk$)' <<<"$files"; then`,
			`|| echo "$files" | grep -E '^(internal/codegen/cstable/|internal/codegen/csharp/|generated/cs/|test/cs-tables/|make/cs\.mk$|make/cs-[^/]*\.mk$)' >/dev/null; then`},
		{"ci-full render smoke back to echo | grep -q", "workflows/ci-full.yml",
			`grep -q "language        %" <<<"$out" ||`,
			`echo "$out" | grep -q "language        %" ||`},
	} {
		mutated := strings.Replace(full, tc.old, tc.new, 1)
		if mutated == full {
			t.Fatalf("negative control %q did not mutate %s", tc.name, tc.file)
		}
		if errs := echoIntoGrepErrors(map[string]string{tc.file: mutated}); len(errs) == 0 {
			t.Errorf("NEGATIVE CONTROL FAILED: %q left echoIntoGrepErrors green", tc.name)
		}
	}
	// a grep that reads the whole stream (no -q, output kept) decides nothing
	// by stopping early, so it is not this defect
	if errs := echoIntoGrepErrors(map[string]string{"x.yml": `rows=$(echo "$out" | grep -cE "^  [a-z]+ ")`}); len(errs) != 0 {
		t.Errorf("a counting grep was flagged: %v", errs)
	}
}
