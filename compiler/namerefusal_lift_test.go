package compiler

import (
	"bufio"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
)

// EVERY GENERATE-TIME NAME REFUSAL IS A CHECK REFUSAL (SPEC §4.6, Emitter name
// rules). A unit `schema check` accepts generates in every target, so a name
// a target's generate refuses must be refused by the checker first, for every
// target, naming that target. The name-buildability corpus
// (testdata/name-buildability) is the net: every row the checker accepts
// generates, in memory, in every target this build registers, and every row
// the checker refuses through an emitter's own name rule is refused at
// generate by that target once the rule is taken away (the reversed witness:
// the check, not luck, is what keeps the row out of a generate).

const nameCorpus = "../testdata/name-buildability"

type nameCorpusRow struct{ name, verdict, names string }

func readNameCorpus(t *testing.T) []nameCorpusRow {
	t.Helper()
	f, err := os.Open(filepath.Join(nameCorpus, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }() // read-only
	var rows []nameCorpusRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		c := strings.Split(line, "\t")
		if len(c) != 4 {
			t.Fatalf("cases.tsv: %q has %d columns, want 4", line, len(c))
		}
		rows = append(rows, nameCorpusRow{c[0], c[1], c[2]})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("cases.tsv holds no rows")
	}
	return rows
}

func nameCorpusPaths(t *testing.T, row string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(nameCorpus, row, "*.schema"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("row %s: no schema files (%v)", row, err)
	}
	return paths
}

// nameRuleLangs maps the target a check diagnostic names to the --lang
// spellings that generate it.
var nameRuleLangs = map[string][]string{
	"C": {"c"}, "C#": {"cs", "unity"}, "Goose": {"goose"}, "Java": {"java"},
	"Lobster": {"lobster"}, "Lua": {"lua"}, "MOO": {"moo"}, "Python": {"python"}, "Ruby": {"ruby"},
}

// TestEveryAcceptedCorpusRowGeneratesEverywhere: each row the checker accepts
// generates in every registered target. A target that refuses one by name
// at generate is a name the checker does not hold yet. It is minutes of
// every emitter over some two hundred rows, so it runs in the
// name-buildability nightly (make/name-buildability.mk), never in the fast
// lane's two minutes; the witness below is the fast lane's.
func TestEveryAcceptedCorpusRowGeneratesEverywhere(t *testing.T) {
	slowtest.Gate(t, "every target's generate over every accepted name-buildability row (the name-buildability nightly)")
	t.Parallel()
	c := New()
	targets := c.Targets()
	n := 0
	for _, r := range readNameCorpus(t) {
		if r.verdict != "builds" {
			continue
		}
		n++
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			u, err := c.Load(nameCorpusPaths(t, r.name))
			if err != nil {
				t.Fatalf("schema check refuses a builds row: %v", err)
			}
			for _, tg := range targets {
				if _, err := c.Generate(u, tg, Options{}); err != nil {
					t.Errorf("schema check accepts %s, but generate --lang %s refuses it: %v", r.name, tg, err)
				}
			}
		})
	}
	if n == 0 {
		t.Fatal("no builds rows")
	}
}

// TestGooseViewRowsMeetNoTableVariants: the Goose view names a vocabulary's
// rows _<name>VocabularyRows, so an enum fooView beside an enum foo, both in a
// table, generates (the table module's _fooViewVariants and the view's rows
// of foo were one name, refused at generate; name-buildability n_internal_21,
// 32, 44, 68). Reversed, with the view's suffix ViewVariants again, generate
// refuses _fooViewVariants and so does schema check.
func TestGooseViewRowsMeetNoTableVariants(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := "package probe\n\nenum foo\n{ A, B }\n\nenum fooView\n{ C, D }\n\ntable T\n{\n    a foo\n    b fooView\n}\n"
	path := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	c := New()
	u, err := c.Load([]string{path})
	if err != nil {
		t.Fatalf("schema check: %v", err)
	}
	files, err := c.Generate(u, "goose", Options{})
	if err != nil {
		t.Fatalf("generate --lang goose: %v", err)
	}
	var all strings.Builder
	for _, b := range files {
		all.Write(b)
	}
	for _, want := range []string{"let _fooVocabularyRows:", "let _fooViewVocabularyRows:", "let _fooViewVariants:"} {
		if !strings.Contains(all.String(), want) {
			t.Errorf("the Goose output declares no %s", want)
		}
	}
}

// TestEmitterNameRulesAreTheOnlyGuard is the reversed witness. Every row the
// checker refuses through an emitter's name rule is accepted without the
// rules, and then refused at generate by a target the diagnostic names: had
// the checker not lifted the refusal, the test above would meet it at
// generate. The rows include the C, C# and Goose refusals of this change
// (c_kind_Kind, e_unmanaged, tb_goose_declare).
func TestEmitterNameRulesAreTheOnlyGuard(t *testing.T) {
	t.Parallel()
	c := New()
	registered := c.Targets()
	seen := map[string]bool{}
	for _, r := range readNameCorpus(t) {
		if r.verdict != "refuse-at-check" {
			continue
		}
		paths := nameCorpusPaths(t, r.name)
		_, err := c.Load(paths)
		if err == nil {
			t.Errorf("%s: a refuse-at-check row is accepted", r.name)
			continue
		}
		if !strings.Contains(err.Error(), "target cannot express a name in this unit") {
			continue // refused by the checker's own rules, not an emitter's
		}
		u, err := c.LoadWithoutEmitterRules(paths)
		if err != nil {
			continue // the checker's own rules refuse it as well
		}
		refused := false
		for _, target := range strings.Split(r.names, ",") {
			target = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(target), "the "), " target")
			for _, lang := range nameRuleLangs[target] {
				if !slices.Contains(registered, lang) {
					continue // a leg this build does not register (moo is tagged)
				}
				if _, err := c.Generate(u, lang, Options{}); err != nil {
					refused = true
				}
			}
		}
		if !refused {
			t.Errorf("%s: without the emitter name rules no target %s refuses it at generate; the check's refusal names no generate refusal", r.name, r.names)
		}
		seen[r.name] = true
	}
	for _, want := range []string{"c_kind_Kind", "e_unmanaged", "tb_goose_declare"} {
		if !seen[want] {
			t.Errorf("witness row %s is not an emitter-rule refusal of the corpus", want)
		}
	}
}
