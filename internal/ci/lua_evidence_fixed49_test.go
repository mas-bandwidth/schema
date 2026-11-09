package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
)

const (
	luaFixed49Overlay   = "docs/roadmap-evidence/lua.sexp"
	luaFixed49SuiteFile = "internal/codegen/luatable/fixed_proof_suite_test.go"
	luaFixed49IDPrefix  = "port/lua/fixed/"
	luaFixed49Values    = "TestLuaFixedProofSuiteValues"
	luaFixed49Negative  = "TestLuaFixedProofSuiteNegativeControl"
	luaFixed49Evolution = "TestLuaFixedProofEvolution"
	luaFixedCutProof    = "internal/codegen/luatable/fixed_evolution_proof_test.go: TestLuaFixedEvolution"
)

// luaFixed49CutRows are the eight fixed rows the cut rewrites; they are
// recorded by their own card (lua-4), each citing its own per-row proof in
// luaFixedCutProofFile and never the 49-row suite.
var luaFixed49CutRows = []string{
	"numeric-evolution",
	"reports",
	"array-bounds",
	"text",
	"scalar-bounds",
	"ordinals",
	"normalization",
	"hostile-input",
}

// luaFixed49EvolutionRows are the 16 non-value rows of fixedproof.Cases():
// fixed-file-roundtrip and the 15 evolution/layout/plan/lineage/file rows.
// They cite the evolution suite rather than the value suite.
var luaFixed49EvolutionRows = map[string]bool{
	"fixed-file-roundtrip": true,
	"file-envelope":        true,
	"batch-capacity":       true,
	"plan-selection":       true,
	"compiled-plans":       true,
	"definition-hash":      true,
	"fixed-closure":        true,
	"retirement":           true,
	"retired-runtime":      true,
	"optional-values":      true,
	"field-evolution":      true,
	"live-extents":         true,
	"writing":              true,
	"union-guards":         true,
	"plan-execution":       true,
	"interoperability":     true,
}

// TestLuaEvidenceFixed49Rows verifies the 49 untouched fixed rows appended
// to docs/roadmap-evidence/lua.sexp: exactly the 49 rows of
// fixedproof.Cases() are present as :done entries, none of the eight cut
// rows is present, and each entry cites exactly its own value (or, for the
// 16 non-value rows, evolution) subtest and its negative-control subtest,
// both naming rows that exist in fixedproof.Cases().
func TestLuaEvidenceFixed49Rows(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	overlayBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(luaFixed49Overlay)))
	if err != nil {
		t.Fatalf("read %s: %v", luaFixed49Overlay, err)
	}
	overlay := string(overlayBytes)
	if err := validateLuaFixed49Overlay(root, overlay); err != nil {
		t.Fatal(err)
	}

	// Negative controls: each defect below must be refused.
	firstRow := luaFixed49IDPrefix + fixedproof.Cases()[0].ID
	controls := []struct {
		name string
		text string
	}{
		{"missing row", dropLuaFixed49Row(overlay, firstRow)},
		{"unknown subtest", strings.Replace(overlay, luaFixed49Values+"/"+fixedproof.Cases()[0].ID, luaFixed49Values+"/no-such-row", 1)},
		{"blanket citation", strings.Replace(overlay, luaFixed49Negative+"/"+fixedproof.Cases()[0].ID, luaFixed49Negative, 1)},
		{"cut row done", overlay + "(:id \"" + luaFixed49IDPrefix + luaFixed49CutRows[0] + "\" :state :done :evidence (\"" + luaFixed49SuiteFile + ": " + luaFixed49Values + "/" + luaFixed49CutRows[0] + "\") :tested-revision \"" + strings.Repeat("a", 40) + "\" :note \"x\")\n"},
		{"short revision", strings.Replace(overlay, ":tested-revision \"", ":tested-revision \"zz", 1)},
	}
	for _, c := range controls {
		if c.text == overlay {
			t.Fatalf("negative control %q changed nothing", c.name)
		}
		if err := validateLuaFixed49Overlay(root, c.text); err == nil {
			t.Fatalf("negative control %q: validator accepted the defect", c.name)
		}
	}
}

func dropLuaFixed49Row(overlay, id string) string {
	var keep []string
	for line := range strings.SplitSeq(overlay, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "(:id \""+id+"\"") {
			continue
		}
		keep = append(keep, line)
	}
	return strings.Join(keep, "\n")
}

func validateLuaFixed49Overlay(root, overlay string) error {
	suiteSrc, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(luaFixed49SuiteFile)))
	if err != nil {
		return fmt.Errorf("read %s: %w", luaFixed49SuiteFile, err)
	}
	defined := map[string]bool{}
	for _, m := range squirrelGoTestFuncRx.FindAllStringSubmatch(string(suiteSrc), -1) {
		defined[m[1]] = true
	}
	for _, name := range []string{luaFixed49Values, luaFixed49Negative, luaFixed49Evolution} {
		if !defined[name] {
			return fmt.Errorf("%s does not define %s", luaFixed49SuiteFile, name)
		}
	}

	suiteRows := map[string]bool{}
	for _, c := range fixedproof.Cases() {
		suiteRows[c.ID] = true
	}
	if len(suiteRows) != 49 {
		return fmt.Errorf("fixed proof suite has %d rows, want 49", len(suiteRows))
	}
	for _, cut := range luaFixed49CutRows {
		if suiteRows[cut] {
			return fmt.Errorf("cut row %s is part of the 49-row suite", cut)
		}
	}
	if len(luaFixed49EvolutionRows) != 16 {
		return fmt.Errorf("evolution row set has %d rows, want 16", len(luaFixed49EvolutionRows))
	}
	for row := range luaFixed49EvolutionRows {
		if !suiteRows[row] {
			return fmt.Errorf("evolution row %s is not one of the 49 suite rows", row)
		}
	}

	rows := map[string]string{}
	seenFixed := map[string]bool{}
	var fixedIDs []string
	for line := range strings.SplitSeq(overlay, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, ";") {
			continue
		}
		m := squirrelOverlayIDRx.FindStringSubmatch(trimmed)
		if m == nil {
			return fmt.Errorf("malformed overlay line: %s", trimmed)
		}
		if !strings.HasPrefix(m[1], luaFixed49IDPrefix) {
			continue
		}
		if seenFixed[m[1]] {
			return fmt.Errorf("duplicate overlay row %s", m[1])
		}
		seenFixed[m[1]] = true
		fixedIDs = append(fixedIDs, m[1])
		if slices.Contains(luaFixed49CutRows, strings.TrimPrefix(m[1], luaFixed49IDPrefix)) {
			if !strings.Contains(trimmed, luaFixedCutProof) || strings.Contains(trimmed, luaFixed49SuiteFile) {
				return fmt.Errorf("cut row %s must cite its own proof (%s...) and not the 49-row suite", m[1], luaFixedCutProof)
			}
			if st := squirrelOverlayStateRx.FindStringSubmatch(trimmed); st == nil || st[1] != "done" {
				return fmt.Errorf("%s: a recorded cut row must be :state :done", m[1])
			}
			if rev := squirrelOverlayRevRx.FindStringSubmatch(trimmed); rev == nil || !isFortyHexSHA(rev[1]) {
				return fmt.Errorf("%s: :done row must carry a 40-hex :tested-revision", m[1])
			}
			continue
		}
		rows[m[1]] = trimmed
	}

	if len(rows) != 49 {
		return fmt.Errorf("overlay holds %d %s rows, want exactly 49", len(rows), luaFixed49IDPrefix)
	}
	if !sort.StringsAreSorted(fixedIDs) {
		return fmt.Errorf("overlay %s rows are not sorted by :id", luaFixed49IDPrefix)
	}

	for id := range rows {
		row, ok := strings.CutPrefix(id, luaFixed49IDPrefix)
		if !ok || !suiteRows[row] {
			return fmt.Errorf("overlay row %s is not one of the 49 suite rows", id)
		}
	}

	for _, c := range fixedproof.Cases() {
		id := luaFixed49IDPrefix + c.ID
		line, ok := rows[id]
		if !ok {
			return fmt.Errorf("missing overlay row %s", id)
		}
		stateMatch := squirrelOverlayStateRx.FindStringSubmatch(line)
		if stateMatch == nil || stateMatch[1] != "done" {
			return fmt.Errorf("%s: every fixed49 entry must be :state :done", id)
		}
		rev := squirrelOverlayRevRx.FindStringSubmatch(line)
		if rev == nil || !isFortyHexSHA(rev[1]) {
			return fmt.Errorf("%s: :done row must carry a 40-hex :tested-revision", id)
		}
		if n := squirrelOverlayNoteRx.FindStringSubmatch(line); n == nil || strings.TrimSpace(n[1]) == "" {
			return fmt.Errorf("%s: :done row must carry a :note", id)
		}
		first := luaFixed49Values
		if luaFixed49EvolutionRows[c.ID] {
			first = luaFixed49Evolution
		}
		// batch-capacity is the one evolution row the Lua runner skips by
		// design (its default batch capacity stays 4,096; only a caller-set
		// capacity goes above it, held by TestLuaFixedBatch4097Roundtrip).
		// Its skipped evolution subtest is not cited; the value subtest
		// that did run is cited instead.
		if c.ID == "batch-capacity" {
			first = luaFixed49Values
		}
		want := []string{
			luaFixed49SuiteFile + ": " + first + "/" + c.ID,
			luaFixed49SuiteFile + ": " + luaFixed49Negative + "/" + c.ID,
		}
		got := extractSquirrelEvidenceStrings(line)
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			return fmt.Errorf("%s: evidence = %q, want %q", id, got, want)
		}
	}
	return nil
}
