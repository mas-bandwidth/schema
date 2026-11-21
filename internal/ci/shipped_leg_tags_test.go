package ci

// TestNoShippedLegBehindABuildTag holds the ruling of 2026-11-21
// (docs/rulings.json every-target-default-build). No shipped language sits behind a
// build tag anywhere, the conformance harness included. v3.0.1 shipped with
// compiler/target_moo.go behind a build tag for its leg, so the default
// binary answered --lang moo with "not implemented" while the docs listed Moo
// as supported; this gate makes that state unrepresentable.
//
// It reads every tracked Go file, make file, workflow file and script, and
// fails on `schema_leg_` followed by the name of any of the eighteen shipped
// targets, as a whole name (the lua tag is not found inside a longer name
// such as the luaux in the control). A `schema new-leg` leg still being written may carry
// schema_leg_<its name>, which is not a shipped name. History files are
// exempt by path only (shippedLegTagHistory); there is no other exemption.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// shippedLegNames are the eighteen languages a release ships, as their
// compiler targets name them (compiler/target_moo_builtin_test.go,
// shippedTargets, holds the compiler to the same list).
var shippedLegNames = []string{
	"c", "cpp", "cs", "dart", "elixir", "go", "goose", "java", "js",
	"lobster", "lua", "luau", "moo", "python", "ruby", "rust", "squirrel", "unity",
}

// shippedLegTagHistory are the path prefixes whose files record what was run
// at a past commit; they are never rewritten, so they may name an old tag.
var shippedLegTagHistory = []string{
	"bench/results/",
	"docs/roadmap-evidence/",
	"docs/notes/",
	"notes/",
	"docs/measurements/",
	"docs/validation/",
	"docs/plans/",
}

// shippedLegTagRx matches schema_leg_<shipped name> as a whole name.
func shippedLegTagRx() *regexp.Regexp {
	return regexp.MustCompile(`schema_leg_(` + strings.Join(shippedLegNames, "|") + `)\b`)
}

// shippedLegTagScanned is whether path is a Go file, a make file, a workflow
// or action file, or a script (a script extension, or a first line that
// starts with #!).
func shippedLegTagScanned(path string, data []byte) bool {
	base := filepath.Base(path)
	switch {
	case strings.HasSuffix(base, ".go"),
		base == "Makefile", base == "GNUmakefile", strings.HasSuffix(base, ".mk"),
		strings.HasPrefix(path, ".github/"),
		strings.HasSuffix(base, ".sh"), strings.HasSuffix(base, ".bash"),
		strings.HasSuffix(base, ".pl"), strings.HasSuffix(base, ".py"),
		strings.HasSuffix(base, ".rb"), strings.HasSuffix(base, ".exs"),
		strings.HasSuffix(base, ".lisp"):
		return true
	}
	return strings.HasPrefix(string(data), "#!")
}

// shippedLegTagOffenders is every "<path>:<line>: <tag>" in files that
// tracked names, read from root.
func shippedLegTagOffenders(t *testing.T, root string, tracked []string) []string {
	t.Helper()
	rx := shippedLegTagRx()
	var out []string
	for _, path := range tracked {
		exempt := false
		for _, prefix := range shippedLegTagHistory {
			exempt = exempt || strings.HasPrefix(path, prefix)
		}
		if exempt {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			if os.IsNotExist(err) {
				continue // deleted in the working tree, not yet in the index
			}
			t.Fatal(err)
		}
		if !shippedLegTagScanned(path, data) {
			continue
		}
		for i, line := range strings.Split(string(data), "\n") {
			for _, m := range rx.FindAllString(line, -1) {
				out = append(out, path+":"+strconv.Itoa(i+1)+": "+m)
			}
		}
	}
	sort.Strings(out)
	return out
}

func TestNoShippedLegBehindABuildTag(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	tracked := gitLsFiles(t, root, ".")
	if len(tracked) == 0 {
		t.Fatal("git ls-files named no tracked file: this gate is looking at the wrong tree")
	}
	for _, path := range shippedLegTagOffenders(t, root, tracked) {
		t.Errorf("%s: a shipped language behind a build tag; every shipped target is built in, "+
			"in every build, the harness included (2026-11-21, docs/rulings.json every-target-default-build)", path)
	}
}

// TestShippedLegTagGateSeesWhatItRefuses is the gate's control: a planted tag
// for a shipped leg in each scanned kind of file is caught, a whole-name miss
// (luau is not lua), a made-up leg, a doc page and a history path are not.
func TestShippedLegTagGateSeesWhatItRefuses(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	files := map[string]string{
		"compiler/target_x.go":           "//go:build schema_leg_" + "moo\n\npackage compiler\n",
		"make/x.mk":                      "x:\n\tgo test -tags schema_leg_" + "python ./...\n",
		".github/workflows/x.yml":        "env:\n  HARNESS_TAGS: schema_leg_" + "goose\n",
		"test/x/driver":                  "#!/bin/sh\ngo build -tags schema_leg_" + "lua ./cmd/schema\n",
		"tools/x.py":                     "TAGS = {'ruby': 'schema_leg_" + "ruby'}\n",
		"compiler/target_zig.go":         "//go:build schema_leg_zig\n\npackage compiler\n",
		"compiler/carries_doc.go":        "// a schema_leg_<lang> leg still being built\npackage compiler\n",
		"compiler/target_luau_note.go":   "// schema_leg_luaux is not a shipped name\npackage compiler\n",
		"docs/PAGE.md":                   "behind the `schema_leg_" + "unity` tag\n",
		"bench/results/old.md":           "-tags schema_leg_" + "moo\n",
		"docs/roadmap-evidence/moo.sexp": "-tags schema_leg_" + "moo\n",
		"docs/plans/leg.md":              "schema_leg_" + "ruby\n",
		"notes/history.txt":              "schema_leg_" + "go\n",
		"docs/notes/history.go":          "// schema_leg_" + "go\n",
		"docs/measurements/run.sh":       "go build -tags schema_leg_" + "cs ./...\n",
		"docs/validation/run.sh":         "go build -tags schema_leg_" + "cpp ./...\n",
	}
	var tracked []string
	for path, body := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		tracked = append(tracked, path)
	}
	got := shippedLegTagOffenders(t, root, tracked)
	want := []string{
		".github/workflows/x.yml:2: schema_leg_" + "goose",
		"compiler/target_x.go:1: schema_leg_" + "moo",
		"make/x.mk:2: schema_leg_" + "python",
		"test/x/driver:2: schema_leg_" + "lua",
		"tools/x.py:1: schema_leg_" + "ruby",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("the gate found:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
