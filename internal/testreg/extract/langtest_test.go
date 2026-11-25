package extract

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// TestAPerLanguageRowCoversItsTestForEveryLanguage: a per-language row whose
// inputs name a Go test with testreg.LangPlaceholder covers that test once for
// every language the registry lists, never for the language-free row; any
// number of case rows naming the same test cover it together. A test for a
// language the registry does not list stays uncovered, and so does every Go
// test when the inputs name no placeholder.
func TestAPerLanguageRowCoversItsTestForEveryLanguage(t *testing.T) {
	t.Parallel()
	row := func(key, inputs string) testreg.Test {
		return testreg.Test{Key: key, Type: "codegen", Family: "fam", Inputs: []byte(inputs)}
	}
	reg := &testreg.Registry{
		Path:  "r.jsonl",
		Langs: []testreg.Lang{{Name: "go"}, {Name: "rust"}, {Name: testreg.LangFree}},
		Tests: []testreg.Test{
			row("codegen.fam.a", `{"package":"./tools/p","test":"TestP_{lang}","subtest":"a"}`),
			row("codegen.fam.b", `{"package":"./tools/p","test":"TestP_{lang}","subtest":"b"}`),
			row("codegen.fam.plain", `{"package":"./tools/p","test":"TestPlain"}`),
		},
	}
	out, problems := Covered(reg)
	if len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}
	for name, want := range map[string]string{
		"TestP_go":   "codegen.fam.a",
		"TestP_rust": "codegen.fam.a",
		"TestP_none": "",
		"TestP_java": "",
		"TestPlain":  "",
	} {
		id := Item{Kind: KindGoTest, Dir: "tools/p", Name: name}.ID()
		if got := out[id]; got != want {
			t.Errorf("%s is covered by %q, want %q", name, got, want)
		}
	}
	if len(out) != 2 {
		t.Errorf("%d items covered, want 2: %v", len(out), out)
	}
}
