package testreg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// GoTest names one language's Go test for a per-language row: the package
// it is in (./ relative to the repository root), its top-level name, and the
// build tags it needs (a leg's schema_leg_<leg>), if any.
type GoTest struct {
	Package string `json:"package"`
	Test    string `json:"test"`
	Tags    string `json:"tags,omitempty"`
}

var goTestFuncName = regexp.MustCompile(`^(Test|Fuzz)[A-Za-z0-9_]*$`)

// GoTestsOf reads a union row's inputs: {"tests": {"<lang>": GoTest, ...}},
// each language's own Go test for the one case the row is. A union family is
// a case set the languages' hand-written Go tests each answered in their own
// package; the row holds the case once and names, per language, the test that
// answers it. A language the map does not name has no test for the case,
// and its instance is red by name (never absent). ok is false for a row
// whose inputs carry no "tests" (another kind of row); err names a union row
// whose inputs do not read.
func GoTestsOf(t Test) (tests map[string]GoTest, ok bool, err error) {
	var probe map[string]json.RawMessage
	if json.Unmarshal(t.Inputs, &probe) != nil {
		return nil, false, nil
	}
	if _, has := probe["tests"]; !has {
		return nil, false, nil
	}
	var in struct {
		Tests map[string]GoTest `json:"tests"`
	}
	d := json.NewDecoder(bytes.NewReader(t.Inputs))
	d.DisallowUnknownFields()
	if err := d.Decode(&in); err != nil {
		return nil, true, fmt.Errorf("%s: union inputs: %w", t.Key, err)
	}
	if len(in.Tests) == 0 {
		return nil, true, fmt.Errorf("%s: union inputs name no language's Go test", t.Key)
	}
	for lang, g := range in.Tests {
		if !strings.HasPrefix(g.Package, "./") || !goTestFuncName.MatchString(g.Test) {
			return nil, true, fmt.Errorf("%s: union inputs for %s want a ./package and a Go test name, got %q %q", t.Key, lang, g.Package, g.Test)
		}
	}
	return in.Tests, true, nil
}
