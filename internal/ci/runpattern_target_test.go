package ci

import (
	"slices"
	"sort"
	"strings"
	"testing"
)

// recipesOf extracts every -run recipe from an in-memory makefile set, in a
// fixed file order.
func recipesOf(files map[string]string) []runRecipe {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var recipes []runRecipe
	for _, n := range names {
		recipes = append(recipes, runRecipes(n, files[n])...)
	}
	return recipes
}

// insertedRule is a whole new rule with its own -run recipe, the shape every
// card that adds a make rule takes.
const insertedRule = ".PHONY: inserted-rule-for-test\n" +
	"inserted-rule-for-test:\n" +
	"\tgo test ./internal/ci -run '^TestInsertedRuleForTest$$' -count=1\n" +
	"\n"

// replaceOnce replaces old with new in text, and fails unless old occurs
// exactly once, so a planted control cannot silently plant nothing.
func replaceOnce(t *testing.T, text, old, new string) string {
	t.Helper()
	if n := strings.Count(text, old); n != 1 {
		t.Fatalf("planted control: %q occurs %d times, want exactly once", old, n)
	}
	return strings.Replace(text, old, new, 1)
}

func wantProblem(t *testing.T, problems []string, control string, parts ...string) {
	t.Helper()
	for _, p := range problems {
		ok := true
		for _, part := range parts {
			if !strings.Contains(p, part) {
				ok = false
				break
			}
		}
		if ok {
			return
		}
	}
	t.Errorf("%s: no problem names %q; got %q", control, parts, problems)
}

// TestRunPatternExceptionsSurviveAnInsertedRule inserts a new rule at the top
// of the Makefile and of make/go.mk, which holds exceptions, and binds the
// exception list over the shifted texts: no exception goes stale, and each
// one excuses the same recipe it excused before. A renamed excepted rule, a
// changed pattern in an excepted rule, a wrong target name, and an exception
// with no reason or no pattern are each refused.
func TestRunPatternExceptionsSurviveAnInsertedRule(t *testing.T) {
	t.Parallel()
	files := makefileFiles(t, repoRoot(t))
	for _, name := range []string{"Makefile", "make/go.mk"} {
		if _, ok := files[name]; !ok {
			t.Fatalf("makefile set does not include %s", name)
		}
	}

	before, problems := bindRunExceptions(recipesOf(files), runPatternExceptions)
	if len(problems) != 0 {
		t.Fatalf("the exception list does not bind to the make files as they stand:\n%s", strings.Join(problems, "\n"))
	}
	if len(before) != len(runPatternExceptions) {
		t.Fatalf("%d exceptions bound %d recipes: two exceptions name the same recipe", len(runPatternExceptions), len(before))
	}

	shifted := map[string]string{}
	for n, text := range files {
		shifted[n] = text
	}
	shifted["Makefile"] = insertedRule + shifted["Makefile"]
	shifted["make/go.mk"] = insertedRule + shifted["make/go.mk"]

	recipes := recipesOf(shifted)
	after, problems := bindRunExceptions(recipes, runPatternExceptions)
	for _, p := range problems {
		t.Errorf("after inserting a rule: %s", p)
	}
	for b := range before {
		if !after[b] {
			t.Errorf("after inserting a rule, the exception %s -run %q no longer excuses its recipe", b.key, b.pattern)
		}
	}
	if len(after) != len(before) {
		t.Errorf("after inserting a rule the exceptions excuse %d recipes, want %d", len(after), len(before))
	}
	inserted := 0
	for _, r := range recipes {
		if r.rule == "inserted-rule-for-test" {
			inserted++
			if after[runBound{r.key, r.pattern}] {
				t.Errorf("the inserted rule's recipe %s is excused, but no exception names it", r.where)
			}
		}
	}
	if inserted != 2 {
		t.Errorf("found the inserted rule's recipe %d times, want 2 (Makefile and make/go.mk)", inserted)
	}

	// Planted controls: each must make exactly the named exception stale.
	renamed := map[string]string{}
	for n, text := range shifted {
		renamed[n] = text
	}
	renamed["make/go.mk"] = replaceOnce(t, renamed["make/go.mk"], "\ntables-go-soak:", "\ntables-go-soak-renamed:")
	_, problems = bindRunExceptions(recipesOf(renamed), runPatternExceptions)
	wantProblem(t, problems, "renamed rule", "make/go.mk tables-go-soak ", "no longer names a go test -run recipe")
	if len(problems) != 1 {
		t.Errorf("renamed rule: want one stale exception, got %q", problems)
	}

	repatterned := map[string]string{}
	for n, text := range shifted {
		repatterned[n] = text
	}
	repatterned["make/go.mk"] = replaceOnce(t, repatterned["make/go.mk"], "go test -run Soak ", "go test -run SoakRenamed ")
	_, problems = bindRunExceptions(recipesOf(repatterned), runPatternExceptions)
	wantProblem(t, problems, "changed pattern", "make/go.mk tables-go-soak", `-run "Soak"`)
	if len(problems) != 1 {
		t.Errorf("changed pattern: want one stale exception, got %q", problems)
	}

	planted := []runPatternException{
		{"make/go.mk no-such-rule-for-test", "planted", "Soak"},
		{"make/go.mk tables-go-soak", "", "Soak"},
		{"make/go.mk tables-go-fuzz", "planted", "Fuzz"},
	}
	_, problems = bindRunExceptions(recipes, planted[:1])
	wantProblem(t, problems, "wrong target name", "make/go.mk no-such-rule-for-test", "no longer names a go test -run recipe")
	_, problems = bindRunExceptions(recipes, planted[1:2])
	wantProblem(t, problems, "no reason", "make/go.mk tables-go-soak", "carries no reason")
	_, problems = bindRunExceptions(recipes, planted[2:3])
	wantProblem(t, problems, "ordinal dropped from a rule with two Fuzz recipes", "make/go.mk tables-go-fuzz", "no longer names a go test -run recipe")
	_, problems = bindRunExceptions(recipes, []runPatternException{{"make/go.mk tables-go-soak", "planted", ""}})
	wantProblem(t, problems, "no pattern", "make/go.mk tables-go-soak", "names no -run pattern")
}

// TestRunRecipesKeyByRuleDefineAndOrdinal pins the key runRecipes gives a
// recipe: the rule's first target, a define's name, an ordinal only when a
// rule holds the same pattern twice, and no rule for a variable assignment.
func TestRunRecipesKeyByRuleDefineAndOrdinal(t *testing.T) {
	t.Parallel()
	const fixture = `VAR := a:b
first second: dep
	go test ./a -run '^TestA$$'
	go test ./a -run '^TestB$$'
twice: dep ## a comment
	go test ./a -run Fuzz -count 1 .
	go test -race ./a -run Fuzz -count 1 .
ifeq ($(X),y)
OTHER = c:d
endif
define tmpl
$(1): dep
	go test ./a \
		-run '$(3)' -count 1
endef
after-define: dep
	go test ./a -run '^TestC$$'
`
	var got []string
	for _, r := range runRecipes("fixture.mk", fixture) {
		got = append(got, r.key+" | "+r.pattern+" | "+r.where)
	}
	want := []string{
		"fixture.mk first | ^TestA$ | fixture.mk:3",
		"fixture.mk first | ^TestB$ | fixture.mk:4",
		"fixture.mk twice #1 | Fuzz | fixture.mk:6",
		"fixture.mk twice #2 | Fuzz | fixture.mk:7",
		"fixture.mk define tmpl | $(3) | fixture.mk:14",
		"fixture.mk after-define | ^TestC$ | fixture.mk:17",
	}
	if !slices.Equal(got, want) {
		t.Errorf("runRecipes keys:\n got %q\nwant %q", got, want)
	}
}
