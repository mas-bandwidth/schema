package ci

// THE PER-PR TIER IS THE HAPPY PATH, AND ONLY THE HAPPY PATH (the owner,
// 2026-11-25, live: per pull request, schema runs ONLY compile + one simple
// happy path per language, one job of two minutes or less; everything else
// is nightly).
//
// ci-fast.yml is the one workflow a push or a pull request starts that runs
// schema's code (TestOnlyTheFastLaneRunsOnPushOrPullRequest holds the rest
// to the schedule). This holds what it runs:
//
//   - its jobs are exactly happy-path and the fork guard, fork-pull-request;
//   - happy-path uses only actions/checkout, the merge-main action and the
//     hosted-only setup-go, runs make for bin/schema, happy-legs and the
//     happy-<lang> targets only, and runs no go test, go vet, go run or other
//     test runner of its own;
//   - make/happy-path.mk's HAPPY_LEGS is the registry's languages
//     (test/registry/registry.jsonl), each with its happy-<lang> rule.
//
// ci-fast-nightly.yml, the old per-PR lane, holds every job that moved; it
// starts on the schedule and on dispatch only.

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

var happyMakeCall = regexp.MustCompile(`(?:^|[\s;&|(!])make((?:\s+[^\s;&|)]+)*)`)

// happyAllowedUses are the only actions the happy-path job may call.
var happyAllowedUses = []string{"actions/checkout@", "./.github/actions/merge-main", "actions/setup-go@"}

// happyForbiddenRun are commands that would run a check beyond the happy path.
var happyForbiddenRun = regexp.MustCompile(`\bgo (?:test|vet|run)(?:\s|$)|\bschema-test\b|\bcargo\b|\bnpm\b|\bctest\b|\bpytest\b|\bgolangci-lint\b|\./tools/`)

// happyBlockScalar is a YAML block scalar header: |, >, with any chomping
// and indentation indicator (|-, |+, |2, >-2 ...).
var happyBlockScalar = regexp.MustCompile(`^[|>][-+0-9]*$`)

// happyShell is the one shell the happy-path job's steps run under.
const happyShell = "go run ./tools/pgroup -- bash -e -o pipefail {0}"

// happyRecipeForbidden are what a happy-<lang> recipe may not run: another
// make (a heavy target behind the happy name) or a test runner.
var happyRecipeForbidden = regexp.MustCompile(`\$\(MAKE\)|\$\{MAKE\}|(?:^|[\s;&|(])make\s|\bgo (?:test|vet|run)(?:\s|$)|\bcargo\b|\bmix test\b|\bdotnet test\b|\bschema-test\b|\bnpm\b|\bctest\b|\bpytest\b|\./tools/`)

// happyRuleErrors holds each happy-<lang> rule of make/happy-path.mk to the
// happy path: its prerequisites are bin/schema and toolchain-<lang> only, and
// its recipe runs no other make and no test runner.
func happyRuleErrors(mk string, legs []string) []string {
	var errs []string
	lines := strings.Split(mk, "\n")
	for _, l := range legs {
		head := "happy-" + l + ":"
		found := false
		for i, line := range lines {
			rest, ok := strings.CutPrefix(line, head)
			if !ok {
				continue
			}
			found = true
			for p := range strings.FieldsSeq(rest) {
				if p != "bin/schema" && p != "toolchain-"+l {
					errs = append(errs, fmt.Sprintf("make/happy-path.mk:%d: happy-%s needs %s; a happy path needs bin/schema and toolchain-%s only", i+1, l, p, l))
				}
			}
			for j := i + 1; j < len(lines) && strings.HasPrefix(lines[j], "\t"); j++ {
				if m := happyRecipeForbidden.FindString(lines[j]); m != "" {
					errs = append(errs, fmt.Sprintf("make/happy-path.mk:%d: happy-%s runs %q: the happy path compiles and runs one round trip, nothing more", j+1, l, strings.TrimSpace(m)))
				}
			}
		}
		if !found {
			errs = append(errs, fmt.Sprintf("make/happy-path.mk has no happy-%s rule", l))
		}
	}
	return errs
}

func happyPathErrors(wfs map[string]string, mk string, langs []string) []string {
	var errs []string
	fast, ok := wfs["ci-fast.yml"]
	if !ok {
		return []string{"there is no .github/workflows/ci-fast.yml: no pull request runs the happy path"}
	}
	jobs := yamlJobs(fast)
	got := slices.Sorted(maps.Keys(jobs))
	if want := []string{"fork-pull-request", "happy-path"}; !slices.Equal(got, want) {
		errs = append(errs, fmt.Sprintf("ci-fast.yml has jobs %v; the per-PR tier is exactly %v (the owner, 2026-11-25: per pull request, ONLY compile + one simple happy path per language): a heavier job runs in ci-fast-nightly.yml", got, want))
	}
	body := jobs["happy-path"]
	if body == "" {
		return append(errs, "ci-fast.yml has no happy-path job")
	}
	inRun := false
	runIndent := 0
	sawHappy := false
	for raw := range strings.SplitSeq(body, "\n") {
		trimmed := strings.TrimSpace(raw)
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		if inRun && trimmed != "" && indent <= runIndent {
			inRun = false
		}
		if strings.HasPrefix(trimmed, "#") || trimmed == "" {
			continue
		}
		if s, ok := strings.CutPrefix(strings.TrimPrefix(trimmed, "- "), "shell:"); ok && !inRun {
			if strings.TrimSpace(s) != happyShell {
				errs = append(errs, fmt.Sprintf("ci-fast.yml:happy-path sets shell %q; the happy path runs under %q only", strings.TrimSpace(s), happyShell))
			}
			continue
		}
		if u, ok := strings.CutPrefix(strings.TrimPrefix(trimmed, "- "), "uses:"); ok && !inRun {
			u = strings.Trim(strings.TrimSpace(strings.SplitN(u, "#", 2)[0]), `"'`)
			if !slices.ContainsFunc(happyAllowedUses, func(p string) bool {
				if strings.HasSuffix(p, "@") {
					return strings.HasPrefix(u, p)
				}
				return u == p
			}) {
				errs = append(errs, fmt.Sprintf("ci-fast.yml:happy-path uses %s; the happy path uses only %v", u, happyAllowedUses))
			}
			continue
		}
		line := trimmed
		if r, ok := strings.CutPrefix(strings.TrimPrefix(trimmed, "- "), "run:"); ok {
			r = strings.TrimSpace(r)
			if happyBlockScalar.MatchString(r) {
				inRun, runIndent = true, indent
				continue
			}
			line = r
		} else if !inRun {
			continue
		}
		if m := happyForbiddenRun.FindString(line); m != "" {
			errs = append(errs, fmt.Sprintf("ci-fast.yml:happy-path runs %q (%s): the per-PR job runs compile + one round trip per language and nothing more; that check runs nightly", m, line))
		}
		for _, mc := range happyMakeCall.FindAllStringSubmatch(line, -1) {
			for w := range strings.FieldsSeq(mc[1]) {
				w = strings.Trim(w, `"'`)
				switch {
				case strings.HasPrefix(w, "-"):
				case w == "bin/schema", w == "happy-legs":
				case w == "happy-$l":
					sawHappy = true
				default:
					errs = append(errs, fmt.Sprintf("ci-fast.yml:happy-path runs make %s: the per-PR job makes bin/schema, happy-legs and happy-<lang> only", w))
				}
			}
		}
	}
	if !sawHappy {
		errs = append(errs, "ci-fast.yml:happy-path never runs make happy-<lang>: the per-PR tier checks nothing")
	}

	var legs []string
	for line := range strings.SplitSeq(mk, "\n") {
		if rest, ok := strings.CutPrefix(line, "HAPPY_LEGS :="); ok {
			legs = strings.Fields(rest)
		}
	}
	if len(legs) == 0 {
		errs = append(errs, "make/happy-path.mk sets no HAPPY_LEGS")
	}
	sortedLegs := slices.Sorted(slices.Values(legs))
	if !slices.Equal(sortedLegs, langs) {
		errs = append(errs, fmt.Sprintf("make/happy-path.mk HAPPY_LEGS is %v; the registry's languages are %v: every language gets its happy path, per pull request", sortedLegs, langs))
	}
	return append(errs, happyRuleErrors(mk, legs)...)
}

// happyPlantMk replaces from with to in make/happy-path.mk, failing the test
// when from is not there (a plant that changes nothing proves nothing).
func happyPlantMk(t *testing.T, mk, from, to string) string {
	t.Helper()
	if !strings.Contains(mk, from) {
		t.Fatalf("control cannot plant: make/happy-path.mk does not contain %q", from)
	}
	return strings.Replace(mk, from, to, 1)
}

// scheduleSkips lists the ci-fast-nightly.yml jobs a schedule run with every
// plan output set would skip. The schedule is the old per-PR lane's only
// regular run, so a job it skips runs nowhere; the verdict refuses any skip on
// a schedule, and this holds the job conditions to that.
func scheduleSkips(wf string) ([]string, int, error) {
	ev := fullPlan(ciEvent{Name: "schedule", Branch: "main", Repo: "mas-bandwidth/schema"})
	var skips []string
	n := 0
	for _, job := range slices.Sorted(maps.Keys(yamlJobs(wf))) {
		if job == "fork-pull-request" {
			continue
		}
		n++
		runs, err := jobRunsInWorkflow(wf, job, ev)
		if err != nil {
			return nil, 0, fmt.Errorf("%s: %w", job, err)
		}
		if !runs {
			skips = append(skips, job)
		}
	}
	return skips, n, nil
}

func TestTheScheduleRunsEveryNightlyJob(t *testing.T) {
	t.Parallel()
	wf, ok := workflows(t, repoRoot(t))["ci-fast-nightly.yml"]
	if !ok {
		t.Fatal("there is no .github/workflows/ci-fast-nightly.yml")
	}
	skips, n, err := scheduleSkips(wf)
	if err != nil {
		t.Fatal(err)
	}
	if n < 20 {
		t.Fatalf("ci-fast-nightly.yml parsed as %d jobs; the old per-PR lane had more than 20: the job scan matched too little", n)
	}
	for _, job := range skips {
		t.Errorf("ci-fast-nightly.yml:%s is skipped on the schedule with every plan output set: it runs nowhere", job)
	}
	from := "if: (needs.plan.outputs.legs != '[]' && (github.event_name == 'workflow_dispatch' || github.event_name == 'schedule'))"
	if !strings.Contains(wf, from) {
		t.Fatalf("control cannot plant: ci-fast-nightly.yml leg-gate has no %q", from)
	}
	planted := strings.Replace(wf, from, "if: (needs.plan.outputs.legs != '[]' && github.event_name == 'workflow_dispatch')", 1)
	if skips, _, err := scheduleSkips(planted); err != nil || !slices.Contains(skips, "leg-gate") {
		t.Errorf("PLANTED leg-gate off the schedule left the gate green (skips %v, err %v)", skips, err)
	}
}

// registryLangs reads the lang rows of test/registry/registry.jsonl.
func registryLangs(t *testing.T, root string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "test", "registry", "registry.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var langs []string
	for line := range strings.SplitSeq(string(raw), "\n") {
		var rec struct{ Rec, Name string }
		if strings.TrimSpace(line) == "" {
			continue
		}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("test/registry/registry.jsonl: %v", err)
		}
		if rec.Rec == "lang" {
			langs = append(langs, rec.Name)
		}
	}
	sort.Strings(langs)
	if len(langs) == 0 {
		t.Fatal("test/registry/registry.jsonl has no lang rows: this gate would pass over no language")
	}
	return langs
}

func TestPerPRIsTheHappyPathOnly(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	wfs := workflows(t, root)
	raw, err := os.ReadFile(filepath.Join(root, "make", "happy-path.mk"))
	if err != nil {
		t.Fatal(err)
	}
	mk := string(raw)
	langs := registryLangs(t, root)
	for _, e := range happyPathErrors(wfs, mk, langs) {
		t.Error(e)
	}
	if _, ok := wfs["ci-fast-nightly.yml"]; !ok {
		t.Error("there is no .github/workflows/ci-fast-nightly.yml: the jobs that left the per-PR tier run nowhere")
	} else if workflowStartsOnPushOrPR(wfs["ci-fast-nightly.yml"]) {
		t.Error("ci-fast-nightly.yml starts on a push or a pull request; it runs on the schedule and on dispatch only")
	}

	plant := func(from, to string) map[string]string {
		t.Helper()
		m := maps.Clone(wfs)
		if !strings.Contains(m["ci-fast.yml"], from) {
			t.Fatalf("control cannot plant: ci-fast.yml does not contain %q", from)
		}
		m["ci-fast.yml"] = strings.Replace(m["ci-fast.yml"], from, to, 1)
		return m
	}
	heavyJob := "  go-test:\n    if: (github.event_name != 'pull_request' || github.event.pull_request.head.repo.full_name == github.repository)\n    runs-on: [self-hosted, Linux, X64, fleet, schema]\n    timeout-minutes: 2\n    steps:\n      - run: go test ./...\n\n"
	for _, c := range []struct {
		name string
		wfs  map[string]string
		mk   string
	}{
		{"a heavy job back on the per-PR tier", plant("  happy-path:\n", heavyJob+"  happy-path:\n"), mk},
		{"a go test step in the happy path", plant("      - name: build bin/schema once\n", "      - name: go test\n        run: go test ./internal/...\n      - name: build bin/schema once\n"), mk},
		{"a heavy make target in the happy path", plant("        run: make bin/schema\n", "        run: make bin/schema test-go\n"), mk},
		{"the registry driver in the happy path", plant("        run: make bin/schema\n", "        run: |\n          make bin/schema\n          go run ./tools/schema-test run --group pr\n"), mk},
		{"another action in the happy path", plant("      - uses: ./.github/actions/merge-main\n", "      - uses: ./.github/actions/merge-main\n      - uses: actions/cache@v4\n"), mk},
		{"a leg dropped from HAPPY_LEGS", wfs, strings.Replace(mk, " unity\n", "\n", 1)},
		{"a leg with no rule", wfs, strings.Replace(mk, "\nhappy-unity:", "\nhappy-unity-gone:", 1)},
		{"a go test behind a |+ block scalar", plant("        run: make bin/schema\n", "        run: |+\n          make bin/schema\n          go test ./...\n"), mk},
		{"a step-level shell in the happy path", plant("        run: make bin/schema\n", "        shell: bash\n        run: make bin/schema\n"), mk},
		{"a look-alike action in the happy path", plant("      - uses: ./.github/actions/merge-main\n", "      - uses: ./.github/actions/merge-main-and-test\n"), mk},
		{"a heavy prerequisite on a happy rule", wfs, happyPlantMk(t, mk, "happy-go: bin/schema\n", "happy-go: bin/schema test-go\n")},
		{"another make in a happy recipe", wfs, happyPlantMk(t, mk, "\t./build/happy/c/happy\n", "\t./build/happy/c/happy\n\t$(MAKE) test-c\n")},
		{"a plain make in a happy recipe", wfs, happyPlantMk(t, mk, "\t./build/happy/c/happy\n", "\t./build/happy/c/happy\n\tmake test-c\n")},
		{"go test in a happy recipe", wfs, happyPlantMk(t, mk, "\t./build/happy/go/happy-bin\n", "\t./build/happy/go/happy-bin\n\tgo test ./internal/...\n")},
		{"cargo in a happy recipe", wfs, happyPlantMk(t, mk, "\t./build/happy/rust/happy_rust\n", "\t./build/happy/rust/happy_rust\n\tcargo test\n")},
		{"mix test in a happy recipe", wfs, happyPlantMk(t, mk, "\t$(ELIXIR) test/happy/elixir/main.exs build/happy/elixir\n", "\t$(ELIXIR) test/happy/elixir/main.exs build/happy/elixir\n\tmix test\n")},
		{"dotnet test in a happy recipe", wfs, happyPlantMk(t, mk, "\t$(DOTNET) exec build/happy/cs/happy.dll\n", "\t$(DOTNET) exec build/happy/cs/happy.dll\n\tdotnet test\n")},
	} {
		if len(happyPathErrors(c.wfs, c.mk, langs)) == 0 {
			t.Errorf("PLANTED %q left the gate green", c.name)
		}
	}
}
