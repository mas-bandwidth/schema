// An enum variant may be deprecated in place, one way only, and the standard
// states that rule in docs/SPEC-TABLES.md §2.15. This gate holds the section
// to the eight rules the feature owes, and holds §3.4, §16.2 and §21.1 to the
// cross-references that keep one rule in one place.
package compiler

import (
	"os"
	"strings"
	"testing"
)

const deprecatedVariantHeading = "### 2.15 Enum variants: `| deprecated` in place"

func TestSpecGoDeprecatedVariant(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../docs/SPEC-TABLES.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	section, ok := docSection(doc, deprecatedVariantHeading)
	if !ok {
		t.Fatalf("docs/SPEC-TABLES.md has no section %q — the standard for a deprecated enum variant is missing", deprecatedVariantHeading)
	}

	rules := []struct {
		name    string
		literal string
	}{
		{"syntax", "| deprecated"},
		{"identity", "never reused"},
		{"lock", "widens"},
		{"undeprecation", "undeprecat"},
		{"read", "declared default"},
		{"write", "invalid_value"},
		{"compile time", "refused by name"},
		{"text/JSON", "keeps its name"},
	}
	for _, rule := range rules {
		if !strings.Contains(section, rule.literal) {
			t.Errorf("§2.15 does not state the %s rule: missing %q", rule.name, rule.literal)
		}
	}

	references := []struct {
		section string
		heading string
	}{
		{"§3.4", "### 3.4 The fixed form: a record of values under a layout"},
		{"§16.2", "### 16.2 The mapping, field kind by field kind"},
		{"§21.1", "### 21.1 The law: everything that inputs into a fixed table only widens"},
	}
	for _, ref := range references {
		body, ok := docSection(doc, ref.heading)
		if !ok {
			t.Errorf("docs/SPEC-TABLES.md has no %s section %q", ref.section, ref.heading)
			continue
		}
		if !strings.Contains(body, "§2.15") {
			t.Errorf("%s does not cross-reference §2.15", ref.section)
		}
	}
}

// docSection returns the body under heading, up to the next heading at the
// same or a higher level. A page that has no such heading returns false.
func docSection(doc, heading string) (string, bool) {
	_, after, ok := strings.Cut(doc, heading)
	if !ok {
		return "", false
	}
	rest := after
	end := len(rest)
	for _, sep := range []string{"\n## ", "\n### "} {
		if i := strings.Index(rest, sep); i >= 0 && i < end {
			end = i
		}
	}
	return rest[:end], true
}
