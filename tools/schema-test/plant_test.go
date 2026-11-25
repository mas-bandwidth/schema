package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// memRead reads files held in memory by repository path.
func memRead(files map[string]string) func(string) ([]byte, error) {
	return func(p string) ([]byte, error) {
		s, ok := files[p]
		if !ok {
			return nil, errors.New("no such file")
		}
		return []byte(s), nil
	}
}

var plantSources = map[string]string{
	"internal/a/a.go":      "func reset() {\n\tclear(x) // the reset\n}\n",
	"internal/a/a_test.go": "// the test names it: clear(x) // the reset\n",
	"internal/b/b.go":      "func other() {}\n",
	"docs/A.md":            "clear(x) // the reset\n",
}

func plantFiles() []string {
	return []string{"internal/a/a.go", "internal/a/a_test.go", "internal/b/b.go", "docs/A.md"}
}

// TestAnAnchorThatDriftedFailsByName: an edit whose anchor is not in its
// file (0 matches) is a named failure, per edit, and nothing is written.
func TestAnAnchorThatDriftedFailsByName(t *testing.T) {
	t.Parallel()
	edits := []plantEdit{
		{File: "internal/a/a.go", Anchor: "clear(x) // the reset", With: "// SABOTAGED"},
		{File: "internal/b/b.go", Anchor: "func moved() {}", With: "func moved() { panic(1) }"},
	}
	_, err := applyEdits(edits, memRead(plantSources), plantFiles())
	var pe plantEditError
	if !asEditError(err, &pe) || !strings.Contains(pe.msg, "edit 2") || !strings.Contains(pe.msg, "0 matches") || !strings.Contains(pe.msg, "internal/b/b.go") {
		t.Fatalf("a drifted anchor: %v, want edit 2's 0 matches in internal/b/b.go", err)
	}
}

// TestAnAnchorWrittenTwiceFails: an anchor that matches twice in its file
// is a named failure: the plant would not know which one it sabotages.
func TestAnAnchorWrittenTwiceFails(t *testing.T) {
	t.Parallel()
	files := map[string]string{"internal/a/a.go": "x := 1\nx := 1\n"}
	_, err := applyEdits([]plantEdit{{File: "internal/a/a.go", Anchor: "x := 1", With: "x := 2"}}, memRead(files), []string{"internal/a/a.go"})
	var pe plantEditError
	if !asEditError(err, &pe) || !strings.Contains(pe.msg, "2 times") {
		t.Fatalf("a twice-written anchor: %v", err)
	}
	// once, it lands, and only in its own file
	out, err := applyEdits([]plantEdit{{File: "internal/a/a.go", Anchor: "clear(x) // the reset", With: "// SABOTAGED"}}, memRead(plantSources), plantFiles())
	if err != nil || len(out) != 1 || !strings.Contains(string(out["internal/a/a.go"]), "// SABOTAGED") || strings.Contains(string(out["internal/a/a.go"]), "clear(x)") {
		t.Fatalf("an exactly-once anchor: %v %q", err, out)
	}
}

// TestAPlantReachesEveryCopy (G21): the anchor also written in another
// source file of the same kind that no edit names fails the plant; one
// in a test, testdata or the docs is not a copy of the code; naming both
// copies lands.
func TestAPlantReachesEveryCopy(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"internal/a/a.go": "clear(x) // the reset\n",
		"internal/c/c.go": "clear(x) // the reset\n",
	}
	all := []string{"internal/a/a.go", "internal/c/c.go"}
	_, err := applyEdits([]plantEdit{{File: "internal/a/a.go", Anchor: "clear(x) // the reset", With: "// SABOTAGED"}}, memRead(files), all)
	var pe plantEditError
	if !asEditError(err, &pe) || !strings.Contains(pe.msg, "internal/c/c.go") || !strings.Contains(pe.msg, "every copy") {
		t.Fatalf("a second copy no edit reaches: %v", err)
	}
	if _, err := applyEdits([]plantEdit{
		{File: "internal/a/a.go", Anchor: "clear(x) // the reset", With: "// SABOTAGED"},
		{File: "internal/c/c.go", Anchor: "clear(x) // the reset", With: "// SABOTAGED"},
	}, memRead(files), all); err != nil {
		t.Fatalf("both copies named: %v", err)
	}
	if _, err := applyEdits([]plantEdit{{File: "internal/a/a.go", Anchor: "clear(x) // the reset", With: "// SABOTAGED"}}, memRead(plantSources), plantFiles()); err != nil {
		t.Fatalf("a mention in a test and in the docs is not a copy: %v", err)
	}
}

// TestASabotageThatReddensAnUnnamedRowFails: the planted run passes only as
// exactly its named rows red and every other row green.
func TestASabotageThatReddensAnUnnamedRowFails(t *testing.T) {
	t.Parallel()
	names := []string{"none.unit.tooling.f.a", "none.unit.tooling.f.b", "none.unit.tooling.f.c"}
	red := []string{"none.unit.tooling.f.a"}
	for _, c := range []struct {
		status map[string]string
		says   string
	}{
		{map[string]string{names[0]: "FAIL", names[1]: "PASS", names[2]: "PASS"}, ""},
		{map[string]string{names[0]: "FAIL", names[1]: "FAIL", names[2]: "PASS"}, "none.unit.tooling.f.b was FAIL, and the plant does not name it"},
		{map[string]string{names[0]: "PASS", names[1]: "PASS", names[2]: "PASS"}, "none.unit.tooling.f.a was PASS, and the plant names it red"},
		{map[string]string{names[0]: "MISSING", names[1]: "PASS", names[2]: "PASS"}, "none.unit.tooling.f.a was MISSING"},
		{map[string]string{names[0]: "FAIL", names[1]: "PASS"}, "none.unit.tooling.f.c did not report"},
	} {
		got := judgePlant(red, names, c.status)
		if c.says == "" && got != "" || c.says != "" && !strings.Contains(got, c.says) {
			t.Errorf("%v: %q, want %q", c.status, got, c.says)
		}
	}
}

// TestAnEditPlantIsJudgedOnItsTreesRun: the edit form hands its edits and
// names to its tree, a sabotage that did not land is a FAIL verdict (never
// a crash and never a pass), and the verdict is judgePlant's.
func TestAnEditPlantIsJudgedOnItsTreesRun(t *testing.T) {
	t.Parallel()
	reg, err := testreg.Load(cliRegistry)
	if err != nil {
		t.Fatal(err)
	}
	const target = "go.functional.roundtrip.fam.one"
	in, err := json.Marshal(plantInputs{Edits: []plantEdit{{File: "a.go", Anchor: "x", With: "y"}}, Red: []string{target}})
	if err != nil {
		t.Fatal(err)
	}
	inst := testreg.Instance{Full: "none.functional.plant.fam.edit", Lang: testreg.LangFree, Test: testreg.Test{Scope: testreg.LangFree, Type: "plant", Adapter: "plant", Inputs: in}}
	for _, c := range []struct {
		tree    func(context.Context, []plantEdit, []string) (map[string]string, string, error)
		verdict string
		says    string
	}{
		{func(_ context.Context, e []plantEdit, n []string) (map[string]string, string, error) {
			if len(e) != 1 || len(n) != 1 || n[0] != target {
				return nil, "", errors.New("the tree was handed the wrong plant")
			}
			return map[string]string{target: "FAIL"}, "", nil
		}, testreg.Pass, ""},
		{func(context.Context, []plantEdit, []string) (map[string]string, string, error) {
			return map[string]string{target: "PASS"}, "", nil
		}, testreg.Fail, "names it red"},
		{func(context.Context, []plantEdit, []string) (map[string]string, string, error) {
			return nil, "", plantEditError{"edit 1: the anchor is not in a.go (0 matches)"}
		}, testreg.Fail, "0 matches"},
	} {
		a := plantAdapter{rn: &testreg.Runner{Reg: reg}, tree: c.tree}
		if !a.Answers(inst.Test) {
			t.Fatal("the edit plant is not answered")
		}
		res, err := a.Run(context.Background(), testreg.LangFree, []testreg.Instance{inst})
		if err != nil || len(res) != 1 || res[0].Verdict != c.verdict || !strings.Contains(res[0].Reason, c.says) {
			t.Errorf("%v %+v, want %s saying %q", err, res, c.verdict, c.says)
		}
	}
}

// TestPlantInputsAreOneForm: a plant's inputs are exactly one of its three
// forms, each whole; anything else is not answered.
func TestPlantInputsAreOneForm(t *testing.T) {
	t.Parallel()
	for in, ok := range map[string]bool{
		`{"target":"go.functional.roundtrip.fam.one","env":{"B":"1"}}`:                               true,
		`{"edits":[{"file":"a.go","anchor":"x","with":"y"}],"red":["a"]}`:                            true,
		`{"make":"x-negative-control","file":"Makefile","group":"g"}`:                                true,
		`{"target":"go.functional.roundtrip.fam.one"}`:                                               false,
		`{"edits":[{"file":"a.go","anchor":"x","with":"x"}],"red":["a"]}`:                            false,
		`{"edits":[{"file":"../a.go","anchor":"x","with":"y"}],"red":["a"]}`:                         false,
		`{"edits":[{"file":"a.go","anchor":"x","with":"y"}]}`:                                        false,
		`{"make":"x-negative-control","group":"g"}`:                                                  false,
		`{"make":"x","file":"Makefile","group":"g","target":"go.functional.roundtrip.fam.one"}`:      false,
		`{"edits":[{"file":"a.go","anchor":"x","with":"y"}],"red":["a"],"env":{"B":"1"}}`:            false,
		`{"make":"x-negative-control","file":"Makefile","group":"g","flags":"-DNDEBUG"}`:             false,
		`{"edits":[{"file":"a.go","anchor":"x","with":"y","flags":"-O2"}],"red":["a"]}`:              false,
		`{"target":"go.functional.roundtrip.fam.one","env":{"B":"1"},"red":["go.functional.x.y.z"]}`: false,
	} {
		test := testreg.Test{Scope: testreg.LangFree, Type: "plant", Inputs: json.RawMessage(in)}
		if got := (plantAdapter{}).Answers(test); got != ok {
			t.Errorf("%s: answered %v, want %v", in, got, ok)
		}
	}
}

// TestParseOutcomesReadsTheRunsLines: the planted run's printed outcomes
// give each name's status; the counts line and run-level problems are not
// names.
func TestParseOutcomesReadsTheRunsLines(t *testing.T) {
	t.Parallel()
	log := "schema-test run a.b.c.d.e: 2 names\n  none.unit.tooling.f.a\nPASS none.unit.tooling.f.a\nFAIL none.unit.tooling.f.b: boom: at x\nFAIL run: expected 2\nschema-test FAIL: expected 2, ran 2\n"
	got := parseOutcomes(log)
	if len(got) != 2 || got["none.unit.tooling.f.a"] != "PASS" || got["none.unit.tooling.f.b"] != "FAIL" {
		t.Fatalf("%v", got)
	}
}
