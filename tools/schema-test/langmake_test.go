package main

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

const langMakeRules = `c-gate-check:
	@true
c-gate-negative-control:
	@true
lua-gate-check:
	@true
`

const langMakeInputs = `{"make":{"c":[{"file":"make/x.mk","target":"c-gate-check"},{"file":"make/x.mk","target":"c-gate-negative-control","group":"g1"}],"lua":[{"file":"make/x.mk","target":"lua-gate-check","vars":{"LUA":"lua5.4"}}]}}`

func langMakeInst(lang, inputs string) testreg.Instance {
	return testreg.Instance{Full: lang + ".functional.lint.x.gate", Lang: lang,
		Test: testreg.Test{Key: "lint.x.gate", Family: "x", Inputs: []byte(inputs)}}
}

// TestAPerLanguageRowRunsItsLanguagesMakeGates: the corpus adapter answers
// a per-language row whose inputs name each language's make gates; a
// language's instance runs its gates in order (a negative control through
// the controls adapter, with no -j) and passes only when every gate passes.
// The plants, each red by name: a gate that fails, a gate whose rule is
// gone, a language the row names no gate for (it owes the test), and
// inputs that name a gate without a target.
func TestAPerLanguageRowRunsItsLanguagesMakeGates(t *testing.T) {
	t.Parallel()
	type run = struct {
		log string
		err error
	}
	adapter := func(runs map[string]run) (*harnessAdapter, *[]string, *[]string) {
		var made, controlled []string
		a := &harnessAdapter{mk: fakeMake("make/x.mk", langMakeRules, runs, &made), controls: fakeMake("make/x.mk", langMakeRules, runs, &controlled)}
		return a, &made, &controlled
	}
	a, made, controlled := adapter(nil)
	if !a.Answers(langMakeInst("c", langMakeInputs).Test) {
		t.Fatal("the corpus adapter does not answer a per-language make row")
	}
	res, err := a.Run(context.Background(), "c", []testreg.Instance{langMakeInst("c", langMakeInputs)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Verdict != testreg.Pass {
		t.Fatalf("c's gates all pass, and the instance is %+v", res)
	}
	if !slices.Equal(*made, []string{"c-gate-check"}) || !slices.Equal(*controlled, []string{"c-gate-negative-control"}) {
		t.Errorf("c ran %v through make and %v through the controls, want [c-gate-check] and [c-gate-negative-control]", *made, *controlled)
	}

	for name, c := range map[string]struct {
		lang, inputs string
		runs         map[string]run
		says         string
	}{
		"a gate that fails": {"c", langMakeInputs, map[string]run{"c-gate-negative-control": {"NEGATIVE CONTROL FAILED", errors.New("exit status 2")}},
			"make c-gate-negative-control: exit status 2"},
		"a gate whose rule is gone": {"lua", strings.Replace(langMakeInputs, "lua-gate-check", "lua-gone-check", 1), nil,
			`writes no rule "lua-gone-check"`},
		"a language with no gate": {"go", langMakeInputs, nil,
			"the row names no make gate for go (only c, lua): go fails this test until the row gives it a gate"},
	} {
		a, _, _ := adapter(c.runs)
		res, err := a.Run(context.Background(), c.lang, []testreg.Instance{langMakeInst(c.lang, c.inputs)})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(res) != 1 || res[0].Verdict != testreg.Fail || !strings.Contains(res[0].Reason, c.says) {
			t.Errorf("%s: want a FAIL saying %q, got %+v", name, c.says, res)
		}
	}

	// inputs that do not read are refused, never answered
	bad := langMakeInst("c", `{"make":{"c":[{"file":"make/x.mk"}]}}`)
	if a.Answers(bad.Test) {
		t.Error("a gate without a target is answered")
	}
	if _, err := a.Run(context.Background(), "c", []testreg.Instance{bad}); err == nil || !strings.Contains(err.Error(), "want a make file and one target") {
		t.Errorf("a gate without a target ran: %v", err)
	}
	// a plant's make control names its target as a string: not a gate list
	if _, has, _ := langMakeGates(testreg.Test{Inputs: []byte(`{"make":"x-negative-control","file":"make/x.mk","group":"g"}`)}); has {
		t.Error("a plant's make control reads as per-language gates")
	}
	// a language-free row is never a per-language make row
	if _, has, _ := langMakeGates(testreg.Test{Scope: testreg.LangFree, Inputs: []byte(langMakeInputs)}); has {
		t.Error("a language-free row reads as per-language gates")
	}
}
