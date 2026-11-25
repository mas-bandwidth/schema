package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSchemaLuauHostileCorpusAndWireFuzzWiring holds the Luau port's hostile
// corpus and wire-fuzz blades without the interpreter in the Go unit tier, as
// TestSchemaElixirRunsHostileCorpusWiring holds the Elixir port's: each gate
// is a make target the port's unit target runs, each control is registered on
// the pull-request tier of make/negative-controls.json, the wire fuzz fails on
// any root its leg cannot open, with no pinned list to hold one back (the
// no-skips ruling, docs/rulings.json), and the corpus workflow runs
// nightly and on dispatch only (the owner, 2026-11-15), fleet only, fork guarded.
func TestSchemaLuauHostileCorpusAndWireFuzzWiring(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	read := func(rel string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s is missing: %v", rel, err)
		}
		return string(b)
	}

	carries := func(rel, text string, wants ...string) {
		t.Helper()
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Errorf("%s does not carry %q", rel, want)
			}
		}
	}
	carries("make/schema-luau-runs-hostile-corpus.mk", read("make/schema-luau-runs-hostile-corpus.mk"),
		"test-luau: schema-luau-runs-hostile-corpus",
		"schema-luau-runs-hostile-corpus-negative-control:",
		"TestLuauRunsHostileCorpusPlantedPattern")
	carries("make/luau-fuzz.mk", read("make/luau-fuzz.mk"),
		"test-luau: tables-luau-wire-fuzz tables-luau-wire-fuzz-negative-control",
		"LUAUFUZZ_NOTE=",
		`if [ "$$n" -ne 0 ]; then`,
		"0 divergences",
		"sh test/conformance/luau/wire-fuzz-negative-control")
	carries("test/conformance/luau/wirefuzz.luau", read("test/conformance/luau/wirefuzz.luau"), "host.note(")

	// no pinned list of absent roots: it held roots back, and it is retired
	for _, retired := range []string{"wire-fuzz-absent", "LUAU_WIRE_FUZZ_ABSENT_COUNT", "LUAU_WIRE_FUZZ_ABSENT_PIN"} {
		if strings.Contains(read("make/luau-fuzz.mk"), retired) {
			t.Errorf("make/luau-fuzz.mk carries %q: a pinned list of absent roots is retired", retired)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "test", "conformance", "luau", "wire-fuzz-absent.txt")); err == nil {
		t.Error("test/conformance/luau/wire-fuzz-absent.txt is back: a pinned list of absent roots is retired")
	}

	controls := read("make/negative-controls.json")
	for _, want := range []struct {
		group   string
		targets []string
	}{
		{"luau-conformance", []string{"schema-luau-runs-hostile-corpus-negative-control"}},
		{"luau-fuzz", []string{"tables-luau-wire-fuzz-negative-control", "luau-nightly-fuzz-negative-control"}},
	} {
		// the group, or the slices it was split into (want.group-a, -b, ...):
		// each target sits in one of them, and that one runs per pull request
		var groups []string
		for rest := controls; ; {
			at := strings.Index(rest, `"name": "`+want.group)
			if at < 0 {
				break
			}
			group, tail, _ := strings.Cut(rest[at:], "}")
			name, _, _ := strings.Cut(strings.TrimPrefix(group, `"name": "`+want.group), `"`)
			if name == "" || (len(name) == 2 && name[0] == '-') {
				groups = append(groups, group)
			}
			rest = tail
		}
		if len(groups) == 0 {
			t.Fatalf("make/negative-controls.json has no %s group", want.group)
		}
		for _, target := range want.targets {
			found := false
			for _, group := range groups {
				if strings.Contains(group, `"`+target+`"`) {
					found = true
					carries("make/negative-controls.json (the "+want.group+" group holding "+target+")", group, `"when": "pull-request"`)
				}
			}
			if !found {
				t.Errorf("make/negative-controls.json's %s group does not carry %q", want.group, target)
			}
		}
	}

	workflow := read(".github/workflows/schema-luau-runs-hostile-corpus.yml")
	carries("schema-luau-runs-hostile-corpus.yml", workflow,
		"schedule:", "workflow_dispatch:",
		"if: github.event_name != 'pull_request' || github.event.pull_request.head.repo.full_name == github.repository",
		"make dist-luau",
		"make schema-luau-runs-hostile-corpus\n",
		"make schema-luau-runs-hostile-corpus-negative-control")
	// NIGHTLY AND DISPATCH ONLY (the owner, 2026-11-15: "I want everything that is
	// not light moved to nightly test now." "no exceptions."): no push and no
	// pull request starts the corpus workflow; those run only ci-fast.yml.
	for _, p := range nightlyOnlyTriggerProblems(workflow) {
		t.Errorf("schema-luau-runs-hostile-corpus.yml: %s", p)
	}
	if len(nightlyOnlyTriggerProblems(strings.Replace(workflow, "\non:\n", "\non:\n  push:\n", 1))) == 0 {
		t.Error("nightly-only control: a restored push trigger was not refused")
	}
	if strings.Contains(workflow, "runs-on: ubuntu-latest") {
		t.Error("the corpus workflow runs on a GitHub-hosted runner; every CI job is on the fleet")
	}
	for _, ev := range []ciEvent{
		{Name: "pull_request", Branch: "main", Repo: "mas-bandwidth/schema", HeadRepo: "fork/schema"},
		{Name: "pull_request", Branch: "dev", Repo: "mas-bandwidth/schema", HeadRepo: "fork/schema", Labels: []string{"full-ci"}},
	} {
		for _, job := range []string{"corpus", "negative-control"} {
			if ok, err := jobSelectsSelfHosted(workflow, job, ev); err != nil || ok {
				t.Errorf("the Luau %s job admitted a fork PR (%+v): ok=%v err=%v", job, ev, ok, err)
			}
		}
	}
	for _, ev := range []ciEvent{
		{Name: "pull_request", Branch: "main", HeadRef: "dev", Repo: "mas-bandwidth/schema", HeadRepo: "mas-bandwidth/schema"},
		{Name: "push", Branch: "main", Repo: "mas-bandwidth/schema"},
		{Name: "push", Branch: "dev", Repo: "mas-bandwidth/schema"},
	} {
		if ok, err := jobSelectsSelfHosted(workflow, "corpus", ev); err != nil || ok {
			t.Errorf("the Luau corpus job ran on %+v: ok=%v err=%v; the workflow is nightly and dispatch only", ev, ok, err)
		}
	}
	for _, ev := range []ciEvent{
		{Name: "schedule", Branch: "main", Repo: "mas-bandwidth/schema"},
		{Name: "workflow_dispatch", Branch: "main", Repo: "mas-bandwidth/schema"},
	} {
		for _, job := range []string{"corpus", "negative-control"} {
			if ok, err := jobSelectsSelfHosted(workflow, job, ev); err != nil || !ok {
				t.Errorf("the Luau %s job did not run on %+v: ok=%v err=%v", job, ev, ok, err)
			}
		}
	}
}
