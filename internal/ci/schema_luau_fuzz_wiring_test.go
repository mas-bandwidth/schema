package ci

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestSchemaLuauHostileCorpusAndWireFuzzWiring holds the Luau port's hostile
// corpus and wire-fuzz blades without the interpreter in the Go unit tier, as
// TestSchemaElixirRunsHostileCorpusWiring holds the Elixir port's: each gate
// is a make target the port's unit target runs, each control is registered on
// the pull-request tier of make/negative-controls.json, the wire fuzz holds the
// roots its leg cannot open to a pinned list, and the corpus workflow has both
// a push/pull-request gate and a scheduled control, fleet only, fork guarded.
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
		"test/conformance/luau/wire-fuzz-absent.txt",
		"LUAU_WIRE_FUZZ_ABSENT_COUNT",
		"0 divergences",
		"sh test/conformance/luau/wire-fuzz-negative-control")
	carries("test/conformance/luau/wirefuzz.luau", read("test/conformance/luau/wirefuzz.luau"), "host.note(")

	// the pinned absent list: one line per root, and the count the target
	// holds it to
	pinned := 0
	for line := range strings.SplitSeq(read("test/conformance/luau/wire-fuzz-absent.txt"), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if f := strings.Fields(line); len(f) != 3 || !strings.HasPrefix(f[1], "form=") {
			t.Errorf("wire-fuzz-absent.txt line is not <unit>.<root> form=<f> <reason>: %q", line)
		}
		pinned++
	}
	fuzz := read("make/luau-fuzz.mk")
	count := ""
	for line := range strings.SplitSeq(fuzz, "\n") {
		if rest, ok := strings.CutPrefix(line, "LUAU_WIRE_FUZZ_ABSENT_COUNT := "); ok {
			count = strings.TrimSpace(rest)
		}
	}
	if count == "" || count != strconv.Itoa(pinned) {
		t.Errorf("LUAU_WIRE_FUZZ_ABSENT_COUNT is %q, the pinned list has %d roots", count, pinned)
	}

	controls := read("make/negative-controls.json")
	at := strings.Index(controls, `"name": "luau"`)
	if at < 0 {
		t.Fatal("make/negative-controls.json has no luau group")
	}
	group, _, _ := strings.Cut(controls[at:], "}")
	carries("make/negative-controls.json (the luau group)", group,
		`"when": "pull-request"`,
		`"schema-luau-runs-hostile-corpus-negative-control"`,
		`"tables-luau-wire-fuzz-negative-control"`,
		`"luau-nightly-fuzz-negative-control"`)

	workflow := read(".github/workflows/schema-luau-runs-hostile-corpus.yml")
	carries("schema-luau-runs-hostile-corpus.yml", workflow,
		"push:", "pull_request:", "schedule:", "workflow_dispatch:",
		"branches: [ main, dev, fixed-table-form, 'sprint/**' ]",
		"if: github.event_name != 'pull_request' || github.event.pull_request.head.repo.full_name == github.repository",
		"make dist-luau",
		"make schema-luau-runs-hostile-corpus\n",
		"make schema-luau-runs-hostile-corpus-negative-control")
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
	} {
		if ok, err := jobSelectsSelfHosted(workflow, "corpus", ev); err != nil || !ok {
			t.Errorf("the Luau corpus job did not run on %+v: ok=%v err=%v", ev, ok, err)
		}
		if ok, _ := jobSelectsSelfHosted(workflow, "negative-control", ev); ok {
			t.Errorf("the Luau negative-control job ran on %+v; it is the scheduled half", ev)
		}
	}
}
