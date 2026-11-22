package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/ci/hostilecorpus"
)

// TestHostileTargetsGradeScorerOutcomes pins that every port's hostile target
// grades the shared scorer outcome cases (hostilecorpus.ScorerOutcomesPath)
// through the comparison its corpus run uses: a refusal conforms on its
// verdict alone (docs/rulings.json refusal-verdict-only), so a runner that
// went back to comparing reasons, or dropped the cases, is red by name. C and
// C++ grade through tools/hostilecorpusexec, whose own test reads the fixture.
func TestHostileTargetsGradeScorerOutcomes(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	outcomes, err := hostilecorpus.LoadScorerOutcomes(root)
	if err != nil {
		t.Fatal(err)
	}
	// the fixture keeps both halves: a refusal that names another reason (or
	// none) passes, and a changed verdict fails
	var reasonOnly, verdictFlip bool
	for _, o := range outcomes {
		name, refused := o.Refused()
		if refused && o.Verdict == hostilecorpus.Refuse && name != o.Reason && o.Pass {
			reasonOnly = true
		}
		if refused && o.Verdict == hostilecorpus.Accept && !o.Pass {
			verdictFlip = true
		}
	}
	if !reasonOnly || !verdictFlip {
		t.Errorf("%s lost a half: a passing refusal under another reason (%t), a failing changed verdict (%t)",
			hostilecorpus.ScorerOutcomesPath, reasonOnly, verdictFlip)
	}
	for file, want := range map[string]string{
		"make/schema-cs-runs-hostile-corpus.mk":       "cshostile.dll --scorer " + hostilecorpus.ScorerOutcomesPath,
		"make/schema-unity-runs-hostile-corpus.mk":    "unityhostile.dll --scorer " + hostilecorpus.ScorerOutcomesPath,
		"make/schema-dart-runs-hostile-corpus.mk":     "runner.dart --scorer " + hostilecorpus.ScorerOutcomesPath,
		"make/schema-elixir-runs-hostile-corpus.mk":   "runner.exs --scorer " + hostilecorpus.ScorerOutcomesPath,
		"make/schema-java-runs-hostile-corpus.mk":     "Runner --scorer " + hostilecorpus.ScorerOutcomesPath,
		"make/schema-js-runs-hostile-corpus.mk":       "runner.mjs scorer " + hostilecorpus.ScorerOutcomesPath,
		"make/schema-rust-runs-hostile-corpus.mk":     "rust-hostile --scorer " + hostilecorpus.ScorerOutcomesPath,
		"make/schema-go-runs-hostile-corpus.mk":       "runner scorer " + hostilecorpus.ScorerOutcomesPath,
		"make/goose-fuzz.mk":                          "goose-hostile.sh --scorer " + hostilecorpus.ScorerOutcomesPath,
		"make/schema-lua-runs-hostile-corpus.mk":      "TestSchemaLuaHostileScorerOutcomes",
		"make/schema-luau-runs-hostile-corpus.mk":     "TestLuauHostileScorerOutcomes",
		"make/schema-moo-runs-hostile-corpus.mk":      "TestSchemaMooHostileScorerOutcomes",
		"make/schema-ruby-runs-hostile-corpus.mk":     "TestSchemaRubyHostileScorerOutcomes",
		"make/schema-squirrel-runs-hostile-corpus.mk": "TestSquirrelHostileScorerOutcomes",
		"make/python-hostile.mk":                      "TestPythonHostileScorerOutcomes",
		"make/lobster-hostile.mk":                     "TestScorerOutcomes",
	} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), want) {
			t.Errorf("%s does not grade the scorer outcome cases: it carries no %q", file, want)
		}
	}
}
