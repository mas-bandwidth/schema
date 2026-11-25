package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// TestEveryRetiredCheckIsStillRun: every check a deleted workflow or a
// dropped ci-fast.yml job ran (test/registry/retired.tsv) is run by the
// registry row the file names. The plants, each red by the line that names
// the check: the row removed, one language's gate dropped from a
// per-language row, a language-free row's target renamed, a Go test's row
// pointed at another test, a per-PR check whose row runs only at night, a
// check no row runs yet, and a line that does not read; and a per-PR
// workflow that runs the nightly row by name keeps it per PR.
func TestEveryRetiredCheckIsStillRun(t *testing.T) {
	t.Parallel()
	reg := loadRepoRegistry(t)
	data, err := os.ReadFile(filepath.Join(repoRoot, retiredPath))
	if err != nil {
		t.Fatal(err)
	}
	wfs, err := readWorkflows(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	named := keysRunByName(wfs)
	// the repository's account is checked after the plants, so a red
	// account never hides a plant that stopped biting
	defer func() {
		if p := checkRetired(reg, data, named); len(p) > 0 {
			t.Errorf("retired checks the registry does not run (%d):\n  %s", len(p), strings.Join(p, "\n  "))
		}
	}()

	gates := `{"make":{"c":[{"file":"make/c.mk","target":"schema-c-x"}],"lua":[{"file":"make/lua.mk","target":"schema-lua-x"}]}}`
	fixture := func() *testreg.Registry {
		return &testreg.Registry{Tiers: []testreg.Tier{{Name: "unit"}, {Name: "functional"}, {Name: "nightly"}}, Langs: []testreg.Lang{{Name: "c", Adapter: "corpus"}, {Name: "lua", Adapter: "corpus"}}, Groups: []testreg.Group{{Name: "pr", Stored: testreg.GroupQuery{Tiers: []string{"unit", "functional"}}}}, Tests: []testreg.Test{
			{Key: "load-refusal.x.runs", Inputs: []byte(gates)},
			{Key: "tooling.x.free", Scope: testreg.LangFree, Adapter: "make", Inputs: []byte(`{"file":"Makefile","target":"free-check"}`)},
			{Key: "tooling.ci.wiring", Tier: "unit", Scope: testreg.LangFree, Adapter: "gotest", Inputs: []byte(`{"package":"./internal/ci","test":"TestXWiring"}`)},
			// a pr-group row no retired line names, so the group is never empty
			{Key: "tooling.x.pr", Tier: "unit", Scope: testreg.LangFree, Adapter: "gotest", Inputs: []byte(`{"package":"./internal/ci","test":"TestXPR"}`)},
		}}
	}
	file := "# retired\n" +
		"schema-c-x.yml\tcorpus\tmake\tschema-c-x\tload-refusal.x.runs\tc\n" +
		"schema-lua-x.yml\tcorpus\tmake\tschema-lua-x\tload-refusal.x.runs\tlua\n" +
		"free.yml\tjob\tmake\tfree-check\ttooling.x.free\tnone\n" +
		"x.yml\twiring\tgotest\tinternal/ci TestXWiring\ttooling.ci.wiring\tnone\n"
	if p := checkRetired(fixture(), []byte(file), nil); len(p) > 0 {
		t.Fatalf("the fixture is red: %v", p)
	}
	for name, c := range map[string]struct {
		plant func(*testreg.Registry) string
		says  string
	}{
		"the row removed": {func(r *testreg.Registry) string { r.Tests = r.Tests[1:]; return file },
			retiredPath + ":2: schema-c-x.yml corpus ran schema-c-x, and the registry has no row load-refusal.x.runs"},
		"a language's gate dropped": {func(r *testreg.Registry) string {
			r.Tests[0].Inputs = []byte(strings.Replace(gates, `,"lua":[{"file":"make/lua.mk","target":"schema-lua-x"}]`, "", 1))
			return file
		}, retiredPath + ":3: schema-lua-x.yml corpus ran schema-lua-x, and row load-refusal.x.runs names no lua gate schema-lua-x"},
		"a target renamed": {func(r *testreg.Registry) string {
			r.Tests[1].Inputs = []byte(`{"file":"Makefile","target":"other-check"}`)
			return file
		}, retiredPath + ":4: free.yml job ran free-check, and row tooling.x.free runs other-check, not free-check"},
		"a go test's row moved to another test": {func(r *testreg.Registry) string {
			r.Tests[2].Inputs = []byte(`{"package":"./internal/ci","test":"TestXOther"}`)
			return file
		}, retiredPath + ":5: x.yml wiring ran internal/ci TestXWiring, and row tooling.ci.wiring runs ./internal/ci TestXOther, not internal/ci TestXWiring"},
		"a per-PR check moved to the night": {func(r *testreg.Registry) string {
			r.Tests[2].Tier = "nightly"
			return file + "ci-fast.yml\tgo-test\tgotest\tinternal/ci TestXWiring\ttooling.ci.wiring\tnone\n"
		}, retiredPath + ":6: ci-fast.yml go-test ran internal/ci TestXWiring, and row tooling.ci.wiring is tier nightly in none, outside the pr group"},
		"a check no row runs yet": {func(r *testreg.Registry) string { return file + "ci-fast.yml\tlint-fast\tmake\tgofmt-check\t-\tnone\n" },
			retiredPath + ":6: ci-fast.yml lint-fast ran gofmt-check, and no registry row runs it yet"},
		"a line that does not read": {func(r *testreg.Registry) string { return file + "short\tline\n" },
			retiredPath + ":6: want 6 tab-separated fields"},
		"a kind that is no kind": {func(r *testreg.Registry) string { return file + "x.yml\tj\tbogus\tc\ttooling.x.free\tnone\n" },
			retiredPath + `:6: kind "bogus" is not make, script or gotest`},
		"a per-language row's check with no language": {func(r *testreg.Registry) string {
			return file + "x.yml\tj\tmake\tschema-c-x\tload-refusal.x.runs\tnone\n"
		}, retiredPath + ":6: x.yml j ran schema-c-x, and row load-refusal.x.runs is per-language, and the check names no language"},
		"a row whose inputs do not read": {func(r *testreg.Registry) string {
			r.Tests[1].Inputs = []byte(`{`)
			return file
		}, retiredPath + ":4: free.yml job ran free-check, and row tooling.x.free has inputs that do not read"},
		"a row that runs no make target": {func(r *testreg.Registry) string {
			r.Tests[1].Inputs = []byte(`{"file":"Makefile"}`)
			return file
		}, retiredPath + ":4: free.yml job ran free-check, and row tooling.x.free is no make row"},
		"a go test's line on a make row": {func(r *testreg.Registry) string {
			return file + "x.yml\tj\tgotest\tinternal/ci TestXWiring\ttooling.x.free\tnone\n"
		}, retiredPath + ":6: x.yml j ran internal/ci TestXWiring, and row tooling.x.free is no gotest row of internal/ci TestXWiring"},
		"an empty file": {func(r *testreg.Registry) string { return "# nothing\n" },
			retiredPath + " holds no retired check"},
	} {
		r := fixture()
		f := c.plant(r)
		p := checkRetired(r, []byte(f), nil)
		if !strings.Contains(strings.Join(p, "\n"), c.says) {
			t.Errorf("%s: want %q in:\n  %s", name, c.says, strings.Join(p, "\n  "))
		}
	}
	// a per-language row of one target, {lang} spelled per language, runs
	// that language's target; the plant: another language's line for it
	r0 := fixture()
	r0.Tests = append(r0.Tests, testreg.Test{Key: "decode.x.case", Inputs: []byte(`{"target":"schema-{lang}-y","case":"a"}`)})
	one := file + "schema-lua-y.yml\tcorpus\tmake\tschema-lua-y\tdecode.x.case\tlua\n"
	if p := checkRetired(r0, []byte(one), nil); len(p) > 0 {
		t.Errorf("a {lang} target row is named red for its own language: %v", p)
	}
	if p := checkRetired(r0, []byte(file+"schema-lua-y.yml\tcorpus\tmake\tschema-lua-y\tdecode.x.case\tc\n"), nil); !strings.Contains(strings.Join(p, "\n"), "row decode.x.case runs schema-c-y, not schema-lua-y") {
		t.Errorf("a {lang} target row is not named red for another language's target: %v", p)
	}
	// a nightly row a per-PR workflow runs by name stays per PR
	r := fixture()
	r.Tests[2].Tier = "nightly"
	pr := file + "roadmap.yml\troadmap\tgotest\tinternal/ci TestXWiring\ttooling.ci.wiring\tnone\n"
	step := "      - run: go run ./tools/schema-test run none.nightly.tooling.ci.wiring\n"
	roadmap := "name: r\non:\n  pull_request:\njobs:\n  roadmap:\n    runs-on: x\n    steps:\n" + step + "  page:\n    runs-on: x\n    steps:\n      - run: go run ./tools/schema-test run none.nightly.tooling.x.page\n"
	namedHere := keysRunByName(map[string][]byte{"roadmap.yml": []byte(roadmap)})
	if p := checkRetired(r, []byte(pr), namedHere); len(p) > 0 {
		t.Errorf("a nightly row roadmap.yml's roadmap job runs by name is named red: %v", p)
	}
	if p := checkRetired(r, []byte(pr), nil); len(p) == 0 {
		t.Error("the same row, run by name by no workflow, is not named red")
	}
	// the plants of the by-name run: only in a comment, and only in a job
	// that is not the per-PR one
	for name, text := range map[string]string{
		"run by name only in a comment":     strings.Replace(roadmap, step, "      # go run ./tools/schema-test run none.nightly.tooling.ci.wiring\n      - run: go run ./tools/schema-test ci --check\n", 1),
		"run by name only in an echo":       strings.Replace(roadmap, step, "      - run: echo go run ./tools/schema-test run none.nightly.tooling.ci.wiring\n", 1),
		"run by name only in a step's name": strings.Replace(roadmap, step, "      - name: go run ./tools/schema-test run none.nightly.tooling.ci.wiring\n        run: go run ./tools/schema-test ci --check\n", 1),
		"run by name in another language":   strings.Replace(roadmap, "none.nightly.tooling.ci.wiring", "c.nightly.tooling.ci.wiring", 1),
		"run by name at another tier":       strings.Replace(roadmap, "none.nightly.tooling.ci.wiring", "none.unit.tooling.ci.wiring", 1),
		"run by name only in another job":   strings.Replace(strings.Replace(roadmap, step, "      - run: go run ./tools/schema-test ci --check\n", 1), "tooling.x.page", "tooling.ci.wiring", 1),
	} {
		if p := checkRetired(r, []byte(pr), keysRunByName(map[string][]byte{"roadmap.yml": []byte(text)})); !strings.Contains(strings.Join(p, "\n"), "a per-PR check moved to the night") {
			t.Errorf("%s: the nightly row is not named red: %v", name, p)
		}
	}
	// a per-PR check in a language the pr group does not run is not per PR,
	// whatever the row's tier
	r = fixture()
	r.Tests[0].Tier = "unit"
	r.Groups[0].Stored.Langs = []string{"c"}
	lua := file + "ci-fast.yml\tlang-lint\tmake\tschema-lua-x\tload-refusal.x.runs\tlua\n"
	if p := checkRetired(r, []byte(lua), nil); !strings.Contains(strings.Join(p, "\n"), retiredPath+":6: ci-fast.yml lang-lint ran schema-lua-x, and row load-refusal.x.runs is tier unit in lua, outside the pr group") {
		t.Errorf("a per-PR check in a language outside the pr group is not named red: %v", p)
	}
	r.Groups[0].Stored.Langs = nil
	r.Groups[0].Stored.Ready = true
	r.Langs = []testreg.Lang{{Name: "c", Adapter: "c"}, {Name: "lua"}}
	if p := checkRetired(r, []byte(lua), nil); !strings.Contains(strings.Join(p, "\n"), "is tier unit in lua, outside the pr group") {
		t.Errorf("a per-PR check in a language with no adapter is not named red: %v", p)
	}
}
