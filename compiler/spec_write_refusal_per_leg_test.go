// TestSpecWriteRefusalPerLegTableStands pins the per-leg write mechanism of
// docs/rulings.json `write-refusal` across the pages that state it: a
// debug-only assert in the legs whose idiom is one, a refusal in every build in
// the others, for every write-side check, a check that guards the writer's
// own storage included. SPEC.md §5.1's idiom table is the one statement of
// the build per leg; no page may state a write check that holds in every
// build in every leg.
package compiler

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestSpecWriteRefusalPerLegTableStands(t *testing.T) {
	t.Parallel()

	pages := map[string]string{}
	for _, name := range []string{"SPEC.md", "FAQ.md", "CONTRIBUTING.md"} {
		data, err := os.ReadFile("../docs/" + name)
		if err != nil {
			t.Fatal(err)
		}
		pages[name] = string(data)
	}

	// (a) No page states a write check uniform across every build and leg.
	for _, name := range []string{"SPEC.md", "FAQ.md", "CONTRIBUTING.md"} {
		flat := strings.Join(strings.Fields(pages[name]), " ")
		for _, stale := range []string{"in every build in every leg", "assert legs included"} {
			if strings.Contains(flat, stale) {
				t.Errorf("docs/%s still holds the stale %q", name, stale)
			}
		}
	}

	// (b) The ruling's two leg lists, against §5.1's idiom table.
	asserts, everyBuild := writeRefusalLegLists(t)
	section := specSection(t, "../docs/SPEC.md", "### 5.1 The write contract", "## 6. ")
	builds := idiomTableBuilds(t, section)

	listed := map[string]bool{}
	for _, leg := range asserts {
		listed[leg] = true
		build, ok := builds[leg]
		if !ok {
			t.Errorf("§5.1's idiom table has no row for the assert leg %s", leg)
			continue
		}
		if !strings.HasPrefix(build, "debug") {
			t.Errorf("§5.1's idiom table gives the assert leg %s the build %q, not a debug one", leg, build)
		}
	}
	for _, leg := range everyBuild {
		listed[leg] = true
		build, ok := builds[leg]
		if !ok {
			t.Errorf("§5.1's idiom table has no row for the every-build leg %s", leg)
			continue
		}
		if build != "every build" {
			t.Errorf("§5.1's idiom table gives the every-build leg %s the build %q", leg, build)
		}
	}
	// A leg the ruling does not place is skipped here: today that is Lobster,
	// whose mechanism is placed separately.
	for leg := range builds {
		if !listed[leg] && leg != "Lobster" {
			t.Errorf("§5.1's idiom table carries %s, which `write-refusal` places in neither list", leg)
		}
	}

	// (c) The paragraph on a count or length past the writer's own storage
	// names both mechanisms.
	start := strings.Index(section, "**One rule holds in every leg on top of the table.**")
	if start < 0 {
		t.Fatal("§5.1 no longer holds the paragraph on the storage a count or length describes")
	}
	storage := section[start:]
	if end := strings.Index(storage, "\n\n"); end >= 0 {
		storage = storage[:end]
	}
	storage = strings.Join(strings.Fields(storage), " ")
	for _, want := range []string{
		"a count or a length that would index past the storage it describes",
		"a debug assert in an assert leg",
		"a refusal in every build elsewhere",
	} {
		if !strings.Contains(strings.ToLower(storage), want) {
			t.Errorf("§5.1's storage paragraph does not hold %q", want)
		}
	}
}

// writeRefusalLegLists parses the parenthesised leg list after "debug-only
// assert" and the one after "refusal in every build" out of the
// `write-refusal` ruling's text.
func writeRefusalLegLists(t *testing.T) (asserts, everyBuild []string) {
	t.Helper()
	data, err := os.ReadFile("../docs/rulings.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Rulings []struct {
			ID     string `json:"id"`
			Source string `json:"source"`
		} `json:"rulings"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("docs/rulings.json: %v", err)
	}
	text := ""
	for _, r := range doc.Rulings {
		if r.ID == "write-refusal" {
			text = r.Source
		}
	}
	if text == "" {
		t.Fatal("docs/rulings.json has no `write-refusal` ruling")
	}
	asserts = legListAfter(t, text, "debug-only assert")
	everyBuild = legListAfter(t, text, "refusal in every build")
	if len(asserts) == 0 || len(everyBuild) == 0 {
		t.Fatalf("`write-refusal` names %d assert legs and %d every-build legs", len(asserts), len(everyBuild))
	}
	return asserts, everyBuild
}

func legListAfter(t *testing.T, text, marker string) []string {
	t.Helper()
	i := strings.Index(text, marker)
	if i < 0 {
		t.Fatalf("`write-refusal` no longer says %q", marker)
	}
	rest := text[i:]
	open := strings.Index(rest, "(")
	closing := strings.Index(rest, ")")
	if open < 0 || closing < open {
		t.Fatalf("`write-refusal` has no leg list after %q", marker)
	}
	var legs []string
	for leg := range strings.SplitSeq(rest[open+1:closing], ",") {
		if leg = strings.TrimSpace(leg); leg != "" {
			legs = append(legs, leg)
		}
	}
	return legs
}

// idiomTableBuilds reads §5.1's idiom table, the one headed
// "| leg | idiom | build |", into leg -> build cell.
func idiomTableBuilds(t *testing.T, section string) map[string]string {
	t.Helper()
	header := "| leg | idiom | build | how a refusal surfaces |"
	i := strings.Index(section, header)
	if i < 0 {
		t.Fatal("§5.1 no longer holds the idiom table")
	}
	builds := map[string]string{}
	for _, line := range strings.Split(section[i:], "\n")[2:] {
		if !strings.HasPrefix(line, "|") {
			break
		}
		cells := strings.Split(line, "|")
		if len(cells) < 5 {
			t.Fatalf("§5.1's idiom table row %q has too few cells", line)
		}
		builds[strings.TrimSpace(cells[1])] = strings.TrimSpace(cells[3])
	}
	return builds
}
