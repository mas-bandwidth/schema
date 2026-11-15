package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSchemaNativeRunsHostileCorpusWiring holds the C and C++ cards' blades
// without a compiler in the Go unit tier: each port's make target and its
// planted-verdict control exist, the case binary is driven through
// tools/hostilecorpusexec, every FIX-CARD names a case the manifest carries
// with that case's own input, and the workflow is fleet-only, runs nightly
// and on dispatch only (the owner, 2026-11-15), with its control registered in
// make/negative-controls.json.
func TestSchemaNativeRunsHostileCorpusWiring(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	manifestBytes, err := os.ReadFile(filepath.Join(root, "internal", "ci", "hostilecorpus", "corpus", "manifest.tsv"))
	if err != nil {
		t.Fatalf("the committed manifest is missing: %v", err)
	}
	cases := map[string]string{}
	for line := range strings.SplitSeq(string(manifestBytes), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 9 {
			t.Fatalf("manifest line has %d fields, want 9: %q", len(f), line)
		}
		cases[f[0]] = f[4]
	}
	controls, err := os.ReadFile(filepath.Join(root, "make", "negative-controls.json"))
	if err != nil {
		t.Fatalf("the controls registry is missing: %v", err)
	}

	for _, port := range []string{"c", "cpp"} {
		target := "schema-" + port + "-runs-hostile-corpus"
		makeFile, err := os.ReadFile(filepath.Join(root, "make", target+".mk"))
		if err != nil {
			t.Fatalf("make/%s.mk is missing: %v", target, err)
		}
		for _, want := range []string{
			".PHONY: " + target,
			target + "-negative-control",
			"hostilecorpusexec run -port " + port,
			"-fsanitize=address,undefined",
			"internal/ci/hostilecorpus/corpus",
			"test/" + port + "-hostile/known-findings.tsv",
		} {
			if !strings.Contains(string(makeFile), want) {
				t.Errorf("make/%s.mk does not carry %q", target, want)
			}
		}
		if !strings.Contains(string(controls), `"`+target+`-negative-control"`) {
			t.Errorf("make/negative-controls.json does not register %s-negative-control", target)
		}

		findings, err := os.ReadFile(filepath.Join(root, "test", port+"-hostile", "known-findings.tsv"))
		if err != nil {
			t.Fatalf("test/%s-hostile/known-findings.tsv is missing: %v", port, err)
		}
		for line := range strings.SplitSeq(string(findings), "\n") {
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			f := strings.Split(line, "\t")
			if len(f) != 4 {
				t.Errorf("%s carried finding has %d fields, want 4: %q", port, len(f), line)
				continue
			}
			input, ok := cases[f[0]]
			if !ok {
				t.Errorf("%s carried finding %q names a case the manifest does not carry", port, f[0])
				continue
			}
			if f[2] != input {
				t.Errorf("%s carried finding %q names input %q, manifest says %q", port, f[0], f[2], input)
			}
			if !strings.HasPrefix(f[1], port+"-hostile-") || strings.TrimSpace(f[3]) == "" {
				t.Errorf("%s carried finding %q has no FIX-CARD slug and one line: %q", port, f[0], line)
			}
		}

		workflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", target+".yml"))
		if err != nil {
			t.Fatalf("the %s workflow is missing: %v", target, err)
		}
		text := string(workflow)
		for _, want := range []string{
			"schedule:", "workflow_dispatch:",
			"runs-on: [self-hosted, linux, x64, schema]",
			"if: github.event_name != 'pull_request' || github.event.pull_request.head.repo.full_name == github.repository",
			"make " + target + " ",
			"make " + target + "-negative-control ",
		} {
			if !strings.Contains(text, want) {
				t.Errorf("%s.yml does not carry %q", target, want)
			}
		}
		// NIGHTLY AND DISPATCH ONLY (the owner, 2026-11-15: "I want everything that
		// is not light moved to nightly test now." "no exceptions."): no push and
		// no pull request starts this workflow; those run only ci-fast.yml.
		for _, p := range nightlyOnlyTriggerProblems(text) {
			t.Errorf("%s.yml: %s", target, p)
		}
		for _, ev := range []ciEvent{
			{Name: "pull_request", Branch: "main", Repo: "mas-bandwidth/schema", HeadRepo: "mas-bandwidth/schema"},
			{Name: "push", Branch: "main", Repo: "mas-bandwidth/schema"},
			{Name: "push", Branch: "dev", Repo: "mas-bandwidth/schema"},
		} {
			if ok, err := jobSelectsSelfHosted(text, "corpus", ev); err != nil || ok {
				t.Errorf("%s.yml:corpus ran on %+v: ok=%v err=%v; the workflow is nightly and dispatch only", target, ev, ok, err)
			}
		}
		for _, ev := range []ciEvent{
			{Name: "schedule", Branch: "main", Repo: "mas-bandwidth/schema"},
			{Name: "workflow_dispatch", Branch: "main", Repo: "mas-bandwidth/schema"},
		} {
			for _, job := range []string{"corpus", "negative-control"} {
				if ok, err := jobSelectsSelfHosted(text, job, ev); err != nil || !ok {
					t.Errorf("%s.yml:%s did not run on %+v: ok=%v err=%v", target, job, ev, ok, err)
				}
			}
		}
		// Mutation control: a restored push trigger is refused.
		if len(nightlyOnlyTriggerProblems(strings.Replace(text, "\non:\n", "\non:\n  push:\n", 1))) == 0 {
			t.Errorf("%s.yml: nightly-only control did not refuse a restored push trigger", target)
		}
		fork := ciEvent{Name: "pull_request", Branch: "main", Repo: "mas-bandwidth/schema", HeadRepo: "fork/schema"}
		for _, job := range []string{"corpus", "negative-control"} {
			if ok, err := jobSelectsSelfHosted(text, job, fork); err != nil || ok {
				t.Errorf("%s.yml:%s admitted a fork pull request: ok=%v err=%v", target, job, ok, err)
			}
		}
	}
}
