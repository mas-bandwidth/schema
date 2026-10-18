package compiler

// TestSpecGoS16S17TextForm pins the §16 and §17 text-form rules this card
// folded into docs/SPEC-TABLES.md from Go: trailing data, the 128 depth cap,
// the writer's -1 refusals, the union and flags rows, the duplicate-key cap,
// the writer's escapes, timestamps, the empty json key, the text-form limits
// and the arena-edge one-file rule. Each row names the literal the page now
// holds; a stale row names the sentence it replaced, which the page must no
// longer hold.
//
// One row's BOM half does not hold at BASE — Go skips a leading BOM
// (internal/codegen/gotable/json.go, byteOrderMark) and §16.2 already states
// that — so it has no assertion here.

import (
	"os"
	"strings"
	"testing"
)

func TestSpecGoS16S17TextForm(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../docs/SPEC-TABLES.md")
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)

	present := []struct {
		row  string
		want string
	}{
		{"§16.1 trailing data", "data after the root object is `malformed`"},
		{"§16.1 refused FromJson", "the instance is reset to its declared defaults (§6.5)"},
		{"§16.2 depth cap", "The cap is 128"},
		{"§16.3 writer refusals", "would hold no field"},
		{"§16.2 union row", "or `None` as the key, is skipped and counted `unknown`"},
		{"§16.2 flags row", "a flags element that is not a string is `kind_mismatch`"},
		{"§16.2 duplicate-key cap", "fields at an index"},
		{"§16.1/§16.3 writer escapes", "seven short escapes"},
		{"§16.2 timestamp", "timestamp_out_of_range"},
		{"§16.4 empty json key", "`json = \"\"`"},
		{"§16.9.4 limits", "negative limit takes the contract defaults"},
		{"§16.7/§17.2 one-file root", "NeedsLabels"},
	}
	for _, c := range present {
		if !strings.Contains(page, c.want) {
			t.Errorf("%s: docs/SPEC-TABLES.md does not hold %q", c.row, c.want)
		}
	}

	absent := []struct {
		row   string
		stale string
	}{
		{"§16.1 refused FromJson", "the instance holds what was placed before the stop"},
		{"§16.9.4 limits", "Callers must supply finite resource limits"},
		{"§17.2 one-file root", "a VARIABLE root is that one file and nothing else"},
	}
	for _, c := range absent {
		if strings.Contains(page, c.stale) {
			t.Errorf("%s: docs/SPEC-TABLES.md still holds the stale sentence %q", c.row, c.stale)
		}
	}
}
