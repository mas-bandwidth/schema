package fixedunlawful

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoFile(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestEveryLegAnswers: every leg in Legs holds DriverFile, and the driver
// answers through Run under its own leg name, so the harness's refusal of a
// missing answer reaches every leg. A leg with no driver is a leg that answers
// nothing, and that is a failure here, never a skip.
func TestEveryLegAnswers(t *testing.T) {
	t.Parallel()
	for _, leg := range Legs {
		path := filepath.Join("..", "codegen", leg, DriverFile)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("leg %s answers no unlawful case: %v", leg, err)
			continue
		}
		src := string(data)
		if !strings.Contains(src, "fixedunlawful.Run(") || !strings.Contains(src, `"`+leg+`"`) {
			t.Errorf("leg %s: %s does not answer through fixedunlawful.Run(t, %q, ...)", leg, path, leg)
		}
	}
}

// TestEveryCaseIsInTheCorpus: each case is defined once, here, and its two
// schemas, its generation line, its dump line and its docs row exist.
func TestEveryCaseIsInTheCorpus(t *testing.T) {
	t.Parallel()
	dump := repoFile(t, "test/tables/fixedform_dump.cpp")
	makefile := repoFile(t, "Makefile")
	docs := repoFile(t, "docs/FIXED-FORM-VERSIONING-TESTS.md")
	seen := map[string]bool{}
	for _, c := range Cases {
		if seen[c.Name] {
			t.Errorf("case %s is defined twice", c.Name)
		}
		seen[c.Name] = true
		for _, base := range []string{c.Old, c.New} {
			src := repoFile(t, filepath.Join("test", "tables", base+".schema"))
			if !strings.Contains(src, "fixed table "+c.Root) {
				t.Errorf("case %s: %s declares no fixed table %s", c.Name, base, c.Root)
			}
			if !strings.Contains(makefile, "test/tables/"+base+".schema") {
				t.Errorf("case %s: the Makefile generates no %s", c.Name, base)
			}
		}
		if !strings.Contains(dump, `"`+c.File+`"`) {
			t.Errorf("case %s: fixedform_dump.cpp writes no %s", c.Name, c.File)
		}
		if strings.Contains(dump, `"new_`+strings.TrimPrefix(c.File, "old_")+`"`) {
			t.Errorf("case %s: an unlawful pair's NEW side is never dumped", c.Name)
		}
		if !strings.Contains(docs, "`"+c.File+"`") {
			t.Errorf("case %s: docs/FIXED-FORM-VERSIONING-TESTS.md has no line for %s", c.Name, c.File)
		}
		if c.Records < 1 {
			t.Errorf("case %s: a corpus file holds at least one record", c.Name)
		}
	}
}

// TestUnansweredRefuses is the harness's own red: a missing answer, a stray
// answer and an unlisted leg each refuse by name, and a whole answer passes.
func TestUnansweredRefuses(t *testing.T) {
	t.Parallel()
	whole := map[string]int{}
	for _, c := range Cases {
		whole[c.Name] = 1
	}
	if why := Unanswered("gotable", whole); why != "" {
		t.Fatalf("a whole answer refused: %s", why)
	}
	short := map[string]int{}
	for _, c := range Cases[1:] {
		short[c.Name] = 1
	}
	if why := Unanswered("gotable", short); !strings.Contains(why, "missing ["+Cases[0].Name+"]") {
		t.Fatalf("a missing answer was not refused by name: %q", why)
	}
	stray := map[string]int{"no_such_row": 1}
	for _, c := range Cases {
		stray[c.Name] = 1
	}
	if why := Unanswered("gotable", stray); !strings.Contains(why, "no_such_row") {
		t.Fatalf("a stray answer was not refused by name: %q", why)
	}
	if why := Unanswered("nosuchtable", whole); !strings.Contains(why, "not in fixedunlawful.Legs") {
		t.Fatalf("an unlisted leg was not refused: %q", why)
	}
}
