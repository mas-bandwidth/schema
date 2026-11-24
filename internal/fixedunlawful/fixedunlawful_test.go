package fixedunlawful

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
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
		if !c.Lawful && strings.Contains(dump, `"new_`+strings.TrimPrefix(c.File, "old_")+`"`) {
			t.Errorf("case %s: an unlawful pair's NEW side is never dumped", c.Name)
		}
		if !strings.Contains(docs, "`"+c.File+"`") {
			t.Errorf("case %s: docs/FIXED-FORM-VERSIONING-TESTS.md has no line for %s", c.Name, c.File)
		}
		if c.Records < 1 {
			t.Errorf("case %s: a corpus file holds at least one record", c.Name)
		}
	}
	controlValuesAreTheManifest(t)
}

// controlValuesAreTheManifest holds the LAWFUL control's Values, a Go
// constant every leg's verdict is judged against, to the reference dump's
// own manifest line for its file: "values=r0.lead=…,r0.v=…,r0.trail=…" is
// "lead=… v=… trail=…". A Values that drifts from the file is refused here,
// not discovered as fourteen legs failing at once.
func controlValuesAreTheManifest(t *testing.T) {
	t.Helper()
	path := filepath.Join("..", "..", "build", "fixedform-corpus", "manifest.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		if slowtest.CorpusRequired() {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set (or CI runs the slow half) and the reference's corpus is not built: %v", err)
		}
		t.Skip("the reference's corpus is not built: make tables-fixedform-corpus")
	}
	for _, c := range Cases {
		if !c.Lawful {
			continue
		}
		var line string
		for l := range strings.SplitSeq(string(data), "\n") {
			if strings.HasPrefix(l, "file="+c.File+" ") {
				line = l
			}
		}
		if line == "" {
			t.Errorf("control %s: the manifest has no line for %s", c.Name, c.File)
			continue
		}
		_, vals, ok := strings.Cut(line, " values=")
		if !ok || c.Records != 1 {
			t.Errorf("control %s: the manifest line names no values, or the control is not one record: %s", c.Name, line)
			continue
		}
		var parts []string
		for f := range strings.SplitSeq(vals, ",") {
			parts = append(parts, strings.TrimPrefix(f, "r0."))
		}
		if want := strings.Join(parts, " "); c.Values != want {
			t.Errorf("control %s: Values %q is not the manifest's %q (%s)", c.Name, c.Values, want, c.File)
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

// TestJudgeRefusesAHollowDriver is the control's own red: a driver that
// answers Refused without loading fails the LAWFUL control, a driver that
// answers nothing fails every case, a wrong value fails the control, and a
// load of an UNLAWFUL case fails it. The right verdicts pass.
func TestJudgeRefusesAHollowDriver(t *testing.T) {
	t.Parallel()
	controls := 0
	for _, c := range Cases {
		if why := Judge(c, c.Want()); why != "" {
			t.Errorf("case %s: its own Want was refused: %s", c.Name, why)
		}
		if why := Judge(c, ""); why == "" {
			t.Errorf("case %s: an empty verdict passed", c.Name)
		}
		if !c.Lawful {
			if why := Judge(c, Loaded(c.Records, "lead=1 trail=2")); why == "" {
				t.Errorf("case %s: an UNLAWFUL case that loaded passed", c.Name)
			}
			continue
		}
		controls++
		if why := Judge(c, Refused); !strings.Contains(why, "never loads cannot pass") {
			t.Errorf("case %s: a hollow driver answering refused passed the control: %q", c.Name, why)
		}
		if why := Judge(c, Loaded(c.Records, strings.Replace(c.Values, "v=65535", "v=0", 1))); why == "" {
			t.Errorf("case %s: a load with the wrong value passed the control", c.Name)
		}
		if c.Values == "" || c.Records < 1 {
			t.Errorf("case %s: the control names no values", c.Name)
		}
	}
	if controls != 1 {
		t.Errorf("the corpus holds %d LAWFUL controls, want exactly 1", controls)
	}
}
