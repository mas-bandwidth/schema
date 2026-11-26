package testreg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strings"
)

// GoTest names one language's Go test for a per-language row: the package
// it is in (./ relative to the repository root), its top-level name, the
// build tags it needs (a leg's schema_leg_<leg>), if any, and the seconds it
// measured, if it was measured: a union row's languages answer with their
// own tests, which take their own time, so a union row's seconds are each
// language's, never the row's (Weight). Tier, when set, is the tier of this
// language's instance in place of the row's: a test that measured past one
// shard's budget on its own runs nightly, while the languages whose tests
// fit stay per pull request (Test.TierIn).
type GoTest struct {
	Package string  `json:"package"`
	Test    string  `json:"test"`
	Tags    string  `json:"tags,omitempty"`
	Seconds float64 `json:"seconds,omitempty"`
	Tier    string  `json:"tier,omitempty"`
}

// TierIn is the tier of t's instance in lang: the row's, or, for a union
// row whose Go test for lang names a tier of its own, that one.
func (t Test) TierIn(lang string) string {
	if tier := unionTier(t, lang); tier != "" {
		return tier
	}
	return t.Tier
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
		// Reads names the data the case's tests read: the extract credits
		// it to the row (extract.CoveredIn); the run needs nothing of it.
		Reads []string `json:"reads"`
		// ReadPlant is the read plant that proves what Reads credits.
		ReadPlant string `json:"read_plant"`
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
		if g.Seconds < 0 {
			return nil, true, fmt.Errorf("%s: union inputs for %s give seconds %v: a measured run time is not negative", t.Key, lang, g.Seconds)
		}
		if !strings.HasPrefix(g.Package, "./") || !goTestFuncName.MatchString(g.Test) {
			return nil, true, fmt.Errorf("%s: union inputs for %s want a ./package and a Go test name, got %q %q", t.Key, lang, g.Package, g.Test)
		}
		// one spelling per package, so the loader sees a Go test two rows
		// name however they write it
		if rest := strings.TrimPrefix(g.Package, "./"); path.Clean(rest) != rest || rest == ".." || strings.HasPrefix(rest, "../") {
			return nil, true, fmt.Errorf("%s: union inputs for %s name the package %q, want it written ./%s", t.Key, lang, g.Package, path.Clean(rest))
		}
	}
	return in.Tests, true, nil
}
