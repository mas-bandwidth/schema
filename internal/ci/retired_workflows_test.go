package ci

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// retiredRow is one registry row that runs what a retired workflow's job
// ran: its key, its tier (unit, functional, nightly) and the make variables
// the job set on make's command line. seed names the make variable the
// driver sets to the run's seed, for a fuzz campaign whose job seeded it
// from the run (github.run_id); "" for a row that takes no seed.
type retiredRow struct {
	key  string
	tier string
	vars map[string]string
	seed string
}

// retiredWorkflows is every workflow the test registry's L6b layer retired,
// each with the rows that run what its jobs ran, one row per make target or
// Go test a job step ran (a step that only installs a pinned toolchain,
// make dist-<x>, is the runner's and not a test). A job that ran on the
// schedule and on dispatch is a nightly row; the per-pull-request forms of
// the lint gates, which ci-fast.yml's lang-lint ran, are functional rows of
// the pr group.
var retiredWorkflows = map[string][]retiredRow{
	"schema-dart-generated-strict-analyze-gate.yml": {
		{key: "lint.make-schema-dart-generated-strict-analyze-gate.schema-dart-generated-strict-analyze-gate", tier: "functional", vars: map[string]string{"DART": "dart"}},
		{key: "plant.schema-dart-strict-analyze.schema-dart-generated-strict-analyze-gate-negative-control", tier: "functional", vars: map[string]string{"DART": "dart"}},
		{key: "lint.make-schema-dart-generated-strict-analyze-gate.schema-dart-generated-strict-analyze-gate-all", tier: "functional", vars: map[string]string{"DART": "dart"}},
	},
	"schema-go-generated-golangci-lint-gate.yml": {
		{key: "lint.make-schema-go-generated-golangci-lint-gate.schema-go-generated-golangci-lint-gate", tier: "functional"},
		{key: "lint.make-schema-go-generated-golangci-lint-gate.schema-go-generated-golangci-lint-gate-nightly", tier: "nightly"},
	},
	"schema-lobster-generated-strict-gate.yml": {
		{key: "lint.make-schema-lobster-generated-strict-gate.tables-lobster-clean", tier: "functional"},
		{key: "plant.lobster.schema-lobster-generated-strict-gate-negative-control", tier: "functional"},
	},
	"schema-luau-generated-analyze-strict-gate.yml": {
		{key: "plant.excluded.schema-luau-generated-analyze-strict-gate-negative-control", tier: "functional"},
		{key: "lint.make-schema-luau-generated-analyze-strict-gate.schema-luau-generated-analyze-strict-gate", tier: "functional"},
	},
	"schema-moo-generated-analyze-strict-gate.yml": {
		{key: "plant.moo.schema-moo-generated-analyze-strict-gate-negative-control", tier: "functional"},
		{key: "plant.moo.schema-moo-generated-analyze-strict-gate-pin-negative-control", tier: "nightly"},
		{key: "lint.make-schema-moo-generated-analyze-strict-gate.schema-moo-generated-analyze-strict-gate", tier: "functional"},
	},
	"schema-lua-generated-luacheck-gate.yml": {
		{key: "lint.make-schema-lua-generated-luacheck-gate.tables-lua-luacheck", tier: "functional"},
		{key: "plant.excluded.tables-lua-luacheck-negative-control", tier: "functional"},
		{key: "lint.make-schema-lua-generated-luacheck-gate.tables-lua-format", tier: "functional"},
		{key: "plant.lua-format.tables-lua-format-negative-control", tier: "functional"},
	},
	"schema-lua-nightly-fuzz.yml": {
		{key: "tooling.ci.lua-nightly-fuzz-lane", tier: "unit"},
		{key: "plant.lua-fuzz.schema-lua-fuzz-negative-control", tier: "nightly"},
		{key: "fuzz.make-schema-lua-nightly-fuzz.schema-lua-fuzz-replay", tier: "functional"},
		{key: "fuzz.make-schema-lua-nightly-fuzz.schema-lua-nightly-fuzz", tier: "nightly", vars: map[string]string{"LUA_FUZZ_SECONDS": "600"}, seed: "LUA_FUZZ_SEED"},
	},
	"schema-luau-nightly-fuzz.yml": {
		{key: "fuzz.make-schema-luau-nightly-fuzz.luau-nightly-fuzz-replay", tier: "functional"},
		{key: "fuzz.make-schema-luau-nightly-fuzz.luau-nightly-fuzz", tier: "nightly", vars: map[string]string{"LUAU_FUZZ_TIME": "600"}, seed: "LUAU_FUZZ_SEED"},
	},
	"schema-squirrel-nightly-fuzz.yml": {
		{key: "tooling.ci.squirrel-nightly-fuzz", tier: "unit"},
		{key: "fuzz.make-schema-squirrel-nightly-fuzz.schema-squirrel-nightly-fuzz", tier: "nightly", seed: "SQUIRREL_FUZZ_SEED"},
		{key: "plant.squirrel-tools-2.schema-squirrel-nightly-fuzz-negative-control", tier: "nightly"},
	},
	"schema-lua-resource-bounds.yml": {
		{key: "plant.lua-a.schema-lua-resource-bounds-negative-control", tier: "functional"},
		{key: "resource-bounds.make-schema-lua-resource-bounds.schema-lua-resource-bounds", tier: "functional"},
	},
	"schema-luau-resource-bounds.yml": {
		{key: "plant.excluded.schema-luau-resource-bounds-negative-control", tier: "functional"},
		{key: "resource-bounds.make-schema-luau-resource-bounds.schema-luau-resource-bounds", tier: "functional"},
	},
	"schema-python-resource-bounds.yml": {
		{key: "plant.make-python-hostile.schema-python-resource-bounds-negative-control", tier: "nightly"},
		{key: "resource-bounds.make-python-hostile.schema-python-resource-bounds", tier: "functional"},
	},
	"schema-ruby-resource-bounds.yml": {
		{key: "resource-bounds.make-schema-ruby-runs-hostile-corpus.schema-ruby-resource-bounds", tier: "functional"},
		{key: "plant.make-schema-ruby-runs-hostile-corpus.schema-ruby-resource-bounds-negative-control", tier: "nightly"},
	},
	"schema-squirrel-resource-bounds.yml": {
		{key: "resource-bounds.make-schema-squirrel-resource-bounds.schema-squirrel-resource-bounds-fast", tier: "functional"},
		{key: "resource-bounds.make-schema-squirrel-resource-bounds.schema-squirrel-resource-bounds", tier: "nightly"},
		{key: "plant.squirrel-tools-2.schema-squirrel-resource-bounds-negative-control", tier: "nightly"},
	},
	"schema-lua-version-differential.yml": {
		{key: "tooling.ci.lua-version-differential-make-targets", tier: "unit"},
		{key: "tooling.ci.lua-version-differential-negative-control-creates-build-dir", tier: "unit"},
		{key: "differential.make-schema-lua-version-differential.schema-lua-version-differential", tier: "nightly"},
		{key: "plant.excluded.schema-lua-version-differential-negative-control", tier: "nightly"},
	},
	"schema-squirrel-all-port-differential.yml": {
		{key: "differential.make-schema-squirrel-all-port-differential.schema-squirrel-all-port-differential-fast", tier: "functional"},
		{key: "differential.make-schema-squirrel-all-port-differential.schema-squirrel-all-port-differential", tier: "nightly"},
	},
}

// retiredWorkflowProblems is every way the repository disagrees with the
// retired workflows' table: a retired workflow file that is back (two
// harnesses running one gate), and a row that is missing, of another tier,
// run by an adapter that is not make, plant or gotest, or that no longer
// sets the variables or the seed its job set.
func retiredWorkflowProblems(root string, reg *testreg.Registry) []string {
	var out []string
	names := make([]string, 0, len(retiredWorkflows))
	for wf := range retiredWorkflows {
		names = append(names, wf)
	}
	sort.Strings(names)
	for _, wf := range names {
		if _, err := os.Stat(filepath.Join(root, ".github", "workflows", wf)); err == nil {
			out = append(out, fmt.Sprintf("%s is back: its jobs are registry rows the schema-test jobs run, and a workflow beside them runs the gate twice", wf))
		}
		for _, want := range retiredWorkflows[wf] {
			t, ok := reg.Test(want.key)
			if !ok {
				out = append(out, fmt.Sprintf("%s: no registry row %s runs what its job ran", wf, want.key))
				continue
			}
			if t.Tier != want.tier {
				out = append(out, fmt.Sprintf("%s: row %s is tier %s, want %s", wf, want.key, t.Tier, want.tier))
			}
			switch t.Adapter {
			case "make", "plant", "gotest":
			default:
				out = append(out, fmt.Sprintf("%s: row %s runs on adapter %q, not make, plant or gotest", wf, want.key, t.Adapter))
			}
			var in struct {
				Vars map[string]string `json:"vars"`
				Seed string            `json:"seed"`
			}
			if err := json.Unmarshal(t.Inputs, &in); err != nil {
				out = append(out, fmt.Sprintf("%s: row %s inputs: %v", wf, want.key, err))
				continue
			}
			for k, v := range want.vars {
				if in.Vars[k] != v {
					out = append(out, fmt.Sprintf("%s: row %s sets %s=%q, want %s=%s as its job did", wf, want.key, k, in.Vars[k], k, v))
				}
			}
			if in.Seed != want.seed {
				out = append(out, fmt.Sprintf("%s: row %s takes seed %q, want %q", wf, want.key, in.Seed, want.seed))
			}
		}
	}
	return out
}

// TestRetiredWorkflowsAreRegistryRows: each workflow L6b retired is gone,
// and what each of its jobs ran is a registry row of the tier, variables
// and seed the job gave it.
func TestRetiredWorkflowsAreRegistryRows(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	reg, err := testreg.Load(filepath.Join(root, testreg.DefaultPath))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range retiredWorkflowProblems(root, reg) {
		t.Error(p)
	}
}

// TestRetiredWorkflowProblemsAreSeen plants each fault in a copy of the
// registry and a scratch root: a workflow back, a row gone, a row moved to
// another tier, a dropped variable and a dropped seed are each named.
func TestRetiredWorkflowProblemsAreSeen(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, testreg.DefaultPath))
	if err != nil {
		t.Fatal(err)
	}
	load := func(t *testing.T, edit func(string) string) *testreg.Registry {
		t.Helper()
		reg, err := testreg.Parse("planted", strings.NewReader(edit(string(data))))
		if err != nil {
			t.Fatal(err)
		}
		return reg
	}
	scratch := t.TempDir()
	if err := os.MkdirAll(filepath.Join(scratch, ".github", "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	clean := load(t, func(s string) string { return s })
	if got := retiredWorkflowProblems(scratch, clean); len(got) != 0 {
		t.Fatalf("the committed registry in an empty root: %v", got)
	}
	if err := os.WriteFile(filepath.Join(scratch, ".github", "workflows", "schema-lua-resource-bounds.yml"), []byte("name: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	back := retiredWorkflowProblems(scratch, clean)
	if len(back) != 1 || !strings.Contains(back[0], "schema-lua-resource-bounds.yml is back") {
		t.Errorf("a retired workflow restored: %v", back)
	}
	empty := t.TempDir()
	for name, c := range map[string]struct{ old, new, want string }{
		"row gone": {`"key":"resource-bounds.make-schema-luau-resource-bounds.schema-luau-resource-bounds"`,
			`"key":"resource-bounds.make-schema-luau-resource-bounds.planted-away"`,
			"no registry row resource-bounds.make-schema-luau-resource-bounds.schema-luau-resource-bounds"},
		"tier moved": {`"key":"lint.make-schema-lua-generated-luacheck-gate.tables-lua-luacheck","type":"lint","family":"make-schema-lua-generated-luacheck-gate","case":"tables-lua-luacheck","tier":"functional"`,
			`"key":"lint.make-schema-lua-generated-luacheck-gate.tables-lua-luacheck","type":"lint","family":"make-schema-lua-generated-luacheck-gate","case":"tables-lua-luacheck","tier":"nightly"`,
			"tables-lua-luacheck is tier nightly, want functional"},
		"variable dropped": {`"LUA_FUZZ_SECONDS":"600"`, `"LUA_FUZZ_SECONDS":"60"`, `sets LUA_FUZZ_SECONDS="60"`},
		"seed dropped":     {`"seed":"LUAU_FUZZ_SEED"`, `"seed":""`, `takes seed "", want "LUAU_FUZZ_SEED"`},
	} {
		if strings.Count(string(data), c.old) != 1 {
			t.Errorf("%s: the plant's anchor %q is not in the registry exactly once", name, c.old)
			continue
		}
		got := retiredWorkflowProblems(empty, load(t, func(s string) string { return strings.Replace(s, c.old, c.new, 1) }))
		if !strings.Contains(strings.Join(got, "\n"), c.want) {
			t.Errorf("%s: problems %v name no %q", name, got, c.want)
		}
	}
}
