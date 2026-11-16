package ci

import (
	"maps"
	"strings"
	"testing"
)

func TestRunPatternExceptionsSurviveAnInsertedRule(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	files := makefileFiles(t, root)
	const inserted = "# an inserted rule with its own recipe\ninserted-runpattern-rule:\n\tgo test ./internal/ci -run '^TestRunPatternGateRefusesAZeroMatchPattern$$' -count=1\n\n"
	shifted := maps.Clone(files)
	for _, name := range []string{"Makefile", "make/go.mk"} {
		text, ok := shifted[name]
		if !ok {
			t.Fatalf("makefile set does not include %s", name)
		}
		shifted[name] = inserted + text
	}

	_, _, problems := bindRunPatternExceptions(shifted, runPatternExceptions)
	if len(problems) > 0 {
		t.Fatalf("inserting a rule moved %d exception(s):\n%s", len(problems), strings.Join(problems, "\n"))
	}

	const key = "Makefile slow-gate-coverage"
	excepted := false
	for _, e := range runPatternExceptions {
		if e.where == key {
			excepted = true
			break
		}
	}
	if !excepted {
		t.Fatalf("runPatternExceptions does not carry %q", key)
	}

	renamed := maps.Clone(shifted)
	renamed["Makefile"] = strings.Replace(renamed["Makefile"], "slow-gate-coverage: deps", "slow-gate-coverage-renamed: deps", 1)
	if renamed["Makefile"] == shifted["Makefile"] {
		t.Fatal("could not rename the slow-gate-coverage rule in the Makefile text")
	}
	assertOneStaleException(t, renamed, key, "renaming the rule")

	changed := maps.Clone(shifted)
	changed["Makefile"] = strings.Replace(changed["Makefile"], "$$(go run ./tools/slowgatescan -emit-gated-run)", "$$(go run ./tools/slowgatescan -emit-gated-run-changed)", 1)
	if changed["Makefile"] == shifted["Makefile"] {
		t.Fatal("could not change the slow-gate-coverage pattern in the Makefile text")
	}
	assertOneStaleException(t, changed, key, "changing the pattern")
}

func assertOneStaleException(t *testing.T, files map[string]string, key, action string) {
	t.Helper()
	_, _, problems := bindRunPatternExceptions(files, runPatternExceptions)
	if len(problems) != 1 {
		t.Fatalf("%s should make exactly one exception stale, got %d:\n%s", action, len(problems), strings.Join(problems, "\n"))
	}
	if !strings.Contains(problems[0], key) {
		t.Fatalf("%s made the wrong exception stale, want %q: %s", action, key, problems[0])
	}
}

func TestRunRecipesKeyByRuleDefineAndOrdinal(t *testing.T) {
	t.Parallel()
	const fixture = `.PHONY: alpha beta

alpha:
	go test ./internal/ci -run '^TestAlpha$' -count=1
	go test ./internal/ci -run '^TestAlpha$' -count=1
	go test ./internal/ci -run '^TestOther$' -count=1

beta:
	go test ./internal/ci -run '^TestBeta$' -count=1

define gamma_control
	go test ./internal/ci -run '$(1)' -count=1
endef
`
	recipes := runRecipes("fixture.mk", fixture)
	if len(recipes) != 5 {
		t.Fatalf("want 5 recipes, got %d: %+v", len(recipes), recipes)
	}
	got := map[string]runRecipe{}
	for _, r := range recipes {
		got[r.where] = r
	}
	wants := []struct {
		where, pattern, rule string
		ordinal, line        int
	}{
		{"fixture.mk alpha #1", "^TestAlpha$", "alpha", 1, 4},
		{"fixture.mk alpha #2", "^TestAlpha$", "alpha", 2, 5},
		{"fixture.mk alpha", "^TestOther$", "alpha", 1, 6},
		{"fixture.mk beta", "^TestBeta$", "beta", 1, 9},
		{"fixture.mk define gamma_control", "$(1)", "define gamma_control", 1, 12},
	}
	for _, w := range wants {
		r, ok := got[w.where]
		if !ok {
			t.Errorf("no recipe keyed %q; got %v", w.where, got)
			continue
		}
		if r.file != "fixture.mk" || r.rule != w.rule || r.ordinal != w.ordinal || r.line != w.line || r.pattern != w.pattern {
			t.Errorf("recipe %q = file %q rule %q ordinal %d line %d pattern %q, want file %q rule %q ordinal %d line %d pattern %q", w.where, r.file, r.rule, r.ordinal, r.line, r.pattern, "fixture.mk", w.rule, w.ordinal, w.line, w.pattern)
		}
	}
}

func TestPythonDynamicRunPatternBindingsAndCoverageWiring(t *testing.T) {
	t.Parallel()
	files := makefileFiles(t, repoRoot(t))
	checks := []struct{ file, rule, pattern, changed string }{
		{"make/python-fixed-proof-evolution.mk", "python-fixed-proof-evolution", "$(PYTHON_FIXED_EVOLUTION_AGGREGATE)", "$(UNKNOWN_EVOLUTION)"},
		{"make/python-handtext.mk", "python-handtext-core", "$(PYTHON_HANDTEXT_CORE)", "$(UNKNOWN_HANDTEXT)"},
	}
	for _, tc := range checks {
		original := files[tc.file]
		key := tc.file + " " + tc.rule
		changed := maps.Clone(files)
		changed[tc.file] = strings.Replace(original, "-run '"+tc.pattern+"'", "-run '"+tc.changed+"'", 1)
		if changed[tc.file] == original {
			t.Fatalf("missing recipe for %s", key)
		}
		assertOneStaleException(t, changed, key, "changing the dynamic selector")
		recipes, bound, _ := bindRunPatternExceptions(changed, runPatternExceptions)
		found := false
		for _, r := range recipes {
			if r.where == key && r.pattern == tc.changed {
				found = true
				if bound[runRecipeBound{r.where, r.pattern}] {
					t.Fatalf("unknown selector %s unexpectedly exempted", tc.changed)
				}
			}
		}
		if !found {
			t.Fatalf("changed selector %s not discovered", tc.changed)
		}
	}
	for _, tc := range []struct{ file, target, test string }{
		{"make/python-fixed-proof-evolution.mk", "python-fixed-evolution-ci-coverage", "TestPythonFixedEvolutionCICoverage"},
		{"make/python-handtext.mk", "python-handtext-ci-coverage", "TestPythonHandtextCISelection"},
	} {
		s := files[tc.file]
		for _, want := range []string{tc.target + ":", "SCHEMA_SLOW=1 go test -count=1 -tags schema_leg_python -run '^" + tc.test + "$$' ./internal/ci/", "test-python:"} {
			if !strings.Contains(s, want) {
				t.Errorf("%s lacks %q", tc.file, want)
			}
		}
		for _, prefix := range []string{"test-python:", "PYTHON_CI_SUPPORT_GROUPS +="} {
			found := false
			for _, line := range strings.Split(s, "\n") {
				if strings.HasPrefix(line, prefix) {
					for _, field := range strings.Fields(line[len(prefix):]) {
						if field == tc.target {
							found = true
						}
					}
				}
			}
			if !found {
				t.Errorf("%s lacks %s in %s", tc.file, tc.target, prefix)
			}
		}
	}
}
