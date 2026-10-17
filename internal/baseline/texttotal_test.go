package baseline_test

// A TEXT LIST's TOTAL in the baseline (docs/SPEC-TABLES.md §2.13, §18.1,
// §18.2): the field line carries `total=N`, and the total is an extent like a
// bound — a reader keeps the prefix within its own total and counts clamped —
// so a lowered or newly declared total warns and a raised or removed one
// passes. Each case carries this package's two controls: DISCRIMINATION (the
// same field edited in the direction the wire absorbs) and ATTRIBUTION (the
// same edit re-judged with the `total` row removed, which must go quiet).

import (
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
)

const totalFixtureSrc = `package fixture

table Handover
{
    rules [..16]string(64) | total = 256
    repos []?string(64)
}
`

func totalDiff(t *testing.T, base, live string, policy map[string]baseline.TokenRule) []baseline.Finding {
	t.Helper()
	return baseline.Diff(committed(t, base), baseline.Render(unit(t, live)), policy)
}

// TestTextTotalRendersOnTheFieldLine: `total=N` on the list's own line, and
// no `total=` on a list that declares none.
func TestTextTotalRendersOnTheFieldLine(t *testing.T) {
	t.Parallel()
	text := baseline.Render(unit(t, totalFixtureSrc)).Text()
	var rules, repos string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "field rules "):
			rules = line
		case strings.HasPrefix(line, "field repos "):
			repos = line
		}
	}
	if !strings.HasSuffix(rules, " total=256") {
		t.Errorf("the text list's line does not end with its total=256: %q\n%s", rules, text)
	}
	if repos == "" || strings.Contains(repos, "total=") {
		t.Errorf("a text list with no total carries a total= token, or is missing: %q", repos)
	}
	for _, tbl := range baseline.Render(unit(t, totalFixtureSrc)).Tables {
		for _, f := range tbl.Fields {
			for _, tok := range f.Tokens {
				if _, ok := baseline.DefaultTokenPolicy[tok.Key]; !ok {
					t.Errorf("%s.%s carries the token %q and DefaultTokenPolicy has no row for it", tbl.Name, f.Name, tok.Key)
				}
			}
		}
	}
}

// TestTextTotalLoweredWarns: a lowered total drops stored elements past it
// and counts clamped, so it warns, naming what is lost.
func TestTextTotalLoweredWarns(t *testing.T) {
	t.Parallel()
	lowered := editOf(t, totalFixtureSrc, "| total = 256", "| total = 128")
	fs := totalDiff(t, totalFixtureSrc, lowered, baseline.DefaultTokenPolicy)
	if !find(fs, baseline.Warn, "Handover.rules", "text list total 256 -> 128") {
		t.Errorf("a lowered total is not warned on the list's line: %s", summary(fs))
	}
	if !find(fs, baseline.Warn, "Handover.rules", "drops the rest and counts clamped") {
		t.Errorf("the warning does not say what a lowered total costs: %s", summary(fs))
	}
	// ATTRIBUTION: the total row alone carries the verdict
	if fs := totalDiff(t, totalFixtureSrc, lowered, without("total")); len(fs) != 0 {
		t.Errorf("with the total row dropped the edit still reports: %s", summary(fs))
	}
	// DISCRIMINATION: a RAISED total loses nothing
	raised := editOf(t, totalFixtureSrc, "| total = 256", "| total = 512")
	if fs := totalDiff(t, totalFixtureSrc, raised, baseline.DefaultTokenPolicy); len(fs) != 0 {
		t.Errorf("a raised total is not silent: %s", summary(fs))
	}
}

// TestTextTotalAddedWarns: a total declared where the list had none is a
// tightening from the count times M onto N, so it warns; removing one passes.
func TestTextTotalAddedWarns(t *testing.T) {
	t.Parallel()
	added := editOf(t, totalFixtureSrc, "repos []?string(64)\n", "repos []?string(64) | total = 4096\n")
	fs := totalDiff(t, totalFixtureSrc, added, baseline.DefaultTokenPolicy)
	if !find(fs, baseline.Warn, "Handover.repos", "text list total 4096 added") {
		t.Errorf("a newly declared total is not warned on the list's line: %s", summary(fs))
	}
	// ATTRIBUTION
	if fs := totalDiff(t, totalFixtureSrc, added, without("total")); len(fs) != 0 {
		t.Errorf("with the total row dropped the addition still reports: %s", summary(fs))
	}
	// DISCRIMINATION: a REMOVED total loses nothing
	removed := editOf(t, totalFixtureSrc, " | total = 256", "")
	if fs := totalDiff(t, totalFixtureSrc, removed, baseline.DefaultTokenPolicy); len(fs) != 0 {
		t.Errorf("a removed total is not silent: %s", summary(fs))
	}
	if _, ok := maps.Clone(baseline.DefaultTokenPolicy)["total"]; !ok {
		t.Error("DefaultTokenPolicy has no total row")
	}
}
