package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/nightlyrows"
	"github.com/mas-bandwidth/schema/v2/internal/provesran"
)

// makeTargetWord is a make command argument that names a target, not a
// flag, a variable or a piece of a shell expansion.
var makeTargetWord = regexp.MustCompile("^[a-z0-9][a-z0-9._-]*$")

// TestNightlyFastRowsNameDefinedTargets holds every make target a row of
// .github/nightly-fast-rows.txt runs to a rule in the tree, and every leg to
// one the nightly-fast job sets up: a row that names nothing would be a green
// row that ran nothing.
func TestNightlyFastRowsNameDefinedTargets(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	var makeText strings.Builder
	for _, glob := range []string{"Makefile", "make/*.mk", "make/checks/*.mk"} {
		paths, err := provesran.Glob(filepath.Join(root, filepath.FromSlash(glob)))
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range paths {
			raw, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			makeText.Write(raw)
			makeText.WriteByte('\n')
		}
	}
	mk := makeText.String()
	for _, reg := range []struct {
		path  string
		extra string
	}{{nightlyrows.RegistryPath, ""}} {
		f, err := os.Open(filepath.Join(root, filepath.FromSlash(reg.path)))
		if err != nil {
			t.Fatal(err)
		}
		rows, err := nightlyrows.Parse(f)
		_ = f.Close()
		if err != nil {
			t.Fatalf("%s: %v", reg.path, err)
		}
		legs := map[string]bool{"-": true}
		for l := range strings.FieldsSeq("c cpp cs unity rust js java dart elixir go lua luau lobster moo python ruby squirrel goose " + reg.extra) {
			legs[l] = true
		}
		for _, r := range rows {
			if !legs[r.Leg] {
				t.Errorf("%s:%d: row %q names leg %q, which its job does not set up", reg.path, r.Line, r.Name, r.Leg)
			}
			fields := strings.Fields(r.Run)
			if len(fields) < 2 || fields[0] != "make" {
				t.Errorf("%s:%d: row %q runs %q; a row runs one make command", reg.path, r.Line, r.Name, r.Run)
				continue
			}
			targets := 0
			for _, a := range fields[1:] {
				if !makeTargetWord.MatchString(a) {
					continue
				}
				targets++
				if !regexp.MustCompile("(?m)^" + regexp.QuoteMeta(a) + ":").MatchString(mk) {
					t.Errorf("%s:%d: row %q runs make target %q, which no rule in Makefile or make/ defines", reg.path, r.Line, r.Name, a)
				}
			}
			if targets == 0 {
				t.Errorf("%s:%d: row %q names no make target", reg.path, r.Line, r.Name)
			}
		}
	}
}

// TestNightlyFastRunsTheRubyStrictFormOnARubyDiff is the footgun, replayed on
// the real registry: a diff to the Ruby emitter (the leg the plan names ruby)
// selects the strict gate fast form. Before 2026-11-22 no pull-request job
// selected any form of schema-ruby-generated-strict-gate.
func TestNightlyFastRunsTheRubyStrictFormOnARubyDiff(t *testing.T) {
	t.Parallel()
	f, err := os.Open(filepath.Join(repoRoot(t), filepath.FromSlash(nightlyrows.RegistryPath)))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := nightlyrows.Parse(f)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range nightlyrows.Select(rows, []string{"internal/codegen/ruby/emit.go"}, []string{"ruby"}, false) {
		got = append(got, r.Name)
	}
	for _, want := range []string{"strict-ruby", "hostile-ruby"} {
		if !slices.Contains(got, want) {
			t.Errorf("a Ruby emitter diff selects %v, without %s", got, want)
		}
	}
	if fast := nightlyrows.Select(rows, []string{"docs/FAQ.md"}, nil, false); len(fast) != 0 {
		t.Errorf("a docs-only diff selects %d nightly-fast rows, want none", len(fast))
	}
}
