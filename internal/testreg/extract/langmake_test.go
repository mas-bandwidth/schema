package extract

import (
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// TestAPerLanguageRowCoversEachLanguagesGates: a per-language row whose
// inputs name each language's make gates covers every gate it names: a make
// target by its file, a negative control by its group. The plants, each red
// by name: a gate whose rule is gone, a gate another row already runs, a
// gate without a target, and the row removed (its checks drop out of the
// account: a coverage drop).
func TestAPerLanguageRowCoversEachLanguagesGates(t *testing.T) {
	t.Parallel()
	res := &Result{Items: []Item{
		{Kind: KindMakeTarget, Dir: "make/c.mk", Name: "c-gate-check", Source: "make/c.mk:1"},
		{Kind: KindMakeTarget, Dir: "make/lua.mk", Name: "lua-gate-check", Source: "make/lua.mk:1"},
		{Kind: KindNegativeControl, Dir: "(excluded)", Name: "c-gate-negative-control", Source: "make/negative-controls.json:1"},
	}}
	row := func(key, inputs string) testreg.Test {
		return testreg.Test{Key: key, Inputs: []byte(inputs)}
	}
	gated := row("lint.x.gate", `{"make":{"c":[{"file":"make/c.mk","target":"c-gate-check"},{"file":"make/c.mk","target":"c-gate-negative-control","group":"(excluded)"}],"lua":[{"file":"make/lua.mk","target":"lua-gate-check"}]}}`)
	good := []testreg.Test{gated}
	langs := []testreg.Lang{{Name: "c"}, {Name: "lua"}, {Name: "go"}}
	if p := Check(res, &testreg.Registry{Path: "r.jsonl", Langs: langs, Tests: good}, nil); len(p) != 0 {
		t.Fatalf("the exact account: %v", p)
	}
	for name, c := range map[string]struct {
		tests []testreg.Test
		says  string
	}{
		"a gate whose rule is gone": {append(slices.Clone(good), row("lint.y.gate", `{"make":{"go":[{"file":"make/go.mk","target":"go-gone-check"}]}}`)),
			"registry row lint.y.gate runs make-target make/go.mk go-gone-check, which the repository does not hold"},
		"a gate twice": {append(slices.Clone(good), row("lint.y.gate", `{"make":{"go":[{"file":"make/c.mk","target":"c-gate-check"}]}}`)),
			"lint.y.gate runs make c-gate-check, which lint.x.gate already runs"},
		"a gate without a target": {append(slices.Clone(good), row("lint.y.gate", `{"make":{"go":[{"file":"make/go.mk"}]}}`)),
			"lint.y.gate names a go make gate without a file and a target"},
		"a gate under no language": {append(slices.Clone(good), row("lint.y.gate", `{"make":{"cx":[{"file":"make/c.mk","target":"c-gate-check"}]}}`)),
			`lint.y.gate names make gates for "cx", which is no language of the registry`},
		"the row removed": {nil,
			"c-gate-check"},
	} {
		p := Check(res, &testreg.Registry{Path: "r.jsonl", Langs: langs, Tests: c.tests}, nil)
		if !strings.Contains(strings.Join(p, "\n"), c.says) {
			t.Errorf("%s: want %q in:\n%s", name, c.says, strings.Join(p, "\n"))
		}
	}
	// a language-free row's "make" object is not a per-language gate list
	free := row("tooling.x.gate", `{"make":{"c":[{"file":"make/c.mk","target":"c-gate-check"}]}}`)
	free.Scope = testreg.LangFree
	if p := Check(res, &testreg.Registry{Path: "r.jsonl", Tests: []testreg.Test{free}}, nil); !strings.Contains(strings.Join(p, "\n"), "c-gate-check") {
		t.Errorf("a language-free row covered a per-language gate: %v", p)
	}
}
