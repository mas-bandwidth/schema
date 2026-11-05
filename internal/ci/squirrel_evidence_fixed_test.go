package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
)

const (
	squirrelFixedOverlay   = "docs/roadmap-evidence/fixed-squirrel.sexp"
	squirrelFixedSuiteFile = "internal/codegen/squirreltable/fixed_proof_suite_test.go"
	squirrelFixedIDPrefix  = "port/squirrel/fixed/"
	squirrelFixedValues    = "TestSquirrelFixedProofSuiteValues"
	squirrelFixedNegative  = "TestSquirrelFixedProofSuiteNegativeControl"
)

// squirrelFixedCutRows are the eight fixed rows the strict-only cut rewrites;
// they are recorded by their own card and are never marked done here.
var squirrelFixedCutRows = []string{
	"numeric-evolution",
	"reports",
	"array-bounds",
	"text",
	"scalar-bounds",
	"ordinals",
	"normalization",
	"hostile-input",
}

// TestSquirrelEvidenceFixedRows verifies docs/roadmap-evidence/fixed-squirrel.sexp:
// one row per fixed-proof case (49), each citing its own Values and
// NegativeControl subtest of the suite file, with a 40-hex tested revision and
// a note, and none of the eight cut rows marked done.
func TestSquirrelEvidenceFixedRows(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	overlayBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(squirrelFixedOverlay)))
	if err != nil {
		t.Fatalf("read %s: %v", squirrelFixedOverlay, err)
	}
	overlay := string(overlayBytes)
	if err := validateSquirrelFixedOverlay(root, overlay); err != nil {
		t.Fatal(err)
	}

	// Negative controls: each defect below must be refused.
	firstRow := squirrelFixedIDPrefix + fixedproof.Cases()[0].ID
	controls := []struct {
		name string
		text string
	}{
		{"missing row", dropOverlayRow(overlay, firstRow)},
		{"unknown subtest", strings.Replace(overlay, squirrelFixedValues+"/"+fixedproof.Cases()[0].ID, squirrelFixedValues+"/no-such-row", 1)},
		{"blanket citation", strings.Replace(overlay, squirrelFixedNegative+"/"+fixedproof.Cases()[0].ID, squirrelFixedNegative, 1)},
		{"cut row done", overlay + "(:id \"" + squirrelFixedIDPrefix + squirrelFixedCutRows[0] + "\" :state :done :evidence (\"" + squirrelFixedSuiteFile + ": " + squirrelFixedValues + "/" + squirrelFixedCutRows[0] + "\") :tested-revision \"" + strings.Repeat("a", 40) + "\" :note \"x\")\n"},
		{"short revision", strings.Replace(overlay, ":tested-revision \"", ":tested-revision \"zz", 1)},
	}
	for _, c := range controls {
		if err := validateSquirrelFixedOverlay(root, c.text); err == nil {
			t.Fatalf("negative control %q: validator accepted the defect", c.name)
		}
	}
}

func dropOverlayRow(overlay, id string) string {
	var keep []string
	for line := range strings.SplitSeq(overlay, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "(:id \""+id+"\"") {
			continue
		}
		keep = append(keep, line)
	}
	return strings.Join(keep, "\n")
}

func validateSquirrelFixedOverlay(root, overlay string) error {
	suiteSrc, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(squirrelFixedSuiteFile)))
	if err != nil {
		return fmt.Errorf("read %s: %v", squirrelFixedSuiteFile, err)
	}
	defined := map[string]bool{}
	for _, m := range squirrelGoTestFuncRx.FindAllStringSubmatch(string(suiteSrc), -1) {
		defined[m[1]] = true
	}
	for _, name := range []string{squirrelFixedValues, squirrelFixedNegative} {
		if !defined[name] {
			return fmt.Errorf("%s does not define %s", squirrelFixedSuiteFile, name)
		}
	}

	suiteRows := map[string]bool{}
	for _, c := range fixedproof.Cases() {
		suiteRows[c.ID] = true
	}
	if len(suiteRows) != 49 {
		return fmt.Errorf("fixed proof suite has %d rows, want 49", len(suiteRows))
	}
	for _, cut := range squirrelFixedCutRows {
		if suiteRows[cut] {
			return fmt.Errorf("cut row %s is part of the 49-row suite", cut)
		}
	}

	rows := map[string]string{}
	for line := range strings.SplitSeq(overlay, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, ";") {
			continue
		}
		m := squirrelOverlayIDRx.FindStringSubmatch(trimmed)
		if m == nil {
			return fmt.Errorf("malformed overlay line: %s", trimmed)
		}
		if _, dup := rows[m[1]]; dup {
			return fmt.Errorf("duplicate overlay row %s", m[1])
		}
		rows[m[1]] = trimmed
	}

	for _, cut := range squirrelFixedCutRows {
		if line, ok := rows[squirrelFixedIDPrefix+cut]; ok {
			if s := squirrelOverlayStateRx.FindStringSubmatch(line); s != nil && s[1] == "done" {
				return fmt.Errorf("cut row %s is marked done", cut)
			}
		}
	}

	for id := range rows {
		row, ok := strings.CutPrefix(id, squirrelFixedIDPrefix)
		if !ok || !suiteRows[row] {
			return fmt.Errorf("overlay row %s is not one of the 49 suite rows", id)
		}
	}

	for _, c := range fixedproof.Cases() {
		id := squirrelFixedIDPrefix + c.ID
		line, ok := rows[id]
		if !ok {
			return fmt.Errorf("missing overlay row %s", id)
		}
		stateMatch := squirrelOverlayStateRx.FindStringSubmatch(line)
		if stateMatch == nil {
			return fmt.Errorf("%s: missing :state", id)
		}
		switch stateMatch[1] {
		case "todo":
			if n := squirrelOverlayNoteRx.FindStringSubmatch(line); n == nil || strings.TrimSpace(n[1]) == "" {
				return fmt.Errorf("%s: :todo row must carry a :note saying which subtest was not run", id)
			}
			continue
		case "done":
		default:
			return fmt.Errorf("%s: unexpected :state :%s", id, stateMatch[1])
		}
		rev := squirrelOverlayRevRx.FindStringSubmatch(line)
		if rev == nil || !isFortyHexSHA(rev[1]) {
			return fmt.Errorf("%s: :done row must carry a 40-hex :tested-revision", id)
		}
		if n := squirrelOverlayNoteRx.FindStringSubmatch(line); n == nil || strings.TrimSpace(n[1]) == "" {
			return fmt.Errorf("%s: :done row must carry a :note", id)
		}
		want := []string{
			squirrelFixedSuiteFile + ": " + squirrelFixedValues + "/" + c.ID,
			squirrelFixedSuiteFile + ": " + squirrelFixedNegative + "/" + c.ID,
		}
		got := extractSquirrelEvidenceStrings(line)
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			return fmt.Errorf("%s: evidence = %q, want %q", id, got, want)
		}
	}
	return nil
}
